package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Jevs21/cctop/internal/tui"
)

func main() {
	onceMode := flag.Bool("once", false, "Print the table once and exit (no live refresh)")
	debugMode := flag.Bool("debug", false, "Print timing diagnostics to stderr")

	// Support -1 as an alias for --once
	flag.BoolVar(onceMode, "1", false, "Alias for --once")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "cctop — Claude Session Monitor\n\n")
		fmt.Fprintf(os.Stderr, "Usage: cctop [OPTIONS]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  --once, -1    Print the table once and exit (no live refresh)\n")
		fmt.Fprintf(os.Stderr, "  --debug       Print timing diagnostics to stderr\n")
		fmt.Fprintf(os.Stderr, "  -h, --help    Show usage information\n")
	}

	flag.Parse()

	if err := tui.Run(*onceMode, *debugMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
