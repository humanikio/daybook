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
	"strconv"
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
		Output:   Output{Root: "~/Desktop/daybook", NoRemote: "committed"},
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
		if _, err := ParseDuration(c.Narrate.Timeout); err != nil {
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

// ParseDuration is time.ParseDuration plus days and weeks.
//
// Go's parser stops at hours, so "7d" — the obvious way to write a week, and
// the first thing anyone types into a field labelled "how far back each run
// looks" — is rejected with `unknown unit "d"`. Every duration in this config
// is measured in days or hours by the people setting it, so the units they
// reach for should work.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if v, err := strconv.ParseFloat(n, 64); err == nil {
			return time.Duration(v * float64(24*time.Hour)), nil
		}
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		if v, err := strconv.ParseFloat(n, 64); err == nil {
			return time.Duration(v * float64(7*24*time.Hour)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("want something like 24h, 7d or 2w — got %q", s)
	}
	return d, nil
}

func (c Config) WindowLength() (time.Duration, error) {
	if c.Window.Length == "" {
		return 24 * time.Hour, nil
	}
	return ParseDuration(c.Window.Length)
}

func (c Config) StaleAfter() (time.Duration, error) {
	if c.Window.StaleAfter == "" {
		return 120 * time.Hour, nil
	}
	return ParseDuration(c.Window.StaleAfter)
}

func (c Config) NarrateTimeout() time.Duration {
	if d, err := ParseDuration(c.Narrate.Timeout); err == nil && d > 0 {
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

// DetectAuthors reads git's idea of who you are — GLOBAL first, then whatever
// the current directory resolves to if that differs.
//
// The global config is asked for explicitly because a bare `git config --get
// user.email` answers for the CURRENT DIRECTORY, and a repo with a local
// identity override answers with that. Running `daybook init` from inside such
// a repo therefore set the author filter to an address used by exactly one
// repository — and daybook counted 12 commits across 1 repo on a day that
// really had 30 across 6. It reported a number, so nothing looked wrong.
//
// Both are returned when they differ, because someone with a per-repo identity
// genuinely authors under both and should have both counted.
func DetectAuthors() []string {
	seen := map[string]bool{}
	var out []string
	add := func(args ...string) {
		b, err := exec.Command("git", args...).Output()
		if err != nil {
			return
		}
		if e := strings.TrimSpace(string(b)); e != "" && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	add("config", "--global", "--get", "user.email")
	add("config", "--get", "user.email") // current directory; may be a local override
	return out
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

// ---- writing the config back out -------------------------------------------

// Render writes the config file, comments and all.
//
// Generated rather than templated from a struct so the file a person opens
// explains itself — the reason a value is what it is matters more than the
// value, and a marshalled struct would throw all of that away.
func Render(cfg Config) []byte {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }

	w("# daybook config. Precedence: env vars > this file > built-in defaults.")
	w("#")
	w("# Every string is quoted on purpose. Under YAML 1.1 an unquoted 12:00 is")
	w("# the integer 43200, NO is false, and 010 is 8.")
	w("")
	w("watch:")
	w("  agents:")
	for _, a := range cfg.Watch.Agents {
		w("    - { source: %q, path: %q }", a.Source, a.Path)
	}
	w("  repos:")
	for _, r := range cfg.Watch.Repos {
		w("    - { path: %q, depth: %d }", r.Path, r.Depth)
	}
	w("  # `git push` updates the local tracking ref, so the unpushed count is")
	w("  # already right for anything sent from this machine. Turn this on only")
	w("  # to notice pushes made somewhere else; it costs a round trip per repo.")
	w("  fetch: %v", cfg.Watch.Fetch)
	w("  ignore: %s", yamlList(cfg.Watch.Ignore))
	w("")
	w("window:")
	w("  length: %q", cfg.Window.Length)
	w("  # window  — report only messages inside the window (a week-old session")
	w("  #           contributes today's work, not its whole history)")
	w("  # session — report the entire session, every day it is active")
	w("  scope: %q", cfg.Window.Scope)
	w("  stale_after: %q", cfg.Window.StaleAfter)
	w("")
	w("schedule:")
	w("  at: %q", cfg.Schedule.At)
	w("  days: %-14s # empty = every day", yamlList(cfg.Schedule.Days))
	w("  catch_up: %v         # asleep at `at`? run on wake rather than skip the day", cfg.Schedule.CatchUp)
	w("")
	w("identity:")
	if len(cfg.Identity.Authors) > 0 {
		// The whole list. This wrote Authors[0] and dropped the rest, which on
		// a machine with a per-repo identity silently excluded every commit
		// made under the second one.
		w("  authors: %s", yamlList(cfg.Identity.Authors))
	} else {
		w("  authors: []        # empty = detect from git config user.email")
	}
	w("  machine: %q          # empty = hostname; namespaces output files", cfg.Identity.Machine)
	w("")
	w("output:")
	w("  # Where the reports land. Somewhere you will actually open — a report")
	w("  # you never see is not a report. Note it holds prompt text, so a")
	w("  # visible folder is also a visible folder during a screen share.")
	w("  root: %q", cfg.Output.Root)
	w("  # The bar for repos with no remote, where \"shipped\" is undefined.")
	w("  no_remote: %s      # committed | exclude", cfg.Output.NoRemote)
	w("")
	w("narrate:")
	w("  enabled: %-10v # uses the claude you are already signed in with", cfg.Narrate.Enabled)
	w("  provider: %q        # auto | cli | api | off", providerOr(cfg.Narrate.Provider))
	w("  binary: %q", cfg.Narrate.Binary)
	w("  model: %q           # empty = the API provider's default", cfg.Narrate.Model)
	w("  effort: %q          # low | medium | high | xhigh | max (api only)", cfg.Narrate.Effort)
	w("  timeout: %q", cfg.Narrate.Timeout)
	w("")
	w("privacy:")
	w("  # Redaction runs before anything reaches disk. Prompts carry pasted")
	w("  # secrets far more often than people expect.")
	w("  keep_raw_prompts: %v", cfg.Privacy.KeepRawPrompts)
	w("  redact:")
	for _, r := range cfg.Privacy.Redact {
		w("    - { name: %q, pattern: %q }", r.Name, r.Pattern)
	}
	w("")
	w("# Group repos into businesses for the shipped-to table. Optional.")
	w("# business:")
	w("#   - { name: \"Acme\", repos: [\"acme-*\", \"acme\"] }")
	return []byte(b.String())
}

// yamlList renders a string slice as inline YAML, quoted.
//
// Values are quoted for the same reason every other string here is: an
// unquoted `no` in a days list becomes the boolean false under YAML 1.1, and a
// weekday list is exactly where someone would write one.
func yamlList(xs []string) string {
	if len(xs) == 0 {
		return "[]"
	}
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = fmt.Sprintf("%q", x)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func providerOr(p string) string {
	if p == "" {
		return "auto"
	}
	return p
}

// Save writes the config to its path, creating the directory if needed.
//
// It REGENERATES the file from the struct rather than editing it in place, so
// the comments come back but any comment you added by hand does not. That is a
// deliberate trade: surgical yaml.Node editing preserves everything but is a
// large amount of machinery for a file whose comments are ours, and a
// half-preserved file is more confusing than a cleanly regenerated one.
func Save(cfg Config) error {
	p := cfg.path
	if p == "" {
		p = File()
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, Render(cfg), 0o600)
}
