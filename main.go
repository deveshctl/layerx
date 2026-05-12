package main

import (
	"os"

	"github.com/deveshpharswan/layerx/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
