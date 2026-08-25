//go:build darwin

package tui

import "golang.org/x/sys/unix"

// BSD and Linux disagree on the ioctl numbers for termios. Everything else in
// raw_unix.go is identical, so only the constants are split.
const (
	ioctlRead  = unix.TIOCGETA
	ioctlWrite = unix.TIOCSETA
)
