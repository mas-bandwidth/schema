package elixirtable

// THE ROADMAP'S VERSIONING TASKS ON THE ELIXIR LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2 to §5.7;
// docs/FIXED-FORM-VERSIONING-TESTS.md "The floor and the hash" and
// "The old contract's tests, retired by name"; docs/SPEC-TABLES.md §3.4 and
// §21.2). One subtest per task id, table-driven, t.Parallel() first, every
// assertion reading the Elixir this leg emits and the shared runtime body it
// emits with — so the harness is reachable on a tree that never built the C++
// reference corpus and never carries a sibling runtime checkout. A clause that
// needs the emitted code to RUN is named by the existing test that runs it
// (fixedversioning_test.go), which is where this leg's probe harness lives.
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
// elixir/E3/other-required-widens [capability]: all other required
// widen-ladder cases.
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
// table backend produced, never the shared FixedRuntime.ex beside it.
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
func t04Lineage(t *testing.T, oldSrc, newSrc string) (string, *ir.Unit) {
	t.Helper()
	older := unitFrom(t, oldSrc)
	own := unitFrom(t, newSrc)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range fixedRoots(older) {
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

// ---- elixir/R3 ---------------------------------------------------------------

// t04R3 holds elixir/R3 to §5.2's one number: "R.floor := 1 + the highest index
// marked RETIRED in lock.lineage(T), or 0 when none is", emitted beside the
// lineage, and to LOAD step 6: "if i < R.floor: REFUSE layout_unsupported,
// reporting h". The runtime half is
// TestFixedVersioningFloor/{floor_at,floor_below,floor_raise_live}; the
// arithmetic and the emitted site are read here.
func t04R3(t *testing.T) {
	two := t04Retire(t, 2)
	t04Has(t, two, "@lineage_floor 2", "R3 1 + the highest retired index (two retired)")
	one := t04Retire(t, 1)
	t04Has(t, one, "@lineage_floor 1", "R3 1 + the highest retired index (one retired)")
	none := t04Retire(t, 0)
	t04Has(t, none, "@lineage_floor 0", "R3 the floor is 0 when none is retired")

	runtime := fixedRuntimeBody
	t04Has(t, runtime, "case index_of(known, hash, 0) do", "R3 the hash is looked up")
	t04Has(t, runtime, "i when i < floor ->", "R3 below the floor")
	t04Has(t, runtime, "{:error, :layout_unsupported, hash}", "R3 below the floor is layout_unsupported, reporting the file's hash")
	t04Has(t, none, "R.select(@lineage_known, @lineage_floor, stated, layout)", "R3 the emitted load selects through the floor")
	t04Has(t, none, "%{report | layout_hash: file_hash}", "R3 both layout refusals report the file's hash")
}

// ---- elixir/R32 --------------------------------------------------------------

// t04R32 holds elixir/R32 to §5.2's retirement, read off the emitted load and
// the runtime select: the operator's mark is laid down per entry (a `# RETIRED:`
// comment and the hash/reason pair in @<snake>_retired), the floor check sits
// ONCE, after the hash lookup and BEFORE the byte comparison or any record, and
// its whole body is the named refusal — a pure read of `floor`, so a second
// load of the same file answers the same name. REFUSE is TOTAL: the report is
// built zeroed and the refusal never touches it.
func t04R32(t *testing.T) {
	text := t04Retire(t, 1)
	t04Has(t, text, "# RETIRED: the operator retired this layout", "R32 the retired entry's mark is laid down")
	t04Has(t, text, "@lineage_retired [{0x", "R32 the retired hash and its reason are laid down")
	t04Has(t, text, `"the operator retired this layout"`, "R32 the operator's own sentence rides with the hash")

	runtime := fixedRuntimeBody
	if n := strings.Count(runtime, "i when i < floor ->"); n != 1 {
		t.Errorf("R32: the floor is tested %d times, want once", n)
	}
	t04Has(t, runtime, "i when i < floor ->\n        {:error, :layout_unsupported, hash}", "R32 the floor's whole body is the named refusal")

	sel := strings.Index(runtime, "case index_of(known, hash, 0) do")
	newer := strings.Index(runtime, "{:error, :layout_newer, hash}")
	at := strings.Index(runtime, "i when i < floor ->")
	bytes := strings.Index(runtime, "if byte_size(layout) == k.layout_bytes and layout == k.layout do")
	if sel < 0 || newer < 0 || at < 0 || bytes < 0 {
		t.Fatalf("R32: the select's shape moved (%d, %d, %d, %d)", sel, newer, at, bytes)
	}
	if !(sel < newer && newer < at && at < bytes) {
		t.Errorf("R32: the floor is not after the hash lookup and before the byte comparison (%d, %d, %d, %d)", sel, newer, at, bytes)
	}

	// ONCE AND IDEMPOTENT: the guarded body reads `floor` and returns the named
	// refusal; nothing before it mutates the report or a destination.
	body := runtime[at:]
	if end := strings.Index(body, "\n      i ->"); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "report") || strings.Contains(body, "bump(") || strings.Contains(body, "value") {
		t.Errorf("R32: the floor refusal is not a pure named refusal:\n%s", body)
	}
	for _, zero := range []string{"malformed: false", "unknown: 0", "kind_mismatch: 0", "clamped: 0", "widened: 0", "duplicate: 0"} {
		t04Has(t, runtime, zero, "R32 REFUSE is TOTAL")
	}
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
