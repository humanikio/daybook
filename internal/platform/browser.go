// Package platform detects the parts of this machine daybook depends on but
// does not control.
//
// Right now that is Claude Code's browser integration. daybook does not drive a
// browser yet — this exists so `daybook verify` can tell you whether it WOULD
// work, because every one of these prerequisites fails silently. The integration
// is invisible by construction: absent from `claude mcp list`, absent from any
// config file unless somebody already knew to add it, and switched off entirely
// by an environment variable nobody associates with browsers.
package platform

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// The Claude extension, and the native-messaging manifest `claude --chrome`
	// writes. The manifest is matched on CONTENT as well as name: a file with
	// the right name and a foreign allowed_origins is not this integration, and
	// reporting it as one sends somebody debugging a working install.
	chromeExtensionID      = "fcoeoabgfenejglbffodgkkbkcdhcgfn"
	nativeHostManifestName = "com.anthropic.claude_code_browser_extension.json"

	// ChromeStoreURL is where the extension comes from.
	ChromeStoreURL = "https://chromewebstore.google.com/detail/claude/" + chromeExtensionID

	// BrowserToolPrefix is the allowlist entry that permits the browser tools.
	//
	// NOTE THE HYPHENS. Connector display names get normalised to underscores
	// (`claude.ai Gmail` → `mcp__claude_ai_Gmail`), but this built-in server's
	// tools keep theirs — `mcp__claude-in-chrome__computer`. The normalised
	// spelling matches nothing, and refuses every call while reading as
	// correctly configured.
	BrowserToolPrefix = "mcp__claude-in-chrome"

	// apiKeyVar switches the browser integration off. See APIKeySites.
	apiKeyVar = "ANTHROPIC_API_KEY"
)

// APIKeySites records WHERE ANTHROPIC_API_KEY was found, kept separate by
// location because the remedy differs and naming the wrong place is useless
// advice.
//
// Claude Code disables the browser for API-key auth — silently, even with the
// flag set. So a key inherited from a service definition turns the browser off
// for every job that service runs, and "unset ANTHROPIC_API_KEY" is meaningless
// to someone whose shell has never had it set.
type APIKeySites struct {
	Process bool   // this process's environment
	Session bool   // the launchd session, on macOS — inherited by everything started since
	Service string // a service definition that injects it, by path
}

func (a APIKeySites) Any() bool { return a.Process || a.Session || a.Service != "" }

// BrowserState is what could be observed about the browser integration.
type BrowserState struct {
	// ManifestPath is the native-messaging manifest, or "".
	ManifestPath string
	// Paired is Claude Code's own record of a completed extension pairing.
	Paired bool
	// Running is whether a Chromium browser is up FOR THIS USER, ON THIS
	// MACHINE. See BrowserRunning for why that qualifier carries the caveat.
	Running bool
	// Checkable is false where the manifest cannot be observed — Windows keeps
	// it in the registry, so a path check would report "not set up" on every
	// correctly configured machine. Unknown is the honest answer.
	Checkable bool
	APIKey    APIKeySites
}

// Ready reports whether every observable prerequisite is met.
//
// On a platform where the manifest cannot be seen this can only ever be a
// partial answer, which is what Checkable is for.
func (b BrowserState) Ready() bool {
	if b.APIKey.Any() {
		return false
	}
	if !b.Paired || !b.Running {
		return false
	}
	return !b.Checkable || b.ManifestPath != ""
}

// DetectBrowser inspects the machine. It never changes anything.
func DetectBrowser() BrowserState {
	return BrowserState{
		ManifestPath: nativeHostManifest(),
		Paired:       extensionPaired(),
		Running:      browserRunning(),
		Checkable:    len(nativeHostDirs()) > 0,
		APIKey:       apiKeySites(),
	}
}

func apiKeySites() APIKeySites {
	return APIKeySites{
		Process: os.Getenv(apiKeyVar) != "",
		Session: launchdSessionHasAPIKey(),
		Service: serviceDefInjectingAPIKey(),
	}
}

func launchdSessionHasAPIKey() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	out, err := exec.Command("launchctl", "getenv", apiKeyVar).Output()
	if err != nil {
		return false
	}
	// The OUTPUT decides, not the exit code. Measured on macOS 15: `launchctl
	// getenv` for an unset variable exits 0 with an empty line, so an
	// exit-code check reports every machine as having the key set — and this
	// check's whole job is to explain why the browser is off.
	return strings.TrimSpace(string(out)) != ""
}

// serviceDefInjectingAPIKey returns the service file that names the variable.
//
// A text search rather than a plist parse: this only needs a yes/no, and
// parsing adds a dependency and a second way to be wrong.
func serviceDefInjectingAPIKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{filepath.Join(home, "Library", "LaunchAgents", "daybook.plist")}
	case "linux":
		paths = []string{filepath.Join(home, ".config", "systemd", "user", "daybook.service")}
	default:
		return ""
	}
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), apiKeyVar) {
			return p
		}
	}
	return ""
}

