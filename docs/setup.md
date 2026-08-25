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

Writes `~/.daybook/config.yaml` and creates `outputs/`, `raw/` and `state/`
under `output.root`. Everything is `0600`, and the directory holds your prompt
text — keep it private.

## Daily use

```sh
daybook scan       # read the window, join against git, write the report
daybook day        # read today
daybook day 2026-08-23
daybook verify     # check everything is wired up
```

`scan` is idempotent — run it as often as you like. A full run over a 1.1GB
transcript corpus and 44 repositories takes about five seconds.

## Configuration

`~/.daybook/config.yaml`, or `--config PATH`. Precedence is
**env vars > file > built-in defaults**; `DAYBOOK_DIR`, `DAYBOOK_OUTPUT`,
`DAYBOOK_WINDOW` and `DAYBOOK_MACHINE` override at the top.

Every string is quoted on purpose. Under YAML 1.1 an unquoted `12:00` is the
integer 43200, `NO` is `false`, and `010` is `8`.

See `config.example.yaml` for the annotated file.
