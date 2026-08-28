package svc

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
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

This follows the same primitives as humanikd's Windows path, which was worked
out against real Windows 11 machines. What is NOT carried over is its
removeLegacyService: humanikd registered an SCM service in v0.1.9 and earlier
and has to clear it. daybook has routed Windows to a logon task since v0.1.0,
so there is no dead service to remove and pretending otherwise would be a
comment describing somebody else's history.
*/

// winTaskName is the registered task.
const winTaskName = Name

/*
winTaskXML renders the task definition.

WHY XML AND NOT FLAGS. `schtasks /create` in its flag form (/sc /tr /rl) can
express the trigger, the action and the run level — and NOTHING ELSE. Every other
setting silently takes Task Scheduler's schema default, and those defaults are
written for a desktop app you launch occasionally, not something that runs nightly:

	DisallowStartIfOnBatteries  true   -> won't start at all if you log in on battery
	StopIfGoingOnBatteries      true   -> killed the moment the charger comes out
	ExecutionTimeLimit          PT72H  -> killed after 3 days of normal operation
	RestartCount                0      -> none of the above ever recovers

daybook's whole promise is that a report appears every night without being asked.
On a laptop the flag form breaks that in four separate ways, silently, while
`schtasks /query` reports the task as registered and fine. None of those values
were ever chosen — they were inherited by using the flag form. The only way to
set them is /xml (or the COM API, which needs cgo).

NOTE PT0S. Omitting ExecutionTimeLimit does not mean "unlimited" — it means the
schema default of 72 hours. Unlimited has to be said explicitly.
*/
func winTaskXML(exe, args, user string) string {
	// The Task Scheduler schema is order-sensitive and the file must be UTF-16
	// with a BOM (written by winTaskInstall).
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>daybook - writes a daily report from your Claude Code sessions and git history. Runs as you, at every logon.</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + xmlEscape(user) + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + xmlEscape(user) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(exe) + `</Command>
      <Arguments>` + xmlEscape(args) + `</Arguments>
    </Exec>
  </Actions>
</Task>`
}

// xmlEscape is enough for the three fields interpolated — a Windows path, an
// argument string, and DOMAIN\user. Hand-rolled rather than encoding/xml because
// the document is a literal and only these values vary.
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// utf16BOM encodes s as little-endian UTF-16 with a byte-order mark. schtasks
// /xml rejects a UTF-8 file whose declaration says UTF-16, and writing the
// declaration as UTF-8 is not reliably accepted across Windows builds — so match
// what Task Scheduler's own export produces.
func utf16BOM(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2+len(units)*2)
	b = append(b, 0xFF, 0xFE) // LE BOM
	for _, u := range units {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// serveArgs is what the task runs. The config path is carried explicitly because
// a logon task inherits none of the shell environment that would otherwise point
// at a non-default config.
func serveArgs(cfg interface{ Path() string }) string {
	if p := cfg.Path(); p != "" {
		return `serve --config "` + p + `"`
	}
	return "serve"
}

func winTaskInstall(cfg interface{ Path() string }) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve this binary's path: %w", err)
	}

	// The XML names the principal explicitly, so an unknown user is a hard stop
	// rather than a task registered against the wrong identity.
	user := currentUser()
	if user == unknownUser {
		return fmt.Errorf("cannot determine the current Windows user (USERNAME is unset), " +
			"so the logon task has no identity to run as")
	}

	f, err := os.CreateTemp("", "daybook-task-*.xml")
	if err != nil {
		return fmt.Errorf("cannot write the task definition: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(utf16BOM(winTaskXML(exe, serveArgs(cfg), user))); err != nil {
		f.Close()
		return fmt.Errorf("cannot write the task definition: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cannot write the task definition: %w", err)
	}

	// Try the SUBFOLDER first. Registering at the Task Scheduler ROOT folder is
	// gated by local policy on some machines — non-elevated `schtasks /create`
	// returns a bare "ERROR: Access is denied." even though the task itself needs
	// no elevation to RUN (RunLevel LeastPrivilege, running as the user). A
	// subfolder usually has its own, laxer ACL, so this succeeds unelevated where
	// the root does not.
	var lastOut []byte
	var lastErr error
	for _, name := range taskNameCandidates() {
		out, err := exec.Command("schtasks",
			"/create",
			"/tn", name,
			"/xml", path,
			"/f", // replace rather than fail — install is idempotent
		).CombinedOutput()
		if err == nil {
			return nil
		}
		lastOut, lastErr = out, err
	}

	msg := firstLine(lastOut, lastErr)
	if isAccessDenied(lastOut) {
		// Interpreted. The raw message next to a README that never asks for admin
		// reads as a contradiction, and the remedy is one run — not a permanent
		// requirement.
		return fmt.Errorf(
			"registering the logon task needs ONE elevated run on this machine.\n"+
				"    This is a Task Scheduler policy on the task folder, not a daybook\n"+
				"    requirement: the task it creates still runs UNPRIVILEGED as %s.\n\n"+
				"    Open PowerShell as Administrator, once, and run:\n\n"+
				"        daybook service install\n\n"+
				"    After that everything else works unelevated. (%s)",
			user, msg)
	}
	return fmt.Errorf("schtasks /create failed: %s", msg)
}

