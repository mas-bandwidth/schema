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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
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
		{"dart/R6", t03R6},
		{"dart/R14", t03R14},
		{"dart/R22", t03R22},
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

// t03R6: R6 "a table past §3.4's 65536 ceiling is not a fixed-form root: the
// refusal names the table, no form is emitted, and no lineage entry is parsed
// for it even when the lock carries one". SPEC-TABLES §3.4: "65536 BYTES OF
// RECORD BODY: THE FORM IS NOT EMITTED, AND THE TABLE IS NAMED"; §5.9 #48:
// "Such a table is NOT a fixed-form root, so COMPILE consults no lineage for
// it and parses no entry of it, even when the LOCK carries one". The pin is
// the JavaScript leg's TestJSFixedNoFormMeansNoLineageToParse, whose hurt
// (#931) this row exists to keep off every other leg.
func t03R6(t *testing.T) {
	const src = `package probe

fixed table Small
{
    keep uint32 = 0
}

fixed table Huge
{
    payload bytes(70000)
}
`
	u := unitFrom(t, src)
	huge := findTable(t, u, "Huge")
	if n := fixedTypeBytes(huge); n <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture's body is %d bytes, which is not past the %d-byte ceiling", n, ir.TableFixedRecordMaxBytes)
	}

	// (1) NOT A FIXED-FORM ROOT — this leg's own roots and ir's agree.
	var legRoots []string
	for _, f := range u.Files {
		for _, st := range fixedRoots(f.Tables) {
			legRoots = append(legRoots, st.Name)
		}
	}
	if len(legRoots) != 1 || legRoots[0] != "Small" {
		t.Errorf("R6: a table past §3.4's ceiling is NOT a fixed-form root; this leg's roots are %v, want [Small]", legRoots)
	}
	var irRoots []string
	for _, st := range ir.TableFixedFormRoots(u) {
		irRoots = append(irRoots, st.Name)
	}
	if len(irRoots) != 1 || irRoots[0] != "Small" {
		t.Errorf("R6: ir.TableFixedFormRoots answers %v, want [Small]", irRoots)
	}

	// (2) THE REFUSAL NAMES THE TABLE.
	if reason := fixedRefusal(huge); reason == "" {
		t.Error("R6: a table past §3.4's ceiling is refused BY NAME, so the module can say which table and why")
	} else if !strings.Contains(reason, "65536") {
		t.Errorf("R6: the refusal names %q, which does not carry the ceiling it refuses by", reason)
	}

	// (3) NO FORM IS EMITTED, AND NO LINEAGE ENTRY IS PARSED even when the lock
	// carries one — handed in exactly as `lockfile.Lineage` hands the one a
	// lock holds, GENERATE MUST NOT REFUSE IT: it is not an entry of this
	// form (#48), and #8's "a bug in the lock, the BUILD FAILS" rules only on
	// an entry that is.
	hugeWalk := fixedWalkRoot(huge)
	hugeLayout := fixedLayoutBytes(hugeWalk.entries)
	overCeiling := FixedLineageEntry{
		Wire:   ir.TableFixedLayoutHash(hugeLayout, huge),
		Layout: hugeLayout,
		Record: fixedHashBytes + fixedTypeBytes(huge),
	}
	own, ok := FixedLineageOf(u, "Small")
	if !ok {
		t.Fatal("R6: no lineage entry for Small")
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{
		"Small": {own},
		"Huge":  {overCeiling},
	})
	if err != nil {
		t.Fatalf("R6: a lineage entry for a table with NO FORM must not fail the build: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("R6: the unit has a fixed root, so a module is written")
	}
	// AND NOTHING OF IT IS EMITTED: no known layout, no plan, no body size.
	// The line naming the refusal is the whole of what Huge gets.
	sawRefusalLine := false
	for name, body := range files {
		s := string(body)
		for _, bad := range []string{
			"hugeFixedBodyBytes", "hugeFixedRecordBytes", "hugeFixedLayout",
			"hugeFixedKnown", "hugeFixedIdentity", "hugeFixedWriteBody",
			"hugeFixedDecode", fixedDartHex(overCeiling.Wire),
		} {
			if strings.Contains(s, bad) {
				t.Errorf("R6: %s names %q — a table with no fixed form has no fixed surface and no known layout", name, bad)
			}
		}
		sawRefusalLine = sawRefusalLine || strings.Contains(s, "table Huge has NO FIXED FORM in Dart")
	}
	if !sawRefusalLine {
		t.Error("R6: NAMED, NEVER SILENT: a generated module says which table has no fixed form and why")
	}
	// the positive control: Small's own surface IS emitted, so the ceiling
	// refuses Huge alone and not the file it shares.
	sawSmall := false
	for _, body := range files {
		if strings.Contains(string(body), "smallFixedKnown") {
			sawSmall = true
		}
	}
	if !sawSmall {
		t.Error("R6: the ceiling refuses the past-bound table alone; Small keeps its form")
	}
}

