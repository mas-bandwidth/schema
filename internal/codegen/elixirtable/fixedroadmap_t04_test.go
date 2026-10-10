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

// ---- elixir/R10 -----------------------------------------------------------------

// t04R10 holds elixir/R10 to §5.6 and §5.3 step 7: "the hash is looked up, the
// floor is checked, and the layout is COMPARED — the seven rules do not fire at
// run time". The emitted records path parses no layout (`parse_layout` is the
// module-load lane's, over the LOCK's bytes) and never recomputes the header's
// hash: the header's eight bytes are taken as given and matched on.
func t04R10(t *testing.T) {
	text := t04Retire(t, 0)
	records := t04Part(t, text, "defp lineage_fixed_records(", "\n  end\n")
	t04Has(t, records, "case R.select(@lineage_known, @lineage_floor, stated, layout) do", "R10 the hash is looked up and the layout compared")
	if strings.Contains(records, "parse_layout") {
		t.Error("R10: the records path parses a stranger's layout")
	}
	if strings.Contains(records, "R.hash(") {
		t.Error("R10: the records path recomputes the header's hash")
	}
	if strings.Contains(text, "R.parse_layout") {
		t.Error("R10: the emitted module walks a stranger's layout")
	}
	if strings.Contains(text, "R.hash(") {
		t.Error("R10: the emitted module recomputes the header's hash")
	}
	runtime := fixedRuntimeBody
	selectBody := t04Part(t, runtime, "def select(known, floor, hash, layout) do", "\n  end\n")
	t04Has(t, selectBody, "if byte_size(layout) == k.layout_bytes and layout == k.layout do", "R10 the layout is a byte comparison")
	if strings.Contains(selectBody, "parse_layout") {
		t.Error("R10: select walks a stranger's layout")
	}
	t04Has(t, runtime, "{:error, :layout_malformed, 0}", "R10 a lie about a known version is layout_malformed")
	t04Has(t, runtime, "def hash(bytes) when is_binary(bytes), do: fnv(bytes, 0xCBF29CE484222325)", "R10 the hash survives for the writer side")
	t04Has(t, text, "{:ok, stated, layout, records} ->", "R10 the header's hash is taken as given")
}

// ---- elixir/R15 -----------------------------------------------------------------

// t04R15 holds elixir/R15 to §5.6's retirement list and §5.3 step 5: the four
// forward-read clamps are gone, and each is `layout_newer` now. A hash the
// lineage does not hold is refused BEFORE any plan or record, on the file's
// hash; the count clamp that REMAINS is the writer's bound carried by the plan
// (bill §12.5); an unknown variant lands None through the plan's OWN remap and
// counts; and the unknown-field count lands from the COMPILE census after a
// returning read, never per record.
func t04R15(t *testing.T) {
	text := t04Retire(t, 0)
	records := t04Part(t, text, "defp lineage_fixed_records(", "\n  end\n")
	miss := strings.Index(records, "{:error, why, file_hash} ->")
	ok := strings.Index(records, "{:ok, i} ->")
	run := strings.Index(records, "lineage_fixed_lane(")
	if miss < 0 || ok < 0 || run < 0 {
		t.Fatalf("R15: the records path moved (%d, %d, %d)", miss, ok, run)
	}
	if !(miss < ok && ok < run) {
		t.Errorf("R15: an unknown hash is not refused before any plan or record (%d, %d, %d)", miss, ok, run)
	}
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "{:error, :layout_newer, hash}", "R15 a hash the lineage does not hold is layout_newer")
	if strings.Contains(runtime, "{:clamp,") {
		t.Error("R15: a forward-read clamp op survives in the plan")
	}
	t04Has(t, runtime, "defp clamp_count(raw, max, report) do", "R15 the surviving count clamp")
	t04Has(t, runtime, "raw < 0 -> {0, bump(report, :clamped)}", "R15 the count clamp counts")
	t04Has(t, runtime, "raw > max -> {max, bump(report, :clamped)}", "R15 the count clamp is the writer's bound")
	t04Has(t, runtime, "defp ordinal(raw, variants, report) when is_integer(variants) do", "R15 an unknown variant lands None through the writer's own count")
	t04Has(t, runtime, "if raw <= variants, do: {raw, report}, else: {0, bump(report, :clamped)}", "R15 the variant clamp counts once")
	t04Has(t, runtime, "nil -> if census, do: bump(report, :unknown), else: report", "R15 an unknown field is the compile census, not a per-record count")
	t04Has(t, runtime, "def census(report, 0, 0), do: report", "R15 the census lands once")
	if n := strings.Count(text, "R.census(report, census_u, census_k)"); n != 1 {
		t.Errorf("R15: the census lands %d times, want once after the loop", n)
	}
}

