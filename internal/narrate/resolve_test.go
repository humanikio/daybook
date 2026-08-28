package narrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/humanikio/daybook/internal/config"
)

func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// The failure this exists for: a scheduled run does not inherit your shell.
// launchd hands the daemon /usr/bin:/bin:/usr/sbin:/sbin, Claude Code installs
// to ~/.local/bin, so LookPath succeeds every time you run daybook by hand and
// fails every night at 22:00.
func TestFindsTheBinaryOffPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute bits do not carry the same meaning here")
	}
	dir := t.TempDir()
	fakeExe(t, dir, "claude")

	if got := findIn("claude", []string{dir}); got != filepath.Join(dir, "claude") {
		t.Errorf("did not find the binary beside daybook: %q", got)
	}
	if got := findIn("claude", []string{t.TempDir()}); got != "" {
		t.Errorf("found something in an empty directory: %q", got)
	}
}

// A directory named claude must not be mistaken for the binary, and a file with
// no execute bit is not runnable.
func TestUsableRejectsWhatCannotRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute bits do not carry the same meaning here")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if usable(filepath.Join(dir, "claude")) {
		t.Error("a directory was treated as an executable")
	}
	plain := filepath.Join(dir, "notexec")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if usable(plain) {
		t.Error("a non-executable file was treated as runnable")
	}
}

// Someone who writes down a path means that binary. Silently running a
// different one is worse than failing.
func TestExplicitPathIsTakenAsGiven(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute bits do not carry the same meaning here")
	}
	dir := t.TempDir()
	want := fakeExe(t, dir, "my-claude")

	cfg := config.Config{Narrate: config.Narrate{Binary: want}}
	got, err := resolveBinary(cfg)
	if err != nil || got != want {
		t.Fatalf("got %q, %v — want the path that was configured", got, err)
	}

	cfg.Narrate.Binary = filepath.Join(dir, "does-not-exist")
	if _, err := resolveBinary(cfg); err == nil {
		t.Error("a configured path that does not exist was accepted")
	}
}

// The error has to name the actual cause. "not found on PATH" sent somebody
// looking at their shell profile for a problem that only happens under launchd.
func TestNotFoundErrorExplainsTheScheduledCase(t *testing.T) {
	cfg := config.Config{Narrate: config.Narrate{Binary: "definitely-not-a-real-binary-xyz"}}
	_, err := resolveBinary(cfg)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "does not inherit") {
		t.Errorf("the error does not mention that a scheduled run has a different PATH: %v", err)
	}
}
