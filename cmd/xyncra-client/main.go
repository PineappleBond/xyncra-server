package main

import (
	"fmt"
	"os"

	"github.com/PineappleBond/xyncra-server/internal/cli"
)

// Version information, injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// Handle --version / -version flag before cobra parses args.
	for _, arg := range os.Args[1:] {
		if arg == "-version" || arg == "--version" {
			fmt.Printf("xyncra-client version %s (%s) built %s\n", version, commit, buildTime)
			return
		}
	}

	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
