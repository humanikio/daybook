# Changelog

Notes for a tag live under a `## vX.Y.Z` heading here — the release workflow
reads this file to build the release body. A tag with no matching section is
refused before anything is built, as is a tag over an unchanged tree.

## v0.3.1

### The scheduler picks up new code

An installed scheduler ran for twenty-five hours across three rebuilds and a
release, still executing the binary it was launched with. Config is re-read
every tick; code was not. It reported itself as running and up to date while
producing output from code that no longer existed on disk — a stale process in
its worst shape, invisible and confidently wrong.

`serve` now notices its own binary being replaced and exits. The service is
installed with KeepAlive, so leaving hands the next start back to the service
manager, which execs the new one. Checked before a run rather than after, so a
run never straddles two versions. A stamp that cannot be read counts as "could
not tell" and never as a change, because a false positive here is a restart
loop.

Upgrading still means installing the new binary. This makes the installation
take effect instead of waiting for a reboot.

### Screenshots on the nightly run

`preview.on_schedule`, off by default and asked for separately from
`preview.enabled`. The capture drives your real browser and acts as you while it
does. Kicking that off yourself is reasonable; having it seize the browser at
22:00 while you are using it is not, so it is opted into rather than inherited.

`daybook config edit` asks for it under Screenshots, phrased as what it does to
you rather than what it does for you, and the summary line now says whether
capture happens nightly or only when you run it.

Without this, a scheduled run produced a report with no pictures in it and no
indication why — the capture was never part of that path.

## v0.3.0

### Screenshots of what shipped

`daybook shoot` photographs the capabilities in a day's report and embeds them
in it. The agent navigates to each one rather than being handed a URL, because a
route like `/w/[workspaceId]/bulletin` is a pattern and not an address — the
workspace id only exists once something has signed in and looked.

A capability with no screen is skipped. A day whose consequential work was
backend produces fewer pictures than `max_photos` allows, and that is the
correct outcome: a picture of an adjacent screen is worse than no picture,
because it is wrong and it is persuasive.

The agent names which capability a picture is of **by number**, and the wording
is resolved from the list it was given. It previously reported the wording
itself and paraphrased it every time, so the report keyed pictures to
capabilities by string and matched none of them — every screenshot was taken,
filed, and then rendered nowhere.

### HTML reports

Written whenever a day has pictures, or when `output.formats` asks for it.
Self-contained: images inline as data URIs, no external requests, and the
viewer's light or dark theme is honoured.

### The capture agent owns the dev servers

daybook no longer starts or stops them. It passes the command, the directory and
the expected boot time, and the agent runs the lifecycle.

Owning that from here never worked. `next dev` spawns a child that escapes the
process group, so a teardown that printed "stopping" left the port held for
hours and the next run collided with it. The port recorded on an earlier day was
frequently not the port the app came up on, so the already-running check missed a
live server and tried to start a second copy. The agent is already in a shell,
can read the port the app announces, and is told to stop what it started, to
stop only what it started, and to kill the process group.

This grants `Bash` to the capture step, and only when `preview.start_servers` is
on — which is off by default. With it off the agent is told which apps it may
not start rather than being handed commands it cannot run.

### What broke now means what broke

Every tool result that came back as an error was reported under one heading, so
a day in which the agent mistyped some paths and a person declined some tool
calls was reported as the software failing 45 times. On the day that produced
that number, the work itself had broken once.

Error results are now classified where they are extracted, into the work
failing, a tool call declined, something unavailable, and the agent's own
malformed call. Only the first is reported as breakage. The rest are counted in
a single line, because how much friction a day carried is worth knowing and the
text of it is not worth reading. An unrecognised result is **not** promoted to
breakage.

The narrator is fed only real breaks. It previously received all of them with no
way to tell a declined tool call from a failing build.

### A renumbered hostname no longer splits your history

macOS appends `-2`, `-3`, `-4` to a machine's name when another machine on the
network claims it, and the number changes on its own. One laptop wrote eight days
under one name and then started writing another, becoming two machines in the
history.

The machine name is now taken from the name already present in `raw/`, when it
matches apart from that suffix. Nothing is renamed and no third name appears.
`identity.machine` still overrides, and only a one-to-three digit suffix is
treated as a collision — a machine genuinely called `build-box-2026` is left
alone.

### Also

- CI builds every platform the release builds. A build only ever tried on Linux
  hid `syscall.Kill` not existing on Windows until the release job compiled for
  it, by which point the tag was already pushed.
- `docs/plans.md` records intended work that is not built: clearing up
  unreferenced screenshots, and ranking capabilities that have something to
  photograph.
- Failure text is shown without its harness wrapper tags and cut at a word
  rather than mid-word.

## v0.2.0

### `daybook upgrade`

Checks whether a newer release exists and prints the one command that installs
it. It never replaces the binary — upgrading means re-running the installer,
which already knows where this platform puts things and how to verify a
signature.

A failed check reports **unknown** and exits non-zero, so a scripted call
cannot read silence as "up to date". A build from source always reports an
update: its version names what it was built from rather than what is in it, so
comparing it against a release tag answers a question the number cannot answer.

The install command it prints depends on where the running binary came from. A
binary from `go install` lives in `GOPATH/bin`, where the shell installer would
not replace it — it writes to `~/.local/bin`, leaving two copies on PATH and an
upgrade that appears not to have worked.

### Browser detection in `daybook verify`

daybook does not drive a browser. `verify` now reports whether Claude Code
**could** on this machine, ahead of a feature that will need it, because every
prerequisite fails silently and the capability is invisible by construction.

Four signals: the extension paired, the native-messaging handshake written and
naming the extension, a Chromium browser running, and `ANTHROPIC_API_KEY`
absent.

That last one is the surprise. Claude Code turns the browser off for API-key
auth — silently, even with the flag set — so nothing in the browser
configuration is wrong and it still does not work. The key is tracked by
*location*, because the remedy differs: "unset `ANTHROPIC_API_KEY`" is useless
advice to somebody whose shell has never had it set, when the value is in a
plist or a launchd session.

Two things it will not pretend to know. On Windows the manifest is a registry
key, so a path check would report "not set up" on every correctly configured
machine — it reports unknown. And pairing is per Claude Code *account*, not per
host, so a browser on another machine may be reachable while this check sees
nothing.

### Release process

A tag with no changelog section, an empty one, or an unchanged tree now **fails**
the release rather than publishing a generic body. The gates run before the
build, so a mistake costs ten seconds instead of six cross-compiles and a
signing pass. CI separately checks that this file is well formed on every push.

The procedure is written down in `docs/releasing.md`.

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
