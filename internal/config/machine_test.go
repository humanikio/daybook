package config

import (
	"os"
	"path/filepath"
	"testing"
)

// macOS renumbers the hostname when another machine claims the same name, which
// silently splits one laptop's history in two.
func TestMachineKeepsTheNameTheHistoryAlreadyUses(t *testing.T) {
	root := t.TempDir()
	c := Config{Output: Output{Root: root}}
	if err := os.MkdirAll(c.RawDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-08-18", "2026-08-19", "2026-08-20"} {
		if err := os.WriteFile(filepath.Join(c.RawDir(), d+".MacBook-Pro-4.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.priorMachine(base("MacBook-Pro-5")); got != "MacBook-Pro-4" {
		t.Fatalf("got %q — a renumbered hostname must not start a second history", got)
	}
	// A genuinely different machine must not be adopted.
	if got := c.priorMachine(base("Air")); got != "" {
		t.Fatalf("adopted %q for an unrelated machine", got)
	}
}

func TestBaseStripsOnlyTheCollisionSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"MacBook-Pro-4":  "MacBook-Pro",
		"MacBook-Pro":    "MacBook-Pro",
		"build-box-2026": "build-box-2026", // four digits is not a suffix
		"ci-7":           "ci",
	} {
		if got := base(in); got != want {
			t.Errorf("base(%q) = %q, want %q", in, got, want)
		}
	}
}
