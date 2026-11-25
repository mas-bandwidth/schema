// The table compiler FRONT END plus the GO table emitter, fuzzed as a unit.
// FuzzPipeline drives the whole pipeline (parse -> check -> generate in every
// backend) but never reaches internal/codegen/gotable: no construct of the
// table wire is fuzzed through Go generation today. This target isolates the
// front end's own bounds (record size, leaf cap, depth) and then hands every
// unit that survives them to the Go emitter, checking the oracle — no panic,
// a non-empty refusal message, and (for one input in four) that every emitted
// .go file parses.
//
// A crasher the fuzzer writes under testdata/fuzz/FuzzTableFrontEnd/ is
// committed with its fix: it becomes a permanent regression input and runs
// under plain `go test` forever.
package fuzz_test

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadUnit runs Compiler.Load's steps in memory, in Load's order: parse;
// check.Unit; ir.TableFixedRecordBounds(u, 0); ir.TableFixedLeafCapRefusals(u);
// ir.TableFixedDepthBounds(u). It returns the unit, or every error the load
// steps produced.
func loadUnit(name, src string) (*ir.Unit, []error) {
	ast, perrs := parser.Parse(name, []byte(src))
	if len(perrs) > 0 || ast == nil {
		return nil, perrs
	}
	files := []check.SourceFile{{
		Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
		Bytes: []byte(src), AST: ast,
	}}
	unit, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		return nil, cerrs
	}
	_, ferrs := ir.TableFixedRecordBounds(unit, 0)
	if len(ferrs) > 0 {
		return nil, ferrs
	}
	_, lerrs := ir.TableFixedLeafCapRefusals(unit)
	if len(lerrs) > 0 {
		return nil, lerrs
	}
	_ = ir.TableFixedDepthBounds(unit)
	return unit, nil
}

// seedSchemas adds every *.schema file under dir to the fuzzer corpus and
// returns how many. A walk error or an unreadable schema is an error, never a
// shorter corpus.
func seedSchemas(dir string, add func(string)) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".schema" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		add(string(b))
		n++
		return nil
	})
	return n, err
}

// TestSeedSchemasCountsWhatItAdds is the control: a missing directory is an
// error, an empty one counts zero, and each real seed directory holds schemas.
func TestSeedSchemasCountsWhatItAdds(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedSchemas(filepath.Join(dir, "absent"), func(string) {}); err == nil {
		t.Fatal("a missing seed directory walked without error")
	}
	if n, err := seedSchemas(dir, func(string) {}); err != nil || n != 0 {
		t.Fatalf("an empty directory seeded %d (err %v), want 0", n, err)
	}
	for _, real := range tableSeedDirs {
		if n, err := seedSchemas(real, func(string) {}); err != nil || n == 0 {
			t.Errorf("%s seeds %d schemas (err %v), want at least one", real, n, err)
		}
	}
}

// tableSeedDirs are the trees FuzzTableFrontEnd seeds from.
var tableSeedDirs = []string{
	"../../tables", "../../test", "../../internal",
	"../../examples", "../../examples128", "../../bench", "../../testdata",
}

// tableHandSeeds are shapes aimed at the table front end's own guards, so a
// regression in any of them is found in seconds rather than hours.
var tableHandSeeds = []string{
	"package t\ntype T { x int64 | min = -9223372036854775808, max = 100 }\n",
	"package t\ntype T { x uint64 | min = 0, max = 18446744073709551615 }\n",
	"package t\nenum E { One, Two }\ntype T { x [4]E }\n",
	"package t\nenum E { One, Two }\ntable T { m map[E]int32 }\n",
	"package t\ntable Sub { v int32 }\nunion U { s *Sub }\ntable T { u U }\n",
	"package t\ntable T { lines []string(16) }\n",
	"package t\nfixed table Sub { v int32 }\nfixed table T { s ?Sub }\n",
	"package t\ntable T | json = \"array\"\n{\n    xs []int32\n}\n",
	"package pkg\nenum E {}\ntable T {\n    vals [E]int32\n}\n",
	"package probe\ntable Leaf { values []int32 }\nunion Choice {\n    Empty\n    pointer *Leaf\n    ptrs [4]*Leaf\n    blob *bytes\n}\ntable UnionListRoot { values []Choice\n    bounded [..3]Choice }\n",
}

