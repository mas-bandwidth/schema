// Tests for the tables generation surface (SPEC-TABLES.md): the C++ and C#
// targets grow table sources, every other target refuses BY NAME, non-table
// output is byte-identical with or without tables, and the generated codecs
// allocate nothing.
package compiler

import (
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
// dropping the tables. cpp and cs carry one (SPEC-TABLES.md, backend status).
func TestTablelessTargetsRefuseTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, tableSrc)
	for _, target := range []string{"c", "dart", "elixir", "go", "java", "js", "rust"} {
		if _, err := c.Generate(u, target, Options{}); err == nil {
			t.Errorf("--lang %s accepted a unit with tables — it must refuse by name", target)
		} else if !strings.Contains(err.Error(), "C++ and C# only") || !strings.Contains(err.Error(), "Config") {
			t.Errorf("--lang %s refusal does not name the rule and the tables: %v", target, err)
		}
	}
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

// TestCsRefusesPointeredTables: the C# backend carries the FIXED class, and a
// unit whose closure declares a pointer is refused BY NAME with the variable
// class named as the follow-on (SPEC-TABLES.md §11).
func TestCsRefusesPointeredTables(t *testing.T) {
	c := New()
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	_, err := c.Generate(u, "cs", Options{})
	if err == nil {
		t.Fatal("--lang cs accepted a pointered unit — it must refuse by name")
	}
	if !strings.Contains(err.Error(), "Node") || !strings.Contains(err.Error(), "variable class is a named follow-on") {
		t.Errorf("the C# pointer refusal does not name the table and the follow-on: %v", err)
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
	// text form's walk (SPEC-TABLES.md §6.1, §13.5), and the block form's own
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
	// descriptor's reset hook (SPEC-TABLES.md §8) which is the same prefill
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
// include (SPEC-TABLES.md §2.2). Per TABLE the guarantee is narrower and
// TestPointerSurfaceEmitted states it exactly.
//
// What the gate is NOT about: the reflection surface and the text form that
// walks it ride in every table closure's header by design, fixed class
// included (SPEC-TABLES.md §16.1), and <cstdlib> is one of the text form's
// three number-conversion includes rather than the arena's — which is why it
// left this list when the walk landed.
func TestZeroCostForValueOnlyTables(t *testing.T) {
	header := tableHeader(t, tableSrc)
	for _, leak := range []string{
		"TableArena", "TableSlot", "TableWorker", "TableRef", "TableRegion",
		"kTableSegment", "kTableSlab", "kTableMaxDepth", "is_pointer", "variable",
		"Builder", "LayoutId", "OpenWalk", "PackMeasure", "LoadMeasure",
		"Cook", "Open", "<atomic>", "template",
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
	// (SPEC-TABLES.md §11's 23). Line comments are stripped first — prose is
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
