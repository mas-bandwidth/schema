package gotable

// fixedroadmap_t02 — the go leg's compiled-plans and definition-hash rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T)), §5.3 and the contract
// table, docs/FIXED-FORM-VERSIONING-TESTS.md, docs/SPEC-TABLES.md §3.4. One
// subtest per task id, table-driven, t.Parallel() first, and the assertion under
// each is the clause the task's own title states.
//
// EVERY ASSERTION READS THE BYTES THIS LEG EMITS or the IR the emitter is held
// to, so the harness is reachable on a tree whose C++ reference corpus and
// sibling serialize.go checkout may be elsewhere: the compile-and-run half of
// this leg's fixed form is fixedversioning_test.go. A clause already held by an
// existing test of this leg is named by that test in the card's verdicts and is
// not repeated here; this file carries the direct assertion of every clause that
// had none.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Flat is the smallest declared fixed table with two adjacent scalars, which
// the walk lays down as two copies at offsetof(x)/offsetof(y) and the coalescer
// joins into one eight-byte run.
const t02Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t02One is the same table one field SHORTER: the append is the widening §5.1
// permits, so it is the older entry a lock holds for the table above.
const t02One = `package probe

fixed table T
{
    x int32
}
`

// t02Ranged is a table whose digest carries both a range and a resolution: the
// 'R' and 'Q' rows §5.2's digest table names.
const t02Ranged = `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/R1", t02R1},
		{"go/R2", t02R2},
		{"go/R23", t02R23},
		{"go/R25", t02R25},
		{"go/R26", t02R26},
		{"go/W14", t02W14},
		{"go/R4", t02R4},
		{"go/R5", t02R5},
		{"go/W11", t02W11},
		{"go/W12", t02W12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- readers of the emitted source -----------------------------------------

// t02Table answers the unit's fixed table by its declared name.
func t02Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	st := u.Tables[name]
	if st == nil {
		t.Fatalf("the unit declares no fixed table %s", name)
	}
	return st
}

// t02Has is a required substring of the emitted source, named by rule.
func t02Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t02Block is the text between two markers, the first occurrence of `end` after
// `start`. It fails rather than answering "" so a moved section is red by name.
func t02Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the emitted source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the emitted source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t02StructFields reads a Go struct's field names in declaration order.
func t02StructFields(t *testing.T, src, name string) []string {
	t.Helper()
	block := t02Block(t, src, "type "+name+" struct {", "\n}")
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\t([A-Za-z_][A-Za-z0-9_]*)\s+`).FindAllStringSubmatch(block, -1) {
		out = append(out, m[1])
	}
	return out
}

// t02ByteArray reads one emitted `var NAME = []byte{ ... }`.
func t02ByteArray(t *testing.T, src, name string) []byte {
	t.Helper()
	block := t02Block(t, src, "var "+name+" = []byte{", "\n}")
	var out []byte
	for _, spell := range regexp.MustCompile(`0x[0-9a-f]{2}`).FindAllString(block, -1) {
		n, err := strconv.ParseUint(spell[2:], 16, 8)
		if err != nil {
			t.Fatalf("%s: byte %q: %v", name, spell, err)
		}
		out = append(out, byte(n))
	}
	return out
}

// t02Known is one emitted TableFixedKnownLayout row: the wire hash, the record
// size the entry carries, and the layout bytes verbatim.
type t02Known struct {
	hash   uint64
	record int64
	layout []byte
}

