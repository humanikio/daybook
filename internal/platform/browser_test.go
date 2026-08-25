package platform

import (
	"path/filepath"
	"testing"
)

// Ready has to fold four independent signals, and the API key is the one that
// overrides everything else: the extension can be paired, the manifest written
// and the browser open, and it stays off while that variable exists.
func TestReadyRequiresEverything(t *testing.T) {
	full := BrowserState{ManifestPath: "/m", Paired: true, Running: true, Checkable: true}
	if !full.Ready() {
		t.Fatal("a fully configured machine reported not ready")
	}

	cases := map[string]BrowserState{
		"api key in the process":   {ManifestPath: "/m", Paired: true, Running: true, Checkable: true, APIKey: APIKeySites{Process: true}},
		"api key in the session":   {ManifestPath: "/m", Paired: true, Running: true, Checkable: true, APIKey: APIKeySites{Session: true}},
		"api key in a service def": {ManifestPath: "/m", Paired: true, Running: true, Checkable: true, APIKey: APIKeySites{Service: "/x.plist"}},
		"not paired":               {ManifestPath: "/m", Running: true, Checkable: true},
		"no browser running":       {ManifestPath: "/m", Paired: true, Checkable: true},
		"no manifest":              {Paired: true, Running: true, Checkable: true},
	}
	for name, st := range cases {
		if st.Ready() {
			t.Errorf("%s: reported ready", name)
		}
	}

	// Where the manifest cannot be observed, its absence must not count against
	// the machine — Windows keeps it in the registry, and a path check would
	// call every correct install broken.
	unobservable := BrowserState{Paired: true, Running: true, Checkable: false}
	if !unobservable.Ready() {
		t.Error("an unobservable manifest was treated as a missing one")
	}
}

// The remedy differs by where the key is, and naming the wrong place is useless
// advice to whoever has to act on it.
func TestSetupStepsNameThePlace(t *testing.T) {
	svc := filepath.Join("/tmp", "daybook.plist")
	steps := BrowserSetupSteps(BrowserState{
		ManifestPath: "/m", Paired: true, Running: true, Checkable: true,
		APIKey: APIKeySites{Service: svc},
	})
	if len(steps) != 1 {
		t.Fatalf("want one step, got %d: %v", len(steps), steps)
	}
	if !contains(steps[0], svc) {
		t.Errorf("the step does not name the file that injects it: %q", steps[0])
	}
}

// A machine with nothing wrong should be told nothing.
func TestNoStepsWhenReady(t *testing.T) {
	if s := BrowserSetupSteps(BrowserState{ManifestPath: "/m", Paired: true, Running: true, Checkable: true}); len(s) != 0 {
		t.Errorf("want no steps, got %v", s)
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (hay == needle || len(needle) == 0 || indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
