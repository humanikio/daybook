# Privacy

daybook reads the most sensitive text on your machine: everything you have ever
typed to Claude Code, and everything it said back.

## Two rules

**1. Nothing leaves your machine.** No telemetry, no sync, no analytics, no
network calls of any kind. `watch.fetch: true` is the only setting that touches
a network, and it talks to your own git remotes.

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
