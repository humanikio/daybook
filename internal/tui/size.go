//go:build darwin || linux

package tui

import (
	"os"

	"golang.org/x/sys/unix"
)

// width is the terminal's column count, or 80 when it cannot be asked.
//
// The redraw walks back a fixed number of lines to erase what it drew. That
// count is only right if every row occupied exactly one line — and a row wider
// than the terminal wraps into two, so the walk-back falls short and leaves a
// copy of the header on screen for every keypress.
func width() int {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col < 20 {
		return 80
	}
	return int(ws.Col)
}
