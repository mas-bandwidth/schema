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

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
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

// seedSchemas adds every *.schema file under dir to the fuzzer corpus.
func seedSchemas(dir string, add func(string)) {
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(d.Name()) != ".schema" {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil {
			add(string(b))
		}
		return nil
	})
}

// tableHandSeeds are shapes aimed at the table front end's own guards, so a
// regression in any of them is found in seconds rather than hours.
var tableHandSeeds = []string{
	"package t\ntype T { x int64 | min = -9223372036854775808, max = 100 }\n",
	"package t\ntype T { x uint64 | min = 0, max = 18446744073709551615 }\n",
	"package t\nenum E { One, Two }\ntype T { x [4]E }\n",
	"package t\nenum E { One, Two }\ntable T { m map[E]int32 }\n",
	"package t\ntype Sub { v int32 }\nunion U { s *Sub }\ntable T { u U }\n",
	"package t\nfixed table T { lines []string(16) }\n",
	"package t\nfixed table Sub { v int32 }\nfixed table T { s ?Sub }\n",
	"package t\ntable T | json = \"array\"\n{\n    xs []int32\n}\n",
}

// FuzzTableFrontEnd: any bytes the front end accepts into a unit must give the
// Go emitter a unit it neither panics nor refuses in silence on, and the
// emitted Go — for one input in four — must parse.
func FuzzTableFrontEnd(f *testing.F) {
	for _, dir := range []string{
		"../../tables", "../../test", "../../internal",
		"../../examples", "../../examples128", "../../bench", "../../testdata",
	} {
		seedSchemas(dir, func(s string) { f.Add(s) })
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
