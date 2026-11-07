package elixirtable

// THE ROADMAP'S FIELD-EVOLUTION, COUNTER AND TEXT TASKS ON THE ELIXIR LEG
// (docs/roadmap.sexp node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.4's
// counters, §5.9's census; docs/SPEC-TABLES.md §3.4's text rules;
// docs/FIXED-FORM-VERSIONING-TESTS.md). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits it with, so the harness is reachable on a tree that never built
// the C++ reference corpus and never carries a sibling runtime checkout: the
// compile-and-run half of this leg's fixed form is fixedversioning_test.go,
// which skips exactly there.
//
// elixir/E6 [weak]: Renaming uses the declared identity: a field's table-wire
// id derives from its `was` alias, so the layout bytes of a renamed field equal
// those of the name it was.
//
// elixir/E8 [verify]: Append and deprecate under the backward-read contract: a
// deprecated field keeps its slot and is read on every plan, and an appended
// field lands its declared default without moving a counter.
//
// elixir/R19 [verify]: a clean NEW-READS-OLD moves no counter at all.
//
// elixir/E9 [verify]: duplicate never raised.
//
// elixir/R16 [weak]: §5.4's counters exactly (unknown once per endpoint at COMPILE,
// widened and clamped once per entry per record, the bounds pass counts a
// forged ordinal remapped to None, copy/const/present/ordinal move nothing).
//
// elixir/R18 [weak]: a clamp that cannot fire is not emitted and nothing moves.
//
// elixir/C3 [weak]: text length clamp.
//
// elixir/C5 [weak]: wide text code units.
//
// elixir/C4 [weak]: text content refuses BY NAME. elixir/R24 [owed]: ill-formed
// text in the USED units refuses by name (`text_ill_formed`), text is counted in
// bytes with the clamp in units, and slack is unspecified on read and not a
// refusal. Both are proved here by docs/FIXED-FORM-ALGORITHM.md §4.5 fix 11,
// which is THE RULING where that page and SPEC-TABLES §3.4 disagree (§5.8 row 8
// names the disagreement: "ill-formed text zeroes the field ... where the ruling
// is a refusal by name"; "Reference fixes pending": "Where this page and the C++
// reference or §3.4 disagree, this page is the ruling"). This leg emits the
// refusal: `text/4` and `wtext/4` and the compiled plan's text op THROW
// `{:refuse, :text_ill_formed}`, and the generated load catches it as a refusal
// by name with nothing written and every counter zero.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t05Fixed is the emitted fixed module of a unit: the one <Base>Fixed.ex the
// table backend produced, never the shared FixedRuntime.ex beside it.
func t05Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// t05Runtime is this leg's shared runtime body, the module every fixed surface
// reaches for its counters and its text rules.
func t05Runtime(t *testing.T) string {
	t.Helper()
	return fixedRuntimeBody
}

func t05Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

func t05No(t *testing.T, src, bad, what string) {
	t.Helper()
	if strings.Contains(src, bad) {
		t.Errorf("%s: the emitted source still carries %q", what, bad)
	}
}

// ---- elixir/E6 ---------------------------------------------------------------

// t05E6 holds elixir/E6 to SPEC-TABLES §5: a field's table-wire id is the name
// it was, so the layout bytes of `b int32 was = "a"` are byte for byte the
// layout bytes of a plain `a int32`, and the runtime needs no rename rule of
// its own — the id matches like any other.
func t05E6(t *testing.T) {
	renamed := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    b int32 | was = "a"
}
`), "T")
	f := renamed.Fields[0]
	if f.Name != "b" {
		t.Fatalf("E6: the fixture's field is named %q, want b", f.Name)
	}
	if got := ir.TableFieldWireName(f); got != "a" {
		t.Fatalf("E6: the renamed field's declared identity = %q, want a", got)
	}
	was := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    a int32
}
`), "T")
	renamedLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(renamed))
	wasLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(was))
	if string(renamedLayout) != string(wasLayout) {
		t.Errorf("E6: the rename moved the layout bytes:\n renamed % x\n was     % x", renamedLayout, wasLayout)
	}
	if !strings.Contains(t05Runtime(t), "A RENAMED FIELD NEEDS NO RULE") {
		t.Error("E6: the runtime does not state the rename rule (identity matches on the id)")
	}
	files, err := Generate(unitFrom(t, `package probe

fixed table T
{
    b int32 | was = "a"
}
`))
	if err != nil {
		t.Fatalf("E6: Generate: %v", err)
	}
	if got := t03Layout(t, t05Fixed(t, files), "t"); string(got) != string(wasLayout) {
		t.Errorf("E6: the emitted layout for the rename = % x, want the old name's % x", got, wasLayout)
	}
}

