package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Driving the browser.
//
// The agent is given what shipped, where it is served, and a directory to write
// into. It navigates — it does not construct URLs. That distinction is the
// whole reason this works: the routes that changed are patterns like
// /w/[workspaceId]/bulletin, which are not addresses. A person reaches that
// page by clicking, in a browser already signed in, and so does the agent.
//
// What the picture is FOR sets the bar. It is not a finished asset; it is
// "here is where in the product this lives", pairing with the file paths each
// capability already carries. Files tell an engineer where to look in the code.
// A picture tells anyone where to look in the app.

// Shot is one captured image.
type Shot struct {
	// Capability is the `what` line this illustrates.
	Capability string `json:"capability"`
	// File is the image, relative to the report.
	File string `json:"file"`
	// URL is where it was taken. Recorded because a screenshot asserts
	// something nothing can verify, and naming its source is the closest
	// available substitute for a proof.
	URL string `json:"url"`
	// Note is the agent's one line on what the reader is looking at.
	Note string    `json:"note,omitempty"`
	At   time.Time `json:"at"`
}

const captureSystem = `You are photographing what shipped today, so a teammate who was not there can
see where in the product it lives.

You will be told what shipped and which local servers are running. Use the
browser tools to look.

FOR EACH capability you are asked about:
  1. Navigate to it. Start from the app's root and CLICK THROUGH. Do not build a
     URL from a route pattern — /w/[workspaceId]/bulletin is not an address, and
     guessing an id lands on an error page that photographs perfectly.
  2. Look at the page and decide whether it is really the thing described. If it
     is a login screen, an error, an empty state, or simply the wrong screen,
     SKIP IT and say why.
  3. If it is right, take one screenshot.

Then return ONLY a JSON array, one entry per screenshot you actually took:
[{"capability":"","file":"","url":"","note":""}]

file     the path you saved to, inside the directory you were given
url      the address the browser was on
note     one sentence: what a reader is looking at and where it sits in the UI

RULES
- A missing picture is fine. A picture of the wrong screen is worse than
  nothing, because it is wrong and it is persuasive.
- Never sign in, never enter credentials, never submit a form, never click
  anything destructive. You are looking, not using.
- Prefer the screen where the feature LIVES over a modal mid-interaction — a
  reader needs to find it again.
- If nothing can be reached, return [].`

// CaptureRequest is what the agent gets.
type CaptureRequest struct {
	// Capabilities are the `what` lines to illustrate, most consequential first.
	Capabilities []string
	// Running describes the servers that are up, in the agent's own words.
	Running []string
	// Dir is where images go.
	Dir string
	// Max caps how many pictures come back, because a day with twenty-one
	// capabilities does not want twenty-one screenshots — it wants the few
	// worth looking at.
	Max int
}

// Prompt builds the instruction. Exported so `daybook shoot --dry-run` can show
// exactly what would be sent without sending it.
func (r CaptureRequest) Prompt() string {
	var b strings.Builder
	b.WriteString("Take at most ")
	fmt.Fprintf(&b, "%d screenshots.\n\n", r.Max)

	b.WriteString("RUNNING NOW:\n")
	for _, s := range r.Running {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	b.WriteString("\nWHAT SHIPPED, most consequential first:\n")
	for i, c := range r.Capabilities {
		fmt.Fprintf(&b, "%d. %s\n", i+1, c)
	}
	fmt.Fprintf(&b, "\nSave images into: %s\n", r.Dir)
	b.WriteString("Name them after the capability, lowercase with hyphens, .png\n")
	return b.String()
}

// Capture drives the agent and returns what it managed to photograph.
//
// Everything about the result is treated as a claim, not a fact: an image is
// kept only if it exists on disk and is big enough to be a screenshot rather
// than an error page's worth of blank. Nothing here can verify the picture is
// of the right thing — only the agent's own look can do that, which is why the
// prompt spends its strictest language on skipping.
func Capture(ctx context.Context, run Runner, req CaptureRequest) ([]Shot, error) {
	if len(req.Capabilities) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(req.Dir, 0o700); err != nil {
		return nil, err
	}
	out, err := run(ctx, captureSystem, req.Prompt())
	if err != nil {
		return nil, err
	}
	raw := ExtractArray(out)
	if raw == "" {
		return nil, fmt.Errorf("the agent returned no list")
	}
	var shots []Shot
	if err := json.Unmarshal([]byte(raw), &shots); err != nil {
		return nil, err
	}

	var kept []Shot
	for _, s := range shots {
		if len(kept) >= req.Max {
			break
		}
		p := s.File
		if !filepath.IsAbs(p) {
			p = filepath.Join(req.Dir, filepath.Base(p))
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue // claimed a file that is not there
		}
		// A few hundred bytes of PNG is a blank page or a failed render. Small
		// enough to be certain, large enough not to discard a legitimately
		// plain screen.
		if fi.Size() < 3000 {
			_ = os.Remove(p)
			continue
		}
		s.File = filepath.Base(p)
		s.At = time.Now()
		kept = append(kept, s)
	}
	return kept, nil
}

// Runner is how the agent is invoked, injected so this package neither imports
// narrate nor knows what a provider is.
type Runner func(ctx context.Context, system, prompt string) (string, error)

// ExtractArray pulls the first JSON array out of a response.
func ExtractArray(s string) string {
	start := strings.IndexByte(s, '[')
	end := strings.LastIndexByte(s, ']')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
