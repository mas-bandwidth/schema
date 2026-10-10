package darttable

// THE ROADMAP'S VERSIONING TASKS ON THE DART LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.dart.
// A clause that needs the emitted code to RUN is asserted at the emitted site
// that runs it, and that site is named in the check — the shape
// fixedroadmap_t02_test.go states for the same reason.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// `TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live}` probes,
// which a gate runs (`make tables-dart-versioning`). The subtests below carry
// the clauses those probes cannot read off the bytes: the floor's ARITHMETIC
// in the emitter, and the refusal's position and purity in the emitted load.
//
// dart/R3 [verify]: the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash.
//
// dart/R32 [weak]: retire for real: a retired version is refused by name, once,
// idempotently.
//
// dart/R10 [verify]: the run-time walk of a stranger's layout and the recompute
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

// t04Fixed is the emitted fixed module text of a unit, every file the table
// backend wrote in name order, so an assertion reads the emitter's own bytes
// without depending on which file a section landed in.
func t04Fixed(t *testing.T, files map[string][]byte) string {
	t.Helper()
	text := t02Source(t, files)
	if !strings.Contains(text, "TableFixedRefusal") {
		t.Fatalf("no fixed module was emitted")
	}
	return text
}

// t04Emit generates one unit and answers the emitted fixed text.
func t04Emit(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t04Fixed(t, files)
}

// t04Fn is the emitted body of one top-level Dart function `NAME(`, up to its
// column-zero closing brace. A call site is indented, a definition is not, so
// the definition is the occurrence whose line prefix is not blank.
func t04Fn(text, name string) string {
	needle := name + "("
	for from := 0; ; {
		at := strings.Index(text[from:], needle)
		if at < 0 {
			return ""
		}
		at += from
		line := strings.LastIndexByte(text[:at], '\n') + 1
		if strings.TrimSpace(text[line:at]) != "" {
			rest := text[at:]
			if end := strings.Index(rest, "\n}\n"); end >= 0 {
				return rest[:end]
			}
			return rest
		}
		from = at + len(needle)
	}
}

// t04Has is a required substring of the emitted source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04Absent is a forbidden substring of the emitted source, named by rule.
func t04Absent(t *testing.T, src, bad, what string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source still carries %q", what, bad)
	}
}

// t04Retire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module together with the floor those entries compute.
func t04Retire(t *testing.T, retired int) string {
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
	return t04Fixed(t, files)
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

// ---- dart/R3 -----------------------------------------------------------------

// t04R3 holds dart/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	text := t04Retire(t, 2)
	t04Has(t, text, "const int lineageFixedFloor = 2;", "R3 1 + the highest retired index")
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R3: lineageFixedLoad is not emitted")
	}
	t04Has(t, load, "if (pick < lineageFixedFloor) {", "R3 the floor check")
	t04Has(t, load, "report.layoutHash = hash;", "R3 below the floor reports the file's hash")
	t04Has(t, load, "report.refused = TableFixedRefusal.layoutUnsupported;", "R3 below the floor is layout_unsupported")
	t04Has(t, fixedRuntime, "int layoutHash = 0;", "R3 the report carries the file's hash")

	// ONE RETIRED ENTRY is a floor of ONE, and NOTHING RETIRED is a floor of
	// ZERO — the arithmetic §5.2 states, read off the emitted constant.
	one := t04Retire(t, 1)
	t04Has(t, one, "const int lineageFixedFloor = 1;", "R3 one retired entry is a floor of 1")
	none := t04Retire(t, 0)
	t04Has(t, none, "const int lineageFixedFloor = 0;", "R3 the floor is 0 when none is retired")
}

// ---- dart/R32 ----------------------------------------------------------------

// t04R32 holds dart/R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per entry (`// RETIRED`), the floor check sits
// ONCE, after the hash select and BEFORE the byte comparison or any record, and
// its whole body is the named refusal — a pure read of `pick`, so a second load
// of the same file answers the same name. REFUSE is TOTAL: report.reset zeroes
// every counter and the guarded body runs no plan.
func t04R32(t *testing.T) {
	text := t04Retire(t, 1)
	t04Has(t, text, "// RETIRED: the operator retired this layout", "R32 the retired entry's mark is laid down")
	t04Has(t, text, "const int lineageFixedFloor = 1;", "R32 the retired entry moves the floor")
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
	// refusal; nothing before it touches the plan, the destination or a counter.
	body := load[at:]
	if end := strings.Index(body, "\n  }"); end >= 0 {
		body = body[:end]
	}
	for _, bad := range []string{"report.unknown", "report.kindMismatch", "report.widened", "report.clamped", "plan.", "image[", "values["} {
		if strings.Contains(body, bad) {
			t.Errorf("R32: the floor refusal is not a pure named refusal, it touches %q:\n%s", bad, body)
		}
	}
	runtime := fixedRuntime
	for _, zero := range []string{
		"malformed = false;",
		"unknown = 0;",
		"kindMismatch = 0;",
		"clamped = 0;",
		"widened = 0;",
	} {
		t04Has(t, runtime, zero, "R32 REFUSE is TOTAL")
	}
}

// ---- dart/R10 -----------------------------------------------------------------

// t04R10 holds dart/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted load parses no layout (`TableFixedLayout` is the
// build-time plan's), walks no stranger's bytes, and never recomputes the
// header's hash: the header's eight bytes are taken as given and matched on.
func t04R10(t *testing.T) {
	text := t04Retire(t, 0)
	load := t04Fn(text, "lineageFixedLoad")
	if load == "" {
		t.Fatal("R10: lineageFixedLoad is not emitted")
	}
	t04Has(t, load, "if (layoutBytes != known.layoutBytes) {", "R10 the layout length is a comparison")
	t04Has(t, load, "bytes[TableFixedLimits.layoutAt + i] != known.layout[i]", "R10 the layout bytes are a comparison")
	t04Absent(t, load, "TableFixedLayout(", "R10 the load parses a stranger's layout")
	t04Absent(t, load, "hashOf(", "R10 the load recomputes the header's hash")
	// THE HASH FUNCTION IS STILL A WIRE IDENTITY: it survives in the runtime for
	// the writer's stamp and for any later reader, but no load path calls it.
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
	text := t04Retire(t, 0)
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
	selects := strings.Index(load, "final lane = lineageFixedLineagePlans[pick];")
	if selects >= 0 && selects < miss {
		t.Error("R15: a plan is selected before the hash is known")
	}
	t04Absent(t, load, "report.unknown += 1", "R15 an unknown field is dropped-and-counted per record on the read path")
	t04Has(t, load, "report.unknown += censusUnknown;", "R15 unknown is the compile census")
	// THE COUNT AND TEXT CLAMPS THAT REMAIN ARE THE WRITER'S BOUND carried by
	// the plan: `size` is the entry's writer-side bound, never the reader's.
	t04Has(t, fixedRuntime, "} else if (counted > size) {", "R15 the count clamp is the writer's bound")
	t04Has(t, fixedRuntime, "} else if (used > cap) {", "R15 the text clamp is the writer's span")
	// A VARIANT PAST THE WRITER'S SET LANDS None AND COUNTS, on the plan's own
	// writer-side remap — never a forward-read remap to a name the reader has.
	t04Has(t, fixedRuntime, "if (raw < 0 || raw > remap[remapAt]) {", "R15 a variant past the writer's set is None")
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
