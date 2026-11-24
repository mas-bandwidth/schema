// Command conformancerows runs EVERY row file of one leg's conformance rows
// directory and holds the failures to the leg's owed-row ledger.
//
//	conformancerows -leg c [-shard 0/2] [-j 8]
//
// A row under test/conformance/<leg>/rows/ is a self-contained program that
// drives one law of docs/FIXED-FORM-ALGORITHM.md through the generated code.
// The list of rows a leg runs is the directory itself: there is no list to
// copy by hand, so a row file added is a row run. A row the leg cannot answer
// today is named, with its reason, in test/conformance/<leg>/rows-owed.tsv
// (ledger.go), and the ledger is matched as an exact set: a failing row it
// does not name is red, and a row it names that passes is red until its line
// is removed. Nothing is skipped: an owed row still runs, so the ledger can
// never outlive the failure it excuses.
//
// How a row is built is read off the row itself, never off a list: the C and
// C++ rows by their #include lines (each generated header is found in the
// generated trees, and its unit's table source is linked beside the row), the
// Rust rows as integration tests of one probe crate that depends on every
// generated crate a row names, and the scripted legs (Dart, Java, JavaScript,
// Elixir) and the C# projects by their own toolchain's single-file run. The
// generated trees and toolchains come from make (make/conformance-rows.mk),
// which passes them in the environment.
//
// -shard i/n runs the rows of shard i (every n-th row, sorted by name) so a
// leg's rows fit the per-commit budget across parallel CI jobs; every row is
// in exactly one shard.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func main() {
	leg := flag.String("leg", "", "the leg whose rows run: c, cpp, cs, dart, elixir, java, js or rust")
	shard := flag.String("shard", "0/1", "run shard i of n: i/n")
	jobs := flag.Int("j", runtime.NumCPU(), "rows built and run at once")
	root := flag.String("root", ".", "the repository root")
	measured := flag.String("measured", "", "also write every failing row of the run as a ledger line `<row><TAB>owed: <measured cause>` to this file, for a person to read and copy; the ledger itself is never written")
	flag.Parse()
	if err := run(*root, *leg, *shard, *jobs, *measured); err != nil {
		fmt.Fprintf(os.Stderr, "conformancerows %s: %v\n", *leg, err)
		os.Exit(1)
	}
}

// a recipe knows how one leg names, prepares and runs its rows.
type recipe struct {
	ext string   // the row file's extension; "" for a directory (C#)
	env []string // what make must pass: an unset one stops the run before any row
	// prepare runs once before the rows of the shard (Rust builds every row
	// as one cargo package here). It may record per-row failures.
	prepare func(c *ctx, rows []string) error
	// row builds and runs one row; the error is the row's failure.
	row func(c *ctx, row string) error
	// canFail, on a scripted leg, reports whether a row's source has a way
	// to fail; a row that passes without one is red (failpath.go).
	canFail func(src string) bool
}

var recipes = map[string]recipe{
	"c":      {ext: ".c", env: []string{"CC", "SERIALIZE_C"}, row: rowC},
	"cpp":    {ext: ".cpp", env: []string{"CXX", "SERIALIZE"}, row: rowCpp},
	"cs":     {ext: "", env: []string{"DOTNET", "SERIALIZE_CS"}, row: rowCs},
	"dart":   {ext: ".dart", env: []string{"DART"}, row: rowDart, canFail: canFailDart},
	"elixir": {ext: ".exs", env: []string{"ELIXIR"}, row: rowElixir, canFail: canFailElixir},
	"java":   {ext: ".java", env: []string{"JAVA", "JAVAC"}, prepare: prepareJava, row: rowJava, canFail: canFailJava},
	"js":     {ext: ".mjs", env: []string{"NODE"}, row: rowJs, canFail: canFailJs},
	"rust":   {ext: ".rs", env: []string{"CARGO", "SCHEMA"}, prepare: prepareRust, row: rowRust},
}

// ctx is one run's shared state.
type ctx struct {
	root    string
	leg     string
	rowsDir string
	work    string
	mu      sync.Mutex
	pre     map[string]error  // failures recorded by prepare
	exe     map[string]string // Rust: row -> built test executable
	javaCP  map[string]string // Java: row -> the classpath of the units it names
}

func (c *ctx) rowPath(row, ext string) string { return filepath.Join(c.rowsDir, row+ext) }

