package config

import (
	"os"
	"strings"
	"testing"
)

// Every built-in pattern, against something it must remove and something
// shaped like it that it must leave alone. A redaction that over-reaches
// destroys the report it is protecting; one that under-reaches is worse.
//
// Key-shaped samples are assembled at run time. Written out whole they are
// what secret scanners look for, and GitHub refuses a push containing one —
// fake or not.
func TestBuiltinRedaction(t *testing.T) {
	r := Default().Redactor()
	fake := func(prefix string) string { return prefix + "abcdefghijklmnopqrstuvwx0123" }
	cases := []struct{ in, want string }{
		{"AKIA" + "ABCDEFGHIJKLMNOP", "[redacted]"},
		{"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "aws_secret_access_key = [redacted]"},
		{fake("sk-" + "ant-api03-"), "[redacted]"},
		{fake("sk-" + "proj-"), "[redacted]"},
		{fake("sk_" + "live_"), "[redacted]"},
		{fake("rk_" + "test_"), "[redacted]"},
		{fake("wh" + "sec_"), "[redacted]"},
		{fake("gh" + "p_"), "[redacted]"},
		{fake("github" + "_pat_11"), "[redacted]"},
		{fake("xo" + "xb-1234567890-"), "[redacted]"},
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abc123signature", "[redacted]"},
		{"postgres://admin:hunter2@db.example.com/prod", "postgres://admin:[redacted]@db.example.com/prod"},
		{"Authorization: Bearer abcdef1234567890abcdef", "Authorization: Bearer [redacted]"},
		{"DATABASE_PASSWORD=correcthorsebattery", "DATABASE_PASSWORD=[redacted]"},
		{`OPENROUTER_API_KEY: "abcd1234efgh5678"`, `OPENROUTER_API_KEY: "[redacted]"`},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEAxyz\n-----END RSA PRIVATE KEY-----\nafter", "[redacted]\nafter"},
		// Clipped mid-key: the key body still must not survive.
		{"see -----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAA", "see [redacted]"},

		// Look-alikes that are not secrets.
		{"MAX_TOKENS=4096", "MAX_TOKENS=4096"},
		{"tokens = tokenizer.encode(text)", "tokens = tokenizer.encode(text)"},
		{"https://github.com/humanikio/daybook", "https://github.com/humanikio/daybook"},
		{"desk-chair-standing-up-for-a-while", "desk-chair-standing-up-for-a-while"},
		{"the bearer of bad news", "the bearer of bad news"},
		{"pk_live is the publishable prefix", "pk_live is the publishable prefix"},
	}
	for _, c := range cases {
		if got := r.Do(c.in); got != c.want {
			t.Errorf("\n  in   %q\n  got  %q\n  want %q", c.in, got, c.want)
		}
	}
}

// The reason redaction moved out of the config file: a list written there by an
// older init must not keep shadowing the built-ins.
func TestLegacyPatternsAreDropped(t *testing.T) {
	tmp := t.TempDir() + "/config.yaml"
	body := `privacy:
  keep_raw_prompts: true
  redact:
    - { name: "aws-access-key", pattern: "AKIA[0-9A-Z]{16}" }
    - { name: "bearer-token", pattern: "sk-[A-Za-z0-9_-]{20,}" }
    - { name: "github-token", pattern: "gh[pousr]_[A-Za-z0-9]{20,}" }
    - { name: "private-key", pattern: "-----BEGIN [A-Z ]*PRIVATE KEY-----" }
    - { name: "mine", pattern: "ACME-[0-9]{6}" }
`
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Privacy.Redact) != 1 || c.Privacy.Redact[0].Name != "mine" {
		t.Fatalf("want only the user's own pattern, got %v", c.Privacy.Redact)
	}
	r := c.Redactor()
	if got := r.Do("ACME-123456"); got != "[redacted]" {
		t.Errorf("user pattern not applied: %q", got)
	}
	key := "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----"
	if got := r.Do(key); strings.Contains(got, "MIIE") {
		t.Errorf("key body survived: %q", got)
	}
}

func TestBuiltinPatternsCompile(t *testing.T) {
	c := Default()
	c.Privacy.Redact = Builtin
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if n := len(Default().Redactor().pats); n != len(Builtin) {
		t.Errorf("%d of %d built-in patterns compiled", n, len(Builtin))
	}
}