// t02KnownEntries reads the emitted lineage array in the order it declares its
// entries, which is what R1's "oldest first and the current layout last" is
// read off.
func t02KnownEntries(t *testing.T, src string) []t02Known {
	t.Helper()
	at := strings.Index(src, "var TFixedKnown = []TableFixedKnownLayout{")
	if at < 0 {
		t.Fatalf("the emitted source has no TFixedKnown")
	}
	rest := src[at:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("TFixedKnown does not close")
	}
	re := regexp.MustCompile(`(?s)Hash:\s+0x([0-9a-f]{16}),\s*Record:\s+(\d+),\s*Layout: \[\]byte\{(.*?)\},\s*\},`)
	var out []t02Known
	for _, m := range re.FindAllStringSubmatch(rest[:end], -1) {
		hash, err := strconv.ParseUint(m[1], 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		record, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		var layout []byte
		for _, spell := range regexp.MustCompile(`0x[0-9a-f]{2}`).FindAllString(m[3], -1) {
			n, err := strconv.ParseUint(spell[2:], 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			layout = append(layout, byte(n))
		}
		out = append(out, t02Known{hash: hash, record: record, layout: layout})
	}
	return out
}

// t02RootBody is the record's BODY size as the layout's own root entry carries
// it: entry 0's size field, the u32 at offset 4 + 9. §5.2's record_bytes is
// `8 + body`, so it is the number the eight may be added to exactly once.
func t02RootBody(t *testing.T, layout []byte) int64 {
	t.Helper()
	if len(layout) < 17 {
		t.Fatalf("a layout of %d bytes carries no root entry", len(layout))
	}
	return int64(binary.LittleEndian.Uint32(layout[13:17]))
}

// t02Lineage generates the table with the lock's older entry handed in the way
// §5.9 #1 names, and answers the emitted fixed module with both entries laid
// down. It is the one fixture R1, R2 and R26 share.
func t02Lineage(t *testing.T) (src string, older, own FixedLineageEntry) {
	t.Helper()
	olderU := unitFrom(t, t02One)
	var ok bool
	older, ok = FixedLineageOf(olderU, "T")
	if !ok {
		t.Fatal("the older unit has no lineage entry for T")
	}
	u := unitFrom(t, t02Flat)
	own, ok = FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("the unit has no lineage entry for T")
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return tableFile(files), older, own
}

// ---- go/R1 -------------------------------------------------------------------

// t02R1: R1 "COMPILE lays the lineage down as static data at build time, oldest
// first and the current layout last, from the lock". The emitted lineage array
// opens with the lock's entry and ends with this build's own, each carrying its
// layout bytes verbatim, and one plan per entry is laid down beside it.
func t02R1(t *testing.T) {
	src, older, own := t02Lineage(t)
	got := t02KnownEntries(t, src)
	if len(got) != 2 {
		t.Fatalf("R1: the static data carries %d entries, want the lock's one and the build's own", len(got))
	}
	if got[0].hash != older.Wire {
		t.Errorf("R1: the first entry is 0x%016x, want the lock's oldest 0x%016x (oldest first)", got[0].hash, older.Wire)
	}
	if got[1].hash != own.Wire {
		t.Errorf("R1: the last entry is 0x%016x, want the current layout 0x%016x last", got[1].hash, own.Wire)
	}
	if !bytes.Equal(got[0].layout, older.Layout) {
		t.Errorf("R1: the first entry's layout is not the lock's bytes verbatim: got %d bytes, want %d", len(got[0].layout), len(older.Layout))
	}
	if !bytes.Equal(got[1].layout, own.Layout) {
		t.Errorf("R1: the last entry's layout is not the current layout: got %d bytes, want %d", len(got[1].layout), len(own.Layout))
	}
	t02Has(t, src, "var TFixedLineagePlans = tableFixedLineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash)",
		"R1 one plan per lineage entry is laid down at build time, from the lock")
}

// ---- go/R2 -------------------------------------------------------------------

// t02R2: R2 "record_bytes is 8 + body: the lock stores the body, COMPILE adds
// the eight once, and no backend adds anything". The emitted constant spells the
// sum once, and every entry's record is the eight plus the body its own layout's
// root entry carries — the number handed in, written verbatim.
func t02R2(t *testing.T) {
	src, older, own := t02Lineage(t)
	u := unitFrom(t, t02Flat)
	body := ir.TableFixedTypeBytes(t02Table(t, u, "T"))
	if own.Record != 8+body {
		t.Errorf("R2: the leg's own entry record = %d, want 8 + body = %d", own.Record, 8+body)
	}
	t02Has(t, src, fmt.Sprintf("const TFixedBodyBytes = %d", body), "R2 the body is the declared size")
	t02Has(t, src, "const TFixedRecordBytes = 8 + TFixedBodyBytes", "R2 the eight is added once in the emitted constant")
	got := t02KnownEntries(t, src)
	if len(got) != 2 {
		t.Fatalf("R2: the static data carries %d records, want one per entry", len(got))
	}
	for i, e := range got {
		if want := 8 + t02RootBody(t, e.layout); e.record != want {
			t.Errorf("R2: entry %d record = %d, want 8 + the layout's root body %d", i, e.record, want)
		}
	}
	if got[0].record != older.Record || got[1].record != own.Record {
		t.Errorf("R2: the entries' records are %d/%d, want the lock's %d and the own %d written verbatim", got[0].record, got[1].record, older.Record, own.Record)
	}
}

// ---- go/R23 ------------------------------------------------------------------

// t02R23: R23 "the static data's member names and order — TableFixedKnownLayout
// = hash, layout, layout_bytes, record_bytes; the report's layout_hash last and
// zero on every other path". This leg's entry is hash, layout, record — a Go
// []byte carries its own layout_bytes — and the report's layout_hash is its last
// member, reset by every by-name refusal and restored only when a layout refusal
// already set it.
func t02R23(t *testing.T) {
	files := generate(t, t02Flat)
	src := tableFile(files)
	if got := t02StructFields(t, src, "TableFixedKnownLayout"); !slices.Equal(got, []string{"Hash", "Layout", "Record"}) {
		t.Errorf("R23: TableFixedKnownLayout's members are %v, want [Hash Layout Record]", got)
	}
	report := t02StructFields(t, src, "TableReport")
	if len(report) == 0 || report[len(report)-1] != "LayoutHash" {
		t.Errorf("R23: TableReport's members end in %v, want LayoutHash last", report)
	}
	refuse := t02Block(t, src, "func tableFixedRefuse(report *TableReport, reason string) int64 {", "\n}")
	t02Has(t, refuse, "*report = TableReport{}", "R23 a by-name refusal zeroes the report")
	t02Has(t, refuse, "hash := report.LayoutHash", "R23 the file's hash is the one field that survives")
	t02Has(t, refuse, "report.LayoutHash = hash", "R23 and it is restored")
	t02Has(t, src, "func tableFixedRefuseHash(report *TableReport, reason string, hash uint64) int64 {", "R23 the hash-reporting refusal is its own site")
	load := t02Block(t, src, "func TFixedLoad(", "\n}\n")
	t02Has(t, load, `tableFixedRefuseHash(report, "layout_newer", hash)`, "R23 layout_newer reports the file's hash")
	if strings.Contains(load, `tableFixedRefuseHash(report, "plan_too_large"`) || strings.Contains(load, `tableFixedRefuseHash(report, "layout_malformed"`) {
		t.Error("R23 a non-layout refusal reports a hash")
	}
}

// ---- go/R25 ------------------------------------------------------------------

// t02R25: R25 "plan_too_large when the plan does not fit the caller's capacity".
// The load holds the selected plan's entry count to the caller's plan slice and
// refuses by name before a record byte lands; the build records the same name on
// an entry whose plan would not fit.
func t02R25(t *testing.T) {
	files := generate(t, t02Flat)
	src := tableFile(files)
	load := t02Block(t, src, "func TFixedLoad(", "\n}\n")
	t02Has(t, load, "if int64(entryCount) > int64(len(plan)) {", "R25 the caller's plan slice is the capacity")
	t02Has(t, load, `tableFixedRefuse(report, "plan_too_large")`, "R25 a plan past the capacity is refused by name")
	t02Has(t, src, `out[i].Why = "plan_too_large"`, "R25 the build records the same name on an entry's lane")
}

// ---- go/R26 ------------------------------------------------------------------

// t02R26: R26 "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw". A
// forged entry is laid down as static data, the lane mapping records a layout
// that would not parse as layout_malformed and a plan that would not fit as
// plan_too_large, and the load refuses by that lane's own name.
func t02R26(t *testing.T) {
	files := generate(t, t02Flat)
	src := tableFile(files)
	plans := t02Block(t, src, "func tableFixedLineagePlans(", "\n}\n")
	t02Has(t, plans, `out[i].Why = "layout_malformed"`, "R26 an unparseable entry's lane is layout_malformed")
	t02Has(t, plans, `out[i].Why = "plan_too_large"`, "R26 an entry whose plan would not fit is plan_too_large")
	if strings.Contains(plans, "panic(") {
		t.Error("R26 tableFixedLineagePlans throws where the clause says refuse by name")
	}
	load := t02Block(t, src, "func TFixedLoad(", "\n}\n")
	t02Has(t, load, `if lane.Why != "" {`, "R26 the load reads the selected entry's own lane")
	t02Has(t, load, "return tableFixedRefuse(report, lane.Why)", "R26 the refusal is by that lane's name")

	u := unitFrom(t, t02Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R26: no own lineage entry for T")
	}
	forged := FixedLineageEntry{Wire: 0x1111111111111111, Layout: []byte{0x00}, Record: 9}
	f, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {forged, own}})
	if err != nil {
		t.Fatalf("R26: GenerateLineage: %v", err)
	}
	got := t02KnownEntries(t, tableFile(f))
	if len(got) != 2 || got[0].hash != forged.Wire || !bytes.Equal(got[0].layout, forged.Layout) {
		t.Errorf("R26: the forged entry is not laid down as static data: %+v", got)
	}
}

