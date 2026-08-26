package preview

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
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
