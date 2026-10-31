package elixirtable

// THE ROADMAP'S VERSIONING TASKS ON THE ELIXIR LEG (docs/roadmap.sexp node
// `fixed-tables`, rows retirement / retired-runtime / numeric-evolution /
// optional-values). One subtest per task id, table-driven, t.Parallel() first.
//
// The law is docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash",
// "The old contract's tests, retired by name", the landing rules) and
// docs/FIXED-FORM-ALGORITHM.md §5; the wire law is docs/SPEC-TABLES.md §3.4.
// Every assertion reads the Elixir this leg emits and the shared runtime body
// it emits them with, so the harness is reachable on a tree that never built
// the C++ corpus and never carries a sibling runtime checkout.

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t04Has is a required substring of a source, named by rule.
func t04Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the source does not carry %q", what, want)
	}
}

// t04Floor reads the emitted `@<snake>_floor` constant.
func t04Floor(t *testing.T, text, snake string) string {
	t.Helper()
	i := strings.Index(text, "@"+snake+"_floor ")
	if i < 0 {
		t.Fatalf("@%s_floor is not emitted", snake)
	}
	rest := text[i:]
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "@"+snake+"_floor "))
}

// t04Emitted is the elixir source for one fixed table of a unit, plus the
// shared runtime body it is generated against.
func t04Emitted(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return t03Fixed(t, files)
}

// ---- elixir/R3 --------------------------------------------------------------

// t04R3 holds elixir/R3 to FIXED-FORM-VERSIONING-TESTS.md "The floor and the
// hash" and FIXED-FORM-ALGORITHM.md §5.2: the floor is 1 + the highest retired
// index (0 when none is), and a file below it refuses `layout_unsupported`
// REPORTING THE FILE'S HASH (`select/4`). The emitted module carries the floor
// and the retired table; the runtime's select maps an index below the floor to
// `layout_unsupported` with the hash.
func t04R3(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
}
`)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R3: the fixture is not a fixed root")
	}
	none := t04Emitted(t, u)
	if got := t04Floor(t, none, "t"); got != "0" {
		t.Errorf("R3: with nothing retired the floor = %s, want 0", got)
	}

	older := own
	older.Wire = 0x1122334455667788
	older.Retired = true
	older.Reason = "an old layout"
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R3: GenerateLineage: %v", err)
	}
	text := t03Fixed(t, files)
	if got := t04Floor(t, text, "t"); got != "1" {
		t.Errorf("R3: with index 0 retired the floor = %s, want 1", got)
	}
	t04Has(t, text, "0x1122334455667788", "R3 the retired entry rides the lineage")
	t04Has(t, text, "{0x1122334455667788, \"an old layout\"}", "R3 the retired hash is named by the operator's sentence")
	t04Has(t, fixedRuntimeBody, "i when i < floor ->\n        {:error, :layout_unsupported, hash}", "R3 below the floor is layout_unsupported")
	t04Has(t, fixedRuntimeBody, "{:error, :layout_unsupported, hash}", "R3 the refusal reports the file's hash")
}

// ---- elixir/R32 -------------------------------------------------------------

// t04R32 holds elixir/R32 to FIXED-FORM-ALGORITHM.md §5.2: a retired version is
// refused BY NAME (the operator's reason rides the `_retired` table) and once
// per hash, idempotently — the retired list carries one entry per retired hash.
func t04R32(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
}
`)
	own, _ := FixedLineageOf(u, "T")
	older := own
	older.Wire = 0xDEADBEEFCAFEF00D
	older.Retired = true
	older.Reason = "superseded, upgrade"
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R32: GenerateLineage: %v", err)
	}
	text := t03Fixed(t, files)
	t04Has(t, text, "{0xDEADBEEFCAFEF00D, \"superseded, upgrade\"}", "R32 the retired version is refused by name")
	if n := strings.Count(text, "0xDEADBEEFCAFEF00D"); n != 2 {
		t.Errorf("R32: the retired hash appears %d times, want 2 (lineage entry and retired list exactly once)", n)
	}
	// IDEMPOTENT: handing the SAME retired entry twice must emit what handing it
	// once does — one retired entry and one floor step. The lock holds one entry
	// per layout, but a caller that plays the lock by hand can repeat a hash, and
	// the retired table is "refused by name, ONCE" (the task title): the second
	// mark of one hash adds no entry and moves the floor no further.
	files2, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older, older}})
	if err != nil {
		t.Fatalf("R32: GenerateLineage: %v", err)
	}
	text2 := t03Fixed(t, files2)
	if got := t04Floor(t, text2, "t"); got != "1" {
		t.Errorf("R32: the floor = %s, want 1: a second mark of one retired hash moves the floor no further", got)
	}
	if n := strings.Count(text2, "\"superseded, upgrade\""); n != 1 {
		t.Errorf("R32: the retired name appears %d times, want once: a retired hash is refused once", n)
	}
	if n := strings.Count(text2, "0xDEADBEEFCAFEF00D"); n != 2 {
		t.Errorf("R32: the retired hash appears %d times, want 2 (one lineage entry and one retired list entry)", n)
	}
}

