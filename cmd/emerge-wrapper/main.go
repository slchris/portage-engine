// Package main provides emerge wrapper command.
package main

import (
	"fmt"
	"os"

	"github.com/slchris/portage-engine/internal/emerge"
)

func main() {
	config, err := emerge.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	wrapper, err := emerge.NewWrapper(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create wrapper: %v\n", err)
		os.Exit(1)
	}

	if err := wrapper.Execute(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
