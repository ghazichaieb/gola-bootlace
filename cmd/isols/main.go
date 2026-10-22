// isols lists folders inside an ISO (a development tool: go run ./cmd/isols file.iso / /boot ...).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package main

import (
	"fmt"
	"os"

	"github.com/ghazichaieb/gola-bootlace/internal/disc"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	st, _ := f.Stat()
	img, err := disc.Open(f, st.Size())
	if err != nil {
		panic(err)
	}
	for _, d := range os.Args[2:] {
		names, err := img.List(d)
		fmt.Println(d, err)
		for _, n := range names {
			fmt.Println("  ", n)
		}
	}
}
