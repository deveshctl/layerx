package atomicfile

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Windows may briefly lock the destination while another writer replaces it.
// Retry those errors without removing the destination or exposing partial data.
func Rename(oldPath, newPath string) error {
	for attempt := 0; ; attempt++ {
		err := os.Rename(oldPath, newPath)
		if attempt == 5 || (!errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION)) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
