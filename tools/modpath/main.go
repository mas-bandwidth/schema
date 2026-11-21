// Command modpath moves this repository's Go module path across the whole
// tree, and checks that the move is complete.
//
//	go run ./tools/modpath [-from PATH] [-to PATH] [-first-v3-tag TAG] [-check] [-min-files N] ROOT
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
// -first-v3-tag is the one parameter of the default move: the release tag
// that will be the first cut with the /v3 go.mod. The move sets
// internal/modulemove.FirstV3ModuleTag to it and writes it into the pages
// and the pin corpus that name the boundary (modpath.Fixups). It defaults to
// FirstV3ModuleTag as built, so -check on a moved tree needs no flag; on an
// unmoved tree that is unset, and the default move refuses to run without it.
//
// Exit codes: 0 done (or clean under -check), 1 something to rewrite, a
// problem reported, or (when writing) an edit that found neither its old nor
// its new text, 2 bad usage, an I/O error, or a ROOT that is not the
// repository (no root go.mod, or fewer than -min-files files; ROOT is resolved
// through symlinks first).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mas-bandwidth/schema/v2/internal/modpath"
	"github.com/mas-bandwidth/schema/v2/internal/modulemove"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, modulemove.FirstV3ModuleTag))
}

// run is the command; built is the FirstV3ModuleTag this binary carries.
func run(args []string, stdout, stderr io.Writer, built string) int {
	fs := flag.NewFlagSet("modpath", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", modpath.DefaultFrom, "the module path to move away from")
	to := fs.String("to", modpath.DefaultTo, "the module path to move to")
	first := fs.String("first-v3-tag", built, "the default move only: the release tag that will be the first cut with the /v3 go.mod")
	check := fs.Bool("check", false, "change nothing; list every file:line that would change and exit 1 if any")
	minFiles := fs.Int("min-files", 1000, "refuse a ROOT where the walk reads fewer files than this (a wrong or empty root never passes)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: go run ./tools/modpath [-from PATH] [-to PATH] [-first-v3-tag TAG] [-check] [-min-files N] ROOT")
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
	firstSet := false
	fs.Visit(func(f *flag.Flag) { firstSet = firstSet || f.Name == "first-v3-tag" })
	switch defaultMove := *from == modpath.DefaultFrom && *to == modpath.DefaultTo; {
	case defaultMove && *first == "":
		_, _ = fmt.Fprintln(stderr, "modpath: the default move needs -first-v3-tag, the release tag that will be the first cut with the /v3 go.mod (the next unreleased v3.x.y)")
		return 2
	case defaultMove || firstSet:
		if err := r.SetFirstV3Tag(*first); err != nil {
			_, _ = fmt.Fprintf(stderr, "modpath: -first-v3-tag: %v\n", err)
			return 2
		}
	}
	rep, err := r.Run(fs.Arg(0), *check, *minFiles)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "modpath: %v\n", err)
		return 2
	}
	rep.Print(stdout, *check)
	if len(rep.Problems) > 0 || *check && !rep.Clean() || !*check && len(rep.Notes) > 0 {
		return 1
	}
	return 0
}
