package tui

import "os"

// IsTerminal asks the kernel, not the file mode.
//
// The ModeCharDevice test is not the same question: /dev/null is a character
// device and passes it, so a `< /dev/null` run looks interactive right up until
// the first read returns EOF. Attempting the termios ioctl is the actual test —
// it succeeds only on a real terminal.
func IsTerminal(f *os.File) bool {
	restore, err := makeRaw(f)
	if err != nil {
		return false
	}
	restore()
	return true
}
