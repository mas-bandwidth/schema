package darttable

// THE ROADMAP'S VERSIONING TASKS ON THE DART LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE DART BYTES THIS LEG EMITS and the shared
// runtime body it emits them with, so the harness is reachable on a tree that
// never built the C++ reference corpus and never carries the Dart SDK. A clause
// that needs the emitted code to RUN is asserted at the emitted site that runs
// it, and that site is named in the check — the shape fixedroadmap_t02_test.go
// and fixedroadmap_t03_test.go state for the same reason.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live} probes,
// which a gate runs (make tables-dart-versioning). The subtests below carry
// the clauses those probes cannot read off the bytes: the floor's ARITHMETIC
// in the emitter, and the refusal's position and purity in the emitted load.
//
// dart/R3 [verify]: the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash.
//
// dart/R32 [weak]: retire for real: a retired version is refused by name,
// once, idempotently.
//
// dart/R10 [weak]: the run-time walk of a stranger's layout and the recompute
// of the header's hash are retired.
//
// dart/R15 [verify]: the four forward-read clamps are retired — count clamp
// across bounds, range clamp across versions, remap of an unknown variant to
// None, drop-and-count of an unknown field: each is layout_newer now.
//
// dart/E3/other-required-widens [capability]: all other required widen-ladder
// cases.
//
// dart/C8 [owed]: fixed-point F-shift / bits(N).
//
// dart/R20 [weak]: §21.2's landing rules.
//
// dart/R21 [owed]: widenf is the bit-exact widening — signalling NaNs kept, the
// quiet bit carried as the writer wrote it.
//
// dart/R29 [owed]: the band case: the widening across the 65536 ceiling, and
// the bounds pass clamping to the writer's bounds.
//
// dart/E5 [verify]: ?T vs plain nesting.
//
// dart/W2 [verify]: absent optional skips store.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t04Fixed is the emitted fixed module of a unit (the one *Fixed.dart the table
// backend produced).
func t04Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "Fixed.dart") {
			return string(data)
		}
	}
	t.Fatalf("no *Fixed.dart module was emitted")
	return ""
}

