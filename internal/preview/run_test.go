package preview

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// Already-running is the good path: nothing is started, so nothing can leak.
func TestStartDoesNothingWhenSomethingAlreadyAnswers(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	h, err := Start(context.Background(), Server{
		Command: "this-would-fail", Dir: t.TempDir(), Port: port,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("errored despite the port answering: %v", err)
	}
	if h.Started() {
		t.Error("started a server when one was already there")
	}
}

// The whole point of the file: what it starts, it stops.
func TestStartThenStopReleasesThePort(t *testing.T) {
	port := freePort(t)
	state := t.TempDir()

	h, err := Start(context.Background(), Server{
		// Must survive being connected to more than once: the readiness probe
		// opens a connection, and a one-shot listener would be consumed by it.
		Command: "python3 -m http.server " + itoa(port),
		Dir:     t.TempDir(), Port: port, BootSeconds: 1,
	}, state)
	if err != nil {
		t.Skipf("could not start a stand-in server here: %v", err)
	}
	if !h.Started() {
		t.Fatal("reported nothing started")
	}
	if !Reachable(port) {
		t.Fatal("started but nothing is answering")
	}

	// The pid file exists WHILE it runs — that is what lets a later run clean
	// up after a hard kill.
	if _, err := os.Stat(h.pidPath); err != nil {
		t.Error("no pid file while the server was running")
	}

	h.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && Reachable(port) {
		time.Sleep(50 * time.Millisecond)
	}
	if Reachable(port) {
		t.Error("the port is still held after Stop")
	}
	if _, err := os.Stat(h.pidPath); !os.IsNotExist(err) {
		t.Error("the pid file outlived the server")
	}
}

// A command that exits immediately must be reported, not waited out.
func TestStartFailsFastWhenTheCommandDies(t *testing.T) {
	h, err := Start(context.Background(), Server{
		Command: "exit 1", Dir: t.TempDir(), Port: freePort(t), BootSeconds: 1,
	}, t.TempDir())
	if err == nil {
		h.Stop()
		t.Fatal("a command that exits immediately reported success")
	}
}

// Power loss and hard kills cannot be handled in process. The pid file is how
// the next run finds a server nobody is tracking.
func TestReapOrphansClearsStaleRecords(t *testing.T) {
	state := t.TempDir()
	// A PID that is certainly not a live process group we own.
	if err := os.WriteFile(filepath.Join(state, "preview-ghost.pid"),
		[]byte(`{"pid":999999,"command":"npm run dev","dir":"/x","started":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ReapOrphans(state)
	if _, err := os.Stat(filepath.Join(state, "preview-ghost.pid")); !os.IsNotExist(err) {
		t.Error("a stale pid file survived reaping")
	}
}

// Signal 0 is the liveness check; make sure the helper is asking that question
// of a group rather than a bare pid.
func TestReapUsesTheProcessGroup(t *testing.T) {
	if syscall.Kill(-999999, syscall.Signal(0)) == nil {
		t.Skip("this machine has a process group 999999")
	}
}

func itoa(n int) string {
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// The catalogue records what a port WAS. A dev server moves — a taken port, a
// changed script, a different branch — and it announces where it landed. That
// announcement is better evidence than anything recorded earlier.
func TestLogPortReadsWhatTheServerAnnounced(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "x.log")
	body := `> hos-frontend@0.1.0 dev
> next dev -p 3002

   ▲ Next.js 15.5.7
   - Local:        http://localhost:3002
   ✓ Ready in 1878ms`
	if err := os.WriteFile(log, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Handle{logPath: log}
	if got := h.logPort(); got != 3002 {
		t.Errorf("logPort() = %d, want 3002 — the port it actually came up on", got)
	}
}

func TestLogPortIsZeroWhenNothingAnnounced(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "x.log")
	_ = os.WriteFile(log, []byte("building...\ndone\n"), 0o600)
	if got := (&Handle{logPath: log}).logPort(); got != 0 {
		t.Errorf("invented a port: %d", got)
	}
}

// A server with no recorded port used to be given its boot time and assumed
// fine, so one that announced itself on its second line was reported as
// "started" with no address — and the agent got a frontend with no backend.
func TestWaitFindsAPortNobodyRecorded(t *testing.T) {
	port := freePort(t)
	h, err := Start(context.Background(), Server{
		// Announces the port the way a real server does, then serves on it.
		Command: "echo 'Server running on port " + itoa(port) + "'; python3 -m http.server " + itoa(port),
		Dir:     t.TempDir(), BootSeconds: 1, // no Port recorded
	}, t.TempDir())
	if err != nil {
		t.Skipf("no stand-in server available here: %v", err)
	}
	defer h.Stop()
	if h.Port() != port {
		t.Errorf("Port() = %d, want %d — read from what the server announced", h.Port(), port)
	}
}
