// Tests for the tables generation surface (docs/SPEC-TABLES.md): the C++ and C#
// targets grow table sources, every other target refuses BY NAME, non-table
// output is byte-identical with or without tables, and the generated codecs
// allocate nothing.
package compiler

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
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

// tableSrc adds a table whose closure reaches an ENUM as well as a nested
// type: the enum is what makes the per-enum identity pair (TableEnumId /
// TableEnumValue) part of the generated surface, which
// TestTableRuntimeNamesAreClaimed scans for. Kind is declared by packetSrc
// already, so this adds a table and no other declaration — the independence
// proof below still compares like with like.
const tableSrc = packetSrc + `
table Config
{
    scale  float32 = 1.0
    label  string(24)
    grade  Kind
    points [..8]Point
}
`

// runtimeSrc is tableSrc plus the two FIXED-CLASS spellings whose runtime
// names would otherwise never be emitted here: an enum-keyed array brings
// TableKeyed, and an optional brings the presence surface. It is separate
// from tableSrc on purpose — the zero-cost gate reads tableSrc and a keyed
// array legitimately emits a C++ class template, which that gate greps for.
const runtimeSrc = tableSrc + `
table Keyed
{
    slots [Kind]int32
    extra ?Point
}
`

// TestTablelessTargetsRefuseTables: a unit declaring tables is refused by name
// under every target that carries no table backend — loudly, never by silently
// dropping the tables. cpp, cs and rust carry one (docs/SPEC-TABLES.md, backend
// status).
func TestTablelessTargetsRefuseTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, tableSrc)
	for _, target := range []string{"c", "dart", "elixir", "go", "java", "js"} {
		if _, err := c.Generate(u, target, Options{}); err == nil {
			t.Errorf("--lang %s accepted a unit with tables — it must refuse by name", target)
		} else if !strings.Contains(err.Error(), "C++, C# and Rust only") || !strings.Contains(err.Error(), "Config") {
			t.Errorf("--lang %s refusal does not name the rule and the tables: %v", target, err)
		}
	}
}

// TestRustEmitsTableModules: the rust target adds the table modules beside the
// packet ones for a unit with tables, declares them in the generated crate
// root, and adds NOTHING for a unit without — the same contract the cpp and cs
// targets hold.
func TestRustEmitsTableModules(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "rust", Options{})
	if err != nil {
		t.Fatalf("--lang rust: %v", err)
	}
	for _, want := range []string{"probe_table.rs", "table_runtime.rs", "probe_block.rs", "probe_cook.rs"} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang rust emitted no %s for a unit with tables; got %d files", want, len(with))
		}
	}
	lib := string(with["lib.rs"])
	for _, want := range []string{"mod probe_table;", "mod table_runtime;", "pub use probe_table::*;"} {
		if !strings.Contains(lib, want) {
			t.Errorf("the generated crate root does not declare the table surface: %q missing", want)
		}
	}

	without, err := c.Generate(unitFromSource(t, packetSrc), "rust", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name := range without {
		if name != "lib.rs" && !strings.HasSuffix(name, ".rs") {
			continue
		}
		if strings.Contains(name, "_table") || strings.Contains(name, "_block") ||
			strings.Contains(name, "_cook") || strings.HasPrefix(name, "table_") {
			t.Errorf("--lang rust emitted %s for a table-free unit", name)
		}
	}
	// and adding a table moves not one byte of the PACKET modules
	for name, data := range without {
		if name == "lib.rs" {
			continue // the crate root grows the table modules' declarations
		}
		got, ok := with[name]
		if !ok {
			t.Errorf("rust module %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("rust module %s changed when a table was added — tables must move no packet byte", name)
		}
	}
}

// TestRustRefusesPointeredTables: the Rust variable-class refusal is a refusal
// of the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11), exactly as
// the C# one is. The wire codec is the half the variable class is missing — the
// arena, the builder, the region, the node table — and the two ACCELERATORS
// need none of it: a block and a cook are POINTED AT, not parsed.
func TestRustRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "rust", Options{})
	if err != nil {
		t.Fatalf("--lang rust refused a pointered unit outright — the accelerators need no codec: %v", err)
	}
	cooks := 0
	for name, data := range files {
		if strings.HasSuffix(name, "_table.rs") || name == "table_runtime.rs" {
			t.Errorf("--lang rust emitted the WIRE surface %s for a pointered unit", name)
		}
		if !strings.HasSuffix(name, "_cook.rs") && !strings.HasSuffix(name, "_block.rs") {
			continue
		}
		if strings.HasSuffix(name, "_cook.rs") {
			cooks++
		}
		text := string(data)
		if !strings.Contains(text, "THE RUST WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(text, "Node") || !strings.Contains(text, "named follow-on") {
			t.Errorf("%s does not name the table and the follow-on", name)
		}
	}
	if cooks == 0 {
		t.Error("--lang rust emitted no cook reader for a pointered unit — a root is any table (docs/SPEC-TABLES.md §7)")
	}
	for name, data := range files {
		if !strings.HasSuffix(name, "_cook.rs") {
			continue
		}
		text := string(data)
		if !strings.Contains(text, "pub struct NodeCook") {
			t.Errorf("%s declares no NodeCook", name)
		}
		if !strings.Contains(text, "pub unsafe fn open(bytes: *const u8, length: u64) -> Option<NodeCook>") {
			t.Errorf("%s declares NodeCook without the pointer-and-length open", name)
		}
		if !strings.Contains(text, "pub unsafe fn node_at(slot: *const i64) -> *const NodeRow") {
			t.Errorf("%s declares NodeCook without node_at, which is how a reference is dereferenced (§6.3)", name)
		}
	}
}

