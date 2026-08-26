package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/model"
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

// Shot is one captured image. An alias of the report's own type, so this
// package does not carry a second definition that can drift from it.
type Shot = model.Shot

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

Take the screenshot with save_to_disk set to true. The tool result gives you
the path it wrote. Report that path back — do not try to move, copy or rename
the file, and do not write anything yourself. Something else does that.

Then return ONLY a JSON array, one entry per screenshot you actually took:
[{"capability":"","file":"","url":"","note":""}]

file     the path save_to_disk returned, exactly as given
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
	// Dir is where daybook files the images. The agent never writes here — it
	// reports the paths save_to_disk handed it and this copies them in, which
	// is why the capture agent needs no filesystem tools at all.
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
		src := s.File
		if !filepath.IsAbs(src) {
			src = filepath.Join(req.Dir, filepath.Base(src))
		}
		fi, err := os.Stat(src)
		if err != nil {
			continue // claimed a file that is not there
		}
		// A few hundred bytes of PNG is a blank page or a failed render. Small
		// enough to be certain, large enough not to discard a legitimately
		// plain screen.
		if fi.Size() < 3000 {
			continue
		}
		// Copy rather than move: the browser tool owns where it wrote, and
		// taking its file out from under it is not ours to do.
		// Keep the source's extension. The browser tool writes jpg, and a jpg
		// named .png is a file some viewers refuse to open — a picture nobody
		// can see is the same as no picture.
		ext := strings.ToLower(filepath.Ext(src))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
			ext = ".png"
		}
		name := slug(s.Capability) + ext
		if err := copyFile(src, filepath.Join(req.Dir, name)); err != nil {
			continue
		}
		s.File = name
		s.At = time.Now()
		kept = append(kept, s)
	}
	return kept, nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// slug names the image after what it shows, so the assets directory is
// browsable without opening anything.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash && b.Len() < 60:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "shot"
	}
	// The 60-char cap above stops mid-word and drops the separators after it,
	// which produced ...namespaceitwasissuedin. Cut on a boundary instead.
	if i := strings.LastIndexByte(out, '-'); len(out) > 48 && i > 24 {
		out = out[:i]
	}
	return out
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
