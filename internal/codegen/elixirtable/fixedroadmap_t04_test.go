package elixirtable

// THE ROADMAP'S VERSIONING TASKS ON THE ELIXIR LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize*. A
// clause that needs the emitted code to RUN is asserted at the emitted site
// that runs it, and that site is named in the check — the shape
// fixedroadmap_t03_test.go states for the same reason.
//
// THE FLOOR'S OWN RUNTIME IS ALREADY HELD by fixedversioning_test.go's
// `TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live}` probes,
// which a gate runs (`make tables-elixir-versioning`). The subtests below carry
// the clauses those probes cannot read off the bytes: the floor's ARITHMETIC in
// the emitter, and the refusal's position and purity in the emitted load.
//
// elixir/R3 [verify]: the floor is 1 + the highest retired index (0 when none);
// below the floor is layout_unsupported, reporting the file's hash.
//
// elixir/R32 [weak]: retire for real: a retired version is refused by name,
// once, idempotently.
//
// elixir/R10 [verify]: the run-time walk of a stranger's layout and the
// recompute of the header's hash are retired.
//
// elixir/R15 [verify]: the four forward-read clamps are retired — count clamp
// across bounds, range clamp across versions, remap of an unknown variant to
// None, drop-and-count of an unknown field: each is layout_newer now.
//
// elixir/E3/other-required-widens [capability]: all other required widen-ladder
// cases.
//
// elixir/C8 [owed]: fixed-point F-shift / bits(N).
//
// elixir/R20 [weak]: §21.2's landing rules.
//
// elixir/R21 [weak]: widenf is the bit-exact widening — signalling NaNs kept,
// the quiet bit carried as the writer wrote it.
//
// elixir/R29 [owed]: the band case: the widening across the 65536 ceiling, and
// the bounds pass clamping to the writer's bounds.
//
// elixir/E5 [verify]: ?T vs plain nesting.
//
// elixir/W2 [verify]: absent optional skips store.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures and readers of this leg's output ------------------------------

// t04Fixed is the emitted fixed module of a unit: the one <Base>Fixed.ex the
// table backend produced.
func t04Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// t04Has is a required substring of the emitted source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t04Part is the slice of `src` from `start` to the first `stop` behind it, the
// body of one emitted function read by its own words.
func t04Part(t *testing.T, src, start, stop string) string {
	t.Helper()
	at := strings.Index(src, start)
	if at < 0 {
		t.Fatalf("%q is not emitted", start)
	}
	rest := src[at:]
	if e := strings.Index(rest, stop); e >= 0 {
		return rest[:e]
	}
	return rest
}

// t04Retire builds THREE generations of one table — x, x+y, x+y+z — hands the
// first `retired` of the first two in as the lock's lineage, and answers the
// emitted module.
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
func t04Lineage(t *testing.T, oldSrc, newSrc string) string {
	t.Helper()
	older := unitFrom(t, oldSrc)
	own := unitFrom(t, newSrc)
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
	return t04Fixed(t, files)
}

// ---- elixir/R3 -----------------------------------------------------------------

// t04R3 holds elixir/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is TestFixedVersioningFloor/floor_below; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	text := t04Retire(t, 2)
	t04Has(t, text, "@lineage_floor 2", "R3 1 + the highest retired index")
	t04Has(t, text, "@lineage_retired [{0x", "R3 the retired hashes are named")
	t04Has(t, text, "the operator retired this layout", "R3 the operator's own sentence for each")
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "def select(known, floor, hash, layout) do", "R3 the lineage select")
	t04Has(t, runtime, "i when i < floor ->", "R3 the floor check")
	t04Has(t, runtime, "{:error, :layout_unsupported, hash}", "R3 below the floor is layout_unsupported, reporting the file's hash")

	// NOTHING RETIRED is a floor of ZERO, exactly as §5.2 computes it.
	none := t04Retire(t, 0)
	t04Has(t, none, "@lineage_floor 0", "R3 the floor is 0 when none is retired")
}

// ---- elixir/R32 ----------------------------------------------------------------

// t04R32 holds elixir/R32 to §5.2's retirement, read off the emitted module and
// the runtime's select: the operator's mark is laid down per entry (a RETIRED
// comment and the retired list), the floor check sits ONCE, after the hash
// select and BEFORE the byte comparison, and its whole body is the named
// refusal — a pure read of `i` and the file's hash, so a second load of the same
// file answers the same name. REFUSE is TOTAL: the report starts at zero and no
// counter moves.
func t04R32(t *testing.T) {
	text := t04Retire(t, 1)
	t04Has(t, text, "# RETIRED: the operator retired this layout", "R32 the retired entry's mark is laid down")
	t04Has(t, text, "@lineage_retired [{0x", "R32 the retired hashes are named with the operator's sentence")
	runtime := fixedRuntimeBody
	selectBody := t04Part(t, runtime, "def select(known, floor, hash, layout) do", "\n  end\n")
	if n := strings.Count(selectBody, "i when i < floor ->"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	at := strings.Index(selectBody, "i when i < floor ->")
	known := strings.Index(selectBody, "k = elem(known, i)")
	bytes := strings.Index(selectBody, "if byte_size(layout) == k.layout_bytes and layout == k.layout do")
	if at < 0 || known < 0 || bytes < 0 {
		t.Fatalf("R32: the select's shape moved (%d, %d, %d)", at, known, bytes)
	}
	if !(at < known && known < bytes) {
		t.Errorf("R32: the floor check is not before the byte comparison (%d, %d, %d)", at, known, bytes)
	}
	for _, zero := range []string{"malformed: false", "unknown: 0", "kind_mismatch: 0", "clamped: 0", "widened: 0"} {
		t04Has(t, runtime, zero, "R32 REFUSE is TOTAL")
	}
	t04Has(t, text, "%{report | layout_hash: file_hash}", "R32 the refusal reports the file's hash and touches nothing else")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapElixirT04Versions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/R3", check: t04R3},
		{id: "elixir/R32", check: t04R32},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
