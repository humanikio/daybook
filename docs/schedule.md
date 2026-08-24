# Running it automatically

```sh
daybook service install
daybook service status
```

`daybook init` offers this as its last step. Declining leaves a working tool —
`daybook scan` by hand is a complete workflow.

## Configuring when

```yaml
schedule:
  at: "23:30"          # quoted. bare 23:30 is the integer 84600 under YAML 1.1
  days: []             # empty = every day; e.g. ["mon","tue","wed","thu","fri"]
  catch_up: true
```

Config is **re-read on every tick**, so changing `at` takes effect without
restarting the service. "I changed it and it didn't take" is a bad first bug.

## Slots, and why the laptop case works

A run is owed to a **slot** — a scheduled time that has passed — not to a
moment. Asking *"is it 23:30 right now"* fails the instant the machine is asleep
at 23:30, which for a laptop is the normal case, not the edge.

Instead the scheduler asks *"is there a slot I have not served yet"*. The last
served slot lives in `state/last-run.json`. That gets catch-up for free, needs
no wake-up notifications from the OS, and survives clock jumps.

| `catch_up` | behaviour |
|---|---|
| `true` (default) | any unserved slot is due, however long ago. Close the lid at 23:00, open it at 09:00, and yesterday's report is waiting. |
| `false` | only a slot from the last hour is due. Miss it and the day is skipped — right for an always-on machine where a stale report is worse than none. |

A failed run **does not** record its slot, so the next tick retries. That covers
transient failures like a locked git index without any retry logic.

## Always as you, never as root

The service registers as a **user** agent on every platform:

| OS | mechanism |
|---|---|
| macOS | LaunchAgent in `~/Library/LaunchAgents` |
| Linux | systemd `--user` service |
| Windows | logon task via `schtasks` |

This is not a preference. A root or LocalSystem service has a different `HOME`,
a different keychain, and a different git config — so it cannot read your
transcripts, cannot read the `claude` login narration needs, and cannot resolve
the author identity it filters commits on. It would install cleanly, start
cleanly, and **produce an empty report forever**, which is the worst kind of
failure because it looks like success.

`daybook verify` and `daybook service status` both report a system-level
registration as an error if one exists.

**Windows is a logon task rather than a service** for exactly this reason. SCM
services run as LocalSystem, and kardianos's `UserService` option is honoured
for launchd and systemd only — on Windows it is silently ignored, so the flag
alone would look like a fix while changing nothing.

**Linux headless boxes** need one extra command, or a `--user` service stops at
logout and does not start at boot:

```sh
sudo loginctl enable-linger $USER
```

## What a scheduled run does

1. `scan` — read the window, join against git, write `outputs/` and `raw/`
2. `narrate` — only if `narrate.enabled: true`
3. record the slot

Narration failing does **not** un-serve the slot. The report is already on disk,
and re-running the whole scan tomorrow would not help.

## Weekly rollups

```sh
daybook week              # the week containing today
daybook week 2026-08-17
```

Reads the daily reports and aggregates them — no second pass over transcripts.
A week that disagreed with its own days would be a bug with nowhere to look.

It prints a per-day table rather than only a total, because the shape of a week
is what a total hides: commits per hour swings more than tenfold between a
design day and a build day, and one number reports neither.

## Logs

macOS: `~/Library/Logs/daybook.*.log`. Elsewhere: `~/.daybook/`.

```sh
daybook serve       # foreground, same loop, logs to the terminal
```
