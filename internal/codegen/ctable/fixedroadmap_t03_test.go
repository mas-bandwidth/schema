package ctable

// THE ROADMAP'S DEFINITION-HASH TASKS ON THE C LEG (docs/roadmap.sexp node
// `fixed-tables`, row definition-hash; docs/FIXED-FORM-ALGORITHM.md §1's
// layout, §5.2's THE HASH and THE DEFINITIONS DIGEST;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash";
// docs/SPEC-TABLES.md §3.4; ROADMAP.md "NEW Fixed Tables"). One subtest per
// task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus.
//
//	c/R4  [weak]: the hash is fnv1a64 over the layout bytes then DIGEST(T), the
//	              digest computed at the hash site from the schema; a runtime
//	              never derives a hash from layout bytes it holds.
//	c/R5  [owed]: the digest carries every range, every resolution (tag 'Q') and
//	              every reader limit (tag 'L'), and a flags type deduped by name,
//	              once.
//	c/W11 [owed]: bytes(N) is layout kind 14.
//	c/W12 [owed]: hash includes the 4-byte count.

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Fnv1a64 is fnv1a64 over a run of bytes — the reference's own arithmetic
// (docs/FIXED-FORM-ALGORITHM.md §1: `h := 0xcbf29ce484222325; for each byte v:
// h ^= v; h *= 0x100000001b3`), spelled here so an assertion holds the NUMBER
// to a hand-computed walk and not to the function under test.
func t03Fnv1a64(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, v := range b {
		h ^= uint64(v)
		h *= 0x100000001b3
	}
	return h
}

// t03Header is the one <Base>Table.h this leg emits for a unit, as text.
func t03Header(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatalf("no Table.h was emitted")
	return ""
}

// t03Table finds one declared table by name.
func t03Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the unit declares no table %s", name)
	}
	return st
}

// t03Hash reads the hash constant this leg PUBLISHED for a table, out of the
// generated source rather than recomputed — so an assertion is over the leg's
// own output and not over a shared helper's.
func t03Hash(t *testing.T, text, snake string) uint64 {
	t.Helper()
	marker := snake + "_fixed_hash = 0x"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s_fixed_hash", snake)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "ull")
	if end < 0 {
		t.Fatalf("the published %s_fixed_hash has no end", snake)
	}
	v, err := strconv.ParseUint(strings.TrimSpace(rest[:end]), 16, 64)
	if err != nil {
		t.Fatalf("the published hash %q is not a number: %v", rest[:end], err)
	}
	return v
}

// t03Layout reads the layout bytes this leg published as `<snake>_fixed_layout`.
func t03Layout(t *testing.T, text, snake string) []byte {
	t.Helper()
	marker := snake + "_fixed_layout[] = {"
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the generated source publishes no %s_fixed_layout", snake)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "};")
	if end < 0 {
		t.Fatalf("the published %s_fixed_layout does not close", snake)
	}
	var out []byte
	for _, f := range strings.Split(rest[:end], ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("layout byte %q: %v", f, err)
		}
		out = append(out, byte(n))
	}
	return out
}