// ---- elixir/E8 ---------------------------------------------------------------

// t05E8 holds elixir/E8 to the backward-read contract: a `| deprecated` field is
// RETIRED IN PLACE (its slot stays where it is and a reader reads what it finds
// there), and an APPENDED field rides the prefill to its declared default while
// the report stays zero on a lawful lineage.
func t05E8(t *testing.T) {
	dep := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    a int32
    b int32 | deprecated
    c int32
}
`), "T")
	if len(dep.Fields) != 3 {
		t.Fatalf("E8: the deprecated field's slot did not stay: %d fields", len(dep.Fields))
	}
	if !dep.Fields[1].Deprecated {
		t.Error("E8: the `| deprecated` marker is not on the IR field")
	}
	plain := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    a int32
    b int32
    c int32
}
`), "T")
	if got, want := len(ir.TableFixedWalkRoot(dep)), len(ir.TableFixedWalkRoot(plain)); got != want {
		t.Errorf("E8: the deprecated field's slot did not stay: %d walked entries, want the variable table's %d", got, want)
	}

	// THE APPEND lands its declared default in the prefill, and a lawful
	// lineage is exactly where the census is silent.
	root := t03Table(t, unitFrom(t, `package probe

fixed table T
{
    x int32 = 7
    y int32 = 9
}
`), "T")
	image := fixedDefaultsImage(root)
	if got := int32(image[0]) | int32(image[1])<<8 | int32(image[2])<<16 | int32(image[3])<<24; got != 7 {
		t.Errorf("E8: the appended x = %d, want its declared default 7", got)
	}
	runtime := t05Runtime(t)
	t05Has(t, runtime, "LAWFUL lineage they are both zero,", "E8 the census is silent on a lawful lineage")
}

// ---- elixir/R19 --------------------------------------------------------------

// t05R19 holds elixir/R19: a clean NEW-READS-OLD moves no counter at all. The
// report starts at zero, the census adds the plan's own number (zero on a
// lawful lineage), and the identity helpers are no-ops at zero and at false.
func t05R19(t *testing.T) {
	runtime := t05Runtime(t)
	for _, want := range []string{
		"malformed: false",
		"unknown: 0",
		"kind_mismatch: 0",
		"clamped: 0",
		"widened: 0",
		"duplicate: 0",
		"def clamped(report, 0), do: report",
		"def census(report, 0, 0), do: report",
		"def damaged(report, false), do: report",
		"LAWFUL lineage they are both zero,",
	} {
		t05Has(t, runtime, want, "R19 a clean read moves nothing")
	}
}

// ---- elixir/E9 ---------------------------------------------------------------

