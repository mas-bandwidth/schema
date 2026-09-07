// The tables conformance harness (docs/SPEC-TABLES.md §3, §4, §7, §16, §19).
//
// A port of the tables layer is "make the driver pass". The DATA lives under
// testdata/conformance/tables and names no language; the CONTRACT lives in
// test/conformance/README.md; this tool generates the data, materialises the
// fixtures a driver cannot carry as text, runs every registered driver over
// every surface, and prints the matrix.
//
//	harness generate              rewrite the generated half of the data
//	harness run                   run every registered driver, print the matrix
//	harness matrix                print the CI matrix the registry implies
//	harness wire-fuzz             fuzz one language's tolerant wire read against the engine
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
	defaultDrivers  = defaultDriversDir
	defaultWork     = "build/conformance"
	defaultVectors  = "testdata/wire/tables/fuzz-vectors/INDEX.txt"
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

  run        materialise the fixtures, run every driver in the registry over
             every surface it lists, compare against the data, and print the
             per-language matrix. Exits nonzero if any registered surface
             fails.

  matrix     print the CI matrix as JSON: one row per registered driver, from
             test/conformance/<lang>/ci.json (README.md, "Registering a
             language").

  wire-fuzz  mutate every pinned wire in the corpus, feed each mutant to one
             language's leg on a pipe (--driver) and to the compiler's own
             engine, and require the same report, the same decoded value and
             a LoadMeasure inside the framing's bound, for every mutant
             (docs/SPEC-TABLES.md §4.2). --seed and --n size the random pass;
             the enumerated passes run whatever N is.

flags:
  --manifest <path>   the conformance manifest
  --json <dir>        where an instance's JSON text lives
  --reports <path>    the generated report expectations
  --drivers <path>    the driver registry: a directory of <lang>/driver
                      commands (the committed one), or a file of
                      "<language> <command...>" lines (a substituted one)
  --work <dir>        scratch: fixtures, driver output, the derived manifest
  --only <lang>       run one registered language (run only)
  --driver <cmd>      the leg to fuzz (wire-fuzz only)
  --seed <S> --n <N>  the random pass (wire-fuzz only)
  --replay <file> --unit <key> --root <table> [--message]
                      run one saved mutant alone (wire-fuzz only)
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
	driver := fs.String("driver", "", "the leg to fuzz, as a command (wire-fuzz only)")
	seed := fs.Uint64("seed", 24845619678, "the random pass's seed (wire-fuzz only)")
	n := fs.Int("n", 100000, "the random pass's mutant count (wire-fuzz only)")
	replay := fs.String("replay", "", "one mutant file to run alone (wire-fuzz only)")
	unit := fs.String("unit", "", "the replayed mutant's unit key (wire-fuzz only)")
	root := fs.String("root", "", "the replayed mutant's root table (wire-fuzz only)")
	message := fs.Bool("message", false, "replay the mutant as a MESSAGE against the unit's announced table (docs/SPEC-TABLES.md §3.3; wire-fuzz only)")
	retain := fs.Bool("retain", false, "run the RETENTION ARM: both engines' retaining paths over the variable-class file roots (docs/SPEC-TABLES.md §6.6; wire-fuzz only)")
	failed := fs.String("failed", "build/wire-fuzz/failed.bin", "where a failing mutant is written (wire-fuzz only)")
	vectors := fs.String("vectors", defaultVectors, "the pinned-vector index (wire-fuzz only)")
	if len(os.Args) > 2 {
		_ = fs.Parse(os.Args[2:])
	}

	if command == "matrix" {
		out, err := matrix(*drivers)
		if err != nil {
			fatalf("%v", err)
		}
		fmt.Println(string(out))
		return
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
	case "wire-fuzz":
		if err := wireFuzz(m, wireFuzzOptions{driver: *driver, seed: *seed, n: *n, replay: *replay, unit: *unit, root: *root, message: *message, failed: *failed, vectors: *vectors, retain: *retain}); err != nil {
			fatalf("%v", err)
		}
	case "run":
		ok, err := run(os.Stdout, m, *manifest, *jsonDir, *reports, *drivers, *work, *only)
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