// ---- elixir/E3 ------------------------------------------------------------------

// t04E3 holds elixir/E3 to §5.2's EMIT ladder, in full: "if LADDER(te.kind,
// me.kind): emit widenf when te is f32 else widen, sign := te.kind is a signed
// integer or a signed fixed-point". Every required rung is present — 2..5, 6..9,
// 20..24, 25..29 and 10 -> 11 — the float rung is `widenf`, the sign is the
// WRITER's kind, and a same-kind widen zero-extends. The non-default and
// boundary values are written and read by the gate's corpus rows
// (`int_widen`, `uint_widen`, `float_widen`, `array_elem_widen`, `enum_width`),
// which TestFixedVersioningNewReadsOld runs.
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
	t04Has(t, runtime, "{:widen, their_at, at, their_size, my_size, signed_kind?(their_kind)}", "E3 the integer rung")
	t04Has(t, runtime, "defp signed_kind?(kind), do: (kind >= 2 and kind <= 5) or (kind >= 20 and kind <= 24)", "E3 a ladder widen's sign is the writer's kind")
	t04Has(t, runtime, "{:widen, their_at, at, their_size, my_size, false}", "E3 a same-kind widen zero-extends")
}

// ---- elixir/C8 ------------------------------------------------------------------

// t04C8 holds elixir/C8 to §3.4's constant-size table and §5.2's fixed-point
// family: a `fixed(I,F)` rides as the raw scaled integer at its storage width,
// `I + F`, and the KIND encodes that width and the family and NOT `F` — so an
// `F`-only edit is a definitions-digest change the lock refuses, never a widen.
// The declared real bounds are the raw bounds SHIFTED BY F at the clamp and at
// the landed value. `bits(N)` rides at its declared storage width and is not a
// storage-limit clamp.
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
	t04Has(t, text, "c = R.clamps(c, f_a, -128, 112)", "C8 the F-shift: fixed(4,4) min=-8 max=7 as raw")
	t04Has(t, text, "c = R.clamps(c, f_b, -128, 64)", "C8 the F-shift: fixed(2,6) min=-2 max=1 as raw")
	t04Has(t, text, "min(max(f_a, -128), 112)", "C8 the landed raw value is held to the shifted bounds")
}

// ---- elixir/R20 -----------------------------------------------------------------

// t04R20 holds elixir/R20 to SPEC-TABLES §21.2, clause by clause, on the emitted
// sites: an added field lands the prefill's declared default; a deprecated field
// is dropped and counted once per plan through the compile census; a narrower
// integer or float is widened exactly and counts widened; a shorter array or
// string lands with the reader's slack as the template's zeros; an older enum's
// ordinals are the reader's, its list a prefix.
func t04R20(t *testing.T) {
	text := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n    w int32 = 7\n}\n")
	t04Has(t, text, "@lineage_prefill ", "R20 an added field lands the declared default")
	t04Has(t, text, "R.run(plan, R.detach(body, copy), @lineage_prefill, report)", "R20 the prefill image is what fills the gap")
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "defp assemble(writes, prefill) do", "R20 the image is assembled over the prefill")
	t04Has(t, runtime, "defp splice([], pos, prefill, acc) do", "R20 the reader's slack is the template's zeros")
	t04Has(t, runtime, "def census(report, 0, 0), do: report", "R20 a deprecated field is dropped and counted once by the compile census")
	if n := strings.Count(text, "R.census(report, census_u, census_k)"); n != 1 {
		t.Errorf("R20: the census lands %d times, want once after the loop", n)
	}
	t04Has(t, runtime, "bump(report, :widened)", "R20 a narrower integer or float is widened exactly and counts")
	t04Has(t, runtime, "defp compile_enum_remap(", "R20 an older enum's ordinals are the reader's")
	t04Has(t, runtime, "List.to_tuple(remap)", "R20 the enum's list is the writer's prefix")
}

// ---- elixir/R21 -----------------------------------------------------------------

