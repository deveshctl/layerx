package main

import (
	"errors"
	"os"

	"github.com/deveshctl/layerx/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	err := cmd.Execute()
	if err == nil {
		return
	}
	if _, ok := errors.AsType[*cmd.ErrCIFailed](err); ok {
		os.Exit(1)
	}
	os.Exit(2)
}
