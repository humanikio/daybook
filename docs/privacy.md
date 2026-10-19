# Privacy

daybook reads the most sensitive text on your machine: everything you have ever
typed to Claude Code, and everything it said back.

**To see what your machine actually does, rather than what this page says:**

```sh
daybook privacy
```

It reads your live config and reports which routes can send right now, how many
screenshots are on disk, and whether your reports folder looks like it is inside
a sync client. Nothing authors that output, so it cannot drift. This page is the
explanation; that command is the state.

Every route it lists is checked against this page on every build — a new way out
fails the tests until it is described here. That gate exists because this page
was wrong for three releases after screenshots shipped.

## Two rules

**1. Nothing phones home.** No telemetry, no sync, no analytics. daybook has no
server and reports nothing about you to anyone.

**The scan is entirely local.** Reading transcripts, joining them against git,
and writing the report involve no network at all. `watch.fetch: true` is the
only setting in that path that touches one, and it talks to your own git
remotes.

`daybook upgrade` asks GitHub whether a newer release exists. Checking for a
newer release sends nothing about you — it is an unauthenticated request for a
public release listing, and it only happens when you run that command or the
installer.

**Two features are exceptions, and both are off by default.** Narration is the
smaller one. Screenshots are the larger one, and are covered below.

Writing prose means sending the day's derived facts to a model, and there is no
way around that:

| sent when narration runs | never sent |
|---|---|
| your prompts, as text | the raw transcript files |
| the assistant's replies | the finished report |
| commit subjects, shas, file paths | anything identifying you |

It goes through **your own account** — the `claude` you are signed in with, or
your own API key — so it is covered by whatever agreement you already have.
daybook holds no credential and adds no relationship of its own.

If that trade is not one you want, `narrate.enabled: false` is the default and
the deterministic report is complete without it.

**Screenshots are the larger exception, and off by default.** See
[screenshots](screenshots.md) for the settings; what matters here is what it
costs you.

It sends the same way narration does, through your own account. It also does two
things narration never does:

| | |
|---|---|
| **Drives your real browser, as you** | Whatever that browser can reach, the capture can reach — production admin, customer records, an email tab left open. |
| **Writes photographs of real screens to disk** | They land in your reports folder as ordinary image files. |

**Redaction cannot help here.** It runs over text before it reaches disk. It
cannot run over a picture of a screen. If your environment holds real customer
names, they are in the images, and they stay there until you delete them.

Three settings widen this further, each off by default:
`preview.start_servers` runs your project's code unattended,
`preview.on_schedule` does all of it at 22:00 without you present, and a watched
folder without `preview.repos` opts in every repository beneath it.

Point it at a development environment where you can, and keep the reports folder
private — it already held your prompt history, and now holds pictures of your
product.

**2. Redaction runs before anything is written.** Not after, not on render —
before the first byte reaches disk.

## What is read

| Source | Used for |
|---|---|
| `~/.claude/projects/**/*.jsonl` | sessions, prompts, assistant text, tool calls |
| your git repositories | commits, diffstat, branch, remote state |

Transcript files are opened read-only and never modified. Nothing is written
anywhere except under `output.root`.

The second row applies to the whole session as well as the `.jsonl`: with the
archive on, the directory Claude Code keeps beside each transcript (subagents,
tool results) is read so it can be copied.

## What is written

daybook uses **two** directories, and only one of them holds your words.

```
~/.daybook/                        CONFIG ONLY
  config.yaml                      settings — no prompt text

<output.root>/                     default ~/Desktop/daybook
  outputs/YYYY-MM-DD.md            report — prompt text, file paths, commits
  raw/YYYY-MM-DD.<machine>.json    the same, structured
  sessions/YYYY-MM-DD.<machine>.json  the day's sessions — purpose, state, paths
  transcripts/<project>/<id>.jsonl    the archive — whole transcripts, UNREDACTED
  state/open.json                  the ledger
  state/pins.json                  commit → stream
  state/last-run.json              timestamps only
```

All files are `0600`.

The output directory defaults somewhere **visible** on purpose — a report you
never open is not a report. The trade is that a visible folder holding your
prompt history is also visible during a screen share. Move it if that matters:

```sh
daybook config set output.root ~/Documents/daybook
```

**Do not put `output.root` inside a public repository.** A `.gitignore` is one
`git add -f` away from publishing your prompt history. If you want the record
version-controlled, use a separate private repository.

## Redaction

Applied to every prompt, every assistant message and every kept error before
writing — and so before narration sends any of it. Built in, and always on:

| name | catches | becomes |
|---|---|---|
| `private-key` | a whole `BEGIN … PRIVATE KEY` block, through `END` | `[redacted]` |
| `aws-access-key` | `AKIA…` / `ASIA…` key ids | `[redacted]` |
| `aws-secret-key` | the value after `aws_secret_access_key =` | `aws_secret_access_key = [redacted]` |
| `api-key` | `sk-…` — Anthropic, OpenAI | `[redacted]` |
| `stripe-key` · `stripe-webhook` | `sk_live_`, `rk_test_`, `pk_live_` · `whsec_` | `[redacted]` |
| `github-token` · `github-pat` | `ghp_` and kin · `github_pat_` | `[redacted]` |
| `slack-token` | `xoxb-`, `xoxp-` and kin | `[redacted]` |
| `jwt` | `eyJ….eyJ….sig` | `[redacted]` |
| `url-password` | the password in `scheme://user:pass@host` | `postgres://admin:[redacted]@db` |
| `bearer` | the token after `Bearer` | `Bearer [redacted]` |
| `env-secret` | an UPPER_CASE name containing SECRET, TOKEN, PASSWORD or API_KEY, then `=` or `:` and 8+ characters | `DB_PASSWORD=[redacted]` |

Where a context word is matched, only the secret is replaced, so the report
still says what was connected to. Every pattern is tested against a look-alike
it must leave alone — `MAX_TOKENS=4096`, `tokens = tokenizer.encode(text)`.

`privacy.redact` **adds** patterns; it does not replace these. Name a group
`secret` to replace only that part: `(?P<secret>…)`. `daybook privacy` lists
everything that runs.

**This is a safety net, not a guarantee.** It catches known key shapes; it
cannot catch a password typed as prose. The words themselves are always kept:
reports are written from what was said, and the archive keeps all of it anyway.

## The transcript archive

**On by default, and not redacted.** Every session active in a day's window is
copied byte for byte into `transcripts/`, with the directory Claude Code keeps
beside it, and copied again whenever the source's bytes differ. That is the
whole conversation: every prompt, every reply, and every tool result — the
contents of files the agent read and the output of commands it ran, `.env`
files and credentials included if it read them.

It exists so the history outlives Claude Code, which deletes transcripts after
`cleanupPeriodDays` (30 unless changed), and so another system can read it from
one known place. Redaction is not applied because a redacted copy is no longer
the transcript, and pattern redaction over arbitrary tool output would miss
more than it caught.

What follows from that:

- Files are `0600`, directories `0700`, as with everything else here.
- **A synced `output.root` uploads the archive.** `daybook privacy` warns when
  the folder looks like it is inside a sync client; with the archive on, that
  warning covers every transcript, not only the report.
- It is never pruned. A session whose source Claude Code has deleted keeps its
  last copy. Removing files from `transcripts/` is yours to do.
- With the archive off, the index is still written, pointing at the source.

To stop new copies:

```sh
daybook config set sessions.archive false
```

## What is not collected

No account identifiers, no machine fingerprint beyond the hostname you can
override with `identity.machine`, no usage statistics, no crash reports.
