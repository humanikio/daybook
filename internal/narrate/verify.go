package narrate

import (
	"regexp"
	"strings"
)

// The verification gate.
//
// A report about what you did is worthless if it can invent a commit. So every
// checkable token in the model's output must appear somewhere in the facts it
// was given; if one does not, the narration is discarded and the deterministic
// section stands on its own.
//
// This is deliberately cheap and deliberately strict. It cannot tell whether a
// sentence is TRUE — no automated check can — but it can guarantee that no
// identifier in it was fabricated, which is the failure mode that would destroy
// trust in the whole record.

var (
	// A hex run long enough to be a commit sha. Word-bounded so it does not
	// fire on the middle of a hash-like word.
	shaLike = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	// Something with a slash and an extension: a file path.
	pathLike = regexp.MustCompile(`\b[A-Za-z0-9._@-]+(?:/[A-Za-z0-9._@-]+)+\.[A-Za-z0-9]{1,6}\b`)
)

// Verify reports the first fabricated token in out, or "" when it is clean.
func Verify(out, facts string) string {
	lf := strings.ToLower(facts)
	for _, m := range shaLike.FindAllString(strings.ToLower(out), -1) {
		// Ignore pure-digit runs: years, counts and line numbers are not shas.
		if isAllDigits(m) {
			continue
		}
		if !strings.Contains(lf, m) {
			return m
		}
	}
	for _, m := range pathLike.FindAllString(out, -1) {
		if !strings.Contains(facts, m) {
			return m
		}
	}
	return ""
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// extractJSON pulls the first JSON object out of a model response.
//
// Models wrap JSON in prose or a fenced block however firmly you ask them not
// to. Rather than fail the run on formatting, take the outermost braces — the
// schema check that follows is what actually decides whether the content is
// usable.
func extractJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
