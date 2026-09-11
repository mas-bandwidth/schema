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

fixed table Config
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

// TestBlockRuntimeIsUnitIndependent: the block runtime's bytes must not vary
// with what a unit declares.
func TestBlockRuntimeIsUnitIndependent(t *testing.T) {
	one := generate(t, valueOnly)[BlockRuntimeModule+".rs"]
	two := generate(t, `package other

flags Perks { Shielded, Cloaked }

type Buff
{
    multiplier float32 = 1.0
}

fixed table Wide
{
    blob   bytes(16)
    perks  Perks
    buff   Buff
    tally  [4]uint16
}
`)[BlockRuntimeModule+".rs"]
	if len(one) == 0 || len(two) == 0 {
		t.Fatal("a unit with tables emitted no block runtime")
	}
	body := func(data []byte) string {
		text := string(data)
		if i := strings.Index(text, "// The block form's shared runtime"); i >= 0 {
			return text[i:]
		}
		return text
	}
	if body(one) != body(two) {
		t.Error("the block runtime is not the same bytes in two units")
	}
}

// TestAcceleratorsEmitted: what a table unit gets — the two accelerators
// (block and cook), their shared runtimes, records, and the build version.
// No wire surface (*_table.rs or table_runtime.rs) is emitted.
func TestAcceleratorsEmitted(t *testing.T) {
	out := generate(t, valueOnly)
	for _, unwanted := range []string{"probe_table.rs", "table_runtime.rs"} {
		if _, ok := out[unwanted]; ok {
			t.Errorf("emitted dead wire surface %s", unwanted)
		}
	}
	for _, want := range []string{
		"probe_cook.rs", "probe_block.rs", "probe_records.rs",
		"block_runtime.rs", BuildVersionModule + ".rs",
		CookRuntimeModule + ".rs",
	} {
		if _, ok := out[want]; !ok {
			t.Errorf("a table unit emitted no %s", want)
		}
	}
}

