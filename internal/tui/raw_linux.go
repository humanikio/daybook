//go:build linux

package tui

import "golang.org/x/sys/unix"

const (
	ioctlRead  = unix.TCGETS
	ioctlWrite = unix.TCSETS
)
