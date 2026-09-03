// The tables conformance harness (docs/SPEC-TABLES.md §3, §4, §7, §16, §19).
//
// A port of the tables layer is "make the driver pass". The DATA lives under
// testdata/conformance/tables and names no language; the CONTRACT lives in
// test/conformance/README.md; this tool generates the data, materializes the
// fixtures a driver cannot carry as text, runs every registered driver over
// every surface, and prints the matrix.
//
//	harness generate              rewrite the generated half of the data
//	harness run                   run every registered driver, print the matrix
//
// Flags are the same for both so a Makefile line reads the same way; see usage.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultManifest = "testdata/conformance/tables/MANIFEST.txt"
	defaultJSONDir  = "testdata/conformance/tables/json"
	defaultReports  = "testdata/conformance/tables/reports.txt"
	defaultDrivers  = "test/conformance/drivers.txt"
	defaultWork     = "build/conformance"
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "harness: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: harness <generate|run> [flags]

  generate   rewrite the generated half of testdata/conformance/tables:
             the JSON text of every instance, and the read report of every
             evolution case. Both come from the compiler's own engine, which
             is a third implementation of the two forms.

  pin        rewrite the goldens a driver writes rather than the engine: the
             cook's canonical node dump, and the block forgery battery
             resolved to byte offsets. Both come from the FIRST driver in the
             registry, which is the reference leg.

  run        materialize the fixtures, run every driver in the registry over
             every surface it lists, compare against the data, and print the
             per-language matrix. Exits nonzero if any registered surface
             fails.

flags:
  --manifest <path>   the conformance manifest
  --json <dir>        where an instance's JSON text lives
  --reports <path>    the generated report expectations
  --drivers <path>    the driver registry (run only)
  --work <dir>        scratch: fixtures, driver output, the derived manifest
  --only <lang>       run one registered language (run only)
`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	command := os.Args[1]

	fs := flag.NewFlagSet(command, flag.ExitOnError)
	manifest := fs.String("manifest", defaultManifest, "the conformance manifest")
	jsonDir := fs.String("json", defaultJSONDir, "where an instance's JSON text lives")
	reports := fs.String("reports", defaultReports, "the generated report expectations")
	drivers := fs.String("drivers", defaultDrivers, "the driver registry")
	work := fs.String("work", defaultWork, "scratch directory")
	only := fs.String("only", "", "run one registered language")
	if len(os.Args) > 2 {
		_ = fs.Parse(os.Args[2:])
	}

	m, err := ReadManifest(*manifest, *jsonDir)
	if err != nil {
		fatalf("%v", err)
	}

	switch command {
	case "generate":
		if err := generate(m, *jsonDir, *reports); err != nil {
			fatalf("%v", err)
		}
	case "pin":
		if err := pin(m, *drivers, *work, "testdata/conformance/tables/cook"); err != nil {
			fatalf("%v", err)
		}
	case "run":
		ok, err := run(m, *manifest, *jsonDir, *reports, *drivers, *work, *only)
		if err != nil {
			fatalf("%v", err)
		}
		if !ok {
			os.Exit(1)
		}
	default:
		usage()
	}
}

// repoRelative keeps every path in the data and in a driver's argv relative to
// the repository root, because a golden that carries an absolute path is a
// golden that only passes on one machine.
func repoRelative(path string) string {
	if filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(wd, path); err == nil {
				return rel
			}
		}
	}
	return filepath.ToSlash(path)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func indent(text string, by string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := range lines {
		lines[i] = by + lines[i]
	}
	return strings.Join(lines, "\n")
}
