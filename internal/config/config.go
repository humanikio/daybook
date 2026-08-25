// Package config loads ~/.daybook/config.yaml.
//
// Precedence: env vars > file > built-in defaults. Every field is optional;
// omitting one keeps its default, so a config file can be three lines long.
//
// QUOTING. Every string value in the example config is quoted, and the loader
// takes times as strings rather than letting the YAML layer guess. This is not
// stylistic: YAML 1.1 implementations resolve `12:00` to the integer 43200
// (base-60), `no` and `NO` to false, and `010` to 8. Go's yaml.v3 is stricter
// than that, but the config is written by hand and read by humans who may run
// it through other tools, so the safe form is the documented one.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type AgentSource struct {
	Source string `yaml:"source"`
	Path   string `yaml:"path"`
}

type RepoRoot struct {
	Path  string `yaml:"path"`
	Depth int    `yaml:"depth"`
}

type Watch struct {
	Agents []AgentSource `yaml:"agents"`
	Repos  []RepoRoot    `yaml:"repos"`
	// Fetch before reading remote refs. Off by default and rarely needed:
	// `git push` updates the local tracking ref, so the ahead-count is already
	// correct for anything pushed from this machine. Turn it on only to see
	// pushes made from somewhere else — it costs a network round trip per repo
	// and can prompt for credentials.
	Fetch bool `yaml:"fetch"`
	// Ignore repos whose basename matches any of these globs.
	Ignore []string `yaml:"ignore"`
}

type Window struct {
	Length string `yaml:"length"`
	// Scope decides how much of an active session is reported.
	//
	//   window  — only messages inside the window. A seven-day session
	//             contributes today's work and nothing else.
	//   session — the entire session, every day it is active.
	//
	// Measured on the reference corpus: session scope reads 667,071 tokens
	// against 177,826 for window, and almost all of the difference is work
	// already reported on previous days.
	Scope      string `yaml:"scope"`
	StaleAfter string `yaml:"stale_after"`
}

type Schedule struct {
	At      string   `yaml:"at"`
	Days    []string `yaml:"days"`
	CatchUp bool     `yaml:"catch_up"`
}

type Identity struct {
	// Authors bounds which commits are yours. On a shared repo, leaving this
	// empty after detection fails means claiming your team's output.
	Authors []string `yaml:"authors"`
	Machine string   `yaml:"machine"`
}

type Output struct {
	Root string `yaml:"root"`
	// NoRemote is the bar for repos that have no remote at all, where
	// "shipped" is undefined: `committed` treats a commit as done, `exclude`
	// leaves them out. Without an answer those streams could never close.
	NoRemote string `yaml:"no_remote"`
}

type Narrate struct {
	Enabled bool `yaml:"enabled"`
	// Provider resolves which backend writes the prose.
	//
	//   auto — claude CLI if it works, else the API if credentials resolve,
	//          else off. The default.
	//   cli  — always the CLI. Uses the login already on this machine and
	//          spends that subscription's quota.
	//   api  — always the Anthropic API.
	//   off  — never narrate.
	//
	// "off" is not a degraded mode. The deterministic report is the product;
	// narration is a layer on top of a file that is already written.
	Provider string `yaml:"provider"`
	Binary   string `yaml:"binary"`
	Model    string `yaml:"model"`
	// Effort trades depth for cost on the api provider: low | medium | high |
	// xhigh | max. Empty uses the API default. It changes how hard the model
	// thinks, not which model runs, so it is the right lever to reach for
	// before downgrading the model.
	Effort  string `yaml:"effort"`
	Timeout string `yaml:"timeout"`
	// Concurrency bounds parallel per-stream calls.
	Concurrency int `yaml:"concurrency"`
}

type Redaction struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"`
}

type Privacy struct {
	KeepRawPrompts bool        `yaml:"keep_raw_prompts"`
	Redact         []Redaction `yaml:"redact"`
}

type Business struct {
	Name  string   `yaml:"name"`
	Repos []string `yaml:"repos"`
}

