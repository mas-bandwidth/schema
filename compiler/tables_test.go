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

// TestZeroCostForValueOnlyTables is the mode's whole justification: a table
// with no pointer in its by-value closure must pay NOTHING for the pointer
// machinery — no builder, no arena, no handles, no lifecycle surface, no extra
// descriptor columns (SPEC-TABLES.md §2).
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
	// Plain has no pointer in its by-value closure: it stays a plain struct
	// with the three free functions and gets no builder of its own
	if strings.Contains(header, "struct PlainBuilder") {
		t.Error("a value-only table in a pointered unit grew a Builder")
	}
	if !strings.Contains(header, "inline int64_t PlainMeasure( const Plain & value )") {
		t.Error("a value-only table in a pointered unit lost its by-value Measure")
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

// TestLayoutIdMovesWithTheSchema: the cooked form's build lock. A schema edit
// that changes the packed layout must change the layout id, or a stale cooked
// file would be pointed at and misread.
func TestLayoutIdMovesWithTheSchema(t *testing.T) {
	base := layoutIdOf(t, tableHeader(t, pointerSrc))
	widened := layoutIdOf(t, tableHeader(t, strings.Replace(pointerSrc, "value int32", "value int64", 1)))
	if base == widened {
		t.Error("widening a field did not move the layout id — a stale cooked file would be pointed at")
	}
	renamed := layoutIdOf(t, tableHeader(t, strings.Replace(pointerSrc, "meta Plain", "info Plain", 1)))
	if base == renamed {
		t.Error("renaming a field did not move the layout id")
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
