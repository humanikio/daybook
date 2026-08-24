// Package derive is the join: streams on one side, commits on the other, and
// the rules that decide how far each piece of work has got.
//
// Everything here is a pure function of data already at rest, which is what
// makes the whole tool safe to iterate on: change a rule, re-run over every day
// of history, and the reports rebuild. There is no migration step because
// nothing derived is ever the source of truth.
package derive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tndigitalmark/claude-code-daybook/internal/config"
	"github.com/tndigitalmark/claude-code-daybook/internal/model"
	"github.com/tndigitalmark/claude-code-daybook/internal/vcs"
)

// Input is everything a day is built from.
type Input struct {
	Cfg         config.Config
	Streams     []model.Stream
	Commits     []model.Commit
	Repos       []model.RepoState
	WindowStart time.Time
	WindowEnd   time.Time
	ParseErrors int
}

// attributionWindow is how far back a stream may reach to claim a commit.
//
// Seven days, not one. You routinely fix something belonging to an older stream
// without reopening that chat, and restricting candidates to today's active
// streams sends every one of those to the unattributed pile.
const attributionWindow = 7 * 24 * time.Hour

// Build produces the day.
func Build(in Input) model.Day {
	day := model.Day{
		Schema:      model.Schema,
		Date:        in.WindowEnd.Format("2006-01-02"),
		Machine:     in.Cfg.Machine(),
		Generated:   time.Now(),
		WindowStart: in.WindowStart,
		WindowEnd:   in.WindowEnd,
		Repos:       in.Repos,
		ParseErrors: in.ParseErrors,
	}

	streams := append([]model.Stream(nil), in.Streams...)
	for i := range streams {
		streams[i].Repos = reposFor(streams[i], in.Repos)
	}

	pins := loadPins(in.Cfg)
	remote := map[string]bool{}
	for _, r := range in.Repos {
		remote[r.Repo] = r.HasRemote
	}

	byID := map[string]int{}
	for i, s := range streams {
		byID[s.ID] = i
	}

	for _, c := range in.Commits {
		c.State = shipState(c, remote[c.Repo], in.Cfg.Output.NoRemote)

		// A pinned commit is never re-judged. That keeps attribution stable:
		// today's run cannot quietly reshuffle what yesterday's report said.
		key := c.Repo + "@" + c.SHA
		if p, ok := pins[key]; ok {
			if i, ok := byID[p.StreamID]; ok {
				c.StreamID = p.StreamID
				c.Confidence = p.Confidence
				streams[i].Commits = append(streams[i].Commits, c)
				continue
			}
		}

		i, conf := attribute(c, streams, in.WindowEnd)
		if i < 0 {
			c.Confidence = model.ConfNone
			day.Unattributed = append(day.Unattributed, c)
			continue
		}
		c.StreamID = streams[i].ID
		c.Confidence = conf
		streams[i].Commits = append(streams[i].Commits, c)
		pins[key] = pin{StreamID: c.StreamID, Confidence: conf}
	}

	staleAfter, _ := in.Cfg.StaleAfter()
	for i := range streams {
		sort.Slice(streams[i].Commits, func(a, b int) bool {
			return streams[i].Commits[a].At.Before(streams[i].Commits[b].At)
		})
		streams[i].State = streamState(streams[i], in.Repos, in.WindowEnd, staleAfter)
	}

	sort.Slice(streams, func(a, b int) bool { return streams[a].First.Before(streams[b].First) })
	day.Streams = streams
	day.Totals = totals(streams, day.Unattributed)

	savePins(in.Cfg, pins)
	return day
}

