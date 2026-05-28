//go:build windows

package tui

import "os"

// applyUmask is a no-op on Windows: the OS does not honour Unix-style mode
// bits beyond the read-only flag, and os.WriteFile passes perm straight
// through. Returning perm unchanged matches that behaviour exactly.
func applyUmask(perm os.FileMode) os.FileMode {
	return perm
}
