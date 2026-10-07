package darttable

// THE DART LEG'S FIXED-TABLE definition-hash AND fixed-closure ROWS
// (docs/roadmap.sexp node `fixed-tables`, rows definition-hash and
// fixed-closure). Each subtest is one roadmap task id and asserts the clause
// its own title states. Law: docs/FIXED-FORM-ALGORITHM.md §1 (the layout and
// its hash), §5.2 (THE HASH, THE DEFINITIONS DIGEST, §5.9 #47/#48),
// docs/SPEC-TABLES.md §3.4, ROADMAP.md "NEW Fixed Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. A clause an existing test of this leg
// already holds is named by that test in the card's verdicts and is not
// repeated here.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Fnv1a64 is fnv1a64 over a run of bytes — the reference's own arithmetic
// (docs/FIXED-FORM-ALGORITHM.md §1: `h := 0xcbf29ce484222325; for each byte v:
// h ^= v; h *= 0x100000001b3`), spelled here so an assertion can hold the
// NUMBER to a hand-computed walk and not to the function under test.
func t03Fnv1a64(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, v := range b {
		h ^= uint64(v)
		h *= 0x100000001b3
	}
	return h
}

// t03Emitted compiles a schema and answers the hash constant this leg
// PUBLISHED for its table T, read out of the generated source rather than
// recomputed, plus the layout bytes the leg's own walk lays down — so an
// assertion is over the leg's own output and not over a shared helper's.
func t03Emitted(t *testing.T, src string) (hash uint64, layout []byte, text string) {
	t.Helper()
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	w := fixedWalkRoot(st)
	layout = fixedLayoutBytes(w.entries)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	text = t02Source(t, files)
	const marker = "const int tFixedHash = "
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no tFixedHash")
	}
	rest := text[at+len(marker):]
	end := strings.IndexByte(rest, ';')
	if end < 0 {
		t.Fatalf("the published tFixedHash has no end")
	}
	v, err := strconv.ParseUint(strings.TrimSpace(rest[:end]), 0, 64)
	if err != nil {
		t.Fatalf("the published hash %q is not a number: %v", rest[:end], err)
	}
	return v, layout, text
}