// attribute picks the stream a commit belongs to.
//
// Two tiers, and the gap between them is the honest part. An exact match means
// the stream demonstrably touched a file this commit changed. A repo match
// means only that both were in the same repository at the same time — which on
// a day with several streams in one repo is a guess, and is labelled as one.
func attribute(c model.Commit, streams []model.Stream, now time.Time) (int, model.Confidence) {
	best, bestScore := -1, 0
	bestConf := model.ConfNone

	for i, s := range streams {
		if s.Agent {
			continue // agent runs do not claim your commits
		}
		if now.Sub(s.Last) > attributionWindow {
			continue
		}
		// A commit cannot predate the work that produced it. Half an hour of
		// slack absorbs clock skew and a commit written just after the last
		// message of a stream.
		if c.At.Before(s.First.Add(-30 * time.Minute)) {
			continue
		}

		score, conf := 0, model.ConfNone
		if n := fileOverlap(c, s); n > 0 {
			score, conf = 10000+n*100, model.ConfExact
		} else if s.Repos[c.Repo] > 0 {
			// Deliberately NOT ranked by how often the stream mentioned this
			// repo. Mention count measures how chatty a stream was, not whether
			// it produced this commit — and it hands every commit in a busy
			// repo to whichever stream talked about it most. What actually
			// distinguishes concurrent streams in one repo is which of them was
			// running at the moment the commit was made.
			score, conf = 100, model.ConfRepo
		} else {
			continue
		}
		// Proximity in minutes to the nearest message, for both tiers: it
		// separates streams that overlap in files as well as ones that only
		// overlap in repo.
		gap := nearestGap(s, c.At)
		switch {
		case gap <= 5*time.Minute:
			score += 90
		case gap <= 30*time.Minute:
			score += 60
		case gap <= 2*time.Hour:
			score += 30
		}
		if score > bestScore {
			best, bestScore, bestConf = i, score, conf
		}
	}
	return best, bestConf
}

// nearestGap is how far a commit sits from the closest message in a stream.
//
// A commit is made at a moment, and the stream that was live at that moment is
// overwhelmingly the one that produced it. Using the nearest MESSAGE rather
// than the stream's span matters because a stream can be open for a week with
// long silences inside it.
func nearestGap(s model.Stream, at time.Time) time.Duration {
	best := time.Duration(1<<62 - 1)
	for _, p := range s.Prompts {
		if d := absDur(at.Sub(p.At)); d < best {
			best = d
		}
	}
	for _, n := range s.Notes {
		if d := absDur(at.Sub(n.At)); d < best {
			best = d
		}
	}
	return best
}

// fileOverlap counts files this commit changed that the stream also touched.
func fileOverlap(c model.Commit, s model.Stream) int {
	root := c.Root()
	if root == "" || len(s.Files) == 0 {
		return 0
	}
	changed := make(map[string]struct{}, len(c.Files))
	for _, f := range c.Files {
		changed[f] = struct{}{}
	}
	n := 0
	for p := range s.Files {
		abs := resolve(p, s.CWD)
		if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
			continue
		}
		rel := strings.TrimPrefix(abs, root+string(filepath.Separator))
		if _, ok := changed[rel]; ok {
			n++
		}
	}
	return n
}

// reposFor maps the absolute paths a stream touched onto repository names, and
// gives the stream's own working directory a small weight of its own.
func reposFor(s model.Stream, repos []model.RepoState) map[string]int {
	out := map[string]int{}
	for p, n := range s.Files {
		if name := repoOf(resolve(p, s.CWD), repos); name != "" {
			out[name] += n
		}
	}
	// A directory the stream operated in is worth more than a path that merely
	// appeared in a command: `cd` and `git -C` name the tree unambiguously,
	// where a scraped path might be a string in a heredoc.
	for d, n := range s.Dirs {
		if name := repoOf(filepath.Join(resolve(d, s.CWD), "x"), repos); name != "" {
			out[name] += n * 3
		}
	}
	if s.CWD != "" {
		if name := repoOf(filepath.Join(s.CWD, "x"), repos); name != "" {
			out[name] += 2
		}
	}
	return out
}

