// newleg lays down the skeleton of a new language leg — `schema new-leg
// <lang>` runs it (nova-tools#2498 S5; docs/CONTRIBUTING.md, "Adding a
// language").
//
// A port touches one file or one directory per registry, and finding the
// registries is most of what a new leg used to cost. This tool writes the
// first five of them from templates, each already wired the way the tree
// discovers it:
//
//	compiler/target_<lang>.go         the target, registered from its init
//	internal/codegen/<lang>/<lang>.go the backend: one file per schema file
//	internal/codegen/<lang>/<lang>_test.go the one passing fixture test
//	make/<lang>.mk                    test-<lang>, registered on TEST_LEGS
//	test/<lang>/Fixture.schema        the fixture unit the test and the leg read
//
// so the tree builds, `go test ./internal/codegen/<lang>/` passes, `make
// registry` names the leg and `make test-<lang>` runs green before a line of
// the real emitter is written. The skeleton's backend only names each
// declaration in a comment; the port replaces it, and grows make/<lang>.mk the
// way the other legs grew (a pinned toolchain, the goldens, a conformance
// driver, a negative control per gate).
//
// The tool never overwrites: a leg that exists, or any one of the five files,
// is refused by name and nothing is written.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	fs := flag.NewFlagSet("new-leg", flag.ExitOnError)
	root := fs.String("root", ".", "the schema checkout to lay the leg into")
	ext := fs.String("ext", "", "extension of the files the backend emits (default ."+"<lang>)")
	comment := fs.String("comment", "", "the language's line-comment opener (default from a short table, else //)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: schema new-leg [--root <checkout>] [--ext .x] [--comment //] <lang>")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:]) // ExitOnError: Parse never returns an error
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	written, err := Scaffold(*root, fs.Arg(0), *ext, *comment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema new-leg: %v\n", err)
		os.Exit(1)
	}
	for _, path := range written {
		fmt.Println("wrote " + path)
	}
	fmt.Printf("next: go test ./internal/codegen/%s/ && make test-%s\n", fs.Arg(0), fs.Arg(0))
}
