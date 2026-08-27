# Screenshots

daybook can photograph where a new feature lives in your product, and put the
pictures in the report. A teammate reading "you can now test a transform against
the payloads that actually arrived" then also sees the screen it happens on.

This is off by default. Read the whole page before turning it on: it drives your
real browser, as you, and it sends more to Anthropic than narration does.

## Turning it on

```sh
daybook config edit        # arrow to Screenshots
```

Or by hand:

```yaml
preview:
  enabled: true
watch:
  repos:
    - { path: "~/work", depth: 4, preview: true }
```

**Two gates, and neither alone does anything.** `preview.enabled` is the master
switch. `preview: true` on a watched folder says which code you are willing to
have photographed. A switch that appears to be on while nothing happens is the
worst outcome here, so both are asked for separately and `daybook config edit`
tells you when one is set and the other is not.

Then run it:

```sh
daybook shoot              # today
daybook shoot 2026-08-26
daybook shoot --dry-run    # show what it would do and the exact prompt, drive nothing
```

`shoot` runs after `narrate`, because the capability list it illustrates is what
narration produces. Running it on a day with no narration tells you so.

## Every setting

| key | default | what it does |
|---|---|---|
| `preview.enabled` | `false` | the master switch |
| `preview.max_photos` | `6` | most pictures in one run, across every repo |
| `preview.per_capability` | `1` | so one busy feature cannot take the lot |
| `preview.start_servers` | `false` | may the agent start an app that is not running |
| `preview.on_schedule` | `false` | photograph during the nightly run too |
| `preview.repos` | *(absent)* | narrow to particular repositories by name |
| `preview.timeout` | `20m` | one whole capture session |
| `output.formats` | `[]` | add `html` to write an HTML report every day |

### `max_photos` is a maximum, not a target

Fewer pictures than the cap is usually the correct answer. Most shipped work has
no screen: a queue that no longer double-sweeps, a drip that holds its interval,
a tier of permissions that no longer exists. There is nothing to point a camera
at, and the agent is told to skip rather than photograph an adjacent screen.

A run that offers eight capabilities and returns two has not failed. A run that
returns eight pictures for eight capabilities, six of which were backend, has.

### `preview.repos` — which repositories

The folder gate is matched by a **path prefix**. If you watch one folder that
holds twenty repositories, `preview: true` on that folder opts in all twenty.

```yaml
preview:
  repos: ["web", "docs"]
```

Absent or empty means every repository under a folder marked `preview: true`,
which is what the folder gate already meant — so adding this key changes nothing
until you list something.

`daybook config edit` lists the repositories it actually found before asking,
because naming twenty from memory is not something anyone can do, and a name
typed wrong would otherwise fail by simply never matching. A name that matches
nothing is reported rather than accepted. A run that skips a repository says so.

### `start_servers` — running your code

Off by default, and the line it crosses is a real one.

Using a server you already have running carries no risk: something is listening,
the agent visits it. Starting one means **running your project's code
unattended** — whatever `pnpm dev` does in that repo, including any setup script
it triggers.

With it on, daybook hands the agent the exact start commands it found in your
own transcripts, and the agent starts them, reads the port each app announces,
and stops what it started when it finishes. It is told to stop only what it
started, and to kill the whole process group, because dev servers spawn children
that outlive their parent and keep holding the port.

daybook does not start or stop servers itself. It used to, and was bad at it.

### `on_schedule` — during the nightly run

Off by default, and asked for separately from `preview.enabled`, because the
question is not "is this risky" but "may it take your browser at 22:00".

The capture drives your real browser and acts as you for the duration. Kicking
that off yourself is reasonable. Having it seize the browser mid-sentence while
you are working is not, so it is opted into rather than inherited.

With it off, the nightly report is written without pictures and `daybook shoot`
adds them whenever you run it.

## Where the pictures go

```
<output_root>/assets/<date>/<capability-name>.jpg
<output_root>/outputs/<date>.html
```

The markdown links to the files. The HTML embeds them as data URIs, so the page
is one self-contained file you can send to somebody.

**HTML is written automatically on any day that has pictures**, and not
otherwise. That is how a report ends up with today's markdown beside yesterday's
HTML — a capture run writes both, the next scheduled run has no pictures, writes
markdown only, and leaves the old HTML sitting there. Both files are correct and
they disagree. `output.formats: ["html"]` writes both every day and makes that
impossible.

Nothing removes old images yet. See [plans](plans.md).

## What the agent actually does

It **navigates**. It is not handed a URL.

That is the whole design. A route like `/w/[workspaceId]/bulletin` is a pattern,
not an address — the workspace id only exists once something has signed in and
looked. So the agent opens the app, finds the feature the way a person would,
and reports the address it ended up on. That address appears under the picture
in the report, which is how a reader gets to the same screen.

It reports which capability a picture is of **by number**, and daybook resolves
the wording from the list it was given. It used to report the wording itself and
paraphrased it every time, so pictures were taken, filed, and matched to nothing.

Two rules it is held to:

- **A missing picture is fine. A wrong one is not** — it is wrong and it is
  persuasive. Skipping is the correct answer when the feature has no screen.
- **It reports paths; daybook files the images.** The agent has no filesystem
  tools. What it says it captured is a claim, checked against what is on disk.

## What this sends, and where

Turning this on has two costs that narration does not.

**It sends to Anthropic.** The capability list, and the agent's navigation, go
through the same `claude` you are signed in with. Same account, same agreement,
no credential held by daybook — but it is not local.

**It drives your real browser, signed in as you.** Whatever that browser can
reach, the agent can reach: your production admin, your customers' records, your
email if it is open in a tab. The pictures it takes are of **real data**, and
they land in your reports folder as ordinary image files.

Those images are not redacted. Redaction runs over text before it reaches disk;
it cannot run over a photograph of a screen. If your dev environment holds real
customer names, they are in the pictures.

Two practical consequences:

- Keep the reports folder private. It already held your prompt history; it now
  holds screenshots of your product with whatever was on screen.
- Point it at a development environment where you can. `preview.repos` is how
  you keep it away from the repositories you would rather it did not open.

With `start_servers: true` it also runs your project's code unattended. With
`on_schedule: true` it does all of the above at 22:00 without you present.

See [privacy](privacy.md) for the full picture and
[browser detection](browser.md) for what has to be true of the machine.

## When nothing gets photographed

`daybook shoot --dry-run` prints what it would do, including the exact prompt the
agent receives. Start there.

| it says | what it means |
|---|---|
| screenshots are off | `preview.enabled` is false |
| no server was recorded in a folder marked for screenshots | daybook has never seen you start a dev server in a `preview: true` folder — it reads the command out of your transcripts rather than guessing |
| nothing that shipped runs in a folder marked for screenshots | the day's work and the running apps are different products |
| every repo with a recorded server is excluded by `preview.repos` | the allowlist is too narrow |
| has no capability list to illustrate | run `daybook narrate <date>` first |
| nothing worth photographing was reachable | the agent looked and declined — usually correct, and usually means the day's work was backend |

daybook learns how to start an app by reading the command out of your own
sessions, catalogued over the last thirty days rather than the report window,
because how an app starts is durable knowledge. If you have never started it in
a terminal Claude Code could see, there is nothing to record.
