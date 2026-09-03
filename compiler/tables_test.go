// Tests for the tables generation surface (docs/SPEC-TABLES.md): the C, C++,
// C#, Go and Rust targets grow table sources, every other target refuses BY
// NAME, non-table output is byte-identical with or without tables, and the
// generated codecs allocate nothing.
package compiler

import (
	"fmt"
	"maps"
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
// dropping the tables. c, cpp, cs, go and rust all carry one
// (docs/SPEC-TABLES.md, backend status).
func TestTablelessTargetsRefuseTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, tableSrc)
	for _, target := range []string{"dart", "elixir", "js"} {
		if _, err := c.Generate(u, target, Options{}); err == nil {
			t.Errorf("--lang %s accepted a unit with tables — it must refuse by name", target)
		} else if !strings.Contains(err.Error(), "C, C++, C#, Go, Rust and Java only") || !strings.Contains(err.Error(), "Config") {
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
// WHAT THE SCAN COLLECTS, and it collects ALL THREE SPELLINGS the emitter
// uses, because the first version of this test collected only the C# one and
// was blind to two of them. In Rust a declaration produces a TYPE (PascalCase,
// exactly as declared) or — for a const or a flags variant — a SCREAMING_SNAKE
// value; it never produces a bare snake_case crate item, and the runtime's
// snake_case free functions are therefore spellings no declaration can reach.
// That makes them SCOPED, not invisible: they are registered as such, and this
// scan finds them, so a helper somebody adds has to be accounted for rather
// than slipping in under a regex that could not see it.
//
// The SCREAMING half is where the real defect was. Rust's constant spelling is
// MANY-TO-ONE — TableCookMagic, TABLE_COOK_MAGIC and table_cook_magic all
// lower to one crate-scope TABLE_COOK_MAGIC — so a registry entry is not a
// claim until internal/check makes it in the mapped space too. It does now,
// and TestRustRuntimeConstantsClaimInTheMappedSpace below is what holds it.
func TestRustTableRuntimeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "rust", Options{})
	if err != nil {
		t.Fatal(err)
	}
	pascal := regexp.MustCompile(`\bTable[A-Za-z0-9_]*\b`)
	// BUILD_VERSION is the one unit-level name the generated table sources
	// define that is not a Table* spelling, and the registry already says so.
	screaming := regexp.MustCompile(`\b(?:TABLE_[A-Z0-9_]+|BUILD_VERSION)\b`)
	// and the snake_case family, which the first version of this scan could
	// not see at all
	snake := regexp.MustCompile(`\btable_[a-z0-9_]+\b`)
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
			for _, m := range snake.FindAllString(line, -1) {
				emitted[snakeToPascal(m)] = true
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

// snakeToPascal maps a generated Rust free function back to the registry
// spelling: table_json_read -> TableJsonRead. It is the inverse of
// ir.RustSnake over the names this registry carries.
func snakeToPascal(name string) string { return screamingToPascal(name) }

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

// TestGoEmitsTableSources: the go target adds <Base>Table.go beside the packet
// sources for a unit with tables, and adds NOTHING for one without — the same
// contract the cpp and cs targets hold.
func TestGoEmitsTableSources(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "go", Options{})
	if err != nil {
		t.Fatalf("--lang go: %v", err)
	}
	if _, ok := with["ProbeTable.go"]; !ok {
		t.Fatalf("--lang go emitted no ProbeTable.go for a unit with tables; got %d files", len(with))
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "go", Options{})
	if err != nil {
		t.Fatalf("--lang go: %v", err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.go") || strings.HasSuffix(name, "Block.go") ||
			strings.HasSuffix(name, "Cook.go") || strings.HasSuffix(name, "TableJson.go") {
			t.Errorf("--lang go emitted %s for a table-free unit", name)
		}
	}
}

// TestGoRefusesPointeredTables: the Go variable-class refusal is a refusal of
// the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11), exactly as
// the C# one is. The two ACCELERATORS need no codec — a block and a cook are
// POINTED AT, not parsed — so both are emitted, the cook's <Root>Open opens
// this unit's cooked assets in full, and every source the unit does emit opens
// with a banner naming each refused table and the follow-on.
func TestGoRefusesPointeredTables(t *testing.T) {
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := New().Generate(u, "go", Options{})
	if err != nil {
		t.Fatalf("--lang go refused a pointered unit outright: %v", err)
	}
	for name := range files {
		if strings.HasSuffix(name, "Table.go") {
			t.Errorf("--lang go emitted %s for a pointered unit — the wire surface is refused by name", name)
		}
	}
	cook, ok := files["ProbeCook.go"]
	if !ok {
		t.Fatal("--lang go emitted no ProbeCook.go: the COOK needs no wire codec and must still be emitted")
	}
	if !strings.Contains(string(cook), "NodeOpen") {
		t.Error("ProbeCook.go carries no NodeOpen — a root is any table, and a pointered unit's cooks open in full")
	}
	for name, data := range files {
		if !strings.HasSuffix(name, "Block.go") && !strings.HasSuffix(name, "Cook.go") {
			continue
		}
		if !strings.Contains(string(data), "REFUSED, BY NAME") || !strings.Contains(string(data), "Node") {
			t.Errorf("%s does not carry the variable-class banner naming Node", name)
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

// TestTablesMoveNoGeneratedGoByte is the Go half of the same proof: adding a
// table changes not one byte of the non-table generated Go, and grows only the
// four Go table sources — the codecs, the two accelerators, and the unit's one
// text walk.
func TestTablesMoveNoGeneratedGoByte(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "go", Options{})
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
		if _, ok := without[name]; ok {
			continue
		}
		if !strings.HasSuffix(name, "Table.go") && !strings.HasSuffix(name, "Block.go") &&
			!strings.HasSuffix(name, "Cook.go") && !strings.HasSuffix(name, "TableJson.go") {
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

// goRuntimeSrc is runtimeSrc plus a table carrying a UNION field, which is the
// one construct the Go runtime's arms table needs to exist. It is a THIRD
// table rather than a union added to an existing one, because a union in a
// closure keeps that table out of both accelerators (§19.3) and the scan below
// has to see the block and cook halves too.
const goRuntimeSrc = runtimeSrc + `
type Boost
{
    power int32 = 0
}

union Effect
{
    boost Boost
}

table Effected
{
    effect Effect
}
`

// TestTableRuntimeNamesAreClaimedGo is the §11 promise's GO half, and the
// reason it is a second test rather than a parameter is that the two backends
// define overlapping but different sets: the scan asserts both directions
// against tablenames.Go, so a name the Go emitter defines and nobody registered
// fails here, and a name registered for Go that nothing emits fails here too.
//
// The scan is the C# one's, shape-independent for the same reason: it collects
// every table-prefixed identifier in the emitted text, declaration or use, and
// requires the whole set to be registered. Line comments are stripped first —
// prose is not an identifier.
//
// IT MATCHES BOTH CASES, and that is the whole reason this scan is not the C#
// one copied. Go is the first backend whose runtime puts UNEXPORTED names at
// package scope, and unexported is not private: a Go package is one namespace,
// so `const tableJsonMaxDepth = 5` in a schema is a redeclaration and the unit
// does not compile. A PascalCase-only scan is blind to exactly what this port
// adds, which is how the hole reached a reviewer.
func TestTableRuntimeNamesAreClaimedGo(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, goRuntimeSrc), "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// BuildVersion rides in the alternation because it is the one registered
	// name that is not a table spelling, and the Go backend defines it
	// (docs/SPEC-TABLES.md §20).
	ident := regexp.MustCompile(`\b(?:[Tt]able[A-Za-z0-9_]*|BuildVersion)\b`)
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range ident.FindAllString(line, -1) {
				emitted[m] = true
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted Go at all — the scan, not the registry, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Go table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Go that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Go) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Go backend defines %s, but nothing in the emitted "+
				"Go names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
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

// ---- the JAVA table backend (internal/codegen/javatable) ----

// javaFiles generates the java target's whole output for one source.
func javaFiles(t *testing.T, src string) map[string][]byte {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, src), "java", Options{})
	if err != nil {
		t.Fatalf("--lang java: %v", err)
	}
	return files
}

// TestJavaEmitsTableSources: the java target adds <Base>Table.java plus the
// unit's shared runtime for a unit with tables, and adds NOTHING for one
// without — the zero-cost property, at the grain Java has it. Java's unit scope
// is the PACKAGE and a public type lives in a file of its own name, so the
// runtime is one file per type rather than one home file, and "nothing" means
// not one of them.
func TestJavaEmitsTableSources(t *testing.T) {
	with := javaFiles(t, tableSrc)
	if _, ok := with["ProbeTable.java"]; !ok {
		t.Fatalf("--lang java emitted no ProbeTable.java for a unit with tables; got %d files", len(with))
	}
	for _, want := range []string{
		"TableReport.java", "TableWriter.java", "TableReader.java", "TableTypeInfo.java",
		"TableFieldInfo.java", "TableJson.java", "TableEnumId.java", "TableEnumValue.java",
		"TableBytes.java", "BuildVersion.java", "TableCookLayout.java",
	} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang java emitted no %s for a unit with tables", want)
		}
	}
	without := javaFiles(t, packetSrc)
	for name := range without {
		if strings.HasPrefix(name, "Table") || strings.HasSuffix(name, "Table.java") ||
			strings.HasSuffix(name, "Row.java") || strings.HasSuffix(name, "Block.java") ||
			strings.HasSuffix(name, "Cook.java") || name == "BuildVersion.java" {
			t.Errorf("--lang java emitted %s for a table-free unit — the form is zero-cost or it is not", name)
		}
	}
}

// TestJavaTablesMoveNoGeneratedPacketByte is the independence proof for this
// backend: beyond the protocol id, adding a table changes not one byte of the
// NON-TABLE generated Java.
func TestJavaTablesMoveNoGeneratedPacketByte(t *testing.T) {
	with := javaFiles(t, tableSrc)
	without := javaFiles(t, packetSrc)
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
}

// TestJavaRefusesPointeredTables: the Java variable-class refusal is a refusal of
// the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11), exactly as the
// C# one is. The two ACCELERATORS need no codec — a block and a cook are read
// where they lie — so both are emitted and the cook's <Root>Cook.open opens this
// unit's cooked assets in full.
//
// NAMED, NEVER SILENT is what this holds: no Table source at all, and every
// source the unit does emit opening with a banner that names each refused table
// and the follow-on.
func TestJavaRefusesPointeredTables(t *testing.T) {
	files := javaFiles(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	var cooks int
	for name, data := range files {
		if strings.HasSuffix(name, "Table.java") {
			t.Errorf("--lang java emitted the WIRE surface %s for a pointered unit", name)
		}
		if name == "TableJson.java" || name == "TableReader.java" || name == "TableWriter.java" {
			t.Errorf("--lang java emitted the wire runtime %s for a pointered unit", name)
		}
		if !strings.HasSuffix(name, "Cook.java") && !strings.HasSuffix(name, "Block.java") {
			continue
		}
		if strings.HasSuffix(name, "Cook.java") {
			cooks++
		}
		text := string(data)
		if !strings.Contains(text, "THE JAVA WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(text, "Node") || !strings.Contains(text, "is a named follow-on") {
			t.Errorf("%s does not name the table and the follow-on", name)
		}
	}
	if cooks == 0 {
		t.Error("--lang java emitted no cook reader for a pointered unit — a root is any table (docs/SPEC-TABLES.md §7)")
	}
	// and the cook's own surface is there: <Root>Cook with open and at on it
	cook := string(files["NodeCook.java"])
	if cook == "" {
		t.Fatal("--lang java emitted no NodeCook.java")
	}
	for _, want := range []string{
		"public static NodeCook open(byte[] data, int offset, long length)",
		"public int at(int slot, int size)",
		"public static TableCookInfo type()",
	} {
		if !strings.Contains(cook, want) {
			t.Errorf("NodeCook.java is missing %q", want)
		}
	}
}

// javaRuntimeIdent is the Java leg's scan: every Table*-prefixed identifier the
// emitted text carries, plus BuildVersion, which is the one unit-level name this
// backend defines that does not start with Table.
var javaRuntimeIdent = regexp.MustCompile(`\b(?:Table[A-Za-z0-9_]*|BuildVersion)\b`)

var javaBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// javaEmittedNames collects the scan's answer over one map of generated Java.
// Block comments are stripped as well as line comments: Java's generated
// runtime documents itself in javadoc, and prose is not an identifier.
func javaEmittedNames(files map[string][]byte, ignore map[string]bool) map[string]bool {
	emitted := map[string]bool{}
	for _, data := range files {
		text := javaBlockComment.ReplaceAllString(string(data), "")
		for line := range strings.SplitSeq(text, "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range javaRuntimeIdent.FindAllString(line, -1) {
				if !ignore[m] {
					emitted[m] = true
				}
			}
		}
	}
	return emitted
}

// TestJavaTableRuntimeNamesAreClaimed is the SELF-MAINTAINING half of the §11
// promise for this backend, and it is the C# test's shape with the two things
// Java changes:
//
//   - the scan strips BLOCK comments as well as line comments, because the
//     generated Java documents itself in javadoc and prose is not an identifier;
//   - it collects BuildVersion beside the Table* family, because Java puts that
//     constant's home at PACKAGE level (a class of its own file) where C# hangs
//     it off Schema — so it is a name this backend claims and the scan has to see.
//
// The ignore set is the SCHEMA's own names, not the runtime's: a file named
// Probe.schema generates the class Probe and, when it declares a table,
// ProbeTable — neither of which is a runtime spelling.
func TestJavaTableRuntimeNamesAreClaimed(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	ignore := map[string]bool{"Probe": true, "ProbeTable": true}
	emitted := javaEmittedNames(files, ignore)
	if len(emitted) == 0 {
		t.Fatal("the scan found no runtime identifier in the emitted Java at all — the scan, not the registry, is what broke")
	}

	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Java table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Java that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Java) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Java backend defines %s, but nothing in the emitted "+
				"Java names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}
}

// stripCComments removes /* ... */ comments, which is the whole of C's comment
// grammar in generated output. The C# scan strips `//` for the same reason: the
// runtime's own prose is not an identifier, and scanning it would make the gate
// a spelling police for the documentation.
func stripCComments(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if i+1 < len(text) && text[i] == '/' && text[i+1] == '*' {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				break
			}
			// keep the newlines so line-oriented reading of the rest survives
			for _, r := range text[i : i+2+end+2] {
				if r == '\n' {
					out.WriteByte('\n')
				}
			}
			i += 2 + end + 2
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// TestCTableRuntimeNamesAreClaimed is the C leg's half of the §11 promise, and
// it is the C# scan's twin over the C emitter's output.
//
// C IS THE HARD CASE AND THAT IS WHY THIS EXISTS. C++ has a namespace and C# a
// nested class, so each can put a runtime spelling somewhere a schema cannot
// reach; C has neither, and every name the generated header or source declares
// sits in the one namespace a declaration lands in. The registry is therefore
// longest for this backend, and the scan is what keeps it honest in both
// directions:
//
//   - EVERY Table*, kTable* and table_* identifier in the emitted C is
//     registered. A runtime name somebody added to the emitter and forgot to
//     register fails here.
//   - EVERY name the registry says the C backend defines appears in the emitted
//     C. A claim nothing needs takes a name away from every schema for free.
//
// The SCHEMA_ prefix is excluded and is not an exception: it is the packet
// emitter's own reserved marker (SCHEMA_UNUSED, schema_utf8_valid_), spelled
// the way the generated externals are, and no schema identifier can be it.
func TestCTableRuntimeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, cRuntimeSrc), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ident := regexp.MustCompile(`\b(?:Table|kTable|table_|BuildVersion)[A-Za-z0-9_]*\b`)
	// the unit's own type names start with Table for a schema that declares one;
	// the corpus here declares none, and the file base does, so the two file
	// spellings the include lines carry are excluded by name rather than by a
	// pattern that could hide a real hit.
	emitted := map[string]bool{}
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.h") && !strings.HasSuffix(name, "Table.c") &&
			!strings.HasSuffix(name, "Block.h") && !strings.HasSuffix(name, "Block.c") {
			continue
		}
		for _, m := range ident.FindAllString(stripCComments(string(data)), -1) {
			emitted[m] = true
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted C at all — the scan, not the registry, is what broke")
	}

	emittedNames := make([]string, 0, len(emitted))
	for name := range emitted {
		emittedNames = append(emittedNames, name)
	}
	sort.Strings(emittedNames)

	for _, name := range emittedNames {
		if !tablenames.Registered(name) {
			t.Errorf("the C table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate C that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames, or spell it schema_<package>_..._ "+
				"so it claims nothing", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.C) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the C backend defines %s, but nothing in the emitted "+
				"C names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}
}

// TestJavaRuntimeNameScanGoesRed is the scan's own NEGATIVE CONTROL, and it is
// the control the C# test's comment asks for without running: a scan that has
// gone blind passes every registry it is pointed at, so the only way to know it
// still sees is to hand it a name nobody registered and require it to say so.
//
// The probe is injected into a COPY of the emitted text, in a shape the emitter
// does not use — a bare top-level class declaration — which is exactly the case
// a shape-dependent scan would miss.
func TestJavaRuntimeNameScanGoesRed(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	sabotaged := map[string][]byte{}
	maps.Copy(sabotaged, files)
	sabotaged["TableProbe.java"] = []byte("package probe;\n\npublic final class TableProbe {}\n")
	emitted := javaEmittedNames(sabotaged, map[string]bool{"Probe": true, "ProbeTable": true})
	if !emitted["TableProbe"] {
		t.Fatal("the scan did not see a package-level TableProbe — it is blind, and every green run above proves nothing")
	}
	if tablenames.Registered("TableProbe") {
		t.Fatal("TableProbe is registered, so the control proves nothing — pick a name the registry does not hold")
	}
}

// TestJavaRuntimeNamesAreRefusedByTheChecker is the REPRO the claim exists for:
// a schema that declares one of the Java runtime's package-level names, in a
// unit that declares a table, must be refused by the front end — because the
// generated Java would otherwise carry two public types of that name and not
// compile. TestTableRuntimeNamesAreClaimed already walks every claimed name;
// this pins the two the Java port ADDED to the claim, so a later edit that
// narrowed either would fail here by name rather than silently.
func TestJavaRuntimeNamesAreRefusedByTheChecker(t *testing.T) {
	for _, name := range []string{"TableBytes", "TableJson", "TableBlockLayout"} {
		if !tablenames.Registered(name) {
			t.Fatalf("%s is not registered at all", name)
		}
		claimed := false
		for _, c := range tablenames.Claimed() {
			if c == name {
				claimed = true
			}
		}
		if !claimed {
			t.Errorf("%s is registered but not CLAIMED — the Java backend puts it at package level, so a "+
				"schema declaring it generates two public types of one name", name)
			continue
		}
		src := "package probe\n\nenum " + name + " { A, B }\n\ntable Holder\n{\n    g " + name + "\n}\n"
		errs := checkErrors(t, src)
		if len(errs) == 0 {
			t.Errorf("a declaration named %s was accepted in a unit with a table", name)
		}
		// and the NEGATIVE CONTROL of the claim: a table-free unit keeps the name
		free := "package probe\n\nenum " + name + " { A, B }\n\ntype Holder\n{\n    g " + name + "\n}\n"
		if errs := checkErrors(t, free); len(errs) > 0 {
			t.Errorf("a TABLE-FREE unit must keep the name %s: %v", name, errs)
		}
	}
}

// TestJavaRefusesAFileNamedForARuntimeType is the collision Java's
// one-public-class-per-file rule creates and no other backend has: the CHECKER
// claims declaration names, and a schema FILE's basename is not a declaration —
// it is what names the packet emitter's class. A unit with a table and a file
// called TableReport.schema would have two TableReport.java to write, so the
// backend refuses by name rather than letting one clobber the other.
func TestJavaRefusesAFileNamedForARuntimeType(t *testing.T) {
	f, perrs := parser.Parse("TableReport.schema", []byte(tableSrc))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "TableReport.schema", Name: "TableReport.schema", Base: "TableReport",
		Bytes: []byte(tableSrc), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	_, err := New().Generate(u, "java", Options{})
	if err == nil {
		t.Fatal("--lang java accepted a unit whose file basename is a runtime type's — one of the two would clobber the other")
	}
	if !strings.Contains(err.Error(), "TableReport.java") {
		t.Errorf("the refusal does not name the file it collides with: %v", err)
	}
	// the CONTROL: the same source under any other basename generates
	g, perrs := parser.Parse("Probe.schema", []byte(tableSrc))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	ok, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(tableSrc), AST: g,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	if _, err := New().Generate(ok, "java", Options{}); err != nil {
		t.Errorf("the same unit under an ordinary basename must generate: %v", err)
	}
}

// TestJavaDescriptorsAreSafelyPublished is the test the unsafe-publication
// defect asks for, and it is a STRUCTURAL one on purpose.
//
// The defect it guards is a data race the Java memory model PERMITS rather than
// requires: a descriptor cached by a plain write can be read non-null with its
// field array still null, on a machine whose store order allows it. A test that
// tried to OBSERVE that would be a race detector — nondeterministic, green on
// x86 almost always, and worthless as a gate. What is deterministic is the
// SHAPE the emitter writes, and the shape is what was wrong.
//
// So this asserts the shape: every generated descriptor accessor is a read of a
// holder's final field, and none of them is the `if (cached != null)` idiom the
// defect had. A port that reintroduces the plain cache fails here, in every
// build, on every host.
func TestJavaDescriptorsAreSafelyPublished(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	// the four sites: the wire descriptor, the block projection, and a record's
	// block and cook descriptors
	accessors := regexp.MustCompile(`public static Table(?:Type|Block|Cook)Info ([A-Za-z0-9_]+)\(\) \{([^}]*)\}`)
	holders := 0
	for name, data := range files {
		text := string(data)
		for _, m := range accessors.FindAllStringSubmatch(text, -1) {
			holders++
			body := strings.TrimSpace(m[2])
			// a holder read, or a one-line delegation to another accessor that is
			// itself holder-backed — <Table>Cook.type() hands back its root
			// record's descriptor rather than keeping a second one
			delegates := strings.Contains(body, "Row.cookInfo()") || strings.Contains(body, "Row.blockInfo()")
			if !strings.Contains(body, "Holder.INFO") && !delegates {
				t.Errorf("%s: %s() neither reads a holder's final field nor delegates to one — its "+
					"body is %q; a plain cache publishes a mutable descriptor unsafely (JLS §17.4)",
					name, m[1], body)
			}
		}
		// and the idiom itself must be gone, wherever it appears
		for line := range strings.SplitSeq(text, "\n") {
			if strings.Contains(line, "if (info != null) { return info; }") {
				t.Errorf("%s carries the plain-cache idiom: %q", name, strings.TrimSpace(line))
			}
		}
	}
	if holders == 0 {
		t.Fatal("the scan found no descriptor accessor at all — the scan, not the emitter, is what broke")
	}
	// every holder is a private static final class whose one field is final
	for name, data := range files {
		text := string(data)
		for line := range strings.SplitSeq(text, "\n") {
			if strings.Contains(line, "Holder {") && !strings.Contains(line, "private static final class") {
				t.Errorf("%s: a descriptor holder is not a private static final class: %q", name, strings.TrimSpace(line))
			}
			if strings.Contains(line, "INFO =") && !strings.Contains(line, "static final") {
				t.Errorf("%s: a holder's INFO is not final: %q", name, strings.TrimSpace(line))
			}
		}
	}
}

// TestCEmitsTableSources: the c target adds <Base>Table.h and <Base>Table.c
// beside the packet sources for a unit with tables, and adds NOTHING for one
// without — the same contract the cpp and cs targets hold.
func TestCEmitsTableSources(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "c", Options{})
	if err != nil {
		t.Fatalf("--lang c: %v", err)
	}
	for _, want := range []string{"ProbeTable.h", "ProbeTable.c"} {
		if _, ok := with[want]; !ok {
			t.Fatalf("--lang c emitted no %s for a unit with tables; got %d files", want, len(with))
		}
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "c", Options{})
	if err != nil {
		t.Fatalf("--lang c: %v", err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.h") || strings.HasSuffix(name, "Table.c") ||
			strings.HasSuffix(name, "Block.h") || strings.HasSuffix(name, "Block.c") {
			t.Errorf("--lang c emitted %s for a table-free unit", name)
		}
	}
	// and the PACKET half is byte-identical with the tables there and gone:
	// the table surface is emitted beside it, never woven into it
	packetOnly, err := c.Generate(unitFromSource(t, packetSrc), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range packetOnly {
		if !strings.HasSuffix(name, ".h") {
			continue
		}
		if other, ok := without[name]; ok && string(other) != string(body) {
			t.Errorf("%s is not stable across two generations of the same unit", name)
		}
	}
}

// TestJavaGeneratedMethodsAreLowerCamel: Java has one naming rule and the
// generated table surface follows it, as this backend's own packet half already
// does (writeVec3, readVec3). §6.1's NAME-FIRST order is untouched — the method
// is the declaration's name and then the verb — so only the case is the port's,
// and it is the language's rather than C++'s.
func TestJavaGeneratedMethodsAreLowerCamel(t *testing.T) {
	files := javaFiles(t, runtimeSrc)
	decl := regexp.MustCompile(`^\s*public static [A-Za-z0-9_.\[\]<>]+ ([A-Za-z0-9_]+)\(`)
	seen := 0
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.java") {
			continue // the runtime types are types, and a TYPE is UpperCamel in Java
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			m := decl.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			seen++
			if m[1][0] >= 'A' && m[1][0] <= 'Z' {
				t.Errorf("%s: generated method %s is UpperCamelCase — Java's rule, and this "+
					"backend's packet half, spell a method lowerCamel", name, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("the scan found no generated method at all — the scan, not the emitter, is what broke")
	}
}

// TestCExternalsCarryThePackage: two units whose type names collide must LINK
// together, which is what a C consumer of two schema units does and what the
// conformance driver itself does. C has no namespace, so the property is held
// by the spelling: every external the table backend emits carries the package.
func TestCExternalsCarryThePackage(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, tableSrc), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	source, ok := files["ProbeTable.c"]
	if !ok {
		t.Fatal("no ProbeTable.c")
	}
	// every definition at file scope in the .c is either static or carries the
	// package prefix; a bare `const TableTypeInfo ConfigTableInfo` would be the
	// defect this test names
	for line := range strings.SplitSeq(stripCComments(string(source)), "\n") {
		if !strings.HasPrefix(line, "const ") && !strings.HasPrefix(line, "int ") &&
			!strings.HasPrefix(line, "int64_t ") {
			continue
		}
		if !strings.Contains(line, "schema_probe_") {
			t.Errorf("a definition with external linkage does not carry the package, so two units "+
				"could not link together: %s", strings.TrimSpace(line))
		}
	}
}

// cRuntimeSrc is runtimeSrc plus a POINTERED table, because the C scan above
// has to meet the whole runtime and C emits the variable-length half only into
// a unit that has one. The claim does not vary with that — a name free today
// must not become a collision the day a table gains a pointer (§11) — so the
// scan needs a corpus where every name is actually emitted.
const cRuntimeSrc = runtimeSrc + `
table Node
{
    value int32
    next  *Node
}
`

// TestCForceInlineStopsAtTheVariableClass is the C twin of the reference's
// recursion guard (schema#343). The force-inline qualifier carries the fixed
// class's bodies and stops there, because that is the class whose save/load
// call graph cannot hold a cycle: a fixed table nests by value, and a by-value
// cycle is an infinite `sizeof`. A pointered body reaches its pointee through
// the depth-carrying form, which a self-referential declaration makes directly
// recursive — and a recursive always_inline is a compile error under gcc.
//
// It also holds the line the reference draws around MEASURE: neither backend
// force-inlines it, because it is called once per nested body to decide elision
// and its result is a number, so it neither holds the cursor nor merges stores.
func TestCForceInlineStopsAtTheVariableClass(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, cRuntimeSrc), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	header := string(files["ProbeTable.h"])
	if header == "" {
		t.Fatal("no ProbeTable.h")
	}
	for _, want := range []string{
		"static SCHEMA_UNUSED SCHEMA_PROBE_TABLE_INLINE int config_save_body( TableWriter * w, const Config * value )",
		"static SCHEMA_UNUSED SCHEMA_PROBE_TABLE_INLINE int config_load_body( TableReader * r, Config * value )",
		"static SCHEMA_UNUSED SCHEMA_PROBE_TABLE_INLINE void table_writer_put32( TableWriter * w, uint32_t v )",
		"static SCHEMA_UNUSED SCHEMA_PROBE_TABLE_INLINE uint32_t table_reader_get32( TableReader * r )",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("the fixed class did not carry the force-inline qualifier: %q", want)
		}
	}
	// the VARIABLE class's bodies must not carry it: their pointer walk can
	// recurse, and a recursive always_inline does not compile under gcc
	for _, forbidden := range []string{
		"SCHEMA_PROBE_TABLE_INLINE int node_save_body",
		"SCHEMA_PROBE_TABLE_INLINE int node_load_body",
		"SCHEMA_PROBE_TABLE_INLINE int64_t node_pack_measure",
		"SCHEMA_PROBE_TABLE_INLINE int node_pack",
	} {
		if strings.Contains(header, forbidden) {
			t.Errorf("a variable-length table's body was force-inlined; its pointer walk can recurse: %q", forbidden)
		}
	}
	// MEASURE stays plain in both classes, as it does in the reference
	if strings.Contains(header, "SCHEMA_PROBE_TABLE_INLINE int64_t config_measure") {
		t.Error("Measure was force-inlined; it holds no cursor and merges no stores")
	}
	// and the macro is PACKAGE-SCOPED, so several units' headers can meet in
	// one translation unit without a redefinition
	if !strings.Contains(header, "#ifndef SCHEMA_PROBE_TABLE_INLINE") {
		t.Error("the force-inline macro is not package-scoped behind its own guard")
	}
}

// cMacroSrc names every declaration with the same distinctive prefix, so a
// macro in the emitted C that does NOT carry it is one the GENERATOR owns
// rather than one the schema asked for. That is what makes the scan below
// shape-independent: it recognises no spelling and no family, it simply
// subtracts the schema's own contribution and looks at what is left.
const cMacroSrc = `package zqqpkg

const ZqqMax = 4

enum ZqqKind { Alpha, Beta }

flags ZqqPerks { Fast, Slow }

type ZqqPoint
{
    x float32
    y float32
}

table ZqqConfig
{
    scale  float32 = 1.0
    label  string(24)
    grade  ZqqKind
    perks  ZqqPerks
    slots  [ZqqKind]int32
    extra  ?ZqqPoint
    points [..ZqqMax]ZqqPoint
}

table ZqqNode
{
    value int32
    next  *ZqqNode
}
`

// TestCGeneratorMacrosAreOwned closes the class C has that neither other
// backend does: the PREPROCESSOR namespace.
//
// A schema's constants, enum variants and flag masks are `#define`s in the C
// target, and the generated sources define macros of their own beside them. A
// collision there is not a redeclaration error — it is a SILENT REWRITE, since
// the generator's `#ifndef` sees the user's definition standing and skips its
// own. So every macro the generator defines must be one the front end refuses a
// declaration for, and this is what says so in both directions:
//
//   - every macro in the emitted C that the SCHEMA did not ask for carries the
//     reserved SCHEMA_ prefix, or is a registered runtime name;
//   - and a declaration spelling one is refused, with the diagnostic naming the
//     silent rewrite (the repro cases live in internal/check's break-the-
//     language suite).
func TestCGeneratorMacrosAreOwned(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, cMacroSrc), "c", Options{})
	if err != nil {
		t.Fatal(err)
	}
	define := regexp.MustCompile(`^#define ([A-Za-z_][A-Za-z0-9_]*)`)
	owned := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			m := define.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			// anything carrying the schema's own prefix is a macro the
			// DECLARATIONS asked for, in either spelling the emitter uses
			if strings.Contains(m[1], "ZQQ") || strings.Contains(m[1], "Zqq") {
				continue
			}
			owned[m[1]] = true
		}
	}
	if len(owned) == 0 {
		t.Fatal("the scan found no generator-owned macro at all — the scan, not the emitter, is what broke")
	}
	for name := range owned {
		switch {
		case strings.HasPrefix(name, "SCHEMA_"):
			// reserved, and internal/check refuses a declaration spelling it
		case tablenames.Registered(name):
			// a registered runtime name, refused by the §11 claim
		case name == "ZQQPKG_PROTOCOL_ID":
			// the packet emitter's per-unit id; its claim is the packet
			// emitter's own business and predates the table backend
		default:
			t.Errorf("the generated C defines the macro %s, which is neither under the reserved "+
				"SCHEMA_ prefix nor a registered runtime name — a schema declaring it would silently "+
				"rewrite the generator's own definition; move it under SCHEMA_ or register it", name)
		}
	}
}