// t05E9 holds elixir/E9: `duplicate` never raised. The report carries the lane
// and no plan op, no bounds pass and no projection ever bumps it — a record
// whose every field is named twice over still moves nothing.
func t05E9(t *testing.T) {
	runtime := t05Runtime(t)
	t05Has(t, runtime, "duplicate: 0", "E9 the duplicate lane starts at zero")
	t05No(t, runtime, "bump(report, :duplicate)", "E9 duplicate never raised")
	if n := strings.Count(runtime, "duplicate"); n != 1 {
		t.Errorf("E9: `duplicate` occurs %d times in the runtime, want the report lane only", n)
	}
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    x int32
}
`))
	if err != nil {
		t.Fatalf("E9: Generate: %v", err)
	}
	t05No(t, t05Fixed(t, out), "duplicate", "E9 the generated surface never raises duplicate")
}

// ---- elixir/R16 --------------------------------------------------------------

// t05R16 holds elixir/R16 to §5.4's counters, each asserted by the site that
// owns it: the census ONCE per endpoint after the loop, the widen and clamp bumps of
// the plan's step, the bounds pass's `clamps`/`fclamps`, and the ordinal remap
// that lands None and counts one in the bounds pass and nowhere else.
func t05R16(t *testing.T) {
	runtime := t05Runtime(t)
	for _, want := range []string{
		// unknown: ONCE PER ENDPOINT, after the record loop (§5.9 #6).
		"def census(report, unknown, kind_mismatch) do",
		"unknown: report.unknown + unknown",
		// widened: once per entry per record.
		"bump(report, :widened)",
		// clamped: once per entry per record, count and text.
		"def clamped(report, n), do: Map.update!(report, :clamped, &(&1 + n))",
		"def clamps(acc, v, lo, hi) when v < lo or v > hi, do: acc + 1",
		"def fclamps(acc, v, lo, hi) when v < lo or v > hi, do: acc + 1",
		// the bounds pass counts a forged ordinal remapped to None.
		"def past(acc, v, variants) when v > variants, do: acc + 1",
	} {
		t05Has(t, runtime, want, "R16 §5.4's counters")
	}
	// COPY, CONST, PRESENT AND ORDINAL MOVE NOTHING: the only bump sites are the
	// census, the widen/clamp steps and the ordinal bounds pass.
	for _, bad := range []string{
		"bump(report, :copy)",
		"bump(report, :const)",
		"bump(report, :present)",
		"bump(report, :ordinal)",
	} {
		t05No(t, runtime, bad, "R16 an op that must move nothing")
	}
}

// ---- elixir/R18 --------------------------------------------------------------

// t05R18 holds elixir/R18: a clamp that cannot fire is not emitted. There is no
// `{:clamp` plan op at all — the bounds pass is the one clamp, and it is emitted
// as `R.clamps/4` in the projection.
func t05R18(t *testing.T) {
	out, err := Generate(unitFrom(t, `package probe

fixed table Cfg
{
    a     int32 = 5 | min = 0, max = 1000
    marks [..4]int32 | min = 0, max = 10
}
`))
	if err != nil {
		t.Fatalf("R18: Generate: %v", err)
	}
	runtime := string(out[FixedRuntimeModule+".ex"])
	body := string(out["ProbeFixed.ex"])
	t05No(t, runtime, "{:clamp", "R18 no plan op clamps")
	t05No(t, body, "{:clamp", "R18 no plan op clamps")
	t05Has(t, body, "R.clamps(c, ", "R18 the bounds pass is the clamp")
}

// ---- elixir/C3 ---------------------------------------------------------------

// t05C3 holds elixir/C3: a text length is clamped ONCE in the bounds pass, at
// the field's declared capacity, for `string(N)` and `wstring(N)` alike.
func t05C3(t *testing.T) {
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    label string(8)
    wide  wstring(8)
}
`))
	if err != nil {
		t.Fatalf("C3: Generate: %v", err)
	}
	body := string(out["ProbeFixed.ex"])
	t05Has(t, body, "R.clamps(c, n_label, 0, 8)", "C3 a string(N)'s length clamps to its bound")
	t05Has(t, body, "R.clamps(c, n_wide, 0, 8)", "C3 a wstring(N)'s length clamps to its bound")
	t05Has(t, t05Runtime(t), "def clamps(acc, v, lo, hi) when v < lo or v > hi, do: acc + 1", "C3 the clamp counts once")
}

// ---- elixir/C5 ---------------------------------------------------------------