// t03Put appends one seventeen-byte layout entry, the format §1 fixes: an id,
// a kind, a size and a child count, every number little-endian.
func t03Put(b []byte, id uint64, kind byte, size, children uint32) []byte {
	var e [17]byte
	binary.LittleEndian.PutUint64(e[0:8], id)
	e[8] = kind
	binary.LittleEndian.PutUint32(e[9:13], size)
	binary.LittleEndian.PutUint32(e[13:17], children)
	return append(b, e[:]...)
}

// t03R14: R14 "layout_record_too_large for an entry reaching past the writer's
// declared record, not only for the 65536 bound and a zero root".
// docs/FIXED-FORM-ALGORITHM.md §5.2's PLAN: "record := x.root.size — EVERY
// entry is bounded by this, and by nothing the file said ... if an entry
// reached past `record`: REFUSE layout_record_too_large". The layout is
// hand-built out of THIS build's own field ids, read out of the walk rather
// than spelled as hashes, and it passes every rule of §1.1 — the refusal is
// the BOUND. It is the Go twin of the reference's record_bound_case() and
// gotable's TestFixedFormRecordBound (dart/W10's shared leaf).
func t03R14(t *testing.T) {
	const schema = `package probe

fixed table Host
{
    keep  uint32 = 7
    label string(8) = "hi"
    blob  bytes(6)
}
`
	u := unitFrom(t, schema)
	st := findTable(t, u, "Host")
	w := fixedWalkRoot(st)

	// THE IDS ARE READ OUT OF THIS BUILD'S OWN LAYOUT, not spelled as hashes,
	// so the case cannot quietly stop naming the fields it means.
	var rootID, keepID, blobID, blobElemID uint64
	for i, e := range w.entries {
		switch {
		case i == 0:
			rootID = e.id
		case e.kind == 8 && e.size == 4 && keepID == 0:
			keepID = e.id // keep, a uint32
		case e.kind == 14 && i+1 < len(w.entries) &&
			w.entries[i+1].kind == 6 && w.entries[i+1].size == 1:
			blobID, blobElemID = e.id, w.entries[i+1].id // blob, bytes(6): an array of u8
		}
	}
	if rootID == 0 || keepID == 0 || blobID == 0 {
		t.Fatal("R14: the ids this case names are the ids this build's own layout carries")
	}

	// THE HOSTILE LAYOUT: the record body is FOUR bytes (keep fills it), then
	// an array of size ZERO — a valid bare array of no elements, which §1.1
	// admits (`size % elem == 0`). This reader's own row takes `bytes(N)` as
	// a COUNTED array, so the count entry reads at 4..8 of a record that ends
	// at 4: an entry reaching past the writer's declared record.
	hostile := binary.LittleEndian.AppendUint32(nil, 4)
	hostile = t03Put(hostile, rootID, 13, 4+0, 2)
	hostile = t03Put(hostile, keepID, 8, 4, 0)
	hostile = t03Put(hostile, blobID, 14, 0, 1)
	hostile = t03Put(hostile, blobElemID, 6, 1, 0)
	hostileWire := ir.TableFixedLayoutHash(hostile, st)

	// AN OLDER LEGAL ENTRY, the positive control: the same table with a
	// narrower label — a real lineage widening whose plan must still compile, so
	// the bound refuses the overreach and not the peer.
	older := unitFrom(t, `package probe

fixed table Host
{
    keep  uint32 = 7
    label string(4) = "hi"
    blob  bytes(6)
}
`)
	olderEntry, ok := FixedLineageOf(older, "Host")
	if !ok {
		t.Fatal("R14: no lineage entry for the older Host")
	}

	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Host": {
		{Wire: hostileWire, Layout: hostile, Record: fixedHashBytes + 4},
		olderEntry,
	}})
	if err != nil {
		t.Fatalf("R14: GenerateLineage: %v", err)
	}

	// THE STATIC HALF: the generated runtime carries the rule at all.
	text := t02Source(t, files)
	for _, want := range []string{
		"ONE ENTRY PAST THE WRITER'S RECORD REFUSES THE PLAN WHOLE",
		"TableFixedRefusal.layoutRecordTooLarge",
	} {
		t02Has(t, text, want, "R14 the runtime carries the record bound")
	}

	// THE RUN HALF: a probe over the generated library, run with the leg's
	// own toolchain (§5.9 #18) — skipped with the SDK absent, the way every
	// probe in this package is, and a failure under SCHEMA_REQUIRE_CORPUS.
	dartBin := dartBinary(t)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var list strings.Builder
	for _, b := range hostile {
		fmt.Fprintf(&list, "  %d,\n", b)
	}
	probe := fmt.Sprintf(`// GENERATED BY internal/codegen/darttable/fixedroadmap_t03_test.go — the
// dart/R14 probe, one hostile lineage entry handed to the build the way a
// lock would hand it (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #18).
import 'dart:io';
import 'dart:typed_data';

import './ProbeFixed.dart' as t;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

void main() {
  // 1. THE HOSTILE LAYOUT PASSES THE SEVEN RULES of §1.1, so the refusal
  //    below is the record bound and not a malformation.
  final hostile = Uint8List.fromList(const <int>[
%[1]s  ]);
  final lay = t.TableFixedLayout();
  final parses = lay.parse(ByteData.sublistView(hostile), 0, hostile.length);
  check(parses,
      'R14: the hostile layout passes every rule of §1.1: ${t.TableFixedRefusal.name(lay.refusal)}');

  // 2. THE PLAN IT RESOLVES TO refuses layout_record_too_large, WHOLE —
  //    never partly compiled.
  final plans = t.hostFixedLineagePlans;
  check(plans[0].why == t.TableFixedRefusal.layoutRecordTooLarge,
      'R14: the hostile entry owes layout_record_too_large, got '
      '${t.TableFixedRefusal.name(plans[0].why)}');
  check(plans[0].count == 0,
      'R14: the plan is refused WHOLE, never partly compiled (${plans[0].count} entries)');

  // 3. THE POSITIVE CONTROL: the legal older entry still compiles, so the
  //    bound refuses the overreach and not the peer.
  check(plans[1].why == t.TableFixedRefusal.none && plans[1].count > 0,
      'R14: the legal older entry compiles: '
      '${t.TableFixedRefusal.name(plans[1].why)} count=${plans[1].count}');

  // 4. THE IDENTITY LANE KEEPS ITS EMPTY LANE.
  check(plans[2].why == t.TableFixedRefusal.none && plans[2].count == 0,
      'R14: the identity entry keeps its empty lane');

  // 5. A LOAD OF THE SAME BYTES refuses by the same name, nothing decoded:
  //    the header's hash selects the hostile entry, the file carries its own
  //    layout, and the lane's why is the load's refusal. The wire rides as
  //    the leg's own spelling of a full 64-bit two's-complement value.
  final file = Uint8List(20 + hostile.length + 12);
  file[0] = 3;
  final view = ByteData.sublistView(file);
  view.setUint64(8, %[2]s, Endian.little);
  view.setUint32(16, hostile.length, Endian.little);
  file.setRange(20, 20 + hostile.length, hostile);
  view.setUint64(20 + hostile.length, %[2]s, Endian.little);
  final plan = t.hostFixedNewPlan();
  final report = t.TableFixedReport();
  final values = <t.Host>[for (var i = 0; i < 1; i++) t.Host()];
  final n = t.hostFixedLoad(values, 1, file, file.length, plan, report);
  check(n == -1 && report.refused == t.TableFixedRefusal.layoutRecordTooLarge,
      'R14: the load owes -1 layout_record_too_large, got $n '
      '${t.TableFixedRefusal.name(report.refused)}');
  if (exitCode == 0) {
    stdout.writeln('ok');
  }
}
`, list.String(), fixedDartHex(hostileWire))
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("R14: the probe over the generated library: %v\n%s", err, out)
	}
}

