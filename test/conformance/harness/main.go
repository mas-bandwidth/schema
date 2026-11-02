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
//	harness cross-read            the C++ reference reads what one leg wrote (the nightly)
//	harness coverage              the nightly's coverage table, from its rows
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
             language"). --ports leaves the reference leg out.

  cross-read the nightly's second direction (README.md, "The nightly"): the
             C++ reference loads every answer --lang left under --writes
             (a copy of build/conformance/out/<lang>) on the surfaces that
             write a form it reads back, must accept each and re-save it
             byte-identical, and each must equal C++'s own canonical write.
             --coverage writes its rows. Exits nonzero on any failure.

  coverage   read every coverage row under --in and print the nightly's
             coverage table as markdown (README.md, "The nightly").

  wire-fuzz  mutate every pinned wire in the corpus, feed each mutant to one
             language's leg on a pipe (--driver) and to the compiler's own
             engine, and require the same report, the same decoded value and
             a LoadMeasure inside the framing's bound, for every mutant
             (docs/SPEC-TABLES.md §4.2). --seed and --n size the random pass;
             the enumerated passes run whatever N is.
  cook-differential
             cook every cook-write row's wire with the tool's engine and
             with the C++ reference driver (--driver, default its committed
             leg), in both byte orders, and require the same bytes; the
             committed cook-write files are not read (docs/SPEC-TABLES.md
             §2.8, §2.9, §7.6).

