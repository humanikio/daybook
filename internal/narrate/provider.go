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
	"os/exec"
	"strings"

	"github.com/tndigitalmark/daybook/internal/config"
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

func probeCLI(cfg config.Config) error {
	bin := cfg.Narrate.Binary
	if bin == "" {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("`%s` not found on PATH — install Claude Code, or set narrate.binary", bin)
	}
	return nil
}

// cliProvider spawns Claude Code headlessly.
//
// It holds no credentials: the CLI uses whatever login is already on this
// machine. That is the reason this is the default provider — for the people who
// would want daybook at all, it needs no setup, no key, and no new secret.
type cliProvider struct{ cfg config.Config }

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

func (c *cliProvider) Complete(ctx context.Context, system, prompt string) (string, error) {
	bin := c.cfg.Narrate.Binary
	if bin == "" {
		bin = "claude"
	}
	args := []string{
		"-p",
		"--output-format", "text",
		// A headless run cannot answer a permission prompt, and this step has no
		// business touching the filesystem: it takes text and returns text. With
		// no tools granted under dontAsk, it structurally cannot do anything else.
		"--permission-mode", "dontAsk",
		"--tools", "",
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
			return "", fmt.Errorf("claude timed out after %s (narrate.timeout)", c.cfg.NarrateTimeout())
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
