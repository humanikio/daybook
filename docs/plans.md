# Plans

Things daybook intends to do and does not do yet. Each entry says what the
current behaviour is, why it is not good enough, and what would replace it. An
entry leaves this file when it ships or when it is decided against.

## Clearing up old screenshots

**Now.** `daybook shoot` writes images into `<output_root>/assets/<date>/` and
never removes any of them. Nothing reads the directory to find out which files a
report still points at.

**Why it matters.** Names are derived from the capability text, so a capability
whose wording changes between runs leaves the previous image behind under the
old name. After a handful of runs the directory holds files no report
references. On 26 August the directory held five images, of which two were in
the report and three were left from earlier runs of the same day. Nothing breaks
and nobody notices, which is the problem: it grows quietly.

**Intended.** A reap that reads the day's `raw/<date>.<machine>.json`, collects
the files its shots name, and removes images in that date's asset directory
which no shot names. Scoped to one date at a time, because that is the only
scope where the set of referenced files is knowable without reading every report
ever written.

**Rules it has to follow.**

- Only inside `<output_root>/assets/<date>/`, never anywhere else.
- Only when the raw file for that date exists and parsed. No raw file means no
  knowledge of what is referenced, and deleting on no knowledge is wrong.
- Only image extensions daybook itself writes.
- Off by default, or behind an explicit `daybook reap` the user runs, because
  deleting a marketing screenshot somebody was about to use is worse than
  leaving disk space occupied.

**Not decided.** Whether it belongs on `shoot` as a final step or as its own
command; whether a retention window (keep N days) is wanted as well.

## Choosing capabilities that have something to photograph

**Now.** `preview.max_photos` caps how many capabilities are offered to the
capture agent, and they are chosen by consequence alone. On a day where the
consequential work was backend, most of the candidates have no screen and the
agent correctly declines to photograph them.

**Why it is not urgent.** `max_photos` is a maximum. Fewer pictures than the cap
is a correct outcome when fewer things had a user interface, and a picture of an
adjacent screen would be worse than no picture.

**Intended, if it is ever wanted.** Rank candidates that touch a frontend path
above those that do not, using the paths already recorded on each shipped item,
so the offered list spends its places on work that can be photographed. This
changes which capabilities are offered, never how many pictures are taken.
