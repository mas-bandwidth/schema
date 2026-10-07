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
