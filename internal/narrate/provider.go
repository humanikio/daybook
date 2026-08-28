// Package narrate turns the derived facts into prose.
//
// THE DETERMINISTIC REPORT IS ALREADY WRITTEN before anything here runs. That
// ordering is the whole safety model: narration failing, timing out, or being
// refused by the verification gate costs you a few paragraphs, never the day.
//
// Two rules shape everything in this package:
//
//   - The model never sees a raw transcript. It sees the derived facts for one
//     stream. A day of transcripts is millions of tokens and inviting a model to
//     parse them would be slow, expensive, and non-deterministic in the one part
//     of the system that must be an accurate record.
//   - Anything checkable in the output must appear in the input. See verify.go.
package narrate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/humanikio/daybook/internal/config"
)

// Provider produces text from a prompt. No tools, no filesystem, no state.
type Provider interface {
	Name() string
	Complete(ctx context.Context, system, prompt string) (string, error)
}

// Resolve picks a provider from config, and says why when it cannot.
//
// The error is user-facing and is the ONLY place most people will learn that
// narration is unavailable — there is no earlier check that can tell them,
// because `claude doctor` exits 0 whether or not you are signed in. So it has
// to name the remedy, not just the failure.
func Resolve(cfg config.Config) (Provider, error) {
	want := cfg.Narrate.Provider
	if want == "" {
		want = "auto"
	}
	if want == "off" {
		return nil, fmt.Errorf("narration is off (narrate.provider: off)")
	}

	cliErr := probeCLI(cfg)
	switch want {
	case "cli":
		if cliErr != nil {
			return nil, cliErr
		}
		return &cliProvider{cfg: cfg}, nil
	case "api":
		return newAPIProvider(cfg)
	default: // auto
		// CLI first: it needs no key, no configuration and no spend beyond a
		// subscription the user already has. The API is the fallback rather
		// than the preference precisely because it asks for more.
		if cliErr == nil {
			return &cliProvider{cfg: cfg}, nil
		}
		p, apiErr := newAPIProvider(cfg)
		if apiErr == nil {
			return p, nil
		}
		return nil, fmt.Errorf("no narration provider available:\n  cli: %v\n  api: %v", cliErr, apiErr)
	}
}

// resolveBinary finds the Claude Code CLI, and does not rely on PATH alone.
//
// A scheduled run does not inherit your shell. launchd hands the daemon
// /usr/bin:/bin:/usr/sbin:/sbin, systemd gives it something similarly bare, and
// Claude Code installs to ~/.local/bin — so `exec.LookPath("claude")` succeeds
// every time you run daybook by hand and fails every night at 22:00. It did,
// silently, on the machine this was written on: narration was skipped and
// screenshots with it, and the only trace was one line in a log nobody reads.
//
// The most reliable candidate is daybook's OWN directory. Both are installed to
// ~/.local/bin by their own installers, and a service knows its own executable
// path however impoverished its PATH is.
//
// Returns an absolute path so the spawn does not depend on PATH either.
func resolveBinary(cfg config.Config) (string, error) {
	name := cfg.Narrate.Binary
	explicit := name != ""
	if !explicit {
		name = "claude"
	}

	// An explicit setting that names a path is taken as given: someone who wrote
	// it down means that binary, and silently using a different one is worse than
	// failing.
	if explicit && strings.ContainsRune(name, os.PathSeparator) {
		if usable(name) {
			return name, nil
		}
		return "", fmt.Errorf("narrate.binary is %q and that is not an executable file", name)
	}

	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	if p := findIn(name, binaryDirs()); p != "" {
		return p, nil
	}

	where := "PATH"
	if !explicit {
		where = "PATH or the usual install locations"
	}
	return "", fmt.Errorf("`%s` not found on %s — install Claude Code, or set narrate.binary "+
		"to its full path (a scheduled run does not inherit your shell's PATH)", name, where)
}

// findIn returns the first directory holding an executable called name, or "".
func findIn(name string, dirs []string) string {
	for _, dir := range dirs {
		if p := filepath.Join(dir, name); usable(p) {
			return p
		}
	}
	return ""
}

// binaryDirs is where Claude Code is found when PATH does not name it, best
// first.
func binaryDirs() []string {
	var dirs []string
	// daybook's own directory. Both installers write to ~/.local/bin, so this is
	// the one that holds when nothing else does.
	if exe, err := os.Executable(); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, "bin"),
		)
	}
	return append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin")
}

// usable reports whether a path is a file this process can execute.
func usable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true // execute bits do not carry the same meaning here
	}
	return fi.Mode().Perm()&0o111 != 0
}

func probeCLI(cfg config.Config) error {
	_, err := resolveBinary(cfg)
	return err
}

