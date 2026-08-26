//go:build !windows

package preview

import "syscall"

// killGroup stops a process group a previous run left behind, and reports
// whether there was one to stop.
//
// Signal 0 asks "is this alive" without touching it. A recycled PID is possible
// in principle; killing a stranger's process is not worth the tidiness, so only
// a still-running group daybook itself recorded is signalled.
func killGroup(pid int) bool {
	if syscall.Kill(-pid, syscall.Signal(0)) != nil {
		return false
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	return true
}
