// THE DECLARED CLASS (docs/SPEC-TABLES.md §2.2): `fixed table` is the fixed
// wire and a plain `table` is the variable one, and nothing is inferred in
// either direction. The owner's ruling is both halves of that — "that way if
// we add any feature that stops it from being fixed, it is a compile error",
// and "otherwise, it could be a bit of a guess whether the table is fixed or
// variable, couldn't it? we don't want to surprise the user."
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuse parses and checks a source that is expected to be REFUSED, and hands
// back the first diagnostic.
func refuse(t *testing.T, src string) string {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		return perrs[0].Error()
	}
	_, cerrs := Unit([]SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) == 0 {
		t.Fatalf("the source was accepted:\n%s", src)
	}
	return cerrs[0].Error()
}

// TestFixedTableClosureRefusals: each of the six constructs that make a body
// variable size is refused inside a `fixed table`'s BY-VALUE CLOSURE, and the
// refusal NAMES THE FIELD and the fixed table it breaks — which is the whole
// point of the keyword, since a feature that stops a table being fixed has to
// be a compile error at the declaration rather than a silent change of wire.
func TestFixedTableClosureRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string // the `Owner.field` the refusal must name
		want  string // the phrase that says WHICH construct broke it
		src   string
	}{
		{
			name: "a map", field: "Fleet.ships", want: "is a map",
			src: "package t\nfixed table Fleet { ships map[uint32]int32 }\n",
		},
		{
			name: "an unbounded array", field: "Log.entries", want: "is an unbounded array",
			src: "package t\nfixed table Log { entries []int32 }\n",
		},
		{
			name: "a pointer", field: "Scene.head", want: "is a pointer",
			src: "package t\ntable Node { x int32 }\nfixed table Scene { head *Node }\n",
		},
		{
			name: "an unbounded byte buffer", field: "Asset.data", want: "is a byte buffer",
			src: "package t\nfixed table Asset { data *bytes }\n",
		},
		{
			name: "an unbounded string", field: "Asset.caption", want: "is a byte buffer",
			src: "package t\nfixed table Asset { caption *string }\n",
		},
		{
			name: "a guarded branch", field: "Patrol.target_id", want: "sits in an `if` branch",
			src: "package t\nfixed table Patrol {\n    has_target bool\n    if has_target\n    {\n        target_id bits(12)\n    }\n}\n",
		},
		{
			name: "a nested plain table", field: "Root.child", want: "holds the plain table Child by value",
			src: "package t\ntable Child { x int32 }\nfixed table Root { child Child }\n",
		},
		// THE CLOSURE IS BY-VALUE AND REACHES THROUGH A `type` (§2.2), which
		// is how bench/corpus/FixedTable.schema's wrapper is refused: the
		// guard is BenchMixed's, and the fixed table is the wrapper's.
		{
			name: "a guard inside a `type` held by value", field: "Body.extra", want: "sits in an `if` branch",
			src: "package t\ntype Body {\n    gate bool\n    if gate\n    {\n        extra int32\n    }\n}\nfixed table Wrap { value Body }\n",
		},
		// AN ARM IS A FIELD LINE (§2.6), so the arms take the same rules and
		// the refusal names the UNION, which is the declaration to edit.
		{
			name: "a pointer arm", field: "Shape.chunk", want: "is a pointer arm",
			src: "package t\ntable Chunk { seq uint32 }\nunion Shape { chunk *Chunk }\nfixed table Holder { shape Shape }\n",
		},
		// AN ELEMENT OF A BOUNDED ARRAY is a by-value edge like any other.
		{
			name: "a plain table in a bounded array", field: "Fleet.ships", want: "holds the plain table Ship by value",
			src: "package t\ntable Ship { hp int32 }\nfixed table Fleet { ships [..4]Ship }\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := refuse(t, tc.src)
			if !strings.Contains(got, tc.field) {
				t.Errorf("the refusal does not name the field %s: %s", tc.field, got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("the refusal does not say what broke it (%q): %s", tc.want, got)
			}
			if !strings.Contains(got, "docs/SPEC-TABLES.md") {
				t.Errorf("the refusal cites no spec section: %s", got)
			}
		})
	}
}

// TestABoundedPlainTableIsStillVariable is the OTHER half of the ruling, and
// the half a derivation could never give: a plain `table` whose every field is
// bounded is on the VARIABLE wire, because the declaration says so. Nothing is
// inferred from the fields, so adding a map to this table later is an edit to
// its body and not a change of its wire.
func TestABoundedPlainTableIsStillVariable(t *testing.T) {
	u := buildUnit(t, "package t\ntable Bounded {\n    name  string(16)\n    slots [4]int32\n    flag  bool\n}\nfixed table Sized {\n    name  string(16)\n    slots [4]int32\n    flag  bool\n}\n")
	variable := ir.VariableTables(u)
	if !variable["Bounded"] {
		t.Error("a plain `table` of bounded fields is the VARIABLE wire — the class is declared, not derived (docs/SPEC-TABLES.md §2.2)")
	}
	if variable["Sized"] {
		t.Error("`fixed table` is the FIXED wire")
	}
	// THE GENERATED SHAPE follows the declaration: only a fixed table has a
	// block form (§2.7), so the block surface is the shape gate on the pair.
	blocks := ir.Blocks(u)
	if blocks == nil {
		t.Fatal("a unit with tables has a block surface")
	}
	if blocks.Block("Sized") == nil {
		t.Error("the fixed table has no block form — every FIXED table has one (docs/SPEC-TABLES.md §2.7)")
	}
	if blocks.Block("Bounded") != nil {
		t.Error("the plain table got a block form — it is the fixed class's alone (docs/SPEC-TABLES.md §2.7)")
	}
	if _, said := blocks.Skipped["Bounded"]; !said {
		t.Error("the plain table's missing block form is not recorded with its reason")
	}
}

// TestFixedIsRefusedAwayFromATableDeclaration: `fixed` at file scope is the
// class keyword and nothing else. The same word inside a body is the
// `fixed(I, F)` scalar (SPEC §4.3), and the two never meet.
func TestFixedIsRefusedAwayFromATableDeclaration(t *testing.T) {
	got := refuse(t, "package t\nfixed type P { x int32 }\n")
	if !strings.Contains(got, "`fixed` at file scope qualifies a table declaration") {
		t.Errorf("the refusal does not say what `fixed` qualifies: %s", got)
	}
}

// TestTheDeclaredClassReachesTheIr: the flag the parser sets is the flag every
// emitter switches on, and [ir.VariableTables] is its complement.
func TestTheDeclaredClassReachesTheIr(t *testing.T) {
	f, perrs := parser.Parse("T.schema", []byte("package t\nfixed table A { x int32 }\ntable B { y int32 }\n"))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	for _, d := range f.Decls {
		td, ok := d.(*ast.TableDecl)
		if !ok {
			continue
		}
		if want := td.Name == "A"; td.Fixed != want {
			t.Errorf("%s: parsed Fixed = %v, want %v", td.Name, td.Fixed, want)
		}
	}
	u := buildUnit(t, "package t\nfixed table A { x int32 }\ntable B { y int32 }\n")
	if !u.Tables["A"].Fixed || u.Tables["B"].Fixed {
		t.Errorf("the declared class did not reach the IR: A.Fixed=%v B.Fixed=%v", u.Tables["A"].Fixed, u.Tables["B"].Fixed)
	}
	if v := ir.VariableTables(u); v["A"] || !v["B"] {
		t.Errorf("VariableTables is not the flag's complement: %v", v)
	}
}
