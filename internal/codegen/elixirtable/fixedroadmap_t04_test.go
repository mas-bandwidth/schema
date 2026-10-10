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

// ---- elixir/R10 --------------------------------------------------------------

// t04R10 holds elixir/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted load parses no layout (the lock's own bytes are parsed
// once, at module load, by `R.lineage_init/7`) and never recomputes the header's
// hash: the header's eight bytes are taken as given and matched on, and §1.1's
// layout rules are not carried into the read path.
func t04R10(t *testing.T) {
	text := t04Retire(t, 0)
	for _, leaked := range []string{"parse_layout", "R.hash(", "check_entry", "check_shape", "known_kind", "leaf_size"} {
		if strings.Contains(text, leaked) {
			t.Errorf("R10: the emitted load carries a run-time walk of a stranger's layout (%q)", leaked)
		}
	}
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "def select(known, floor, hash, layout) do", "R10 the header's hash is taken as given")
	t04Has(t, runtime, "if byte_size(layout) == k.layout_bytes and layout == k.layout do", "R10 the layout is a byte comparison")
	t04Has(t, runtime, "THE HEADER'S HASH IS TAKEN AS", "R10 the hash is not re-derived from the wire")
	t04Has(t, runtime, "def hash(bytes) when is_binary(bytes), do: fnv(bytes, 0xCBF29CE484222325)", "R10 the hash function survives for the writer side")
	t04Has(t, text, "case R.select(@lineage_known, @lineage_floor, stated, layout) do", "R10 the emitted load takes the stated hash to the select")
	t04Has(t, text, "R.lineage_lane(__MODULE__, :lineage, i)", "R10 the plan is a lookup, not a walk")
}

// ---- elixir/R15 --------------------------------------------------------------

// t04R15 holds elixir/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any record and before any plan is
// selected, on the file's hash; the count and text clamps that REMAIN are the
// writer's bounds carried by the plan (bill §12.5), which only a backward read
// over a locked entry can reach; and the unknown-field count lands from the
// COMPILE census after a returning read, never per record.
func t04R15(t *testing.T) {
	text := t04Retire(t, 0)
	runtime := fixedRuntimeBody
	selectAt := strings.Index(text, "case R.select(@lineage_known, @lineage_floor, stated, layout) do")
	lane := strings.Index(text, "lineage_fixed_lane(i, records, stated, report, cap, bcap, copy)")
	run := strings.Index(text, "R.run(plan,")
	if selectAt < 0 || lane < 0 || run < 0 {
		t.Fatalf("R15: the emitted load's shape moved (%d, %d, %d)", selectAt, lane, run)
	}
	if !(selectAt < lane && lane < run) {
		t.Errorf("R15: a plan is selected before the hash is known (%d, %d, %d)", selectAt, lane, run)
	}
	t04Has(t, runtime, "{:error, :layout_newer, hash}", "R15 a hash the lineage does not hold is layout_newer")
	t04Has(t, runtime, "{:count, their_at, aux_at, their_n}", "R15 the count clamp is the writer's bound")
	t04Has(t, runtime, "units = min(size_at(mine, mi) - 4, size_at(theirs, ti) - 4)", "R15 the text clamp is the writer's span")
	t04Has(t, runtime, "if census, do: bump(report, :unknown), else: report", "R15 an unknown field is counted by the compile census")
	t04Has(t, runtime, "census and i == 0", "R15 once per FIELD per peer, not per element")
	t04Has(t, text, "R.census(report, census_u, census_k)", "R15 the census lands once, after the loop")
}

// ---- elixir/E3 ---------------------------------------------------------------

