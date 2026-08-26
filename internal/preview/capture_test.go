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
	if _, err := os.Stat(tiny); !os.IsNotExist(err) {
		t.Error("left the rejected image on disk")
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
	for _, want := range []string{"at most 4", "localhost:3000", "You can now do X", "/tmp/shots"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt never mentions %q", want)
		}
	}
}
