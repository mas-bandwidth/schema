package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// extractCmd is "schema-test extract": every test the repository holds,
// checked against the registry and the unmoved ledger.
//
//	schema-test extract                    check, and print the counts
//	schema-test extract --json             every test, one JSON line each
//	schema-test extract --write-ledger     rewrite the ledger to what is unmoved now
//	schema-test extract --shrinks-from F   also hold the ledger to F (main's), which it may not exceed
//	schema-test extract --since DIR        also name each test DIR (main's tree) lacks that has no row
//	schema-test extract --move L           give every unmoved test of layer L's ledger lines its
//	                                       registry row (the kinds the driver has an adapter for), then
//	                                       rewrite the ledger
func extractCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test extract", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "")
	path := fs.String("registry", testreg.DefaultPath, "")
	asJSON := fs.Bool("json", false, "")
	write := fs.Bool("write-ledger", false, "")
	shrinks := fs.String("shrinks-from", "", "")
	since := fs.String("since", "", "")
	move := fs.String("move", "", "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"extract takes no names"}
	}
	res, err := extract.Extract(*root)
	if err != nil {
		return err
	}
	if *asJSON {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		for _, it := range res.Items {
			if err := enc.Encode(it); err != nil {
				return err
			}
		}
		_, err := stdout.Write(b.Bytes())
		return err
	}
	reg, err := testreg.Load(filepath.Join(*root, *path))
	if err != nil {
		return err
	}
	ledgerPath := filepath.Join(*root, extract.LedgerPath)
	if *move != "" {
		ledger, err := extract.LoadLedger(ledgerPath)
		if err != nil {
			return err
		}
		n, err := moveLayer(*root, *path, *move, res, reg, ledger)
		if err != nil {
			return err
		}
		if reg, err = testreg.Load(filepath.Join(*root, *path)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, "schema-test extract: moved %d tests of layer %s into %s\n", n, *move, *path); err != nil {
			return err
		}
		*write = true
	}
	if *write {
		// the unmoved lines are counted again; the skip-red lines are
		// measurements, not counts, so they carry over as they are
		lines := extract.Ledger(res, reg, plan(*root, reg))
		if old, err := extract.LoadLedger(ledgerPath); err == nil {
			lines = append(lines, extract.SkipReds(old)...)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var b bytes.Buffer
		if err := extract.WriteLedger(&b, ledgerHeader, lines); err != nil {
			return err
		}
		return os.WriteFile(ledgerPath, b.Bytes(), 0o644)
	}
	ledger, err := extract.LoadLedger(ledgerPath)
	if err != nil {
		return err
	}
	if err := printCounts(stdout, res, reg, ledger); err != nil {
		return err
	}
	problems := extract.Check(res, reg, ledger)
	if *shrinks != "" {
		old, err := extract.LoadLedger(*shrinks)
		if err != nil {
			return err
		}
		problems = append(problems, extract.Shrinks(old, ledger)...)
		problems = append(problems, extract.KeepsSkipReds(old, ledger, res)...)
	}
	if *since != "" {
		before, err := extract.Extract(*since)
		if err != nil {
			return fmt.Errorf("--since %s: %w", *since, err)
		}
		problems = append(problems, extract.NewWithoutRows(before, res, reg)...)
	}
	if len(problems) > 0 {
		var b strings.Builder
		for _, p := range problems {
			fmt.Fprintf(&b, "FAIL extract: %s\n", p)
		}
		if _, err := io.WriteString(stdout, b.String()); err != nil {
			return err
		}
		return fmt.Errorf("extract: %d disagreements between the repository, the registry and %s", len(problems), extract.LedgerPath)
	}
	_, err = fmt.Fprintf(stdout, "schema-test extract PASS: every test is a registry row or counted in %s\n", extract.LedgerPath)
	return err
}

