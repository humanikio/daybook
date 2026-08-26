package model

import "testing"

// The shapes below are the error results a real 45-hour run produced. That run
// reported "What broke (45)" when the work itself had broken once.
func TestClassifyDoesNotCallFrictionBreakage(t *testing.T) {
	cases := []struct {
		want FailKind
		text string
	}{
		{FailRefused, "The user doesn't want to proceed with this tool use. The tool use was rejected"},
		{FailRefused, "Permission for this action was denied by the Claude Code auto mode classifier."},
		{FailRefused, "<tool_use_error>Blocked: sleep 45 followed by: echo status</tool_use_error>"},
		{FailUnavailable, "claude-sonnet-5[1m] is temporarily unavailable (timed out), so auto mode cannot"},
		{FailUnavailable, `failed: unknown tool "mcp__claude-in-chrome__read_console_messages"`},
		{FailUnavailable, `The "browser_batch" tool did not respond in time.`},
		{FailCommand, "Exit code 1 (eval):cd:1: no such file or directory: api/docs/site"},
		{FailCommand, "Exit code 1 cat: src/app/global.css: No such file or directory"},
		{FailCommand, "<tool_use_error>String to replace not found in file. String: const SEP = ' ';"},
		{FailCommand, "<tool_use_error>InputValidationError: Monitor failed due to the following issues"},
		{FailBroke, "> api@1.0.0 type-check > tsc --noEmit"},
		{FailBroke, "--- FAIL: TestCaptureFilesTheImageItself (0.00s)"},
	}
	for _, c := range cases {
		if got := Classify(c.text); got != c.want {
			t.Errorf("classified as %q, want %q:\n  %s", got, c.want, c.text)
		}
	}
}

// An unrecognised result must not be promoted to breakage. Reporting the
// product as broken on evidence this thin is the defect being fixed.
func TestUnknownIsNotBreakage(t *testing.T) {
	if got := Classify("something nobody has seen before"); got == FailBroke {
		t.Fatal("an unrecognised error result was reported as the work breaking")
	}
}

// Raw files written before failures carried a kind must still load.
func TestFailureAcceptsTheOlderBareString(t *testing.T) {
	var f Failure
	if err := f.UnmarshalJSON([]byte(`"Exit code 1 cat: nope: No such file or directory"`)); err != nil {
		t.Fatal(err)
	}
	if f.Text == "" || f.Kind != FailCommand {
		t.Fatalf("got %+v", f)
	}
}