// ---- elixir/R10 -------------------------------------------------------------

// t04R10 holds elixir/R10 to FIXED-FORM-ALGORITHM.md §5.2 and
// FIXED-FORM-VERSIONING-TESTS.md "hash_known_bytes_differ"/"hash_identity": the
// run-time walk of a stranger's layout and the recompute of the header's hash
// are RETIRED — select takes the header's hash as given and holds a known
// hash's layout to a BYTE COMPARISON, never a parse, and never derives a hash
// from the bytes it holds.
func t04R10(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "def select(known, floor, hash, layout) do", "R10 select takes the header's hash as given")
	t04Has(t, fixedRuntimeBody, "case index_of(known, hash, 0) do", "R10 the header's hash is matched against the handed lineage")
	t04Has(t, fixedRuntimeBody, "defp index_of(known, hash, i) do", "R10 the hash is never recomputed from the bytes behind it")
	t04Has(t, fixedRuntimeBody, "if byte_size(layout) == k.layout_bytes and layout == k.layout do", "R10 a known hash's layout is a byte comparison")
	t04Has(t, fixedRuntimeBody, "{:error, :layout_malformed, 0}", "R10 a known hash whose bytes differ is layout_malformed")
	// THE RETIRED HALF, ASSERTED BY ABSENCE: nothing on the run-time selection
	// path parses a stranger's layout or re-derives the header's hash. The path
	// is the text from `select` to the helper it falls to; a re-added walk would
	// have to parse there, and a recompute would have to hash there, so either
	// turns this red.
	i := strings.Index(fixedRuntimeBody, "def select(known, floor, hash, layout) do")
	if i < 0 {
		t.Fatal("R10: the run-time selection path is not emitted")
	}
	j := strings.Index(fixedRuntimeBody[i:], "defp index_of")
	if j < 0 {
		t.Fatal("R10: the selection path's helper is not emitted")
	}
	if path := fixedRuntimeBody[i : i+j]; strings.Contains(path, "parse_layout") || strings.Contains(path, "hash(") || strings.Contains(path, "fnv(") {
		t.Error("R10: the run-time selection path parses a stranger's layout or recomputes the header's hash")
	}
}

// ---- elixir/R15 -------------------------------------------------------------

// t04R15 holds elixir/R15 to FIXED-FORM-VERSIONING-TESTS.md "The old contract's
// tests, retired by name": the four forward-read clamps (count across bounds,
// range across versions, remap of an unknown variant to None, drop-and-count of
// an unknown field) are retired — the cross-version half is now a refusal by
// the lineage, `layout_newer`, before any record is decoded.
func t04R15(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "def select(known, floor, hash, layout) do", "R15 the reader matches the file's hash against the lineage")
	t04Has(t, fixedRuntimeBody, "{:error, :layout_newer, hash}", "R15 a hash in no lineage entry is layout_newer")
	t04Has(t, fixedRuntimeBody, "nil ->\n        {:error, :layout_newer, hash}", "R15 the refusal precedes any walk")
	// THE FOUR FORWARD-READ CLAMPS ARE RETIRED: the cross-version half is the
	// refusal above, so no plan entry clamps a count across a version, remaps an
	// unknown variant to None, or drops-and-counts an unknown field FORWARD. The
	// runtime's one count clamp is the reader's own bound within a version, and
	// the one unknown census is the backward drop the landing rules count.
	if strings.Contains(fixedRuntimeBody, "defp forward") || strings.Contains(fixedRuntimeBody, "layout_newer, report") {
		t.Error("R15: a forward-read clamp survives the retired-runtime row")
	}
}

// ---- elixir/E3/other-required-widens ---------------------------------------