// ---- go/W14 ------------------------------------------------------------------

// t02W14: W14 "plan dst == offsetof/sizeof". Two adjacent int32 fields coalesce
// to one run whose dst is offsetof(x) and whose size is sizeof(T); the emitted
// leaves carry those same offsetofs and one four-byte size per field.
func t02W14(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Table(t, u, "T")
	plan, _ := ir.TableFixedBuildPlan(u, st)
	if len(plan) != 1 {
		t.Fatalf("W14: the two adjacent int32 fields coalesce to one run; got %d leaves", len(plan))
	}
	if got, want := plan[0].Dst, ir.TableFixedMemberOffset(u, st, "x"); got != want {
		t.Errorf("W14: the plan's dst = %d, want offsetof(x) = %d", got, want)
	}
	if got, want := plan[0].Size, ir.TableFixedTypeBytes(st); got != want {
		t.Errorf("W14: the plan's size = %d, want sizeof(T) = %d", got, want)
	}
	files := generate(t, t02Flat)
	src := tableFile(files)
	leaves := t02Block(t, src, "func TFixedLeaves(", "\n}\n")
	t02Has(t, leaves, "uint32(unsafe.Offsetof(T{}.X))", "W14 the dst is the field's offsetof")
	t02Has(t, leaves, "uint32(unsafe.Offsetof(T{}.Y))", "W14 the second dst is the field's offsetof")
	if got := strings.Count(leaves, "Size: 4"); got != 2 {
		t.Errorf("W14: the leaves carry %d four-byte sizes, want one per int32", got)
	}
	t02Has(t, src, fmt.Sprintf("const TFixedBodyBytes = %d", ir.TableFixedTypeBytes(st)), "W14 the body constant is sizeof(T)")
}

