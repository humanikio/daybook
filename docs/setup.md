# Setup

## Install

```sh
go install github.com/humanikio/daybook/cmd/daybook@latest
```

Requires Go 1.24+ and `git`. `claude` and `gh` are optional.

## Run the wizard

```sh
daybook init
```

Four steps. It never installs anything — where something is missing it prints the
command and moves on. It never writes credentials. It is safe to re-run; an
existing config is kept unless you pass `--force`.

### [1/4] Checking this machine

Reports what is present:

- **claude** — needed only for narration. **Sign-in is not verified here.**
  `claude doctor` exits 0 whether or not you are logged in, so the only honest
  check is the one narration does at the moment it runs.
- **git** — required. Setup stops without it.
- **gh** — optional, for pull-request enrichment.
- **transcripts** — how many were found, and where. Zero here usually means
  Claude Code keeps them somewhere else; set `watch.agents[].path`.

### [2/4] What should we watch?

Repo roots, comma-separated. The default is guessed by looking for the first of
`~/code`, `~/src`, `~/Projects`, `~/dev`, `~/Desktop`, `~/Documents` that
actually contains a repository — existing but empty directories are skipped.

Then your commit email, defaulted from `git config user.email`. **This matters
on shared repositories**: with no author set, every commit in those repos counts
as yours.

Repos with no remote are called out here, because "shipped" means off the
machine and those have no bar to clear. `output.no_remote` decides what happens
to them — `committed` (default) treats a commit as done, `exclude` leaves them
out.

### [3/4] When should the summary run?

Stored as `schedule.at`. **Scheduling is not wired up yet** — this records your
preference for when it lands. Until then, run `daybook scan` whenever you like.

### [4/4] Writing config

Writes `~/.daybook/config.yaml`, and creates `outputs/`, `raw/` and `state/`
under `output.root` — which defaults to `~/Desktop/daybook`.

**Those are two different places on purpose.** Settings live in a dotfile you
never need to look at; reports land somewhere you actually will. Everything is
`0600`, and the output directory holds your prompt text, so move it if a folder
on your Desktop is the wrong place for that:

```sh
daybook config set output.root ~/Documents/daybook
```

## Catching up

`daybook` can only report tomorrow unless you ask it for yesterday:

```sh
daybook backfill 2w
```

Each day is scanned through the same pipeline a live run uses, so a backfilled
day and a live one are the same file. Days that produced nothing are skipped
rather than written as empty — a file saying "you did nothing" would make
`daybook week` count a day that was never measured.

Narration is opt-in here even when `narrate.enabled` is set: a fortnight at two
minutes a day is half an hour and a large slice of quota, and nobody asking for
their history back is asking for that. Add `--narrate` when you want it.

How far back you can go is bounded by Claude Code's own retention — backfill
prints the oldest transcript it can see, and warns for any range before it,
where you will get commits but no sessions.

## Changing settings later

`init` re-asks everything and overwrites answers you were happy with. For a
single change:

```sh
daybook config edit                        # arrow through the settings
daybook config set narrate.enabled true    # if you know the key
daybook watch ~/clientwork                 # add a folder
daybook watch ~/clientwork --preview       # and allow screenshots of it
daybook unwatch ~/oldwork                  # stop watching one
daybook schedule 22:00 --days mon,wed,fri
```

`config edit` needs a terminal. Everywhere else — a script, CI, a pipe — use
`config set`, which takes the same values and validates them the same way.

The rows it offers:

| row | what it changes |
|---|---|
| Watching | folders scanned, and which of them may be photographed |
| Commit email | which commits count as yours |
| Runs at · Catch up | [schedule](schedule.md) |
| Reports in | where reports land |
| **Formats** | markdown only, or markdown and HTML every day |
| Narration | [prose summaries](narration.md) |
| Window | how far back a run looks |
| Keep prompts | store prompt text, or only its shape |
| **Screenshots** | [everything on this page](screenshots.md) |

Screenshots asks four things: whether to enable them, how many pictures at most,
whether it may start an app that is not running, whether to photograph on the
nightly run, and which repositories to include. It lists the repositories it
found rather than asking you to name them from memory.

## Prose summaries

Step 6 of `init` asks how they should be written:

| | needs | costs |
|---|---|---|
| **Claude Code** | the `claude` login already on this machine | your Claude subscription quota |
| **Anthropic API** | `ANTHROPIC_API_KEY`, or `ant auth login` | about $1 a day |
| **Off** | nothing | the report is complete without it |

daybook stores no credentials either way.

**Being signed in is separate from turning it on.** `claude doctor` exits 0
whether or not you are logged in, so nothing can verify it ahead of time — if
you are logged out, narration says so and names the fix, and the deterministic
report is already written by then.

## Daily use

```sh
daybook scan       # read the window, join against git, write the report
daybook day        # read today
daybook day 2026-08-23
daybook shoot      # photograph where today's work lives, if screenshots are on
daybook verify     # check everything is wired up
daybook upgrade    # is there a newer release
```

`scan` is idempotent — run it as often as you like. A full run over a 1.1GB
transcript corpus and 44 repositories takes about five seconds.

## Configuration

`~/.daybook/config.yaml`, or `--config PATH`. Precedence is
**env vars > file > built-in defaults**; `DAYBOOK_DIR`, `DAYBOOK_OUTPUT`,
`DAYBOOK_WINDOW` and `DAYBOOK_MACHINE` override at the top.

Every string is quoted on purpose. Under YAML 1.1 an unquoted `12:00` is the
integer 43200, `NO` is `false`, and `010` is `8`.

See `config.example.yaml` for the annotated file. Every key is in it, and a test
loads it on every build so it cannot drift from what the code accepts.
