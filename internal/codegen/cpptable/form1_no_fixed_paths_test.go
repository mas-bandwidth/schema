package cpptable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func generateCpp(t *testing.T, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

// THE REFERENCE HAS ONE FORM-1 ENGINE, AND ALWAYS DID (docs/SPEC-TABLES.md §3).
//
// The pass that emptied the other three emitters of their fixed-table fast
// paths found NOTHING to remove here, and this test is why — written down so
// the next reader does not go looking again.
//
// THE CENSUS THAT PASS WAS HANDED NAMED SEVEN ITEMS IN codecs.go AND
// fixedform.go AS "form-1-only". Every one of them is `ir.ArrayFixed` — a
// FIXED-SIZE ARRAY FIELD, `[N]T` — and not a FIXED TABLE. The two phrases are
// a word apart and mean nothing alike: a fixed-size array is §3's positional
// by-value array, whose all-default elision is wire law for EVERY table that
// declares one, a variable-length table included. Deleting those cases would
// not remove a fixed-table path; it would break the wire for any table with an
// array field. The test below plants a VARIABLE table — it holds a pointer —
// carrying fixed-size arrays, and proves the emitter walks exactly those cases
// for it.
//
// WHAT THE REFERENCE DOES SELECT ON THE CLASS, and why none of it is a fast
// path for a fixed table on form 1:
//
//   - The measure/save/load SIGNATURES. A variable-length body takes the
//     context and the numbering because it reaches a pointee; a pointer-free
//     one cannot and does not. Two signatures for two shapes, not two speeds.
//   - The buffer-level Save/Load entry points, which a variable-length table
//     does not have at all: it is never held by value, so its save takes a
//     builder and its load hands back a region root.
//   - The force-inline qualifier on a pointer-free body. That is a LEGALITY
//     switch: a pointered body reaches its pointee through the depth-carrying
//     template form, a self-referential declaration makes that directly
//     recursive, and a recursive always_inline is a compile error under gcc.
//     The class test is where force-inline stops being LEGAL, not where it
//     starts being wanted.
//   - `ids.ref_at( ordinal, id )`. The ordinal slot cache is spelled by EVERY
//     form-1 field header this emitter writes, in both classes. It is the
//     variable form's own interning, not an accelerator bolted onto one class.
func TestFixedSizeArrayPathsServeVariableTables(t *testing.T) {
	var body string
	for name, data := range generateCpp(t, `package probe
table Leaf
{
    n int32
}
union Choice
{
    a int32
    b int32
}
table Root
{
    target *Leaf
    scalars [4]int32
    tables [3]Leaf
    choices [2]Choice
}
`) {
		if strings.HasSuffix(name, ".h") {
			body += string(data)
		}
	}
	if body == "" {
		t.Fatal("no generated header")
	}
	for _, want := range []string{
		"all_default_scalars",        // the fixed-size scalar array's elision
		"any_choices",                // the fixed-size union array's elision
		"tables (fixed [3])",         // the fixed-size table array always rides
		"TableNumbering & numbering", // and this body is the VARIABLE one
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("a variable table lost the fixed-size array path %q", want)
		}
	}
}

// No helper in this emitter decides an ENCODING by a table's class beyond the
// signature and entry-point switch named above: no typed or arithmetic
// measure, no sizing-to-write element cache, no second save. The other three
// emitters each carried one of these and no longer do; the reference must not
// grow one.
func TestNoClassSelectedFastPath(t *testing.T) {
	gone := []string{
		"SaveTyped",
		"CollectTyped",
		"BodySizeTyped",
		"element_sizes",
		"emitArithMeasure",
		"cacheArrayLengths",
		"ScalarLeafMeasure",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || name == "form1_no_fixed_paths_test.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range gone {
			if strings.Contains(string(data), symbol) {
				t.Errorf("%s names a class-selected fast path %q", name, symbol)
			}
		}
	}
}