// t04Fn is the emitted body of one Dart function, up to its column-zero
// closing brace.
func t04Fn(text, name string) string {
	at := strings.Index(text, name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t04Has is a required substring of the emitted source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04Retire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module together with the floor it published.
func t04Retire(t *testing.T, retired int) (text string, floor int) {
	t.Helper()
	older := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	middle := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	own := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    z int32\n}\n")
	e1, ok := FixedLineageOf(older, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the first generation")
	}
	e2, ok := FixedLineageOf(middle, "Lineage")
	if !ok {
		t.Fatal("no lineage entry for the second generation")
	}
	if retired > 0 {
		e1.Retired = true
		e1.Reason = "the operator retired this layout"
	}
	if retired > 1 {
		e2.Retired = true
		e2.Reason = "the operator retired this layout"
	}
	files, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {e1, e2}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t04Fixed(t, files), retired
}

// t04Lineage builds the versioning pair of one row and answers the emitted
// module: the reader is `newSrc`, handed `oldSrc`'s locked entry as its oldest
// lineage entry, exactly as fixedversioning_test.go's probe harness does.
func t04Lineage(t *testing.T, oldSrc, newSrc string) (text string, own *ir.Unit) {
	t.Helper()
	older := unitFrom(t, oldSrc)
	own = unitFrom(t, newSrc)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	files, err := GenerateLineage(own, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t04Fixed(t, files), own
}

// t04HexBytes reads one emitted `const <int>[ ... ]` byte run by its opening
// marker, so a value assertion names the constant and not a random hex byte.
func t04HexBytes(t *testing.T, text, marker string) []byte {
	t.Helper()
	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("the emitted source carries no %q", marker)
	}
	rest := text[at+len(marker):]
	end := strings.Index(rest, "]);")
	if end < 0 {
		t.Fatalf("the emitted byte run after %q is not closed", marker)
	}
	var out []byte
	for field := range strings.FieldsSeq(rest[:end]) {
		field = strings.TrimSuffix(field, ",")
		if field == "" || !strings.HasPrefix(field, "0x") {
			continue
		}
		var v int
		for _, c := range field[2:] {
			v = v*16 + strings.IndexByte("0123456789abcdef", byte(c))
		}
		out = append(out, byte(v))
	}
	return out
}


// ---- dart/R3 -----------------------------------------------------------------

// t04R3 holds dart/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	for _, tc := range []struct {
		retired int
		want    string
	}{
		{0, "const int lineageFixedFloor = 0;"},
		{1, "const int lineageFixedFloor = 1;"},
		{2, "const int lineageFixedFloor = 2;"},
	} {
		text, _ := t04Retire(t, tc.retired)
		t04Has(t, text, tc.want, "R3 1 + the highest retired index")
	}
	text, _ := t04Retire(t, 2)
	t04Has(t, text, "1 + the highest RETIRED index, 0 when none is", "R3 the emitter states its own arithmetic")
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R3: lineageFixedLoad is not emitted")
	}
	t04Has(t, load, "if (pick < lineageFixedFloor) {", "R3 the floor check")
	t04Has(t, load, "report.refused = TableFixedRefusal.layoutUnsupported;", "R3 below the floor is layout_unsupported")
	t04Has(t, load, "report.layoutHash = hash;", "R3 below the floor reports the file's hash")
	// NOTHING RETIRED is a floor of ZERO: the emitter still publishes the
	// number, and the comparison it writes is one no pick can satisfy because
	// the hash select already refused every pick below zero on the line above.
	none, _ := t04Retire(t, 0)
	noneLoad := t04Fn(none, "lineageFixedLoad")
	if !strings.Contains(noneLoad, "if (pick < 0) {") {
		t.Error("R3: the floor-0 reader carries no layout_newer select, so the floor check cannot be read against it")
	}
	if strings.Count(noneLoad, "if (pick < lineageFixedFloor) {") != 1 {
		t.Error("R3: a floor of 0 must still emit the one comparison, whose pick is never below it")
	}
}


// ---- dart/R32 ----------------------------------------------------------------

// t04R32 holds dart/R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per retired entry, the floor check sits ONCE,
// after the hash select and BEFORE the byte comparison or any record, and its
// whole body is the named refusal — a pure read of `pick`, so a second load of
// the same file answers the same name. REFUSE is TOTAL: the report is reset at
// the top of the load, so no counter survives into the refusal.
func t04R32(t *testing.T) {
	text, _ := t04Retire(t, 1)
	t04Has(t, text, "// RETIRED: the operator retired this layout", "R32 the retired entry's mark is laid down")
	// THE CURRENT ENTRY CARRIES NO MARK: the third generation is not retired.
	if strings.Count(text, "// RETIRED:") != 1 {
		t.Errorf("R32: the module carries %d retired marks, want the one the lock retired", strings.Count(text, "// RETIRED:"))
	}
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R32: lineageFixedLoad is not emitted")
	}
	if n := strings.Count(load, "if (pick < lineageFixedFloor) {"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "if (pick < lineageFixedFloor) {")
	known := strings.Index(load, "final known = lineageFixedKnown[pick];")
	bytes := strings.Index(load, "if (layoutBytes != known.layoutBytes) {")
	records := strings.Index(load, "for (var k = 0; k < n; k++) {")
	if at < 0 || known < 0 || bytes < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d, %d)", at, known, bytes, records)
	}
	if !(at < known && known < bytes && bytes < records) {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d, %d)", at, known, bytes, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads `pick` and returns the named
	// refusal; nothing before it mutates the report or the destination.
	body := load[at:]
	if end := strings.Index(body, "\n  }\n"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "report.unknown") || strings.Contains(body, "plan.image") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	for _, zero := range []string{
		"refused = TableFixedRefusal.none;",
		"unknown = 0;",
		"kindMismatch = 0;",
		"widened = 0;",
		"clamped = 0;",
		"malformed = false;",
		"layoutHash = 0;",
	} {
		t04Has(t, fixedRuntime, zero, "R32 REFUSE is TOTAL")
	}
}


