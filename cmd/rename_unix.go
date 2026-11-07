//go:build !windows

package cmd

import "os"

func renameJSON(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
