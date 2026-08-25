# daybook

**What you actually got done today — not just what you committed.**

Daily work reports built from your Claude Code sessions and your git history.

```
Monday, 24 August 2026
10.0h active · 10 streams · 180 prompts · 30 commits +5,724/-1,058 · 6 repos

## Summary
The day ran on two tracks that never really touched: getting Acme into a shape
someone could buy, and a long stretch of api work. A theme repeated across
nearly everything: the written artifact ran ahead of the verified one.

### Sun 18:27–Mon 13:42 · Calendar booking embeds        shipped
Get the booking page embeddable so the marketing site hosts booking inline.

Built the three embed modes, then found by actually opening the modal that
required-question toggles and time entry were unusable; fixed those alongside
three embed bugs the same run surfaced.

- **Decided.** Embed config stays stateless: the snippet carries its own
  configuration rather than storing it server-side.
- **Open.** Shipped to main untested. TRUST_PROXY_HOPS=1 needs prod confirmation.

**Shipped** (4 exact, 8 inferred)
- web@f9064af3  Add booking page embeds: inline, popup and floating  +1372/-162

## Not off this machine
| repo     | branch | unpushed | uncommitted |
| gateway | main   |        5 |          75 |
```

---

## Install

```sh
go install github.com/a-handle/daybook/cmd/daybook@latest
```

Needs Go 1.24+ and `git`. `claude` and `gh` are optional.

## Quickstart

```sh
daybook init      # guided setup — points it at your repos, ~30 seconds
daybook scan      # read the last 24h, join against git, write the report
daybook day       # read it
```

That's the whole loop. `scan` takes about five seconds and is safe to run as
often as you like.

---

## What it's for

Your commit log records what landed. It does not record what happened, and the
gap runs in the direction that makes you look worse.

Measured over one real week: **commits per hour swung 13× between days.** The
heaviest day — twelve hours, 143 prompts — produced six commits, because the
day's output was an architecture decision rather than code. The next day, the
same threads executing, produced 34. Work also lands a day after it happens, so
a commit graph lags reality and inverts exactly when the thinking was hardest.

daybook reads both sides and says which is which.

## How people use it

**End of day — "what did I actually do?"**

```sh
daybook scan && daybook day
```

Every stream of work, in the order it ran, with the commits attributed to it and
the ones that never left your machine.

**Standup, without trying to remember**

```sh
daybook day yesterday
```

The `Summary` section is written to be readable aloud.

**Weekly review, or an invoice line**

```sh
daybook week
```

A per-day table plus every stream that spanned more than one day. Because the
shape of a week is exactly what a weekly total hides.

**Finding work you dropped**

```sh
daybook open
```

Everything you did that hasn't finished proving itself — shipped-but-untested,
blocked, unverified in prod — oldest first. Items past 14 days get flagged.
This is the list nobody keeps by hand and everybody needs.

**Proving where the time went**

Contractors, agencies, and anyone billing by project: the report groups by repo
or by business (`business:` in config), and every figure traces to a commit or a
timestamp. Nothing is estimated.

**A day with no commits at all**

Research, ops runs, a client call, a data room. Those days look empty in git and
are fully recorded here — that's most of the point.

**Unattended**

```sh
daybook service install
```

Report waiting for you every morning. See [docs/schedule.md](docs/schedule.md).

---

## Commands

```
daybook init            guided setup
daybook scan            read the window, join against git, write the report
daybook day [date]      print a report — date, today, or yesterday
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

Flags: `--config PATH`, `--window 48h`, `--stdout`, `--narrate`.

---

## How it works

**Discovery.** Any session that received a prompt in the last 24 hours. How long
it has been open is irrelevant — over half of real sessions span multiple days
and some run a week, so the *stream* is the unit and the day is a view over
streams.

**Extraction.** Your prompts *and* the assistant's replies. In a 24-hour window
**73% of the text is on the assistant's side** — that's where "found three bugs
by running it" lives. A record built from prompts alone throws away three
quarters of itself.

**The join.** Commits from `git log`, matched to streams in two tiers, and the
report always prints the split rather than one clean number:

- **exact** — the stream touched a file this commit changed. Provable.
- **inferred** — same repo, and the stream was live nearest the commit time.

On real data that lands around **40% exact**. A commit no stream can claim is
listed as unattributed rather than guessed. Once attributed it's *pinned* and
never re-judged, so today's run can't reshuffle yesterday's report.

**Ship state.** Four states, because *done / not done* can't express the one that
matters most:

| state | meaning |
|---|---|
| `shipped` | on the remote |
| `local` | committed, never pushed — **at risk** |
| `open` | active, changes still in the tree |
| `stale` | silent for `stale_after`, never shipped |

No `git fetch` needed: `git push` updates the local tracking ref, so the unpushed
count is already right for anything sent from this machine.

---

## Prose summaries (optional)

```sh
daybook scan --narrate
```

Adds what git structurally cannot: what you were trying to do, what actually
happened, **decisions no commit records**, and what's still unproven.

Two providers. `cli` (default) spawns the `claude` you're already signed in
with — no key, no setup — and runs it with no tools under `dontAsk`, so the step
*cannot* touch your filesystem. `api` uses the Anthropic API instead if you'd
rather not spend that quota (≈$1/day). **Neither passes a key through daybook.**

Every sha and path in the model's output must appear in its input, or the
narration is discarded and the deterministic report stands alone. A record about
what you did is worthless if it can invent a commit.

Full detail: [docs/narration.md](docs/narration.md).

---

## Privacy

- **Nothing leaves your machine.** No telemetry, no sync, no network calls.
- **Redaction runs before anything is written** — AWS keys, bearer tokens,
  GitHub tokens, private keys. Add your own under `privacy.redact`, or set
  `keep_raw_prompts: false` to store no prompt text at all.
- Output is `0600`. **Keep the output directory private** — it holds your prompt
  history. Don't put it inside a public repo.

[docs/privacy.md](docs/privacy.md)

## Caveats

**This parses an undocumented format.** `~/.claude/projects/*.jsonl` is internal
to Claude Code and carries no compatibility guarantee — one reference corpus
spanned 27 CLI versions. Parsing fails soft per line rather than per file, and
`daybook verify` reports the parse-failure rate so breakage is visible rather
than silent. It will break eventually; please open an issue.

**Stream titles come from Claude and describe where a session started**, not
what it's doing now. A session kept open for a week keeps its original name.
Narration reads what actually happened instead of trusting the title.

## Docs

[setup](docs/setup.md) · [narration](docs/narration.md) ·
[schedule](docs/schedule.md) · [privacy](docs/privacy.md) ·
[record format](docs/format.md) · [troubleshooting](docs/troubleshooting.md)

## Licence

MIT. See [LICENSE](LICENSE).

## A note on names

daybook is an independent open-source project. It is not built, endorsed,
sponsored by, or affiliated with Anthropic. "Claude" and "Claude Code" are
trademarks of Anthropic, referred to here only to describe what this tool reads.

The transcript reader is one adapter behind an interface — other coding agents
can be added without touching the join or the renderer. Today there is one, and
it reads Claude Code.
