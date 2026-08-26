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
[{"item":0,"file":"","url":"","note":""}]

item     the NUMBER of the capability from the list above. Just the number.
         Do not retype the wording — it is matched exactly and a reworded
         line is dropped.
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
	// Servers are the apps the capabilities live in, with the command that
	// starts each one. The agent starts what it needs and stops it afterwards.
	Servers []ServerNote
	// MayStartServers is the config's start_servers gate. When it is off the
	// agent uses only what is already up and starts nothing.
	MayStartServers bool
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
// ServerNote is one app the agent may need serving, and how to serve it.
type ServerNote struct {
	Repo      string
	Command   string
	Dir       string
	Port      int
	AlreadyUp bool
	BootWait  string
}

// AnyUp reports whether anything is already serving.
func (r CaptureRequest) AnyUp() bool {
	for _, s := range r.Servers {
		if s.AlreadyUp {
			return true
		}
	}
	return false
}

func (r CaptureRequest) Prompt() string {
	var b strings.Builder
	b.WriteString("Take at most ")
	fmt.Fprintf(&b, "%d screenshots.\n\n", r.Max)

	b.WriteString("THE APPS:\n")
	for _, s := range r.Servers {
		if s.AlreadyUp {
			fmt.Fprintf(&b, "- %s is ALREADY SERVING on http://localhost:%d. Do not start it and do not stop it.\n", s.Repo, s.Port)
			continue
		}
		if !r.MayStartServers {
			fmt.Fprintf(&b, "- %s is not running, and you may not start it. Skip anything that needs it.\n", s.Repo)
			continue
		}
		fmt.Fprintf(&b, "- %s is NOT running. Start it with `%s` in %s. It usually takes about %s, and it announces its port on startup — read that, do not assume %d.\n",
			s.Repo, s.Command, s.Dir, s.BootWait, s.Port)
	}
	if r.MayStartServers {
		b.WriteString(`
STARTING AND STOPPING
Run each server in the background and keep its output, so you can read the port
it announces. It is frequently not the port written above.

YOU MUST STOP EVERY SERVER YOU STARTED before you finish, including when you
take no pictures at all and including when something goes wrong. Stop only what
you started — never one marked ALREADY SERVING. Kill the whole process group,
not just the process you launched: these dev servers spawn children that outlive
their parent and keep holding the port.

Do not run anything other than the start commands given above.
`)
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
	// The agent reports which capability a picture is of by NUMBER, and the
	// wording is then looked up here. It used to report the wording itself and
	// paraphrased it every time — "You can now test an ingest transform…" came
	// back as "Test an ingest transform…" — so the report keyed pictures to
	// capabilities by string and matched none of them. Every screenshot was
	// taken, stored, and then rendered nowhere.
	var claims []struct {
		Item int    `json:"item"`
		File string `json:"file"`
		URL  string `json:"url"`
		Note string `json:"note"`
		// Tolerated on input only, never trusted as the key.
		Capability string `json:"capability"`
	}
	if err := json.Unmarshal([]byte(raw), &claims); err != nil {
		return nil, err
	}
	var shots []Shot
	for _, c := range claims {
		cap := ""
		switch {
		case c.Item >= 1 && c.Item <= len(req.Capabilities):
			cap = req.Capabilities[c.Item-1]
		default:
			cap = closest(c.Capability, req.Capabilities)
		}
		if cap == "" {
			continue // cannot say what it is a picture of, so it is not usable
		}
		shots = append(shots, Shot{Capability: cap, File: c.File, URL: c.URL, Note: c.Note})
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
// closest falls back to word overlap when a number was not given, so an older
// agent reply that names the capability in its own words still lands. It has to
// clear a real bar: half the shorter line's words, and at least three of them.
func closest(claim string, options []string) string {
	want := words(claim)
	if len(want) < 3 {
		return ""
	}
	best, bestScore := "", 0
	for _, o := range options {
		have := words(o)
		n := 0
		for w := range want {
			if have[w] {
				n++
			}
		}
		if n > bestScore {
			best, bestScore = o, n
		}
	}
	shorter := len(want)
	if n := len(words(best)); n < shorter {
		shorter = n
	}
	if bestScore < 3 || bestScore*2 < shorter {
		return ""
	}
	return best
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,:;—-()\"'`")
		if len(w) > 3 {
			m[w] = true
		}
	}
	return m
}

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
