// Package model is the single definition of daybook's schema.
//
// Everything else depends on these types and nothing else defines a field.
// The JSON written to raw/ is this, verbatim — so a change here is a schema
// change, and Schema below is what tells a later version it is reading an
// older shape.
package model

import "time"

// Schema version of the raw/*.json documents.
const Schema = 1

// Confidence records HOW a commit was attributed to a stream.
//
// This is a permanent field, not scaffolding. Measured over one week of real
// history: 37% exact, 46% repo, 15% none. A report that prints a single clean
// number when nearly half of it is inference is worse than one that shows the
// split, so the renderer always prints both.
type Confidence string

const (
	// ConfExact — the stream touched a file this commit changed. Provable.
	ConfExact Confidence = "exact"
	// ConfRepo — the stream touched this repo inside the window. Plausible.
	ConfRepo Confidence = "repo"
	// ConfNone — neither. Left unattributed rather than guessed.
	ConfNone Confidence = "none"
)

// State is how far a piece of work has got.
//
// Two states (done / not done) cannot express the case that matters most: work
// that is committed but has never left this machine. `local` is where work gets
// lost, and `stale` is how dropped work becomes visible.
type State string

const (
	StateShipped State = "shipped" // on the remote — done
	StateLocal   State = "local"   // committed, never pushed — at risk
	StateOpen    State = "open"    // active, changes still in the tree
	StateStale   State = "stale"   // silent for stale_after, never shipped
)

