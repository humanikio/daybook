// Package preview finds the way to see a change, rather than guessing it.
//
// To photograph a feature something has to be serving it, and the obvious
// implementation guesses: try `npm run dev`, hope. That is wrong often enough
// to be useless — the command differs per repo, per package manager, and
// sometimes per person.
//
// The transcripts already contain the answer. Whoever built the feature started
// the app to look at it, and that command is recorded verbatim, in the
// directory it ran in, alongside how long they waited for it. So this extracts
// rather than infers, which is the same rule the rest of daybook runs on:
// resolve from a live source, never author.
package preview

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/model"
)

// Server is a command observed starting a development server.
type Server struct {
	// Command is the core invocation, lifted out of whatever shell line it was
	// wrapped in — `pnpm dev`, not `(pnpm dev > /tmp/x.log 2>&1 &) ; sleep 25`.
	//
	// The wrapper is how a person ran it once; the core is the thing to run.
	// Replaying the whole line would also background the process outside our
	// control, and a server we cannot see the PID of is one we cannot stop.
	Command string `json:"command"`
	// Dir is where it ran.
	Dir string `json:"dir"`
	// BootSeconds is the wait observed alongside it, when there was one. A
	// `sleep 25` next to a dev server is somebody having learned how long that
	// app takes, which is better information than a default.
	BootSeconds int `json:"bootSeconds,omitempty"`
	// Port, when the command or its surroundings named one.
	Port int       `json:"port,omitempty"`
	At   time.Time `json:"at"`
	// Repo is filled in later, once repo roots are known.
	Repo string `json:"repo,omitempty"`
}

// serverPat matches the invocations that start something long-running and
// serving. Deliberately a list rather than a heuristic: "any command that does
// not exit" cannot be recognised from its text, and a false positive here means
// daybook launches something arbitrary.
var serverPat = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
	`npm run dev`, `npm start`, `npm run start`,
	`pnpm dev`, `pnpm run dev`, `pnpm start`,
	`yarn dev`, `yarn start`,
	`bun dev`, `bun run dev`,
	`next dev`, `vite`, `nuxt dev`, `remix dev`,
	`vercel dev`,
	`nodemon`,
	`docker compose up`, `docker-compose up`,
	`firebase emulators:start`,
	`rails s(?:erver)?`, `flask run`, `uvicorn`, `gunicorn`,
	`python -m http\.server`,
	`go run \./cmd/[\w./-]+`,
	`air`, `mix phx\.server`,
}, `|`) + `)\b`)

// sleepPat finds a wait in the same command — `sleep 25` beside a server start.
var sleepPat = regexp.MustCompile(`\bsleep\s+(\d+)`)

// portPat finds an explicit port flag or a localhost URL in the same line.
var portPat = regexp.MustCompile(`(?:--port[= ]|-p[= ]|localhost:|127\.0\.0\.1:)(\d{2,5})`)

// FromCommand extracts a Server from one bash invocation, or nil.
//
// The whole line is inspected for the wait and the port, because both routinely
// sit beside the start rather than inside it: `(pnpm dev &) ; sleep 25; curl
// localhost:3000` carries all three facts and none of them are in the same word.
func FromCommand(cmd, dir string, at time.Time) *Server {
	m := serverPat.FindString(cmd)
	if m == "" {
		return nil
	}
	// Ignore a command that is only ever talked about — a grep for it, a line
	// in a heredoc, a doc being written. Those appear constantly in transcripts
	// about servers and start nothing.
	if isQuoted(cmd, m) {
		return nil
	}
	s := &Server{Command: strings.TrimSpace(m), Dir: dir, At: at}
	if w := sleepPat.FindStringSubmatch(cmd); w != nil {
		if n, err := strconv.Atoi(w[1]); err == nil && n > 0 && n < 600 {
			s.BootSeconds = n
		}
	}
	if p := portPat.FindStringSubmatch(cmd); p != nil {
		if n, err := strconv.Atoi(p[1]); err == nil && n > 0 && n < 65536 {
			s.Port = n
		}
	}
	return s
}

