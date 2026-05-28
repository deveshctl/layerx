//go:build windows

package tui

// syncDir is a no-op on Windows: the OS does not expose directory-fsync as
// a primitive, and NTFS journals MFT updates (including the rename of the
// temp into place) atomically as part of os.Rename. The atomic-replace
// guarantee atomicWriteFile advertises is therefore upheld by NTFS itself
// rather than by an explicit sync here.
func syncDir(dir string) {}
