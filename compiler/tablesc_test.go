// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

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
