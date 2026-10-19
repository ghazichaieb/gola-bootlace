// Gola Bootlace: a network boot server for ISO files.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ghazichaieb/gola-bootlace/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process exit, so tests can call it.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootlace", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version.String())
		return 0
	}
	fmt.Fprintln(stderr, "Gola Bootlace is in early design: there is nothing to run yet. See docs/start-plan.md.")
	return 1
}
