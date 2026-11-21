//go:build !windows

package atomicfile

import "os"

func Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
