package darttable

// THE DART LEG'S FIXED-TABLE COMPILED-PLANS ROW (docs/roadmap.sexp node
// `fixed-tables`, row compiled-plans). Each subtest is one roadmap task id and
// asserts the clause its own title states: the SUBJECT is the emitter's own
// bytes, read off the generated <Base>Fixed.dart, plus the compiler's own IR
// where a value is not a wire fact. Law: docs/FIXED-FORM-ALGORITHM.md §5.2
// (COMPILE(lock, T) and the lineage as static data), §5.3 (the load's steps)
// and §5.9 #4/#5/#8, docs/SPEC-TABLES.md §3.4, ROADMAP.md "NEW Fixed Tables".
//
// The harness is TABLE-DRIVEN, one subtest per task id, and every assertion
// quotes the task sentence it implements. A clause an existing test of this leg
// already holds is named by that test in the card's verdicts and is not
// repeated here.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Flat is the smallest declared fixed table: two adjacent scalars, which the
// coalescer lands as ONE run, so the plan's dst and size are the record's.
const t02Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t02Counted parts the C ABI from the fixed form's canonical image: §3.4 lays
// every field at its DECLARED width with nothing padded, so the int32 count
// rides directly behind the int8 at 1, while a C compiler aligns that count to
// four. It is the fixture that shows which ABI the emitted plan is asserted
// against.
const t02Counted = `package probe

fixed table T
{
    a int8
    m [..2]int32
}
`

// t02Source is every file the emitter wrote for one unit, in name order, so an
// assertion reads the emitter's own bytes without depending on which file a
// section landed in.
func t02Source(t *testing.T, files map[string][]byte) string {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "/* ===== %s ===== */\n", n)
		b.Write(files[n])
		b.WriteString("\n")
	}
	return b.String()
}

// t02Block is the text between two markers: the first `end` after `start`. It
// fails rather than answering "" so a moved section is red by name.
func t02Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the generated source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the generated source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t02Has is a required substring of the emitted source, named by rule.
func t02Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t02Table is the named fixed root of a unit, failed by name rather than nil.
func t02Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("the unit declares no fixed table %s", name)
	return nil
}

// t02Rows is every integer row under a fixed-format list the emitter wrote for
// `name` (for example `final Int32List tFixedIdentity`), each returned as its
// cells with the trailing note stripped. It is how a lane of the plan is
// compared as a VALUE rather than searched for as a substring.
func t02Rows(t *testing.T, src, name string) [][]int64 {
	t.Helper()
	block := t02Block(t, src, name+" = Int32List.fromList(const <int>[", "]);")
	var rows [][]int64
	for _, line := range strings.Split(block, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row []int64
		for cell := range strings.SplitSeq(line, ",") {
			cell = strings.TrimSpace(cell)
			if cell == "" {
				continue
			}
			n, err := strconv.ParseInt(cell, 10, 64)
			if err != nil {
				t.Fatalf("the row %q of %s is not an integer: %v", line, name, err)
			}
			row = append(row, n)
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("the list %s has no row", name)
	}
	return rows
}

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"dart/R1", t02R1},
		{"dart/R2", t02R2},
		{"dart/R23", t02R23},
		{"dart/R25", t02R25},
		{"dart/R26", t02R26},
		{"dart/W14", t02W14},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02R1: R1 "COMPILE lays the lineage down as static data at build time, oldest
// first and the current layout last, from the lock". The lineage the build holds
// for a table rides as the `known` list — the lock's entries first, the current
// layout last — and it is a compile-time constant, not a value a load computes.
func t02R1(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Table(t, u, "T")
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	// TWO LOCKED ENTRIES, the oldest retired, so both the order and the floor
	// are exercised; the current layout is appended last by COMPILE.
	oldest := FixedLineageEntry{
		Wire:    own.Wire ^ 0x1111111111111111,
		Layout:  []byte{0x01, 0x02, 0x03, 0x04},
		Record:  12,
		Retired: true,
		Reason:  "superseded by the current layout",
	}
	middle := FixedLineageEntry{
		Wire:   own.Wire ^ 0x2222222222222222,
		Layout: []byte{0x05, 0x06},
		Record: 14,
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {oldest, middle}})
	if err != nil {
		t.Fatalf("R1: GenerateLineage: %v", err)
	}
	src := t02Source(t, files)
	lower := lowerFirst(st.Name)

	t02Has(t, src,
		fmt.Sprintf("final List<TableFixedKnownLayout> %sFixedKnown = <TableFixedKnownLayout>[", lower),
		"R1 the lineage is static data at build time")
	block := t02Block(t, src, fmt.Sprintf("%sFixedKnown = <TableFixedKnownLayout>[", lower), "\n];")
	at := -1
	for _, e := range []FixedLineageEntry{oldest, middle, own} {
		i := strings.Index(block, fmt.Sprintf("TableFixedKnownLayout(%s,", fixedDartHex(e.Wire)))
		if i < 0 {
			t.Fatalf("R1: the known list does not carry the entry for %s", fixedDartHex(e.Wire))
		}
		if i < at {
			t.Errorf("R1: the entry %s is out of order; the lock's entries come first and the current layout last", fixedDartHex(e.Wire))
		}
		at = i
	}
	t02Has(t, block, "// RETIRED: "+oldest.Reason, "R1 a retired entry keeps its reason")
	t02Has(t, src, fmt.Sprintf("const int %sFixedFloor = 1;", lower),
		"R1 the floor is 1 + the highest retired index")
	t02Has(t, src, fmt.Sprintf("tableFixedSelect(%sFixedKnown, hash)", lower),
		"R1 the load selects from the static list and builds no lineage")
}

