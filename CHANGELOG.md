# Changelog

Notes for a tag live under a `## vX.Y.Z` heading here — the release workflow
reads this file to build the release body. A tag with no matching section still
publishes, with a fallback body and a warning in the job log.

## v0.1.1

Documentation only. No code changed; the binaries differ from v0.1.0 by the
version string the release injects with `-ldflags -X main.version` and nothing
else.

The README now leads with the report rather than with an install command. The
part worth showing is one capability entry: plain language first, then the
mechanism for whoever maintains it, then the files and the branch. No amount
of prose about that format lands the way one example does.

Two figures in it were also corrected. "40% exact attribution" had been
measured under a different repo set and a broken author filter and had quietly
stopped being true; measured again across eight real days it is **55% overall,
ranging 28-67% by day**. A range is the honest shape anyway — a single number
implies a stability this does not have. Repo count and scan time were stale
too, and both were re-measured.

## v0.1.0

First release.

daybook reads your Claude Code sessions and your git history and writes a daily
report of what you actually worked on, joined against what of it shipped.

**What it does**

- Finds every session that received a prompt in the last 24 hours, and reads
  both sides of the conversation — in a typical window 73% of the text is the
  assistant's, and that is where "found three bugs by running it" lives.
- Attributes commits to streams in two tiers and always prints the split:
  *exact* (the stream touched a file the commit changed) and *inferred* (same
  repo, and the stream was live nearest the commit). Around 40% lands exact on
  real data. Anything unclaimable is listed rather than guessed.
- Tracks four ship states, because *done / not done* cannot express the one that
  matters: `shipped`, `local` (committed, never pushed — at risk), `open`, and
  `stale`.
- Optional prose summaries via the `claude` you are already signed in with, or
  the Anthropic API. Every sha and path in the model's output must appear in its
  input or the narration is discarded.
- A running ledger of work that has not finished proving itself, closed only
  against citable evidence.
- Runs on a schedule as a per-user LaunchAgent, systemd user service, or Windows
  logon task. A run is owed to a *slot* rather than a moment, so a laptop asleep
  at 23:30 gets its report on wake instead of skipping the day.

**Privacy.** Nothing leaves your machine. Redaction runs before anything is
written. Output is `0600`.

**Known limitation.** This parses `~/.claude/projects/*.jsonl`, which is
internal to Claude Code and carries no compatibility guarantee. Parsing fails
soft per line rather than per file, and `daybook verify` reports the
parse-failure rate. It will break eventually — please open an issue.