// resolve makes a path absolute against the session's working directory.
//
// In auto mode the shell is already inside the repository, so most paths in a
// command are relative and name nothing on their own. Without this, the busiest
// streams look like they touched no repository at all.
func resolve(p, cwd string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if cwd == "" {
		return ""
	}
	return filepath.Join(cwd, p)
}

// repoRoots is filled by SetRepoRoots before Build. Package-level because the
// mapping from absolute path to repo name is needed in several places and
// threading it through every signature buys nothing.
var repoRoots []vcs.Repo

// SetRepoRoots gives derive the absolute paths of discovered repositories.
func SetRepoRoots(rs []vcs.Repo) { repoRoots = rs }

func repoOf(path string, _ []model.RepoState) string {
	best, bestLen := "", 0
	for _, r := range repoRoots {
		pre := r.Root + string(filepath.Separator)
		if strings.HasPrefix(path, pre) && len(r.Root) > bestLen {
			best, bestLen = r.Name, len(r.Root)
		}
	}
	return best
}

// shipState answers "has this left the machine".
func shipState(c model.Commit, hasRemote bool, noRemote string) model.State {
	if c.Pushed {
		return model.StateShipped
	}
	if !hasRemote {
		// Nothing to push to. Either a commit is the bar, or the repo does not
		// participate — without a rule these commits could never reach a
		// terminal state and would sit local forever.
		if noRemote == "committed" {
			return model.StateShipped
		}
		return model.StateLocal
	}
	return model.StateLocal
}

// streamState derives the stream's standing from its commits and its repos.
func streamState(s model.Stream, repos []model.RepoState, now time.Time, staleAfter time.Duration) model.State {
	if len(s.Commits) > 0 {
		allShipped := true
		for _, c := range s.Commits {
			if c.State != model.StateShipped {
				allShipped = false
				break
			}
		}
		if allShipped {
			return model.StateShipped
		}
		return model.StateLocal
	}
	if now.Sub(s.Last) > staleAfter {
		return model.StateStale
	}
	return model.StateOpen
}

func totals(streams []model.Stream, orphans []model.Commit) model.Totals {
	var t model.Totals
	buckets := map[int64]struct{}{}
	repos := map[string]struct{}{}

	for _, s := range streams {
		if s.Agent {
			t.AgentStreams++
			continue
		}
		t.Streams++
		t.Prompts += len(s.Prompts)
		// Bucket union, not a sum: streams run concurrently, so adding their
		// active minutes would report more hours than the day contains.
		for _, p := range s.Prompts {
			buckets[p.At.Unix()/600] = struct{}{}
		}
		for _, n := range s.Notes {
			buckets[n.At.Unix()/600] = struct{}{}
		}
		for _, c := range s.Commits {
			t.Commits++
			t.Added += c.Added
			t.Deleted += c.Deleted
			repos[c.Repo] = struct{}{}
			switch c.State {
			case model.StateShipped:
				t.Shipped++
			case model.StateLocal:
				t.Local++
			}
		}
	}
	for _, c := range orphans {
		t.Commits++
		t.Added += c.Added
		t.Deleted += c.Deleted
		repos[c.Repo] = struct{}{}
		if c.State == model.StateShipped {
			t.Shipped++
		} else {
			t.Local++
		}
	}
	t.ActiveMinutes = len(buckets) * 10
	t.Repos = len(repos)
	return t
}

// ---- pins -------------------------------------------------------------------

type pin struct {
	StreamID   string           `json:"streamId"`
	Confidence model.Confidence `json:"confidence"`
}

func pinPath(cfg config.Config) string { return filepath.Join(cfg.StateDir(), "pins.json") }

func loadPins(cfg config.Config) map[string]pin {
	m := map[string]pin{}
	b, err := os.ReadFile(pinPath(cfg))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func savePins(cfg config.Config, m map[string]pin) {
	if err := os.MkdirAll(cfg.StateDir(), 0o700); err != nil {
		return
	}
	b, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return
	}
	_ = os.WriteFile(pinPath(cfg), b, 0o600)
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
