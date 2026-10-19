package config

import (
	"regexp"
	"strings"
)

// Builtin is the redaction every install runs, ahead of anything in
// privacy.redact.
//
// Built in rather than written into the config, because a default written into
// a file stops being a default: `init` used to copy these four lines into
// config.yaml, the file's list then replaced the built-in one, and no later
// release could improve them for anybody already installed. The private-key
// pattern shipped matching only the BEGIN line, leaving the key itself in the
// report, and the fix would have reached nobody.
//
// A pattern with a group named "secret" replaces only that group, so the
// report keeps `postgres://admin:[redacted]@db` — what was connected to, and
// not how.
var Builtin = []Redaction{
	// The whole block, through END. A clipped prompt can end mid-key, so a
	// block with no END is redacted to the end of the text.
	{Name: "private-key", Pattern: `(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`},
	{Name: "aws-access-key", Pattern: `\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`},
	{Name: "aws-secret-key", Pattern: `(?i)\baws_secret_access_key\b["']?\s*[:=]\s*["']?(?P<secret>[A-Za-z0-9/+=]{40})`},
	// sk- covers Anthropic (sk-ant-) and OpenAI (sk-proj-, sk-).
	{Name: "api-key", Pattern: `\bsk-[A-Za-z0-9_-]{20,}`},
	{Name: "stripe-key", Pattern: `\b(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{16,}`},
	{Name: "stripe-webhook", Pattern: `\bwhsec_[A-Za-z0-9]{20,}`},
	{Name: "github-token", Pattern: `\bgh[pousr]_[A-Za-z0-9]{20,}`},
	{Name: "github-pat", Pattern: `\bgithub_pat_[A-Za-z0-9_]{20,}`},
	{Name: "slack-token", Pattern: `\bxox[abposr]-[A-Za-z0-9-]{10,}`},
	{Name: "jwt", Pattern: `\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`},
	{Name: "url-password", Pattern: `\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:(?P<secret>[^\s@/]+)@`},
	{Name: "bearer", Pattern: `(?i)\bbearer\s+(?P<secret>[A-Za-z0-9._~+/=-]{16,})`},
	// An environment assignment whose NAME says it is a secret. Upper case
	// only, which is how environment variables are written; matching the words
	// in any case catches `tokens = tokenizer.encode(text)` in every code
	// excerpt the assistant writes. Eight characters, so `MAX_TOKENS=4096` is
	// left alone.
	{Name: "env-secret", Pattern: `\b[A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|PASSWD|API_KEY|APIKEY|PRIVATE_KEY)[A-Z0-9_]*["']?\s*[:=]\s*["']?(?P<secret>[^\s"'` + "`" + `]{8,})`},
}

// legacy is what `init` used to write into config.yaml. Dropped on load:
// Builtin supersedes every one of them, and keeping them would leave the old
// private-key pattern stripping the BEGIN line out from under the new one.
var legacy = map[string]bool{
	`AKIA[0-9A-Z]{16}`:                   true,
	`sk-[A-Za-z0-9_-]{20,}`:              true,
	`gh[pousr]_[A-Za-z0-9]{20,}`:         true,
	`-----BEGIN [A-Z ]*PRIVATE KEY-----`: true,
}

func withoutLegacy(rs []Redaction) []Redaction {
	var out []Redaction
	for _, r := range rs {
		if !legacy[strings.TrimSpace(r.Pattern)] {
			out = append(out, r)
		}
	}
	return out
}

// Redactor compiles the patterns once.
type Redactor struct{ pats []*regexp.Regexp }

// Redactor is Builtin, then the config's own patterns. Built-ins first, so a
// user pattern can never consume half of something a built-in would have
// removed whole.
func (c Config) Redactor() *Redactor {
	r := &Redactor{}
	for _, p := range append(append([]Redaction{}, Builtin...), c.Privacy.Redact...) {
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
		g := re.SubexpIndex("secret")
		if g < 0 {
			s = re.ReplaceAllString(s, "[redacted]")
			continue
		}
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			ix := re.FindStringSubmatchIndex(m)
			if ix == nil || ix[2*g] < 0 {
				return "[redacted]"
			}
			return m[:ix[2*g]] + "[redacted]" + m[ix[2*g+1]:]
		})
	}
	return s
}