// t04E3 holds elixir/E3 to §5.2's EMIT ladder, in full: "if LADDER(te.kind,
// me.kind): emit widenf when te is f32 else widen, sign := te.kind is a signed
// integer or a signed fixed-point". Every required rung is present — 2..5, 6..9,
// 20..24, 25..29 and 10 -> 11 — the float rung is `widenf`, the sign is the
// WRITER's kind, and a same-kind widen zero-extends. The non-default and
// boundary values are written and read by the gate's corpus rows (`int_widen`,
// `uint_widen`, `float_widen`, `array_elem_widen`, `enum_width`), run by
// TestFixedVersioningNewReadsOld.
func t04E3(t *testing.T) {
	runtime := fixedRuntimeBody
	for _, rung := range []string{
		"defp widens?(from, to) when from >= 6 and from <= 9 and to >= 6 and to <= 9, do: to > from",
		"defp widens?(from, to) when from >= 2 and from <= 5 and to >= 2 and to <= 5, do: to > from",
		"defp widens?(from, to) when from >= 20 and from <= 24 and to >= 20 and to <= 24, do: to > from",
		"defp widens?(from, to) when from >= 25 and from <= 29 and to >= 25 and to <= 29, do: to > from",
		"defp widens?(from, to), do: from == 10 and to == 11",
	} {
		t04Has(t, runtime, rung, "E3 the widen ladder")
	}
	t04Has(t, runtime, "if their_kind == 10 do", "E3 the float rung is chosen by the writer's kind")
	t04Has(t, runtime, "{:widenf, their_at, at}", "E3 the float rung is widenF")
	t04Has(t, runtime, "defp signed_kind?(kind), do: (kind >= 2 and kind <= 5) or (kind >= 20 and kind <= 24)", "E3 a ladder widen's sign is the writer's kind")
	t04Has(t, runtime, "entry = {:widen, their_at, at, their_size, my_size, false}", "E3 a same-kind widen zero-extends")
	t04Has(t, runtime, "{acc, bump(report, :kind_mismatch)}", "E3 every other pair stays a kind mismatch")
}

// ---- elixir/C8 ---------------------------------------------------------------

