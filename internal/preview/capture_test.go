package preview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeAgent(reply string) Runner {
	return func(context.Context, string, string) (string, error) { return reply, nil }
}

// A shot is a claim until the file is on disk. An agent that says it took a
// picture and did not must not put a broken image in the report.
func TestCaptureDropsClaimsWithNoFile(t *testing.T) {
	dir := t.TempDir()
	got, err := Capture(context.Background(),
		fakeAgent(`[{"capability":"A","file":"missing.png","url":"http://x"}]`),
		CaptureRequest{Capabilities: []string{"A"}, Dir: dir, Max: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("kept a shot whose file does not exist: %v", got)
	}
}

// A few hundred bytes of PNG is a blank page or a failed render — the shape a
// wrong screenshot actually takes.
func TestCaptureDropsBlankImages(t *testing.T) {
	dir := t.TempDir()
	tiny := filepath.Join(dir, "blank.png")
	if err := os.WriteFile(tiny, make([]byte, 400), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := Capture(context.Background(),
		fakeAgent(`[{"capability":"A","file":"blank.png","url":"http://x"}]`),
		CaptureRequest{Capabilities: []string{"A"}, Dir: dir, Max: 3})
	if len(got) != 0 {
		t.Error("kept an image too small to be a screenshot")
	}
}

// The cap is the whole reason a day with twenty-one capabilities does not
// produce twenty-one pictures.
func TestCaptureHonoursTheCap(t *testing.T) {
	dir := t.TempDir()
	var entries []string
	for _, n := range []string{"a", "b", "c", "d"} {
		p := filepath.Join(dir, n+".png")
		if err := os.WriteFile(p, make([]byte, 9000), 0o600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, `{"capability":"`+n+`","file":"`+n+`.png","url":"http://x"}`)
	}
	got, _ := Capture(context.Background(),
		fakeAgent("["+strings.Join(entries, ",")+"]"),
		CaptureRequest{Capabilities: []string{"a"}, Dir: dir, Max: 2})
	if len(got) != 2 {
		t.Errorf("returned %d shots against a cap of 2", len(got))
	}
}

// The agent hands back wherever save_to_disk wrote. daybook files it, so the
// agent needs no filesystem tools — the smallest surface that can do the job.
func TestCaptureFilesTheImageItself(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	orig := filepath.Join(src, "screenshot-1724630000.png")
	if err := os.WriteFile(orig, make([]byte, 9000), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Capture(context.Background(),
		fakeAgent(`[{"capability":"You can now sort the board","file":"`+orig+`","url":"http://x"}]`),
		CaptureRequest{Capabilities: []string{"x"}, Dir: dir, Max: 3})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[0].File != "you-can-now-sort-the-board.png" {
		t.Errorf("named it %q — want it named after what it shows", got[0].File)
	}
	if _, err := os.Stat(filepath.Join(dir, got[0].File)); err != nil {
		t.Error("did not file the image into the assets directory")
	}
	// The browser tool owns where it wrote; taking its file is not ours to do.
	if _, err := os.Stat(orig); err != nil {
		t.Error("moved the original instead of copying it")
	}
}

// An empty array is a legitimate answer — nothing reachable was worth a
// picture — and must not read as a failure.
func TestCaptureAcceptsNothingFound(t *testing.T) {
	got, err := Capture(context.Background(), fakeAgent(`[]`),
		CaptureRequest{Capabilities: []string{"A"}, Dir: t.TempDir(), Max: 3})
	if err != nil || len(got) != 0 {
		t.Errorf("an empty result errored: %v %v", got, err)
	}
}

// The prompt is what someone will read to decide whether to trust this.
func TestPromptCarriesTheCapAndTheServers(t *testing.T) {
	p := CaptureRequest{
		Capabilities: []string{"You can now do X"},
		Running:      []string{"web on http://localhost:3000"},
		Dir:          "/tmp/shots", Max: 4,
	}.Prompt()
	for _, want := range []string{"at most 4", "localhost:3000", "You can now do X"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt never mentions %q", want)
		}
	}
}

// The browser tool writes jpg. A jpg named .png is a file some viewers refuse,
// and a picture nobody can open is the same as no picture.
func TestCaptureKeepsTheSourceExtension(t *testing.T) {
	src, dir := t.TempDir(), t.TempDir()
	orig := filepath.Join(src, "screenshot-1787719766684-1.jpg")
	if err := os.WriteFile(orig, make([]byte, 9000), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := Capture(context.Background(),
		fakeAgent(`[{"capability":"A thing shipped","file":"`+orig+`","url":"http://x"}]`),
		CaptureRequest{Capabilities: []string{"x"}, Dir: dir, Max: 1})
	if len(got) != 1 || filepath.Ext(got[0].File) != ".jpg" {
		t.Fatalf("named it %v — want the extension it actually is", got)
	}
}

// The length cap used to stop mid-word and drop the separators after it,
// producing names like ...namespaceitwasissuedin.
func TestSlugCutsOnAWordBoundary(t *testing.T) {
	got := slug("SQL run from the agent or any console is confined to the namespace it was issued in")
	if strings.HasSuffix(got, "-") || strings.Contains(got, "namespaceit") {
		t.Errorf("slug ran words together: %q", got)
	}
}
