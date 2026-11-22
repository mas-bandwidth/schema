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
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/ir"
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

// FuzzTableFrontEnd: any bytes the front end accepts into a unit must give the
// Go emitter a unit it neither panics nor refuses in silence on, and the
// emitted Go — for one input in four — must parse.
func FuzzTableFrontEnd(f *testing.F) {
	for _, dir := range tableSeedDirs {
		n, err := seedSchemas(dir, func(s string) { f.Add(s) })
		if err != nil {
			f.Fatalf("seed directory %s: %v", dir, err)
		}
		if n == 0 {
			f.Fatalf("seed directory %s holds no .schema file", dir)
		}
	}
	for _, s := range tableHandSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
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
	})
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
	for _, target := range c.Targets() {
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
		_, _ = moo.Generate(u)
		_, _ = mootable.Generate(u)
	}()
}
