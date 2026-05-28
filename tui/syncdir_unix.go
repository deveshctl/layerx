//go:build unix

package tui

import "os"

// syncDir fsyncs the directory containing a freshly-renamed file so the
// rename survives power loss on filesystems where the directory inode is
// journaled separately from the rename metadata (ext4 default, btrfs).
//
// Errors are intentionally swallowed: this is a best-effort durability
// step. On filesystems where Sync on a directory fd returns EINVAL (some
// FUSE mounts, network filesystems), failure is benign — the rename has
// already returned, and reporting it here would propagate spurious errors
// to a save the user has been told succeeded.
func syncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	defer f.Close()
	_ = f.Sync()
}