func nativeHostManifest() string {
	for _, dir := range nativeHostDirs() {
		p := filepath.Join(dir, nativeHostManifestName)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), chromeExtensionID) {
			return p
		}
	}
	return ""
}

// nativeHostDirs are the per-browser NativeMessagingHosts directories.
//
// Windows is deliberately absent rather than forgotten: there is no file there,
// the manifest is a registry key, and a path check would report "not set up" on
// every correctly configured Windows machine.
func nativeHostDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		app := filepath.Join(home, "Library", "Application Support")
		return []string{
			filepath.Join(app, "Google", "Chrome", "NativeMessagingHosts"),
			filepath.Join(app, "Microsoft Edge", "NativeMessagingHosts"),
			filepath.Join(app, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
			filepath.Join(app, "Chromium", "NativeMessagingHosts"),
		}
	case "linux":
		cfg := filepath.Join(home, ".config")
		return []string{
			filepath.Join(cfg, "google-chrome", "NativeMessagingHosts"),
			filepath.Join(cfg, "microsoft-edge", "NativeMessagingHosts"),
			filepath.Join(cfg, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
			filepath.Join(cfg, "chromium", "NativeMessagingHosts"),
		}
	}
	return nil
}

// extensionPaired reads Claude Code's own record of a completed pairing.
func extensionPaired() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return false
	}
	var state struct {
		ChromeExtension struct {
			PairedDeviceID string `json:"pairedDeviceId"`
		} `json:"chromeExtension"`
	}
	if json.Unmarshal(b, &state) != nil {
		return false
	}
	return state.ChromeExtension.PairedDeviceID != ""
}

// browserRunning reports whether a Chromium browser is up for this user, on this
// machine.
//
// That qualifier is the whole caveat. Pairing is per Claude Code ACCOUNT, not
// per host — a browser on a different machine, even a different OS, can be
// reachable by an agent running here. Nothing outside an agent session can
// enumerate those, because listing connected browsers is itself an MCP tool
// inside that session.
//
// So false does not mean "no browser is reachable". It means "none is reachable
// in the only way this process can observe", which is why callers report it as
// a local answer rather than a verdict.
func browserRunning() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	for _, name := range []string{"Google Chrome", "chrome", "chromium", "Microsoft Edge", "msedge", "Brave Browser", "brave"} {
		if err := exec.Command("pgrep", "-u", currentUID(), "-x", name).Run(); err == nil {
			return true
		}
	}
	return false
}

func currentUID() string {
	if u := os.Getenv("UID"); u != "" {
		return u
	}
	out, err := exec.Command("id", "-u").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// BrowserSetupSteps lists what this machine still owes, in the order it has to
// happen. Empty when everything observable is in place.
func BrowserSetupSteps(b BrowserState) []string {
	var steps []string

	// NAME THE PLACE. "unset ANTHROPIC_API_KEY" is useless to someone whose
	// shell has never had it set — the value is in a plist or a launchd session,
	// and that advice sends them looking in the one place it is not.
	switch {
	case b.APIKey.Service != "":
		steps = append(steps, "remove "+apiKeyVar+" from "+b.APIKey.Service+
			" — Claude Code turns the browser off for API-key auth, silently, even with the flag set. "+
			"Reinstall the service afterwards, and sign in with `claude` → /login instead.")
	case b.APIKey.Session:
		steps = append(steps, "run `launchctl unsetenv "+apiKeyVar+
			"` — it is set in your launchd session, so everything started since inherits it, and "+
			"Claude Code turns the browser off for API-key auth, silently.")
	case b.APIKey.Process:
		steps = append(steps, "unset "+apiKeyVar+
			" — Claude Code turns the browser off for API-key auth, silently, even with the flag set.")
	}
	if !b.Paired {
		steps = append(steps, "install the Claude extension and sign in:  "+ChromeStoreURL)
	}
	if b.Checkable && b.ManifestPath == "" {
		steps = append(steps, "run `claude --chrome` once, in a terminal, as yourself — this writes the "+
			"native-messaging handshake. Then QUIT AND REOPEN the browser: it reads that file only at "+
			"startup, which is why a fresh setup still reports the extension as missing.")
	}
	if !b.Running && runtime.GOOS != "windows" {
		steps = append(steps, "leave a Chromium browser running — the integration drives a live browser, "+
			"not a headless one. (A browser paired to your Claude Code account on another machine may also "+
			"serve; this check cannot see one, so treat this as unmet rather than failed.)")
	}
	return steps
}