// TestRustTableRuntimeNamesAreClaimed is the Rust half of the §11 promise: no
// legal schema reaches a generated Rust module that does not compile.
//
// WHAT THE SCAN COLLECTS, and why it is narrower than the C# one. In Rust a
// declaration produces a TYPE (PascalCase, exactly as declared) or — for a
// const or a flags variant — a SCREAMING_SNAKE value. It never produces a bare
// snake_case item, so the runtime's snake_case free functions
// (table_json_read, table_cook_open, table_block_read64 …) are spellings no
// declaration can reach, and registering forty walk helpers would take forty
// names away from every schema for nothing. The scan therefore collects the two
// classes that CAN collide — Table* identifiers and TABLE_* constants,
// normalised back to the PascalCase a schema would spell — and requires the
// whole set to be registered.
func TestRustTableRuntimeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "rust", Options{})
	if err != nil {
		t.Fatal(err)
	}
	pascal := regexp.MustCompile(`\bTable[A-Za-z0-9_]*\b`)
	// BUILD_VERSION is the one unit-level name the generated table sources
	// define that is not a Table* spelling, and the registry already says so.
	screaming := regexp.MustCompile(`\b(?:TABLE_[A-Z0-9_]+|BUILD_VERSION)\b`)
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range pascal.FindAllString(line, -1) {
				emitted[m] = true
			}
			for _, m := range screaming.FindAllString(line, -1) {
				emitted[screamingToPascal(m)] = true
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted Rust at all — the scan, not the registry, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Rust table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Rust that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Rust) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Rust backend defines %s, but nothing in the emitted "+
				"Rust names it — drop the registration or fix the backend", name)
		}
	}
}

