package render

// The page's styling.
//
// One report, read on whatever the reader has open, in whichever theme they
// keep their machine. Colours are declared once as tokens on :root and
// redefined for dark, so nothing is stated in a place that only applies to one
// of them — the classic way a generated page ends up with one theme's text on
// the other theme's background.
const htmlCSS = `
:root{
  --ground:#f6f7f5; --surface:#fff; --ink:#141715; --ink-2:#4a524d; --ink-3:#79817b;
  --rule:#dde1dc; --accent:#1d5b4c; --code-bg:#f1f3f0; --warn:#8a5d10;
}
@media (prefers-color-scheme:dark){:root{
  --ground:#111413; --surface:#171b19; --ink:#e4e7e4; --ink-2:#a8b0aa; --ink-3:#7c857f;
  --rule:#2a312d; --accent:#6fbfa4; --code-bg:#1b211e; --warn:#d8a64b;
}}
*{box-sizing:border-box}
body{margin:0;background:var(--ground);color:var(--ink);
  font:17px/1.6 "Source Serif 4",Georgia,serif;-webkit-font-smoothing:antialiased}
main{max-width:46rem;margin:0 auto;padding:0 1.4rem 5rem}
header{padding:3.5rem 0 1.5rem;border-bottom:1px solid var(--rule);margin-bottom:2rem}
h1{font:700 2.4rem/1.05 ui-sans-serif,system-ui,sans-serif;letter-spacing:-.03em;margin:.4rem 0 1.4rem}
h2{font:600 1.5rem/1.15 ui-sans-serif,system-ui,sans-serif;letter-spacing:-.02em;margin:2.6rem 0 1rem}
h3{font:600 1.05rem/1.35 ui-sans-serif,system-ui,sans-serif;margin:0 0 .6rem}
p{margin:0 0 .9rem}
.eyebrow{font:500 .72rem/1 ui-monospace,Menlo,monospace;letter-spacing:.12em;
  text-transform:uppercase;color:var(--accent);margin:0}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(6.5rem,1fr));gap:1px;
  background:var(--rule);border:1px solid var(--rule);border-radius:4px;overflow:hidden}
.stat{background:var(--surface);padding:.7rem .8rem}
.stat .v{font:700 1.25rem/1.1 ui-sans-serif,system-ui,sans-serif;font-variant-numeric:tabular-nums}
.stat .l{font:.66rem/1.3 ui-monospace,Menlo,monospace;letter-spacing:.08em;
  text-transform:uppercase;color:var(--ink-3);margin-top:.2rem}
.summary{color:var(--ink-2);border-left:3px solid var(--accent);padding-left:1rem;margin:0 0 1rem}
.cap{background:var(--surface);border:1px solid var(--rule);border-radius:5px;
  padding:1.1rem 1.2rem;margin:0 0 1.1rem}
.cap p{color:var(--ink-2);font-size:.97rem}
figure{margin:1rem 0 .6rem}
/* The picture is the point of this format, so it gets the room. */
img{width:100%;height:auto;display:block;border:1px solid var(--rule);border-radius:4px;background:var(--ground)}
figcaption{font-size:.85rem;color:var(--ink-3);margin-top:.5rem}
.src{display:block;font-family:ui-monospace,Menlo,monospace;font-size:.72rem;
  color:var(--ink-3);margin-top:.2rem;word-break:break-all}
code{font-family:ui-monospace,Menlo,monospace;font-size:.8rem;background:var(--code-bg);
  padding:.1em .35em;border-radius:3px}
.where{font-size:.85rem;line-height:2}
.commits{font-family:ui-monospace,Menlo,monospace;font-size:.72rem;color:var(--ink-3);
  word-break:break-all;margin:.6rem 0 0}
.branch{color:var(--accent)}
details{border:1px solid var(--rule);border-radius:5px;padding:.7rem 1rem;margin:0 0 1.1rem;
  background:var(--surface)}
summary{cursor:pointer;font:600 .95rem ui-sans-serif,system-ui,sans-serif}
summary:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.ledger{list-style:none;padding:0;margin:1rem 0 0;font-size:.92rem}
.ledger li{padding:.4rem 0;border-top:1px solid var(--rule);color:var(--ink-2)}
.age{font-family:ui-monospace,Menlo,monospace;font-size:.72rem;color:var(--ink-3);
  display:inline-block;min-width:3rem}
.stale .age{color:var(--warn);font-weight:600}
table{border-collapse:collapse;width:100%;font-size:.92rem;margin:0 0 1.2rem}
th{text-align:left;font:600 .68rem ui-sans-serif,system-ui,sans-serif;letter-spacing:.08em;
  text-transform:uppercase;color:var(--ink-3);padding:0 .8rem .5rem 0;border-bottom:1px solid var(--rule)}
td{padding:.5rem .8rem .5rem 0;border-bottom:1px solid var(--rule);color:var(--ink-2)}
td.n{text-align:right;font-variant-numeric:tabular-nums}
footer{margin-top:3rem;padding-top:1.4rem;border-top:1px solid var(--rule);
  font-size:.85rem;color:var(--ink-3)}
@media (prefers-reduced-motion:reduce){*{transition:none!important;animation:none!important}}
`
