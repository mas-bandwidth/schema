// Tests for the tables generation surface (docs/SPEC-TABLES.md): the C, C++,
// C#, Go, JavaScript and Rust targets grow table sources, every other target
// refuses BY NAME, non-table output is byte-identical with or without tables,
// and the
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
		"static_assert( __is_trivially_copyable( Config )",
		"static_assert( __is_standard_layout( Config )",
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

// tableDialectViolations lists the spellings in a generated C++ table or
// block header that are not the C-like dialect serialize.h sets
// (docs/SPEC-TABLES.md §13.9): the <c*> header spellings, the STL headers,
// a std:: name, an inline constexpr, and a call into the C library that does
// not go through a hook. The hook blocks — `#ifndef schema_...` through their
// `#endif` — are the one place a raw C-library call belongs, because they ARE
// the default, so they are cut out before the scan, and so are comments. A
// pointered unit's <atomic> and <new> are named follow-ons and not on the
// list, and std::atomic and std::memory_order are the names <atomic> brings.
func tableDialectViolations(header string) []string {
	var code strings.Builder
	inHook := false
	for line := range strings.SplitSeq(header, "\n") {
		if strings.HasPrefix(line, "#ifndef schema_") {
			inHook = true
		}
		if !inHook {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			code.WriteString(line)
			code.WriteString("\n")
		}
		if inHook && strings.HasPrefix(line, "#endif") {
			inHook = false
		}
	}
	text := strings.NewReplacer("std::atomic", "ATOMIC", "std::memory_order", "MEMORY_ORDER").Replace(code.String())
	var found []string
	for _, banned := range []string{
		"<cstdint>", "<cstring>", "<cstddef>", "<cstdlib>", "<cassert>", "<cstdio>",
		"<type_traits>", "<iterator>", "<algorithm>", "std::", "inline constexpr",
	} {
		if strings.Contains(text, banned) {
			found = append(found, banned)
		}
	}
	// a raw call: the name as a whole token followed by its open parenthesis.
	// `allocator.free(` and the hook's own `table_default_free(` are not the
	// token; ` free(` is.
	for _, call := range []string{"abort", "assert", "malloc", "calloc", "realloc", "free"} {
		for line := range strings.SplitSeq(text, "\n") {
			at := 0
			for {
				i := strings.Index(line[at:], call+"(")
				if i < 0 {
					break
				}
				i += at
				before := byte(' ')
				if i > 0 {
					before = line[i-1]
				}
				identifier := before == '_' || before == '.' || before == ':' || before == '>' ||
					(before >= 'a' && before <= 'z') || (before >= 'A' && before <= 'Z') || (before >= '0' && before <= '9')
				if !identifier {
					found = append(found, call+"(")
					break
				}
				at = i + len(call)
			}
		}
	}
	return found
}

// TestTableHeadersAreTheCDialect: every generated C++ table and block header
// is the C-like dialect (docs/SPEC-TABLES.md §13.9), in each class that
// changes what the header carries — a fixed unit, a keyed one whose accessor
// refuses through the assert and fatal hooks, a pointered one whose arena
// allocates through the allocator hook, and the block header whose default
// pair lands in the same hook. The cook's runtime and its WRITE side (§7.6)
// ride in the Table header, so the scan covers them by covering it — and the
// probe below is what says so, rather than the scan passing over a header
// that happened not to carry them.
func TestTableHeadersAreTheCDialect(t *testing.T) {
	for name, src := range map[string]string{"fixed": tableSrc, "keyed": runtimeSrc, "pointered": pointerSrc} {
		cook := []string{"TableCookOpen(", "ConfigCookMeasure(", "ConfigCookBody(", "ConfigCook("}
		if name == "pointered" {
			cook = []string{"TableCookOpen(", "PlainCookBody("} // the fixed table in a pointered unit; a pointered root's Cook is a follow-on (§15)
		}
		files, err := New().Generate(unitFromSource(t, src), "cpp", Options{})
		if err != nil {
			t.Fatalf("%s: generate: %v", name, err)
		}
		scanned := 0
		for file, body := range files {
			if !strings.HasSuffix(file, "Table.h") && !strings.HasSuffix(file, "Block.h") {
				continue
			}
			scanned++
			if strings.HasSuffix(file, "Table.h") {
				for _, want := range cook {
					if !strings.Contains(string(body), want) {
						t.Errorf("%s: %s does not carry the cook surface %q, so the dialect scan is not over it", name, file, want)
					}
				}
			}
			for _, v := range tableDialectViolations(string(body)) {
				t.Errorf("%s: %s contains %q — the table half is C-like C++ (schema#382)", name, file, v)
			}
		}
		if scanned == 0 {
			t.Fatalf("%s: no table or block header generated", name)
		}
	}
}

