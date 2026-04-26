package main

import (
	"fmt"
	"io"
	"os"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "hello from ghd-example")
		return 0
	}

	switch args[0] {
	case "--help", "-h", "help":
		printUsage(stdout)
		return 0
	case "--version", "version":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "%s accepts no arguments\n", args[0])
			return 2
		}
		fmt.Fprintf(stdout, "ghd-example %s (%s) built %s\n", version, commit, date)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: ghd-example [version|--version]")
}
