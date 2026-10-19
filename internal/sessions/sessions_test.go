package sessions

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// fixture lays out a Claude Code projects dir with one session, its sidecar
// directory, and an output root, and returns a config pointing at both.
func fixture(t *testing.T) (config.Config, string) {
	t.Helper()
	base := t.TempDir()
	proj := filepath.Join(base, "projects", "-Users-me-repo")
	write(t, filepath.Join(proj, "abc.jsonl"), "{\"a\":1}\n")
	write(t, filepath.Join(proj, "abc", "subagents", "agent-1.jsonl"), "{\"s\":1}\n")
	write(t, filepath.Join(proj, "abc", "tool-results", "x.txt"), "output")

	cfg := config.Default()
	cfg.Output.Root = filepath.Join(base, "out")
	cfg.Watch.Agents = []config.AgentSource{{Source: "claude-code", Path: filepath.Join(base, "projects")}}
	return cfg, filepath.Join(proj, "abc.jsonl")
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dayWith(src string) model.Day {
	end := time.Date(2026, 10, 19, 23, 30, 0, 0, time.UTC)
	return model.Day{
		Date: "2026-10-19", Machine: "m", WindowStart: end.Add(-24 * time.Hour), WindowEnd: end,
		Streams: []model.Stream{{
			ID: "abc", Project: "-Users-me-repo", CWD: "/Users/me/repo", Title: "a title",
			Transcript: src, Last: end.Add(-10 * time.Minute), State: model.StateOpen,
		}},
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The whole contract in one sequence: first run copies, an untouched source
// copies nothing, a changed source is recopied, a deleted source keeps its copy.
func TestArchiveUpsertsOnByteDifference(t *testing.T) {
	cfg, src := fixture(t)
	sd := Build(cfg, dayWith(src))
	main := filepath.Join(cfg.OutputRoot(), "transcripts", "-Users-me-repo", "abc.jsonl")
	sub := filepath.Join(cfg.OutputRoot(), "transcripts", "-Users-me-repo", "abc", "subagents", "agent-1.jsonl")

	if r := Archive(cfg, sd); r.Copied != 3 || len(r.Errors) > 0 {
		t.Fatalf("first run: want 3 copied, got %+v", r)
	}
	if read(t, main) != "{\"a\":1}\n" || read(t, sub) != "{\"s\":1}\n" {
		t.Fatal("copy is not byte-for-byte")
	}
	if r := Archive(cfg, sd); r.Copied != 0 || r.Unchanged != 3 {
		t.Fatalf("unchanged source was recopied: %+v", r)
	}

	// Same length, different bytes: size alone must not decide.
	write(t, src, "{\"a\":2}\n")
	if r := Archive(cfg, sd); r.Copied != 1 {
		t.Fatalf("a same-size edit was missed: %+v", r)
	}
	if read(t, main) != "{\"a\":2}\n" {
		t.Fatal("archive does not hold the new bytes")
	}

	write(t, src, "{\"a\":2}\n{\"b\":3}\n")
	Archive(cfg, sd)
	if read(t, main) != "{\"a\":2}\n{\"b\":3}\n" {
		t.Fatal("an appended session was not recopied")
	}

	// Claude Code's cleanup removes the source. The archive is why we kept it.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if r := Archive(cfg, sd); r.Missing != 1 || len(r.Errors) > 0 {
		t.Fatalf("a missing source was not reported as missing: %+v", r)
	}
	if read(t, main) != "{\"a\":2}\n{\"b\":3}\n" {
		t.Fatal("the archived copy was lost when its source went away")
	}
}

func TestArchiveIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix permission bits")
	}
	cfg, src := fixture(t)
	Archive(cfg, Build(cfg, dayWith(src)))
	fi, err := os.Stat(filepath.Join(cfg.OutputRoot(), "transcripts", "-Users-me-repo", "abc.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("archive is %v, want 0600", fi.Mode().Perm())
	}
}

func TestArchiveOffWritesNothing(t *testing.T) {
	cfg, src := fixture(t)
	cfg.Sessions.Archive = false
	sd := Build(cfg, dayWith(src))
	if sd.Sessions[0].Transcript.Archive != "" {
		t.Error("index names an archive that will not be written")
	}
	if sd.Sessions[0].Transcript.Source != src {
		t.Error("index lost the source path")
	}
	Archive(cfg, sd)
	if _, err := os.Stat(cfg.TranscriptsDir()); err == nil {
		t.Error("wrote an archive anyway")
	}
}

// Raw files from before streams recorded their path must still resolve.
func TestBuildFindsTranscriptByID(t *testing.T) {
	cfg, src := fixture(t)
	d := dayWith("")
	sd := Build(cfg, d)
	if got := sd.Sessions[0].Transcript.Source; got != src {
		t.Errorf("source = %q, want %q", got, src)
	}
	if sd.Sessions[0].Transcript.Dir == "" {
		t.Error("sidecar directory not indexed")
	}
}

func TestPurposePrefersNarration(t *testing.T) {
	cfg, src := fixture(t)
	d := dayWith(src)
	if s := Build(cfg, d).Sessions[0]; s.Purpose != "a title" || s.PurposeSource != "title" {
		t.Errorf("without narration: %q from %q", s.Purpose, s.PurposeSource)
	}
	d.Streams[0].Narration = &model.Narration{Intent: "Designing a session index."}
	if s := Build(cfg, d).Sessions[0]; s.Purpose != "Designing a session index." || s.PurposeSource != "narration" {
		t.Errorf("with narration: %q from %q", s.Purpose, s.PurposeSource)
	}
}

func TestLiveAt(t *testing.T) {
	end := time.Date(2026, 10, 19, 23, 30, 0, 0, time.UTC)
	stale := 120 * time.Hour
	for _, c := range []struct {
		ago  time.Duration
		want model.Live
	}{
		{0, model.LiveActive},
		{time.Hour, model.LiveActive},
		{time.Hour + time.Minute, model.LiveIdle},
		{stale, model.LiveIdle},
		{stale + time.Minute, model.LiveDormant},
	} {
		if got := LiveAt(end.Add(-c.ago), end, stale); got != c.want {
			t.Errorf("%v before the window closed: %s, want %s", c.ago, got, c.want)
		}
	}
}

// A path with a quote in it must not break out of the quoting.
func TestResumeCommandQuotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell quoting")
	}
	got := ResumeCommand("/Users/me/it's here", "abc")
	want := `cd '/Users/me/it'\''s here' && claude --resume abc`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if !strings.HasPrefix(ResumeCommand("", "abc"), "claude --resume") {
		t.Error("no cwd should resume without a cd")
	}
}
