# Privacy

daybook reads the most sensitive text on your machine: everything you have ever
typed to Claude Code, and everything it said back.

## Two rules

**1. Nothing phones home.** No telemetry, no sync, no analytics. daybook has no
server and reports nothing about you to anyone.

**The scan is entirely local.** Reading transcripts, joining them against git,
and writing the report involve no network at all. `watch.fetch: true` is the
only setting in that path that touches one, and it talks to your own git
remotes.

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
the deterministic report is complete without it. `privacy.keep_raw_prompts:
false` narrows it further: counts, commits and timings still work, and no words
are stored or sent.

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

## What is written

daybook uses **two** directories, and only one of them holds your words.

```
~/.daybook/                        CONFIG ONLY
  config.yaml                      settings — no prompt text

<output.root>/                     default ~/Desktop/daybook
  outputs/YYYY-MM-DD.md            report — prompt text, file paths, commits
  raw/YYYY-MM-DD.<machine>.json    the same, structured
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

Applied to every prompt and every assistant message before writing:

| pattern | catches |
|---|---|
| `AKIA[0-9A-Z]{16}` | AWS access key IDs |
| `sk-[A-Za-z0-9_-]{20,}` | bearer-style API keys |
| `gh[pousr]_[A-Za-z0-9]{20,}` | GitHub tokens |
| `-----BEGIN … PRIVATE KEY-----` | private keys |

Add your own under `privacy.redact`. Matches become `[redacted]`.

**This is a safety net, not a guarantee.** It catches known key shapes; it
cannot catch a password typed as prose. If that matters more to you than
fidelity:

```yaml
privacy:
  keep_raw_prompts: false
```

Timestamps, counts, tools, files, repos and commits are still recorded — you
keep the whole shape of the day, and none of the words.

## What is not collected

No account identifiers, no machine fingerprint beyond the hostname you can
override with `identity.machine`, no usage statistics, no crash reports.