// t04E3 holds elixir/E3 to FIXED-FORM-ALGORITHM.md §4's widening rungs as the
// runtime carries them: a kind that grew decodes at the writer's width and
// lands exactly, counting one `widened`; the integer, fixed-point and float
// families each have their own ladder and a kind that MOVED is reported.
func t04E3(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "defp widens?(from, to) when from >= 2 and from <= 5 and to >= 2 and to <= 5, do: to > from", "E3 the signed/unsigned integer ladder")
	t04Has(t, fixedRuntimeBody, "defp widens?(from, to) when from >= 20 and from <= 24 and to >= 20 and to <= 24, do: to > from", "E3 the signed fixed-point ladder")
	t04Has(t, fixedRuntimeBody, "defp widens?(from, to) when from >= 25 and from <= 29 and to >= 25 and to <= 29, do: to > from", "E3 the unsigned fixed-point ladder")
	t04Has(t, fixedRuntimeBody, "{:widen, their_at, at, their_size, my_size, signed_kind?(their_kind)}", "E3 a widened integer lands at the writer's width")
	t04Has(t, fixedRuntimeBody, "{dst, image_bytes(raw, width, signed)}", "E3 a widened integer lands sign-extended at the reader's width")
	t04Has(t, fixedRuntimeBody, "bump(report, :widened)", "E3 a widening counts one widened")
	t04Has(t, fixedRuntimeBody, "{acc, bump(report, :kind_mismatch)}", "E3 a kind that moved is reported")

	// [CAPABILITY]: the leg WRITES the older integer at its boundary values and
	// the WIDENED leg READS them back exactly, counting one `widened` per record.
	// The write is this leg's own generated save (`VOLD_int_widen`); the read is
	// the widening rung of `VNEW_int_widen`, whose int16 became an int32.
	file := filepath.Join(t.TempDir(), "int_widen.bin")
	write := fmt.Sprintf(`  data =
    T.int_widen_fixed_save([
      %%VoldIntWiden.IntWiden{lead: 11, v: -1, trail: 12},
      %%VoldIntWiden.IntWiden{lead: 21, v: -32768, trail: 22},
      %%VoldIntWiden.IntWiden{lead: 31, v: 32767, trail: 32}
    ])

  File.write!(%s, data)
  {tag, values, report} = load(data)
  check(tag == :ok and length(values) == 3, "the leg's own write does not read back: #{inspect(tag)} #{why(report)}")
  check(Enum.map(values, & &1.v) == [-1, -32768, 32767], "the leg's own write lost a boundary value: #{inspect(Enum.map(values, & &1.v))}")`,
		elixirString(file))
	if out, err := runVersionProbe(t, elixirBinary(t), "VOLD_int_widen", nil, 0, file, write); err != nil {
		t.Fatalf("E3 write: %v\n%s", err, out)
	}
	read := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the widened reader refused the old writer's file: #{inspect(tag)} #{why(report)}")
  check(report.malformed == false, "a clean widening read is not malformed")
  check(Enum.map(values, & &1.v) == [-1, -32768, 32767], "the widened read did not sign-extend a boundary value exactly: #{inspect(Enum.map(values, & &1.v))}")
  check(report.widened == 3, "each of the three records owes one widened: #{report.widened}")`
	if out, err := runVersionProbe(t, elixirBinary(t), "VNEW_int_widen", []string{"VOLD_int_widen"}, 0, file, read); err != nil {
		t.Fatalf("E3 read: %v\n%s", err, out)
	}
}

// ---- elixir/C8 --------------------------------------------------------------

// t04C8 holds elixir/C8 to the fixed-point F-shift and bits(N) of
// FIXED-FORM-ALGORITHM.md §1, §5.2: the fixed-point kinds have their own
// ladders and a fixed-point whose I grew at equal F is a widen inside its
// family, upward only; bits(N) is a fixed storage width.
func t04C8(t *testing.T) {
	// bits(N): a fixed storage width — 4 bytes for N <= 32, else 8
	// (docs/FIXED-FORM-ALGORITHM.md §1's C table: "`bits(N)` | 4 for `N <= 32`,
	// else 8").
	if got := ir.TableFixedStorageBytes(ir.FieldType{Kind: ir.TBits, Width: 12}); got != 4 {
		t.Errorf("C8: bits(12) storage = %d bytes, want 4", got)
	}
	if got := ir.TableFixedStorageBytes(ir.FieldType{Kind: ir.TBits, Width: 40}); got != 8 {
		t.Errorf("C8: bits(40) storage = %d bytes, want 8", got)
	}

	// F-shift: a fixed-point field's declared bounds are in VALUE UNITS and its
	// storage is the RAW scaled integer, so both ends are shifted by F first
	// (docs/FIXED-FORM-ALGORITHM.md §5.2: "both ends are shifted by `F` first").
	u := unitFrom(t, `package probe

