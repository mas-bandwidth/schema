// Tests for the table frontend (SPEC-TABLES.md): the refusals that keep the
// table wire sound, and the independence proof — tables move no protocol id.
package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func buildUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := Unit([]SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestTableRefusals is the tables extension of the break-the-language suite:
// every way to misuse a table that the checker provably catches.
func TestTableRefusals(t *testing.T) {
	cases := []struct {
		name string
		want string
		src  string
	}{
		{name: "a type field cannot reference a table", want: "is a table, not a wire type",
			src: "package t\ntable Tab { x int32 }\ntype P { tab Tab }\n"},
		{name: "was on a type field is refused by name", want: "was is a table-wire concept",
			src: "package t\ntype P { speed float32 | was = \"velocity\" }\n"},
		{name: "was naming the field's own current name", want: "names the field's own current name",
			src: "package t\ntable Tab { speed float32 | was = \"speed\" }\n"},
		{name: "was with an empty string", want: "names nothing",
			src: "package t\ntable Tab { speed float32 | was = \"\" }\n"},
		{name: "was takes a quoted string", want: "was takes the field's old name as a quoted string",
			src: "package t\ntable Tab { speed float32 | was = velocity }\n"},
		{name: "effective wire ids collide (was aliases a live sibling)", want: "collide on table-wire id",
			src: "package t\ntable Tab {\n    a int32 | was = \"b\"\n    b int32\n}\n"},
		{name: "a table nesting itself is a composition cycle", want: "type composition cycle",
			src: "package t\ntable Tab { inner Tab }\n"},
		{name: "a table cycle through a chain", want: "type composition cycle",
			src: "package t\ntable Aaa { b Bbb }\ntable Bbb { a Aaa }\n"},
		{name: "const(value, bits) in a table body", want: "const(value, bits) is a packet-wire construct",
			src: "package t\ntable Tab {\n    const(7, 4)\n    x int32\n}\n"},
		{name: "reserved(bits) in a table body", want: "reserved(bits) is a packet-wire construct",
			src: "package t\ntable Tab {\n    reserved(4)\n    x int32\n}\n"},
		{name: "align in a table body", want: "align is a packet-wire construct",
			src: "package t\ntable Tab {\n    align\n    x int32\n}\n"},
		{name: "a table is not a union payload", want: "not a union payload",
			src: "package t\ntable Tab { x int32 }\nunion U\n{\n    tab Tab\n}\n"},
		{name: "fixed(I, F) has no table-wire kind", want: "has no table-wire kind",
			src: "package t\ntable Tab { x fixed(16, 16) | min = 0, max = 5 }\n"},
		{name: "fixed in a type pulled into a closure", want: "has no table-wire kind",
			src: "package t\ntype Inner { x fixed(16, 16) | min = 0, max = 5 }\ntable Tab { inner Inner }\n"},
		{name: "uint128 has no table-wire kind", want: "has no table-wire kind",
			src: "package t\ntable Tab { x uint128 }\n"},
		{name: "string past the uint16 length", want: "table-wire lengths ride in uint16",
			src: "package t\ntable Tab { s string(70000) }\n"},
		{name: "array bound past the uint16 count", want: "table-wire counts ride in uint16",
			src: "package t\ntable Tab { xs [..70000]int32 }\n"},
		{name: "an array of unions in a closure", want: "an array of unions may not sit on a table-closure path",
			src: "package t\ntype P { x int32 }\nunion U\n{\n    p P\n}\ntable Tab { us [..4]U }\n"},
		{name: "a declaration colliding with the table runtime", want: "generated TABLE-wire runtime",
			src: "package t\ntable Tab { x int32 }\ntype TableReport { y int32 }\n"},
		{name: "a declaration colliding with generated table codecs", want: "generated TABLE-wire functions",
			src: "package t\ntable Tab { x int32 }\ntype TabLoad { y int32 }\n"},
		{name: "a declaration colliding with the mutable-life surface", want: "generated TABLE-wire functions",
			src: "package t\ntable Tab { x int32 }\ntype TabBuilder { y int32 }\n"},

		// ---- pointers (SPEC-TABLES.md §11) ----
		{name: "a pointer to a type is refused by name", want: "may only target a `table`",
			src: "package t\ntype P { x int32 }\ntable Tab { p *P }\n"},
		{name: "a pointer to an enum is refused by name", want: "may only target a `table`",
			src: "package t\nenum E { A }\ntable Tab { e *E }\n"},
		{name: "a pointer inside a type body is refused by name", want: "pointers are a TABLE construct",
			src: "package t\ntable Tab { x int32 }\ntype P { t *Tab }\n"},
		{name: "an array of pointers is a named follow-on", want: "an array of pointers is a named follow-on",
			src: "package t\ntable Node { x int32 }\ntable Tab { kids [..4]*Node }\n"},
		{name: "a pointer field takes no specified default", want: "a pointer field takes no specified default",
			src: "package t\ntable Node { x int32 }\ntable Tab { head *Node = 0 }\n"},
		{name: "a pointer to an undeclared table", want: "undefined type",
			src: "package t\ntable Tab { head *Ghost }\n"},
		{name: "by-value recursion stays refused with pointers in the language",
			want: "type composition cycle",
			src:  "package t\ntable Node { x int32\n  self Node }\n"},
		{name: "by-value recursion through a chain stays refused",
			want: "type composition cycle",
			src:  "package t\ntable A { b B }\ntable B { a A }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
			if len(errs) == 0 {
				t.Fatalf("expected a diagnostic containing %q, got none", tc.want)
			}
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
}

// TestTypeFreeOfTableSymbols: a table-free unit keeps its whole namespace —
// the TableReport claim exists only when a table is declared.
func TestTypeFreeOfTableSymbols(t *testing.T) {
	if errs := runUnit(t, map[string]string{"T.schema": "package t\ntype TableReport { y int32 }\n"}); len(errs) > 0 {
		t.Fatalf("a table-free unit must not claim the table runtime names: %v", errs)
	}
}

const tablelessSrc = `package probe

const MaxItems = 16

enum Kind { Alpha, Beta }

type Point
{
    x float32
    y float32
}

type Packet
{
    kind  Kind
    items [..MaxItems]Point
}
`

// TestTablesMoveNoProtocolId is the independence requirement: packets and
// tables version independently, so ADDING a table (and everything in its
// closure that was already there) moves neither the projection nor the id —
// and neither does EDITING one.
func TestTablesMoveNoProtocolId(t *testing.T) {
	withTable := tablelessSrc + `
table Config
{
    scale  float32 = 1.0
    points [..8]Point
}
`
	editedTable := tablelessSrc + `
table Config
{
    scale   float32 = 2.0
    points  [..8]Point
    comment string(32)
}
`
	base := buildUnit(t, tablelessSrc)
	added := buildUnit(t, withTable)
	edited := buildUnit(t, editedTable)

	if ir.WireProjection(base) != ir.WireProjection(added) {
		t.Fatalf("adding a table changed the wire projection:\n--- without ---\n%s\n--- with ---\n%s",
			ir.WireProjection(base), ir.WireProjection(added))
	}
	if base.ProtocolId != added.ProtocolId {
		t.Fatalf("adding a table moved the protocol id %#x -> %#x", base.ProtocolId, added.ProtocolId)
	}
	if added.ProtocolId != edited.ProtocolId {
		t.Fatalf("editing a table moved the protocol id %#x -> %#x", added.ProtocolId, edited.ProtocolId)
	}
}

// TestTableFieldIds pins the id function and the was override.
func TestTableFieldIds(t *testing.T) {
	// frozen values: the id is WIRE FORMAT — a change here breaks every
	// stored table on earth
	pins := map[string]uint16{
		"velocity": 0x2e46,
		"damage":   0x15a9,
		"name":     0x30df,
	}
	for name, want := range pins {
		if got := ir.FieldId(name); got != want {
			t.Errorf("FieldId(%q) = %#04x, want %#04x — the table-wire id function moved", name, got, want)
		}
	}

	u := buildUnit(t, "package t\ntable Tab { speed float32 | was = \"velocity\" }\n")
	f := u.Tables["Tab"].Fields[0]
	if got := ir.TableFieldId(f); got != ir.FieldId("velocity") {
		t.Errorf("was alias not honored: id %#04x, want hash of the old name %#04x", got, ir.FieldId("velocity"))
	}
	if ir.TableFieldId(f) == ir.FieldId("speed") {
		t.Error("was alias ignored — the renamed field kept its new-name id")
	}
}

// TestTablesInvisibleToPacketIR: tables live beside the decl stream, never in
// it — Structs, File.Decls and the projection must not see them.
func TestTablesInvisibleToPacketIR(t *testing.T) {
	u := buildUnit(t, tablelessSrc+"\ntable Config { scale float32 = 1.0 }\n")
	if _, leaked := u.Structs["Config"]; leaked {
		t.Fatal("a table leaked into Unit.Structs — the packet backends would emit it")
	}
	if u.Tables["Config"] == nil {
		t.Fatal("the table is missing from Unit.Tables")
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && st.IsTable {
				t.Fatal("a table leaked into File.Decls — the packet backends would emit it")
			}
		}
	}
	if strings.Contains(ir.WireProjection(u), "Config") {
		t.Fatal("a table leaked into the wire projection — the protocol id depends on it")
	}
}

// TestTableModeDerivation: the mode is DERIVED, never declared. Fixed-size is
// the pointer-free by-value closure — a plain struct, exactly as every table
// was before pointers existed. Variable-length is a pointer anywhere in that
// closure, and the mode propagates UP through by-value nesting only
// (SPEC-TABLES.md §2).
func TestTableModeDerivation(t *testing.T) {
	u := buildUnit(t, `package t

type Plain
{
    x int32
}

table Leaf
{
    v int32
}

table Node
{
    v    int32
    next *Node
}

table HoldsPointer
{
    head *Leaf
}

table NestsVariable
{
    inner Node
}

table ArrayOfVariable
{
    kids [..4]Node
}

table StaysFixed
{
    leaf  Leaf
    p     Plain
    items [..4]int32
}
`)
	variable := ir.VariableTables(u)
	for _, name := range []string{"Node", "HoldsPointer", "NestsVariable", "ArrayOfVariable"} {
		if !variable[name] {
			t.Errorf("%s should derive VARIABLE-LENGTH (a pointer in its by-value closure)", name)
		}
	}
	for _, name := range []string{"Leaf", "StaysFixed", "Plain"} {
		if variable[name] {
			t.Errorf("%s should stay FIXED-SIZE — no pointer in its by-value closure, so it pays for none of the machinery", name)
		}
	}

	// pointing AT a variable table does not make the pointer's owner's
	// NESTER variable through the pointer edge — only the declaring table
	targets := ir.PointerTargets(u)
	if !targets["Node"] || !targets["Leaf"] {
		t.Errorf("pointer targets missing: %v", targets)
	}
	if targets["StaysFixed"] {
		t.Error("StaysFixed is nobody's pointer target")
	}
}

// TestPointerRecursionIsLegal: recursion through a POINTER edge is the whole
// point of the freedom tables were given — the by-value cycle refusal must
// exempt it (SPEC-TABLES.md §2).
func TestPointerRecursionIsLegal(t *testing.T) {
	u := buildUnit(t, "package t\ntable Node\n{\n    v    int32\n    next *Node\n}\n")
	f := u.Tables["Node"].Fields[1]
	if !f.Type.Pointer {
		t.Fatal("the *Node field did not resolve as a pointer")
	}
	if f.Type.Ref == nil || f.Type.Name != "Node" {
		t.Fatalf("the pointer's target did not resolve: %+v", f.Type)
	}
}
