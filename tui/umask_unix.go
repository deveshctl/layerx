//go:build unix

package tui

import (
	"os"
	"syscall"
)

// applyUmask returns perm with the process umask cleared, mirroring what
// os.OpenFile would have done at creation time. The umask is read by
// round-tripping through syscall.Umask — there is no portable getter.
//
// Used by atomicWriteFile so the temp+rename path produces the same final
// mode as the os.WriteFile path it replaced. Without this, tmp.Chmod(perm)
// would set the literal bits, silently widening permissions on systems with
// a non-trivial umask (e.g. 0077 → 0644 saves becoming 0644 instead of 0600).
func applyUmask(perm os.FileMode) os.FileMode {
	mask := syscall.Umask(0)
	syscall.Umask(mask)
	return perm &^ os.FileMode(mask)
}
