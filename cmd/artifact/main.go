package main

import (
	"fmt"
	"os"

	"artifactd/internal/cli"
)

func main() {
	if err := cli.NewCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