func TestFixedRoadmapT03Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/R4", t03R4},
		{"c/R5", t03R5},
		{"c/W11", t03W11},
		{"c/W12", t03W12},
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
// hash from layout bytes it holds". §5.2 THE HASH: "The digest is computed AT
// THE HASH SITE, from the schema — the reference's signature is
// `ir.TableFixedLayoutHash(layout, st)` and there is no digest argument for a
// leg to omit", and §5.9 #47: a runtime "NEVER computes a hash from layout
// bytes it holds". §5.2's HASH(layout_bytes, T) note: "the same walk because
// FNV is sequential".
func t03R4(t *testing.T) {
	const src = `package probe

fixed table T
{
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 100
    y    float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
    tail uint32 = 2
}
`
	u := cUnitOf(t, src)
	st := t03Table(t, u, "T")
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no digest fact, so the digest clause cannot bite")
	}
	withDigest := t03Fnv1a64(append(append([]byte{}, layout...), digest...))
	if got := ir.TableFixedLayoutHash(layout, st); got != withDigest {
		t.Errorf("R4: TableFixedLayoutHash = 0x%016x, want fnv1a64(layout || digest) = 0x%016x", got, withDigest)
	}
	alone := t03Fnv1a64(layout)
	if alone == withDigest {
		t.Fatal("R4: the fixture's digest does not move the hash, so the hash-site clause cannot bite")
	}
	text := t03Header(t, u)
	snake := ir.RustSnake(st.Name)
	if hash := t03Hash(t, text, snake); hash != withDigest {
		t.Errorf("R4: the leg published hash 0x%016x, want the digest-folded 0x%016x — the digest is computed at the hash site from the schema", hash, withDigest)
	}
	// (3) A RUNTIME NEVER DERIVES A HASH from layout bytes it holds: the one
	// fnv1a64 in the emitted unit is hash_of's definition and nothing calls it.
	if n := strings.Count(text, "table_fixed_hash_of"); n != 1 {
		t.Errorf("R4: the emitted unit carries %d mentions of table_fixed_hash_of, want exactly its one definition — a runtime never derives a hash from layout bytes it holds", n)
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
	// ranges differ — the leg's own published hash must move.
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
	nU, wU := cUnitOf(t, narrow), cUnitOf(t, wide)
	nSt, wSt := t03Table(t, nU, "T"), t03Table(t, wU, "T")
	nLay := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(nSt))
	wLay := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(wSt))
	if !bytes.Equal(nLay, wLay) {
		t.Fatal("R5: the range fixture moved the layout bytes too, so the clause cannot bite on the digest alone")
	}
	hNarrow := t03Hash(t, t03Header(t, nU), "t")
	hWide := t03Hash(t, t03Header(t, wU), "t")
	if hNarrow == hWide {
		t.Errorf("R5: a range that widened (0..100 -> 0..200) left the leg's hash at 0x%016x — every range is carried", hNarrow)
	}

	// (2) EVERY RESOLUTION (tag 'Q'): the step is nowhere in the layout bytes,
	// so only the digest can move the hash.
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
	cU, fU := cUnitOf(t, coarse), cUnitOf(t, fine)
	cLay := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(t03Table(t, cU, "T")))
	fLay := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(t03Table(t, fU, "T")))
	if !bytes.Equal(cLay, fLay) {
		t.Fatal("R5: the resolution fixture moved the layout bytes too, so the clause cannot bite on the digest alone")
	}
	hCoarse := t03Hash(t, t03Header(t, cU), "t")
	hFine := t03Hash(t, t03Header(t, fU), "t")
	if hCoarse == hFine {
		t.Errorf("R5: a resolution that refined (0.1 -> 0.01) left the leg's hash at 0x%016x — every resolution is carried", hCoarse)
	}

	// (3) EVERY READER LIMIT (tag 'L') — RESERVED: a digest carrying every
	// OTHER row carries no 'L'.
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
	digest := ir.TableFixedDefinitionsDigest(t03Table(t, cUnitOf(t, carrying), "T"))
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

	// (4) A FLAGS TYPE DEDUPED BY NAME, ONCE.
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
	dTwice := ir.TableFixedDefinitionsDigest(t03Table(t, cUnitOf(t, twice), "T"))
	dOnce := ir.TableFixedDefinitionsDigest(t03Table(t, cUnitOf(t, once), "T"))
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
	u := cUnitOf(t, src)
	st := t03Table(t, u, "T")
	w := ir.TableFixedWalkRoot(st)
	at := -1
	for i, e := range w {
		if e.Note == "blob" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("W11: no layout entry for the bytes(N) field")
	}
	if w[at].Kind != ir.TableKindArray {
		t.Errorf("W11: the bytes(N) entry's kind is %d, want %d — bytes(N) is layout kind 14", w[at].Kind, ir.TableKindArray)
	}
	if w[at].Children != 1 || at+1 >= len(w) {
		t.Fatalf("W11: the bytes(N) entry must have the one synthetic u8 child")
	}
	child := w[at+1]
	if child.Kind != ir.TableKindU8 || child.Size != 1 || child.Children != 0 {
		t.Errorf("W11: the bytes(N) entry's child is (kind %d, size %d, children %d), want the synthetic u8 (6, 1, 0)", child.Kind, child.Size, child.Children)
	}
	layout := ir.TableFixedLayoutBytes(w)
	if got := layout[4+at*17+8]; got != 14 {
		t.Errorf("W11: the layout's kind byte for bytes(N) = %d, want 14 — never a text kind (12 or 33)", got)
	}
	if got := layout[4+(at+1)*17+8]; got != 6 {
		t.Errorf("W11: the layout's kind byte for the synthetic child = %d, want 6 (u8)", got)
	}
	// AND THE LEG PUBLISHES those bytes.
	mine := t03Layout(t, t03Header(t, u), "t")
	if !bytes.Equal(mine, layout) {
		t.Errorf("W11: the leg's emitted layout is not the walk's own bytes")
	}
	if ir.TableKindArray != 14 {
		t.Fatalf("W11: TableKindArray = %d, want 14", ir.TableKindArray)
	}
}

// t03W12: W12 "hash includes the 4-byte count". §1: "The hash is
// `h := 0xcbf29ce484222325; for each byte v: h ^= v; h *= 0x100000001b3` over
// the layout's bytes as written, **the 4-byte count included**, and then over
// the DEFINITIONS DIGEST".
func t03W12(t *testing.T) {
	const src = `package probe

fixed table T
{
    a int32
    b int32
    c int32
}
`
	u := cUnitOf(t, src)
	st := t03Table(t, u, "T")
	w := ir.TableFixedWalkRoot(st)
	if len(w) < 2 {
		t.Fatal("W12: the fixture needs more than one layout entry, or the count cannot bite")
	}
	layout := ir.TableFixedLayoutBytes(w)
	if got := binary.LittleEndian.Uint32(layout[:4]); got != uint32(len(w)) {
		t.Errorf("W12: the layout's first four bytes are %d, want the entry count %d", got, len(w))
	}
	digest := ir.TableFixedDefinitionsDigest(st)
	withCount := t03Fnv1a64(append(append([]byte{}, layout...), digest...))
	bare := t03Fnv1a64(append(append([]byte{}, layout[4:]...), digest...))
	if withCount == bare {
		t.Fatal("W12: the fixture cannot tell the count's absence, so the clause cannot bite")
	}
	if got := ir.TableFixedLayoutHash(layout, st); got != withCount {
		t.Errorf("W12: TableFixedLayoutHash = 0x%016x, want fnv1a64(count || entries || digest) = 0x%016x", got, withCount)
	}
	if hash := t03Hash(t, t03Header(t, u), "t"); hash != withCount {
		t.Errorf("W12: the leg published hash 0x%016x, want the count-included 0x%016x", hash, withCount)
	}
}
