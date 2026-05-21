package image

import (
	"fmt"
	"io/fs"
)

func FormatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)

	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatSignedBytes formats b like FormatBytes but with an explicit sign
// for non-zero values: "+12 MB", "-3.0 MB", "0 B". Used for net deltas.
func FormatSignedBytes(b int64) string {
	if b == 0 {
		return "0 B"
	}
	if b < 0 {
		return "-" + FormatBytes(-b)
	}
	return "+" + FormatBytes(b)
}

func FormatMode(m fs.FileMode) string {
	var buf [10]byte
	if m.IsDir() {
		buf[0] = 'd'
	} else if m&fs.ModeSymlink != 0 {
		buf[0] = 'l'
	} else {
		buf[0] = '-'
	}
	const rwx = "rwx"
	perm := m.Perm()
	for i := 0; i < 9; i++ {
		if perm&(1<<uint(8-i)) != 0 {
			buf[1+i] = rwx[i%3]
		} else {
			buf[1+i] = '-'
		}
	}
	return string(buf[:])
}