// rowSource is the row's source text: its file, or for a C# row every file
// of its directory. An unreadable row reads as empty.
func (c *ctx) rowSource(rc recipe, row string) string {
	if rc.ext != "" {
		data, _ := os.ReadFile(c.rowPath(row, rc.ext))
		return string(data)
	}
	var b strings.Builder
	ents, _ := os.ReadDir(filepath.Join(c.rowsDir, row))
	for _, e := range ents {
		if !e.IsDir() {
			data, _ := os.ReadFile(filepath.Join(c.rowsDir, row, e.Name()))
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func run(root, leg, shard string, jobs int, measured string) error {
	rc, ok := recipes[leg]
	if !ok {
		return fmt.Errorf("no row recipe for leg %q", leg)
	}
	i, n, err := parseShard(shard)
	if err != nil {
		return err
	}
	if jobs < 1 {
		jobs = 1
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	c := &ctx{root: abs, leg: leg, rowsDir: filepath.Join(abs, "test", "conformance", leg, "rows"),
		work: filepath.Join(abs, "build", "conformance-rows", leg), pre: map[string]error{}, exe: map[string]string{}}
	all, err := listRows(c.rowsDir, rc.ext)
	if err != nil {
		return err
	}
	for _, name := range rc.env {
		if _, err := env(name); err != nil {
			return err
		}
	}
	if len(all) == 0 {
		return fmt.Errorf("%s holds no row file: a leg with no rows has no rows target", c.rowsDir)
	}
	ledgerPath := filepath.Join("test", "conformance", leg, "rows-owed.tsv")
	var ledger map[string]string
	if data, rerr := os.ReadFile(filepath.Join(abs, ledgerPath)); rerr == nil {
		if ledger, err = parseLedger(bytes.NewReader(data), ledgerPath); err != nil {
			return err
		}
	} else if !errors.Is(rerr, fs.ErrNotExist) {
		return rerr
	}
	ran := shardOf(all, i, n)
	if len(ran) == 0 {
		return fmt.Errorf("shard %d/%d of %d rows is empty: use fewer shards", i, n, len(all))
	}
	if err := os.MkdirAll(c.work, 0o755); err != nil {
		return err
	}
	// A ledger line must say what failed (cause.go): a banner or a line of
	// the row's own source is refused, in every shard.
	var refused []string
	for _, row := range all {
		if why, ok := ledger[row]; ok {
			if err := checkCause(row, why, c.rowSource(rc, row)); err != nil {
				refused = append(refused, err.Error())
			}
		}
	}
	if rc.prepare != nil {
		if err := rc.prepare(c, ran); err != nil {
			return err
		}
	}
	failed := map[string]bool{}
	reasons := map[string]string{}
	causes := map[string]string{}
	var mu sync.Mutex
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for _, row := range ran {
		wg.Add(1)
		sem <- struct{}{}
		go func(row string) {
			defer wg.Done()
			defer func() { <-sem }()
			src := c.rowSource(rc, row)
			rerr := c.pre[row]
			built := rerr == nil
			if built {
				rerr = rc.row(c, row)
			}
			why := ""
			switch {
			case rerr != nil:
				log := rerr.Error()
				if data, lerr := os.ReadFile(filepath.Join(c.work, row+".log")); built && lerr == nil && len(data) > 0 {
					log = string(data)
				}
				why = cause(c.root, src, log)
			case rc.canFail != nil && !rc.canFail(src):
				// A scripted row that exits 0 and has no way to exit
				// otherwise checked nothing: it is the scripted legs'
				// twin of a Rust test binary that ran no test.
				why = canFailNote
				rerr = fmt.Errorf("%s", why)
			}
			mu.Lock()
			defer mu.Unlock()
			if rerr != nil {
				failed[row] = true
				reasons[row] = rerr.Error()
				causes[row] = why
			}
		}(row)
	}
	wg.Wait()
	pass, owed := 0, 0
	for _, row := range ran {
		_, isOwed := ledger[row]
		switch {
		case !failed[row] && !isOwed:
			pass++
			fmt.Printf("PASS %s/%s\n", leg, row)
		case failed[row] && isOwed:
			owed++
			fmt.Printf("OWED %s/%s — %s (still red: %s)\n", leg, row, ledger[row], causes[row])
		case failed[row]:
			fmt.Printf("FAIL %s/%s — %s\n%s\n", leg, row, causes[row], indent(reasons[row]))
		default:
			fmt.Printf("PASSES-BUT-OWED %s/%s — %s\n", leg, row, ledger[row])
		}
	}
	fmt.Printf("conformance rows %s, shard %d/%d: %d of %d row files ran, %d passed, %d owed (ledgered in %s, ran and still red)\n",
		leg, i, n, len(ran), len(all), pass, owed, ledgerPath)
	if measured != "" {
		var b strings.Builder
		for _, row := range ran {
			if failed[row] {
				fmt.Fprintf(&b, "%s\towed: %s\n", row, causes[row])
			}
		}
		if err := os.WriteFile(measured, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	verr := verdict(all, ran, failed, ledger, ledgerPath)
	if len(refused) > 0 {
		sort.Strings(refused)
		msg := fmt.Sprintf("%s: %d ledger lines name no failure:\n  %s", ledgerPath, len(refused), strings.Join(refused, "\n  "))
		if verr != nil {
			msg += "\n" + verr.Error()
		}
		return errors.New(msg)
	}
	return verr
}

func parseShard(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "/")
	i, e1 := strconv.Atoi(a)
	n, e2 := strconv.Atoi(b)
	if !ok || e1 != nil || e2 != nil || n < 1 || i < 0 || i >= n {
		return 0, 0, fmt.Errorf("-shard %q: want i/n with 0 <= i < n", s)
	}
	return i, n, nil
}

// listRows names every row of the directory: a file with the extension, or
// for ext "" every subdirectory.
func listRows(dir, ext string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var rows []string
	for _, e := range ents {
		switch {
		case ext == "" && e.IsDir():
			rows = append(rows, e.Name())
		case ext != "" && !e.IsDir() && strings.HasSuffix(e.Name(), ext):
			rows = append(rows, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(rows)
	return rows, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], fmt.Sprintf("... (%d more lines in the row's log)", len(lines)-40))
	}
	return "    " + strings.Join(lines, "\n    ")
}

// env reads a toolchain or tree the make recipe passes; unset is an error
// naming the variable, never a default that hides a missing toolchain.
func env(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is not set (make/conformance-rows.mk passes it)", name)
	}
	return v, nil
}
