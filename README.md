# daybook

**What you actually got done today — not just what you committed.**

Daily work reports built from your Claude Code transcripts and your git history.

---

## Install

```sh
go install github.com/a-handle/claude-code-daybook/cmd/daybook@latest
```

Or from a clone:

```sh
git clone https://github.com/a-handle/claude-code-daybook
cd claude-code-daybook && go build -o daybook ./cmd/daybook
```

## Quickstart

```sh
daybook init      # guided setup, writes ~/.daybook/config.yaml
daybook scan      # read the last 24h, join against git, write the report
daybook day       # read it
```

`scan` takes a few seconds and is safe to run as often as you like. Point it at
where your repos live and it will find them.

```
daybook init            guided setup
daybook scan            read the window, join against git, write the report
daybook day [date]      print a report (default: today)
daybook week [date]     rollup for the week containing date
daybook narrate [date]  add prose and reconcile the open ledger
daybook open            work that has not finished proving itself
daybook close <id>      close a ledger item by hand
daybook reopen <id>     undo a close
daybook serve           run the scheduler in the foreground
daybook service …       install | uninstall | start | stop | restart | status
daybook verify          check config, sources, repos, parse health
daybook version
```

Flags: `--config PATH`, `--window 48h`, `--stdout`.

## What you get

```markdown
# Monday, 24 August 2026

10.0h active · 10 streams · 180 prompts · 30 commits +5,724/-1,058 · 6 repos

| shipped to | commits | lines | repos |
|---|--:|--:|---|
| api | 12 | +1,688/-111 | api |
| web | 9 | +2,505/-843 | web |

## Streams

### Sun 18:27–Mon 13:42 · Calendar booking embeds

`shipped` · 54 prompts · 240k tokens · web, humanikDocs

**Asked for**
- `11:18` what pages in the ui are we showing the embed options

**Shipped** (4 exact, 14 inferred)
- `web@f9064af3` Add booking page embeds: inline, popup and floating `+1372/-162`

## Not off this machine

| repo | branch | unpushed | uncommitted |
|---|---|--:|--:|
| gateway | main | 5 | 75 |
```

## Why

A commit log records what landed, not what happened, and the gap runs in the
direction that makes you look worse. Measured over one real week: commits per
hour swung 13× between days. The heaviest day produced six commits, because the
day's output was a design decision rather than code. Work also lands a day after
it happens, so the graph lags reality and inverts exactly when the thinking was
hardest.

Three things follow, and they shape everything here:

- **Sessions are not day-shaped.** Over half of them span multiple days; some run
  a week. So the *stream* is the unit and the day is a view over streams.
- **Most of the substance is on the assistant's side.** In a 24-hour window, 73%
  of the text is assistant messages. A record built from your prompts alone
  throws away three quarters of itself.
- **Done means off the machine, not committed.** A commit that never left is
  work at risk, and the report says so.

## What "shipped" means

Four states, because *done / not done* cannot express the one that matters:

| state | meaning |
|---|---|
| `shipped` | on the remote |
| `local` | committed, never pushed — at risk |
| `open` | active, changes still in the tree |
| `stale` | silent for `stale_after`, never shipped |

No `git fetch` is required. `git push` updates the local tracking ref, so the
unpushed count is already correct for anything sent from this machine. Set
`watch.fetch: true` only if you want to notice pushes made elsewhere.

## Attribution, honestly

Commits are matched to streams in two tiers, and the report always prints the
split rather than one clean number:

- **exact** — the stream touched a file this commit changed. Provable.
- **inferred** — same repo, same window, and the stream was live nearest to the
  commit. Plausible, not proven.

On real data this lands around 40% exact. A commit no stream can claim is listed
as unattributed rather than guessed. Once a commit is attributed it is *pinned*
and never re-judged, so today's run cannot reshuffle yesterday's report.

## Privacy

- **Nothing leaves your machine.** No telemetry, no sync, no network calls.
- **Redaction runs before anything is written.** AWS keys, bearer tokens, GitHub
  tokens and private keys are stripped from prompt text by default; add your own
  patterns under `privacy.redact`. Set `privacy.keep_raw_prompts: false` to store
  no prompt text at all.
- Output is written `0600`. **Keep the output directory private** — it holds your
  prompt history.

## Caveats

**This parses an undocumented format.** `~/.claude/projects/*.jsonl` is internal
to Claude Code and carries no compatibility guarantee — one reference corpus
spanned 27 CLI versions. Parsing is defensive and fails soft per line rather than
per file, and `daybook verify` reports the parse-failure rate so breakage is
visible instead of silent. It will break eventually; please open an issue.

**Stream titles come from Claude and reflect where a session started**, not
necessarily what it is doing now. A long-lived session keeps its original name.

## Automatic daily reports

```sh
daybook service install
```

Registers a LaunchAgent (macOS), a systemd `--user` service (Linux), or a logon
task (Windows) — **always as you, never as root**, because a root service has a
different `HOME`, keychain and git identity, and would produce an empty report
forever.

Runs are owed to a *slot* rather than a moment, so a laptop asleep at 23:30 gets
its report on wake instead of skipping the day. See `docs/schedule.md`.

## Prose summaries

```sh
daybook scan --narrate
```

Narration spawns the `claude` you are already signed in with — daybook holds no
credentials — and adds what git cannot: what you were trying to do, what
actually happened, **decisions no commit records**, and what is still unproven.

Everything checkable in its output must appear in the input or the narration is
discarded. See `docs/narration.md`.

## Not built yet

The `api` narration provider. `provider: cli` is the default and works today.

## Licence

Not yet chosen.
