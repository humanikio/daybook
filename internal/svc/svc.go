// Package svc runs daybook as a native OS service — launchd on macOS, systemd
// on Linux, a logon task on Windows — and drives install/uninstall/start/stop.
//
// ALWAYS AS THE USER, NEVER AS ROOT. This is not a preference, it is the only
// arrangement that works:
//
//   - narration spawns `claude`, whose credentials live in the macOS login
//     keychain and under the user's ~/.claude. A root service has a different
//     HOME and a different keychain, and fails auth on every run.
//   - the transcripts being read are under the user's home directory.
//   - the repositories being scanned are the user's, and git's own config —
//     including the author identity daybook filters on — is per-user.
//
// A system daemon would install cleanly, start cleanly, and produce an empty
// report forever. So there is no system-service path here at all.
package svc

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"

	"github.com/humanikio/daybook/internal/config"
)

// Name is the service identifier. Kept boring: it becomes a plist filename, a
// systemd unit name, and a scheduled-task name.
const Name = "daybook"

type program struct {
	run func() error
}

func (p *program) Start(service.Service) error {
	go func() { _ = p.run() }()
	return nil
}

func (p *program) Stop(service.Service) error { return nil }

// New builds the service handle. run is the loop `daybook serve` executes.
func New(cfg config.Config, run func() error) (service.Service, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"serve"}
	// Carry the config path through, or the service starts with defaults and
	// silently watches the wrong directories.
	if p := cfg.Path(); p != "" {
		args = append(args, "--config", p)
	}

	sc := &service.Config{
		Name:        Name,
		DisplayName: "daybook",
		Description: "Daily work reports from Claude Code transcripts and git history.",
		Executable:  exe,
		Arguments:   args,
		Option: service.KeyValue{
			"UserService": true,
			// Restart after a wake or a crash rather than going quiet. A
			// scheduler that stops silently is worse than no scheduler,
			// because it looks like nothing happened rather than like a
			// failure.
			"KeepAlive":    true,
			"RunAtLoad":    true,
			"LogDirectory": logDir(),
		},
	}
	return service.New(&program{run: run}, sc)
}

func logDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs")
	default:
		return filepath.Join(home, ".daybook")
	}
}

// Control runs an install/uninstall/start/stop/restart action.
func Control(cfg config.Config, action string) error {
	if runtime.GOOS == "windows" {
		return winControl(cfg, action)
	}
	s, err := New(cfg, func() error { return nil })
	if err != nil {
		return err
	}
	return service.Control(s, action)
}

// Status reports whether the service is registered and running.
//
// Registration is checked by looking for the file, because querying the other
// domain needs privileges and a check that prompts for a password is a check
// nobody runs.
func Status(cfg config.Config) (installed, running bool, err error) {
	if runtime.GOOS == "windows" {
		return winStatus()
	}
	s, err := New(cfg, func() error { return nil })
	if err != nil {
		return false, false, err
	}
	st, err := s.Status()
	switch {
	case err == service.ErrNotInstalled:
		return false, false, nil
	case err != nil:
		return unitFileExists(), false, nil
	}
	return true, st == service.StatusRunning, nil
}

func unitFileExists() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	var p string
	switch runtime.GOOS {
	case "darwin":
		p = filepath.Join(home, "Library", "LaunchAgents", Name+".plist")
	case "linux":
		p = filepath.Join(home, ".config", "systemd", "user", Name+".service")
	default:
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Conflicts returns service registrations daybook did not create.
//
// Normally empty. Non-empty means a SYSTEM-level registration exists — usually
// from someone running `sudo daybook service install` — and that one can never
// work, for all the reasons at the top of this file. Worth naming explicitly,
// because the symptom is an empty report rather than an error.
func Conflicts() []string {
	var out []string
	switch runtime.GOOS {
	case "darwin":
		for _, p := range []string{
			"/Library/LaunchDaemons/" + Name + ".plist",
			"/Library/LaunchAgents/" + Name + ".plist",
		} {
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
	case "linux":
		for _, p := range []string{
			"/etc/systemd/system/" + Name + ".service",
			"/lib/systemd/system/" + Name + ".service",
		} {
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// AutoStartKind names the mechanism in the platform's own vocabulary, so the
// wizard says something a user can search for.
func AutoStartKind() string {
	switch runtime.GOOS {
	case "darwin":
		return "LaunchAgent"
	case "windows":
		return "logon task"
	default:
		return "systemd user service"
	}
}

// PostInstallNote is the platform caveat worth saying out loud, or "".
func PostInstallNote() string {
	switch runtime.GOOS {
	case "linux":
		// A systemd --user service stops at logout and does not start at boot
		// unless the account has lingering enabled. On a headless box the
		// "automatic" is a lie without this.
		return "headless box? survive logout and reboot:  sudo loginctl enable-linger $USER"
	case "windows":
		return "a logon task runs only while you are logged in — which is required anyway, since it needs your credentials"
	default:
		return ""
	}
}

func errUnsupported(action string) error {
	return fmt.Errorf("service %s is not supported on %s", action, runtime.GOOS)
}