// t03R22: R22 "the closure rule: every table or type reached by value is
// itself fixed; a pointer, map or unbounded array in the closure is a compile
// refusal; T is never in its own closure". §5.5: "Every table or type in it
// is declared `fixed`; a pointer, map or unbounded array in it is a compile
// refusal; `T` is never in its own closure" — the rows of
// docs/FIXED-FORM-VERSIONING-TESTS.md (closure_plain_table,
// closure_variable_kind, closure_self), refused at the declaration through
// THIS leg's own compile.
func t03R22(t *testing.T) {
	rows := []struct {
		name, want, src string
	}{
		// "every table or type reached by value is itself fixed"
		{"a plain table by value", "plain table Child",
			"package probe\n\ntable Child { x int32 }\n\nfixed table T { child Child }\n"},
		{"a plain table in an array", "plain table Ship",
			"package probe\n\ntable Ship { hp int32 }\n\nfixed table T { ships [..4]Ship }\n"},
		// "a pointer, map or unbounded array in the closure is a compile
		// refusal"
		{"a pointer", "is a pointer",
			"package probe\n\ntable Node { x int32 }\n\nfixed table T { head *Node }\n"},
		{"a map", "is a map",
			"package probe\n\nfixed table T { ships map[uint32]int32 }\n"},
		{"an unbounded array", "is an unbounded array",
			"package probe\n\nfixed table T { entries []int32 }\n"},
		// "T is never in its own closure"
		{"itself by value", "T",
			"package probe\n\nfixed table T { seq uint32\n    next T }\n"},
		{"itself in an array", "T",
			"package probe\n\nfixed table T { seq uint32\n    kids [..4]T }\n"},
		{"itself through a type", "T",
			"package probe\n\ntype Link { to T }\n\nfixed table T { seq uint32\n    link Link }\n"},
		{"itself through a pointer", "T",
			"package probe\n\nfixed table T { seq uint32\n    next *T }\n"},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, perrs := parser.Parse("Probe.schema", []byte(tc.src))
			if len(perrs) > 0 {
				t.Fatalf("%s: parse: %v", tc.name, perrs[0])
			}
			_, cerrs := check.Unit([]check.SourceFile{{
				Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(tc.src), AST: f,
			}})
			if len(cerrs) == 0 {
				t.Fatalf("%s: the closure break compiled — %q is a compile refusal", tc.name, tc.want)
			}
			var got strings.Builder
			for _, e := range cerrs {
				fmt.Fprintf(&got, "%s", e)
			}
			if !strings.Contains(got.String(), tc.want) {
				t.Errorf("%s: the refusal must name %q: %s", tc.name, tc.want, got.String())
			}
		})
	}

	// THE POSITIVE CONTROL: a closure of fixed things alone compiles, and the
	// leg emits the holder's form — the refusal is the construct's, never the
	// keyword's.
	legal := `package probe

fixed table Inner
{
    x int32
}

fixed table T
{
    child Inner
}
`
	u := unitFrom(t, legal)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R22: a closure of fixed things alone compiles: %v", err)
	}
	t02Has(t, t02Source(t, files), "tFixedKnown", "R22 the legal closure keeps its form")
}
