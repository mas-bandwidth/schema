// Command vacuoustests lists Go tests that run and pass while checking
// nothing. The rules, and which of them are a gate, are in internal/vacuous.
//
//	go run ./tools/vacuoustests          the gate: refused findings, exit 1 on any
//	go run ./tools/vacuoustests -all     every class, with a count per class
//	go run ./tools/vacuoustests -fix     write the missing non-empty check before
//	                                     every class 2 loop that can take one
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/vacuous"
)

func main() {
	root := flag.String("root", ".", "the tree to analyze")
	all := flag.Bool("all", false, "list every class, not only the gated ones")
	fix := flag.Bool("fix", false, "write the missing non-empty check before every class 2 loop that can take one")
	flag.Parse()
	if *fix {
		n, err := vacuous.Fix(*root)
		fmt.Fprintf(os.Stderr, "vacuoustests: wrote %d non-empty checks\n", n)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vacuoustests: %v\n", err)
			os.Exit(2)
		}
		return
	}
	status, _ := vacuous.Run(*root, *all, os.Stdout, os.Stderr)
	os.Exit(status)
}
