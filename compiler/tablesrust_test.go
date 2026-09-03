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
