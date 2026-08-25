// Package selfupdate reports whether a newer daybook release exists.
//
// It never replaces the binary. Upgrading means re-running the installer, which
// already knows where this platform puts things and how to check a signature —
// and a program that overwrites itself on disk is a much larger promise than
// "tell me if I am behind". So this only reports, and prints the one command
// that does the job.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// Unauthenticated: the repo is public, and GitHub allows 60 requests an
	// hour per IP, which is ample for a check somebody runs by hand.
	latestReleaseURL = "https://api.github.com/repos/humanikio/daybook/releases/latest"
	downloadBase     = "https://github.com/humanikio/daybook/releases/latest/download"
	modulePath       = "github.com/humanikio/daybook/cmd/daybook"
)

// InstallCmd returns the command that upgrades THIS installation.
//
// Three answers, not one. `curl … | sh` cannot work on Windows twice over:
// PowerShell aliases curl to Invoke-WebRequest, which rejects -fsSL before
// fetching anything, and install.sh supports linux and darwin only.
//
// And a binary that came from `go install` lives in GOPATH/bin, where the shell
// installer would not replace it — it writes to ~/.local/bin, leaving two
// daybooks on PATH and an upgrade that appears not to have worked. Detect where
// this one is running from and answer for that.
func InstallCmd() string {
	if fromGoInstall() {
		return "go install " + modulePath + "@latest"
	}
	if runtime.GOOS == "windows" {
		return "irm " + downloadBase + "/install.ps1 | iex"
	}
	return "curl -fsSL " + downloadBase + "/install.sh | sh"
}

// fromGoInstall reports whether the running binary sits in a Go bin directory.
func fromGoInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	dir := filepath.Dir(exe)
	if gobin := os.Getenv("GOBIN"); gobin != "" && sameDir(dir, gobin) {
		return true
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, g := range filepath.SplitList(gopath) {
			if sameDir(dir, filepath.Join(g, "bin")) {
				return true
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return sameDir(dir, filepath.Join(home, "go", "bin"))
	}
	return false
}

func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// Result is the outcome of a check.
type Result struct {
	Current   string // the running binary's version, v-stripped
	Latest    string // newest published release tag, v-stripped
	Available bool
	// Local means Current is not a released version — built from source, or
	// from a branch. See IsLocalBuild.
	Local bool
}

// IsLocalBuild reports whether a version names something that was never
// released.
//
// This matters more than it looks. A source build's version is a statement of
// intent, not of content: `go build` with no ldflags reports "dev", and a
// branch build reports what it was aiming at rather than what is in it.
// Comparing either against a release tag answers a question the number cannot
// answer — and answering it anyway is actively harmful, because "you are on the
// latest" is exactly the wrong thing to tell a machine that is behind.
//
// So a local build always reports an update. Installing a release over one is
// the right move for anyone not developing daybook, and anyone who is knows to
// ignore it.
func IsLocalBuild(version string) bool {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	return v == "" || v == "dev" || strings.Contains(v, "-")
}

// Check queries the latest release and compares it against current.
//
// A non-nil error means the check could not complete — offline, rate-limited,
// no release yet. Callers must treat that as "unknown", never as "up to date":
// silence is the one answer this must not give.
func Check(ctx context.Context, current string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Result{}, fmt.Errorf("no releases published yet")
	case http.StatusForbidden:
		return Result{}, fmt.Errorf("github rate-limited this IP — try again later")
	default:
		return Result{}, fmt.Errorf("github api: %s", resp.Status)
	}

	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Result{}, err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(body.TagName), "v")
	if latest == "" {
		return Result{}, fmt.Errorf("the release had no tag")
	}
	cur := strings.TrimPrefix(strings.TrimSpace(current), "v")

	if IsLocalBuild(cur) {
		return Result{Current: cur, Latest: latest, Available: true, Local: true}, nil
	}
	return Result{Current: cur, Latest: latest, Available: newer(latest, cur)}, nil
}

// newer reports whether semver a is strictly greater than b, segment by segment
// and numerically — a string compare puts 0.9.0 above 0.10.0.
func newer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var an, bn int
		if i < len(as) {
			an, _ = strconv.Atoi(leadingDigits(as[i]))
		}
		if i < len(bs) {
			bn, _ = strconv.Atoi(leadingDigits(bs[i]))
		}
		if an != bn {
			return an > bn
		}
	}
	return false
}

// leadingDigits keeps the numeric run at the start of a segment, so a junk tag
// degrades instead of crashing the check.
func leadingDigits(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}
