// Package egress is the single declaration of every way daybook can send data
// off this machine.
//
// It exists because prose has no build gate. Screenshots shipped in v0.3.0 and
// both privacy pages went on saying narration was the only thing that left the
// machine — for three releases, until somebody went looking. Every other claim
// in this repo is checked by something: the changelog gate refuses a tag with no
// entry, CI builds all six platforms, a test loads the example config, narration
// is discarded if a sha in the output was not in the input. The one class of
// claim where being wrong matters most was the only one running on trust.
//
// So the list below is the truth, and egress_test.go enforces two things about
// it. First, that it is COMPLETE: no file may open a network connection or spawn
// a process without appearing here or in LocalOnly, so a new way out fails the
// build until somebody classifies it. Second, that it is DOCUMENTED: every route
// must be described in docs/privacy.md.
//
// Neither test can tell whether the prose is any good. They can tell that a
// route was added and nobody wrote anything, which is what actually happened.
package egress

// Route is one way data leaves this machine.
type Route struct {
	// Name is the identifier used in tests and in the `daybook privacy` output.
	Name string
	// File is where it lives, relative to the repository root.
	File string
	// Sends says what actually goes, in the terms a person would care about.
	Sends string
	// To is the other end.
	To string
	// Gate is the config that controls it, or "" when nothing does.
	Gate string
	// Doc is a phrase that must appear in docs/privacy.md. It is the thing the
	// test checks, so it should be specific enough that its presence means the
	// route is genuinely described rather than merely name-dropped.
	Doc string
}

// Routes is every way out. Adding a network call or a subprocess without adding
// it here fails the build.
var Routes = []Route{
	{
		Name:  "narration",
		File:  "internal/narrate/provider.go",
		Sends: "your prompts, the assistant's replies, commit subjects, shas and file paths",
		To:    "Anthropic, through the claude CLI you are already signed in with",
		Gate:  "narrate.enabled",
		Doc:   "Narration is the smaller one",
	},
	{
		Name:  "narration-api",
		File:  "internal/narrate/api.go",
		Sends: "the same facts as narration",
		To:    "the Anthropic API, with your own key",
		Gate:  "narrate.provider",
		Doc:   "your own API key",
	},
	{
		Name: "screenshots",
		File: "internal/preview/capture.go",
		Sends: "the capability list and the agent's navigation, plus it drives your " +
			"real browser and writes photographs of real screens to disk",
		To:   "Anthropic, through the claude CLI, and your own browser",
		Gate: "preview.enabled",
		Doc:  "Screenshots are the larger exception",
	},
	{
		Name:  "git-fetch",
		File:  "internal/vcs/vcs.go",
		Sends: "nothing of yours — it asks your own git remotes what they have",
		To:    "your own git remotes",
		Gate:  "watch.fetch",
		Doc:   "talks to your own git remotes",
	},
	{
		Name:  "upgrade-check",
		File:  "internal/selfupdate/selfupdate.go",
		Sends: "nothing but the request itself",
		To:    "the GitHub releases API",
		Gate:  "", // only when you run `daybook upgrade`
		Doc:   "checking for a newer release",
	},
}

// LocalOnly are files that spawn a process or look like they might reach the
// network, and do not. A file here must not also appear in Routes: api.go was in
// both, because it runs `ant auth status` locally AND talks to the Anthropic API,
// and being on this list is a claim about the whole file. vcs.go was the same:
// mostly local git, plus the one `git fetch` that is not. A file that can reach
// out is a route, whatever else it also does. Listed so the completeness test can tell "checked and
// local" from "nobody has looked at this yet" — an unclassified file is the
// thing being caught, and silence is not a classification.
var LocalOnly = []string{
	"internal/config/config.go",    // git config --get user.email
	"internal/platform/browser.go", // launchctl getenv, pgrep, id
	"internal/svc/wintask.go",      // schtasks
	"internal/wizard/wizard.go",    // git rev-parse while picking folders
	"cmd/daybook/shoot.go",         // starts nothing; the agent runs the servers
	"internal/preview/run.go",      // net.Dial to 127.0.0.1 — asking whether a local port answers
}
