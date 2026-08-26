package preview

import (
	"testing"
	"time"
)

var now = time.Now()

// The real line from a transcript. It carries three separate facts and none of
// them are in the same word: what to run, how long it takes, and where it ends
// up listening.
func TestExtractsTheRealWorldShape(t *testing.T) {
	cmd := `(pnpm dev > /tmp/fe-dev.log 2>&1 &) ; sleep 25; curl -s localhost:3000 | head -3`
	s := FromCommand(cmd, "/repo/web", now)
	if s == nil {
		t.Fatal("extracted nothing")
	}
	if s.Command != "pnpm dev" {
		t.Errorf("Command = %q, want the core invocation without the wrapper", s.Command)
	}
	if s.BootSeconds != 25 {
		t.Errorf("BootSeconds = %d, want 25 — the wait somebody learned", s.BootSeconds)
	}
	if s.Port != 3000 {
		t.Errorf("Port = %d, want 3000", s.Port)
	}
}

// A command being written ABOUT is not a command being run. Transcripts of work
// on servers are full of these, and launching one would be daybook starting
// something arbitrary off the back of a sentence.
func TestIgnoresCommandsThatAreOnlyDiscussed(t *testing.T) {
	for _, cmd := range []string{
		`echo "run npm run dev to start it"`,
		`cat > README.md <<'EOF'` + "\n" + `Start with npm run dev` + "\n" + `EOF`,
		`grep -rn 'pnpm dev' docs/`,
	} {
		if s := FromCommand(cmd, "/repo", now); s != nil {
			t.Errorf("started something from a mention: %q → %q", cmd, s.Command)
		}
	}
}

func TestIgnoresOrdinaryCommands(t *testing.T) {
	for _, cmd := range []string{"go test ./...", "git status", "npm install", "ls -la"} {
		if s := FromCommand(cmd, "/repo", now); s != nil {
			t.Errorf("FromCommand(%q) matched %q, want nothing", cmd, s.Command)
		}
	}
}

// The same server is started many times a day. The most recent invocation is
// the current command, but the boot time may only have been written down once.
func TestDedupeKeepsLatestAndCarriesWhatItLearned(t *testing.T) {
	old := Server{Command: "pnpm dev", Dir: "/w", BootSeconds: 25, Port: 3000, At: now.Add(-2 * time.Hour)}
	recent := Server{Command: "pnpm dev", Dir: "/w", At: now}
	got := Dedupe([]Server{old, recent})
	if len(got) != 1 {
		t.Fatalf("want one entry, got %d", len(got))
	}
	if !got[0].At.Equal(now) {
		t.Error("kept the older observation")
	}
	if got[0].BootSeconds != 25 || got[0].Port != 3000 {
		t.Errorf("lost what the earlier run knew: boot=%d port=%d", got[0].BootSeconds, got[0].Port)
	}
}

func TestDedupeSeparatesDirectories(t *testing.T) {
	got := Dedupe([]Server{
		{Command: "npm run dev", Dir: "/a", At: now},
		{Command: "npm run dev", Dir: "/b", At: now},
	})
	if len(got) != 2 {
		t.Fatalf("two directories collapsed into %d entry", len(got))
	}
}

// The observation is faithful to what somebody did, which is right for a
// record and wrong for a timeout: a `sleep 2` beside a dev server was part of
// checking something else, and honouring it would call every slow app broken.
func TestBootWaitTreatsTheObservationAsAFloor(t *testing.T) {
	cases := map[int]time.Duration{
		0:   15 * time.Second, // never observed → a sane minimum
		2:   15 * time.Second, // too short to have been a real boot wait
		25:  37 * time.Second, // observed, plus half again: nobody is watching now
		600: 3 * time.Minute,  // capped
	}
	for observed, want := range cases {
		if got := (Server{BootSeconds: observed}).BootWait(); got != want {
			t.Errorf("BootSeconds=%d → %v, want %v", observed, got, want)
		}
	}
}
