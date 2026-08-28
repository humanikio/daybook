package svc

import (
	"strings"
	"testing"
	"unicode/utf16"
)

type fakeCfg struct{ path string }

func (f fakeCfg) Path() string { return f.path }

// The reason this file uses /xml instead of schtasks' flag form. Each of these
// takes a schema default written for a desktop app you launch occasionally, and
// each one breaks a nightly run on a laptop while `schtasks /query` still reports
// the task as registered and fine.
func TestTaskXMLOverridesTheDefaultsThatBreakALaptop(t *testing.T) {
	xml := winTaskXML(`C:\Users\a\daybook.exe`, "serve", `CORP\a`)

	for setting, want := range map[string]string{
		// true by default: would not start at all if you log in on battery
		"DisallowStartIfOnBatteries": "false",
		// true by default: killed the moment the charger comes out
		"StopIfGoingOnBatteries": "false",
		// PT72H by default: killed after three days of normal operation.
		// Omitting it does NOT mean unlimited — PT0S has to be said.
		"ExecutionTimeLimit": "PT0S",
		// missed logon should still run once the machine is available
		"StartWhenAvailable": "true",
	} {
		if !strings.Contains(xml, "<"+setting+">"+want+"</"+setting+">") {
			t.Errorf("%s is not set to %q — the schema default breaks a nightly run", setting, want)
		}
	}

	// RestartOnFailure is what makes the others recoverable; the default count is 0.
	if !strings.Contains(xml, "<RestartOnFailure>") || !strings.Contains(xml, "<Count>3</Count>") {
		t.Error("no RestartOnFailure — nothing recovers from a crash")
	}
}

// The task must run as the user and unprivileged. A task that silently runs
// elevated is a privilege nobody granted.
func TestTaskRunsAsTheUserWithoutElevation(t *testing.T) {
	xml := winTaskXML(`C:\daybook.exe`, "serve", `CORP\alice`)
	for _, want := range []string{
		"<LogonType>InteractiveToken</LogonType>",
		"<RunLevel>LeastPrivilege</RunLevel>",
		`<UserId>CORP\alice</UserId>`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("the task definition never says %s", want)
		}
	}
}

// A Windows path or username containing XML metacharacters must not break the
// document. `&` in a path is unusual and legal.
func TestTaskXMLEscapesInterpolatedValues(t *testing.T) {
	xml := winTaskXML(`C:\R&D\daybook.exe`, `serve --config "C:\a b\c.yaml"`, `CORP\a<b`)
	if strings.Contains(xml, `C:\R&D\`) {
		t.Error("an unescaped & reached the document")
	}
	if !strings.Contains(xml, "C:\\R&amp;D\\daybook.exe") {
		t.Error("& was not escaped in the command path")
	}
	if !strings.Contains(xml, "&quot;") {
		t.Error("quotes in the arguments were not escaped")
	}
	if strings.Contains(xml, `a<b</UserId>`) {
		t.Error("an unescaped < reached the principal")
	}
}

// schtasks /xml rejects a UTF-8 file whose declaration says UTF-16, so the bytes
// have to be little-endian UTF-16 with a BOM — what Task Scheduler's own export
// produces.
func TestTaskFileIsUTF16WithABOM(t *testing.T) {
	b := utf16BOM("<Task/>")
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("no little-endian BOM: % x", b[:min(4, len(b))])
	}
	var units []uint16
	for i := 2; i+1 < len(b); i += 2 {
		units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != "<Task/>" {
		t.Errorf("decoded to %q", got)
	}
}

// The subfolder is tried first because the ROOT folder's ACL blocks a
// non-elevated create on some machines. The root stays as a fallback so a
// machine that cannot create folders still installs, and so an upgrade from a
// build that registered at the root still finds its task.
func TestSubfolderIsTriedBeforeTheRoot(t *testing.T) {
	names := taskNameCandidates()
	if len(names) != 2 {
		t.Fatalf("want two candidates, got %v", names)
	}
	if !strings.HasPrefix(names[0], `\`) || !strings.Contains(names[0][1:], `\`) {
		t.Errorf("first candidate %q is not a subfolder path", names[0])
	}
	if names[1] != winTaskName {
		t.Errorf("second candidate %q is not the bare root name", names[1])
	}
}

// schtasks reports a missing task with a message that is indistinguishable from
// other failures by exit code alone. Getting this wrong makes uninstall fail on
// an already-clean machine and makes status report "not installed" for a real
// error.
func TestRecognisesSchtasksMessages(t *testing.T) {
	notFound := []string{
		"ERROR: The system cannot find the file specified.",
		"ERROR: The specified task name \"\\daybook\\daybook\" does not exist in the system.",
	}
	for _, s := range notFound {
		if !isTaskNotFound([]byte(s)) {
			t.Errorf("not recognised as a missing task: %q", s)
		}
	}
	if isTaskNotFound([]byte("ERROR: Access is denied.")) {
		t.Error("access denied was read as a missing task — that would hide a real failure")
	}
	if !isAccessDenied([]byte("ERROR: Access is denied.")) {
		t.Error("access denied was not recognised")
	}
}

// The config path has to travel with the task. A logon task inherits none of the
// shell environment that would otherwise point at a non-default config.
func TestServeArgsCarryTheConfigPath(t *testing.T) {
	if got := serveArgs(fakeCfg{}); got != "serve" {
		t.Errorf("default config should add no flag, got %q", got)
	}
	got := serveArgs(fakeCfg{path: `C:\Users\a\.daybook\config.yaml`})
	if !strings.Contains(got, "--config") || !strings.Contains(got, `C:\Users\a\.daybook\config.yaml`) {
		t.Errorf("config path did not reach the task arguments: %q", got)
	}
}

// schtasks' own message is usually specific. A bare exit status is not.
func TestFirstLinePrefersTheToolsMessage(t *testing.T) {
	if got := firstLine([]byte("\n\nERROR: Access is denied.\nmore\n"), nil); got != "ERROR: Access is denied." {
		t.Errorf("got %q", got)
	}
	if got := firstLine(nil, nil); got != "no output" {
		t.Errorf("empty output with no error should not panic or lie, got %q", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
