// Tests for the tables generation surface (SPEC-TABLES.md): the C++ target
// grows Table headers, every other target refuses BY NAME, non-table output
// is byte-identical with or without tables, and the generated codecs
// allocate nothing.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitFromSource(t *testing.T, src string) *ir.Unit {
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
	return u
}

const packetSrc = `package probe

enum Kind { Alpha, Beta }

type Point
{
    x float32
    y float32
}

type Packet
{
    kind  Kind
    items [..16]Point
}
`

const tableSrc = packetSrc + `
table Config
{
    scale  float32 = 1.0
    label  string(24)
    points [..8]Point
}
`

// TestNonCppTargetsRefuseTables: a unit declaring tables is refused by name
// under every target but cpp — loudly, never by silently dropping the tables.
func TestNonCppTargetsRefuseTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, tableSrc)
	for _, target := range []string{"c", "cs", "dart", "elixir", "go", "java", "js", "rust"} {
		if _, err := c.Generate(u, target, Options{}); err == nil {
			t.Errorf("--lang %s accepted a unit with tables — it must refuse by name", target)
		} else if !strings.Contains(err.Error(), "C++-only") || !strings.Contains(err.Error(), "Config") {
			t.Errorf("--lang %s refusal does not name the rule and the tables: %v", target, err)
		}
	}
	// the aliases refuse too
	if _, err := c.Generate(u, "csharp", Options{}); err == nil {
		t.Error("--lang csharp accepted a unit with tables")
	}
}