// Check reports whether narration would work, without spending a request.
//
// It cannot prove a sign-in — `claude doctor` exits 0 either way, and the only
// real proof is a call that costs money or quota. It proves the reachable
// parts: a binary on PATH, or credentials the SDK will find. The rest is
// reported at the moment it fails, with a message naming the remedy.
func Check(cfg config.Config) error {
	_, err := Resolve(cfg)
	return err
}

// cliProvider spawns Claude Code headlessly.
//
// It holds no credentials: the CLI uses whatever login is already on this
// machine. That is the reason this is the default provider — for the people who
// would want daybook at all, it needs no setup, no key, and no new secret.
type cliProvider struct {
	cfg     config.Config
	browser bool
	// shell grants Bash, so the agent can start and stop the dev servers the
	// capabilities live in. Only ever set when preview.start_servers is on.
	shell bool
}

func (c *cliProvider) Name() string { return "claude-cli" }

// authMarkers are stderr fragments meaning "not signed in" rather than "the run
// failed". The distinction matters because the remedies are unrelated: one is a
// bug to report, the other is one `claude` login away and nobody can fix it
// from a log line they do not understand.
var authMarkers = []string{
	"Please run /login",
	"authentication_failed",
	"oauth_org_not_allowed",
	"Invalid API key",
	"not authenticated",
	"credentials",
}

// BrowserRunner returns a runner with the browser tools loaded and permitted.
//
// TWO FLAGS, both required, and having one is the silent-failure shape:
// --chrome loads the server, and the allowlist entry permits it. With only the
// first, the agent sees the tools in its context and every call is refused.
//
// NOTE THE HYPHENS in the allowlist entry. Connector display names are
// normalised to underscores; this built-in server's tools keep theirs
// (mcp__claude-in-chrome__computer). The underscored spelling matches nothing
// and refuses every call while reading as correctly configured.
//
// No Write and no Edit. The agent looks and reports paths, and daybook files the
// images. A shell is granted only when the agent has to own the dev servers.
//
// daybook used to start and stop those itself and was bad at it: `next dev`
// spawns a next-server grandchild that escapes the process group, so a teardown
// that reported success left the port held for hours, and the port recorded on
// an earlier day was frequently not the port the app came up on. The agent is
// already in a shell and can read the port the app announces. The prompt names
// the exact commands it may run and requires it to stop what it started.
//
// This widens what the step can do, so it is gated on preview.start_servers,
// which is off by default.
func BrowserRunner(cfg config.Config) (func(context.Context, string, string) (string, error), error) {
	if err := probeCLI(cfg); err != nil {
		return nil, err
	}
	c := &cliProvider{cfg: cfg, browser: true, shell: cfg.Preview.StartServers}
	return c.Complete, nil
}

func (c *cliProvider) Complete(ctx context.Context, system, prompt string) (string, error) {
	// PER CALL, not per run. The budget used to be a single context spanning
	// every request, so a day with twelve streams spent it on them and the
	// final pass — the one that produces "what shipped", the section a reader
	// opens first — died of a timeout it never had a share of.
	//
	// A timeout named for one agent turn should bound one agent turn.
	budget := c.cfg.NarrateTimeout()
	if c.browser {
		budget = c.cfg.PreviewTimeout()
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	bin, err := resolveBinary(c.cfg)
	if err != nil {
		return "", err
	}
	args := []string{
		"-p",
		"--output-format", "text",
		// A headless run cannot answer a permission prompt, and this step has no
		// business touching the filesystem: it takes text and returns text. With
		// no tools granted under dontAsk, it structurally cannot do anything else.
		"--permission-mode", "dontAsk",
	}
	if c.browser {
		tools := "mcp__claude-in-chrome"
		if c.shell {
			tools += ",Bash"
		}
		args = append(args, "--chrome", "--allowedTools", tools)
	} else {
		args = append(args, "--tools", "")
	}
	{
		_ = 0
	}
	if system != "" {
		args = append(args, "--system-prompt", system)
	}
	if c.cfg.Narrate.Model != "" {
		args = append(args, "--model", c.cfg.Narrate.Model)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		stderr := errb.String()
		for _, m := range authMarkers {
			if strings.Contains(strings.ToLower(stderr), strings.ToLower(m)) {
				return "", fmt.Errorf("claude is not signed in on this machine — run `claude` and log in, then re-run `daybook narrate`")
			}
		}
		if ctx.Err() != nil {
			which := "narrate.timeout"
			if c.browser {
				which = "preview.timeout"
			}
			return "", fmt.Errorf("claude timed out after %s (%s)", budget, which)
		}
		if s := strings.TrimSpace(stderr); s != "" {
			return "", fmt.Errorf("claude failed: %s", firstLine(s))
		}
		return "", fmt.Errorf("claude failed: %w", err)
	}
	return out.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
