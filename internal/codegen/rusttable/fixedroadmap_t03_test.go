package rusttable

// THE ROADMAP'S DEFINITION-HASH AND FIXED-CLOSURE TASKS ON THE RUST LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §1's
// layout, §5.2's THE HASH and THE DEFINITIONS DIGEST, §5.5's closure;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash";
// docs/SPEC-TABLES.md §3.4). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs.
//
// rust/R4 [weak]: the hash is fnv1a64 over the layout bytes then DIGEST(T),
// the digest computed at the hash site from the schema; a runtime never
// derives a hash from layout bytes it holds.
//
// rust/R5 [owed]: the digest carries every range, every resolution (tag 'Q')
// and every reader limit (tag 'L'), and a flags type deduped by name, once.
//
// rust/W11 [owed]: bytes(N) is layout kind 14.
//
// rust/W12 [weak]: hash includes the 4-byte count.

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t03Table resolves one declared table of a checked unit by name.
func t03Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the unit declares no table %s", name)
	}
	return st
}

// t03Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t03Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t03Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t03Fn(text, name string) string {
	at := strings.Index(text, "pub fn "+name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t03ByteArray reads one emitted `pub const NAME: [u8; N] = [ ... ];`.
func t03ByteArray(t *testing.T, text, name string) []byte {
	t.Helper()
	at := strings.Index(text, "pub const "+name+": [u8; ")
	if at < 0 {
		t.Fatalf("%s is not emitted", name)
	}
	rest := text[at:]
	open := strings.Index(rest, "= [")
	if open < 0 {
		t.Fatalf("%s has no array literal", name)
	}
	rest = rest[open+3:]
	closeAt := strings.Index(rest, "];")
	if closeAt < 0 {
		t.Fatalf("%s's array literal does not close", name)
	}
	var out []byte
	for _, f := range strings.Fields(rest[:closeAt]) {
		f = strings.TrimSuffix(f, ",")
		if f == "" {
			continue
		}
		n, err := strconv.ParseUint(f, 0, 8)
		if err != nil {
			t.Fatalf("%s: byte %q: %v", name, f, err)
		}
		out = append(out, byte(n))
	}
	return out
}

// t03CheckErrs parses and checks one source and answers the checker's errors,
// so a closure refusal can be read by name rather than caught as a fixture bug.
func t03CheckErrs(t *testing.T, src string) []error {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return []error{perrs[0]}
	}
	_, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	return cerrs
}

// t03Has is a required substring of the emitted source, named by rule.
func t03Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// ---- rust/R4 -----------------------------------------------------------------

// t03R4 holds rust/R4 to the two clauses that make the hash a wire identity:
// the compiler's number is fnv1a64 over the layout bytes and then the digest
// computed AT THE HASH SITE from the schema (§5.2's HASH(layout_bytes, T), "the
// digest is computed AT THE HASH SITE, from the schema"), and the emitted load
// takes the file's header hash as given and never derives one from the layout
// bytes it holds (§5.2, §5.3).
func t03R4(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t03Table(t, u, "T")
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no digest")
	}
	h := uint64(0xcbf29ce484222325)
	for _, b := range append(append([]byte(nil), layout...), digest...) {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	if got := ir.TableFixedLayoutHash(layout, st); got != h {
		t.Errorf("R4: hash = %#x, want fnv1a64(layout || digest) = %#x", got, h)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R4: Generate: %v", err)
	}
	text := t03Fixed(t, files)
	t03Has(t, text, fmt.Sprintf("pub const T_FIXED_HASH: u64 = 0x%016x;", h), "R4 the emitted hash constant")
	load := t03Fn(text, "t_fixed_load")
	if load == "" {
		t.Fatal("R4: no emitted t_fixed_load function found")
	}
	t03Has(t, load, "data[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]", "R4 the file's hash is read from the header")
	if strings.Contains(load, "table_fixed_hash(") || strings.Contains(load, "0x0000_0100_0000_01b3") {
		t.Error("R4: the load path derives a hash from layout bytes it holds")
	}
}

// ---- rust/R5 -----------------------------------------------------------------

