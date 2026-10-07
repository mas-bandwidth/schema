package rusttable

// THE ROADMAP'S VERSIONING TASKS ON THE RUST LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs. A
// clause that needs the emitted code to RUN is asserted at the emitted site
// that runs it, and that site is named in the check — the shape
// fixedroadmap_t02_test.go states for the same reason.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// `TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live}` probes,
// which a gate runs (`make tables-rust-versioning`). The subtests below carry
// the clauses those probes cannot read off the bytes: the floor's ARITHMETIC
// in the emitter, and the refusal's position and purity in the emitted load.
//
// rust/R3 [verify]: the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash.
//
// rust/R32 [verify]: retire for real: a retired version is refused by name,
// once, idempotently.
//
// rust/R10 [verify]: the run-time walk of a stranger's layout and the recompute
// of the header's hash are retired.
//
// rust/R15 [verify]: the four forward-read clamps are retired — count clamp
// across bounds, range clamp across versions, remap of an unknown variant to
// None, drop-and-count of an unknown field: each is layout_newer now.
//
// rust/E3/other-required-widens [capability]: all other required widen-ladder
// cases.
//
// rust/C8 [weak]: fixed-point F-shift / bits(N).
//
// rust/R20 [verify]: §21.2's landing rules.
//
// rust/R21 [owed]: widenf is the bit-exact widening — signalling NaNs kept, the
// quiet bit carried as the writer wrote it.
//
// rust/R29 [owed]: the band case: the widening across the 65536 ceiling, and
// the bounds pass clamping to the writer's bounds.
//
// rust/E5 [verify]: ?T vs plain nesting.
//
// rust/W2 [owed]: absent optional skips store.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t04Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t04Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// t04Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t04Fn(text, name string) string {
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

// t04Has is a required substring of the emitted source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04Retire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module together with the entries it was handed.
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

// ---- rust/R3 -----------------------------------------------------------------

// t04R3 holds rust/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	text := t04Retire(t, 2)
	t04Has(t, text, "LINEAGE_FIXED_FLOOR: usize = 2;", "R3 1 + the highest retired index")
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R3: lineage_fixed_load is not emitted")
	}
	t04Has(t, load, "if found < LINEAGE_FIXED_FLOOR {", "R3 the floor check")
	t04Has(t, load, "return report.refuse_layout(TableFixedReason::LayoutUnsupported, hash);", "R3 below the floor is layout_unsupported")
	if !strings.Contains(string(fixedRuntimeBody), "self.layout_hash = hash;") {
		t.Error("R3: refuse_layout does not report the file's hash")
	}

	// NOTHING RETIRED is a floor of ZERO and a comparison the emitted code does
	// not carry, exactly as §5.2 computes it and as the emitted comment says.
	none := t04Retire(t, 0)
	t04Has(t, none, "LINEAGE_FIXED_FLOOR: usize = 0;", "R3 the floor is 0 when none is retired")
	noneLoad := t04Fn(none, "lineage_fixed_load")
	if strings.Contains(noneLoad, "found < LINEAGE_FIXED_FLOOR") {
		t.Error("R3: a floor of 0 still emits a comparison no value can satisfy")
	}
}

// ---- rust/R32 ----------------------------------------------------------------

// t04R32 holds rust/R32 to §5.2's retirement, read off the emitted load: the
// operator's mark is laid down per entry (`retired: true`), the floor check
// sits ONCE, after the hash select and BEFORE the byte comparison or any
// record, and its whole body is the named refusal — a pure read of `found`, so
// a second load of the same file answers the same name. REFUSE is TOTAL: the
// runtime's refuse zeroes every counter and no plan runs.
func t04R32(t *testing.T) {
	text := t04Retire(t, 1)
	t04Has(t, text, "retired: true,", "R32 the retired entry's mark is laid down")
	t04Has(t, text, "retired: false,", "R32 the current entry is not retired")
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R32: lineage_fixed_load is not emitted")
	}
	if n := strings.Count(load, "found < LINEAGE_FIXED_FLOOR"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(load, "found < LINEAGE_FIXED_FLOOR")
	known := strings.Index(load, "let known = &LINEAGE_FIXED_LINEAGE[found];")
	bytes := strings.Index(load, "if block != known.layout {")
	records := strings.Index(load, "for k in 0..n {")
	if at < 0 || known < 0 || bytes < 0 || records < 0 {
		t.Fatalf("R32: the load's shape moved (%d, %d, %d, %d)", at, known, bytes, records)
	}
	if !(at < known && known < bytes && bytes < records) {
		t.Errorf("R32: the floor check is not before the byte comparison and the record loop (%d, %d, %d, %d)", at, known, bytes, records)
	}
	// ONCE AND IDEMPOTENT: the guarded body reads `found` and returns the named
	// refusal; nothing before it mutates the report or the destination.
	body := load[at:]
	if end := strings.Index(body, "\n    }"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "report.unknown") || strings.Contains(body, "image[") || strings.Contains(body, "values[") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	runtime := string(fixedRuntimeBody)
	for _, zero := range []string{"self.unknown = 0;", "self.kind_mismatch = 0;", "self.widened = 0;", "self.clamped = 0;", "self.malformed = false;"} {
		t04Has(t, runtime, zero, "R32 REFUSE is TOTAL")
	}
}