fixed table T
{
    pos fixed(48, 16) | min = -1000, max = 1000
}
`)
	st := t03Table(t, u, "T")
	lo, hi := fixedRangeOf(st.Fields[0])
	if lo != "-65_536_000" || hi != "65_536_000" {
		t.Errorf("C8: fixed(48,16) bounds in value units [-1000, 1000] shift to raw [%s, %s], want [-65_536_000, 65_536_000]", lo, hi)
	}
	text := t04Emitted(t, u)
	t04Has(t, text, "-65_536_000", "C8 the shifted low bound rides the emitted clamp")
	t04Has(t, text, "65_536_000", "C8 the shifted high bound rides the emitted clamp")
}

// ---- elixir/R20 -------------------------------------------------------------

// t04R20 holds elixir/R20 to the landing rules: an added field lands its
// declared default (the prefill image the plan does not write), a deprecated
// field is dropped and counted once under `unknown` per plan, a narrower
// integer or float is widened exactly and counted, a shorter array or string
// lands with the reader's slack as template zeros, and an older enum's
// ordinals are the reader's.
func t04R20(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32
    y int32 = 7
}
`)
	// AN ADDED FIELD LANDS ITS DECLARED DEFAULT: the record image is assembled
	// over the PREFILL, whose bytes ARE the declared defaults, and the plan
	// writes no byte for a field the writer never carried.
	st := t03Table(t, u, "T")
	img := fixedDefaultsImage(st)
	if len(img) != 8 || binary.LittleEndian.Uint32(img[0:4]) != 0 || binary.LittleEndian.Uint32(img[4:8]) != 7 {
		t.Errorf("R20: a declared default is not the added field's landing bytes: % x", img)
	}
	text := t04Emitted(t, u)
	t04Has(t, text, "@t_prefill", "R20 the declared defaults ride the emitted prefill")

	// A DEPRECATED FIELD IS DROPPED AND COUNTED ONCE UNDER `unknown` PER PLAN:
	// the census flag counts a writer's field once PER FIELD PER ENDPOINT, so only the
	// first slot of an array element censuses.
	t04Has(t, fixedRuntimeBody, "nil -> if census, do: bump(report, :unknown), else: report", "R20 a dropped field counts once per field per endpoint")
	t04Has(t, fixedRuntimeBody, "def census(report, unknown, kind_mismatch)", "R20 the unknown census is per plan")

	// A NARROWER INTEGER OR FLOAT IS WIDENED EXACTLY AND WIDENED COUNTS: the
	// widen step sign-extends at the reader's width and bumps the counter; the
	// float rung is its own op.
	t04Has(t, fixedRuntimeBody, "defp step([{:widen, src, dst, size, width, signed} | rest]", "R20 a narrower integer is widened at the reader's width")
	t04Has(t, fixedRuntimeBody, "{dst, image_bytes(raw, width, signed)}", "R20 the widened integer lands exactly")
	t04Has(t, fixedRuntimeBody, "defp step([{:widenf, src, dst} | rest]", "R20 a narrower float is widened by its own op")
	t04Has(t, fixedRuntimeBody, "bump(report, :widened)", "R20 a narrower value is widened exactly and counted")

	// A SHORTER ARRAY OR STRING LANDS WITH THE READER'S SLACK AS TEMPLATE ZEROS:
	// a destination the plan never writes is a GAP, and the assembler fills it
	// from the prefill template (the declared image, zeros where the default is
	// zero).
	t04Has(t, fixedRuntimeBody, "gap = binary_part(prefill, pos, dst - pos)", "R20 the reader's slack lands from the prefill template")

	// AN OLDER ENUM'S ORDINALS ARE THE READER'S, THE LIST BEING A PREFIX: the
	// writer's ordinal tables carry the reader's numbers, remapped positionally.
	t04Has(t, fixedRuntimeBody, "{:ordinal, their_at, at, size_at(theirs, ti), size_at(mine, mi), List.to_tuple([their_variants | remap])}", "R20 an older enum's ordinals are remapped to the reader's")
	t04Has(t, fixedRuntimeBody, "{elem(remap, raw), report}", "R20 the writer's ordinal lands the reader's ordinal")

	// a narrower value past its range still counts as clamped, the same bound
	// answer §3 gives every value outside its reader's range.
	t04Has(t, fixedRuntimeBody, "defp clamp_count(raw, max, report)", "R20 an out-of-range count clamps to the reader's bound")
}

