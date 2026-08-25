package narrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/humanikio/daybook/internal/config"
)

// DefaultModel is what the API provider uses unless narrate.model says
// otherwise. Not lowered for cost on the user's behalf — that is their call,
// and narrate.effort is the cheaper lever anyway.
const DefaultModel = "claude-opus-5"

// apiProvider talks to the Anthropic API directly.
//
// Chosen over the CLI when you would rather not spend your Claude Code
// subscription's quota on a nightly report, or want a specific model. It needs
// credentials the CLI does not.
type apiProvider struct {
	cfg    config.Config
	client anthropic.Client
}

func (a *apiProvider) Name() string { return "anthropic-api" }

// credentialsPresent reports whether the SDK will find something to
// authenticate with.
//
// AN UNSET ANTHROPIC_API_KEY DOES NOT MEAN THERE ARE NO CREDENTIALS. The SDK
// resolves, first match wins: ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, the
// active OAuth profile from `ant auth login`, workload identity federation,
// then the default profile on disk. Testing one env var would send anyone
// signed in through `ant` down the "no credentials" path for no reason.
//
// This is a pre-flight hint, not proof. The only proof is a request, and
// burning one to find out would cost money on every scan.
func credentialsPresent() bool {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	// Workload identity federation: activates only when the whole set is present.
	wif := 0
	for _, k := range []string{
		"ANTHROPIC_FEDERATION_RULE_ID", "ANTHROPIC_ORGANIZATION_ID",
		"ANTHROPIC_SERVICE_ACCOUNT_ID",
	} {
		if os.Getenv(k) != "" {
			wif++
		}
	}
	if wif == 3 && (os.Getenv("ANTHROPIC_IDENTITY_TOKEN_FILE") != "" || os.Getenv("ANTHROPIC_IDENTITY_TOKEN") != "") {
		return true
	}
	if _, err := exec.LookPath("ant"); err == nil {
		if err := exec.Command("ant", "auth", "status").Run(); err == nil {
			return true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".config", "anthropic")); err == nil {
			return true
		}
	}
	return false
}

func newAPIProvider(cfg config.Config) (*apiProvider, error) {
	if !credentialsPresent() {
		return nil, fmt.Errorf(
			"no Anthropic API credentials found — export ANTHROPIC_API_KEY, or run `ant auth login`, " +
				"or use narrate.provider: cli which needs no key at all")
	}
	// Zero-arg construction is deliberate: the SDK owns credential resolution,
	// and daybook never reads, stores, or passes a key of its own.
	return &apiProvider{cfg: cfg, client: anthropic.NewClient()}, nil
}

func (a *apiProvider) Complete(ctx context.Context, system, prompt string) (string, error) {
	model := a.cfg.Narrate.Model
	if model == "" {
		model = DefaultModel
	}

	params := anthropic.MessageNewParams{
		Model: anthropic.Model(model),
		// Narration output is short, but thinking is adaptive and bills as
		// output. 16k keeps a long turn from truncating mid-JSON while staying
		// under the SDK's non-streaming HTTP timeout.
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	}
	// Effort is the cost lever that does not change the model. Left unset it
	// defaults to high; "low" is right for a summarisation pass like this and
	// is what the docs suggest tuning first.
	if e := a.cfg.Narrate.Effort; e != "" {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(e)}
	}

	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("the API call timed out after %s (narrate.timeout)", a.cfg.NarrateTimeout())
		}
		return "", fmt.Errorf("anthropic api: %w", err)
	}

	// A refusal is an HTTP 200 with stop_reason "refusal", not an error. Reading
	// content without checking would return an empty string and look like a
	// parse failure, sending anyone debugging it in the wrong direction.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("the model declined this narration (%s)", resp.StopDetails.Category)
	}

	var out strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			out.WriteString(t.Text)
		}
	}
	return out.String(), nil
}
