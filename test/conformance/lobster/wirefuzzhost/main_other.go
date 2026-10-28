//go:build !unix

// wirefuzzhost talks to the Lobster program through a named pipe, which only
// a unix host has; the Lobster leg's wire fuzz runs on the Linux and macOS
// runners (make/lobster-fuzz.mk).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "wirefuzzhost: the Lobster wire-fuzz host needs a unix host (a named pipe carries each reply)")
	os.Exit(1)
}
