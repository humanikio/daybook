# The record

`daybook scan` writes two files per day and keeps a little state.

```
<output.root>/
  outputs/2026-08-24.md              the report you read
  raw/2026-08-24.<machine>.json      the facts it was rendered from
  state/pins.json                    commit → stream, once decided, forever
  state/last-run.json                drives catch-up when scheduling lands
```

**The JSON is the source of truth; the markdown is a view.** Facts are written
first and the prose is derived from them, never the other way round. That is
what makes it safe to change the report format: re-run `scan` over any past day
and the markdown rebuilds. There is no migration step because nothing derived is
ever authoritative.

`raw/` is namespaced by machine so two machines writing into one synced
directory never collide on the same file.

## Schema

`schema: 1`. Types live in `internal/model` and nothing else defines a field.

Notable fields:

- `streams[].confidence` on each commit — `exact`, `repo`, or `none`. Permanent,
  not scaffolding: attribution is partly inference and the record says which
  parts.
- `streams[].agent` — the session was driven by something other than a person at
  a keyboard (`entrypoint: sdk-cli`, or a non-human origin). Recorded, reported
  separately, excluded from your totals.
- `parseErrors` — transcript lines that could not be read. Expected to become
  non-zero as Claude Code's format moves; a silent zero would be the real bug.
- `repos[]` — per-repository `ahead` and `dirty` counts, which is how work that
  never left the machine becomes visible.