// t04C8 holds elixir/C8 to §3.4's constant-size table and §5.2's fixed-point
// family: a `fixed(I,F)` rides as the raw scaled integer at its storage width,
// `I + F`, and the KIND encodes that width and the family and NOT `F` — so an
// `F`-only edit is a definitions-digest change the lock refuses, never a widen.
// The declared bounds are in VALUE units and the image is raw, so the clamp's
// ends are SHIFTED by F. `bits(N)` rides at its declared storage width and is
// RANGED over `[0, 2^N - 1]`, so a value past its own width clamps and counts
// (docs/SPEC-TABLES.md §3, §7 "a bits(N) value over its implied [0, 2^N − 1]
// clamps and counts").
func t04C8(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Lineage
{
    a fixed(4, 4) | min = -8, max = 7
    b fixed(2, 6) | min = -2, max = 1
    c fixed(8, 8) | min = -128, max = 127
    d ufixed(4, 4) | min = 0, max = 15
    e bits(12)
}
`)
	st := u.Tables["Lineage"]
	if st == nil {
		t.Fatal("C8: the fixture declares no Lineage table")
	}
	kind := map[string]int{}
	for _, f := range st.Fields {
		kind[f.Name] = ir.TableScalarKind(f)
	}
	if kind["a"] != 20 || kind["b"] != 20 {
		t.Errorf("C8: fixed(4,4) and fixed(2,6) are kinds %d and %d, want the same kind 20 — F does not ride in the kind", kind["a"], kind["b"])
	}
	if kind["c"] != 21 {
		t.Errorf("C8: fixed(8,8) is kind %d, want 21 (a 16-bit storage width)", kind["c"])
	}
	if kind["d"] != 25 {
		t.Errorf("C8: ufixed(4,4) is kind %d, want 25 (the unsigned family)", kind["d"])
	}
	if kind["e"] != 7 {
		t.Errorf("C8: bits(12) is kind %d, want 7 — a bits(N) rides at its storage width's integer kind", kind["e"])
	}
	if got := ir.TableFixedStorageBytes(st.Fields[4].Type); got != 4 {
		t.Errorf("C8: bits(12) storage = %d, want 4", got)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("C8: Generate: %v", err)
	}
	text := t04Fixed(t, files)
	// THE F-SHIFT: fixed(4,4) | min=-8,max=7 clamps at the RAW scaled ends
	// -128..112; the unsigned fixed(4,4) | 0..15 at 0..240.
	t04Has(t, text, "@r_lineage_a {-128, 112}", "C8 the F-shift scales the signed fixed-point bounds")
	t04Has(t, text, "@r_lineage_d {0, 240}", "C8 the F-shift scales the unsigned fixed-point bounds")
	// bits(12)'s own ceiling, and the fixed-point ladder's storage widths.
	t04Has(t, text, "4095", "C8 bits(12)'s own ceiling")
	t04Has(t, fixedRuntimeBody, "defp leaf_size(7, size), do: {true, size == 2 or size == 4}", "C8 the bits storage widths")
	t04Has(t, fixedRuntimeBody, "defp leaf_size(20, size), do: {true, size == 1}", "C8 the fixed-point ladder's bottom rung")
	t04Has(t, fixedRuntimeBody, "defp leaf_size(24, size), do: {true, size == 16}", "C8 the fixed-point ladder's top rung")
}

// ---- elixir/R20 --------------------------------------------------------------

// t04R20 holds elixir/R20 to SPEC-TABLES §21.2, clause by clause, on the emitted
// sites: an added field lands the prefill's declared default; a deprecated field
// is dropped and counted once per plan through the compile census; a narrower
// integer or float is widened exactly and counts widened; a shorter array or
// string lands with the reader's slack as the template's zeros; an older enum's
// ordinals are the reader's, its list a prefix.
func t04R20(t *testing.T) {
	text := t04Retire(t, 0)
	runtime := fixedRuntimeBody
	// AN ADDED FIELD: the holes are covered from the DEFAULTS image, which the
	// assembler splices every gap of.
	t04Has(t, text, "@lineage_prefill ", "R20 the prefill image is emitted")
	t04Has(t, text, "R.run(plan, R.detach(body, copy), @lineage_prefill, report)", "R20 an added field lands the declared default")
	// A DEPRECATED FIELD, counted ONCE per plan.
	t04Has(t, runtime, "if census, do: bump(report, :unknown), else: report", "R20 an unknown field is counted")
	t04Has(t, runtime, "census and i == 0", "R20 once per FIELD per peer, not per element")
	t04Has(t, text, "R.census(report, census_u, census_k)", "R20 the census lands once, after the loop")
	// A NARROWER INTEGER OR FLOAT: widened exactly, and counted.
	t04Has(t, runtime, "defp step([{:widen, src, dst, size, width, signed} | rest], body, writes, report) do", "R20 a narrower integer widens")
	t04Has(t, runtime, "defp step([{:widenf, src, dst} | rest], body, writes, report) do", "R20 a narrower float widens")
	if n := strings.Count(runtime, "bump(report, :widened)"); n < 2 {
		t.Errorf("R20: widened counts at %d sites, want the two rungs", n)
	}
	// A SHORTER ARRAY OR STRING: the image is the template's zeros and the plan
	// writes only the writer's live extent.
	t04Has(t, runtime, "gap = binary_part(prefill, pos, dst - pos)", "R20 the reader's slack is the template's zeros")
	t04Has(t, runtime, "defp splice([], pos, prefill, acc) do", "R20 the image is spliced onto the prefill")
	// AN OLDER ENUM: the remap is by NAME and its list is the writer's count, so
	// a prefix maps position for position.
	t04Has(t, runtime, "their_id = id_at(theirs, ti + 1 + j)", "R20 the ordinal is resolved through the plan's remap")
	t04Has(t, runtime, "{:ordinal, their_at, at, size_at(theirs, ti), size_at(mine, mi), List.to_tuple(remap)}", "R20 the older enum's ordinals are the reader's")
}

// ---- elixir/R21 --------------------------------------------------------------

// t04R21 holds elixir/R21 to §4's float rung and §5.2's table, and to
// docs/PORTING.md M21: "a backend that holds a float32 in a WIDER cell moves the
// pattern BIT FOR BIT and never through a float conversion", because "the
// hardware conversion sets the QUIET BIT on a signalling NaN and drops a payload
// the narrower cell would have kept". The emitted runtime takes the sign, an
// all-ones exponent and the 23 payload bits into the top of the double's 52, and
// the quiet bit rides exactly as the writer wrote it.
func t04R21(t *testing.T) {
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "defp widen_float({:nonfinite, bits}) do", "R21 the all-ones exponent is handled on the bits")
	t04Has(t, runtime, "sign = bits >>> 31 &&& 1", "R21 the sign is carried")
	t04Has(t, runtime, "mantissa = bits &&& 0x7FFFFF", "R21 the 23 payload bits are carried")
	t04Has(t, runtime, "wide = sign <<< 63 ||| 0x7FF0000000000000 ||| mantissa <<< 29", "R21 the payload rides in the top of the double's 52")
	if strings.Contains(runtime, "0x8000000000000") {
		t.Error("R21: the NaN widening sets the quiet bit; the pattern the writer wrote must ride unchanged (docs/PORTING.md M21)")
	}
	t04Has(t, runtime, "defp widen_float(value), do: value", "R21 every other exponent converts exactly")
}

// ---- elixir/R29 --------------------------------------------------------------

// t04R29 holds elixir/R29 to the 65536 band and to bill §12.5's carried writer
// bounds. THE BAND: a record body AT 65536 is a fixed root and one byte past it
// is not (SPEC-TABLES §3.4, "65536 BYTES OF RECORD BODY: THE FORM IS NOT
// EMITTED, AND THE TABLE IS NAMED"), and a layout entry reaching past the
// writer's own declared record refuses the plan WHOLE under
// layout_record_too_large. THE WRITER'S BOUNDS: the count op clamps to the
// writer's element bound carried by the plan and the text op to the writer's
// span, so a value forged past what the WRITER could have written clamps and
// counts while a value the reader merely widened does not.
func t04R29(t *testing.T) {
	if ir.TableFixedRecordMaxBytes != 65536 {
		t.Fatalf("R29: the ceiling is %d, want 65536", ir.TableFixedRecordMaxBytes)
	}
	at := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    blob bytes(65532)\n}\n")
	past := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    blob bytes(65533)\n}\n")
	if got := ir.TableFixedTypeBytes(at.Tables["Lineage"]); got != 65536 {
		t.Fatalf("R29: the AT fixture body is %d, want 65536", got)
	}
	found := false
	for _, r := range ir.TableFixedFormRoots(at) {
		if r.Name == "Lineage" {
			found = true
		}
	}
	if !found {
		t.Error("R29: a body AT 65536 is not a fixed-form root")
	}
	for _, r := range ir.TableFixedFormRoots(past) {
		if r.Name == "Lineage" {
			t.Error("R29: a body one past 65536 is still a fixed-form root")
		}
	}
	warnings, errs := ir.TableFixedRecordBounds(at, 0)
	if len(errs) != 0 {
		t.Errorf("R29: a body at the ceiling is a warning and not a refusal: %v", errs)
	}
	if joined := strings.Join(warnings, "\n"); !strings.Contains(joined, "Lineage") {
		t.Errorf("R29: the warning does not name the table: %s", joined)
	}
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "@record_max 65536", "R29 the reader holds the peer's layout to the same ceiling")
	t04Has(t, runtime, "defp layout_root({_id, 13, size, _children}) when size > 0 and size <= @record_max", "R29 the zero root and the ceiling")
	t04Has(t, runtime, "defp any_past?(entries, record), do: Enum.any?(entries, fn e -> src_end(e) > record end)", "R29 an entry past the writer's declared record")
	t04Has(t, runtime, "{:count, their_at, aux_at, their_n}", "R29 the count clamps to the writer's bound")
	t04Has(t, runtime, "units = min(size_at(mine, mi) - 4, size_at(theirs, ti) - 4)", "R29 the text's cap is the writer's span")
}

// ---- elixir/E5 ---------------------------------------------------------------

// t04E5 holds elixir/E5 to SPEC-TABLES §3.4's kind 35: "ON THIS FORM THEY ARE
// ONE BYTE APART ... the edit reads as kind_mismatch and the field takes its
// declared default", so `?T` and a plain `T` nesting are distinguishable in the
// layout, the wrapper is one present byte plus its child, and `T` into `?T`
// lands a CONSTANT present 1 under the row's own guard.
func t04E5(t *testing.T) {
	if ir.TableKindOptional != 35 {
		t.Fatalf("E5: TableKindOptional = %d, want 35", ir.TableKindOptional)
	}
	plain := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    link int32\n}\n")
	optional := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	if got := ir.TableFixedTypeBytes(plain.Tables["Lineage"]); got != 4 {
		t.Errorf("E5: a plain int32 nested body is %d, want 4", got)
	}
	if got := ir.TableFixedTypeBytes(optional.Tables["Lineage"]); got != 5 {
		t.Errorf("E5: a ?int32 body is %d, want 5 — one present byte plus the payload", got)
	}
	w := ir.TableFixedWalkRoot(plain.Tables["Lineage"])
	if len(w) < 2 || w[1].Kind != ir.TableKindI32 {
		t.Errorf("E5: a plain int32 walks as %v, want the field at kind %d", w, ir.TableKindI32)
	}
	wo := ir.TableFixedWalkRoot(optional.Tables["Lineage"])
	if len(wo) < 2 || wo[1].Kind != ir.TableKindOptional {
		t.Errorf("E5: a ?int32 walks as %v, want the field at kind %d", wo, ir.TableKindOptional)
	}
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "defp check_composite(35, size, children, facts) do", "E5 the wrapper is its own layout kind")
	t04Has(t, runtime, "size != facts.sum + 1 -> {:error, :layout_size_mismatch}", "E5 the wrapper is one present byte plus its child")
	t04Has(t, runtime, "my_kind == 35 and their_kind != 35 ->", "E5 T into ?T is its own emit")
	t04Has(t, runtime, "{:const, aux_at, 1, 1}", "E5 the present companion lands a constant 1")
}

// ---- elixir/W2 ---------------------------------------------------------------

// t04W2 holds elixir/W2 to SPEC-TABLES §3.4's present rule: "the payload rides
// WHOLE whether or not it is present; ZERO on write when the flag is 0". The
// emitted write body stores the flag and then the payload behind ONE `if`, so
// an absent optional costs the branch and not one store — the template's zeros
// are what rides, and a caller's untouched payload storage never reaches the
// wire.
func t04W2(t *testing.T) {
	text, _ := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    link int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	t04Has(t, text, "if(v_link_present, do: 1, else: 0)::unsigned-8", "W2 the present flag is stored")
	guard := strings.Index(text, "if v_link_present do")
	if guard < 0 {
		t.Fatalf("W2: an absent optional does not guard its store:\n%s", text)
	}
	if payload := strings.Index(text, "R.fits(v_link, 32, true)::little-signed-32"); payload < guard {
		t.Error("W2: the payload's store is not behind the present guard")
	}
	t04Has(t, text, "0::size(4)-unit(8)", "W2 an absent optional rides the template's zeros")
	if n := strings.Count(text, "if v_link_present do"); n != 1 {
		t.Errorf("W2: the absent-optional guard appears %d times, want once", n)
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
		{id: "elixir/R10", check: t04R10},
		{id: "elixir/R15", check: t04R15},
		{id: "elixir/E3/other-required-widens", check: t04E3},
		{id: "elixir/C8", check: t04C8},
		{id: "elixir/R20", check: t04R20},
		{id: "elixir/R21", check: t04R21},
		{id: "elixir/R29", check: t04R29},
		{id: "elixir/E5", check: t04E5},
		{id: "elixir/W2", check: t04W2},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