// t02R2: R2 "record_bytes is 8 + body: the lock stores the body, COMPILE adds
// the eight once, and no backend adds anything". THE WHOLE RECORD IS COMPILE's:
// compiler/lineage.go adds the eight hash bytes to the lock's BODY once and
// hands every backend the whole number (docs/FIXED-FORM-ALGORITHM.md §5.2, rule
// #46), and a backend writes what it is handed VERBATIM. The leg's own entry is
// 8 + body — and since that entry wins for the current layout whatever a
// backend does (#46), the assertion that BITES is the OLDER entry: handed a
// whole record, the emitted known-layout entry carries it unchanged. The
// addition itself is held by compiler.TestFixedLineageRecordSizeIsTheWholeRecord.
func t02R2(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Table(t, u, "T")
	body := fixedTypeBytes(st)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R2: FixedLineageOf has no entry for T")
	}
	// THE LEG'S OWN ENTRY, for the current layout alone: 8 + body (#46).
	if own.Record != 8+body {
		t.Errorf("R2 record_bytes is 8 + body: FixedLineageOf(T).Record = %d, want 8 + %d = %d", own.Record, body, 8+body)
	}
	// AN OLDER ENTRY OF A LOCK-SHAPED LINEAGE, with a record size that is NOT
	// 8 + its body, so a backend that added the eight a second time answers a
	// different number here.
	older := FixedLineageEntry{
		Wire:   own.Wire ^ 0x3333333333333333,
		Layout: []byte{0x0a, 0x0b},
		Record: 13,
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R2: GenerateLineage: %v", err)
	}
	src := t02Source(t, files)
	lower := lowerFirst(st.Name)
	t02Has(t, src, fmt.Sprintf("const int %sFixedRecordBytes = %d; // the hash and the body", lower, 8+body),
		"R2 the COMPILE adds the eight once")
	// THE BACKEND ADDS NOTHING: the older entry is emitted with the whole
	// number the caller handed, and the current one with its own 8 + body.
	t02Has(t, src, fmt.Sprintf("]), %d, %d),", len(older.Layout), older.Record),
		"R2 the known entry writes the handed record size verbatim")
	if added := fmt.Sprintf("]), %d, %d),", len(older.Layout), older.Record+8); strings.Contains(src, added) {
		t.Errorf("R2: a backend added the eight a second time: the source carries %q", added)
	}
	t02Has(t, src, fmt.Sprintf("]), %d, %d),", len(own.Layout), 8+body),
		"R2 the current layout's entry is its own 8 + body")
	t02Has(t, src, "var recordBytes = known.recordBytes;",
		"R2 the load takes the record size from the lock's entry")
}