// TestTableDialectCheckBites is the NEGATIVE CONTROL for the scan above: a
// clean header reports nothing, and a header with one raw spelling planted —
// an include, an abort, an allocation, a free, a std:: name — reports exactly
// that spelling. A scan that never went red would prove nothing about what
// the corpus contains.
func TestTableDialectCheckBites(t *testing.T) {
	clean := tableHeader(t, runtimeSrc)
	if v := tableDialectViolations(clean); len(v) != 0 {
		t.Fatalf("the clean header reports %v", v)
	}
	for _, plant := range []struct{ text, want string }{
		{"#include <cstdlib>\n", "<cstdlib>"},
		{"#include <iterator>\n", "<iterator>"},
		{"#include <type_traits>\n", "<type_traits>"},
		{"static_assert( std::is_standard_layout<int>::value, \"\" );\n", "std::"},
		{"inline constexpr int planted = 1;\n", "inline constexpr"},
		{"inline void planted() { abort(); }\n", "abort("},
		{"inline void planted() { assert( false ); }\n", "assert("},
		{"inline void * planted() { return malloc( 8 ); }\n", "malloc("},
		{"inline void * planted() { return calloc( 1, 8 ); }\n", "calloc("},
		{"inline void * planted( void * p ) { return realloc( p, 8 ); }\n", "realloc("},
		{"inline void planted( void * p ) { free( p ); }\n", "free("},
	} {
		got := tableDialectViolations(clean + plant.text)
		if len(got) != 1 || got[0] != plant.want {
			t.Errorf("planted %q: the scan reports %v, want [%s]", strings.TrimSpace(plant.text), got, plant.want)
		}
	}
	// and the hook block's OWN raw calls are the default, not a violation
	hook := "#ifndef schema_allocate\n#include <stdlib.h>\n#define schema_allocate( bytes ) calloc( (size_t) 1, (size_t) ( bytes ) )\n#define schema_release( pointer ) free( pointer )\n#endif // #ifndef schema_allocate\n"
	if v := tableDialectViolations(clean + hook); len(v) != 0 {
		t.Errorf("the hook block's default pair reports %v", v)
	}
}

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
// field, a generic method, a non-sealed class. A scan that has to recognize
// declaration syntax is a scan that goes quietly blind the day the syntax
// changes. This one recognizes none: it collects every Table*-prefixed
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
// unit derives VARIABLE-LENGTH, so every text form in it is the builder's
// (§16.7) and the unit's .cpp carries the graph half beside the walk.
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

