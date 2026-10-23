// Settings from the environment: every option of "bootlace serve" can also be given as BOOTLACE_<NAME> (the name in capitals, dashes as underscores), for
// example BOOTLACE_HTTP_PORT=8080 or BOOTLACE_DHCP=proxy. This is how a container is set up without a command line. An option given on the command line wins.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// envName is the environment variable of an option.
func envName(option string) string {
	return "BOOTLACE_" + strings.ToUpper(strings.ReplaceAll(option, "-", "_"))
}

// applyEnv gives each option that is set in the environment its value, before the command line is read (so that the command line wins). A value that is not
// valid for its option is an error that names the variable.
func applyEnv(fs *flag.FlagSet, lookup func(string) (string, bool)) error {
	var err error
	fs.VisitAll(func(f *flag.Flag) {
		v, ok := lookup(envName(f.Name))
		if !ok || v == "" || err != nil {
			return
		}
		if serr := f.Value.Set(v); serr != nil {
			err = fmt.Errorf("%s=%q is not valid: %v", envName(f.Name), v, serr)
		}
	})
	return err
}

func osLookup(name string) (string, bool) { return os.LookupEnv(name) }

// health asks the server that runs on this computer whether it is up, for a container's health check: "bootlace health" exits 0 when the web server answers.
// The port is the one of BOOTLACE_HTTP_PORT (8080 unless set), or -port.
func health(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootlace health", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 8080, "the web port of the server")
	if v := os.Getenv(envName("http-port")); v != "" {
		_ = fs.Set("port", v)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", *port))
	if err != nil {
		fmt.Fprintf(stderr, "bootlace: no answer: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "bootlace: the server answered with error %d\n", resp.StatusCode)
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}
