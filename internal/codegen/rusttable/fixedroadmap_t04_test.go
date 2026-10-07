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

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/R3", check: t04R3},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