// ---- dart/R10 -----------------------------------------------------------------

// t04R10 holds dart/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted load parses no layout (the lock's bytes were parsed by
// tableFixedLineagePlans, built once and never again), walks no stranger's
// bytes, and never recomputes the header's hash: the header's eight bytes are
// taken as given and matched on.
func t04R10(t *testing.T) {
	text, _ := t04Retire(t, 0)
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R10: lineageFixedLoad is not emitted")
	}
	t04Has(t, load, "if (layoutBytes != known.layoutBytes) {", "R10 the layout's length is a byte comparison")
	t04Has(t, load, "bytes[TableFixedLimits.layoutAt + i] != known.layout[i]", "R10 the layout's bytes are compared")
	t04Has(t, load, "report.refused = TableFixedRefusal.layoutMalformed;", "R10 a lie about a known version is layout_malformed")
	if strings.Contains(load, ".parse(") {
		t.Error("R10: the load walks a stranger's layout; the walk is the build's TableFixedLineagePlans")
	}
	if strings.Contains(load, "hashOf(") {
		t.Error("R10: the load recomputes the header's hash")
	}
	if strings.Contains(load, "checkEntry(") || strings.Contains(load, "knownKind(") || strings.Contains(load, "leafSize(") {
		t.Error("R10: the load carries §1.1's layout rules into the run time")
	}
	// THE HASH IS STILL A WIRE IDENTITY: the function survives for the writer
	// side and for any later reader, but no load path calls it.
	t04Has(t, fixedRuntime, "static int hashOf(Uint8List bytes, int at, int length) {", "R10 the hash function")
}


// ---- dart/R15 -----------------------------------------------------------------

// t04R15 holds dart/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan
// selection, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan (bill §12.5), which only a backward read
// over a locked entry can reach; and the unknown-field count lands from the
// COMPILE census after a returning read, never per record.
func t04R15(t *testing.T) {
	text, _ := t04Retire(t, 0)
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R15: lineageFixedLoad is not emitted")
	}
	miss := strings.Index(load, "report.refused = TableFixedRefusal.layoutNewer;")
	records := strings.Index(load, "for (var k = 0; k < n; k++) {")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	if strings.Contains(load, "report.unknown++") {
		t.Error("R15: an unknown field is dropped-and-counted per record on the read path")
	}
	t04Has(t, load, "report.unknown += censusUnknown;", "R15 unknown is the compile census")
	t04Has(t, fixedRuntime, "} else if (counted > size) {", "R15 the count clamp is the writer's bound")
	t04Has(t, fixedRuntime, "} else if (used > cap) {", "R15 the text clamp is the writer's span")
	if strings.Contains(fixedRuntime, "remap.get(") {
		t.Error("R15: a forward-read remap survives in the runtime")
	}
	// THE RANGE CLAMP ACROSS VERSIONS IS RETIRED: the compiled plan carries no
	// range at all — its nine lanes are op, src, dst, size, aux, guard, arg,
	// meta and the guard's width, and the ninth is last — so a plan compiled
	// from a stranger's layout has nowhere to hold the writer's ends. A ranged
	// leaf is clamped in the DECODE against the READER's own declared ends
	// (schema#1164, statement B), not against a peer's.
	t04Has(t, fixedRuntime, "static const int lanes = 9;", "R15 the plan carries no range lane")
	t04Has(t, fixedRuntime, "static const int argW = 8;", "R15 the ninth lane is the guard's width")
	ranged, _ := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    x int32 | min = 0, max = 100\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    x int32 | min = 0, max = 200\n}\n")
	decode := t04Fn(ranged, "lineageFixedDecode")
	if decode == "" {
		t.Fatal("R15: lineageFixedDecode is not emitted for a ranged leaf")
	}
	t04Has(t, decode, "if (value.x > 200) {", "R15 the range clamp is the READER's own declared end")
}


// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapDartT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "dart/R3", check: t04R3},
		{id: "dart/R32", check: t04R32},
		{id: "dart/R10", check: t04R10},
		{id: "dart/R15", check: t04R15},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
