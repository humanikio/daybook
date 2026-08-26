package preview

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Knowing whether a server is up, and clearing up after versions that started
// them from here.
//
// daybook no longer starts dev servers. It was bad at stopping them: `next dev`
// spawns a next-server grandchild that escapes the process group, so a teardown
// that printed "stopping" left the port held for hours, and the next run then
// collided with it. The capture agent is already in a shell, can read the port
// the app announces rather than trusting one recorded on an earlier day, and is
// told to stop what it started. What remains here is the check for whether
// something is already serving, and a reaper for pid records the old path left.

// Reachable reports whether something already answers on a port.
//
// Checked FIRST, always. Most of the time the app is already running because
// somebody is working on it, and using what is there removes every risk this
// file otherwise carries — nothing to start, nothing to leak, no port
// collision, no boot wait.
func Reachable(port int) bool {
	if port <= 0 {
		return false
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// pidRecord is what the old start path wrote before launching a server. Kept so
// an upgrade can still find and stop what those runs left behind.
type pidRecord struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Dir     string `json:"dir"`
	Started string `json:"started"`
}

// ReapOrphans kills servers a previous run started and never stopped.
//
// The in-process cleanup covers panics and timeouts. It cannot cover the
// machine losing power or daybook being killed outright, and those leave a dev
// server running with nobody who knows about it. The pid files are how the next
// run finds out.
func ReapOrphans(stateDir string) []string {
	var reaped []string
	files, _ := filepath.Glob(filepath.Join(stateDir, "preview-*.pid"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var rec pidRecord
		if json.Unmarshal(b, &rec) != nil || rec.PID <= 0 {
			_ = os.Remove(f)
			continue
		}
		// Signal 0 asks "is this alive" without touching it. A recycled PID is
		// possible in principle; killing a stranger's process is not worth the
		// tidiness, so only a still-running group we recorded is touched.
		if syscall.Kill(-rec.PID, syscall.Signal(0)) == nil {
			_ = syscall.Kill(-rec.PID, syscall.SIGTERM)
			reaped = append(reaped, fmt.Sprintf("%s (pid %d, started %s)", rec.Command, rec.PID, rec.Started))
		}
		_ = os.Remove(f)
	}
	return reaped
}

func safe(s string) string {
	if s == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}
