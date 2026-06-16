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
	// gosec G115: int → uint32 conversion. mask comes from syscall.Umask,
	// which on every supported Unix returns a value that fits the file-mode
	// bits (well below 2^32). The narrowing is safe by construction.
	return perm &^ os.FileMode(mask) //nolint:gosec // umask bits fit os.FileMode
}
