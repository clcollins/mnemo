package main

import (
	"os"

	"github.com/clcollins/mnemo/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