func TestFixedRoadmapT03Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/R4", t03R4},
		{"dart/R5", t03R5},
		{"dart/W11", t03W11},
		{"dart/W12", t03W12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03R4: R4 "the hash is fnv1a64 over the layout bytes then DIGEST(T), the
// digest computed at the hash site from the schema; a runtime never derives a
// hash from layout bytes it holds". §5.2 THE HASH: "The digest is computed
// AT THE HASH SITE, from the schema — the reference's signature is
// `ir.TableFixedLayoutHash(layout, st)` and there is no digest argument for a
// leg to omit", and §5.9 #47: a runtime "NEVER computes a hash from layout
// bytes it holds ... A hash a runtime computed itself is a hash of the layout
// BYTES ALONE".
func t03R4(t *testing.T) {
	const src = `package probe

fixed table T
{
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 100
    tail uint32 = 2
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no digest fact, so the digest clause cannot bite")
	}

	// (1) THE HASH IS fnv1a64 OVER THE LAYOUT BYTES THEN DIGEST(T): the number
	// is the walk over the concatenation, which is the same walk because FNV
	// is sequential (§5.2's own note).
	withDigest := t03Fnv1a64(append(append([]byte{}, layout...), digest...))
	if got := ir.TableFixedLayoutHash(layout, st); got != withDigest {
		t.Errorf("R4: TableFixedLayoutHash = 0x%016x, want fnv1a64(layout || digest) = 0x%016x", got, withDigest)
	}
	// (2) THE DIGEST COMPUTED AT THE HASH SITE FROM THE SCHEMA: the constant
	// this leg publishes is that number — and NOT the layout's alone, which is
	// the number "a leg that forgot the second argument, or passed an empty
	// one, would publish" (§5.2).
	alone := t03Fnv1a64(layout)
	if alone == withDigest {
		t.Fatal("R4: the fixture's digest does not move the hash, so the hash-site clause cannot bite")
	}
	hash, _, text := t03Emitted(t, src)
	if hash != withDigest {
		t.Errorf("R4: the leg published hash 0x%016x, want the digest-folded 0x%016x — the digest is computed at the hash site from the schema", hash, withDigest)
	}
	if strings.Contains(text, fmt.Sprintf("0x%x", alone)) {
		t.Errorf("R4: the leg published the layout-alone hash 0x%016x — a runtime never derives one", alone)
	}
	// (3) A RUNTIME NEVER DERIVES A HASH from layout bytes it holds: the one
	// fnv1a64 in the generated library is hashOf's own definition and nothing
	// calls it; the load's own comparison against the handed constants is held
	// by dart/R7/static in fixedroadmap_t01_test.go.
	if n := strings.Count(text, "hashOf("); n != 1 {
		t.Errorf("R4: the generated library carries %d mentions of hashOf, want exactly its one definition — a runtime never derives a hash from layout bytes it holds", n)
	}
	if i := strings.Index(text, "hashOf("); i >= 0 && !strings.HasSuffix(text[:i], "static int ") {
		t.Errorf("R4: hashOf is called somewhere in the generated library — a runtime never derives a hash from layout bytes it holds")
	}
}

// t03R5: R5 "the digest carries every range, every resolution (tag 'Q') and
// every reader limit (tag 'L'), and a flags type deduped by name, once".
// §5.2 THE DEFINITIONS DIGEST: "Without it `int32 | 0..100` and `| 0..200`
// hash identically and an old reader cannot refuse the widening", the 'Q' row
// "is present for every float range", the 'L' row "stays empty until a
// table-declared limit exists", and "Structs, flags and unions share one
// `seen` map keyed by bare name".
func t03R5(t *testing.T) {
	// (1) EVERY RANGE: two schemas whose layouts are byte-equal and whose
	// ranges differ — the leg's own published hash must move, or "an old
	// reader cannot refuse the widening".
	narrow := `package probe

fixed table T
{
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 100
    tail uint32 = 2
}
`
	wide := `package probe

fixed table T
{
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 200
    tail uint32 = 2
}
`
	hNarrow, lNarrow, _ := t03Emitted(t, narrow)
	hWide, lWide, _ := t03Emitted(t, wide)
	if !bytes.Equal(lNarrow, lWide) {
		t.Fatal("R5: the range fixture moved the layout bytes too, so the clause cannot bite on the digest alone")
	}
	if hNarrow == hWide {
		t.Errorf("R5: a range that widened (0..100 -> 0..200) left the leg's hash at 0x%016x — every range is carried", hNarrow)
	}

	// (2) EVERY RESOLUTION (tag 'Q'): the same pair over a compressed float's
	// step, which "rides as the float32 in this form ... so the step is
	// nowhere in the layout bytes" — only the digest can move the hash.
	coarse := `package probe

fixed table T
{
    lead uint32 = 1
    aim  float32 | min = -1, max = 1, resolution = 0.1
    tail uint32 = 2
}
`
	fine := `package probe

fixed table T
{
    lead uint32 = 1
    aim  float32 | min = -1, max = 1, resolution = 0.01
    tail uint32 = 2
}
`
	hCoarse, lCoarse, _ := t03Emitted(t, coarse)
	hFine, lFine, _ := t03Emitted(t, fine)
	if !bytes.Equal(lCoarse, lFine) {
		t.Fatal("R5: the resolution fixture moved the layout bytes too, so the clause cannot bite on the digest alone")
	}
	if hCoarse == hFine {
		t.Errorf("R5: a resolution that refined (0.1 -> 0.01) left the leg's hash at 0x%016x — every resolution is carried", hCoarse)
	}

	// (3) EVERY READER LIMIT (tag 'L') — RESERVED: "no table spelling of a
	// reader-side limit exists ... The row stays empty until a table-declared
	// limit exists", so a digest carrying every OTHER row carries no 'L'.
	carrying := `package probe

flags Caps { Jump, Crouch }

fixed table T
{
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 100
    aim  float32 | min = -1, max = 1, resolution = 0.1
    tag  bits(8)
    pos  fixed(32, 32) | min = 0, max = 100
    caps Caps
    tail uint32 = 2
}
`
	u := unitFrom(t, carrying)
	st := findTable(t, u, "T")
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R5: the fixture carries no digest fact, so the 'L' clause cannot bite")
	}
	for _, want := range []byte{'R', 'Q', 'B', 'X', 'F'} {
		if !bytes.Contains(digest, []byte{want}) {
			t.Errorf("R5: the fixture's digest carries no %q row, so the reserved-'L' check is not looking at a full digest", want)
		}
	}
	if i := bytes.IndexByte(digest, 'L'); i >= 0 {
		t.Errorf("R5: the digest carries an 'L' row at %d with no table spelling of a reader-side limit — the row stays empty until one exists", i)
	}

	// (4) A FLAGS TYPE DEDUPED BY NAME, ONCE: "Structs, flags and unions share
	// one `seen` map keyed by bare name", so a flags type named by two fields
	// spends one 'F' row — the digest a table naming it once spells.
	twice := `package probe

flags Caps { Jump, Crouch }

fixed table T
{
    caps  Caps
    other Caps
}
`
	once := `package probe

flags Caps { Jump, Crouch }

fixed table T
{
    caps Caps
}
`
	dTwice := ir.TableFixedDefinitionsDigest(findTable(t, unitFrom(t, twice), "T"))
	dOnce := ir.TableFixedDefinitionsDigest(findTable(t, unitFrom(t, once), "T"))
	if !bytes.Equal(dTwice, dOnce) {
		t.Errorf("R5: a flags type named by two fields is re-emitted — the digest is deduped by name, once")
	}
	if n := bytes.Count(dTwice, []byte{'F'}); n != 1 {
		t.Errorf("R5: the digest carries %d 'F' rows for one flags type, want 1 — deduped by name, once", n)
	}
}

// t03W11: W11 "bytes(N) is layout kind 14". §1: "`bytes(N)` is walked as an
// ARRAY OF `u8` — kind `14` with one synthetic child at kind `6`, size `1` —
// never as a text kind; only `string(N)` (`12`) and `wstring(N)` (`33`) are
// text kinds in a layout."
func t03W11(t *testing.T) {
	const src = `package probe

fixed table T
{
    blob bytes(6)
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	w := fixedWalkRoot(st)
	at := -1
	for i, e := range w.entries {
		if e.note == "blob" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("W11: no layout entry for the bytes(N) field")
	}
	// THE WALK'S OWN ENTRY: kind 14, one child at kind 6, size 1.
	e := w.entries[at]
	if e.kind != ir.TableKindArray {
		t.Errorf("W11: the bytes(N) entry's kind is %d, want 14 — bytes(N) is layout kind 14", e.kind)
	}
	if e.children != 1 {
		t.Errorf("W11: the bytes(N) entry has %d children, want 1 — the synthetic u8", e.children)
	}
	child := w.entries[at+1]
	if child.kind != ir.TableKindU8 || child.size != 1 || child.children != 0 {
		t.Errorf("W11: the bytes(N) entry's child is (kind %d, size %d, children %d), want the synthetic u8 (6, 1, 0)", child.kind, child.size, child.children)
	}
	// THE BYTES AS THEY RIDE: the kind byte at 4 + 17*i + 8 is 14 — never 12
	// or 33, the text kinds.
	layout := fixedLayoutBytes(w.entries)
	kindAt := fixedHeaderBytes + int64(at)*fixedEntryBytes + 8
	if got := layout[kindAt]; got != byte(ir.TableKindArray) {
		t.Errorf("W11: the layout's kind byte for bytes(N) is %d, want 14 — never a text kind (12 or 33)", got)
	}
	childAt := fixedHeaderBytes + int64(at+1)*fixedEntryBytes + 8
	if got := layout[childAt]; got != byte(ir.TableKindU8) {
		t.Errorf("W11: the layout's kind byte for the synthetic child is %d, want 6 (u8)", got)
	}
	// AND THE LEG PUBLISHES those bytes: the emitted layout list carries the
	// walk's own kind bytes, so a port that remapped the kind moves the file
	// it hands a consumer.
	_, mine, text := t03Emitted(t, src)
	if !strings.Contains(text, "final Uint8List tFixedLayout = Uint8List.fromList(const <int>[") {
		t.Errorf("W11: the generated source publishes no tFixedLayout")
	}
	if len(mine) != len(layout) {
		t.Errorf("W11: the leg's emitted layout is %d bytes, want the walk's %d", len(mine), len(layout))
	}
}

// t03W12: W12 "hash includes the 4-byte count". §1: "The hash is
// `h := 0xcbf29ce484222325; for each byte v: h ^= v; h *= 0x100000001b3`
// over the layout's bytes as written, **the 4-byte count included**, and then
// over the DEFINITIONS DIGEST".
func t03W12(t *testing.T) {
	const src = `package probe

fixed table T
{
    a int32
    b int32
    c int32
}
`
	u := unitFrom(t, src)
	st := findTable(t, u, "T")
	w := fixedWalkRoot(st)
	if len(w.entries) < 2 {
		t.Fatal("W12: the fixture needs more than one layout entry, or the count cannot bite")
	}
	layout := fixedLayoutBytes(w.entries)
	// THE FIRST FOUR BYTES ARE THE ENTRY COUNT, little-endian.
	if got := binary.LittleEndian.Uint32(layout[:fixedCountBytes]); got != uint32(len(w.entries)) {
		t.Errorf("W12: the layout's first four bytes are %d, want the entry count %d", got, len(w.entries))
	}
	digest := ir.TableFixedDefinitionsDigest(st)
	withCount := t03Fnv1a64(append(append([]byte{}, layout...), digest...))
	bare := t03Fnv1a64(append(append([]byte{}, layout[fixedCountBytes:]...), digest...))
	if withCount == bare {
		t.Fatal("W12: the fixture cannot tell the count's absence, so the clause cannot bite")
	}
	// THE HASH COVERS THE COUNT: the shared function's number is the walk over
	// the bytes as written — count first — and the leg publishes that number.
	if got := ir.TableFixedLayoutHash(layout, st); got != withCount {
		t.Errorf("W12: TableFixedLayoutHash = 0x%016x, want fnv1a64(count || entries || digest) = 0x%016x", got, withCount)
	}
	hash, _, text := t03Emitted(t, src)
	if hash != withCount {
		t.Errorf("W12: the leg published hash 0x%016x, want the count-included 0x%016x", hash, withCount)
	}
	if strings.Contains(text, fmt.Sprintf("0x%x", bare)) {
		t.Errorf("W12: the leg published the count-less hash 0x%016x — the 4-byte count is included", bare)
	}
}
