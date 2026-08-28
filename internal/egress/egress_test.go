package egress

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Anything that could reach off this machine. exec.Command is included because
// whether a subprocess talks to the network is not decidable by reading our
// source — `git fetch` does and `git status` does not — so spawning one is
// treated as needing a decision rather than as obviously safe.
var reachesOut = regexp.MustCompile(
	`http\.(Get|Post|Head|NewRequest|NewRequestWithContext|DefaultClient|Client\{)` +
		`|net\.Dial` +
		`|exec\.Command` +
		`|anthropic\.NewClient`,
)

// Network I/O this package does directly, as opposed to spawning something that
// might. LocalOnly means "spawns a process and stays here" — a file on that list
// that starts doing its own HTTP has changed category, and classifying it once
// must not buy it silence forever.
var speaksNetwork = regexp.MustCompile(
	`http\.(Get|Post|Head|NewRequest|NewRequestWithContext|DefaultClient|Client\{)` +
		`|anthropic\.NewClient`,
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// LocalOnly is a claim about a file, not a permanent exemption. A file that was
// classified as local and has since grown an HTTP client is the exact way this
// gate would be defeated by ordinary work rather than by anyone deciding to.
func TestLocalOnlyFilesStayLocal(t *testing.T) {
	root := repoRoot(t)
	for _, f := range LocalOnly {
		p := filepath.Join(root, filepath.FromSlash(f))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("LocalOnly names %s and it is not there — stale classification", f)
			continue
		}
		// 127.0.0.1 is this machine. Asking whether a local port answers is not
		// egress, and preview.Reachable is the only reason net.Dial appears at all.
		if speaksNetwork.Match(b) {
			t.Errorf("%s is classified LocalOnly but now does its own network I/O.\n"+
				"Move it to egress.Routes and describe it in docs/privacy.md.", f)
		}
	}
}

// COMPLETENESS. A file that can reach off this machine must be classified,
// either as a route out or as local. An unclassified one fails, which is the
// whole point: the way a new egress gets shipped silently is that nobody was
// asked about it.
func TestEveryWayOutIsClassified(t *testing.T) {
	root := repoRoot(t)
	known := map[string]bool{}
	for _, r := range Routes {
		known[filepath.FromSlash(r.File)] = true
	}
	for _, f := range LocalOnly {
		known[filepath.FromSlash(f)] = true
	}

	var unclassified []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || known[rel] {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		// This file names the primitives in a regexp; it is not a way out.
		if rel == filepath.FromSlash("internal/egress/egress.go") {
			return nil
		}
		if reachesOut.Match(b) {
			unclassified = append(unclassified, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(unclassified) > 0 {
		t.Errorf("these can reach off this machine and are classified nowhere:\n  %s\n\n"+
			"Add each to egress.Routes (and describe it in docs/privacy.md) or to\n"+
			"egress.LocalOnly if it stays here. Do not delete this test to get past it:\n"+
			"an undocumented way out is exactly what it exists to catch.",
			strings.Join(unclassified, "\n  "))
	}
}

// DOCUMENTED. A route that privacy.md does not describe is a route somebody
// turns on without knowing what it costs them.
func TestEveryRouteIsInThePrivacyDoc(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "privacy.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Two things prose does that a naive Contains does not survive: it hard-wraps,
	// so a phrase spanning a line break is present to a reader and absent to the
	// test; and it capitalises whatever starts a sentence. Normalise both.
	norm := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(s), " "))
	}
	doc := norm(string(b))
	for _, r := range Routes {
		want := norm(r.Doc)
		if !strings.Contains(doc, want) {
			t.Errorf("docs/privacy.md never says %q, so route %q is undocumented",
				r.Doc, r.Name)
		}
	}
}

// A route with no description is a row in a table, not a disclosure.
func TestRoutesAreDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Routes {
		if r.Name == "" || r.File == "" || r.Sends == "" || r.To == "" || r.Doc == "" {
			t.Errorf("route %+v has an empty field", r)
		}
		if seen[r.Name] {
			t.Errorf("two routes named %q", r.Name)
		}
		seen[r.Name] = true
	}
}

// A file cannot be both a way out and local. api.go was in both lists, because
// it runs a local credential check and also talks to the Anthropic API — and
// LocalOnly is a claim about the whole file, not about one line in it.
func TestNoFileIsBothRouteAndLocal(t *testing.T) {
	route := map[string]bool{}
	for _, r := range Routes {
		route[r.File] = true
	}
	for _, f := range LocalOnly {
		if route[f] {
			t.Errorf("%s is listed as both a route out and local-only", f)
		}
	}
}