// t04R21 holds elixir/R21 to SPEC.md §4.3 and §5.2's table: "a backend that
// holds a float32 in a WIDER cell carries the pattern BIT FOR BIT and never
// through a float conversion", so `widenf` is the f32 at src as an f64 at dst
// with the sign, f64's all-ones exponent and the 23 payload bits moved into the
// TOP of the double's 52 — the quiet bit carried as the writer wrote it and a
// signalling NaN kept signalling. docs/PORTING.md "M21 — A float crosses two
// widths by bit surgery, never by conversion" is the rule.
func t04R21(t *testing.T) {
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "defp step([{:widenf, src, dst} | rest], body, writes, report) do", "R21 the f32 rung")
	t04Has(t, runtime, "wide = f64_bits(widen_float(f32_value(bits)))", "R21 the widening is assembled from the f32's own bits")
	t04Has(t, runtime, "defp widen_float({:nonfinite, bits}) do", "R21 an all-ones exponent is handled on the bits")
	t04Has(t, runtime, "mantissa = bits &&& 0x7FFFFF", "R21 the 23-bit mantissa is carried")
	t04Has(t, runtime, "wide = sign <<< 63 ||| 0x7FF0000000000000 ||| mantissa <<< 29", "R21 the mantissa moves into the top of the double's 52 bits")
	t04Has(t, runtime, "{:nonfinite, wide}", "R21 the pattern rides whole, the quiet bit as the writer wrote it")
	if strings.Contains(runtime, "wide ||| 0x8000000000000") {
		t.Error("R21: the widening forces the quiet bit a hardware conversion would set")
	}
	t04Has(t, runtime, "defp widen_float(value), do: value", "R21 every ordinary value is already exact")
}

// ---- elixir/R29 -----------------------------------------------------------------

// t04R29 holds elixir/R29 to the 65536 band and to bill §12.5's carried writer
// bounds. THE BAND: a record body AT 65536 is a fixed root and one byte past it
// is not (SPEC-TABLES §3.4, "65536 BYTES OF RECORD BODY: THE FORM IS NOT
// EMITTED, AND THE TABLE IS NAMED"), and a layout entry reaching past the
// writer's own declared record refuses the plan WHOLE under
// layout_record_too_large. THE WRITER'S BOUNDS: the count op clamps to the
// writer's element bound carried by the plan, so a value forged past what the
// WRITER could have written clamps and counts while a value the reader merely
// widened does not.
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
	t04Has(t, runtime, "defp layout_root({_id, 13, size, _children}) when size > 0 and size <= @record_max,", "R29 the zero root and the 65536 bound")
	t04Has(t, runtime, "defp any_past?(entries, record), do: Enum.any?(entries, fn e -> src_end(e) > record end)", "R29 an entry past the writer's declared record")
	t04Has(t, runtime, "defp clamp_count(raw, max, report) do", "R29 the bounds pass")
	t04Has(t, runtime, "raw > max -> {max, bump(report, :clamped)}", "R29 the bounds pass clamps to the writer's bounds")
	t04Has(t, runtime, "{:count, their_at, aux_at, their_n}", "R29 the count's bound is the writer's")
}

// ---- elixir/E5 ------------------------------------------------------------------

// t04E5 holds elixir/E5 to SPEC-TABLES §3.4's kind 35: "ON THIS FORM THEY ARE
// ONE BYTE APART", so `?T` and a plain `T` nesting are distinguishable in the
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
	runtime := fixedRuntimeBody
	t04Has(t, runtime, "\n      35 ->\n", "E5 the optional wrapper is a layout kind")
	t04Has(t, runtime, "my_kind == 35 and their_kind != 35 ->", "E5 T into ?T is its own emit")
	t04Has(t, runtime, "{:const, aux_at, 1, 1}", "E5 the present companion lands a constant 1")
	t04Has(t, runtime, "defp step([{:present, src, inner} | rest], body, writes, report) do", "E5 the wrapper is one present byte plus its payload")
}

// ---- elixir/W2 ------------------------------------------------------------------

// t04W2 holds elixir/W2 to SPEC-TABLES §3.4's present rule: "the payload rides
// WHOLE whether or not it is present; ZERO on write when the flag is 0". The
// emitted write body stores the flag and then the payload behind ONE `if`, so an
// absent optional costs the branch and the zeros, never a store of the caller's
// untouched payload storage.
func t04W2(t *testing.T) {
	text := t04Lineage(t,
		"package probe\n\nfixed table Lineage\n{\n    link int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    link ?int32\n}\n")
	t04Has(t, text, "if(v_link_present, do: 1, else: 0)::unsigned-8", "W2 the present flag is stored")
	guard := strings.Index(text, "if v_link_present do")
	if guard < 0 {
		t.Fatalf("W2: an absent optional does not guard its store")
	}
	present := text[guard:]
	if e := strings.Index(present, "else"); e >= 0 {
		present = present[:e]
	}
	if !strings.Contains(present, "R.fits(v_link, 32, true)::little-signed-32") {
		t.Errorf("W2: the guard does not wrap the payload's store:\n%s", present)
	}
	if n := strings.Count(text, "if v_link_present do"); n != 1 {
		t.Errorf("W2: the absent-optional guard appears %d times, want once", n)
	}
	t04Has(t, text, "<<acc::binary, 0::size(4)-unit(8)>>", "W2 the absent arm appends the template's zeros")
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
