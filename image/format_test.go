package image

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
		{5242880, "5.0 MB"},
		{10485760, "10.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatBytes(tt.input))
		})
	}
}

func TestFormatSignedBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{1, "+1 B"},
		{1024, "+1.0 KB"},
		{1572864, "+1.5 MB"},
		{-1, "-1 B"},
		{-1024, "-1.0 KB"},
		{-3145728, "-3.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatSignedBytes(tt.input))
		})
	}
}

func TestFormatMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     fs.FileMode
		expected string
	}{
		// plain files
		{"regular rw-r--r--", 0644, "-rw-r--r--"},
		{"regular rwxr-xr-x", 0755, "-rwxr-xr-x"},
		{"regular no perms", 0000, "----------"},
		{"regular rwx------", 0700, "-rwx------"},
		// directory
		{"dir rwxr-xr-x", fs.ModeDir | 0755, "drwxr-xr-x"},
		// symlink
		{"symlink", fs.ModeSymlink | 0777, "lrwxrwxrwx"},
		// setuid: execute bit present → 's'; absent → 'S'
		{"setuid with exec", fs.ModeSetuid | 0755, "-rwsr-xr-x"},
		{"setuid no exec", fs.ModeSetuid | 0644, "-rwSr--r--"},
		// setgid: execute bit present → 's'; absent → 'S'
		{"setgid with exec", fs.ModeSetgid | 0755, "-rwxr-sr-x"},
		{"setgid no exec", fs.ModeSetgid | 0644, "-rw-r-Sr--"},
		// sticky: execute bit present → 't'; absent → 'T'
		{"sticky dir with exec", fs.ModeDir | fs.ModeSticky | 0755, "drwxr-xr-t"},
		{"sticky dir no exec", fs.ModeDir | fs.ModeSticky | 0644, "drw-r--r-T"},
		// combined setuid+setgid
		{"setuid+setgid with exec", fs.ModeSetuid | fs.ModeSetgid | 0755, "-rwsr-sr-x"},
		// special node types (device, fifo, socket)
		{"block device", fs.ModeDevice | 0660, "brw-rw----"},
		{"char device", fs.ModeDevice | fs.ModeCharDevice | 0660, "crw-rw----"},
		{"named pipe / fifo", fs.ModeNamedPipe | 0644, "prw-r--r--"},
		{"socket", fs.ModeSocket | 0600, "srw-------"},
		{"irregular", fs.ModeIrregular, "?---------"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatMode(tt.mode))
		})
	}
}

