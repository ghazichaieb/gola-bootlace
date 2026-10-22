//go:build !linux

// bootlace-nbd only works on Linux (it uses the kernel's network block device).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "bootlace-nbd runs on Linux only")
	os.Exit(1)
}
