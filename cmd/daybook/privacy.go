package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/egress"
	"github.com/humanikio/daybook/internal/vcs"
)

// `daybook privacy` — what THIS machine's configuration actually does.
//
// Derived, never authored. Every other statement about what daybook sends is
// prose somebody wrote once, and prose drifts: both privacy pages went on saying
// narration was the only thing leaving the machine for three releases after
// screenshots shipped. This reads the live config and the filesystem and reports
// what is true now, so there is nothing to keep in sync.
//
// It is also the thing to run before a screen share, or when handing daybook to
// somebody else and being asked what it does with their work.
func cmdPrivacy(args []string) error {
	fs := flag.NewFlagSet("privacy", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}

	fmt.Println("What this machine sends, and where it keeps things.")
	fmt.Println()

	// Ordered by how much a reader should care, not by how the config is laid
	// out. The browser one is first when it is on, because it is the largest.
	on, off := 0, 0
	for _, r := range egress.Routes {
		live, detail := routeState(cfg, r)
		if live {
			on++
			fmt.Printf("  ON   %-14s %s\n", r.Name, r.Sends)
			fmt.Printf("       %-14s → %s\n", "", r.To)
		} else {
			off++
			fmt.Printf("  off  %-14s %s\n", r.Name, detail)
		}
	}
	fmt.Println()
	fmt.Printf("  %d of %s can send from this machine right now.\n",
		on, plural(on+off, "route", "routes"))

	// Screenshots are the one that writes something you cannot take back, so
	// they get counted rather than described.
	if cfg.Preview.Enabled {
		fmt.Println()
		fmt.Println("  Screenshots are on. They are the only thing here that photographs")
		fmt.Println("  real screens, and redaction cannot touch an image — it runs over")
		fmt.Println("  text before it reaches disk.")
		if n, dir := countShots(cfg); n > 0 {
			fmt.Printf("    %s on disk in %s\n", plural(n, "image", "images"), dir)
		} else {
			fmt.Println("    no images written yet")
		}
		if cfg.Preview.StartServers {
			fmt.Println("    start_servers is on — the agent runs your project's code unattended")
		}
		if cfg.Preview.OnSchedule {
			fmt.Printf("    on_schedule is on — this happens at %s without you present\n", cfg.Schedule.At)
		}
		if len(cfg.Preview.Repos) > 0 {
			fmt.Printf("    limited to %s\n", strings.Join(cfg.Preview.Repos, ", "))
		} else if names := previewRepoNames(cfg); len(names) > 0 {
			fmt.Printf("    every repo under the folders you photograph — %s\n",
				plural(len(names), "repo", "repos"))
		}
	}

	fmt.Println()
	root := cfg.OutputRoot()
	fmt.Printf("  Reports  %s\n", root)
	fmt.Printf("           %s\n", describeDirPerms(root))
	if s := syncedRootFor(root); s != "" {
		// Worth saying out loud: the folder holds prompt text and, with
		// screenshots on, pictures of real screens. A sync client uploading that
		// is not daybook's doing and is very much the user's problem.
		fmt.Printf("           ! this looks like it is inside %s — it holds your prompt\n", s)
		fmt.Printf("             history, so check whether you want it syncing\n")
	}
	if !cfg.Privacy.KeepRawPrompts {
		fmt.Println("           prompt text is not stored — counts and commits only")
	}
	fmt.Println()
	fmt.Println("  docs/privacy.md has the detail. Every route above is checked")
	fmt.Println("  against that page on every build.")
	return nil
}

// routeState answers whether a route can fire as configured, and when it cannot,
// why not — a bare "off" leaves somebody wondering which switch they are looking
// for.
func routeState(cfg config.Config, r egress.Route) (bool, string) {
	switch r.Name {
	case "narration":
		if cfg.Narrate.Enabled && !strings.EqualFold(cfg.Narrate.Provider, "api") {
			return true, ""
		}
		return false, "narrate.enabled is false"
	case "narration-api":
		if cfg.Narrate.Enabled && strings.EqualFold(cfg.Narrate.Provider, "api") {
			return true, ""
		}
		return false, "narrate.provider is not api"
	case "screenshots":
		if cfg.Preview.Enabled {
			return true, ""
		}
		return false, "preview.enabled is false"
	case "git-fetch":
		if cfg.Watch.Fetch {
			return true, ""
		}
		return false, "watch.fetch is false"
	case "upgrade-check":
		// No config gates it; it fires only when asked for.
		return false, "only when you run `daybook upgrade`"
	}
	return false, "unknown route"
}

func countShots(cfg config.Config) (int, string) {
	dir := filepath.Join(cfg.OutputRoot(), "assets")
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".png", ".jpg", ".jpeg", ".webp":
			n++
		}
		return nil
	})
	return n, dir
}

func previewRepoNames(cfg config.Config) []string {
	var names []string
	for _, r := range vcs.Discover(cfg) {
		if cfg.PreviewCovers(r.Root) {
			names = appendOnce(names, r.Name)
		}
	}
	sort.Strings(names)
	return names
}

func describeDirPerms(dir string) string {
	fi, err := os.Stat(config.Expand(dir))
	if err != nil {
		return "not created yet"
	}
	m := fi.Mode().Perm()
	if m&0o077 == 0 {
		return fmt.Sprintf("%04o — yours only", m)
	}
	return fmt.Sprintf("%04o — READABLE BY OTHER ACCOUNTS on this machine", m)
}

// syncedRootFor names the sync folder a path sits inside, or "".
//
// daybook does not sync anything. A sync client might, and a reports folder on a
// synced Desktop uploads your prompt history without anyone deciding to.
func syncedRootFor(dir string) string {
	p := config.Expand(dir)
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, c := range []struct{ path, name string }{
		{filepath.Join(home, "Library", "Mobile Documents"), "iCloud Drive"},
		{filepath.Join(home, "Library", "CloudStorage"), "a cloud drive"},
		{filepath.Join(home, "Dropbox"), "Dropbox"},
		{filepath.Join(home, "Google Drive"), "Google Drive"},
		{filepath.Join(home, "OneDrive"), "OneDrive"},
	} {
		if config.HasPathPrefix(p, c.path) {
			return c.name
		}
	}
	return ""
}