// Prompt is one thing the human said.
type Prompt struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Note is one thing the assistant said.
//
// This exists because 73% of the text in a 24-hour window is on the assistant's
// side (130,201 tokens against 47,625 measured). The human's prompts say what
// was ASKED FOR; these say what actually happened — what was found, built, or
// broken. A record built from prompts alone throws away three quarters of itself.
type Note struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Commit is one commit, and where it stands.
type Commit struct {
	Repo    string    `json:"repo"`
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`
	Files   []string  `json:"files,omitempty"`
	Added   int       `json:"added"`
	Deleted int       `json:"deleted"`

	// Pushed means reachable from a remote-tracking ref. Verified not to need a
	// fetch: `git push` updates the local tracking ref, so this is accurate for
	// anything pushed from this machine.
	Pushed bool  `json:"pushed"`
	State  State `json:"state"`

	// StreamID is empty when nothing could be attributed.
	StreamID   string     `json:"streamId,omitempty"`
	Confidence Confidence `json:"confidence"`

	// root is the absolute repo path. Not serialized: it is machine-specific and
	// the report should not leak the filesystem layout into a shared file.
	root string
}

// Root returns the absolute repository path (not serialized).
func (c *Commit) Root() string { return c.root }

// SetRoot records the absolute repository path.
func (c *Commit) SetRoot(p string) { c.root = p }

// Stream is one unit of work: a Claude Code session, bounded by the window.
//
// A session is NOT day-shaped — 16 of 30 titles in the reference corpus span
// multiple days and two ran a full week on one session. So the stream is the
// unit and the day is a view over streams, never the other way round.
type Stream struct {
	ID      string `json:"id"` // sessionId
	Title   string `json:"title"`
	Project string `json:"project"`
	CWD     string `json:"cwd,omitempty"`
	Branch  string `json:"branch,omitempty"`

	First time.Time `json:"first"`
	Last  time.Time `json:"last"`

	// ActiveMinutes counts distinct 10-minute buckets containing a message.
	// Per stream this is wall presence; summed across streams it double-counts,
	// which is why Totals.ActiveMinutes dedupes buckets across the whole day.
	ActiveMinutes int `json:"activeMinutes"`

	Prompts []Prompt `json:"prompts,omitempty"`
	Notes   []Note   `json:"notes,omitempty"`

	Tools map[string]int `json:"tools,omitempty"`
	Files map[string]int `json:"files,omitempty"`

	// Dirs are directories the stream demonstrably operated in — the targets of
	// `cd` and `git -C`. This is a far stronger repo signal than scraping file
	// paths: in auto mode the shell is already inside the repo, so most paths
	// in a command are relative and name no repository at all. Measured on a
	// real day, one stream made 200 Bash calls and yielded exactly one absolute
	// file path.
	Dirs  map[string]int `json:"dirs,omitempty"`
	Repos map[string]int `json:"repos,omitempty"`

	OutputTokens int `json:"outputTokens"`

	// Agent marks a session driven by something other than a human at a
	// keyboard (entrypoint sdk-cli, or a non-human origin). Recorded, reported
	// separately, and kept out of your totals — on one reference day there were
	// six of these against seven real ones.
	Agent bool `json:"agent"`

	Commits []Commit `json:"commits,omitempty"`
	State   State    `json:"state"`

	// Narration is nil whenever narration is off, unavailable, or refused by
	// the verification gate. The report is complete without it.
	Narration *Narration `json:"narration,omitempty"`

	// CarryForward is one line of context for tomorrow's report, so a long
	// stream keeps its thread without re-reading its whole history. Written by
	// the narration pass; empty in v1.
	CarryForward string `json:"carryForward,omitempty"`
}

// Narration is what a model adds to a stream that the deterministic layer
// structurally cannot produce.
//
// FIELDS, NOT PROSE. The model fills these; the renderer owns the document.
// A bad field is then a missing line rather than a mangled report, and each one
// can be verified on its own. It also stops the single most common failure of
// summarising structured data — reading the table back as sentences — because
// there is nowhere in this shape to put a commit count.
type Narration struct {
	// Intent — one sentence. What you were trying to do.
	Intent string `json:"intent"`
	// Happened — what was done, found, built, broke. Drawn mostly from the
	// assistant's side of the conversation, which is where the substance is.
	Happened string `json:"happened"`
	// Decisions — choices made that no commit records. On a day of design work
	// this is the entire output, and it exists nowhere else.
	Decisions []string `json:"decisions,omitempty"`
	// Open — work already done that has not finished proving itself.
	Open []string `json:"open,omitempty"`
	// CarryForward — one line, becomes tomorrow's "previously" so a long stream
	// keeps its thread without re-reading its own history.
	CarryForward string `json:"carryForward,omitempty"`
}

// DayNarration is the synthesis pass, run over the per-stream summaries only —
// never over raw transcripts.
type DayNarration struct {
	Shape    string `json:"shape"`
	Moved    string `json:"moved"`
	Carrying string `json:"carrying"`
}

// Evidence is why an open item was closed.
//
// Required. A close with no citable evidence is refused and the item stays
// open — otherwise the ledger quietly empties itself and stops meaning
// anything.
type Evidence struct {
	Kind  string `json:"kind"` // commit | session | manual
	Repo  string `json:"repo,omitempty"`
	SHA   string `json:"sha,omitempty"`
	Quote string `json:"quote,omitempty"`
}

// OpenItem is one entry in the running ledger.
//
// Append-only: nothing is ever deleted, items change status. Regenerating the
// list nightly would silently drop anything raised on Monday and untouched on
// Tuesday — which is precisely the set that rots.
type OpenItem struct {
	ID       string    `json:"id"`
	Text     string    `json:"text"`
	Stream   string    `json:"stream"`
	StreamID string    `json:"streamId"`
	Repos    []string  `json:"repos,omitempty"`
	Opened   string    `json:"opened"`
	LastSeen string    `json:"lastSeen"`
	Status   string    `json:"status"` // open | closed
	Closed   string    `json:"closed,omitempty"`
	ClosedBy *Evidence `json:"closedBy,omitempty"`
}

// Age in whole days at the given date.
func (o OpenItem) Age(on time.Time) int {
	t, err := time.Parse("2006-01-02", o.Opened)
	if err != nil {
		return 0
	}
	d := int(on.Sub(t).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// RepoState is the working-tree standing of one repository.
//
// Uncommitted work is the riskiest state there is and it costs one `git status`
// to see. Given that most editing happens through bash heredocs rather than the
// Edit tool, a dirty tree is the normal case rather than the edge.
type RepoState struct {
	Repo      string `json:"repo"`
	Branch    string `json:"branch,omitempty"`
	HasRemote bool   `json:"hasRemote"`
	Ahead     int    `json:"ahead"` // commits not on the remote
	Dirty     int    `json:"dirty"` // changed paths in the working tree
	HeadSHA   string `json:"headSha,omitempty"`
}

// Totals are day-level, deduped.
type Totals struct {
	ActiveMinutes int `json:"activeMinutes"`
	Streams       int `json:"streams"`
	AgentStreams  int `json:"agentStreams"`
	Prompts       int `json:"prompts"`
	Commits       int `json:"commits"`
	Shipped       int `json:"shipped"`
	Local         int `json:"local"`
	Added         int `json:"added"`
	Deleted       int `json:"deleted"`
	Repos         int `json:"repos"`
}

// Day is one report: the whole document written to raw/ and rendered to outputs/.
type Day struct {
	Schema      int       `json:"schema"`
	Date        string    `json:"date"`
	Machine     string    `json:"machine"`
	Generated   time.Time `json:"generated"`
	WindowStart time.Time `json:"windowStart"`
	WindowEnd   time.Time `json:"windowEnd"`

	Streams []Stream `json:"streams"`

	// Unattributed are commits inside the window that no stream could claim.
	// Surfaced rather than dropped: a persistently large bucket means the join
	// is wrong, and hiding it would make that invisible.
	Unattributed []Commit `json:"unattributed,omitempty"`

	Repos  []RepoState `json:"repos,omitempty"`
	Totals Totals      `json:"totals"`

	Narration *DayNarration `json:"dayNarration,omitempty"`

	// OpenItems is the ledger as it stands after this day; ClosedToday is what
	// this run closed, printed with its evidence so a wrong close is visible
	// and reversible rather than silent.
	OpenItems   []OpenItem `json:"openItems,omitempty"`
	ClosedToday []OpenItem `json:"closedToday,omitempty"`

	// OtherAuthors names identities that DID commit in this window but were
	// filtered out by identity.authors.
	//
	// It exists because the failure it catches is silent: a wrong author filter
	// produces a report with streams, hours and prompts, and zero commits. That
	// looks like a quiet day rather than a misconfiguration, and it was wrong on
	// the first real run of this tool.
	OtherAuthors []string `json:"otherAuthors,omitempty"`

	// ParseErrors counts transcript lines that could not be read. The format is
	// undocumented and moves between Claude Code versions, so this is expected
	// to be non-zero eventually — and a silent zero would be the real bug.
	ParseErrors int `json:"parseErrors"`
}
