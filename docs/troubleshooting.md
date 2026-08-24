# Troubleshooting

Start with `daybook verify`. It prints the parsed config with types, every
source, repo discovery, and when the last scan ran.

## "no report for YYYY-MM-DD"

`daybook day` reads a file `daybook scan` writes. Run `scan` first. Reports are
only written for days you have scanned — it does not reach back in time on its
own.

## Transcript lines could not be parsed

The report ends with a line counting them.

`~/.claude/projects/*.jsonl` is **internal to Claude Code and undocumented**.
One reference corpus spanned 27 CLI versions. Parsing is defensive and fails
soft per line rather than per file, so a format change degrades the report
instead of breaking the run — but a rising count means something moved. Please
open an issue with your `claude --version`.

A handful of errors is usually a truncated final line in a session that is still
being written. That is harmless.

## No streams found

A session is discovered when it received a **human prompt inside the window**.
Sessions where you only read output, or which ran unattended, will not appear.

- Check the window: `daybook verify` prints it. Widen with `--window 48h`.
- Check the path: `verify` prints how many transcripts it can see.
- Files untouched since before the window are skipped for speed. If your clock
  or filesystem mtimes are wrong, this will hide sessions.

## No commits attributed

The stream shows `No commits attributed` even though you shipped.

1. **Check the author filter.** `verify` prints it. If your commits are authored
   under a different email than `identity.authors`, none of them count. This is
   the most common cause.
2. **Check repo discovery.** `verify` prints the count. If a repo is deeper than
   `watch.repos[].depth`, it is invisible.
3. **Check the window.** Commits are matched inside it, and work committed the
   morning after a late session falls into the next day's report.

## Attribution looks wrong

Every stream prints `Shipped (N exact, M inferred)`.

- **exact** — the stream touched a file the commit changed. Provable.
- **inferred** — same repo, same window, and the stream was live nearest to the
  commit time. Plausible, not proven.

With several streams in one repository at once, inferred attribution is a
judgement call and it will sometimes place a commit on the neighbouring stream.
This is why the split is always printed rather than one clean number. On real
data roughly 40% lands in the exact tier.

Once attributed, a commit is **pinned** in `state/pins.json` and never
re-judged, so reports stay stable. Delete that file to re-attribute everything
from scratch.

## Stream titles are wrong

Titles come from Claude and describe **where a session started**, not what it is
doing now. A session opened for one task and kept open for a week keeps its
original name. Narration reads what actually happened rather than trusting the
title.

## A repo shows unpushed commits I know I pushed

The unpushed count compares against the local remote-tracking ref, which `git
push` updates as part of pushing. It is therefore accurate for anything pushed
**from this machine**. If you pushed from somewhere else, set:

```yaml
watch:
  fetch: true
```

That costs a network round trip per repository and can prompt for credentials,
which is why it is off by default.

## Scans are slow

Expect about five seconds over a large corpus. If it is much worse:

- **`watch.fetch: true`** is the usual culprit — a round trip per repo.
- **Repo roots too broad.** Reduce `depth`, or add `watch.ignore` globs.
- **Network filesystems.** Both transcripts and repos are read from disk;
  neither should live on a network mount.

## Nothing works and the output makes no sense

```sh
daybook verify --config /path/to/config.yaml
```

The parsed config is printed with resolved values. Look for a setting that
became `false` or a number when you wrote a word — an unquoted `no` or `NO` is
`false` under YAML 1.1, and an unquoted `12:00` is `43200`. Quote every string.
