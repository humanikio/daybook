// Package sessions writes the per-day session index and keeps the transcript
// archive it points into.
//
// The index is a view over a Day, like the markdown: rebuilt from the same
// facts every time the day is written, never edited, so it cannot drift from
// the report. The archive is the opposite — the one thing here that is not
// regenerable, because its source is deleted by Claude Code after a month.
//
// The archive is never pruned. A session whose source has gone keeps its last
// copy; deleting on the strength of a file that no longer exists would destroy
// exactly what the archive is for.
package sessions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// activeWithin is how close to the end of the window a session's last message
// must be for it to count as still going when the day closed. An hour, because
// a session left at the nightly run's start and one left at lunch are different
// answers to "should I pick this back up".
const activeWithin = time.Hour

// Build derives the index from a day. Pure apart from locating transcripts
// for raw files written before streams recorded their own path.
func Build(cfg config.Config, day model.Day) model.SessionDay {
	staleAfter, _ := cfg.StaleAfter()
	out := model.SessionDay{
		Schema:      model.SessionSchema,
		Date:        day.Date,
		Machine:     day.Machine,
		Generated:   day.Generated,
		WindowStart: day.WindowStart,
		WindowEnd:   day.WindowEnd,
		Sessions:    []model.Session{},
	}
	archiving := Archiving(cfg)
	for _, s := range day.Streams {
		e := model.Session{
			SessionID: s.ID,
			State:     s.State,
			Live:      LiveAt(s.Last, day.WindowEnd, staleAfter),
			Agent:     s.Agent,
			Project:   s.Project,
			CWD:       s.CWD,
			Branch:    s.Branch,
			First:     s.First,
			Last:      s.Last,
			Resume:    ResumeCommand(s.CWD, s.ID),
		}
		e.Purpose, e.PurposeSource = purpose(s)
		for _, c := range s.Commits {
			e.Commits = append(e.Commits, c.Repo+"@"+short(c.SHA))
		}
		src := transcriptOf(cfg, s)
		e.Transcript.Source = src
		if archiving && src != "" {
			e.Transcript.Archive = filepath.ToSlash(filepath.Join("transcripts", s.Project, s.ID+".jsonl"))
			if isDir(strings.TrimSuffix(src, ".jsonl")) {
				e.Transcript.Dir = filepath.ToSlash(filepath.Join("transcripts", s.Project, s.ID))
			}
		}
		out.Sessions = append(out.Sessions, e)
	}
	return out
}

// Archiving reports whether transcripts are copied.
func Archiving(cfg config.Config) bool { return cfg.Sessions.Archive }

// LiveAt measures a session's last message against the end of its window.
//
// Against the window and not the clock, so a day rebuilt next month says what
// it said the night it was written.
func LiveAt(last, windowEnd time.Time, staleAfter time.Duration) model.Live {
	gap := windowEnd.Sub(last)
	switch {
	case gap <= activeWithin:
		return model.LiveActive
	case gap <= staleAfter:
		return model.LiveIdle
	}
	return model.LiveDormant
}

// ResumeCommand is what restores a session, from the directory it belongs to.
func ResumeCommand(cwd, id string) string {
	if cwd == "" {
		return "claude --resume " + id
	}
	q := "'" + strings.ReplaceAll(cwd, "'", `'\''`) + "'"
	if runtime.GOOS == "windows" {
		// PowerShell 5.1 has no &&, and doubles a quote rather than escaping it.
		q = "'" + strings.ReplaceAll(cwd, "'", "''") + "'"
		return "Set-Location " + q + "; claude --resume " + id
	}
	return "cd " + q + " && claude --resume " + id
}

// purpose prefers the narrated intent, which passed the verification gate,
// over a title nobody checked.
func purpose(s model.Stream) (string, string) {
	if s.Narration != nil && strings.TrimSpace(s.Narration.Intent) != "" {
		return strings.TrimSpace(s.Narration.Intent), "narration"
	}
	return s.Title, "title"
}

// transcriptOf finds a stream's file. Raw files from before Stream.Transcript
// existed are found by id, which is unique across projects.
func transcriptOf(cfg config.Config, s model.Stream) string {
	if s.Transcript != "" {
		return s.Transcript
	}
	for _, a := range cfg.Watch.Agents {
		if a.Source != "" && a.Source != "claude-code" {
			continue
		}
		hits, _ := filepath.Glob(filepath.Join(config.Expand(a.Path), "*", s.ID+".jsonl"))
		if len(hits) > 0 {
			return hits[0]
		}
	}
	return ""
}

// ArchiveResult counts what one pass did.
type ArchiveResult struct {
	Copied    int
	Unchanged int
	// Missing are sources that no longer exist; their archived copy is kept.
	Missing int
	Errors  []error
}

// Archive upserts the transcript of every session in the index: a file is
// copied when the archive has none or its bytes differ from the source, and
// left alone otherwise.
//
// Each session is its .jsonl plus the directory Claude Code keeps beside it
// (subagents, tool results), mirrored file by file under the same rule.
func Archive(cfg config.Config, sd model.SessionDay) ArchiveResult {
	var res ArchiveResult
	if !Archiving(cfg) {
		return res
	}
	root := cfg.OutputRoot()
	for _, s := range sd.Sessions {
		if s.Transcript.Source == "" || s.Transcript.Archive == "" {
			continue
		}
		upsert(&res, s.Transcript.Source, filepath.Join(root, filepath.FromSlash(s.Transcript.Archive)))

		srcDir := strings.TrimSuffix(s.Transcript.Source, ".jsonl")
		if s.Transcript.Dir == "" || !isDir(srcDir) {
			continue
		}
		dstDir := filepath.Join(root, filepath.FromSlash(s.Transcript.Dir))
		err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(srcDir, p)
			if err != nil {
				return nil
			}
			upsert(&res, p, filepath.Join(dstDir, rel))
			return nil
		})
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}
	return res
}

func upsert(res *ArchiveResult, src, dst string) {
	same, err := sameBytes(src, dst)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		res.Missing++
		return
	case err != nil:
		res.Errors = append(res.Errors, err)
		return
	case same:
		res.Unchanged++
		return
	}
	if err := copyFile(src, dst); err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("archive %s: %w", src, err))
		return
	}
	res.Copied++
}

// sameBytes compares the source against its archived copy. A missing SOURCE is
// reported as fs.ErrNotExist; a missing copy is simply "different".
func sameBytes(src, dst string) (bool, error) {
	si, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	di, err := os.Stat(dst)
	if err != nil || di.Size() != si.Size() {
		return false, nil
	}
	a, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer a.Close()
	b, err := os.Open(dst)
	if err != nil {
		return false, nil
	}
	defer b.Close()

	ba, bb := make([]byte, 64*1024), make([]byte, 64*1024)
	for {
		na, ea := io.ReadFull(a, ba)
		nb, _ := io.ReadFull(b, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false, nil
		}
		if ea == io.EOF || ea == io.ErrUnexpectedEOF {
			return true, nil
		}
		if ea != nil {
			return false, ea
		}
	}
}

// copyFile writes through a temporary file and renames it into place, so a
// reader of the archive never sees half a copy — and a crash mid-copy leaves
// the previous version rather than a truncated one.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return err
	}
	// 0600: a transcript is the whole conversation, tool output included.
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