// TestVariableUnitCarriesTheBuilderTextForm: an all-variable unit gets its
// .cpp like any other, and what it carries is the class's own text form
// (§16.7) — the builder-form FromJson and the region-form ToJson declared in
// the header, the graph half beside the walk in the .cpp. A pointer-free unit
// carries the walk and the stubs, and none of the graph half: the zero-cost
// property (§2.2), holding for the text form.
func TestVariableUnitCarriesTheBuilderTextForm(t *testing.T) {
	c := New()
	files, err := c.Generate(unitFromSource(t, allVariableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	header, cpp := "", ""
	for name, body := range files {
		switch {
		case strings.HasSuffix(name, "Table.h"):
			header = string(body)
		case strings.HasSuffix(name, "Table.cpp"):
			cpp = string(body)
		}
	}
	if header == "" || cpp == "" {
		t.Fatal("an all-variable unit did not emit both Table.h and Table.cpp")
	}
	for _, table := range []string{"Chain", "Holder"} {
		if !strings.Contains(header, "bool "+table+"FromJson( "+table+"Builder & builder") {
			t.Errorf("header does not declare the builder-form %sFromJson", table)
		}
		if !strings.Contains(header, "int64_t "+table+"ToJson( const "+table+" * root") {
			t.Errorf("header does not declare the region-form %sToJson", table)
		}
	}
	if !strings.Contains(cpp, "---- json graph walk: begin ----") || !strings.Contains(cpp, "TableJsonReadGraph") {
		t.Error("the .cpp does not carry the graph half")
	}

	// and a pointer-free unit carries the walk, the three stubs, and none of
	// the graph half
	with, err := c.Generate(unitFromSource(t, tableSrc), "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	fixed := ""
	for name, body := range with {
		if strings.HasSuffix(name, "Table.cpp") {
			fixed = string(body)
		}
	}
	if fixed == "" {
		t.Fatal("a unit with a fixed-size table emitted no Table.cpp")
	}
	if !strings.Contains(fixed, "---- json walk: begin ----") || !strings.Contains(fixed, "this unit declares no pointer") {
		t.Error("a pointer-free unit's .cpp does not carry the walk and the stubs")
	}
	if strings.Contains(fixed, "json graph walk") || strings.Contains(fixed, "TableJsonReadGraph") {
		t.Error("the graph half leaked into a pointer-free unit")
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
// shape-independent: it recognizes no spelling and no family, it simply
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

// TestElixirEmitsTableModules: the elixir target adds the table modules beside
// the packet ones for a unit with tables, and adds NOTHING for a unit without —
// the same contract the cpp, cs and rust targets hold.
func TestElixirEmitsTableModules(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir: %v", err)
	}
	for _, want := range []string{"ProbeTable.ex", "TableRuntime.ex", "ProbeBlock.ex", "ProbeCook.ex", "BuildVersion.ex"} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang elixir emitted no %s for a unit with tables; got %d files", want, len(with))
		}
	}

	without, err := c.Generate(unitFromSource(t, packetSrc), "elixir", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.ex") || strings.HasSuffix(name, "Block.ex") ||
			strings.HasSuffix(name, "Cook.ex") || name == "BuildVersion.ex" {
			t.Errorf("--lang elixir emitted %s for a table-free unit", name)
		}
	}
	// ZERO COST, and it is a gate rather than a hope (§2.2): adding a table
	// moves not one byte of the PACKET modules.
	for name, data := range without {
		got, ok := with[name]
		if !ok {
			t.Errorf("elixir module %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("elixir module %s changed when a table was added — tables must move no packet byte", name)
		}
	}
}

// TestElixirRefusesPointeredTables: the Elixir variable-class refusal is a
// refusal of the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11),
// exactly as the C# and Rust ones are. The wire codec is the half the variable
// class is missing — the arena, the builder, the region, the node table — and
// the two ACCELERATORS need none of it: a block and a cook are POINTED AT, not
// parsed.
func TestElixirRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir refused a pointered unit outright — the accelerators need no codec: %v", err)
	}
	cooks := 0
	for name, data := range files {
		if strings.HasSuffix(name, "Table.ex") || name == "TableRuntime.ex" {
			t.Errorf("--lang elixir emitted the WIRE surface %s for a pointered unit", name)
		}
		if strings.HasSuffix(name, "Cook.ex") && !strings.HasSuffix(name, "CookRuntime.ex") {
			cooks++
		}
		if strings.HasSuffix(name, "Cook.ex") || strings.HasSuffix(name, "Block.ex") ||
			name == "BuildVersion.ex" {
			// and the refusal RIDES on every file the unit does emit, so a
			// consumer meets it wherever they look
			if !strings.Contains(string(data), "REFUSED, BY NAME") {
				t.Errorf("%s carries no refusal banner for a pointered unit", name)
			}
		}
	}
	if cooks == 0 {
		t.Error("--lang elixir emitted no cook module for a pointered unit — a cook's root is one")
	}
}

// TestElixirRuntimeNamesAreClaimed is the Elixir half of the §11 promise, and
// its SCAN IS THE LANGUAGE'S OWN COLLISION CLASS rather than C#'s.
//
// An Elixir declaration lowers to a MODULE under the unit's namespace —
// `<Package>.<Name>` — so the names a schema can collide with are exactly the
// unit-level module segments the emitter defines, whatever they are spelled.
// A Table* prefix would have been blind to BlockRuntime, CookRuntime and
// BuildVersion, which are three of the four the Elixir backend actually
// defines; scanning for the segment finds any module the emitter grows,
// including one nobody thought to prefix.
//
// The segments a DECLARATION or a schema FILE produces are excluded, because
// those are the schema author's own names and their collisions are refused
// elsewhere (a declaration colliding with a file's module, and a generated
// filename claimed twice).
func TestElixirRuntimeNamesAreClaimed(t *testing.T) {
	u := unitFromSource(t, runtimeSrc)
	files, err := New().Generate(u, "elixir", Options{})
	if err != nil {
		t.Fatal(err)
	}
	namespace := ir.GoExportName(u.Package)

	// the author's own segments: every declaration, every union's generated
	// tag module, and every module a schema FILE produces
	mine := map[string]bool{}
	for name := range u.DeclFile {
		mine[name] = true
	}
	for _, un := range u.Unions {
		mine[un.Name+"Type"] = true
	}
	for name := range u.Tables {
		mine[name] = true
	}
	for _, f := range u.Files {
		for _, suffix := range []string{"", "Table", "Block", "Cook"} {
			mine[ir.GoExportName(f.Base)+suffix] = true
		}
	}

	segment := regexp.MustCompile(`\b` + namespace + `\.([A-Z][A-Za-z0-9_]*)`)
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			// a `#` comment is prose, not an identifier; scanning it would
			// make this gate a spelling police for the runtime's own
			// documentation
			if i := strings.Index(line, "#"); i >= 0 {
				line = line[:i]
			}
			for _, m := range segment.FindAllStringSubmatch(line, -1) {
				if !mine[m[1]] {
					emitted[m[1]] = true
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no unit-level module segment in the emitted Elixir at all — the scan, not the registry, is what broke")
	}

	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Elixir table emitter defines the unit-level module %s.%s and "+
				"internal/tablenames does not register it — a schema declaring that name would "+
				"generate Elixir that does not compile; register it (with the backends that "+
				"define it) in internal/tablenames", namespace, name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Elixir) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Elixir backend defines %s, but nothing in the "+
				"emitted Elixir names it — drop the registration or fix the backend; a claim "+
				"nothing needs takes a name away from every schema for free", name)
		}
	}
}

