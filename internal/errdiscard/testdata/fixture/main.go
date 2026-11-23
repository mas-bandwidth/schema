// Package main is the negative control of internal/errdiscard: each line
// marked REFUSED drops an error the gate must name, each marked FINE does not.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func refused(w io.Writer) {
	_ = os.Remove("a")             // REFUSED: assigned to _
	b, _ := os.ReadFile("b")       // REFUSED: assigned to _
	os.Remove("c")                 // REFUSED: not looked at
	_, _ = fmt.Fprintln(w, len(b)) // REFUSED: a writer that is not stdout
	_ = os.Remove("d")             // errdiscard:
	go os.Remove("e")              // REFUSED: dropped by go
	f, err := os.Create("f")
	if err != nil {
		return
	}
	defer f.Close() // REFUSED: dropped by defer, a file written through
	cont := flag.NewFlagSet("x", flag.ContinueOnError)
	_ = cont.Parse(nil) // REFUSED: ContinueOnError returns the error
}

func fine(stdout io.Writer) {
	fmt.Fprintf(os.Stderr, "x\n")
	fmt.Println("y")
	_, _ = fmt.Fprintln(stdout, "z")
	var sb strings.Builder
	sb.WriteString("w")
	r, err := os.Open("g")
	if err != nil {
		return
	}
	defer r.Close()
	exit := flag.NewFlagSet("y", flag.ExitOnError)
	_ = exit.Parse(nil)
	// errdiscard: a reason on the line above
	_ = os.Remove("h")
	_ = os.Remove("i") // errdiscard: a reason on the same line
}

func main() {
	refused(os.Stdout)
	fine(os.Stdout)
}