// t02R23: R23 "the static data's member names and order — TableFixedKnownLayout
// = hash, layout, layout_bytes, record_bytes; the report's layout_hash last and
// zero on every other path". The class's constructor and fields are in that
// order, the report's last field is layout_hash, reset() zeroes it, and the two
// layout refusals are the only setters.
func t02R23(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Table(t, u, "T")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R23: Generate: %v", err)
	}
	src := t02Source(t, files)

	known := t02Block(t, src, "final class TableFixedKnownLayout {", "\n}\n")
	at := -1
	for _, member := range []string{
		"this.hash,",
		"this.layout,",
		"this.layoutBytes,",
		"this.recordBytes,",
		"final int hash;",
		"final Uint8List layout;",
		"final int layoutBytes;",
		"final int recordBytes;",
	} {
		i := strings.Index(known, member)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout has no member %q", member)
		}
		if i < at {
			t.Errorf("R23: TableFixedKnownLayout's member %q is out of order", member)
		}
		at = i
	}

	report := t02Block(t, src, "final class TableFixedReport {", "\n}\n")
	decls := report
	if i := strings.Index(decls, "void reset()"); i >= 0 {
		decls = decls[:i]
	}
	last := ""
	for _, line := range strings.Split(decls, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "}") {
			continue
		}
		if strings.HasSuffix(s, ";") {
			last = s
		}
	}
	if !strings.HasPrefix(last, "int layoutHash = 0;") {
		t.Errorf("R23: layout_hash is not the report's last member; last = %q", last)
	}
	t02Has(t, src, "report.reset();", "R23 the report is zeroed before anything else")
	t02Has(t, report, "layoutHash = 0;", "R23 reset() zeroes layout_hash")
	load := t02Block(t, src, fmt.Sprintf("int %sFixedLoad(", lowerFirst(st.Name)), "\n}\n")
	if got := strings.Count(load, "report.layoutHash = hash;"); got != 2 {
		t.Errorf("R23: layout_hash is set on %d paths, want the two layout refusals and no other", got)
	}
	if !strings.Contains(load, "if (pick < 0) {") || !strings.Contains(load, "layoutUnsupported") {
		t.Error("R23: the two layout refusals are the only paths that report the file's hash")
	}
}

// t02R25: R25 "plan_too_large when the plan does not fit the caller's
// capacity". The load measures the selected lane's entry count against the
// caller's declared capacity and refuses by name before a record byte lands.
// The read is DRIVEN with this leg's own toolchain: the older writer's file is
// read through a plan whose capacity is zero, so the older entry's compiled
// lane cannot fit and the refusal names plan_too_large.
func t02R25(t *testing.T) {
	dartBin := dartBinary(t)
	body := `  final known = t.lineageFixedKnown;
  check(known.length >= 2, 'R25 needs the older entry and the current layout: ${known.length}');
  final older = known[0];
  final file = Uint8List(t.TableFixedLimits.layoutAt + older.layoutBytes);
  file[0] = t.tableFixedForm;
  ByteData.sublistView(file).setUint64(t.TableFixedLimits.hashAt, older.hash, Endian.little);
  ByteData.sublistView(file).setUint32(t.TableFixedLimits.headerBytes, older.layoutBytes, Endian.little);
  file.setRange(t.TableFixedLimits.layoutAt, file.length, older.layout);
  final small = t.lineageFixedNewPlan(entryCapacity: 0);
  final rep = t.TableFixedReport();
  final n = LOAD(values, values.length, file, file.length, small, rep);
  check(n == -1, 'R25 the caller capacity is the bound: owes -1, got $n: ${why(rep)}');
  check(rep.refused == t.TableFixedRefusal.planTooLarge,
      'R25 owes plan_too_large, got ${name(rep.refused)}: ${why(rep)}');
  check(!rep.malformed, 'R25 a refusal by name never sets malformed: ${why(rep)}');
`
	out, err := runVersionProbe(t, dartBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0, "", body)
	if err != nil {
		t.Fatalf("R25 plan_too_large: %v\n%s", err, out)
	}
}

// t02R26: R26 "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw".
// The emitter is handed a known hash whose recorded bytes are not a layout; at
// run time that entry's lane carries its own name, the load refuses by it, and
// nothing throws.
func t02R26(t *testing.T) {
	dartBin := dartBinary(t)
	body := `  final known = t.lineageFixedKnown;
  check(known.length >= 2, 'R26 needs the older entry and the current layout: ${known.length}');
  final bad = known[0];
  // THE LOCK GOT THE ENTRY WRONG: the bytes cannot build a plan, and the file
  // carries the same bytes back so the read reaches that entry's own lane.
  bad.layout.fillRange(0, bad.layout.length, 0x5a);
  final file = Uint8List(t.TableFixedLimits.layoutAt + bad.layoutBytes);
  file[0] = t.tableFixedForm;
  ByteData.sublistView(file).setUint64(t.TableFixedLimits.hashAt, bad.hash, Endian.little);
  ByteData.sublistView(file).setUint32(t.TableFixedLimits.headerBytes, bad.layoutBytes, Endian.little);
  file.setRange(t.TableFixedLimits.layoutAt, file.length, bad.layout);
  final rep = t.TableFixedReport();
  final n = LOAD(values, values.length, file, file.length, plan, rep);
  check(n == -1, 'R26 an entry that would not build owes -1, got $n: ${why(rep)}');
  check(rep.refused == t.TableFixedRefusal.layoutMalformed,
      'R26 owes the entry own name layout_malformed, got ${name(rep.refused)}: ${why(rep)}');
  check(!rep.malformed, 'R26 a lane refusal is by name, never malformed: ${why(rep)}');
`
	out, err := runVersionProbe(t, dartBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0, "", body)
	if err != nil {
		t.Fatalf("R26 a lineage entry that would not build: %v\n%s", err, out)
	}
}

