package svc

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

/*
Windows auto-start, as a per-user LOGON TASK rather than a service.

WHY NOT A SERVICE. Windows SCM services run as LocalSystem. daybook needs the
enrolling user's HOME (transcripts live under it), their git config (the author
identity it filters on), and their Claude Code credentials (narration). None of
those are reachable from LocalSystem, so the service would install, start, and
produce an empty report forever — the worst possible failure, because it looks
like success.

WHY NOT kardianos's UserService OPTION. It is honoured for launchd and systemd
user units only; on Windows it is ignored and you get LocalSystem regardless. So
the flag alone would change nothing while reading as a fix.

A logon task is the direct analogue of the macOS LaunchAgent used for the same
reason: it runs AS THE USER, needs no stored password, and starts at every
logon. The trade is that it only runs while that user is logged in, which is
fine — it needs their credentials either way.

schtasks rather than the Task Scheduler COM API: no cgo, no extra dependency,
and the command stays inspectable by the person whose machine it is.
*/

func winControl(cfg interface{ Path() string }, action string) error {
	switch action {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := fmt.Sprintf(`"%s" serve`, exe)
		if p := cfg.Path(); p != "" {
			cmd = fmt.Sprintf(`"%s" serve --config "%s"`, exe, p)
		}
		out, err := exec.Command("schtasks", "/Create", "/F",
			"/TN", Name, "/SC", "ONLOGON", "/RL", "LIMITED", "/TR", cmd).CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks create failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case "uninstall":
		out, err := exec.Command("schtasks", "/Delete", "/F", "/TN", Name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks delete failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case "start":
		out, err := exec.Command("schtasks", "/Run", "/TN", Name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks run failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case "stop":
		out, err := exec.Command("schtasks", "/End", "/TN", Name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("schtasks end failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	case "restart":
		_ = winControl(cfg, "stop")
		return winControl(cfg, "start")
	default:
		return errUnsupported(action)
	}
}

func winStatus() (installed, running bool, err error) {
	out, qerr := exec.Command("schtasks", "/Query", "/TN", Name, "/FO", "LIST").CombinedOutput()
	if qerr != nil {
		return false, false, nil // not registered
	}
	s := string(out)
	return true, strings.Contains(s, "Running"), nil
}