// TestCppEmitsTableHeaders: the cpp target adds <Base>Table.h beside the
// packet headers for a unit with tables, and adds NOTHING for one without.
func TestCppEmitsTableHeaders(t *testing.T) {
	c := New()

	with, err := c.Generate(unitFromSource(t, tableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := with["ProbeTable.h"]; !ok {
		t.Fatalf("no ProbeTable.h emitted for a unit with tables; got %d files", len(with))
	}

	without, err := c.Generate(unitFromSource(t, packetSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.h") {
			t.Fatalf("a table-free unit emitted %s", name)
		}
	}
}

// TestTablesMoveNoGeneratedPacketByte is the other half of the independence
// proof: beyond the protocol id, adding a table changes not one byte of the
// NON-TABLE generated output, in the C++ target and in every refusing
// target's view of the world (they see identical units either way).
func TestTablesMoveNoGeneratedPacketByte(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range without {
		got, ok := with[name]
		if !ok {
			t.Errorf("file %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("file %s changed when a table was added — tables must move no packet byte", name)
		}
	}
	for name := range with {
		if _, ok := without[name]; !ok && !strings.HasSuffix(name, "Table.h") {
			t.Errorf("adding a table grew unexpected non-table file %s", name)
		}
	}
}

// TestGeneratedTableCodeAllocatesNothing: the generated Table header must
// hold zero allocation and zero standard-container usage — the caller owns
// every buffer. The one `new` is the placement-new prefill.
func TestGeneratedTableCodeAllocatesNothing(t *testing.T) {
	c := New()
	files, err := c.Generate(unitFromSource(t, tableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	table := string(files["ProbeTable.h"])
	if table == "" {
		t.Fatal("no ProbeTable.h")
	}
	for _, banned := range []string{"malloc", "std::vector", "std::string", "std::unique_ptr", "std::shared_ptr", "delete "} {
		if strings.Contains(table, banned) {
			t.Errorf("generated table code contains %q — generated codecs must not allocate", banned)
		}
	}
	for line := range strings.SplitSeq(table, "\n") {
		if found := strings.Contains(line, "new "); found && !strings.Contains(line, "new ( &") && !strings.Contains(line, "<new>") {
			t.Errorf("generated table code contains a non-placement new: %q", line)
		}
	}
	// the load-bearing surfaces are present
	for _, want := range []string{
		"ConfigMeasure", "ConfigSave", "ConfigLoad", "ConfigSaveBody", "ConfigLoadBody",
		"ConfigTableType", "struct TableReport",
		"static_assert( std::is_trivially_copyable<Config>::value",
		"static_assert( std::is_standard_layout<Config>::value",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("generated table header is missing %q", want)
		}
	}
	// no serialize dependency (the banner SAYS "no serialize dependency", so
	// the probes are the load-bearing spellings: the include and the symbols)
	for _, banned := range []string{"#include \"serialize", "serialize::", "serialize_"} {
		if strings.Contains(table, banned) {
			t.Errorf("generated table header contains %q — it must stand alone", banned)
		}
	}
}

const pointerSrc = packetSrc + `
table Leaf
{
    quality int32 = 2 | min = 0, max = 4
}

table Node
{
    value int32
    next  *Node
}

table Plain
{
    scale float32 = 1.0
}

table Root
{
    head *Node
    leaf *Leaf
    meta Plain
}
`

// tableHeader returns the generated Table header's text.
func tableHeader(t *testing.T, src string) string {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, body := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(body)
		}
	}
	t.Fatal("no Table header generated")
	return ""
}

// TestZeroCostForValueOnlyTables is the mode's whole justification, at the
// grain the property actually holds: a UNIT whose tables are all fixed-size
// emits none of the pointer machinery — no builder, no arena, no reference
// type, no lifecycle surface, not one extra descriptor column, not one extra
// include (SPEC-TABLES.md §2.2). Per TABLE the guarantee is narrower and
// TestPointerSurfaceEmitted states it exactly.
func TestZeroCostForValueOnlyTables(t *testing.T) {
	header := tableHeader(t, tableSrc)
	for _, leak := range []string{
		"TableArena", "TableSlot", "TableWorker", "TableRef", "TableRegion",
		"kTableSegment", "kTableSlab", "kTableMaxDepth", "is_pointer", "variable",
		"Builder", "LayoutId", "OpenWalk", "PackMeasure", "LoadMeasure",
		"Cook", "Open", "<atomic>", "<cstdlib>", "template",
	} {
		if strings.Contains(header, leak) {
			t.Errorf("pointer machinery leaked into a value-only unit: %q", leak)
		}
	}
	// what a value-only table DOES get: the struct and the three free functions
	for _, want := range []string{
		"struct Config", "ConfigMeasure", "ConfigSave", "ConfigLoad",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("value-only table lost its surface: %q missing", want)
		}
	}
}

// TestPointerSurfaceEmitted: the variable-length surface appears exactly where
// a pointer does, and the value-only table SHARING THE UNIT still gets none of
// it — the mode is per table, not per file.
func TestPointerSurfaceEmitted(t *testing.T) {
	header := tableHeader(t, pointerSrc)
	for _, want := range []string{
		"struct TableArena", "struct TableRef", "struct TableSlot",
		"struct RootBuilder", "struct NodeBuilder",
		"RootLayoutId", "RootCook", "RootOpen", "RootLoadMeasure", "RootOpenWalk",
		"NodeAt(", "NodeEmplace(", "TableRef next;", "TableRef head;",
		"#include <atomic>",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("the pointer surface is missing %q", want)
		}
	}
	// ---- the PER-TABLE property, stated exactly ----
	//
	// A value-only table sharing a unit with pointered ones gets: no Builder,
	// no pointer-graph walkers, and codecs identical to the ones it would have
	// had in a pointer-free unit — no ctx parameter, no depth parameter, no
	// template. What it DOES share with the unit are the per-UNIT facts: the
	// arena runtime and the two extra descriptor columns, which are emitted
	// once per header and belong to the unit, not to any table in it.
	for _, absent := range []string{
		"struct PlainBuilder", "PlainPackMeasure", "PlainPack(",
		"PlainLoadMeasureBody", "PlainEmplace", "PlainCook", "PlainOpen(",
		"PlainLayoutId", "PlainLoadBuilder",
	} {
		if strings.Contains(header, absent) {
			t.Errorf("a value-only table in a pointered unit grew %q", absent)
		}
	}
	// its codecs are the by-value ones, character for character
	for _, want := range []string{
		"inline int64_t PlainMeasure( const Plain & value )",
		"inline bool PlainSaveBody( TableWriter & w, const Plain & value )",
		"inline bool PlainLoadBody( TableReader & r, Plain & value )",
		"inline bool PlainLoad( Plain & value, const uint8_t * buffer, int64_t bytes, TableReport * report )",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("a value-only table in a pointered unit lost its by-value codec: %q", want)
		}
	}
	// it DOES get an Open walk: Open bounds the count companions of every
	// by-value nested member, and a fixed table nested in a variable root is
	// exactly such a member
	if !strings.Contains(header, "PlainOpenWalk") {
		t.Error("a value-only table lost its Open bounds walk — its companions would go unchecked")
	}
	// Leaf is POINTED AT but pointer-free: it needs allocation and resolution,
	// and still no builder
	if strings.Contains(header, "struct LeafBuilder") {
		t.Error("a pointed-at but pointer-free table grew a Builder")
	}
	if !strings.Contains(header, "LeafEmplace(") {
		t.Error("a pointed-at table lost its allocation entry")
	}
	// a variable-length table is never held by value: no by-value Load
	if strings.Contains(header, "inline bool RootLoad( Root & value") {
		t.Error("a pointer-bearing table emitted a by-value Load")
	}
}

// TestPointerGenerationDeterministic: regeneration is byte-stable, pointer
// graphs and layout ids included.
func TestPointerGenerationDeterministic(t *testing.T) {
	first := tableHeader(t, pointerSrc)
	for range 3 {
		if again := tableHeader(t, pointerSrc); again != first {
			t.Fatal("regeneration is not byte-stable across runs")
		}
	}
}