// tableFrontEndSeeds is FuzzTableFrontEnd's seed corpus: every schema file
// under tableSeedDirs and every hand seed, each distinct text once, in walk
// order. A third of the files are copies of another's text (the same unit
// under testdata and test, say), and the oracle is a function of the text
// alone, so a copy ran the same input again: 1587 seeds, 1088 of them distinct
// (2026-11-25). A seed directory that is missing, unreadable or holds no
// schema is an error, never a shorter corpus
// (TestTableFrontEndSeedsAreEveryDistinctSchemaOnce).
func tableFrontEndSeeds() ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, dir := range tableSeedDirs {
		n, err := seedSchemas(dir, add)
		if err != nil {
			return nil, fmt.Errorf("seed directory %s: %w", dir, err)
		}
		if n == 0 {
			return nil, fmt.Errorf("seed directory %s holds no .schema file", dir)
		}
	}
	for _, s := range tableHandSeeds {
		add(s)
	}
	return out, nil
}

// TestTableFrontEndSeedsAreEveryDistinctSchemaOnce holds the seed corpus to
// what it replaced: the set of texts is exactly every schema file under
// tableSeedDirs plus every hand seed (none lost), and no text is in it twice.
// Planted: tableFrontEndSeeds without its seen check repeats texts (red); with
// a seed directory dropped, texts go missing (red).
func TestTableFrontEndSeedsAreEveryDistinctSchemaOnce(t *testing.T) {
	t.Parallel()
	seeds, err := tableFrontEndSeeds()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, s := range seeds {
		got[s]++
	}
	want := map[string]bool{}
	files := 0
	for _, dir := range tableSeedDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".schema" {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			want[string(b)] = true
			files++
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range tableHandSeeds {
		want[s] = true
	}
	for s, n := range got {
		if n > 1 {
			t.Errorf("seed %.60q is in the corpus %d times", s, n)
		}
		if !want[s] {
			t.Errorf("seed %.60q is no schema file and no hand seed", s)
		}
	}
	missing := 0
	for s := range want {
		if got[s] == 0 {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d distinct schema texts of %d files and %d hand seeds are not in the corpus", missing, files, len(tableHandSeeds))
	}
	if len(seeds) != len(want) {
		t.Errorf("the corpus holds %d seeds, want the %d distinct texts", len(seeds), len(want))
	}
}

// FuzzTableFrontEnd: any bytes the front end accepts into a unit must give the
// Go emitter a unit it neither panics nor refuses in silence on, and the
// emitted Go — for one input in four — must parse.
func FuzzTableFrontEnd(f *testing.F) {
	seeds, err := tableFrontEndSeeds()
	if err != nil {
		f.Fatal(err)
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(tableFrontEndOracle)
}

// tableFrontEndOracle is FuzzTableFrontEnd's check of one input: a refusal
// has a message, a unit the front end accepts reaches the Go emitter, which
// neither panics nor refuses in silence, and for one input in four every
// emitted Go file parses.
func tableFrontEndOracle(t *testing.T, src string) {
	if len(src) > 3000 {
		return
	}
	unit, errs := loadUnit("A.schema", src)
	for _, e := range errs {
		if e.Error() == "" {
			t.Fatalf("empty refusal message for input %q", src)
		}
	}
	if unit == nil {
		return
	}
	out, gerr := compiler.New().Generate(unit, "go", nil)
	if gerr != nil && gerr.Error() == "" {
		t.Fatalf("empty generate refusal for input %q", src)
	}
	if len(src)%4 != 0 {
		return
	}
	fset := token.NewFileSet()
	for fn, data := range out {
		if _, perr := goparser.ParseFile(fset, fn, data, goparser.AllErrors); perr != nil {
			t.Fatalf("generated Go %s does not parse: %v\n---- schema ----\n%s", fn, perr, src)
		}
	}
}

// tableFrontEndCorpusDir is where the fuzzer commits FuzzTableFrontEnd's
// crashers, each a permanent regression input.
const tableFrontEndCorpusDir = "testdata/fuzz/FuzzTableFrontEnd"

// readFuzzCorpusString reads one committed corpus file of a fuzz target that
// takes one string: "go test fuzz v1", then string("..."). Any other shape is
// an error, never a seed left out.
func readFuzzCorpusString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	head, body, ok := strings.Cut(strings.TrimRight(string(b), "\n"), "\n")
	if !ok || head != "go test fuzz v1" || strings.Contains(body, "\n") {
		return "", fmt.Errorf("%s: not a one-string go fuzz corpus file", path)
	}
	lit, ok := strings.CutPrefix(body, "string(")
	if !ok || !strings.HasSuffix(lit, ")") {
		return "", fmt.Errorf("%s: its value is not string(...)", path)
	}
	v, err := strconv.Unquote(strings.TrimSuffix(lit, ")"))
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}

// tableFrontEndRegressionSeeds are the inputs TestTableFrontEndRegressionSeeds
// runs per change: every hand seed, every committed crasher, and the first
// schema (in walk order) of each seed directory, one of each kind of unit the
// corpus holds. FuzzTableFrontEnd runs every distinct seed in the nightly tier
// (91 s alone on amd-2, 2026-11-25: past a per-PR shard).
func tableFrontEndRegressionSeeds() ([]string, error) {
	out := append([]string(nil), tableHandSeeds...)
	entries, err := os.ReadDir(tableFrontEndCorpusDir)
	if err != nil {
		return nil, err
	}
	crashers := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		s, err := readFuzzCorpusString(filepath.Join(tableFrontEndCorpusDir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		crashers++
	}
	if crashers == 0 {
		return nil, fmt.Errorf("%s holds no corpus file", tableFrontEndCorpusDir)
	}
	for _, dir := range tableSeedDirs {
		var first []string
		if _, err := seedSchemas(dir, func(s string) {
			if len(first) == 0 {
				first = append(first, s)
			}
		}); err != nil {
			return nil, fmt.Errorf("seed directory %s: %w", dir, err)
		}
		if len(first) == 0 {
			return nil, fmt.Errorf("seed directory %s holds no .schema file", dir)
		}
		out = append(out, first[0])
	}
	return out, nil
}

// TestTableFrontEndRegressionSeeds is FuzzTableFrontEnd's per-change twin: its
// oracle over the hand seeds, the committed crashers and one schema of each
// seed directory. Every one of these is also a seed of FuzzTableFrontEnd
// (TestTableFrontEndRegressionSeedsAreFuzzSeeds), which runs them all with
// every other distinct schema nightly.
func TestTableFrontEndRegressionSeeds(t *testing.T) {
	t.Parallel()
	seeds, err := tableFrontEndRegressionSeeds()
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range seeds {
		t.Run(fmt.Sprint(i), func(t *testing.T) { tableFrontEndOracle(t, s) })
	}
}

// TestTableFrontEndRegressionSeedsAreFuzzSeeds: the twin runs nothing the
// fuzz target does not (each of its inputs is a seed of FuzzTableFrontEnd or
// one of its committed crashers), it runs every hand seed and every crasher,
// and the corpus reader refuses a file it cannot read whole. Planted: the
// reader without its header check reads a v2 file (red).
func TestTableFrontEndRegressionSeedsAreFuzzSeeds(t *testing.T) {
	t.Parallel()
	twin, err := tableFrontEndRegressionSeeds()
	if err != nil {
		t.Fatal(err)
	}
	seeds, err := tableFrontEndSeeds()
	if err != nil {
		t.Fatal(err)
	}
	fuzz := map[string]bool{}
	for _, s := range seeds {
		fuzz[s] = true
	}
	entries, err := os.ReadDir(tableFrontEndCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, s := range twin {
		in[s] = true
	}
	for _, e := range entries {
		s, err := readFuzzCorpusString(filepath.Join(tableFrontEndCorpusDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fuzz[s] = true
		if !in[s] {
			t.Errorf("crasher %s is not in the twin", e.Name())
		}
	}
	for _, s := range tableHandSeeds {
		if !in[s] {
			t.Errorf("hand seed %.60q is not in the twin", s)
		}
	}
	for _, s := range twin {
		if !fuzz[s] {
			t.Errorf("the twin runs %.60q, which is no seed of FuzzTableFrontEnd", s)
		}
	}
	if want := len(tableHandSeeds) + len(entries) + len(tableSeedDirs); len(twin) != want {
		t.Errorf("the twin holds %d inputs, want %d", len(twin), want)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"no-header":    "string(\"package p\\n\")\n",
		"wrong-header": "go test fuzz v2\nstring(\"package p\")\n",
		"two-args":     "go test fuzz v1\nstring(\"a\")\nstring(\"b\")\n",
		"bytes":        "go test fuzz v1\n[]byte(\"package p\")\n",
		"unquoted":     "go test fuzz v1\nstring(package p)\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if v, err := readFuzzCorpusString(p); err == nil {
			t.Errorf("corpus file %s read as %q: want an error", name, v)
		}
	}
	if v, err := readFuzzCorpusString(filepath.Join(tableFrontEndCorpusDir, "f057_valid_no_diagnostics")); err != nil || v != "package fixtures\ntype Good { value uint8 }\n" {
		t.Errorf("a committed crasher read as %q, %v", v, err)
	}
}

// TestZeroStrideAndEdgeShapesAllTargets exercises empty-enum keyed refusals,
// synthetic zero-bound IR defense-in-depth, zero-byte struct/table elements,
// and union list pointer/blob arms across every registered compiler target
// (all 17 default targets plus moo/mootable = all 18 languages).
func TestZeroStrideAndEdgeShapesAllTargets(t *testing.T) {
	for _, src := range []string{
		"package pkg\nenum E {}\ntable T {\n    vals [E]int32\n}\n",
		"package pkg\nenum E {}\nfixed table T { vals [E]int32 }\n",
		"package pkg\nenum E {}\ntype S { vals [E]int32 }\ntable T { s S }\n",
	} {
		u, errs := loadUnit("A.schema", src)
		if len(errs) == 0 || u != nil {
			t.Fatalf("expected loadUnit refusal on empty-enum keyed array %q, got unit=%v errs=%v", src, u, errs)
		}
		for _, e := range errs {
			if e.Error() == "" {
				t.Fatalf("empty refusal message on %q", src)
			}
		}
	}

	// Defense-in-depth: even if a synthetic *ir.Unit reaches codegen with
	// ArrayBound == 0 on a keyed/fixed array field, no target may panic.
	syntheticUnit, errs := loadUnit("A.schema", "package pkg\nenum E { A }\ntable T { vals [E]int32 }\n")
	if len(errs) > 0 || syntheticUnit == nil {
		t.Fatalf("control unit failed: %v", errs)
	}
	syntheticUnit.Enums["E"].Variants = nil
	syntheticUnit.Enums["E"].Max = 0
	syntheticUnit.Tables["T"].Fields[0].ArrayBound = 0
	driveUnitAll18Targets(t, "synthetic-zero-bound", syntheticUnit, false)

	// Valid edge shapes (zero-byte structs/tables, empty enum scalars/arrays/lists,
	// and union lists/counted-arrays carrying *bytes and [N]*Leaf pointer arms)
	// must generate without panic across all 18 targets and emit valid Go syntax.
	validEdgeSchemas := []string{
		"package pkg\ntype Empty {}\ntable T { e Empty\n es [4]Empty\n ec [..4]Empty\n el []Empty }\n",
		"package pkg\ntype Empty {}\nfixed table T { es [4]Empty }\n",
		"package pkg\nfixed table Empty {}\ntable T { es [4]Empty\n el []Empty }\n",
		"package pkg\ntable Empty {}\ntable T { es []*Empty }\n",
		"package pkg\nenum E {}\ntable T { e E\n es [4]E\n el []E }\n",
		"package probe\ntable Leaf { values []int32 }\nunion Choice {\n    Empty\n    pointer *Leaf\n    ptrs [4]*Leaf\n    blob *bytes\n    str *string\n}\ntable UnionListRoot { values []Choice\n    bounded [..3]Choice }\n",
	}
	for _, src := range validEdgeSchemas {
		u, errs := loadUnit("A.schema", src)
		if len(errs) > 0 || u == nil {
			t.Fatalf("valid edge schema failed loadUnit: %v\n%s", errs, src)
		}
		driveUnitAll18Targets(t, src, u, true)
	}
}

func driveUnitAll18Targets(t *testing.T, label string, u *ir.Unit, parseGo bool) {
	t.Helper()
	c := compiler.New()
	allTargets := c.Targets()
	if len(allTargets) == 0 {
		t.Fatal("c.Targets() is empty: the loop below would check nothing")
	}
	for _, target := range allTargets {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: target %s panicked: %v", label, target, r)
				}
			}()
			out, err := c.Generate(u, target, nil)
			if err != nil {
				if err.Error() == "" {
					t.Errorf("%s: target %s returned empty error", label, target)
				}
				return
			}
			if parseGo && target == "go" {
				fset := token.NewFileSet()
				for fn, data := range out {
					if _, perr := goparser.ParseFile(fset, fn, data, goparser.AllErrors); perr != nil {
						t.Errorf("%s: generated Go %s does not parse: %v", label, fn, perr)
					}
				}
			}
		}()
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s: target moo panicked: %v", label, r)
			}
		}()
		// a fuzzed unit may be refused; a refusal that says nothing is the defect
		if _, err := moo.Generate(u); err != nil && err.Error() == "" {
			t.Errorf("%s: target moo returned empty error", label)
		}
		if _, err := mootable.Generate(u); err != nil && err.Error() == "" {
			t.Errorf("%s: target moo (tables) returned empty error", label)
		}
	}()
}
