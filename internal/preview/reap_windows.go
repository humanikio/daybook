//go:build windows

package preview

// killGroup has nothing to stop on Windows.
//
// The records this reaps were written by a start path that put each server in
// its own process group with Setpgid, which does not exist here — so that path
// never ran on Windows and never wrote one of these files. A stale record is
// still deleted by the caller; there is simply no process behind it.
func killGroup(int) bool { return false }
