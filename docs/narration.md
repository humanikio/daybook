# Narration

Narration adds what the deterministic layer structurally cannot: what you were
trying to do, what actually happened, **decisions no commit records**, and what
is still unproven.

It is off by default, and optional in the strongest sense — **the report is
written to disk before narration is ever invoked**. Narration failing, timing
out, or being refused costs you a few paragraphs, never the day.

## When it runs

Three ways in, and they are independent:

| how | when it narrates |
|---|---|
| `daybook scan --narrate` | that run only. Config untouched. |
| `daybook narrate [date]` | on demand, over a scan that already happened |
| `narrate.enabled: true` | **every** scan, including the scheduled one |

**With `narrate.enabled: true`, a plain `daybook scan` narrates too.** That is
usually what you want — but it turns a two-second command into a two-minute
one, so `scan` says it is narrating before it starts, and `--no-narrate` skips
it when you just want the facts refreshed.

For the nightly report — the one you actually read — you almost certainly want
it always on:

```sh
daybook config set narrate.enabled true
```

The scheduler re-reads config every tick, so that takes effect on the next run
with no restart.

To check what will happen:

```sh
daybook verify        # …narration  available (provider auto, enabled=true)
```

## Providers

`auto` tries the CLI first and falls back to the API. The CLI is first because
it asks for less: no key, no configuration, no spend beyond a subscription you
already have.

### `cli` — Claude Code (default)

```
claude -p --output-format text --permission-mode dontAsk --tools "" <prompt>
```

**daybook holds no credentials.** It uses whatever login you already have.

`--tools ""` under `dontAsk` means the narration step **cannot touch the
filesystem**. It takes text and returns text. That is structural, not a promise.

The cost is quota: a nightly report spends the same subscription your work does.

**Sign-in cannot be checked ahead of time.** `claude doctor` exits 0 whether or
not you are logged in, so detection is reactive:

```
narration skipped: claude is not signed in on this machine —
run `claude` and log in, then re-run `daybook narrate`
```

### `api` — the Anthropic API

```yaml
narrate:
  provider: api
  model: ""          # default claude-opus-5
  effort: low        # low | medium | high | xhigh | max
```

Does not touch your subscription quota, and lets you pin a model.

**daybook never reads, stores or passes a key of its own.** The client is
constructed zero-arg and the SDK owns credential resolution, first match wins:

1. `ANTHROPIC_API_KEY`
2. `ANTHROPIC_AUTH_TOKEN`
3. the active OAuth profile from `ant auth login`
4. workload identity federation
5. the default profile on disk

**An unset `ANTHROPIC_API_KEY` does not mean there are no credentials**, so
detection checks all of the above. Testing one env var would tell anyone signed
in through `ant auth login` they had none.

Detection is a pre-flight hint, not proof. The only proof is a request, and
burning one on every scan to find out would cost money.

Rough cost at Opus 5 rates: a day is around 178k input tokens across all
streams, so **≈$1/day**. `effort` is the lever to reach for before downgrading
the model — it changes how hard the model thinks, not which model runs.

A refusal arrives as HTTP 200 with `stop_reason: refusal`, not as an error, so
it is checked explicitly. Reading the content without checking would give an
empty string and look like a parse failure.

### `off`

Never narrate. This is a complete report, not a degraded one.

## What the model sees

**Never a raw transcript.** It sees the derived facts for one stream: your
prompts, the assistant's messages, the attributed commits, and one line of
carry-forward from yesterday.

Per stream, not per day — around a dozen small calls rather than one enormous
one. Each fits comfortably, they run concurrently (`narrate.concurrency`,
default 3), and one failure loses one stream instead of the day. A real run over
ten streams took **2m40s**.

A final synthesis pass runs over the per-stream summaries only.

## The fields

The model fills fields; **the renderer owns the document**. A bad field is a
missing line, not a mangled report.

| field | content |
|---|---|
| `intent` | one sentence — what you were trying to do |
| `happened` | 2–4 sentences — done, found, built, broken |
| `decisions` | choices no commit records |
| `open` | work already done that has not finished proving itself |
| `carryForward` | one line — becomes tomorrow's *previously* |

Day level adds `shape`, `moved`, `carrying`.

The prompt forbids restating counts, times and shas — they are printed
immediately above the prose — and forbids praise or grading. *"92 tool calls,
nothing recorded as produced"* is a fact and belongs in `happened`;
*"unproductive"* is a judgement and does not.

## The verification gate

A record about what you did is worthless if it can invent a commit.

Every sha-shaped and path-shaped token in the output must appear in the facts it
was given. One that does not means the whole narration for that stream is
**discarded** and the deterministic section stands alone:

```
narrated 9 stream(s) via claude-cli · 1 rejected by the verification gate
```

The gate cannot tell whether a sentence is *true* — nothing automated can — but
it guarantees no identifier was fabricated. It has unit tests, because a check
nobody has seen reject anything is not a check.

## The open ledger

`open` items accumulate into `state/open.json` — **append-only**, nothing ever
deleted, items change status. Regenerating nightly would silently drop anything
raised on Monday and untouched on Tuesday, which is exactly the set that rots.

Closing runs in three stages, the same shape as commit attribution:

1. **Narrow deterministically.** An item is a candidate only if today's commits
   touch its repos or its stream was active. Forty items against fifty commits
   becomes a handful; the rest stay open for free.
2. **Judge.** The model returns `closed` or `still_open` per candidate, under an
   instruction to default to *not* closing.
3. **Gate on evidence.** A close must cite a sha present in today's commits, or
   quote a summary verbatim. **No citable evidence, no close** — otherwise the
   ledger quietly empties itself and stops meaning anything.

```sh
daybook open              # everything still open, oldest first
daybook close <id>        # close by hand
daybook reopen <id>       # undo a close
```

Some items can never close automatically — rotating a leaked key leaves no trace
daybook can see. That is what manual close and **age** are for. An item open 30
days is either done-and-unrecorded or genuinely rotting, and either way it is
the one worth looking at. Items past 14 days are flagged.

Closures print their evidence, so a wrong close is visible the day it happens
and costs one command to undo. Reversible beats confident.

## Expect a lot of items

One real day produced **44**. The report shows the 15 oldest and sends the rest
to `daybook open`. If that feels like too much, the signal to watch is age — the
0-day items are today's honesty, the 20-day ones are the actual backlog.