// ---- go/R4 -------------------------------------------------------------------

// t02R4: R4 "the hash is fnv1a64 over the layout bytes then DIGEST(T), the
// digest computed at the hash site from the schema; a runtime never derives a
// hash from layout bytes it holds". The compiler's hash equals fnv1a64 of the
// layout followed by the definitions digest, the emitted constant is that
// number, and the load takes the header's hash as given.
func t02R4(t *testing.T) {
	u := unitFrom(t, t02Ranged)
	st := t02Table(t, u, "T")
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
	files := generate(t, t02Ranged)
	src := tableFile(files)
	t02Has(t, src, fmt.Sprintf("const TFixedHash = 0x%016x", h), "R4 the emitted hash constant is the digest-bound number")
	load := t02Block(t, src, "func TFixedLoad(", "\n}\n")
	t02Has(t, load, "hash := tableFixedGet64(data[TableFixedHashAt:])", "R4 the file's hash is taken as given")
	if strings.Contains(load, "TableFixedLayoutHash") || strings.Contains(load, "0x100000001b3") {
		t.Error("R4: the load path derives a hash from layout bytes it holds")
	}
}

// ---- go/R5 -------------------------------------------------------------------

// t02R5: R5 "the digest carries every range, every resolution (tag 'Q') and
// every reader limit (tag 'L'), and a flags type deduped by name, once". The
// bytes are positional and named by tag, so each clause is read off the digest
// directly. The 'L' clause has two halves: no table spelling of a reader-side
// limit exists in the IR (the premise the reservation rests on), and no digest
// this leg builds carries an 'L'.
func t02R5(t *testing.T) {
	ranged := t02Table(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`), "T")
	rd := ir.TableFixedDefinitionsDigest(ranged)
	if len(rd) != 17 || rd[0] != 'R' {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", rd)
	}

	compressed := t02Table(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`), "T")
	cd := ir.TableFixedDefinitionsDigest(compressed)
	if len(cd) != 26 || cd[0] != 'R' || cd[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", cd)
	}

	flagsOnce := t02Table(t, unitFrom(t, `package probe

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

	flagsTwice := t02Table(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
    n Marks
}
`), "T")
	if got := ir.TableFixedDefinitionsDigest(flagsTwice); string(got) != string(once) {
		t.Errorf("R5: the flags type is not deduped by name: once % x, twice % x", once, got)
	}

	// ALGORITHM §5.2 reserves 'L' because no table spelling of a reader-side
	// limit exists: no member the walk can reach declares one. Prove that
	// premise, so the empty row is the page's reservation and not a coincidence
	// of these fixtures.
	for _, rt := range []reflect.Type{
		reflect.TypeFor[ir.Field](),
		reflect.TypeFor[ir.Struct](),
		reflect.TypeFor[ir.Unit](),
	} {
		for sf := range rt.Fields() {
			if strings.Contains(strings.ToLower(sf.Name), "limit") {
				t.Fatalf("R5: the IR declares %s.%s, a table-declared reader-side limit; ALGORITHM §5.2 reserves 'L' only until one exists", rt.Name(), sf.Name)
			}
		}
	}
	for _, d := range [][]byte{rd, cd, once} {
		if slices.Contains(d, byte('L')) {
			t.Error("R5: the digest carries a reader-limit row no table spelling produces")
		}
	}
}