type Config struct {
	Watch      Watch      `yaml:"watch"`
	Window     Window     `yaml:"window"`
	Schedule   Schedule   `yaml:"schedule"`
	Identity   Identity   `yaml:"identity"`
	Output     Output     `yaml:"output"`
	Narrate    Narrate    `yaml:"narrate"`
	Privacy    Privacy    `yaml:"privacy"`
	Businesses []Business `yaml:"business"`

	// path is where this config was read from. Not a yaml field.
	path string
}

func (c Config) Path() string { return c.path }

// Dir is the config directory: $DAYBOOK_DIR, else ~/.daybook.
func Dir() string {
	if d := os.Getenv("DAYBOOK_DIR"); d != "" {
		return Expand(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".daybook"
	}
	return filepath.Join(home, ".daybook")
}

// File is the config path.
func File() string { return filepath.Join(Dir(), "config.yaml") }

// Expand resolves a leading ~ and any environment variables in a path.
func Expand(p string) string {
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// Default is the built-in config, before file or env.
func Default() Config {
	return Config{
		Watch: Watch{
			Agents: []AgentSource{{Source: "claude-code", Path: "~/.claude/projects"}},
			Repos:  nil,
			Fetch:  false,
		},
		Window:   Window{Length: "24h", Scope: "window", StaleAfter: "120h"},
		Schedule: Schedule{At: "23:30", CatchUp: true},
		Output:   Output{Root: "~/.daybook", NoRemote: "committed"},
		Narrate:  Narrate{Enabled: false, Provider: "auto", Timeout: "5m", Concurrency: 3},
		Privacy: Privacy{
			KeepRawPrompts: true,
			Redact: []Redaction{
				{Name: "aws-access-key", Pattern: `AKIA[0-9A-Z]{16}`},
				{Name: "bearer-token", Pattern: `sk-[A-Za-z0-9_-]{20,}`},
				{Name: "github-token", Pattern: `gh[pousr]_[A-Za-z0-9]{20,}`},
				{Name: "private-key", Pattern: `-----BEGIN [A-Z ]*PRIVATE KEY-----`},
			},
		},
	}
}

// DefaultWithEnv is Default with environment overrides applied.
//
// The wizard needs this: it builds a fresh config rather than loading one, and
// without env applied here DAYBOOK_OUTPUT would be honoured by every command
// except the one that writes the file naming it.
func DefaultWithEnv() Config {
	c := Default()
	applyEnv(&c)
	return c
}

// Load reads the config file, applies defaults for anything absent, then env
// overrides, then validates.
//
// A missing file is NOT an error: the defaults are usable and `daybook init`
// is a convenience, not a precondition. Callers that need a real file (the
// wizard offering to overwrite) check for it themselves.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = File()
	}
	cfg.path = path

	if b, err := os.ReadFile(path); err == nil {
		// Decode over the defaults so an omitted key keeps its default rather
		// than becoming a zero value.
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}

	applyEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("DAYBOOK_OUTPUT"); v != "" {
		c.Output.Root = v
	}
	if v := os.Getenv("DAYBOOK_WINDOW"); v != "" {
		c.Window.Length = v
	}
	if v := os.Getenv("DAYBOOK_MACHINE"); v != "" {
		c.Identity.Machine = v
	}
}