// t03R5 holds rust/R5 to §5.2's digest contract, read positionally by tag: a
// range is 'R' with its two bounds, a float range's resolution is 'Q' straight
// after it (an uncompressed range carries the zero), a reader-side limit is 'L'
// and is RESERVED because no table spelling exists, and a flags type is 'F'
// once by bare name however many fields name it.
func t03R5(t *testing.T) {
	ranged := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`), "T")
	rangedDigest := ir.TableFixedDefinitionsDigest(ranged)
	if d := rangedDigest; len(d) != 17 || d[0] != 'R' || d[1] != 0xfb {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", d)
	}

	compressed := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`), "T")
	compressedDigest := ir.TableFixedDefinitionsDigest(compressed)
	if d := compressedDigest; len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", d)
	}

	flagsOnce := t03Table(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
}
`), "T")
	once := ir.TableFixedDefinitionsDigest(flagsOnce)
	if len(once) != 23 || once[0] != 'F' || once[5] != 'f' || once[14] != 'f' {
		t.Errorf("R5: a flags digest = % x, want 'F' count, then 'f' name per flag", once)
	}
	if n := binary.LittleEndian.Uint32(once[1:5]); n != 2 {
		t.Errorf("R5: the flags row names %d wire bits, want 2", n)
	}

	flagsTwice := t03Table(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
    n Marks
}
`), "T")
	twiceDigest := ir.TableFixedDefinitionsDigest(flagsTwice)
	if got := twiceDigest; string(got) != string(once) {
		t.Errorf("R5: the flags type is not deduped by name: once % x, twice % x", once, got)
	}

	// §5.2 reserves 'L' because no table spelling of a reader-side limit exists:
	// no member the walk can reach declares one. Prove that premise, so the
	// empty row is the page's reservation and not a coincidence of these three
	// fixtures.
	for _, rt := range []reflect.Type{
		reflect.TypeFor[ir.Field](),
		reflect.TypeFor[ir.Struct](),
		reflect.TypeFor[ir.Unit](),
	} {
		for sf := range rt.Fields() {
			if strings.Contains(strings.ToLower(sf.Name), "limit") {
				t.Fatalf("R5: the IR declares %s.%s, a table-declared reader-side limit; §5.2 reserves 'L' only until one exists, and the digest must then carry it", rt.Name(), sf.Name)
			}
		}
	}
	for _, d := range [][]byte{rangedDigest, compressedDigest, once, twiceDigest} {
		for _, b := range d {
			if b == 'L' {
				t.Error("R5: the digest carries a reader-limit row no table spelling produces")
			}
		}
	}
}

// ---- rust/W11 ----------------------------------------------------------------

// t03W11 holds rust/W11 to §1's kind table: `bytes(N)` is walked as an ARRAY of
// `u8` (kind 14 with one synthetic child at kind 6, size 1), and the byte the
// emitted block carries for the field is the same number.
func t03W11(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	st := t03Table(t, u, "T")
	w := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(w)
	found := false
	for i, e := range w {
		if e.Note != "blob" {
			continue
		}
		found = true
		if e.Kind != ir.TableKindArray {
			t.Errorf("W11: bytes(6) walks as kind %d, want %d (array)", e.Kind, ir.TableKindArray)
		}
		if got := int(layout[4+i*17+8]); got != 14 {
			t.Errorf("W11: the bytes(6) layout kind byte = %d, want 14", got)
		}
	}
	if !found {
		t.Fatal("W11: the walk has no blob entry")
	}
	if ir.TableKindArray != 14 {
		t.Fatalf("W11: TableKindArray = %d, want 14", ir.TableKindArray)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W11: Generate: %v", err)
	}
	block := t03ByteArray(t, t03Fixed(t, files), "T_FIXED_BLOCK")
	for i, e := range w {
		if e.Note != "blob" {
			continue
		}
		if got := block[4+i*17+8]; got != 14 {
			t.Errorf("W11: the emitted block's bytes(6) kind byte = %d, want 14", got)
		}
	}
}

// ---- rust/W12 ----------------------------------------------------------------

// t03W12 holds rust/W12 to §1: the layout opens with the u32 entry count, and
// the hash covers it — a count that moves moves the hash.
func t03W12(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	st := t03Table(t, u, "T")
	w := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(w)
	if got := binary.LittleEndian.Uint32(layout[:4]); got != uint32(len(w)) {
		t.Errorf("W12: the layout's count = %d, want %d entries", got, len(w))
	}
	mutated := append([]byte(nil), layout...)
	binary.LittleEndian.PutUint32(mutated[:4], uint32(len(w))+1)
	if ir.TableFixedLayoutHash(layout, st) == ir.TableFixedLayoutHash(mutated, st) {
		t.Error("W12: the hash ignores the layout's 4-byte entry count")
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT03Plans(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/R4", check: t03R4},
		{id: "rust/R5", check: t03R5},
		{id: "rust/W11", check: t03W11},
		{id: "rust/W12", check: t03W12},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
