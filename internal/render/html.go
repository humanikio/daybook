package render

import (
	"encoding/base64"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// The HTML report.
//
// Markdown stays the default: it renders in a terminal, an editor, a pull
// request and a chat message, and it diffs cleanly between days. This exists
// for the one thing markdown cannot do — carry the screenshots inside the file.
//
// SELF-CONTAINED, deliberately. Images are inlined as data URIs so the report
// is one file somebody can send, open on another machine, or keep after the
// assets directory has moved. A report that breaks when it is copied is not a
// report you can hand to anyone.

// HTML renders a day as a single self-contained page.
func HTML(d model.Day, cfg config.Config, assetsDir string) string {
	var b strings.Builder
	t := d.Totals

	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	fmt.Fprintf(&b, "<title>%s</title>", html.EscapeString(d.WindowEnd.Format("Monday 2 January 2006")))
	b.WriteString("<style>" + htmlCSS + "</style></head><body><main>")

	fmt.Fprintf(&b, `<header><p class="eyebrow">%s → %s · %s</p><h1>%s</h1>`,
		html.EscapeString(d.WindowStart.Format("Mon 2 Jan 15:04")),
		html.EscapeString(d.WindowEnd.Format("Mon 2 Jan 15:04")),
		html.EscapeString(d.Machine),
		html.EscapeString(d.WindowEnd.Format("Monday 2 January 2006")))

	b.WriteString(`<div class="stats">`)
	stat(&b, dur(t.ActiveMinutes), "active")
	stat(&b, itoa(t.Streams), "streams")
	stat(&b, itoa(t.Prompts), "prompts")
	stat(&b, itoa(t.Commits), "commits")
	stat(&b, "+"+comma(t.Added), "added")
	stat(&b, "−"+comma(t.Deleted), "removed")
	b.WriteString(`</div></header>`)

	if n := d.Narration; n != nil {
		b.WriteString(`<section class="summary">`)
		para(&b, n.Shape)
		if n.Moved != "" {
			fmt.Fprintf(&b, `<p><strong>Moved.</strong> %s</p>`, html.EscapeString(n.Moved))
		}
		if n.Carrying != "" {
			fmt.Fprintf(&b, `<p><strong>Carrying.</strong> %s</p>`, html.EscapeString(n.Carrying))
		}
		b.WriteString(`</section>`)
	}

	shots := map[string]model.Shot{}
	for _, sh := range d.Shots {
		shots[sh.Capability] = sh
	}

	if len(d.Shipped) > 0 {
		b.WriteString(`<h2>What shipped</h2>`)
		for _, it := range d.Shipped {
			if !it.Internal {
				card(&b, it, shots[it.What], assetsDir)
			}
		}
		var internal []model.ShippedItem
		for _, it := range d.Shipped {
			if it.Internal {
				internal = append(internal, it)
			}
		}
		if len(internal) > 0 {
			fmt.Fprintf(&b, `<details><summary>Internal · %d items with no user-facing surface</summary>`, len(internal))
			for _, it := range internal {
				card(&b, it, model.Shot{}, assetsDir)
			}
			b.WriteString(`</details>`)
		}
	}

	// The long lists are collapsed rather than cut. Over a 48-hour window these
	// run to dozens of entries — long enough to be skipped if they are open,
	// and too useful to drop.
	if open := d.OpenItems; len(open) > 0 {
		fmt.Fprintf(&b, `<details><summary>Still open · %d, oldest first</summary><ul class="ledger">`, len(open))
		for _, it := range open {
			age := it.Age(d.WindowEnd)
			cls := ""
			if age >= 14 {
				cls = ` class="stale"`
			}
			fmt.Fprintf(&b, `<li%s><span class="age">%dd</span> %s</li>`, cls, age, html.EscapeString(it.Text))
		}
		b.WriteString(`</ul></details>`)
	}

	var risky []model.RepoState
	for _, r := range d.Repos {
		if r.Ahead > 0 || r.Dirty > 0 {
			risky = append(risky, r)
		}
	}
	if len(risky) > 0 {
		b.WriteString(`<h2>Still on this machine</h2><table><tr><th>repo</th><th>branch</th><th>unpushed</th><th>uncommitted</th></tr>`)
		for _, r := range risky {
			br := r.Branch
			if br == "" {
				br = "detached"
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td class="n">%d</td><td class="n">%d</td></tr>`,
				html.EscapeString(r.Repo), html.EscapeString(br), r.Ahead, r.Dirty)
		}
		b.WriteString(`</table>`)
	}

	fmt.Fprintf(&b, `<footer>Generated %s. Markdown alongside this file has the full detail.</footer>`,
		html.EscapeString(d.Generated.Format("2 Jan 15:04")))
	b.WriteString(`</main></body></html>`)
	return b.String()
}

func card(b *strings.Builder, it model.ShippedItem, shot model.Shot, assetsDir string) {
	b.WriteString(`<article class="cap">`)
	fmt.Fprintf(b, `<h3>%s</h3>`, html.EscapeString(it.What))
	if it.How != "" {
		para(b, it.How)
	}
	if shot.File != "" {
		if src := dataURI(filepath.Join(assetsDir, shot.File)); src != "" {
			fmt.Fprintf(b, `<figure><img src="%s" alt="%s" loading="lazy">`, src, html.EscapeString(it.What))
			if shot.Note != "" {
				fmt.Fprintf(b, `<figcaption>%s <span class="src">%s</span></figcaption>`,
					html.EscapeString(shot.Note), html.EscapeString(shot.URL))
			}
			b.WriteString(`</figure>`)
		}
	}
	if len(it.Where) > 0 {
		b.WriteString(`<p class="where">`)
		for i, w := range it.Where {
			if i > 0 {
				b.WriteString(" · ")
			}
			fmt.Fprintf(b, `<code>%s</code>`, html.EscapeString(w))
		}
		b.WriteString(`</p>`)
	}
	if len(it.Commits) > 0 {
		br := it.Branch
		if br == "" {
			br = "?"
		}
		fmt.Fprintf(b, `<p class="commits">%s <span class="branch">on %s</span></p>`,
			html.EscapeString(strings.Join(it.Commits, " · ")), html.EscapeString(br))
	}
	b.WriteString(`</article>`)
}

// dataURI inlines an image. Returns "" when it cannot be read, so a missing
// file leaves a card without a picture rather than a broken one.
func dataURI(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	mime := "image/png"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

func para(b *strings.Builder, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	fmt.Fprintf(b, "<p>%s</p>", html.EscapeString(s))
}

func stat(b *strings.Builder, v, label string) {
	fmt.Fprintf(b, `<div class="stat"><div class="v">%s</div><div class="l">%s</div></div>`,
		html.EscapeString(v), html.EscapeString(label))
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
