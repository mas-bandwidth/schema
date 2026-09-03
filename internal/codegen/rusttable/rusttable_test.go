package rusttable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitFrom(t *testing.T, src string) *ir.Unit {
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

const valueOnly = `package probe

enum Grade { Bronze, Silver, Gold }

type Inner
{
    factor float32 = 2.5
}

table Config
{
    scale  float32 = 1.0
    label  string(24)
    grade  Grade = Silver
    slots  [Grade]int32
    inner  Inner
    extra  ?Inner
    items  [..8]int32
}
`

const pointered = valueOnly + `
table Node
{
    value int32
    next  *Node
}
`

func generate(t *testing.T, src string) map[string][]byte {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

// TestGenerationIsDeterministic: regeneration is byte-stable, so a golden pin
// and a diff both mean what they say.
func TestGenerationIsDeterministic(t *testing.T) {
	first := generate(t, pointered)
	for range 3 {
		again := generate(t, pointered)
		if len(again) != len(first) {
			t.Fatalf("regeneration produced %d files, not %d", len(again), len(first))
		}
		for name, data := range first {
			if string(again[name]) != string(data) {
				t.Fatalf("regeneration is not byte-stable: %s moved", name)
			}
		}
	}
}

// TestTableFreeUnitEmitsNothing is the zero-cost statement at the grain the
// Rust target holds it: a unit that declares no table grows no table module at
// all, so nothing about the form reaches a crate that does not use it.
func TestTableFreeUnitEmitsNothing(t *testing.T) {
	out := generate(t, `package probe

type Point
{
    x float32
    y float32
}
`)
	if len(out) != 0 {
		names := make([]string, 0, len(out))
		for name := range out {
			names = append(names, name)
		}
		t.Fatalf("a table-free unit grew %v", names)
	}
}

// TestSharedRuntimeIsUnitIndependent: the text form is ONE walk over the
// descriptors (docs/SPEC-TABLES.md §16.1), so the shared runtime's bytes must
// not vary with what a unit declares. The Makefile's tables-rust-walk holds
// the whole corpus to this; here it is held between two units that disagree
// about everything a schema can disagree about.
func TestSharedRuntimeIsUnitIndependent(t *testing.T) {
	one := generate(t, valueOnly)[RuntimeModule+".rs"]
	two := generate(t, `package other

flags Perks { Shielded, Cloaked }

type Buff
{
    multiplier float32 = 1.0
}

union Effect
{
    buff Buff
}

table Wide
{
    blob   bytes(16)
    perks  Perks
    effect Effect
    tally  [4]uint16
}
`)[RuntimeModule+".rs"]
	if len(one) == 0 || len(two) == 0 {
		t.Fatal("a unit with tables emitted no shared runtime")
	}
	// the banner names the schema file and the package; the runtime below it
	// is the comparison
	body := func(data []byte) string {
		text := string(data)
		if i := strings.Index(text, "// ---- the read report"); i >= 0 {
			return text[i:]
		}
		return text
	}
	if body(one) != body(two) {
		t.Error("the shared table runtime is not the same bytes in two units — the text form would be a codec per unit rather than one walk (§16.1)")
	}
}

// TestPointeredUnitRefusesTheWireSurface: the variable class's refusal is a
// refusal of the WIRE surface and of nothing else (§11). The two ACCELERATORS
// need no codec, so both are emitted, and every file that IS emitted opens
// with a banner naming the refused tables and the follow-on.
func TestPointeredUnitRefusesTheWireSurface(t *testing.T) {
	out := generate(t, pointered)
	for name, data := range out {
		if strings.HasSuffix(name, "_table.rs") || name == RuntimeModule+".rs" {
			t.Errorf("a pointered unit emitted the wire surface %s", name)
		}
		if !strings.Contains(string(data), "REFUSED, BY NAME") {
			t.Errorf("%s carries no refusal banner", name)
		}
		if !strings.Contains(string(data), "Node") {
			t.Errorf("%s does not name the refused table", name)
		}
	}
	if _, ok := out["probe_cook.rs"]; !ok {
		t.Error("a pointered unit emitted no cook reader — a root is any table (§7)")
	}
}

// TestValueOnlyUnitCarriesTheWholeSurface: what a fixed-class unit gets, by
// name — the storage, the three codecs, the descriptors, the text form, and
// the two accelerators.
func TestValueOnlyUnitCarriesTheWholeSurface(t *testing.T) {
	out := generate(t, valueOnly)
	table := string(out["probe_table.rs"])
	if table == "" {
		t.Fatal("no probe_table.rs")
	}
	for _, want := range []string{
		"pub struct Config", "pub fn config_measure", "pub fn config_save",
		"pub fn config_load", "pub fn config_reset", "pub fn config_table_type",
		"pub fn config_from_json", "pub fn config_to_json", "pub fn config_to_json_measure",
		"impl TableEnum for Grade", "TableKeyed<i32, { Grade::MAX.0 as usize }>",
		"pub extra_present: bool",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("the fixed-class surface is missing %q", want)
		}
	}
	// the generated codecs allocate nothing: the caller owns every buffer
	for _, banned := range []string{"Vec<", "String::", "Box<", "to_string()", "format!"} {
		if strings.Contains(table, banned) {
			t.Errorf("the generated table module contains %q — generated codecs must not allocate", banned)
		}
	}
	// and it stands alone: no serialize dependency reaches the table surface
	for _, banned := range []string{"serialize::", "use serialize"} {
		if strings.Contains(table, banned) {
			t.Errorf("the generated table module contains %q — it must stand alone", banned)
		}
	}
	for _, want := range []string{"probe_cook.rs", "probe_block.rs", "probe_records.rs",
		"block_runtime.rs", BuildVersionModule + ".rs",
		CookRuntimeModule + ".rs", RuntimeModule + ".rs"} {
		if _, ok := out[want]; !ok {
			t.Errorf("a fixed-class unit emitted no %s", want)
		}
	}
}

// TestSharedRuntimesAreFileOrderIndependent is #347/#351's rule, held for
// Rust: the unit's THREE shared runtimes are named by the PACKAGE, so a corpus
// file that sorts earlier cannot relocate one of them. Before the cook runtime
// moved out of "the first file that declares a table", adding a file named
// earlier in the alphabet moved two thousand lines between modules — correct
// output, and a diff nobody can read.
func TestSharedRuntimesAreFileOrderIndependent(t *testing.T) {
	one := generate(t, valueOnly)
	two, err := Generate(unitWithExtraFile(t, valueOnly))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, name := range []string{RuntimeModule + ".rs", CookRuntimeModule + ".rs", BlockRuntimeModule + ".rs"} {
		a, ok := one[name]
		if !ok {
			t.Fatalf("no %s", name)
		}
		b, ok := two[name]
		if !ok {
			t.Fatalf("%s disappeared when a file was added ahead of the first one", name)
		}
		if string(a) != string(b) {
			t.Errorf("%s moved when a file sorted ahead of the first: a shared runtime is named by the PACKAGE, so file order must not reach it (docs/SPEC-TABLES.md §19.2)", name)
		}
	}
	// and the per-file modules of the ORIGINAL file are untouched too
	for _, name := range []string{"probe_table.rs", "probe_cook.rs", "probe_block.rs"} {
		if string(one[name]) != string(two[name]) {
			t.Errorf("%s moved when an unrelated file was added ahead of it", name)
		}
	}
}

// A DOCUMENTATION-ONLY file, on purpose. It must declare nothing: adding a
// `type` would move the unit's protocol id, and the protocol id is in every
// generated banner and folded into the build version — so a moved runtime
// would then be correct rather than a defect, and the test would be measuring
// the wrong thing. What this file changes is the FILE ORDER and nothing else.
const extraFileSrc = `package probe

// a file that declares nothing, so the only thing it changes is which
// basename sorts first
`

// unitWithExtraFile is the same unit with one more schema file whose basename
// sorts BEFORE the original's — the exact edit that used to relocate a runtime.
func unitWithExtraFile(t *testing.T, src string) *ir.Unit {
	t.Helper()
	files := []check.SourceFile{}
	for _, f := range []struct{ base, text string }{
		{"Alpha", extraFileSrc},
		{"Probe", src},
	} {
		ast, perrs := parser.Parse(f.base+".schema", []byte(f.text))
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", f.base, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: f.base + ".schema", Name: f.base + ".schema", Base: f.base,
			Bytes: []byte(f.text), AST: ast,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestRelocatableStorageIsAsserted: every closure member is #[repr(C)] and
// Copy, so a table value may be copied, mmapped or handed across a process
// boundary as bytes (§9). The assert is a compile error the day one is not.
func TestRelocatableStorageIsAsserted(t *testing.T) {
	table := string(generate(t, valueOnly)["probe_table.rs"])
	for _, want := range []string{
		"const fn table_relocatable<T: Copy>()",
		"const _: () = table_relocatable::<Config>();",
		"const _: () = table_relocatable::<Inner>();",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("the relocatability assert is missing %q", want)
		}
	}
}

// TestCookLayoutIsAssertedAtCompileTime: the cooked record's C ABI layout is
// the contract (§20.3), and Rust says so with a const block — which fails the
// BUILD rather than a test.
func TestCookLayoutIsAssertedAtCompileTime(t *testing.T) {
	out := generate(t, valueOnly)
	// THE RECORDS AND THEIR CONTRACT travel together, in a module BOTH
	// accelerators are built from: a cooked record IS the blittable row
	// (§7.2, §19.3), so the family belongs to neither cargo feature.
	records := string(out["probe_records.rs"])
	if records == "" {
		t.Fatal("no probe_records.rs")
	}
	for _, want := range []string{
		"pub struct ConfigRow",
		"const _: () = assert!(core::mem::size_of::<ConfigRow>() ==",
		"const _: () = assert!(core::mem::offset_of!(ConfigRow, label) ==",
	} {
		if !strings.Contains(records, want) {
			t.Errorf("the record family is missing %q", want)
		}
	}
	// a string's cooked buffer is char[N + 1] — the layout model's spelling,
	// not the wire storage's [u8; N]
	if !strings.Contains(records, "pub label: [u8; 25],") {
		t.Error("a cooked string buffer is not the layout model's N + 1 bytes (§7.2)")
	}
	cook := string(out["probe_cook.rs"])
	if cook == "" {
		t.Fatal("no probe_cook.rs")
	}
	for _, want := range []string{
		"pub struct ConfigCook",
		"pub unsafe fn open(bytes: *const u8, length: u64) -> Option<ConfigCook>",
	} {
		if !strings.Contains(cook, want) {
			t.Errorf("the cooked form is missing %q", want)
		}
	}
}