// taskNameCandidates is where to try registering, best first.
//
// The subfolder avoids the root folder's ACL on locked-down machines; the root
// stays as the fallback so a machine that cannot create folders still installs,
// and so an upgrade from a build that registered at the root still finds it.
func taskNameCandidates() []string {
	return []string{`\` + winTaskName + `\` + winTaskName, winTaskName}
}

func isAccessDenied(out []byte) bool {
	return strings.Contains(strings.ToUpper(string(out)), "ACCESS IS DENIED")
}

// onEachTaskName runs verb against every candidate name and returns the first
// success. Every operation has to do this, not just install: a machine may carry
// the task at either location depending on which build registered it and whether
// the root folder was writable at the time.
func onEachTaskName(verb string, extra ...string) (out []byte, err error) {
	for _, name := range taskNameCandidates() {
		args := append([]string{verb, "/tn", name}, extra...)
		out, err = exec.Command("schtasks", args...).CombinedOutput()
		if err == nil {
			return out, nil
		}
		if !isTaskNotFound(out) {
			return out, err // a real failure — do not mask it by trying the other name
		}
	}
	return out, err
}

// winControl runs an install/uninstall/start/stop/restart action.
func winControl(cfg interface{ Path() string }, action string) error {
	switch action {
	case "install":
		return winTaskInstall(cfg)
	case "uninstall":
		out, err := onEachTaskName("/delete", "/f")
		if err != nil {
			if isTaskNotFound(out) {
				return nil // already gone — uninstall is idempotent
			}
			return fmt.Errorf("schtasks /delete failed: %s", firstLine(out, err))
		}
		return nil
	case "start":
		out, err := onEachTaskName("/run")
		if err != nil {
			return fmt.Errorf("schtasks /run failed: %s", firstLine(out, err))
		}
		return nil
	case "stop":
		// Ends the running instance. The task stays registered and starts again at
		// the next logon, matching what `service stop` means everywhere else.
		out, err := onEachTaskName("/end")
		if err != nil {
			return fmt.Errorf("schtasks /end failed: %s", firstLine(out, err))
		}
		return nil
	case "restart":
		_ = winControl(cfg, "stop")
		return winControl(cfg, "start")
	default:
		return errUnsupported(action)
	}
}

// winStatus reports registration and whether an instance is running.
//
// `Status: Running` is the field schtasks prints in its list output. Absent
// means registered but idle, which for a logon task is the normal state between
// logging out and back in.
func winStatus() (installed, running bool, err error) {
	out, qerr := onEachTaskName("/query", "/fo", "LIST")
	if qerr != nil {
		if isTaskNotFound(out) {
			return false, false, nil // not registered
		}
		// A query that failed for another reason is not evidence of absence.
		return false, false, fmt.Errorf("schtasks /query failed: %s", firstLine(out, qerr))
	}
	return true, strings.Contains(string(out), "Running"), nil
}

// isTaskNotFound distinguishes "no such task" from a real failure. schtasks says
// "ERROR: The system cannot find the file specified." for a missing task, which
// is indistinguishable from other errors by exit code alone.
func isTaskNotFound(out []byte) bool {
	s := strings.ToUpper(string(out))
	return strings.Contains(s, "CANNOT FIND THE FILE SPECIFIED") ||
		strings.Contains(s, "DOES NOT EXIST")
}

// firstLine keeps schtasks' own message — which is usually specific — rather than
// a bare exit status, falling back to the error when it printed nothing.
func firstLine(out []byte, err error) string {
	for _, l := range strings.Split(string(out), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	if err != nil {
		return err.Error()
	}
	return "no output"
}

// unknownUser is what currentUser reports when the environment does not name
// one. Readable in a status line; NOT usable as a task principal.
const unknownUser = "this user"

// currentUser is DOMAIN\user — the task's principal.
func currentUser() string {
	if u := os.Getenv("USERNAME"); u != "" {
		if d := os.Getenv("USERDOMAIN"); d != "" {
			return d + `\` + u
		}
		return u
	}
	return unknownUser
}