// Validate rejects values that would otherwise fail late and confusingly.
//
// Every duration and enum is checked here rather than at the point of use, so
// `daybook verify` can report a bad config without running a scan.
func (c *Config) Validate() error {
	if _, err := c.WindowLength(); err != nil {
		return fmt.Errorf("window.length: %w", err)
	}
	if _, err := c.StaleAfter(); err != nil {
		return fmt.Errorf("window.stale_after: %w", err)
	}
	switch c.Window.Scope {
	case "window", "session":
	default:
		return fmt.Errorf("window.scope: want \"window\" or \"session\", got %q", c.Window.Scope)
	}
	switch c.Output.NoRemote {
	case "committed", "exclude":
	default:
		return fmt.Errorf("output.no_remote: want \"committed\" or \"exclude\", got %q", c.Output.NoRemote)
	}
	switch c.Narrate.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("narrate.effort: want low|medium|high|xhigh|max, got %q", c.Narrate.Effort)
	}
	switch c.Narrate.Provider {
	case "", "auto", "cli", "api", "off":
	default:
		return fmt.Errorf("narrate.provider: want auto|cli|api|off, got %q", c.Narrate.Provider)
	}
	if c.Narrate.Timeout != "" {
		if _, err := time.ParseDuration(c.Narrate.Timeout); err != nil {
			return fmt.Errorf("narrate.timeout: %w", err)
		}
	}
	if c.Schedule.At != "" {
		if _, err := time.Parse("15:04", c.Schedule.At); err != nil {
			return fmt.Errorf("schedule.at: want a quoted \"HH:MM\", got %q", c.Schedule.At)
		}
	}
	for _, r := range c.Privacy.Redact {
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("privacy.redact[%s]: %w", r.Name, err)
		}
	}
	return nil
}

func (c Config) WindowLength() (time.Duration, error) {
	if c.Window.Length == "" {
		return 24 * time.Hour, nil
	}
	return time.ParseDuration(c.Window.Length)
}

func (c Config) StaleAfter() (time.Duration, error) {
	if c.Window.StaleAfter == "" {
		return 120 * time.Hour, nil
	}
	return time.ParseDuration(c.Window.StaleAfter)
}

func (c Config) NarrateTimeout() time.Duration {
	if d, err := time.ParseDuration(c.Narrate.Timeout); err == nil && d > 0 {
		return d
	}
	return 5 * time.Minute
}

func (c Config) OutputRoot() string { return Expand(c.Output.Root) }
func (c Config) OutputsDir() string { return filepath.Join(c.OutputRoot(), "outputs") }
func (c Config) RawDir() string     { return filepath.Join(c.OutputRoot(), "raw") }
func (c Config) StateDir() string   { return filepath.Join(c.OutputRoot(), "state") }

// Machine names this machine in output filenames, so two machines writing to
// one synced directory never collide on the same file.
func (c Config) Machine() string {
	if c.Identity.Machine != "" {
		return sanitize(c.Identity.Machine)
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "local"
	}
	return sanitize(strings.TrimSuffix(h, ".local"))
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitize(s string) string {
	s = unsafeName.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// BusinessFor maps a repo name to a business label via the configured globs.
// Empty when nothing matches — the renderer then groups by repo instead.
func (c Config) BusinessFor(repo string) string {
	for _, b := range c.Businesses {
		for _, pat := range b.Repos {
			if ok, _ := filepath.Match(pat, repo); ok {
				return b.Name
			}
		}
	}
	return ""
}

// Ignored reports whether a repo basename matches watch.ignore.
func (c Config) Ignored(repo string) bool {
	for _, pat := range c.Watch.Ignore {
		if ok, _ := filepath.Match(pat, repo); ok {
			return true
		}
	}
	return false
}

// DetectAuthors reads git's own idea of who you are. Used by the wizard so the
// common case needs no typing, and by Load-time callers when authors is empty.
func DetectAuthors() []string {
	out, err := exec.Command("git", "config", "--get", "user.email").Output()
	if err != nil {
		return nil
	}
	if e := strings.TrimSpace(string(out)); e != "" {
		return []string{e}
	}
	return nil
}

// Redactor compiles the privacy patterns once.
type Redactor struct{ pats []*regexp.Regexp }

func (c Config) Redactor() *Redactor {
	r := &Redactor{}
	for _, p := range c.Privacy.Redact {
		if re, err := regexp.Compile(p.Pattern); err == nil {
			r.pats = append(r.pats, re)
		}
	}
	return r
}

// Do replaces every match with a marker. It runs before anything reaches disk,
// because prompts carry pasted secrets far more often than people expect.
func (r *Redactor) Do(s string) string {
	for _, re := range r.pats {
		s = re.ReplaceAllString(s, "[redacted]")
	}
	return s
}
