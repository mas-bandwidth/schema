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
// -shard i/n runs the rows of shard i (every n-th row, sorted by name), for
// a person splitting a run by hand; every row is in exactly one shard.
//
// -rows "F4 F10 F12" runs only the rows named (each must be a row file of the
// leg, or the run is refused; -rows with a -shard other than 0/1 is refused,
// since a shard would drop some of them silently), against the same ledger and the same cause
// match: this is a per-PR verify target's narrow run (make/conformance-rows.mk,
// CONFORMANCE_ROWS_PR_<leg>). -gen DIR reads the leg's generated units from
// DIR, a tree holding only the units those rows read, in place of the leg's
// whole generated tree (C, Java and Rust; the C++ and Dart rows name their
// tree's path themselves). -work DIR keeps the run's build products apart
// from the whole leg's, so the two never share a probe crate or a log.
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
	rows := flag.String("rows", "", "run only these rows (names separated by spaces or commas); a name with no row file is refused")
	gen := flag.String("gen", "", "the generated tree the rows read, relative to -root, in place of the leg's whole one (c, java and rust only)")
	work := flag.String("work", "", "the directory for the run's build products (default build/conformance-rows/<leg>)")
	flag.Parse()
	if err := run(opts{root: *root, leg: *leg, shard: *shard, jobs: *jobs, measured: *measured, rows: *rows, gen: *gen, work: *work}); err != nil {
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
	// canFail, on every leg but Rust (whose test binary must run a test),
	// reports whether a row's source has a way to fail; a row that passes
	// without one is red (failpath.go).
	canFail func(src string) bool
}

var recipes = map[string]recipe{
	"c":      {ext: ".c", env: []string{"CC", "SERIALIZE_C"}, row: rowC, canFail: canFailNative},
	"cpp":    {ext: ".cpp", env: []string{"CXX", "SERIALIZE"}, row: rowCpp, canFail: canFailNative},
	"cs":     {ext: "", env: []string{"DOTNET", "SERIALIZE_CS"}, row: rowCs, canFail: canFailCs},
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
	gen     string            // -gen: the generated tree, relative to root; "" is the leg's own
}

// genTree is the generated tree a recipe reads: -gen when given, else the
// leg's whole tree, relative to the root.
func (c *ctx) genTree(whole string) string {
	if c.gen != "" {
		return c.gen
	}
	return whole
}

// opts are the command's flags.
type opts struct {
	root, leg, shard string
	jobs             int
	measured, rows   string
	gen, work        string
}

// genLegs are the legs whose recipe reads its generated tree from one root
// that -gen can replace.
var genLegs = map[string]bool{"c": true, "java": true, "rust": true}

// pickRows is the rows -rows names, in the leg's order; a name that is not a
// row of the leg, a name given twice, or an empty list is refused.
func pickRows(all []string, list string) ([]string, error) {
	names := strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(names) == 0 {
		return nil, fmt.Errorf("-rows %q names no row", list)
	}
	have := map[string]bool{}
	for _, r := range all {
		have[r] = true
	}
	want := map[string]bool{}
	var bad []string
	for _, n := range names {
		switch {
		case !have[n]:
			bad = append(bad, n+" (no row file)")
		case want[n]:
			bad = append(bad, n+" (named twice)")
		}
		want[n] = true
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("-rows refused: %s", strings.Join(bad, ", "))
	}
	var picked []string
	for _, r := range all {
		if want[r] {
			picked = append(picked, r)
		}
	}
	return picked, nil
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

func run(o opts) error {
	root, leg, shard, jobs, measured := o.root, o.leg, o.shard, o.jobs, o.measured
	rc, ok := recipes[leg]
	if !ok {
		return fmt.Errorf("no row recipe for leg %q", leg)
	}
	i, n, err := parseShard(shard)
	if err != nil {
		return err
	}
	// -rows names the rows that must run; a shard of them would run some
	// and drop the rest without a word, so the two never go together.
	if o.rows != "" && n != 1 {
		return fmt.Errorf("-rows with -shard %s: every row -rows names must run, and a shard drops the rest of them; give one or the other", shard)
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
	if o.work != "" {
		if c.work, err = filepath.Abs(o.work); err != nil {
			return err
		}
	}
	if o.gen != "" {
		if !genLegs[leg] {
			return fmt.Errorf("-gen: the %s rows name their generated tree's path themselves; only c, java and rust read a tree -gen can replace", leg)
		}
		if filepath.IsAbs(o.gen) || strings.HasPrefix(filepath.Clean(o.gen), "..") {
			return fmt.Errorf("-gen %q: want a directory inside the repository, relative to -root", o.gen)
		}
		if st, serr := os.Stat(filepath.Join(abs, o.gen)); serr != nil || !st.IsDir() {
			return fmt.Errorf("-gen %q: the generated tree is missing", o.gen)
		}
		c.gen = filepath.Clean(o.gen)
	}
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
	var ledger, notes map[string]string
	if data, rerr := os.ReadFile(filepath.Join(abs, ledgerPath)); rerr == nil {
		if ledger, notes, err = parseLedger(bytes.NewReader(data), ledgerPath); err != nil {
			return err
		}
	} else if !errors.Is(rerr, fs.ErrNotExist) {
		return rerr
	}
	picked := all
	if o.rows != "" {
		if picked, err = pickRows(all, o.rows); err != nil {
			return err
		}
	}
	ran := shardOf(picked, i, n)
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
				fmt.Fprintf(&b, "%s\towed: %s", row, causes[row])
				if n := notes[row]; n != "" {
					fmt.Fprintf(&b, "\t%s", n)
				}
				b.WriteByte('\n')
			}
		}
		if err := os.WriteFile(measured, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	var msg []string
	if len(refused) > 0 {
		sort.Strings(refused)
		msg = append(msg, fmt.Sprintf("%s: %d ledger lines name no failure:\n  %s", ledgerPath, len(refused), strings.Join(refused, "\n  ")))
	}
	if mis := causeMismatches(ran, failed, ledger, causes, ledgerPath); len(mis) > 0 {
		msg = append(msg, fmt.Sprintf("%d owed rows failed with a cause their ledger line does not state (the row's failure changed: read its log, and set the line to what the run printed):\n  %s", len(mis), strings.Join(mis, "\n  ")))
	}
	if verr := verdict(all, ran, failed, ledger, ledgerPath); verr != nil {
		msg = append(msg, verr.Error())
	}
	if len(msg) > 0 {
		return errors.New(strings.Join(msg, "\n"))
	}
	return nil
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
