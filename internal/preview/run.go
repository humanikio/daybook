package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Running a server, and — more importantly — stopping it again.
//
// A development server left running overnight costs somebody a port tomorrow
// and looks like nothing tonight. Every path out of here kills what it started,
// including the ones that are not returns: a panic, a timeout, an interrupt, or
// the process being killed outright. That last one cannot be handled in
// process, which is why the PID is written to disk before the server is
// started rather than after.

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

// Handle is a server this process started and is responsible for.
type Handle struct {
	Server  Server
	cmd     *exec.Cmd
	logPath string
	pidPath string
	// exited closes when the process ends. A goroutine has to be reaping it for
	// that to be knowable: cmd.ProcessState is only populated by Wait, so
	// without this a command that dies on the first line looks identical to one
	// still booting, and the failure takes the entire boot timeout to notice.
	exited chan struct{}
}

// Started reports whether anything was actually launched. False means the port
// already answered and nothing needs stopping.
func (h *Handle) Started() bool { return h != nil && h.cmd != nil }

// Log is where the server's output went, for when it did not come up.
func (h *Handle) Log() string {
	if h == nil {
		return ""
	}
	return h.logPath
}

// Start brings a server up, or returns a nil handle if one is already there.
//
// stateDir holds the pid file. It is written BEFORE the wait, so a daybook that
// is killed during the boot still leaves behind the evidence needed to clean up.
func Start(ctx context.Context, s Server, stateDir string) (*Handle, error) {
	if Reachable(s.Port) {
		return nil, nil // already serving — the good path
	}
	if s.Command == "" || s.Dir == "" {
		return nil, fmt.Errorf("no command recorded for %s", s.Repo)
	}
	if fi, err := os.Stat(s.Dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%s no longer exists", s.Dir)
	}

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	logPath := filepath.Join(stateDir, "preview-"+safe(s.Repo)+".log")
	log, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}

	// Through a shell, because the extracted command is a shell command —
	// `npm run dev` resolves scripts, `docker compose up` is two words.
	cmd := exec.Command("sh", "-c", s.Command)
	cmd.Dir = s.Dir
	cmd.Stdout, cmd.Stderr = log, log
	// Its own process group. A dev server spawns children — a bundler, a
	// watcher, a container — and killing only the parent leaves those holding
	// the port, which looks exactly like the server never stopped.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, err
	}
	h := &Handle{
		Server: s, cmd: cmd, logPath: logPath,
		pidPath: filepath.Join(stateDir, "preview-"+safe(s.Repo)+".pid"),
		exited:  make(chan struct{}),
	}
	go func() { _ = cmd.Wait(); close(h.exited) }()
	h.writePID()

	if err := h.waitReady(ctx, s.BootWait()); err != nil {
		h.Stop()
		return nil, err
	}
	return h, nil
}

func (h *Handle) waitReady(ctx context.Context, within time.Duration) error {
	if h.Server.Port <= 0 {
		// No port to poll. Give it the boot time and hope — better than
		// declaring failure for an app whose port nobody wrote down.
		select {
		case <-time.After(within):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if Reachable(h.Server.Port) {
			return nil
		}
		select {
		case <-h.exited:
			// Dead, and we know immediately rather than at the timeout. The log
			// is named because the reason is in it and nowhere else.
			return fmt.Errorf("%s exited before serving — see %s", h.Server.Command, h.logPath)
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("%s did not answer on :%d within %s — see %s",
		h.Server.Command, h.Server.Port, within, h.logPath)
}

// Stop kills the process group, politely then not.
func (h *Handle) Stop() {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return
	}
	pgid := -h.cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)

	select {
	case <-h.exited:
	case <-time.After(5 * time.Second):
		// A dev server that ignores SIGTERM is common enough — a watcher
		// trapping it, a container runtime taking its time. Waiting forever
		// would hold the whole run open.
		_ = syscall.Kill(pgid, syscall.SIGKILL)
	}
	_ = os.Remove(h.pidPath)
	h.cmd = nil
}

type pidRecord struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Dir     string `json:"dir"`
	Started string `json:"started"`
}

func (h *Handle) writePID() {
	b, err := json.Marshal(pidRecord{
		PID: h.cmd.Process.Pid, Command: h.Server.Command,
		Dir: h.Server.Dir, Started: time.Now().Format(time.RFC3339),
	})
	if err == nil {
		_ = os.WriteFile(h.pidPath, b, 0o600)
	}
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