flags:
  --manifest <path>   the conformance manifest
  --json <dir>        where an instance's JSON text lives
  --reports <path>    the generated report expectations
  --drivers <path>    the driver registry: a directory of <lang>/driver
                      commands (the committed one), or a file of
                      "<language> <command...>" lines (a substituted one)
  --work <dir>        scratch: fixtures, driver output, the derived manifest
  --only <lang>       run one registered language (run only)
  --skip <langs>      registered languages this run does not exercise, comma
                      separated, each printed by name; the Makefile fills it
                      from SCHEMA_SKIP_LEGS and the reference leg is refused
                      (run only)
  --claims            fail a case the leg CLAIMS and answers ABSENT: its unit
                      uses a construct the leg's target registers as carried
                      (compiler/carries_<target>.go); build the harness with
                      every schema_leg_<lang> tag (run only)
  --coverage <path>   write the run's coverage rows as JSON (run, cross-read)
  --lang <lang>       the leg whose writes are re-read (cross-read only)
  --writes <dir>      that leg's answers, <surface>/<case> (cross-read only)
  --in <dir>          where the coverage rows are (coverage only)
  --ports             leave the reference leg out of the matrix (matrix only)
  --results <path>    write every case's verdict as JSON, the input of
                      "go run ./tools/matrix proof" (run only)
  --driver <cmd>      the leg to fuzz (wire-fuzz), or the reference driver
                      (cook-differential)
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
	skip := fs.String("skip", "", "registered languages this run does not exercise, comma separated")
	resultsOut := fs.String("results", "", "write every case's verdict to this JSON file, the input of tools/matrix proof (run only)")
	driver := fs.String("driver", "", "the leg to fuzz, as a command (wire-fuzz only)")
	seed := fs.Uint64("seed", 24845619678, "the random pass's seed (wire-fuzz only)")
	n := fs.Int("n", 100000, "the random pass's mutant count (wire-fuzz only)")
	replay := fs.String("replay", "", "one mutant file to run alone (wire-fuzz only)")
	unit := fs.String("unit", "", "the replayed mutant's unit key (wire-fuzz only)")
	root := fs.String("root", "", "the replayed mutant's root table (wire-fuzz only)")
	message := fs.Bool("message", false, "replay the mutant as a MESSAGE against the unit's announced table (docs/SPEC-TABLES.md §3.3; wire-fuzz only)")
	builder := fs.Bool("builder", false, "compare mutable builder completion, reports and successful saved values (wire-fuzz only)")
	retain := fs.Bool("retain", false, "run the RETENTION ARM: both engines' retaining paths over the variable-class file roots (docs/SPEC-TABLES.md §6.6; wire-fuzz only)")
	failed := fs.String("failed", "build/wire-fuzz/failed.bin", "where a failing mutant is written (wire-fuzz only)")
	finiteOwed := fs.String("finite-owed", "", "the docs/roadmap.sexp card that owes this leg the finite float rule (docs/SPEC-TABLES.md §3.3): a mutant the oracle refuses as non_finite_float is counted and not compared (wire-fuzz only)")
	vectors := fs.String("vectors", defaultVectors, "the pinned-vector index (wire-fuzz only)")
	corpus := fs.String("corpus", "tables/cards/conformance", "the card conformance corpus directory (cards only)")
	expected := fs.String("expected", "", "where the card expected answers live, default <corpus>/expected (cards only)")
	require := fs.String("require", "", "comma-separated required card surfaces, default all nine (cards only)")
	claims := fs.Bool("claims", false, "fail a claimed case answered ABSENT (run only)")
	coverage := fs.String("coverage", "", "write coverage rows as JSON (run, cross-read)")
	lang := fs.String("lang", "", "the leg whose writes are re-read (cross-read only)")
	writes := fs.String("writes", "", "that leg's answers (cross-read only)")
	in := fs.String("in", "", "where the coverage rows are (coverage only)")
	ports := fs.Bool("ports", false, "leave the reference leg out (matrix only)")
	if len(os.Args) > 2 {
		_ = fs.Parse(os.Args[2:])
	}

	if command == "matrix" {
		out, err := matrix(*drivers, *ports)
		if err != nil {
			fatalf("%v", err)
		}
		fmt.Println(string(out))
		return
	}

	if command == "coverage" {
		if *in == "" {
			fatalf("coverage: --in names no directory")
		}
		rows, fixed, err := readCoverage(*in)
		if err != nil {
			fatalf("%v", err)
		}
		coverageMarkdown(os.Stdout, rows, fixed)
		return
	}

	if command == "cards" {
		exp := *expected
		if exp == "" {
			exp = filepath.Join(*corpus, "expected")
		}
		// ONE WORK DIRECTORY PER RUN: a negative control and the clean run it
		// sits beside run at once under make -j, and a shared directory let
		// one run's answers stand in for the other's. --work names it; unset,
		// the run makes its own under build/ and removes it after.
		cardsWork, madeWork := "", false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "work" {
				cardsWork = *work
			}
		})
		if cardsWork == "" {
			if err := os.MkdirAll("build", 0o755); err != nil {
				fatalf("%v", err)
			}
			tmp, err := os.MkdirTemp("build", "cards-conformance-")
			if err != nil {
				fatalf("%v", err)
			}
			cardsWork, madeWork = tmp, true
		}
		ok, err := runCards(os.Stdout, *corpus, exp, *driver, *require, cardsWork, nil)
		if madeWork {
			_ = os.RemoveAll(cardsWork)
		}
		if err != nil {
			fatalf("%v", err)
		}
		if !ok {
			os.Exit(1)
		}
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
		if err := wireFuzz(m, wireFuzzOptions{driver: *driver, seed: *seed, n: *n, replay: *replay, unit: *unit, root: *root, message: *message, failed: *failed, vectors: *vectors, retain: *retain, builder: *builder, finiteOwed: *finiteOwed}); err != nil {
			fatalf("%v", err)
		}
	case "cook-differential":
		ok, err := cookDifferential(os.Stdout, m, *manifest, *driver, *work)
		if err != nil {
			fatalf("%v", err)
		}
		if !ok {
			os.Exit(1)
		}
	case "run":
		var opts runOptions
		var rows []coverageRow
		if *coverage != "" {
			opts.coverage = &rows
		}
		if *claims || *coverage != "" {
			set, err := claimsFor(m, *drivers, *only, *claims)
			if err != nil {
				fatalf("%v", err)
			}
			opts.claims = set
		}
		opts.results = *resultsOut
		ok, err := runWith(os.Stdout, m, *manifest, *jsonDir, *reports, *drivers, *work, *only, *skip, opts)
		if err != nil {
			fatalf("%v", err)
		}
		if *coverage != "" {
			if err := writeCoverage(*coverage, rows); err != nil {
				fatalf("%v", err)
			}
		}
		if !ok {
			os.Exit(1)
		}
	case "cross-read":
		if *lang == "" || *writes == "" {
			fatalf("cross-read: --lang and --writes name the leg and its answers")
		}
		set, err := buildClaims(m, nil)
		if err != nil {
			fatalf("%v", err)
		}
		rows, ok, err := crossRead(os.Stdout, m, *reports, *drivers, *work, *lang, *writes, set)
		if err != nil {
			fatalf("%v", err)
		}
		if *coverage != "" {
			if err := writeCoverage(*coverage, rows); err != nil {
				fatalf("%v", err)
			}
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

// claimsFor builds the claim for the legs a run exercises. enforcing is
// --claims: without it the set only classes the coverage rows, and a leg with
// no carries registration in this build is not an error.
func claimsFor(m *Manifest, driversPath, only string, enforcing bool) (*claimSet, error) {
	drivers, _, err := loadDrivers(driversPath)
	if err != nil {
		return nil, err
	}
	var langs []string
	for _, d := range drivers {
		if only != "" && d.lang != only {
			continue
		}
		langs = append(langs, d.lang)
	}
	if !enforcing {
		set, err := buildClaims(m, nil)
		if err != nil {
			return nil, err
		}
		set.carried = map[string]map[string]bool{} // classes only: nothing is claimed
		return set, nil
	}
	return buildClaims(m, langs)
}