// ---- rust/R10 -----------------------------------------------------------------

// t04R10 holds rust/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted load parses no layout (`TableFixedBlock::parse` is the
// build-time LazyLock's), walks no stranger's bytes, and never recomputes the
// header's hash: the header's eight bytes are taken as given and matched on.
func t04R10(t *testing.T) {
	text := t04Retire(t, 0)
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R10: lineage_fixed_load is not emitted")
	}
	t04Has(t, load, "if block != known.layout {", "R10 the layout is a byte comparison")
	t04Has(t, load, "return report.refuse(TableFixedReason::LayoutMalformed);", "R10 a lie about a known version is layout_malformed")
	if strings.Contains(load, "TableFixedBlock::parse") {
		t.Error("R10: the load walks a stranger's layout; the walk is the build's LazyLock")
	}
	if strings.Contains(load, "table_fixed_hash(") {
		t.Error("R10: the load recomputes the header's hash")
	}
	if strings.Contains(load, "check_entry") || strings.Contains(load, "known_kind") || strings.Contains(load, "leaf_size") {
		t.Error("R10: the load carries §1.1's layout rules into the run time")
	}
	// THE HASH IS STILL A WIRE IDENTITY: the function survives for the writer side
	// and for any later reader, but no load path calls it.
	t04Has(t, string(fixedRuntimeBody), "pub fn table_fixed_hash(block: &[u8]) -> u64 {", "R10 the hash function")
}

// ---- rust/R15 -----------------------------------------------------------------

// t04R15 holds rust/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan
// selection, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan (bill §12.5), which only a backward read
// over a locked entry can reach; and the unknown-field count lands from the
// COMPILE census after a returning read, never per record.
func t04R15(t *testing.T) {
	text := t04Retire(t, 0)
	load := t04Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatal("R15: lineage_fixed_load is not emitted")
	}
	miss := strings.Index(load, "return report.refuse_layout(TableFixedReason::LayoutNewer, hash);")
	records := strings.Index(load, "for k in 0..n {")
	if miss < 0 || records < 0 {
		t.Fatalf("R15: the load's refusals moved (%d, %d)", miss, records)
	}
	if miss > records {
		t.Error("R15: an unknown hash is not refused before the first record")
	}
	if strings.Contains(load, "let identity = found ==") && strings.Index(load, "let identity") < miss {
		t.Error("R15: a plan is selected before the hash is known")
	}
	if strings.Contains(load, "report.unknown += 1") {
		t.Error("R15: an unknown field is dropped-and-counted per record on the read path")
	}
	t04Has(t, load, "report.unknown += census.0;", "R15 unknown is the compile census")
	runtime := string(fixedRuntimeBody)
	t04Has(t, runtime, "let held = raw.clamp(0, p.size as i32);", "R15 the count clamp is the writer's bound")
	t04Has(t, runtime, "let held = raw.clamp(0, p.aux as i32);", "R15 the text clamp is the writer's span")
	if strings.Contains(runtime, "remap.get(base).unwrap_or") {
		t.Error("R15: a forward-read remap survives in the runtime")
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/R3", check: t04R3},
		{id: "rust/R32", check: t04R32},
		{id: "rust/R10", check: t04R10},
		{id: "rust/R15", check: t04R15},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