// TestSharedRuntimesAreFileOrderIndependent is #347/#351's rule, held for
// Rust: the unit's shared runtimes are named by the PACKAGE, so a corpus
// file that sorts earlier cannot relocate one of them.
func TestSharedRuntimesAreFileOrderIndependent(t *testing.T) {
	one := generate(t, valueOnly)
	two, err := Generate(unitWithExtraFile(t, valueOnly))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, name := range []string{CookRuntimeModule + ".rs", BlockRuntimeModule + ".rs"} {
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
	for _, name := range []string{"probe_cook.rs", "probe_block.rs", "probe_records.rs"} {
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

// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----------------

// unionReaching is a table whose by-value closure reaches a UNION. It USED to
// have no fixed form here: the value is the unit's blittable `<Name>Row` and
// Rust spells a union on the packet wire as a real enum with no committed
// payload layout, so the Row family stopped at one. The `#[repr(C)]` twin
// (cook.go's `rowable`) is that Row, and this is now an ordinary fixed root.
const unionReaching = `package probe

type Boost
{
    power int32
}

type Ward
{
    charge float32
}

union Effect
{
    boost Boost
    ward  Ward
}

table Holder
{
    effect Effect
    tail   int32
}
`

// unionWithTextArm is the arm shape that is STILL refused, and it is refused
// by §3.4's own layout law rather than by this port: an arm whose storage needs
// a COMPANION beside it — a string's used length, a counted array's count — is
// two pieces that must occupy one slot of the overlay, which the C++ reference
// spells as an unnamed struct and no port lays out yet.
const unionWithTextArm = `package probe

union Note
{
    text  string(8)
    tally int32
}

table Holder
{
    note Note
}
`

// variableTable is what §3.4 refuses outright rather than what this port does:
// a pointer has no bound to be constant at, which is what makes a table
// VARIABLE, and a variable table keeps §3 entirely.
func TestFixedFormIsEmittedForAFixedRootAndForNothingElse(t *testing.T) {
	fixed := generate(t, valueOnly)
	if _, ok := fixed["probe_fixed.rs"]; !ok {
		t.Fatalf("no probe_fixed.rs for a unit of fixed tables; got %v", keysOf(fixed))
	}
	if _, ok := fixed[FixedRuntimeModule+".rs"]; !ok {
		t.Errorf("no %s.rs beside the fixed form; the plan compiler and the read loop have no home",
			FixedRuntimeModule)
	}
	body := string(fixed["probe_fixed.rs"])
	for _, want := range []string{
		"CONFIG_FIXED_HASH", "CONFIG_FIXED_BLOCK", "CONFIG_FIXED_DEFAULTS",
		"CONFIG_FIXED_PLAN", "config_fixed_save", "config_fixed_load",
		"config_fixed_write_body", "config_fixed_scatter",
		"TableFixedReason::PreviousForm",
		"TableFixedReason::MessageFormAsFile",
		"TableFixedReason::NewerForm",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the fixed form's surface is missing %s", want)
		}
	}
	// THE IDENTITY PLAN IS ONE ENTRY. In the image domain the record's declared
	// order IS the destination's, so §3.4's coalescing rule takes the whole body
	// to a single move — and a plan that grew a second entry means the
	// destination stopped being the image.
	if !strings.Contains(body, "CONFIG_FIXED_PLAN: [TableFixedEntry; 1]") {
		t.Error("the identity plan is not ONE entry — the whole body in one move (docs/SPEC-TABLES.md §3.4)")
	}

	// A UNION HAS A ROW NOW, so a union-reaching table has a fixed form: the
	// twin's tag beside the #[repr(C)] overlay, the union's own store-and-
	// scatter pair, and the whole surface of an ordinary fixed root.
	union := generate(t, unionReaching)
	if _, ok := union["probe_fixed.rs"]; !ok {
		t.Fatalf("a union-reaching table got no fixed form; got %v", keysOf(union))
	}
	if _, ok := union[FixedRuntimeModule+".rs"]; !ok {
		t.Errorf("no %s.rs beside a union-reaching fixed root", FixedRuntimeModule)
	}
	ubody := string(union["probe_fixed.rs"])
	for _, want := range []string{
		"HOLDER_FIXED_HASH", "holder_fixed_save", "holder_fixed_load",
		"effect_fixed_write_body", "effect_fixed_scatter",
		"let arm = unsafe { value.arms.boost };",
		"value.arms.ward = arm;",
		"report.clamped += 1;",
	} {
		if !strings.Contains(ubody, want) {
			t.Errorf("the union's fixed surface is missing %q", want)
		}
	}
	urecords := string(union["probe_records.rs"])
	for _, want := range []string{
		"pub struct EffectRow {",
		// THE TAG AND THE OVERLAY ARE THE CRATE'S. A public tag beside a public
		// overlay, with safe functions reading the arm the tag names, is unsound
		// as an API: nothing stops a caller moving one without the other, and
		// the next safe read builds a `bool` out of a byte that is neither 0 nor
		// 1. The public surface is the CHECKED ACCESSOR PAIR below.
		"pub(crate) tag: u8,",
		"pub(crate) arms: EffectRowArms,",
		"pub union EffectRowArms {",
		"pub fn tag(&self) -> u8 {",
		"pub fn boost(&self) -> Option<BoostRow> {",
		"        if self.tag == 1 {",
		"pub fn set_boost(&mut self, value: BoostRow) {",
		"pub fn set_none(&mut self) {",
		"const _: () = assert!(core::mem::offset_of!(EffectRow, tag) == 0,",
		"const _: () = assert!(core::mem::offset_of!(EffectRowArms, boost) == 0,",
		"pub effect: EffectRow,",
	} {
		if !strings.Contains(urecords, want) {
			t.Errorf("the union twin is missing %q", want)
		}
	}
	// THE TWIN IS ir.UnionLayout AND NOTHING THIS FILE INVENTED: a one-byte tag,
	// the arms overlaid at the greatest arm alignment, the whole rounded up.
	if !strings.Contains(urecords, "const _: () = assert!(core::mem::size_of::<EffectRow>() == 8,") ||
		!strings.Contains(urecords, "const _: () = assert!(core::mem::offset_of!(EffectRow, arms) == 4,") {
		t.Error("the union twin's layout is not ir.UnionLayout's (docs/SPEC-TABLES.md §7.2, §20.3)")
	}
	// AND THE UNION'S OWN READ IS THE ONLY `unsafe` ON THIS PATH. A write of a
	// union field is safe in Rust, so the scatter has none at all.
	if strings.Contains(ubody, "unsafe { &mut") {
		t.Error("the scatter took a mutable reference into the overlay; a WRITE of a union field needs none")
	}

	// THE ARM SHAPE THAT IS STILL REFUSED, and it is §3.4's refusal: an arm
	// whose storage needs a companion beside it.
	text := generate(t, unionWithTextArm)
	if _, ok := text["probe_fixed.rs"]; ok {
		t.Error("a union with a TEXT arm got a fixed form; the overlay's companion pieces are unnamed here")
	}
	if _, ok := text[FixedRuntimeModule+".rs"]; ok {
		t.Error("the fixed runtime rode into a unit with no fixed root at all")
	}
	if strings.Contains(string(text["probe_records.rs"]), "pub union NoteRowArms") {
		t.Error("a union with a companioned arm got a twin; its arm is two pieces in one slot")
	}

	// A POINTER makes its holder VARIABLE (§2.2), which §3.4 refuses outright:
	// the ROOT is gone from the fixed form and the fixed table beside it stays.
	pointerUnit := generate(t, pointered)
	pbody := string(pointerUnit["probe_fixed.rs"])
	if strings.Contains(pbody, "NODE_FIXED_HASH") {
		t.Error("a pointered table got a fixed form; a pointer has no bound to be constant at")
	}
	if !strings.Contains(pbody, "CONFIG_FIXED_HASH") {
		t.Error("a variable table beside a fixed one took the fixed one's form away with it")
	}
}

// TestFixedFormHashAndBlockAreDeterministic: the block is the form's whole
// self-description and its fnv1a64 is the eight bytes every record carries, so
// a block that moved between two runs of the same compiler over the same source
// is a wire that moved.
func TestFixedFormHashAndBlockAreDeterministic(t *testing.T) {
	first := string(generate(t, valueOnly)["probe_fixed.rs"])
	second := string(generate(t, valueOnly)["probe_fixed.rs"])
	if first != second {
		t.Fatal("the fixed form's emission is not byte-stable across two runs")
	}
}

func keysOf(out map[string][]byte) []string {
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	return names
}

// wideKinds declares the two families §15's refusal names — fixed-point and
// 128-bit — on a table that is otherwise a plain fixed root. It is the bench
// corpus's shape in miniature (bench/corpus/Bench.schema's server_time,
// wide_key, flux and ping).
const wideKinds = `package probe

table Wide
{
    server_time fixed(24, 8)  | min = 0, max = 65535
    ping        ufixed(8, 8)  | min = 0, max = 250
    wide_key    uint128
    flux        int128       | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
    plain       int32
}
`

// wideKindsNoFixedRoot is the same two families on a table a pointer makes
// VARIABLE, so the unit has no fixed root at all and §15's refusal is the
// whole answer.
const wideKindsNoFixedRoot = `package probe

table Wide
{
    wide_key uint128
    next     *Wide
}
`

// TestWideKindsAreRefusedByTheAcceleratorsAndCarriedByTheWire holds
// docs/SPEC-TABLES.md §15's refusal to the two things that owe it. THE BLOCK
// AND THE COOK owe it: they name a kind's storage column and its reflection
// descriptor, and they do not carry these two families yet. THE FIXED FORM
// (§3.4) does not owe it: a field there is a store of its width at its offset,
// so a 128-bit field is a `u128` store and a fixed-point one is its raw
// integer, and the block entry carries the kind byte the reader compares.
//
// The row types are the load-bearing half and they are not asserted here
// alone: emitCookLayout's const asserts hold every one of them to
// ir.RecordLayout at COMPILE TIME, so a row that spelled a 128-bit field u64
// fails the build rather than laying a record down at the wrong offsets.
func TestWideKindsAreRefusedByTheAcceleratorsAndCarriedByTheWire(t *testing.T) {
	out := generate(t, wideKinds)

	if _, ok := out["probe_fixed.rs"]; !ok {
		t.Fatalf("a fixed root declaring the wide kinds got no fixed form; got %v", keysOf(out))
	}
	if _, ok := out[FixedRuntimeModule+".rs"]; !ok {
		t.Errorf("no %s.rs beside the fixed form", FixedRuntimeModule)
	}

	// the accelerators stood down, and BOTH of them
	for _, name := range []string{"probe_cook.rs", CookRuntimeModule + ".rs", "probe_block.rs", BlockRuntimeModule + ".rs"} {
		if _, ok := out[name]; ok {
			t.Errorf("%s rode into a unit whose kinds it does not carry", name)
		}
	}

	// the rows the fixed form's value IS: the raw integer at the declared
	// storage width, signed by `fixed` against `ufixed`, and 128 bits native
	rows := string(out["probe_records.rs"])
	for _, want := range []string{
		"pub server_time: i32,",
		"pub ping: u16,",
		// AND THE 128-BIT SLOTS ARE THE ALIGNED WRAPPERS: ir's model puts a
		// 128-bit integer at sixteen ALIGNED SIXTEEN, and Rust's own u128 takes
		// the target's C alignment — eight on s390x — so the bare type would
		// move the field there and the layout asserts say so.
		"pub wide_key: TableU128,",
		"pub flux: TableI128,",
	} {
		if !strings.Contains(rows, want) {
			t.Errorf("the row does not carry %q", want)
		}
	}

	// AND THE REFUSAL IS STILL THE WHOLE ANSWER where nothing is left to
	// emit — BY NAME, naming every field, exactly as §15 states it.
	_, err := Generate(unitFrom(t, wideKindsNoFixedRoot))
	if err == nil {
		t.Fatal("a wide-kind unit with no fixed root generated; §15's refusal is owed")
	}
	for _, want := range []string{"Wide.wide_key uint128", "docs/SPEC-TABLES.md §3, §15"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// wideTextFixed is a `wstring(8)` inside a fixed root, with a scalar either
// side of it so the field's own span is pinned by its neighbours.
//
// THE COMPILER STILL REFUSES THIS UNIT for --lang rust (compiler/widetext.go:
// table wide text is a named follow-on for this port, because the two
// ACCELERATORS have no kind-33 column). This test reaches the emitter directly,
// which is the only way to hold the fixed form to the shape before the
// follow-on lands — and the shape is what was wrong: with no TWString case the
// field fell to the scalar default, got a `u8` row slot and a store of ZERO
// BYTES, `b[at..at + 0]`, which does not compile and would have silently
// skipped the field if it had.
const wideTextFixed = `package probe

table Caption
{
    lead  int32
    wide  wstring(8)
    trail int32
}
`

func TestFixedFormCarriesWideText(t *testing.T) {
	out := generate(t, wideTextFixed)

	// THE ROW is ir's own model (ir/blocklayout.go): char16_t[N + 1] at two,
	// then the int32 used length in CODE UNITS.
	rows := string(out["probe_records.rs"])
	for _, want := range []string{
		"pub wide: [u16; 9], // wstring(8): UTF-16 code units, used length beside it",
		"pub wide_length: i32,",
		"const _: () = assert!(core::mem::offset_of!(CaptionRow, wide_length) == 24,",
	} {
		if !strings.Contains(rows, want) {
			t.Errorf("the row does not carry %q", want)
		}
	}

	fixed := string(out["probe_fixed.rs"])
	if strings.Contains(fixed, "at + 0]") {
		t.Error("a zero-width store survived: the wstring fell to the scalar default again")
	}
	for _, want := range []string{
		// the WRITE: the used length, then that many TWO-BYTE units onto the
		// template's zeros — never the whole declared span
		"value.wide_length >= 0 && value.wide_length <= 8,",
		"b[4..8].copy_from_slice(&(value.wide_length as u32).to_le_bytes());",
		"let n = value.wide_length.clamp(0, 8) as usize;",
		"b[at..at + 2].copy_from_slice(&value.wide[i].to_le_bytes());",
		// and the SCATTER, unit by unit back into the buffer
		"value.wide = [0u16; 9];",
		"value.wide[i] = u16::from_le_bytes(b[at..at + 2].try_into().expect(\"two bytes\"));",
		"value.wide[value.wide_length as usize] = 0;",
	} {
		if !strings.Contains(fixed, want) {
			t.Errorf("the fixed form's wide text is missing %q", want)
		}
	}

	// AND THE SPAN IS ir's: 4 for the lead, then 4 + 2 * 8 for the text, so the
	// trailing scalar sits at 24 and the body is 28. A `wstring(N)` that had
	// been measured as a scalar would put `trail` somewhere else entirely.
	if !strings.Contains(fixed, "pub const CAPTION_FIXED_BODY_BYTES: usize = 28;") {
		t.Error("the body is not 4 + (4 + 2 * 8) + 4 — the wstring's constant size is not ir's")
	}
	if !strings.Contains(fixed, "b[24..24 + 4].copy_from_slice(&(value.trail as u32).to_le_bytes());") &&
		!strings.Contains(fixed, "let at = 24;") {
		t.Error("the field after the wstring does not start at 24")
	}
}

// rustFn is the source of one generated function, from its signature to the
// matching close brace, so a pin can name the load loop without matching the
// rest of the module.
func rustFn(body, name string) string {
	sig := "pub fn " + name + "("
	i := strings.Index(body, sig)
	if i < 0 {
		return ""
	}
	n := 0
	for j := i; j < len(body); j++ {
		switch body[j] {
		case '{':
			n++
		case '}':
			n--
			if n == 0 {
				return body[i : j+1]
			}
		}
	}
	return body[i:]
}

// TestFixedFormLoadPrefillsThePlanHoles holds Glenn's ruling: prefill the bytes
// the plan does not write. The compiler computes the unwritten ranges; the
// load copies defaults into exactly those; identity's list is empty because
// its plan is one Copy of the whole body. There is no identity flag in the
// record loop — an empty list is what skips the work.
func TestFixedFormLoadPrefillsThePlanHoles(t *testing.T) {
	out := generate(t, valueOnly)
	body := string(out["probe_fixed.rs"])
	load := rustFn(body, "config_fixed_load")
	if load == "" {
		t.Fatal("config_fixed_load was not emitted")
	}
	if !strings.Contains(load, "table_fixed_holes") {
		t.Error("the load does not compute the plan's unwritten ranges")
	}
	if !strings.Contains(load, "CONFIG_FIXED_DEFAULTS") {
		t.Error("the load has no default image to copy into holes")
	}
	i := strings.Index(load, "for k in 0..n")
	if i < 0 {
		t.Fatal("the load has no record loop")
	}
	loop := load[i:]
	if strings.Contains(loop, "identity") {
		t.Error("the load loop still branches on identity; an empty hole list is what skips work")
	}
	runtime := string(out[FixedRuntimeModule+".rs"])
	for _, want := range []string{
		"struct TableFixedHole",
		"fn table_fixed_holes",
		"TABLE_FIXED_HEADER_BYTES",
		"TABLE_FIXED_HASH_AT",
	} {
		if !strings.Contains(runtime, want) {
			t.Errorf("the fixed runtime is missing %q", want)
		}
	}
}

// liveWriteSrc is FX1's leftover in miniature: a counted array, a bytes(N)
// and a string(N). The writer used to dump the whole bound after body.fill(0),
// so slack of a short count/length rode. There is ONE writer; identity and
// compiled both call it.
const liveWriteSrc = `package probe

table Probe
{
    marks [..4]int32
    blob  bytes(6)
    label string(8)
}
`

// TestFixedFormWriteIsLiveCountAndLength holds the leftover: write LIVE
// count/length only, and the slack stays the template's zeros. Copying
// `0..4` / `blob[..6]` / `label[..8]` put storage past the used count and
// length on the wire.
func TestFixedFormWriteIsLiveCountAndLength(t *testing.T) {
	out := generate(t, liveWriteSrc)
	body := string(out["probe_fixed.rs"])
	write := rustFn(body, "probe_fixed_write_body")
	if write == "" {
		t.Fatal("probe_fixed_write_body was not emitted")
	}
	for _, want := range []string{
		"for i in 0..value.marks_count.clamp(0, 4) as usize",
		"let n = value.blob_length.clamp(0, 6) as usize;",
		"copy_from_slice(&value.blob[..n]);",
		"let n = value.label_length.clamp(0, 8) as usize;",
		"copy_from_slice(&value.label[..n]);",
		"value.marks_count >= 0 && value.marks_count <= 4,",
		"value.blob_length >= 0 && value.blob_length <= 6,",
		"value.label_length >= 0 && value.label_length <= 8,",
	} {
		if !strings.Contains(write, want) {
			t.Errorf("the writer is missing live count/length %q", want)
		}
	}
	for _, stale := range []string{
		"for i in 0..4",
		"value.blob[..6]",
		"value.label[..8]",
	} {
		if strings.Contains(write, stale) {
			t.Errorf("the writer still dumps the bound: %q", stale)
		}
	}
	save := rustFn(body, "probe_fixed_save")
	if save == "" {
		t.Fatal("probe_fixed_save was not emitted")
	}
	if !strings.Contains(save, "probe_fixed_write_body") {
		t.Error("the save does not call the one writer; identity and compiled share it")
	}
	if strings.Contains(save, "if identity") || strings.Contains(save, "if !identity") {
		t.Error("the save still branches on identity; there is one writer for both paths")
	}
}

// rustDefaultImpl is the source of one generated `impl Default`, from the
// impl header to the matching close brace.
func rustDefaultImpl(body, typeName string) string {
	sig := "impl Default for " + typeName + " {"
	i := strings.Index(body, sig)
	if i < 0 {
		return ""
	}
	n := 0
	for j := i; j < len(body); j++ {
		switch body[j] {
		case '{':
			n++
		case '}':
			n--
			if n == 0 {
				return body[i : j+1]
			}
		}
	}
	return body[i:]
}

// A NAMED string(N) DEFAULT LIVES IN CONSTRUCTED STORAGE, not only in the
// prefill and not only as a used length. FX1's `label string(8) = "fx"` is
// the leftover: C++ writes `char label[8 + 1] = "fx"`, and Rust used to
// emit `core::mem::zeroed()` — two claimed bytes of NULs. Prefill already
// carries 0x66, 0x78; this is Default, not dest-row, not identity fill.
func TestNamedStringDefaultLivesInStorage(t *testing.T) {
	out := generate(t, `package probe

fixed table Fx1
{
    label string(8) = "fx"
}
`)
	records := string(out["probe_records.rs"])
	def := rustDefaultImpl(records, "Fx1Row")
	if def == "" {
		t.Fatal("impl Default for Fx1Row was not emitted")
	}
	for _, want := range []string{
		`copy_from_slice(b"fx")`,
		"label_length = 2",
		"core::mem::zeroed()",
		"let mut value: Self",
	} {
		if !strings.Contains(def, want) {
			t.Errorf("constructed storage is missing %q:\n%s", want, def)
		}
	}

	fixed := string(out["probe_fixed.rs"])
	if !strings.Contains(fixed, "0x66") || !strings.Contains(fixed, "0x78") {
		t.Error("prefill lost the named default bytes; do not double-write, do not drop")
	}
	scatter := rustFn(fixed, "fx1_fixed_scatter")
	if scatter == "" {
		t.Fatal("fx1_fixed_scatter was not emitted")
	}
	if strings.Contains(scatter, `b"fx"`) {
		t.Error("dest-row writes the named default; the hole is Default, not scatter")
	}
	load := rustFn(fixed, "fx1_fixed_load")
	if load == "" {
		t.Fatal("fx1_fixed_load was not emitted")
	}
	i := strings.Index(load, "for k in 0..n")
	if i < 0 {
		t.Fatal("the load has no record loop")
	}
	if strings.Contains(load[i:], "identity") {
		t.Error("the load loop still branches on identity; an empty hole list is what skips work")
	}
}