// TestElixirRuntimeNameCollisionRepro is the REPRO the scan above exists for,
// and its NEGATIVE CONTROL is the second half: a declaration named for a
// generated module is refused by the checker, and the same declaration in a
// TABLE-FREE unit is accepted — which is what says the claim is scoped to
// units that declare a table rather than taken from every schema.
func TestElixirRuntimeNameCollisionRepro(t *testing.T) {
	for _, name := range []string{"TableRuntime", "BlockRuntime", "CookRuntime", "BuildVersion"} {
		t.Run(name, func(t *testing.T) {
			refused := "package probe\n\nenum " + name + " { A, B }\n\ntable Holder\n{\n    g " + name + "\n}\n"
			errs := checkErrors(t, refused)
			if len(errs) == 0 {
				t.Fatalf("a declaration named %s was accepted — the Elixir table backend defines "+
					"the module probe.%s, so the unit cannot compile", name, name)
			}
			// the NEGATIVE CONTROL: the same name in a table-free unit is the
			// author's, and taking it there would be a claim nothing needs
			free := "package probe\n\nenum " + name + " { A, B }\n\ntype Holder\n{\n    g " + name + "\n}\n"
			if errs := checkErrors(t, free); len(errs) > 0 {
				t.Errorf("a TABLE-FREE unit must keep the name %s: %v", name, errs)
			}
		})
	}
}