// t05C5 holds elixir/C5: a wstring(N) rides in UTF-16 CODE UNITS — the length is
// a byte length over paired units, an odd length or an unpaired surrogate is
// damage, and the reader decodes two bytes a unit.
func t05C5(t *testing.T) {
	runtime := t05Runtime(t)
	for _, want := range []string{
		"def wtext(used, default, m), do: wtext(used, default, m, true)",
		"defp text_ok?(2, used), do: rem(byte_size(used), 2) == 0 and utf16_ok?(used)",
		"defp utf16_ok?(<<0::little-unsigned-16, _::binary>>), do: false",
		"when u >= 0xD800 and u <= 0xDBFF",
		"when u >= 0xDC00 and u <= 0xDFFF",
	} {
		t05Has(t, runtime, want, "C5 wide text code units")
	}
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    wide wstring(8)
}
`))
	if err != nil {
		t.Fatalf("C5: Generate: %v", err)
	}
	t05Has(t, string(out["ProbeFixed.ex"]), "R.wtext(", "C5 the wide field reads through the wide-text helper")
}

// ---- elixir/C4 ---------------------------------------------------------------

// t05C4 holds elixir/C4 to §4.5 fix 11: text content refuses BY NAME. Both text
// kinds and the compiled plan's text op throw the named refusal on a content
// violation, and the generated load catches it rather than landing a default.
func t05C4(t *testing.T) {
	runtime := t05Runtime(t)
	t05Has(t, runtime, "do: throw({:refuse, :text_ill_formed}), else: {used, m}", "C4 the text content rule refuses by name")
	if n := strings.Count(runtime, "throw({:refuse, :text_ill_formed})"); n != 3 {
		t.Errorf("C4: the named refusal is thrown %d times, want text/1, wtext/2 and the compiled text op", n)
	}
	t05No(t, runtime, "do: {default, true}", "C4 ill-formed text no longer defaults in silence")
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    label string(8)
    wide  wstring(8)
}
`))
	if err != nil {
		t.Fatalf("C4: Generate: %v", err)
	}
	body := string(out["ProbeFixed.ex"])
	t05Has(t, body, ":throw, {:refuse, why} -> {:error, why, report}", "C4 the load catches the named refusal")
}

// ---- elixir/R24 --------------------------------------------------------------

// t05R24 holds elixir/R24 to all three clauses: ill-formed text in the USED
// units refuses by name (`text_ill_formed`); the length is counted in BYTES
// with the clamp in UNITS; and slack is unspecified on read and not a refusal.
func t05R24(t *testing.T) {
	runtime := t05Runtime(t)
	// (1) the used units alone refuse by name, on both plans.
	t05Has(t, runtime, "throw({:refuse, :text_ill_formed})", "R24 ill-formed text refuses by name")
	// (3) slack is unspecified on read and not a refusal: the content check
	// reads the USED units only, never the slack.
	t05Has(t, runtime, "if text_ok?(flavour, binary_part(take, 0, v * unit)) do", "R24 the content check holds the USED units and not the slack")
	// (2) the length is BYTES, the clamp is in UNITS.
	t05Has(t, runtime, "cap = div(size, unit)", "R24 the clamp is in units")
	out, err := Generate(unitFrom(t, `package probe

fixed table T
{
    label string(8)
    wide  wstring(8)
}
`))
	if err != nil {
		t.Fatalf("R24: Generate: %v", err)
	}
	body := string(out["ProbeFixed.ex"])
	t05Has(t, body, "R.clamps(c, n_label, 0, 8)", "R24 a byte string's length clamps to its unit bound")
	t05Has(t, body, "R.clamps(c, n_wide, 0, 8)", "R24 a wide string's length clamps in code units")
	t05Has(t, body, "binary_part(b_label, 0, min(max(n_label, 0), 8))", "R24 the used bytes are the length's own, never the slack")
	t05Has(t, body, "binary_part(b_wide, 0, 2 * min(max(n_wide, 0), 8))", "R24 the wide used units ride two bytes each")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapElixirT05Evolution(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/E6", check: t05E6},
		{id: "elixir/E8", check: t05E8},
		{id: "elixir/R19", check: t05R19},
		{id: "elixir/E9", check: t05E9},
		{id: "elixir/R16", check: t05R16},
		{id: "elixir/R18", check: t05R18},
		{id: "elixir/C3", check: t05C3},
		{id: "elixir/C4", check: t05C4},
		{id: "elixir/C5", check: t05C5},
		{id: "elixir/R24", check: t05R24},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