// ---- elixir/R21 -------------------------------------------------------------

// t04R21 holds elixir/R21 to FIXED-FORM-ALGORITHM.md §5.2 EMIT, under the
// finite rule (docs/SPEC-TABLES.md §3.3, 2026-10-27): widenf is the ordinary
// float widening — every FINITE f32 is exactly representable in an f64, so
// there is nothing to round and no bit surgery. A NaN or an infinity never
// reaches a value: it stays non-finite in the image and the projection's
// f64_value refuses it as non_finite_float, so no NaN payload is carried and
// widen_float is gone.
func t04R21(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "{:widenf, their_at, at}", "R21 the f32->f64 rung is its own op")
	t04Has(t, fixedRuntimeBody, "defp step([{:widenf, src, dst} | rest]", "R21 widenf is stepped")
	t04Has(t, fixedRuntimeBody, "else: f64_bits(f32_value(bits))", "R21 a finite f32 widens by the ordinary conversion")
	t04Has(t, fixedRuntimeBody, "do: throw({:refuse, :non_finite_float})", "R21 a non-finite float is refused, not carried")
	for _, gone := range []string{"widen_float", "{:nonfinite, bits}", "mantissa <<< 29", "0x8000000000000)"} {
		if strings.Contains(fixedRuntimeBody, gone) {
			t.Errorf("R21 the NaN bit surgery is gone under the finite rule, but the runtime still holds %q", gone)
		}
	}
}

// ---- elixir/R29 -------------------------------------------------------------

// t04R29 holds elixir/R29 to SPEC-TABLES.md §3.4 and FIXED-FORM-ALGORITHM.md
// §5.2: the band case — a widening across the 65536 ceiling is refused WHOLE,
// and the bounds pass clamps a value to the writer's bounds and counts.
func t04R29(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "@record_max 65536", "R29 the 65536 ceiling")
	t04Has(t, fixedRuntimeBody, "size > @record_max -> {:error, :layout_record_too_large}", "R29 a record past the ceiling is refused")
	t04Has(t, fixedRuntimeBody, "defp clamp_count(raw, max, report)", "R29 the bounds pass clamps a count")
	if ir.TableFixedRecordMaxBytes != 65536 {
		t.Errorf("R29: TableFixedRecordMaxBytes = %d, want 65536", ir.TableFixedRecordMaxBytes)
	}
}

// ---- elixir/E5 --------------------------------------------------------------

// t04E5 holds elixir/E5 to SPEC-TABLES.md §3.3/§3.4: `?T` against plain
// nesting of T. The optional wrapper (kind 35) is one present byte in front of
// the payload; T into ?T writes a CONSTANT present byte 1 and then the payload.
func t04E5(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "my_kind == 35 and their_kind != 35 ->", "E5 T into ?T is its own lane")
	t04Has(t, fixedRuntimeBody, "{:const, aux_at, 1, 1}", "E5 the present byte is a constant 1")
	t04Has(t, fixedRuntimeBody, "defp check_composite(35, size, children, facts) do", "E5 the wrapper is kind 35, checked as a composite")
	t04Has(t, fixedRuntimeBody, "size != facts.sum + 1", "E5 the wrapper's layout is one present byte in front of its payload")
	if ir.TableKindOptional != 35 {
		t.Errorf("E5: TableKindOptional = %d, want 35", ir.TableKindOptional)
	}
}

// ---- elixir/W2 --------------------------------------------------------------

// t04W2 holds elixir/W2 to SPEC-TABLES.md §3.4: an absent optional SKIPS its
// store — the payload is unspecified and nobody wrote it, so the content rule
// (`text`/`wtext`'s present flag) does not run and a copy is left alone.
func t04W2(t *testing.T) {
	t04Has(t, fixedRuntimeBody, "defp step([{:present, src, inner} | rest], body, writes, report) do", "W2 an absent optional's payload is not walked")
	t04Has(t, fixedRuntimeBody, "if present and not text_ok?(1, used), do: throw({:refuse, :text_ill_formed}), else: {used, m}", "W2 the present flag gates the content rule")
	t04Has(t, fixedRuntimeBody, "def text(used, _default, m, present) do", "W2 the content rule is handed the present flag")
	t04Has(t, fixedRuntimeBody, "defp present_wrap_entry({:copy, _, _, _} = e, _src), do: e", "W2 an absent optional leaves the copy alone")
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