// t02W14: W14 "plan dst == offsetof/sizeof". §4.1's law is that the plan's
// destinations are ASSERTED AGAINST THE LANGUAGE'S OWN ABI (docs/SPEC-TABLES.md
// §4.1), and Dart controls no struct layout: it has no offsetof and no sizeof
// (internal/codegen/darttable/fixedruntime.go:27, docs/SPEC-TABLES.md §3.4).
// The leg's own ABI is the canonical body image — every field at its declared
// width, nothing padded — whose offsets it lays down in `tFixedDst` and whose
// size it lays down in `tFixedBodyBytes`. The emitted identity plan's dst lanes
// must equal those offsets and its size lane that size, and the emitted
// offsets, not the C ABI's padded ir.TableFixedMemberOffset, are the oracle.
func t02W14(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Table(t, u, "T")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W14: Generate: %v", err)
	}
	src := t02Source(t, files)
	lower := lowerFirst(st.Name)
	// THE PLAN'S LANES, read off the emitted identity array: op, src, dst, size,
	// aux, guard, arg, meta, argw. Two adjacent int32 fields are one run whose
	// dst is x's own offset and whose size is the whole body.
	identity := t02Rows(t, src, "final Int32List "+lower+"FixedIdentity")
	if len(identity) != 1 || len(identity[0]) != 9 {
		t.Fatalf("W14: the two adjacent int32 fields are one nine-lane run; got %d rows of %d lanes", len(identity), len(identity[0]))
	}
	// tFixedDst's first lane is the entry's own offset in the canonical image;
	// its other four are the stride, the aux, the counted flag and the flavour.
	layout := t02Rows(t, src, "final Int32List "+lower+"FixedDst")
	if len(layout) != 3 {
		t.Fatalf("W14: the walk is root, x and y; got %d layout rows", len(layout))
	}
	if got, want := identity[0][2], layout[1][0]; got != want {
		t.Errorf("W14: the emitted plan dst = %d, want the emitted layout's own offset of x = %d", got, want)
	}
	if got, want := identity[0][3], fixedTypeBytes(st); got != want {
		t.Errorf("W14: the emitted plan size = %d, want the canonical sizeof(T) = %d", got, want)
	}
	t02Has(t, src, fmt.Sprintf("const int %sFixedBodyBytes = %d;", lower, fixedTypeBytes(st)),
		"W14 the size lane is the emitted sizeof")

	// THE C ABI IS NOT THE ORACLE. A count rides at 1 in the canonical image and
	// at 4 in the C ABI the shared ir walk assumes; the emitted plan and the
	// emitted layout must both say 1.
	pu := unitFrom(t, t02Counted)
	pst := t02Table(t, pu, "T")
	pfiles, err := Generate(pu)
	if err != nil {
		t.Fatalf("W14: Generate: %v", err)
	}
	psrc := t02Source(t, pfiles)
	plower := lowerFirst(pst.Name)
	pidentity := t02Rows(t, psrc, "final Int32List "+plower+"FixedIdentity")
	playout := t02Rows(t, psrc, "final Int32List "+plower+"FixedDst")
	// a's copy, m's count and m's two elements are three plan entries; the walk
	// is root, a, m and m's element.
	if len(pidentity) != 3 {
		t.Fatalf("W14: a, m's count and m's elements are three plan entries; got %d", len(pidentity))
	}
	if len(playout) != 4 {
		t.Fatalf("W14: the walk is root, a, m and m's element; got %d layout rows", len(playout))
	}
	// m's layout row is dst=buffer 5, stride=4, aux=count 1, counted=1.
	if got, want := pidentity[1][2], playout[2][2]; got != want {
		t.Errorf("W14: the emitted count entry's dst = %d, want the emitted layout's own count offset = %d", got, want)
	}
	if got, want := pidentity[2][2], playout[2][0]; got != want {
		t.Errorf("W14: the emitted element run's dst = %d, want the emitted layout's own buffer offset = %d", got, want)
	}
	cabi := ir.TableFixedMemberOffset(pu, pst, "m_count")
	if cabi == playout[2][2] {
		t.Fatalf("W14: the fixture does not part the C ABI (%d) from the canonical image (%d)", cabi, playout[2][2])
	}
	if got := playout[2][2]; got != 1 {
		t.Errorf("W14: the canonical count offset = %d, want 1 (the C ABI's ir.TableFixedMemberOffset is %d)", got, cabi)
	}
}