// TestElixirRefusesFileModuleCollision: a declaration lowers to a MODULE under
// the unit's namespace, so one named for a generated file's module would merge
// two unrelated modules. The unit-level runtime names are the checker's claim;
// these three are derived from a schema FILE's own basename, which no
// unit-level registry can hold, so the backend refuses them by name.
func TestElixirRefusesFileModuleCollision(t *testing.T) {
	c := New()
	for _, suffix := range []string{"Table", "Block", "Cook"} {
		t.Run(suffix, func(t *testing.T) {
			u := unitFromSource(t, tableSrc+"\ntype Probe"+suffix+"\n{\n    x int32\n}\n")
			_, err := c.Generate(u, "elixir", Options{})
			if err == nil {
				t.Fatalf("--lang elixir accepted a declaration named Probe%s — it is the module the "+
					"backend writes for Probe.schema", suffix)
			}
			if !strings.Contains(err.Error(), "Probe"+suffix) || !strings.Contains(err.Error(), "Probe.schema") {
				t.Errorf("the refusal names neither the declaration nor the file: %v", err)
			}
		})
	}
	// and the same names in a TABLE-FREE unit are the author's: this backend
	// emits no such module for one, so nothing collides
	if _, err := c.Generate(unitFromSource(t, packetSrc+"\ntype ProbeTable\n{\n    x int32\n}\n"), "elixir", Options{}); err != nil {
		t.Errorf("a TABLE-FREE unit must keep the name ProbeTable: %v", err)
	}
}

// unitFromNamedSource is unitFromSource with the schema FILE's basename under
// the caller's control, because a generated MODULE name is derived from it.
func unitFromNamedSource(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	name := base + ".schema"
	f, perrs := parser.Parse(name, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: base, Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestElixirModuleNamesAreAliases: a generated MODULE name is not a filename.
// An Elixir alias segment must begin upper-case, so `my_frame.schema` emits
// `my_frameTable.ex` — the packet emitter's own file convention — carrying
// `<Ns>.MyFrameTable`. Emitting the basename raw produced
// `defmodule Probe.my_frameTable`, which is an ArgumentError at compile time,
// and left this backend's own collision check (which already reads the exported
// form) naming a different module than the emitter wrote.
//
// The corpus is CamelCase throughout, which is exactly why nothing saw it.
func TestElixirModuleNamesAreAliases(t *testing.T) {
	u := unitFromNamedSource(t, "my_frame", packetSrc+`
table Holder
{
    x int32
    p Point
}
`)
	files, err := New().Generate(u, "elixir", Options{})
	if err != nil {
		t.Fatalf("--lang elixir: %v", err)
	}
	// the FILES keep the schema basename, as the packet emitter's do
	for _, want := range []string{"my_frameTable.ex", "my_frameBlock.ex", "my_frameCook.ex"} {
		if _, ok := files[want]; !ok {
			t.Errorf("no %s emitted; got %d files", want, len(files))
		}
	}
	// and every MODULE the output names is a legal alias: an upper-case first
	// letter after every dot
	segment := regexp.MustCompile(`\bProbe\.([A-Za-z0-9_]+)`)
	for name, data := range files {
		for _, m := range segment.FindAllStringSubmatch(string(data), -1) {
			first := m[1][0]
			if first < 'A' || first > 'Z' {
				t.Errorf("%s names the module Probe.%s — an Elixir alias segment must begin "+
					"upper-case, so this unit does not compile", name, m[1])
			}
		}
	}
	// the three the basename derives, spelled out so a rename of the helper
	// cannot make the check vacuous
	for _, want := range []string{"Probe.MyFrameTable", "Probe.MyFrameBlock", "Probe.MyFrameCook"} {
		found := false
		for _, data := range files {
			if strings.Contains(string(data), "defmodule "+want+" do") {
				found = true
			}
		}
		if !found {
			t.Errorf("no file declares %s", want)
		}
	}
}

// TestDartEmitsTableSources: the dart target adds <Base>Table.dart beside the
// packet libraries for a unit with tables, and adds NOTHING for one without —
// the same contract the cpp and cs targets hold.
func TestDartEmitsTableSources(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart: %v", err)
	}
	if _, ok := with["ProbeTable.dart"]; !ok {
		t.Fatalf("--lang dart emitted no ProbeTable.dart for a unit with tables; got %d files", len(with))
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart: %v", err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.dart") {
			t.Errorf("--lang dart emitted %s for a table-free unit", name)
		}
	}
	// and the packet half is byte-identical either way: a table moves no
	// packet byte in this target either
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

// TestDartRefusesPointeredTables: the Dart READING TIER has no arena, no
// builder, no region and no node-table codec, so a unit whose closure declares
// a pointer gets no codec — and the refusal is NAMED, in a file that stays, so
// a consumer reaching for Save or Load meets an explanation rather than a
// missing name with none.
func TestDartRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := c.Generate(u, "dart", Options{})
	if err != nil {
		t.Fatalf("--lang dart refused a pointered unit outright — the refusal is a FILE, not an error: %v", err)
	}
	banner := ""
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.dart") {
			continue
		}
		text := string(data)
		if strings.Contains(text, "SaveBody") || strings.Contains(text, "LoadBody") {
			t.Errorf("--lang dart emitted a codec in %s for a pointered unit", name)
		}
		banner = text
	}
	if banner == "" {
		t.Fatal("--lang dart emitted no file at all for a pointered unit — the refusal has nowhere to live")
	}
	if !strings.Contains(banner, "REFUSED, BY NAME") {
		t.Error("the pointered unit's Dart file carries no refusal banner")
	}
	if !strings.Contains(banner, "Node") || !strings.Contains(banner, "named follow-on") {
		t.Error("the refusal does not name the table and the follow-on")
	}
}

