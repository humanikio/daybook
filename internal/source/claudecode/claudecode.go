// Package claudecode reads Claude Code's session transcripts.
//
// IT PARSES AN UNDOCUMENTED FORMAT. ~/.claude/projects/<project>/<session>.jsonl
// is internal to Claude Code and carries no compatibility guarantee; the
// reference corpus alone spans 27 different CLI versions. Everything here is
// therefore written defensively:
//
//   - decode into structs whose fields are all optional, never index into a
//     shape that was merely observed once;
//   - accept `content` as EITHER a string or an array of blocks, because both
//     appear in practice;
//   - fail soft per LINE, not per file. One malformed record skips that record
//     and increments a counter that the report prints.
//
// The counter is the point. Silent breakage in a tool whose whole job is to be
// an accurate record is the worst failure mode available to it.
package claudecode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tndigitalmark/daybook/internal/config"
	"github.com/tndigitalmark/daybook/internal/model"
	"github.com/tndigitalmark/daybook/internal/source"
)

type Source struct{}

func (Source) Name() string { return "claude-code" }

// record is one line of a transcript. Every field is optional.
type record struct {
	Type        string  `json:"type"`
	Timestamp   string  `json:"timestamp"`
	SessionID   string  `json:"sessionId"`
	CWD         string  `json:"cwd"`
	GitBranch   string  `json:"gitBranch"`
	Entrypoint  string  `json:"entrypoint"`
	IsSidechain bool    `json:"isSidechain"`
	AITitle     string  `json:"aiTitle"`
	Origin      *origin `json:"origin"`
	Message     *msg    `json:"message"`
}

type origin struct {
	Kind string `json:"kind"`
}

type msg struct {
	Role string `json:"role"`
	// Content is a string on some records and an array of blocks on others.
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	OutputTokens int `json:"output_tokens"`
}

type block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

// Path extraction from bash commands.
//
// Necessary because most file edits do not go through the Edit tool. In the
// reference week there were 6,240 Bash calls against 162 Edit calls: editing
// happens inside heredocs and sed, so counting Edit/Write tool calls
// undercounts real file work by roughly forty times.
//
// pathish deliberately requires a slash AND an extension. Without the slash it
// matches version numbers and decimals; without the extension it matches every
// flag and URL fragment. Relative hits are resolved against the session's cwd
// later, in derive, where the repository roots are known.
var pathish = regexp.MustCompile(`[A-Za-z0-9._~@/-]*/[A-Za-z0-9._@-]+\.[A-Za-z0-9]{1,6}\b`)

// dirTarget catches the two commands that name a working directory outright.
//
// This is the highest-signal, lowest-noise thing in a bash command: `cd x` and
// `git -C x` say exactly which tree is being operated on, with none of the
// guesswork that scraping paths involves.
var dirTarget = regexp.MustCompile(`(?:\bcd\s+|\bgit\s+-C\s+)("[^"]+"|'[^']+'|[A-Za-z0-9._~@/-]+)`)

func (s Source) Streams(cfg config.Config, w source.Window) (source.Result, error) {
	var res source.Result
	red := cfg.Redactor()

	for _, a := range cfg.Watch.Agents {
		if a.Source != "" && a.Source != s.Name() {
			continue
		}
		root := config.Expand(a.Path)
		files, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
		if err != nil {
			return res, err
		}
		for _, f := range files {
			// A transcript untouched since before the window cannot hold a
			// message inside it. Skipping those took a full scan of 239
			// transcripts from 24s to under a second — the corpus is 1.1GB and
			// almost none of it is ever relevant to today.
			if fi, err := os.Stat(f); err == nil && fi.ModTime().Before(w.Start) {
				continue
			}
			st, n, ok := s.readOne(f, w, red, cfg.Privacy.KeepRawPrompts)
			res.ParseErrors += n
			if ok {
				res.Streams = append(res.Streams, st)
			}
		}
	}
	return res, nil
}