// screamingToPascal maps a generated Rust constant back to the declaration
// spelling a schema author would write: TABLE_JSON_MAX_KEY -> TableJsonMaxKey.
// It is the inverse of ir.RustConstName over the names this registry carries.
func screamingToPascal(name string) string {
	var b strings.Builder
	for part := range strings.SplitSeq(strings.ToLower(name), "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// TestCsEmitsTableSources: the cs target adds <Base>Table.cs beside the packet
// sources for a unit with tables, and adds NOTHING for one without — the same
// contract the cpp target holds, under both spellings of the target name.
func TestCsEmitsTableSources(t *testing.T) {
	c := New()
	for _, target := range []string{"cs", "csharp"} {
		with, err := c.Generate(unitFromSource(t, tableSrc), target, Options{})
		if err != nil {
			t.Fatalf("--lang %s: %v", target, err)
		}
		if _, ok := with["ProbeTable.cs"]; !ok {
			t.Fatalf("--lang %s emitted no ProbeTable.cs for a unit with tables; got %d files", target, len(with))
		}
		without, err := c.Generate(unitFromSource(t, packetSrc), target, Options{})
		if err != nil {
			t.Fatalf("--lang %s: %v", target, err)
		}
		for name := range without {
			if strings.HasSuffix(name, "Table.cs") {
				t.Errorf("--lang %s emitted %s for a table-free unit", target, name)
			}
		}
	}
}

// TestCsRefusesPointeredTables: the C# variable-class refusal is a refusal of
// the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11). The wire codec is
// the half the variable class is missing — the arena, the builder, the region,
// the node table — and the two ACCELERATORS need none of it: a block and a cook
// are POINTED AT, not parsed, so both are emitted and the cook's <Root>Open
// opens this unit's cooked assets in full.
//
// NAMED, NEVER SILENT is what this holds: no Table source at all, and every
// source the unit does emit opening with a banner that names each refused table
// and the follow-on. A consumer reaching for Save or Load gets a missing name
// from its own compiler, beside a file that says why.
func TestCsRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "cs", Options{})
	if err != nil {
		t.Fatalf("--lang cs refused a pointered unit outright — the accelerators need no codec: %v", err)
	}
	var cooks int
	for name, data := range files {
		if strings.HasSuffix(name, "Table.cs") {
			t.Errorf("--lang cs emitted the WIRE surface %s for a pointered unit", name)
		}
		if !strings.HasSuffix(name, "Cook.cs") && !strings.HasSuffix(name, "Block.cs") {
			continue
		}
		if strings.HasSuffix(name, "Cook.cs") {
			cooks++
		}
		text := string(data)
		if !strings.Contains(text, "THE C# WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(text, "Node") || !strings.Contains(text, "is a named follow-on") {
			t.Errorf("%s does not name the table and the follow-on", name)
		}
	}
	if cooks == 0 {
		t.Error("--lang cs emitted no cook reader for a pointered unit — a root is any table (docs/SPEC-TABLES.md §7)")
	}
	// and the cook's own surface is there: <Root>Cook with Open and At on it
	for name, data := range files {
		if !strings.HasSuffix(name, "Cook.cs") {
			continue
		}
		text := string(data)
		if strings.Contains(text, "struct NodeCook") {
			if !strings.Contains(text, "public static bool Open(out NodeCook cook, IntPtr pointer, long length)") {
				t.Errorf("%s declares NodeCook without the pointer-and-length Open", name)
			}
			if !strings.Contains(text, "public static NodeRow* At(long* slot)") {
				t.Errorf("%s declares NodeCook without At, which is how a reference is dereferenced (§6.3)", name)
			}
		}
	}
	// cpp still carries both classes
	if _, err := c.Generate(u, "cpp", Options{}); err != nil {
		t.Errorf("--lang cpp refused a pointered unit: %v", err)
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
	// A table declaration grows the two TABLE files and the two BLOCK files
	// and nothing else: the header a consumer includes, the .cpp carrying the
	// text form's walk (docs/SPEC-TABLES.md §6.1, §13.5), and the block form's own
	// pair, which nothing declares and which a consumer includes only if it
	// uses the form (§19). The type wire stays header-only, so no packet file
	// appears or moves.
	for name := range with {
		if _, ok := without[name]; ok {
			continue
		}
		if !strings.HasSuffix(name, "Table.h") && !strings.HasSuffix(name, "Table.cpp") &&
			!strings.HasSuffix(name, "Block.h") && !strings.HasSuffix(name, "Block.cpp") {
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
	// every `new` in the header is a PLACEMENT new — `new ( address ) T{}`,
	// which allocates nothing: the read path's in-place prefill, and the
	// descriptor's reset hook (docs/SPEC-TABLES.md §8) which is the same prefill
	// behind a function pointer. An allocating new has no parenthesis after it.
	for line := range strings.SplitSeq(table, "\n") {
		if found := strings.Contains(line, "new "); found && !strings.Contains(line, "new (") && !strings.Contains(line, "<new>") {
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
// emits none of the POINTER machinery — no builder, no arena, no reference
// type, no lifecycle surface, not one extra descriptor column, not one extra
// include (docs/SPEC-TABLES.md §2.2). Per TABLE the guarantee is narrower and
// TestPointerSurfaceEmitted states it exactly.
//
// What the gate is NOT about: the reflection surface and the text form that
// walks it ride in every table closure's header by design, fixed class
// included (docs/SPEC-TABLES.md §16.1), and <cstdlib> is one of the text form's
// three number-conversion includes rather than the arena's — which is why it
// left this list when the walk landed.
func TestZeroCostForValueOnlyTables(t *testing.T) {
	header := tableHeader(t, tableSrc)
	for _, leak := range []string{
		"TableArena", "TableSlot", "TableWorker", "TableRef", "TableRegion",
		"kTableSegment", "kTableSlab", "kTableMaxDepth", "is_pointer", "variable",
		"Builder", "PackMeasure", "LoadMeasure", "<atomic>", "template",
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
		"RootLoadMeasure", "RootPackMeasure", "RootPack(",
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
		"PlainLoadMeasureBody", "PlainEmplace", "PlainLoadBuilder",
	} {
		if strings.Contains(header, absent) {
			t.Errorf("a value-only table in a pointered unit grew %q", absent)
		}
	}
	// its codecs are the by-value ones, character for character — including
	// the force-inline qualifier the FIXED class carries and the
	// variable-length templates do not (schema#343): the two classes differ in
	// what the compiler is allowed to leave out of line, and that difference is
	// per table, exactly as the parameter list is.
	for _, want := range []string{
		"inline int64_t PlainMeasure( const Plain & value )",
		"PROBE_TABLE_INLINE bool PlainSaveBody( TableWriter & w, const Plain & value )",
		"PROBE_TABLE_INLINE bool PlainLoadBody( TableReader & r, Plain & value )",
		"inline bool PlainLoad( Plain & value, const uint8_t * buffer, int64_t bytes, TableReport * report )",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("a value-only table in a pointered unit lost its by-value codec: %q", want)
		}
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
	// THE RECURSION GUARD (schema#343). The force-inline qualifier stops at the
	// fixed class because that is the class whose save/load call graph cannot
	// hold a cycle: a fixed table nests by value, and a by-value cycle is an
	// infinite `sizeof`. The variable-length form reaches its pointee through
	// the depth-carrying template, which a self-referential declaration makes
	// directly recursive — and a recursive always_inline does not compile under
	// gcc. Every line carrying the qualifier must therefore be a non-template
	// one, or the emitter has emitted source no gcc build can compile.
	for line := range strings.SplitSeq(header, "\n") {
		if strings.Contains(line, "PROBE_TABLE_INLINE") && strings.Contains(line, "template") {
			t.Errorf("the force-inline qualifier reached a template — recursion is possible there: %q", line)
		}
	}
	if strings.Contains(header, "PROBE_TABLE_INLINE bool RootSaveBody") ||
		strings.Contains(header, "PROBE_TABLE_INLINE bool NodeSaveBody") {
		t.Error("a variable-length table's body was force-inlined; its pointer walk can recurse")
	}
}

// TestPointerGenerationDeterministic: regeneration is byte-stable, pointer
// graphs included.
func TestPointerGenerationDeterministic(t *testing.T) {
	first := tableHeader(t, pointerSrc)
	for range 3 {
		if again := tableHeader(t, pointerSrc); again != first {
			t.Fatal("regeneration is not byte-stable across runs")
		}
	}
}

// TestTableRuntimeNamesAreClaimed is the SELF-MAINTAINING half of the §11
// promise: no legal schema reaches a generated source that does not compile.
//
// internal/tablenames is the one registry — the checker claims from it, and
// this test holds it honest against what the emitter actually emits. The two
// halves are asserted in both directions:
//
//   - EVERY Table* identifier in the emitted C# is registered. A runtime name
//     somebody added to the emitter and forgot to register fails here.
//   - EVERY name the registry says C# defines appears in the emitted C#. A
//     name that outlived its emitter fails here, so the list cannot rot into
//     claims nothing needs.
//   - EVERY registered name is refused by the checker when a unit declares a
//     table, and accepted when it does not.
//
// THE SCAN IS SHAPE-INDEPENDENT ON PURPOSE. An earlier version matched two
// declaration idioms with two regexes and silently missed five others a port
// could plausibly reach for — a non-ref struct, an enum, a static readonly
// field, a generic method, a non-sealed class. A scan that has to recognise
// declaration syntax is a scan that goes quietly blind the day the syntax
// changes. This one recognises none: it collects every Table*-prefixed
// identifier in the emitted text, declaration or use or comment, and requires
// the whole set to be registered. Over-collection is the safe direction — the
// cost of a false hit is registering a name or rewording a comment, and the
// cost of a miss is a legal schema that does not compile.
//
// The dangerous shape, for the record: an ENUM. `enum TableReset { A, B }`
// puts TableReset in expression position (TableReset.A), where it resolves to
// the method group rather than the type — CS0119.
func TestTableRuntimeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "cs", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Every Table*-prefixed identifier, whatever surrounds it. The word
	// boundary is what keeps the name-first spellings out: RootConfigTableType
	// has no boundary before its Table, and is claimed by suffix instead
	// (docs/SPEC-TABLES.md §11). Line comments are stripped first — prose is
	// not an identifier, and scanning it would make the gate a spelling
	// police for the runtime's own documentation.
	ident := regexp.MustCompile(`\bTable[A-Za-z0-9_]*\b`)
	// the unit's own namespace is capitalize(package), which starts with
	// Table for a package named table*: it is the schema's name, not the
	// runtime's, and claims nothing
	namespace := capitalizeFirst(unitFromSource(t, runtimeSrc).Package)
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range ident.FindAllString(line, -1) {
				if m != namespace {
					emitted[m] = true
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted C# at all — the scan, not the registry, is what broke")
	}

	emittedNames := make([]string, 0, len(emitted))
	for name := range emitted {
		emittedNames = append(emittedNames, name)
	}
	sort.Strings(emittedNames)

	for _, name := range emittedNames {
		if !tablenames.Registered(name) {
			t.Errorf("the C# table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate C# that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Cs) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the C# backend defines %s, but nothing in the emitted "+
				"C# names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}

	// and the claim itself: every registered name is refused when a unit
	// declares a table, and kept when it does not
	for _, name := range tablenames.Claimed() {
		t.Run(name, func(t *testing.T) {
			refused := "package probe\n\nenum " + name + " { A, B }\n\ntable Holder\n{\n    g " + name + "\n}\n"
			errs := checkErrors(t, refused)
			if len(errs) == 0 {
				t.Fatalf("a declaration named %s was accepted — the generated table runtime defines that "+
					"name, so the unit cannot compile; internal/tablenames registers it, so the claim in "+
					"internal/check is what broke", name)
			}
			named := false
			for _, e := range errs {
				if strings.Contains(e.Error(), "TABLE-wire runtime") {
					named = true
				}
			}
			if !named {
				t.Fatalf("%s is refused, but not as a runtime-name collision: %v", name, errs)
			}
			// scoped to units that declare a table: a table-free unit keeps
			// its whole namespace
			free := "package probe\n\nenum " + name + " { A, B }\n\ntype Holder\n{\n    g " + name + "\n}\n"
			if errs := checkErrors(t, free); len(errs) > 0 {
				t.Errorf("a TABLE-FREE unit must keep the name %s: %v", name, errs)
			}
		})
	}
}

// checkErrors runs the front end over one source and returns its diagnostics.
func checkErrors(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// capitalizeFirst is the namespace mapping the C# emitters use: package
// `probe` lands in namespace `Probe`.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// allVariableSrc declares nothing but pointered tables: every table in the
// unit derives VARIABLE-LENGTH, so none of them gets a text form (§16.1 —
// the variable class reads through its builder, which no backend emits yet,
// schema#275).
const allVariableSrc = packetSrc + `
table Chain
{
    value int32
    next  *Chain
}

table Holder
{
    head *Chain
}
`

// TestNoTextFormUnitEmitsNoCpp: the .cpp exists to carry definitions, so a
// unit with no definitions to carry does not get one. The alternative that
// looks equivalent is not: an EMPTY .cpp would still have to hold a walker
// to satisfy the generic-walk gate, and a glob-driven build would compile
// 1600 lines exporting nothing on every build.
func TestNoTextFormUnitEmitsNoCpp(t *testing.T) {
	c := New()
	files, err := c.Generate(unitFromSource(t, allVariableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name := range files {
		if strings.HasSuffix(name, "Table.cpp") {
			t.Errorf("an all-variable unit emitted %s — it has no text form to define", name)
		}
	}
	// the header still exists, and still says by name that the class has no
	// text form: the refusal is by absence and it is loud
	header := ""
	for name, body := range files {
		if strings.HasSuffix(name, "Table.h") {
			header = string(body)
		}
	}
	if header == "" {
		t.Fatal("no Table header generated for an all-variable unit")
	}
	for _, want := range []string{"ChainFromJson", "HolderFromJson"} {
		if strings.Contains(header, want+"(") {
			t.Errorf("header declares %s for a variable-length table", want)
		}
		if !strings.Contains(header, "no "+want) {
			t.Errorf("header does not say by name that %s is absent", want)
		}
	}

	// and a unit that DOES have a fixed table still gets its .cpp, so the
	// gate above is testing the guard rather than the emitter being off
	with, err := c.Generate(unitFromSource(t, tableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for name := range with {
		if strings.HasSuffix(name, "Table.cpp") {
			found = true
		}
	}
	if !found {
		t.Error("a unit with a fixed-size table emitted no Table.cpp")
	}
}

// TestEmittedBuildVersionIsTheSpecsNumber holds the two backends to §20's
// number rather than to a construction of their own: the page works an example
// and states its digest, a reader derived that digest independently, and the
// constant the emitters write must BE it.
//
// The protocol id is an INPUT to the cook projection (§20.2's first lines), so
// the example is generated under the id the page states — which is the only
// way the page's number and this build's can be the same number.
func TestEmittedBuildVersionIsTheSpecsNumber(t *testing.T) {
	const workedSource = `package demo

enum Grade { Bronze, Silver, Gold }

table ShipConfig
{
    damage float32 = 21.0
    speed  float32 = 500.0 | was = "velocity"
    armor  uint8 | min = 0, max = 100
    grade  Grade = Silver
}
`
	u := unitFromSource(t, workedSource)
	u.ProtocolId = 0x0123456789abcdef // the id §20.2 works the example under

	const want = "0xc211ce2f3414aa7c"
	if got := ir.BuildVersion(u); fmt.Sprintf("0x%016x", got) != want {
		t.Fatalf("the compiler's build version is 0x%016x, and docs/SPEC-TABLES.md §20.2 states %s", got, want)
	}

	c := New()
	for _, target := range []string{"cpp", "cs"} {
		files, err := c.Generate(u, target, Options{})
		if err != nil {
			t.Fatalf("generate %s: %v", target, err)
		}
		found := false
		for name, data := range files {
			if !strings.HasSuffix(name, "Block.h") && !strings.HasSuffix(name, "Block.cs") {
				continue
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				// the CONSTANT's own definition, not a descriptor that
				// references it
				if !strings.Contains(line, "BuildVersion = 0x") {
					continue
				}
				if !strings.Contains(strings.ToLower(line), "c211ce2f3414aa7c") {
					t.Errorf("%s emits %q — docs/SPEC-TABLES.md §20.2's number for this unit is %s", name, strings.TrimSpace(line), want)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("%s emits no BuildVersion constant at all (docs/SPEC-TABLES.md §20.7)", target)
		}
	}
}