// TestDartTableRuntimeNamesAreClaimed is the Dart half of the §11 promise, and
// it asks two things the C# scan does not have to.
//
// THE SPELLING IS lowerCamelCase for a free function and UpperCamel for a
// class, so the scan collects both and normalizes to the registry's PascalCase
// — the two are a bijection (the packet emitter's dartName), which is what
// lets one registry cover the target.
//
// AND A PRIVATE NAME IS STILL A CLAIM. Dart's privacy is per LIBRARY, and a
// schema identifier may begin with an underscore, so a generated `_foo` at
// library scope is a collision no registry covers. The rule this backend holds
// instead is stronger and checkable: it spells NO private library-scope name at
// all, and the second half of this test is what holds it to that.
func TestDartTableRuntimeNamesAreClaimed(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, runtimeSrc), "dart", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ident := regexp.MustCompile(`\b[Tt]able[A-Za-z0-9_]*\b`)
	namespace := unitFromSource(t, runtimeSrc).Package
	emitted := map[string]bool{}
	for name, data := range files {
		if !dartTableSource(name) {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range ident.FindAllString(line, -1) {
				m = capitalizeFirst(m)
				if !strings.EqualFold(m, namespace) {
					emitted[m] = true
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted Dart at all — the scan, not the registry, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Dart table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Dart that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Dart) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Dart backend defines %s, but nothing in the emitted "+
				"Dart names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}

	// NO PRIVATE LIBRARY-SCOPE NAME. A declaration line at column zero whose
	// name begins with an underscore is the whole class: a schema can spell
	// that identifier, so the day one is emitted it is an unclaimed collision.
	private := regexp.MustCompile(`^[A-Za-z<>,\[\] ]*\b_[A-Za-z0-9_]*\s*[=(;]`)
	for name, data := range files {
		if !dartTableSource(name) {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" || line[0] == ' ' || line[0] == '/' {
				continue // indented: a member, which claims nothing
			}
			if private.MatchString(line) {
				t.Errorf("%s declares a PRIVATE library-scope name: %q — a schema may spell that "+
					"identifier, so it is an unclaimed collision; make it a member instead", name, line)
			}
		}
	}
}

// dartTableSource reports whether a generated file is one the DART TABLE
// backend wrote: the wire's <Base>Table.dart, the block form's
// <Base>Block.dart, the cook's <Base>Cook.dart. The packet emitter's own
// libraries are not this backend's to scan.
func dartTableSource(name string) bool {
	return strings.HasSuffix(name, "Table.dart") ||
		strings.HasSuffix(name, "Block.dart") ||
		strings.HasSuffix(name, "Cook.dart")
}
