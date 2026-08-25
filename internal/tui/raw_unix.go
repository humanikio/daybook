//go:build darwin || linux

package tui

import (
	"os"

	"golang.org/x/sys/unix"
)

// Raw-mode terminal handling, done directly on x/sys/unix rather than pulling
// in golang.org/x/term.
//
// x/term is a thin wrapper over exactly these two ioctls, and x/sys is already
// a dependency by way of the service layer. For a tool whose selling point is a
// single static binary with nothing to install, one fewer module in go.sum is
// worth fifteen lines.

func makeRaw(f *os.File) (func(), error) {
	fd := int(f.Fd())
	prev, err := unix.IoctlGetTermios(fd, ioctlRead)
	if err != nil {
		return nil, err
	}
	raw := *prev
	// Character-at-a-time, no echo. ISIG stays ON deliberately: ctrl-c must
	// still kill the process. A menu you cannot escape is worse than no menu.
	raw.Lflag &^= unix.ECHO | unix.ICANON
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWrite, &raw); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlWrite, prev) }, nil
}
