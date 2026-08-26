package config

import (
	"testing"
	"time"
)

// "7d" is the first thing anyone types into a field labelled "how far back each
// run looks", and Go's parser rejects it — it stops at hours.
func TestParseDurationAcceptsDaysAndWeeks(t *testing.T) {
	cases := map[string]time.Duration{
		"24h":  24 * time.Hour,
		"7d":   7 * 24 * time.Hour,
		"1d":   24 * time.Hour,
		"2w":   14 * 24 * time.Hour,
		"0.5d": 12 * time.Hour,
		"90m":  90 * time.Minute,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Errorf("ParseDuration(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "banana", "7 days", "d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should have failed", bad)
		}
	}
}

// The window is read through Config, so the units have to survive that path too.
func TestWindowLengthUsesTheSameParser(t *testing.T) {
	c := Default()
	c.Window.Length = "7d"
	got, err := c.WindowLength()
	if err != nil || got != 7*24*time.Hour {
		t.Fatalf("WindowLength() = %v, %v — want 168h", got, err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() rejected a 7d window: %v", err)
	}
}

// Two gates, and having exactly one must do nothing. A master switch that looks
// on while nothing happens is the worst outcome available here — worse than
// off, because somebody stops looking for the reason.
func TestPreviewNeedsBothGates(t *testing.T) {
	cases := []struct {
		master, root bool
		want         bool
	}{
		{false, false, false},
		{true, false, false}, // enabled globally, no folder opted in
		{false, true, false}, // a folder asked, but the switch is off
		{true, true, true},
	}
	for _, c := range cases {
		cfg := Default()
		cfg.Preview.Enabled = c.master
		cfg.Watch.Repos = []RepoRoot{{Path: "~/code", Depth: 4, Preview: c.root}}
		if got := cfg.PreviewOn(); got != c.want {
			t.Errorf("master=%v root=%v → PreviewOn()=%v, want %v", c.master, c.root, got, c.want)
		}
	}
}

// Screenshots must be off out of the box. It drives a real browser as the user.
func TestPreviewIsOffByDefault(t *testing.T) {
	if Default().Preview.Enabled {
		t.Error("preview is enabled in the default config")
	}
	if Default().Preview.StartServers {
		t.Error("start_servers is enabled by default — it runs project code unattended")
	}
}

// The whole list survives a write, unlike the authors bug.
func TestPreviewSurvivesRender(t *testing.T) {
	cfg := Default()
	cfg.Preview.Enabled = true
	cfg.Preview.MaxPhotos = 9
	cfg.Watch.Repos = []RepoRoot{{Path: "~/a", Depth: 4, Preview: true}, {Path: "~/b", Depth: 2}}
	out := string(Render(cfg))
	for _, want := range []string{"enabled: true", "max_photos: 9", `path: "~/a", depth: 4, preview: true`} {
		if !contains(out, want) {
			t.Errorf("Render lost %q", want)
		}
	}
	if contains(out, `path: "~/b", depth: 2, preview: true`) {
		t.Error("Render marked a root that had not opted in")
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The same bug three times — commit attribution, file overlap, and which
// folders are marked for screenshots — each time because the fix lived
// privately in whichever package noticed it.
func TestHasPathPrefixFoldsCaseWhereTheFilesystemDoes(t *testing.T) {
	typed := "/Users/x/desktop/projects/example"
	recorded := "/Users/x/Desktop/Projects/Example/frontend"

	// Both behaviours, on whichever platform this runs. Case folding is right on
	// a Mac and wrong on Linux, where those are two different directories — so
	// asserting one of them unconditionally passed on a Mac and failed in CI.
	// Exercising both here means either platform's bug shows up on either.
	for _, folds := range []bool{true, false} {
		t.Run(map[bool]string{true: "folding", false: "exact"}[folds], func(t *testing.T) {
			restore := caseInsensitiveFS
			caseInsensitiveFS = folds
			defer func() { caseInsensitiveFS = restore }()

			if got := HasPathPrefix(recorded, typed); got != folds {
				if folds {
					t.Error("a path typed in lower case did not match one recorded in its real case")
				} else {
					t.Error("matched two paths that are different directories on this filesystem")
				}
			}
		})
	}
	// Matching a path in the case it was recorded in has to work everywhere, or
	// the assertion above is the only thing this test checks and it is skipped
	// on half the platforms.
	if !HasPathPrefix("/Users/x/Desktop/Projects/Example/frontend", "/Users/x/Desktop/Projects/Example") {
		t.Error("a path did not match a prefix written in the same case")
	}
	if HasPathPrefix("/Users/x/other/thing", typed) {
		t.Error("matched an unrelated path")
	}
	// A prefix that is not a whole path component must not match: /a/bc is not
	// inside /a/b.
	if HasPathPrefix("/Users/x/desktopher/thing", typed) {
		t.Error("matched a partial component")
	}
	if HasPathPrefix("", typed) || HasPathPrefix(recorded, "") {
		t.Error("matched an empty path")
	}
}
