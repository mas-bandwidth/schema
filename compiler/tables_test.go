// Tests for the tables generation surface (docs/SPEC-TABLES.md): the C, C++,
// C#, Go, JavaScript and Rust targets grow table sources, every other target
// refuses BY NAME, non-table output is byte-identical with or without tables,
// and the
// generated codecs allocate nothing.
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
// under a target that carries no table backend — loudly, never by silently
// dropping the tables — and the refusal names every target that does carry
// one. The target under test is a probe registered here rather than a
// built-in, so the test keeps testing the day the last tableless built-in
// gains its backend.
func TestTablelessTargetsRefuseTables(t *testing.T) {
	c := New()
	if err := c.Register(probeTarget{}); err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, tableSrc)
	_, err := c.Generate(u, "probe", Options{})
	if err == nil {
		t.Fatal("--lang probe accepted a unit with tables — it must refuse by name")
	}
	msg := err.Error()
	for _, want := range []string{"Config", "probe table backend is a named follow-on", "--lang cpp"} {
		if !strings.Contains(msg, want) {
			t.Errorf("--lang probe refusal does not say %q: %v", want, err)
		}
	}
	if len(tableTargets) == 0 {
		t.Fatal("no built-in target registered with tables")
	}
	for _, name := range tableTargets {
		if !strings.Contains(msg, "--lang "+name) {
			t.Errorf("the refusal does not name the %s table backend: %v", name, err)
		}
	}
	if strings.Contains(msg, "--lang probe") {
		t.Errorf("the refusal names the refusing target as a carrier: %v", err)
	}
}

// probeTarget carries no table backend, whatever the built-ins do.
type probeTarget struct{}

func (probeTarget) Names() []string { return []string{"probe"} }

func (probeTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "probe"); err != nil {
		return nil, err
	}
	return map[string][]byte{}, nil
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

// dartTableSource reports whether a generated file is one the DART TABLE
// backend wrote: the wire's <Base>Table.dart, the block form's
// <Base>Block.dart, the cook's <Base>Cook.dart. The packet emitter's own
// libraries are not this backend's to scan.
func dartTableSource(name string) bool {
	return strings.HasSuffix(name, "Table.dart") ||
		strings.HasSuffix(name, "Block.dart") ||
		strings.HasSuffix(name, "Cook.dart")
}
