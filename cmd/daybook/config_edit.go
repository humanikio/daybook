package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/narrate"
	"github.com/humanikio/daybook/internal/tui"
	"github.com/humanikio/daybook/internal/vcs"
)

// `daybook config edit` — the settings people actually revisit.
//
// `config set` needs you to know the key name; `init` re-asks everything and
// overwrites answers you were happy with. This is the middle: see what is set,
// arrow to the one that is wrong, change it, leave.
//
// Every field writes through config.Save and re-validates, so an edit that
// would produce an unloadable file is refused here rather than at next start.
func cmdConfigEdit(args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	if !tui.Interactive() {
		return fmt.Errorf("`config edit` needs a terminal — use `daybook config set <key> <value>`")
	}

	colour := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "" && os.Getenv("TERM") != "dumb"
	fmt.Println()

	for {
		items := []tui.Item{
			{Label: "Watching", Value: describeRoots(cfg)},
			{Label: "Commit email", Value: orNone(strings.Join(cfg.Identity.Authors, ", "))},
			{Label: "Runs at", Value: describeSchedule(cfg)},
			{Label: "Catch up", Value: yesNo(cfg.Schedule.CatchUp)},
			{Label: "Reports in", Value: cfg.Output.Root},
			{Label: "Formats", Value: describeFormats(cfg)},
			{Label: "Narration", Value: describeNarration(cfg)},
			{Label: "Window", Value: cfg.Window.Length},
			{Label: "Keep prompts", Value: yesNo(cfg.Privacy.KeepRawPrompts)},
			{Label: "Screenshots", Value: describePreview(cfg)},
		}

		i := tui.Select("daybook settings", items, colour)
		if i < 0 {
			fmt.Println("\nsaved.")
			return nil
		}
		fmt.Println()

		switch i {
		case 0:
			// A list, not a scalar. Point at the verbs that manage it rather
			// than asking someone to hand-edit a comma-separated path list.
			fmt.Println("  add:     daybook watch <path>")
			fmt.Println("  remove:  daybook unwatch <path>")
			fmt.Println()
			continue
		case 1:
			cfg.Identity.Authors = splitCSV(tui.Prompt("commit email(s), comma separated",
				strings.Join(cfg.Identity.Authors, ",")))
		case 2:
			v := tui.Prompt("time (HH:MM)", cfg.Schedule.At)
			cfg.Schedule.At = v
			d := tui.Prompt("days (mon,wed,fri — or 'every')", daysOrEvery(cfg))
			if strings.EqualFold(d, "every") || strings.EqualFold(d, "all") {
				cfg.Schedule.Days = nil
			} else {
				cfg.Schedule.Days = splitCSV(strings.ToLower(d))
			}
		case 3:
			cfg.Schedule.CatchUp = !cfg.Schedule.CatchUp
		case 4:
			cfg.Output.Root = tui.Prompt("reports folder", cfg.Output.Root)
			if err := os.MkdirAll(config.Expand(cfg.Output.Root), 0o700); err != nil {
				fmt.Printf("  ! could not create it: %v\n\n", err)
				continue
			}
		case 5:
			editFormats(&cfg)
		case 6:
			chooseNarration(&cfg, colour)
		case 7:
			cfg.Window.Length = tui.Prompt("how far back each run looks", cfg.Window.Length)
		case 8:
			cfg.Privacy.KeepRawPrompts = !cfg.Privacy.KeepRawPrompts
			if !cfg.Privacy.KeepRawPrompts {
				fmt.Println("  prompt text will no longer be stored — counts and commits still are")
			}
		case 9:
			editPreview(&cfg)
		}

		if err := cfg.Validate(); err != nil {
			// Refuse and revert by reloading, so a bad answer cannot leave the
			// file in a state that will not load next time.
			fmt.Printf("  ! %v\n  (not saved)\n\n", err)
			cfg, _ = config.Load(cfg.Path())
			continue
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Println()
	}
}

// chooseNarration is a menu rather than a free-text field: "auto|cli|api|off"
// is a thing you have to already know, and the difference that matters is
// which account pays.
func chooseNarration(cfg *config.Config, colour bool) {
	opts := []tui.Item{
		{Label: "Claude Code", Value: "uses the login on this machine · spends your Claude quota"},
		{Label: "Anthropic API", Value: "needs an API key · about $1/day · leaves your quota alone"},
		{Label: "Off", Value: "the report is complete without it"},
	}
	switch tui.Select("How should prose summaries be written?", opts, colour) {
	case 0:
		cfg.Narrate.Enabled, cfg.Narrate.Provider = true, "cli"
	case 1:
		cfg.Narrate.Enabled, cfg.Narrate.Provider = true, "api"
	case 2:
		cfg.Narrate.Enabled = false
	default:
		return
	}
	fmt.Println()
	// Report the real state, not the setting. Turning narration on does not
	// make it work, and the gap between the two is a sign-in.
	if cfg.Narrate.Enabled {
		if _, err := narrate.Resolve(*cfg); err != nil {
			fmt.Printf("  ! %v\n", err)
		} else {
			fmt.Println("  ✓ ready — every run will narrate")
		}
	} else {
		fmt.Println("  narration off")
	}
	fmt.Println()
}

// editPreview walks the master switch and the caps. Which FOLDERS are
// photographed stays with `daybook watch`, where the rest of that root's
// settings live — splitting it across two screens would be worse than the extra
// line here saying so.
func editPreview(cfg *config.Config) {
	fmt.Println("  Screenshots capture where a new feature lives in the UI, so somebody")
	fmt.Println("  who was not there can find it. They drive your real browser, as you.")
	fmt.Println()

	on := strings.ToLower(tui.Prompt("enable screenshots? (y/n)", yesNo(cfg.Preview.Enabled)))
	cfg.Preview.Enabled = on == "y" || on == "yes"
	if !cfg.Preview.Enabled {
		fmt.Println("  off")
		return
	}
	if n, err := strconv.Atoi(tui.Prompt("most photos in one run", strconv.Itoa(cfg.Preview.MaxPhotos))); err == nil {
		cfg.Preview.MaxPhotos = n
	}
	start := strings.ToLower(tui.Prompt("start apps that are not already running? (y/n)", yesNo(cfg.Preview.StartServers)))
	cfg.Preview.StartServers = start == "y" || start == "yes"

	// Asked separately from enabling, and phrased as what it does to you rather
	// than what it does for you. Running it yourself is a choice; having it take
	// the browser at 22:00 is something you should have to say yes to.
	fmt.Println()
	fmt.Println("  On a schedule, the capture takes over your browser at the run time.")
	sched := strings.ToLower(tui.Prompt("photograph on the nightly run too? (y/n)", yesNo(cfg.Preview.OnSchedule)))
	cfg.Preview.OnSchedule = sched == "y" || sched == "yes"

	pickPreviewRepos(cfg)

	// The second gate. Enabled alone does nothing, and a switch that appears to
	// be on while nothing happens is the worst outcome here.
	var on_ []string
	for _, r := range cfg.Watch.Repos {
		if r.Preview {
			on_ = append(on_, r.Path)
		}
	}
	fmt.Println()
	if len(on_) == 0 {
		fmt.Println("  ! no folder has opted in yet, so nothing will be photographed.")
		fmt.Println("    add one:  daybook watch <path> --preview")
	} else {
		fmt.Printf("  photographing: %s\n", strings.Join(on_, ", "))
	}
	fmt.Println()
}

// pickPreviewRepos narrows the capture to particular repositories.
//
// The folder gate is a path prefix, so watching one umbrella opts in every
// repository under it. Listing what is actually there is the point: naming
// twenty-three repositories from memory is not something anyone can do, and a
// name typed wrong fails silently by simply never matching.
func pickPreviewRepos(cfg *config.Config) {
	found := vcs.Discover(*cfg)
	var names []string
	for _, r := range found {
		if cfg.PreviewCovers(r.Root) {
			names = appendOnce(names, r.Name)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)

	fmt.Println()
	fmt.Printf("  %s under the folders you photograph:\n", plural(len(names), "repo"))
	fmt.Printf("    %s\n", strings.Join(names, ", "))
	fmt.Println("  Leave blank for all of them, or list the ones you mean.")

	cur := strings.Join(cfg.Preview.Repos, ", ")
	in := strings.TrimSpace(tui.Prompt("photograph which repos", cur))
	if in == "" {
		cfg.Preview.Repos = nil
		return
	}

	var picked, unknown []string
	for _, raw := range strings.Split(in, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		match := ""
		for _, n := range names {
			if strings.EqualFold(n, name) {
				match = n
				break
			}
		}
		if match == "" {
			unknown = append(unknown, name)
			continue
		}
		picked = appendOnce(picked, match)
	}
	// A name that matches nothing would leave the capture quietly doing less
	// than asked, which is the failure this whole gate exists to make visible.
	for _, u := range unknown {
		fmt.Printf("  ! no repo called %q under those folders — ignoring it\n", u)
	}
	cfg.Preview.Repos = picked
	if len(picked) == 0 {
		fmt.Println("  nothing matched, so all of them")
	}
}

// editFormats decides whether an HTML report is written every time.
//
// Markdown is not offered as a choice because it is not one: it renders in a
// terminal, an editor, a pull request and a chat message, and it diffs between
// days. HTML is written anyway whenever there are screenshots, since markdown
// cannot carry them — which is how a report can end up with today's markdown
// beside yesterday's HTML, each correct and the two disagreeing.
func editFormats(cfg *config.Config) {
	fmt.Println("  Markdown is always written. HTML carries screenshots and charts,")
	fmt.Println("  and is written automatically on any day that has pictures.")
	fmt.Println()

	on := strings.ToLower(tui.Prompt("write HTML every day as well? (y/n)", yesNo(wantsHTML(*cfg))))
	if on == "y" || on == "yes" {
		if !wantsHTML(*cfg) {
			cfg.Output.Formats = append(cfg.Output.Formats, "html")
		}
		fmt.Println("  markdown and HTML, every day")
		return
	}
	var keep []string
	for _, f := range cfg.Output.Formats {
		if !strings.EqualFold(f, "html") {
			keep = appendOnce(keep, f)
		}
	}
	cfg.Output.Formats = keep
	fmt.Println("  markdown, plus HTML on days with screenshots")
}

func describeFormats(cfg config.Config) string {
	if wantsHTML(cfg) {
		return "markdown + html"
	}
	return "markdown · html when there are screenshots"
}

func describePreview(cfg config.Config) string {
	if !cfg.Preview.Enabled {
		return "off"
	}
	n := 0
	for _, r := range cfg.Watch.Repos {
		if r.Preview {
			n++
		}
	}
	if n == 0 {
		return "on, but no folder opted in"
	}
	where := plural(n, "folder")
	if len(cfg.Preview.Repos) > 0 {
		where = fmt.Sprintf("%s of %s", plural(len(cfg.Preview.Repos), "repo"), where)
	}
	when := "when you run it"
	if cfg.Preview.OnSchedule {
		when = "nightly"
	}
	return fmt.Sprintf("on · %s · max %d · %s", where, cfg.Preview.MaxPhotos, when)
}

func describeRoots(cfg config.Config) string {
	if len(cfg.Watch.Repos) == 0 {
		return "nothing yet"
	}
	var ps []string
	for _, r := range cfg.Watch.Repos {
		ps = append(ps, r.Path)
	}
	return fmt.Sprintf("%s  (%d repos)", strings.Join(ps, ", "), len(vcs.Discover(cfg)))
}

func describeSchedule(cfg config.Config) string {
	return cfg.Schedule.At + ", " + daysOrEvery(cfg)
}

func daysOrEvery(cfg config.Config) string {
	if len(cfg.Schedule.Days) == 0 {
		return "every day"
	}
	return strings.Join(cfg.Schedule.Days, ",")
}

func describeNarration(cfg config.Config) string {
	if !cfg.Narrate.Enabled {
		return "off"
	}
	switch cfg.Narrate.Provider {
	case "api":
		return "on · Anthropic API"
	case "cli":
		return "on · Claude Code"
	default:
		return "on · auto"
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not set — every commit counts as yours"
	}
	return s
}

var _ = filepath.Join
var _ = strconv.Itoa