// readOne parses a single transcript.
//
// Two passes are avoided deliberately: the title arrives on its own record and
// may land anywhere in the file, so it is captured as we go and applied at the
// end rather than re-reading.
func (s Source) readOne(path string, w source.Window, red *config.Redactor, keepPrompts bool) (model.Stream, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return model.Stream{}, 0, false
	}
	defer f.Close()

	st := model.Stream{
		ID:      strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Project: filepath.Base(filepath.Dir(path)),
		Tools:   map[string]int{},
		Files:   map[string]int{},
		Dirs:    map[string]int{},
		Repos:   map[string]int{},
	}
	buckets := map[int64]struct{}{}
	var parseErrors int
	var active bool // saw a message inside the window

	sc := bufio.NewScanner(f)
	// Transcript lines carry whole assistant turns and can be very large; the
	// default 64KiB scanner limit truncates them into parse errors.
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			parseErrors++
			continue
		}
		if r.AITitle != "" {
			st.Title = r.AITitle
		}
		if r.Timestamp == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, r.Timestamp)
		if err != nil {
			parseErrors++
			continue
		}
		ts = ts.Local()

		inWindow := !ts.Before(w.Start) && !ts.After(w.End)
		if inWindow {
			active = true
		}
		// Scope decides what is COLLECTED; the window still decides what makes
		// a session active at all.
		if !inWindow && w.Scope != "session" {
			continue
		}

		if r.CWD != "" {
			st.CWD = r.CWD
		}
		if r.GitBranch != "" && r.GitBranch != "HEAD" {
			st.Branch = r.GitBranch
		}
		if r.Entrypoint == "sdk-cli" {
			st.Agent = true
		}
		if st.First.IsZero() || ts.Before(st.First) {
			st.First = ts
		}
		if ts.After(st.Last) {
			st.Last = ts
		}
		buckets[ts.Unix()/600] = struct{}{}

		switch r.Type {
		case "user":
			// Only a real person typing counts as a prompt. Tool results and
			// task notifications also arrive as "user" records.
			if r.Origin == nil || r.Origin.Kind != "human" || r.Message == nil {
				continue
			}
			text := contentText(r.Message.Content)
			if text = strings.TrimSpace(text); text == "" {
				continue
			}
			if keepPrompts {
				st.Prompts = append(st.Prompts, model.Prompt{At: ts, Text: red.Do(squash(text))})
			} else {
				st.Prompts = append(st.Prompts, model.Prompt{At: ts})
			}

		case "assistant":
			if r.Message == nil {
				continue
			}
			if r.Message.Usage != nil {
				st.OutputTokens += r.Message.Usage.OutputTokens
			}
			blocks := contentBlocks(r.Message.Content)
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if t := strings.TrimSpace(b.Text); t != "" && keepPrompts {
						st.Notes = append(st.Notes, model.Note{At: ts, Text: red.Do(squash(t))})
					}
				case "tool_use":
					if b.Name != "" {
						st.Tools[b.Name]++
					}
					var in toolInput
					if len(b.Input) > 0 {
						_ = json.Unmarshal(b.Input, &in)
					}
					if in.FilePath != "" {
						st.Files[in.FilePath]++
					}
					if in.Command != "" {
						for _, m := range pathish.FindAllString(in.Command, -1) {
							st.Files[m]++
						}
						for _, m := range dirTarget.FindAllStringSubmatch(in.Command, -1) {
							d := strings.Trim(m[1], "\"'")
							if d != "" && d != "-" && !strings.HasPrefix(d, "-") {
								st.Dirs[d]++
							}
						}
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		parseErrors++
	}
	if !active {
		return model.Stream{}, parseErrors, false
	}
	st.ActiveMinutes = len(buckets) * 10
	if st.Title == "" {
		st.Title = fallbackTitle(st)
	}
	return st, parseErrors, true
}

// contentText handles content being a bare string or an array of blocks.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var out []string
	for _, b := range contentBlocks(raw) {
		if b.Type == "text" && b.Text != "" {
			out = append(out, b.Text)
		}
	}
	return strings.Join(out, " ")
}

func contentBlocks(raw json.RawMessage) []block {
	if len(raw) == 0 {
		return nil
	}
	var bs []block
	if err := json.Unmarshal(raw, &bs); err != nil {
		return nil
	}
	return bs
}

var ws = regexp.MustCompile(`\s+`)

func squash(s string) string { return strings.TrimSpace(ws.ReplaceAllString(s, " ")) }

// fallbackTitle names a stream that Claude never titled.
//
// 87 of 237 sessions in the reference corpus had no ai-title record, so this is
// a common path, not a rare one.
func fallbackTitle(st model.Stream) string {
	if len(st.Prompts) > 0 && st.Prompts[0].Text != "" {
		t := st.Prompts[0].Text
		if len(t) > 60 {
			if i := strings.LastIndex(t[:60], " "); i > 20 {
				return t[:i] + "…"
			}
			return t[:60] + "…"
		}
		return t
	}
	if st.CWD != "" {
		return filepath.Base(st.CWD)
	}
	return "(untitled)"
}