// printCounts is the extract's account: tests by kind, how many are rows
// and how many wait, and the gates that decide tiers.
func printCounts(w io.Writer, res *extract.Result, reg *testreg.Registry, ledger []extract.LedgerLine) error {
	covered, _ := extract.Covered(reg)
	total, rows, gated := map[string]int{}, map[string]int{}, map[string]int{}
	for _, it := range res.Items {
		total[it.Kind]++
		if _, ok := covered[it.ID()]; ok {
			rows[it.Kind]++
		}
		for _, g := range it.Gates {
			kind, _, _ := strings.Cut(g, ":")
			gated[it.Kind+" "+kind]++
		}
	}
	waiting := map[string]int{}
	redTests, redSkips := 0, 0
	for _, l := range ledger {
		if l.Kind == extract.KindSkipRed {
			redTests++
			redSkips += l.Count
			continue
		}
		waiting[l.Kind] += l.Count
	}
	var b strings.Builder
	fmt.Fprintf(&b, "schema-test extract: %d tests, %d unparsed files\n", len(res.Items), len(res.Unparsed))
	fmt.Fprintf(&b, "  %-17s %7s %7s %9s\n", "kind", "tests", "rows", "unmoved")
	for _, k := range extract.Kinds() {
		fmt.Fprintf(&b, "  %-17s %7d %7d %9d\n", k, total[k], rows[k], waiting[k])
	}
	keys := make([]string, 0, len(gated))
	for k := range gated {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("  gates (a test may have several):\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "    %-30s %6d\n", k, gated[k])
	}
	fmt.Fprintf(&b, "  skips that hide a red (%s lines): %d tests, %d skip sites\n", extract.KindSkipRed, redTests, redSkips)
	_, err := io.WriteString(w, b.String())
	return err
}

const ledgerHeader = `# The unmoved ledger: the tests the repository holds that are not yet
# registry rows, counted by kind and place. schema-test extract holds it to
# the repository exactly, and against main's copy it only shrinks: a new test
# gets a registry row, never a ledger line. Each line names the layer that
# moves its tests in (docs/TEST-REGISTRY.md, "Every test is in the registry").
# Regenerate with: go run ./tools/schema-test extract --write-ledger
# A skip-red line is one Go test ("<package dir>:<TestName>") whose skip
# hides a red, measured: its count is the test's Skip sites, and it stays
# until the skip is gone. --write-ledger carries those lines over as they are.
# kind	place	count	layer	reason
`

// plan is the layer that moves a kind's tests in, and why they wait.
func plan(root string, reg *testreg.Registry) func(kind, dir string, items []extract.Item) (string, string) {
	langs := map[string]bool{}
	for _, l := range reg.Langs {
		langs[l.Name] = true
	}
	return func(kind, dir string, items []extract.Item) (string, string) {
		switch kind {
		case extract.KindGoTest:
			if l := langOf(dir, langs); l != "" {
				return "L5", "per-language (" + l + ") Go tests: " + l + "'s adapter runs them when their families move in"
			}
			layer, reason := "", ""
			for _, it := range items {
				l, r := goTestLayer(root, it)
				if l == "L3" {
					return l, r
				}
				if layer == "" {
					layer, reason = l, r
				} else if r != reason && !strings.Contains(reason, r) {
					reason += "; " + r
				}
			}
			return layer, reason
		case extract.KindDataCase, extract.KindDriver:
			return "L4", "corpus data and harnesses: each language's adapter answers them when their family moves in"
		case extract.KindNegativeControl:
			return "L6", "negative controls: the plant adapter runs them"
		case extract.KindMakeTarget:
			return "L6", "make gates: the make adapter runs them"
		case extract.KindModel:
			return "L6", "TLC models: the model-check adapter runs them"
		case extract.KindWorkflowJob:
			return "L7", "workflow jobs: CI is generated from the registry"
		}
		panic(errors.New("unknown kind " + kind))
	}
}

// goTestLayer is the layer that moves one language-free Go test in. The
// gotest adapter (L3) runs a test of the root module that cannot skip; a
// benchmark waits for the bench adapter, a test in a Go module of its own
// runs against the generated Go it is copied beside (go's adapter), and a
// test that can still skip is one a make target drives, with the build and
// environment its recipe provides, or on a volume a runner provides (the
// make adapter).
func goTestLayer(root string, it extract.Item) (string, string) {
	switch {
	case strings.HasPrefix(it.Name, "Benchmark"):
		return "L6", "Go benchmarks: the bench adapter runs them"
	case nestedModule(root, it.Dir):
		return "L5", "Go tests in a module of their own, run beside the generated Go they test: go's adapter runs them"
	case slices.ContainsFunc(it.Gates, func(g string) bool { return strings.HasPrefix(g, "skip:") }):
		return "L6", "Go tests that still skip without a build, an environment or a volume a make target or a runner provides: the make adapter runs them"
	}
	return "L3", "language-free Go tests: the gotest adapter runs them when their package moves in"
}

// nestedModule reports whether a directory is in a Go module below root's.
func nestedModule(root, dir string) bool {
	for d := filepath.FromSlash(dir); d != "." && d != string(filepath.Separator); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(root, d, "go.mod")); err == nil {
			return true
		}
	}
	return false
}

// langOf is the language a path names: a segment that is a language, or
// one that starts with it and ends in a known suffix (ctable, gotable,
// pythonsdk...).
func langOf(dir string, langs map[string]bool) string {
	for seg := range strings.SplitSeq(dir, "/") {
		if langs[seg] {
			return seg
		}
		for _, suffix := range []string{"table", "tables", "leg", "sdk", "gen", "emit", "view", "runtime"} {
			if l, ok := strings.CutSuffix(seg, suffix); ok && langs[l] {
				return l
			}
		}
	}
	return ""
}
