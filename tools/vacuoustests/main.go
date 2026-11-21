// Command vacuoustests lists Go tests that run and pass while checking
// nothing. The rules, and which of them are a gate, are in internal/vacuous.
//
//	go run ./tools/vacuoustests          the gate: refused findings, exit 1 on any
//	go run ./tools/vacuoustests -all     every class, with a count per class
//
// There is no automatic fix: whether a finding is a test that checks nothing
// or a test whose empty case is the pass needs a reader.
package main

import (
	"flag"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/vacuous"
)

func main() {
	root := flag.String("root", ".", "the tree to analyze")
	all := flag.Bool("all", false, "list every class, not only the gated ones")
	flag.Parse()
	status, _ := vacuous.Run(*root, *all, os.Stdout, os.Stderr)
	os.Exit(status)
}
