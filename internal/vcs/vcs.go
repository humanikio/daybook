// Package vcs reads git — repository discovery, commit history, and how far
// each piece of work has travelled.
//
// The central question this package answers is "did this leave the machine",
// not "was this committed". A commit that only exists here is work at risk, and
// conflating the two hides the state where work actually gets lost.
package vcs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tndigitalmark/claude-code-daybook/internal/config"
	"github.com/tndigitalmark/claude-code-daybook/internal/model"
)

// Repo is a discovered repository.
type Repo struct {
	Root string
	Name string
}

// Discover walks the configured roots looking for .git.
//
// Measured at 150ms across 44 repositories on a warm cache. Nested repos are
// kept (a
// submodule is a real repo with its own remote and its own unpushed work), but
// anything inside a vendor or node_modules path is not.
func Discover(cfg config.Config) []Repo {
	seen := map[string]bool{}
	var out []Repo
	for _, root := range cfg.Watch.Repos {
		base := config.Expand(root.Path)
		depth := root.Depth
		if depth <= 0 {
			depth = 4
		}
		baseDepth := strings.Count(filepath.Clean(base), string(os.PathSeparator))

		_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable subtree is not fatal
			}
			if !d.IsDir() {
				return nil
			}
			name := d.Name()
			if name == "node_modules" || name == "vendor" || name == "Pods" {
				return filepath.SkipDir
			}
			if strings.Count(filepath.Clean(p), string(os.PathSeparator))-baseDepth > depth {
				return filepath.SkipDir
			}
			if name != ".git" {
				return nil
			}
			r := filepath.Dir(p)
			if seen[r] {
				return filepath.SkipDir
			}
			rn := filepath.Base(r)
			if cfg.Ignored(rn) {
				return filepath.SkipDir
			}
			seen[r] = true
			out = append(out, Repo{Root: r, Name: rn})
			return filepath.SkipDir
		})
	}
	return out
}

// Head returns the current commit of a repo by READING FILES, not by running
// git.
//
// Spawning `git rev-parse HEAD` across 44 repositories measured 400ms, almost
// all of it process spawn rather than work. This does the same job in a handful
// of file reads, which matters because HEAD is captured on every scan.
func Head(root string) string {
	b, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err != nil {
		// A submodule or worktree has .git as a FILE pointing elsewhere.
		return headViaGit(root)
	}
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, "ref:") {
		return s // detached HEAD, already a sha
	}
	ref := strings.TrimSpace(strings.TrimPrefix(s, "ref:"))
	if rb, err := os.ReadFile(filepath.Join(root, ".git", ref)); err == nil {
		return strings.TrimSpace(string(rb))
	}
	// Loose ref absent means it is in packed-refs.
	if pf, err := os.Open(filepath.Join(root, ".git", "packed-refs")); err == nil {
		defer pf.Close()
		sc := bufio.NewScanner(pf)
		for sc.Scan() {
			line := sc.Text()
			if line == "" || line[0] == '#' || line[0] == '^' {
				continue
			}
			if sha, name, ok := strings.Cut(line, " "); ok && name == ref {
				return sha
			}
		}
	}
	return headViaGit(root)
}

func headViaGit(root string) string {
	out, err := run(root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Status reports the working-tree standing of a repo.
func Status(root string, fetch bool) model.RepoState {
	st := model.RepoState{Repo: filepath.Base(root), HeadSHA: Head(root)}

	if out, err := run(root, "remote"); err == nil && strings.TrimSpace(out) != "" {
		st.HasRemote = true
	}
	if b, err := run(root, "symbolic-ref", "--short", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(b)
	}
	if fetch && st.HasRemote {
		_, _ = run(root, "fetch", "--quiet")
	}
	// @{u} fails when the branch has no upstream — not an error, just means
	// nothing to compare against.
	if a, err := run(root, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		st.Ahead, _ = strconv.Atoi(strings.TrimSpace(a))
	}
	if d, err := run(root, "status", "--porcelain"); err == nil {
		st.Dirty = countLines(d)
	}
	return st
}

// Log returns commits authored in [since,until] by any of authors.
//
// --all so work on a side branch is not invisible; --no-merges because a merge
// commit is not a piece of work.
func Log(root string, since, until time.Time, authors []string) ([]model.Commit, error) {
	args := []string{
		"log", "--all", "--no-merges",
		"--since", since.Format(time.RFC3339),
		"--until", until.Format(time.RFC3339),
		"--pretty=format:\x01%H\x02%aI\x02%an <%ae>\x02%s",
		"--numstat",
	}
	out, err := run(root, args...)
	if err != nil {
		return nil, err
	}

	pushed := pushedSet(root, since)
	name := filepath.Base(root)

	var commits []model.Commit
	var cur *model.Commit
	flush := func() {
		if cur != nil {
			commits = append(commits, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "\x01") {
			flush()
			parts := strings.Split(strings.TrimPrefix(line, "\x01"), "\x02")
			if len(parts) < 4 {
				continue
			}
			at, err := time.Parse(time.RFC3339, parts[1])
			if err != nil {
				continue
			}
			if !matchesAuthor(parts[2], authors) {
				continue
			}
			c := model.Commit{
				Repo:       name,
				SHA:        shortSHA(parts[0]),
				Subject:    parts[3],
				Author:     parts[2],
				At:         at.Local(),
				Pushed:     pushed[parts[0]],
				Confidence: model.ConfNone,
			}
			c.SetRoot(root)
			cur = &c
			continue
		}
		if cur == nil || strings.TrimSpace(line) == "" {
			continue
		}
		// numstat: added \t deleted \t path   ("-" for binary)
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		if n, err := strconv.Atoi(f[0]); err == nil {
			cur.Added += n
		}
		if n, err := strconv.Atoi(f[1]); err == nil {
			cur.Deleted += n
		}
		cur.Files = append(cur.Files, f[2])
	}
	flush()
	return commits, nil
}

// pushedSet is every commit reachable from a remote-tracking ref in the period.
//
// ONE git call for the whole repo rather than `branch -r --contains` per
// commit, which is O(commits) process spawns and visibly slow at fifty commits
// a day.
//
// No fetch is required for this to be correct about YOUR work: `git push`
// updates the local remote-tracking ref as part of pushing, so anything sent
// from this machine is already reflected. A fetch only reveals pushes made
// somewhere else, which is why watch.fetch defaults to off.
func pushedSet(root string, since time.Time) map[string]bool {
	set := map[string]bool{}
	out, err := run(root, "log", "--remotes", "--pretty=format:%H",
		"--since", since.Add(-48*time.Hour).Format(time.RFC3339))
	if err != nil {
		return set
	}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	return set
}

// matchesAuthor keeps commits that are yours.
//
// An empty author list means "everything", which is right for a personal
// machine and wrong on a shared repo — the wizard fills it in from git config
// so the permissive case is not the silent default.
func matchesAuthor(author string, authors []string) bool {
	if len(authors) == 0 {
		return true
	}
	a := strings.ToLower(author)
	for _, want := range authors {
		if want = strings.ToLower(strings.TrimSpace(want)); want != "" && strings.Contains(a, want) {
			return true
		}
	}
	return false
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Never let git open an editor, a pager, or a credential prompt: this runs
	// unattended from a scheduler, where any of those is an indefinite hang.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"GIT_OPTIONAL_LOCKS=0",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
