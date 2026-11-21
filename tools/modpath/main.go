// Command modpath moves this repository's Go module path across the whole
// tree, and checks that the move is complete.
//
//	go run ./tools/modpath [-from PATH] [-to PATH] [-check] [-min-files N] ROOT
//
// Without -check it rewrites every spelling of -from under ROOT to -to (Go
// imports and string literals in every build configuration, go.mod, go.sum,
// the emitters and templates that write the path into generated output, the
// committed generated trees, make files, workflows, scripts and current
// docs), prints one line per file it changed and a total, and exits 0. A
// second run changes nothing and says so.
//
// With -check it changes nothing, prints every offending file:line and exits
// 1 when anything would change or a problem is reported; it exits 0 on a
// moved tree.
//
// -from defaults to modpath.DefaultFrom (this module's /v2 path) and -to to
// modpath.DefaultTo (its /v3 path); another move (a renamed product) is the
// same program with other flags. What is matched, how versions written after
// the path are fixed, and the allowlist of records that keep their text are
// in internal/modpath. internal/ci/modpath_test.go fails when a spelling of
// the -from default comes back.
//
// Exit codes: 0 done (or clean under -check), 1 something to rewrite or a
// problem reported, 2 bad usage, an I/O error, or a ROOT that is not the
// repository (no root go.mod, or fewer than -min-files files; ROOT is resolved
// through symlinks first).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/modpath"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("modpath", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", modpath.DefaultFrom, "the module path to move away from")
	to := fs.String("to", modpath.DefaultTo, "the module path to move to")
	check := fs.Bool("check", false, "change nothing; list every file:line that would change and exit 1 if any")
	minFiles := fs.Int("min-files", 1000, "refuse a ROOT where the walk reads fewer files than this (a wrong or empty root never passes)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: go run ./tools/modpath [-from PATH] [-to PATH] [-check] [-min-files N] ROOT")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	r, err := modpath.New(*from, *to)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "modpath: %v\n", err)
		return 2
	}
	rep, err := r.Run(fs.Arg(0), *check, *minFiles)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "modpath: %v\n", err)
		return 2
	}
	rep.Print(stdout, *check)
	if len(rep.Problems) > 0 || *check && !rep.Clean() {
		return 1
	}
	return 0
}
