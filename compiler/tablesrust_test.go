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

// TestRustEmitsTableModules: the rust target adds the table accelerator
// modules beside the packet ones for a unit with tables, declares them in the
// generated crate root, and adds NOTHING for a unit without — the same
// contract the other targets hold.
func TestRustEmitsTableModules(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "rust", Options{})
	if err != nil {
		t.Fatalf("--lang rust: %v", err)
	}
	for _, unwanted := range []string{"probe_table.rs", "table_runtime.rs"} {
		if _, ok := with[unwanted]; ok {
			t.Errorf("--lang rust emitted dead wire surface %s", unwanted)
		}
	}
	for _, want := range []string{"probe_block.rs", "probe_cook.rs", "probe_records.rs", "block_runtime.rs", "cook_runtime.rs", "build_version.rs"} {
		if _, ok := with[want]; !ok {
			t.Errorf("--lang rust emitted no %s for a unit with tables; got %d files", want, len(with))
		}
	}
	lib := string(with["lib.rs"])
	for _, want := range []string{"mod probe_block;", "mod probe_cook;", "mod probe_records;", "mod block_runtime;", "mod cook_runtime;", "mod build_version;"} {
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

// TestRustPointeredTablesEmitAccelerators: pointered tables emit their cook
// reader and records without wire codecs.
func TestRustPointeredTablesEmitAccelerators(t *testing.T) {
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
		t.Fatalf("--lang rust refused a pointered unit outright: %v", err)
	}
	cooks := 0
	for name := range files {
		if strings.HasSuffix(name, "_table.rs") || name == "table_runtime.rs" {
			t.Errorf("--lang rust emitted the WIRE surface %s for a pointered unit", name)
		}
		if strings.HasSuffix(name, "_cook.rs") {
			cooks++
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

// fixedFormSrc reaches every shape §3.4's constant-size table names that this
// port carries: scalars at four widths, floats, a bool, an enum, a flags mask,
// a string and a bytes at their bounds, a fixed array, a counted array, a
// nested table, an enum-keyed array of a table, an optional section, and
// declared defaults on top of the lot so the PREFILL image is not all zeros.
const fixedFormSrc = `package probe

enum Slot { Alpha, Beta, Gamma }

flags Mark { One, Two }

table Leaf
{
    a int32 = 7 | min = 0, max = 1000
    b uint16 = 3
}

table FixedProbe
{
    small   uint8 = 5
    wide    uint64 = 9
    negative int16 = -4
    fl      float32 = 2.5
    db      float64 = 1.25
    yes     bool = true
    slot    Slot = Beta
    mark    Mark
    label   string(12)
    raw     bytes(6)
    trio    [3]uint32
    counted [1..4]Leaf
    nested  Leaf
    perslot [Slot]Leaf
    maybe   ?Leaf
}
`

// TestRustFixedFormBlockIsTheCppReferenceByteForByte is what keeps this port's
// §3.4 layout walk from becoming a SECOND WIRE.
//
// The vocabulary block is the form's whole self-description and its fnv1a64 is
// the eight bytes every record carries, so two backends whose blocks differ
// anywhere are two backends that never read each other's records — silently,
// as `no_block`, which looks like a deployment problem and is not one. The C++
// backend is the REFERENCE for this form (internal/codegen/cpptable/
// fixedform.go), so this generates the SAME unit for both and requires the
// bytes and the hash to be identical, per root.
//
// It is the both-directions form the registry scans already use: every root
// C++ emits a block for that Rust also emits one for is compared, and the
// comparison set has to be non-empty or the test, not the emitter, is what
// broke.
func TestRustFixedFormBlockIsTheCppReferenceByteForByte(t *testing.T) {
	unit := unitFromSource(t, fixedFormSrc)
	cppFiles, err := New().Generate(unit, "cpp", Options{})
	if err != nil {
		t.Fatalf("--lang cpp: %v", err)
	}
	rustFiles, err := New().Generate(unitFromSource(t, fixedFormSrc), "rust", Options{})
	if err != nil {
		t.Fatalf("--lang rust: %v", err)
	}
	cppBlocks, cppHashes := cppFixedBlocks(join(cppFiles))
	rustBlocks, rustHashes := rustFixedBlocks(join(rustFiles))
	if len(rustBlocks) == 0 {
		t.Fatal("the Rust backend emitted no fixed-form block at all for a unit of fixed tables — " +
			"the scan, or the emitter, is what broke")
	}
	names := make([]string, 0, len(rustBlocks))
	for name := range rustBlocks {
		names = append(names, name)
	}
	sort.Strings(names)
	compared := 0
	for _, name := range names {
		want, ok := cppBlocks[name]
		if !ok {
			t.Errorf("the Rust backend emits a fixed-form block for %s and the C++ REFERENCE does not — "+
				"a port may carry fewer shapes than the reference, never more", name)
			continue
		}
		compared++
		if want != rustBlocks[name] {
			t.Errorf("%s's vocabulary block differs from the C++ reference's:\n  cpp  %s\n  rust %s",
				name, want, rustBlocks[name])
		}
		if cppHashes[name] != rustHashes[name] {
			t.Errorf("%s's fixed-form hash differs from the C++ reference's: cpp %s, rust %s — "+
				"a record written by one is `no_block` to the other", name, cppHashes[name], rustHashes[name])
		}
	}
	if compared == 0 {
		t.Fatal("no root was compared against the reference at all")
	}
}

func join(files map[string][]byte) string {
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
		b.WriteString("\n")
	}
	return b.String()
}

var (
	// THE REFERENCE SPELLS THE LAYOUT `FixedLayout` (internal/codegen/cpptable/
	// fixedform.go); Rust spells the same bytes `<ROOT>_FIXED_BLOCK`. The two
	// spellings are compared, never assumed equal, because the BYTES are the
	// contract and the identifier is each target's own.
	cppFixedBlockRe = regexp.MustCompile(`(?s)constexpr uint8_t (\w+)FixedLayout\[\] = \{(.*?)\};`)
	cppFixedHashRe  = regexp.MustCompile(`constexpr uint64_t (\w+)FixedHash = (0x[0-9a-f]+)ull;`)
	rsFixedBlockRe  = regexp.MustCompile(`(?s)pub const ([A-Z0-9_]+)_FIXED_BLOCK: \[u8; \d+\] = \[(.*?)\];`)
	rsFixedHashRe   = regexp.MustCompile(`pub const ([A-Z0-9_]+)_FIXED_HASH: u64 = (0x[0-9a-f]+);`)
	fixedByteRe     = regexp.MustCompile(`0x[0-9a-f]{2}`)
)

// cppFixedBlocks and rustFixedBlocks key on the SCREAMING spelling, which is
// the one shape both targets can be mapped onto without either one's naming
// rules leaking into the other's.
func cppFixedBlocks(text string) (map[string]string, map[string]string) {
	blocks, hashes := map[string]string{}, map[string]string{}
	for _, m := range cppFixedBlockRe.FindAllStringSubmatch(text, -1) {
		blocks[screamingOf(m[1])] = strings.Join(fixedByteRe.FindAllString(m[2], -1), " ")
	}
	for _, m := range cppFixedHashRe.FindAllStringSubmatch(text, -1) {
		hashes[screamingOf(m[1])] = m[2]
	}
	return blocks, hashes
}

func rustFixedBlocks(text string) (map[string]string, map[string]string) {
	blocks, hashes := map[string]string{}, map[string]string{}
	for _, m := range rsFixedBlockRe.FindAllStringSubmatch(text, -1) {
		blocks[m[1]] = strings.Join(fixedByteRe.FindAllString(m[2], -1), " ")
	}
	for _, m := range rsFixedHashRe.FindAllStringSubmatch(text, -1) {
		hashes[m[1]] = m[2]
	}
	return blocks, hashes
}

// screamingOf is ir.RustConstName's rule, spelled here so the test does not
// borrow the emitter's own helper to check the emitter.
func screamingOf(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'a' && r <= 'z' {
			r -= 32
		}
		b.WriteRune(r)
	}
	return b.String()
}