// isQuoted reports whether the match sits inside a quoted string or a heredoc
// body — where it is being written about rather than run.
func isQuoted(cmd, match string) bool {
	i := strings.Index(cmd, match)
	if i < 0 {
		return false
	}
	before := cmd[:i]
	// A heredoc anywhere before it means the rest is a document.
	if strings.Contains(before, "<<'") || strings.Contains(before, "<<\"") || strings.Contains(before, "<<EOF") {
		return true
	}
	// An odd number of quotes before it means we are inside one.
	return strings.Count(before, "'")%2 == 1 || strings.Count(before, `"`)%2 == 1
}

// BootWait is how long to wait before deciding a server failed to come up.
//
// The observed value is faithful to what somebody did, and that is the right
// thing to RECORD — but not always the right thing to reuse. A `sleep 2` beside
// `npm run dev` was usually part of checking something else, not a real boot
// wait, and honouring it would call every slow app broken. So the observation
// sets the floor of our patience, never the ceiling.
func (s Server) BootWait() time.Duration {
	const min, max = 15 * time.Second, 3 * time.Minute
	d := time.Duration(s.BootSeconds) * time.Second
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	// Half again as long as it took whoever measured it. They were watching;
	// this will not be.
	return d + d/2
}

// BootWaitOf is BootWait for a model.Server, so callers holding the report's
// type do not have to convert just to ask a question.
func BootWaitOf(s model.Server) time.Duration { return Server{BootSeconds: s.BootSeconds}.BootWait() }

// PickPerRepo reduces a catalogue to one server per repository, preferring the
// directory that actually looks like where the app lives.
//
// The raw catalogue keys on the session's cwd, and a session sits wherever
// somebody last cd'd — so the same app appears under its root, under src/app,
// and under a docs subfolder three levels down. Starting all of those is ten
// servers to take six pictures, most of them in directories that cannot serve
// anything.
//
// A manifest is the evidence: the directory holding package.json, go.mod or a
// compose file is the one the command was really meant for. Failing that, the
// shallowest observation wins, because a cwd artifact is always deeper than the
// thing it drifted from.
func PickPerRepo(servers []Server) []Server {
	best := map[string]Server{}
	for _, s := range servers {
		if s.Repo == "" {
			continue
		}
		cur, seen := best[s.Repo]
		if !seen || score(s) > score(cur) {
			best[s.Repo] = s
		}
	}
	out := make([]Server, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}

func score(s Server) int {
	n := 0
	for _, manifest := range []string{"package.json", "go.mod", "docker-compose.yml", "compose.yaml", "Cargo.toml"} {
		if _, err := os.Stat(filepath.Join(s.Dir, manifest)); err == nil {
			n += 100
			break
		}
	}
	// A port somebody wrote down is worth more than one nobody did: it means
	// this observation carried enough context to be checked before starting.
	if s.Port > 0 {
		n += 20
	}
	// Shallower beats deeper. A cwd that drifted is always further down.
	n -= strings.Count(s.Dir, string(filepath.Separator))
	return n
}

// Dedupe keeps the most recent observation per (dir, command), because the same
// server gets started many times a day and only the last one is evidence of how
// it is currently run.
func Dedupe(servers []Server) []Server {
	best := map[string]Server{}
	for _, s := range servers {
		k := s.Dir + "\x00" + strings.ToLower(s.Command)
		if prev, ok := best[k]; !ok || s.At.After(prev.At) {
			// Carry forward a boot time or port learned on an earlier run: the
			// most recent invocation is the most current command, but an older
			// one may be the only place somebody wrote down the wait.
			if ok {
				if s.BootSeconds == 0 {
					s.BootSeconds = prev.BootSeconds
				}
				if s.Port == 0 {
					s.Port = prev.Port
				}
			}
			best[k] = s
		}
	}
	out := make([]Server, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	return out
}