// ---- go/W11 ------------------------------------------------------------------

// t02W11: W11 "bytes(N) is layout kind 14". A bytes(N) field walks as an ARRAY
// (kind 14) with one synthetic u8 child, and the byte that rides the layout is
// the same number.
func t02W11(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	st := t02Table(t, u, "T")
	w := (&tableGen{}).fixedWalkRoot(st)
	at := -1
	for i, e := range w.entries {
		if e.note == "blob" {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("W11: the bytes(6) field is not in the leg's walk")
	}
	if e := w.entries[at]; e.kind != ir.TableKindArray {
		t.Errorf("W11: bytes(6) walks as kind %d, want %d (array)", e.kind, ir.TableKindArray)
	}
	if e := w.entries[at]; e.children != 1 {
		t.Errorf("W11: bytes(6) carries %d children, want the one synthetic u8", e.children)
	}
	if at+1 >= len(w.entries) || w.entries[at+1].kind != ir.TableKindU8 || w.entries[at+1].size != 1 || w.entries[at+1].children != 0 {
		t.Errorf("W11: bytes(6)'s element is not the synthetic u8 (kind 6, size 1, no children)")
	}

	files := generate(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	layout := t02ByteArray(t, tableFile(files), "TFixedLayout")
	if len(layout) < 4+2*17 {
		t.Fatalf("W11: the emitted layout is %d bytes, too short for the table and the bytes row", len(layout))
	}
	if got := layout[4+17+8]; got != 14 {
		t.Errorf("W11: the bytes(6) layout kind byte = %d, want 14", got)
	}
	if ir.TableKindArray != 14 {
		t.Fatalf("W11: TableKindArray = %d, want 14", ir.TableKindArray)
	}
}

// ---- go/W12 ------------------------------------------------------------------

// t02W12: W12 "hash includes the 4-byte count". The layout opens with the u32
// entry count, the emitted bytes carry that count, and the hash moves when the
// count moves.
func t02W12(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Table(t, u, "T")
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

	files := generate(t, t02Flat)
	emitted := t02ByteArray(t, tableFile(files), "TFixedLayout")
	if len(emitted) < 4 || binary.LittleEndian.Uint32(emitted[:4]) != uint32(len(w)) {
		t.Errorf("W12: the emitted layout does not open with the %d-entry count", len(w))
	}
}
