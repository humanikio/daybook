<h1>daybook</h1>

**Your git log is an incomplete record of your day. This is the rest of it.**

Reads your Claude Code sessions and your git history, and writes one report:
what you actually worked on, what of it shipped, what broke, and what is still
sitting on your laptop where nobody else can see it.

```sh
curl -fsSL https://github.com/humanikio/daybook/releases/latest/download/install.sh | sh
daybook init && daybook backfill 7
```

---

## The report is written for someone else to read

The top of every day is grouped by **capability**, not by commit — because
fourteen commits that together let you do one new thing are one thing that
happened, not fourteen.

> **You can now test an ingest transform against payloads that actually
> arrived, choose which ones to run it against, and see how old each is.**
>
> `readRecentPayloads.ts` reads live rows alongside stored test sends,
> `collectSamples.ts` merges the two and accepts an explicit list of payload
> ids, and the controller returns provenance per sample. The frontend adds
> `PayloadPicker.tsx`, a multi-select modelled on the field mapping editor.
>
> **Look at:**
> `src/ingest/services/collectSamples.ts` ·
> `src/app/sources/[sourceId]/transform/page.tsx`
>
> `api@7d26888f` · `web@0949b796` on **main**

Plain language, then the mechanism for whoever maintains it, then the files and
the branch. **Every commit lands in exactly one entry** — a day with sixty
commits does not get five bullet points.

Hand it to a teammate and they know what changed and where to look.

---

## Why your commit log undersells you

Measured over one real week: **commits per hour swung 13×** between days.

That is not effort. The heaviest day — twelve hours, 143 prompts — produced
**six commits**, because the output was an architecture decision. The next day,
the same threads executing, produced 34. Work also lands a day after it happens.

So a commit graph lags reality and **inverts exactly when the thinking was
hardest**. daybook reads both sides and says which is which.

---

## What it sees that git cannot

| | |
|---|---|
| **Decisions** | *"Excluded shared infra costs from the P&L on the grounds they are not this product's."* Recorded nowhere else. |
| **What broke** | 282 failed commands in one day. Failures are where the time went, and the part nobody writes down. |
| **Work that never committed** | Research, an ops run, a client call, a data room. Days that look empty in git and were not. |
| **What is stuck on your machine** | Unpushed commits *with their subjects*, uncommitted files, per branch. "14 unpushed" is a number nobody can act on. |
| **What is unproven** | A running ledger of shipped-but-untested work, closed only against citable evidence, sorted oldest first. |

---

## Four states, because *done / not done* is not enough

| | |
|---|---|
| `shipped` | on the remote |
| `local` | committed, never pushed — **at risk** |
| `open` | active, changes still in the tree |
| `stale` | silent for days, never shipped |

No `git fetch` needed: `git push` updates the local tracking ref, so the
unpushed count is already right for anything sent from this machine.

---

## Honest about what it is guessing

Commits are matched to work in two tiers, and the report **always prints the
split** rather than one clean number:

- **exact** — the session touched a file this commit changed. Provable.
- **inferred** — same repo, and the session was live nearest the commit.

Measured across eight real days: **55% exact overall, ranging 28–67% by day**.
The report prints the split per entry, so you always know which half you are
reading. A commit nothing can claim is listed as unattributed rather than
guessed at. Once matched it is pinned and
never re-judged, so today's run cannot quietly rewrite yesterday's report.

---

## Prose summaries, without the hallucinations

```sh
daybook scan --narrate
```

Uses the `claude` you are already signed in with — no key, no setup — or the
Anthropic API if you would rather not spend that quota. **Neither passes a
credential through daybook.**

Every sha and path in the model's output must appear in its input, or the
narration is thrown away and the deterministic report stands alone. A record
of what you did is worthless if it can invent a commit.

---

## Everything else

```
daybook init                 guided setup, about a minute
daybook backfill 2w          build the days from before you installed it
daybook scan                 today, against the last 24h
daybook day [date]           read it — date, "today" or "yesterday"
daybook week                 rollup, with a per-day table
daybook open                 work that has not finished proving itself
daybook config edit          change settings with the arrow keys
daybook service install      run it nightly, as you, never as root
daybook verify               check everything in one pass
```

A scan over a 1.1 GB transcript corpus and 23 repositories takes **about two
seconds**. Backfilling a week takes fourteen.

---

## Privacy

**Nothing leaves your machine.** No telemetry, no sync, no network calls — the
only setting that touches a network talks to your own git remotes.

Redaction runs **before** anything is written: AWS keys, bearer tokens, GitHub
tokens, private keys. Add your own patterns, or set
`privacy.keep_raw_prompts: false` to keep the whole shape of your day and none
of the words.

Output is `0600`. Keep the folder private — it holds your prompt history.

---

## The honest caveat

This parses `~/.claude/projects/*.jsonl`, which is **internal to Claude Code
and carries no compatibility guarantee** — one reference corpus spanned 27 CLI
versions. Parsing fails soft per line rather than per file, and `daybook verify`
reports the parse-failure rate so breakage is visible instead of silent.

It will break eventually. Please open an issue when it does.

---

## Docs

[setup](docs/setup.md) · [narration](docs/narration.md) ·
[schedule](docs/schedule.md) · [privacy](docs/privacy.md) ·
[record format](docs/format.md) · [troubleshooting](docs/troubleshooting.md) ·
[releasing](docs/releasing.md)

Install with Go instead: `go install github.com/humanikio/daybook/cmd/daybook@latest`

Every release binary is [cosign](https://docs.sigstore.dev/)-signed and shipped
with checksums.

## Releases

Every version is described in [CHANGELOG.md](CHANGELOG.md), and the release
workflow refuses to publish a tag that has no entry there. The procedure is
[docs/releasing.md](docs/releasing.md).

## Licence

MIT.

## A note on names

daybook is an independent open-source project. It is not built, endorsed,
sponsored by, or affiliated with Anthropic. "Claude" and "Claude Code" are
trademarks of Anthropic, referred to here only to describe what this tool reads.

The transcript reader is one adapter behind an interface — other coding agents
can be added without touching the join or the renderer. Today there is one, and
it reads Claude Code.