// TestLayoutIdMovesWithTheSchema: the cooked form's build lock. The rule is
// exact — it MOVES when the packed layout moves, and HOLDS when it does not,
// because a false alarm invalidates every cooked file in existence.
func TestLayoutIdMovesWithTheSchema(t *testing.T) {
	base := layoutIdOf(t, tableHeader(t, pointerSrc))

	moves := []struct {
		name string
		src  string
	}{
		{"widening a field", strings.Replace(pointerSrc, "value int32", "value int64", 1)},
		{"renaming a field without was", strings.Replace(pointerSrc, "meta Plain", "info Plain", 1)},
		{"reordering two fields", strings.Replace(pointerSrc,
			"    head *Node\n    leaf *Leaf", "    leaf *Leaf\n    head *Node", 1)},
		{"adding a field", strings.Replace(pointerSrc, "    meta Plain", "    extra int32\n    meta Plain", 1)},
		{"turning a by-value nesting into a pointer", strings.Replace(pointerSrc, "meta Plain", "meta *Plain", 1)},
	}
	for _, tc := range moves {
		if layoutIdOf(t, tableHeader(t, tc.src)) == base {
			t.Errorf("%s did not move the layout id — a stale cooked file would be pointed at", tc.name)
		}
	}
	_ = base

	// HOLDS: a `was` rename moves no byte. The field keeps its wire id, keeps
	// its offset, and every cooked file in the world stays valid — so the id
	// must not budge. This is why the digest keys fields by WIRE ID and not by
	// source name.
	// The id's VALUE is the schema digest xor'd with this build's sizeof and
	// offsetof terms. A `was` rename cannot change any of those values — same
	// type, same field order, same offsets — so identity of the digest plus
	// identity of the term structure IS identity of the value. The field's
	// spelling inside offsetof() is the only thing allowed to differ, and
	// normalising it is what separates "the value held" from "the text held".
	renamedWithWas := strings.Replace(pointerSrc, "    meta Plain", "    facts Plain | was = \"meta\"", 1)
	renamedExpr := layoutIdOf(t, tableHeader(t, renamedWithWas))
	if layoutDigestOf(t, renamedExpr) != layoutDigestOf(t, base) {
		t.Error("a `was` rename moved the layout id's schema digest — it moves no byte, and invalidating every cooked file for it is a false alarm")
	}
	if normalizeOffsets(renamedExpr) != normalizeOffsets(base) {
		t.Error("a `was` rename changed the layout id's sizeof/offsetof terms — a rename moves no member, so every term must be value-identical")
	}
}

// layoutDigestOf returns the schema-side digest constant that opens a layout
// id expression.
func layoutDigestOf(t *testing.T, expr string) string {
	t.Helper()
	i := strings.Index(expr, "0x")
	if i < 0 {
		t.Fatalf("no digest constant in %q", expr)
	}
	end := strings.IndexByte(expr[i:], '\n')
	if end < 0 {
		t.Fatalf("no digest constant in %q", expr)
	}
	return strings.TrimSpace(expr[i : i+end])
}

// normalizeOffsets rewrites `offsetof( T, field )` to `offsetof( T, ? )`, so
// two expressions compare on the terms' VALUES rather than on the spelling of
// a field that was renamed.
func normalizeOffsets(expr string) string {
	var b strings.Builder
	for {
		i := strings.Index(expr, "offsetof( ")
		if i < 0 {
			b.WriteString(expr)
			return b.String()
		}
		comma := strings.Index(expr[i:], ", ")
		close := strings.Index(expr[i:], " )")
		if comma < 0 || close < 0 {
			b.WriteString(expr)
			return b.String()
		}
		b.WriteString(expr[:i+comma])
		b.WriteString(", ? ")
		expr = expr[i+close+2:]
	}
}

// TestLayoutIdCarriesTheABI: the digest is not schema-only. Field offsets ride
// in it as compile-time terms, so a padding or ABI difference that shifts a
// member refuses the cooked file instead of misreading it — a fact no
// schema-side test can produce, and one the emitted expression must show.
func TestLayoutIdCarriesTheABI(t *testing.T) {
	expr := layoutIdOf(t, tableHeader(t, pointerSrc))
	if !strings.Contains(expr, "sizeof( Root )") {
		t.Error("the layout id does not mix this build's sizeof")
	}
	for _, want := range []string{
		"offsetof( Root, head )", "offsetof( Root, leaf )", "offsetof( Root, meta )",
		"offsetof( Node, next )", "offsetof( Leaf, quality )",
	} {
		if !strings.Contains(expr, want) {
			t.Errorf("the layout id does not mix %s — a member that moved would not refuse", want)
		}
	}
}

func layoutIdOf(t *testing.T, header string) string {
	t.Helper()
	i := strings.Index(header, "inline constexpr uint32_t RootLayoutId =")
	if i < 0 {
		t.Fatal("no RootLayoutId in the generated header")
	}
	end := strings.Index(header[i:], ";")
	return header[i : i+end]
}
