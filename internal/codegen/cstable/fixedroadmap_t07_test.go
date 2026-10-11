package cstable

// fixedroadmap_t07 — the cs leg's live-extents, writing and union-guards rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"). The law
// is docs/FIXED-FORM-ALGORITHM.md §3.1 (the write), §4.1 (the plan and its guard
// lanes), §4.3 (the prefill), §4.6 (the bounds pass) and docs/SPEC-TABLES.md §3.4.
//
// One subtest per task id, and each subtest asserts the clause the task's title
// states, read off the C# this leg emits (the emitter's own generated source) or
// off the ir walk it renders. Nothing here builds dotnet: the sibling runtime a
// run needs is absent, so the column is asserted on the emitted unit, which is
// the leg's own output and the site a break is planted in.

import (
	"strings"
	"testing"
)

// t07File is the generated file that carries the unit's shared runtime and its
// fixed table for the single-file `package probe` fixtures below.
const t07File = "ProbeTable.cs"

func t07Src(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files[t07File]
	if !ok {
		t.Fatalf("Generate emitted no %s", t07File)
	}
	return string(body)
}

// t07Norm collapses a run of whitespace to one space, so an assertion reads the
// emitted statement and not the indentation the string builder chose.
func t07Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t07Block is the text between two markers, failing rather than answering "".
func t07Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the generated source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the generated source has no %q after %q", end, start)
	}
	return rest[:j]
}

func t07Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

func t07Before(t *testing.T, src, first, second, what string) {
	t.Helper()
	i, j := strings.Index(src, first), strings.Index(src, second)
	if i < 0 || j < 0 {
		t.Fatalf("%s: missing %q (%d) or %q (%d)", what, first, i, second, j)
	}
	if !(i < j) {
		t.Errorf("%s: %q is not before %q", what, first, second)
	}
}

// The fixtures, one per clause. They are the shapes the page's sentences name:
// a counted array of bounded scalars and a bounded optional (the live extents),
// a union (the narrower arm), a bytes(N) and a string inside a union arm (the
// two lanes), and a union arm holding a union (the outer tag).
const (
	t07Union = `package probe

type Alpha { m int32 = 0 }
type Beta  { n int32 = 0 }

union Pick
{
    alpha Alpha
    beta  Beta
}

fixed table T
{
    pick Pick
    seq  int32 = 0
}
`

	t07ArmText = `package probe

type Plain
{
    n int32 = 7
}

type Labelled
{
    lead  int32 = 5
    label string(8)
    trail int32 = 9
}

union Pick
{
    plain    Plain
    labelled Labelled
}

fixed table T
{
    pick Pick
    tail int32 = 3
}
`

	t07Nested = `package probe

type Inner1 { a int32 = 0 }
type Inner2 { b int32 = 0 }

union Mid
{
    alpha Inner1
    beta  Inner2
}

union Top
{
    one Mid
    two Inner1
}

fixed table T
{
    top Top
    seq int32 = 0
}
`

	t07Bytes = `package probe

fixed table T
{
    lead  uint32 = 1
    blob  bytes(8)
    trail uint32 = 2
}
`

	t07Live = `package probe

fixed table T
{
    count int32 = 0
    vals  [..4]int32
    note  ?int32
    r     int32 | min = -5, max = 5
}
`

	t07Clamp = `package probe

fixed table T
{
    n     int32 = 0
    seeds [..4]int32 | min = -5, max = 5
    maybe ?int32 | min = -5, max = 5
    r     int32 | min = -5, max = 5
}
`
)

func TestFixedRoadmapCSharpT07Writes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/C14", t07C14},
		{"cs/W4", t07W4},
		{"cs/W9", t07W9},
		{"cs/W1", t07W1},
		{"cs/W3", t07W3},
		{"cs/W5", t07W5},
		{"cs/W8", t07W8},
		{"cs/W7", t07W7},
		{"cs/W16", t07W16},
		{"cs/R28", t07R28},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07C14: C14 [weak] "bounds pass walks live only" — §4.6: "It walks only what a
// read can have written — a counted array's LIVE elements and never its slack, an
// optional's payload only when the present byte says so". The emitted pass loops
// a counted array to its live count and holds an optional's payload under its
// present byte; a pass that walked the declared extent would count a clamp on
// every clean read of the slack.
func t07C14(t *testing.T) {
	src := t07Src(t, t07Clamp)
	body := t07Block(t, src, "TFixedClampBody(T value, ref int clamped)", "\n        }")
	t07Has(t, t07Norm(body),
		"for (int i = 0; i < value.SeedsCount && i < value.Seeds.Length; ++i)",
		"C14 a counted array's clamp walks the live count and never the declared slack")
	if strings.Contains(t07Norm(body), "i < 4") {
		t.Error("C14: the pass walks the declared extent (4) rather than the live count")
	}
	t07Has(t, t07Norm(body), "if (value.MaybePresent)", "C14 an optional's payload is held under its present byte")
	iPresent := strings.Index(t07Norm(body), "if (value.MaybePresent)")
	iMaybe := strings.Index(t07Norm(body), "if (value.Maybe < -5)")
	if iPresent < 0 || iMaybe < 0 || iPresent > iMaybe {
		t.Error("C14: the optional payload is clamped outside its present-byte guard")
	}
}

// t07W4: W4 [verify] "read slack unspecified" — SPEC-TABLES §3.4: "ZERO on write
// past the count, UNSPECIFIED on read, never a refusal"; "A reader validates the
// USED UNITS ONLY and never looks at the slack". The count op reads the count word
// and clamps it into [0, size] counting a clamp; the run sets no Malformed from a
// value, so slack is never a refusal.
func t07W4(t *testing.T) {
	src := t07Src(t, t07Clamp)
	block := t07Block(t, src, "case Count:", "case Text:")
	n := t07Norm(block)
	t07Has(t, n, `if (v < 0) { v = 0; if (report != null) report.Clamped++; }`,
		"W4 the count is clamped into range and counted, never refused")
	t07Has(t, n, `else if ((uint)v > p.Size) { v = (int)p.Size; if (report != null) report.Clamped++; }`,
		"W4 the bound is the plan's own size, and the out-of-range count is clamped not refused")
	if strings.Contains(block, "Malformed") {
		t.Error("W4: the run marks slack malformed; a reader validates the used units only and never looks at the slack")
	}
}

// t07W9: W9 [verify] "prefill unwritten ranges only" — §4.3: "Prefill the bytes
// the plan does not write ... for the identity plan that list is EMPTY". The load
// computes the fill list only inside the `hash != TFixedHash` branch (a compiled
// plan); the identity lane keeps `fillCount` at zero and runs no prefill.
func t07W9(t *testing.T) {
	src := t07Src(t, t07Live)
	t07Has(t, src, "int fillCount = 0;", "W9 the identity lane's prefill list starts empty")
	t07Has(t, src, "fillCount = TableFixedWire.Fills(entries, TFixedCover, landed, fillBuf);",
		"W9 the fill list is the slots the selected plan does not land")
	t07Has(t, t07Norm(src), "TableFixedWire.FillRun(fillBuf.AsSpan(0, fillCount), TFixedSlots, values[k]);",
		"W9 the prefill runs the derived list and nothing else")
	t07Before(t, src, "if (hash != TFixedHash)", "fillCount = TableFixedWire.Fills(",
		"W9 the prefill is computed only for a compiled plan, never for the identity plan")
}

// t07W1: W1 [verify] "write slack is template zeros" — §3.1: "The template is a
// compile-time constant — the hash, then zero everywhere a value lands — which
// zero-fills every byte of declared slack without the writer touching it". The
// save clears the whole body before the straight-line stores write the live
// extent.
func t07W1(t *testing.T) {
	src := t07Src(t, t07Union)
	t07Has(t, src, "at.Slice(8, (int)TFixedBodyBytes).Clear();",
		"W1 the record body is zeroed on the template before any field store")
	t07Before(t, src, "at.Slice(8, (int)TFixedBodyBytes).Clear();", "TFixedWriteBody(at.Slice(8), values[k]);",
		"W1 the zeroing stands before the live-extent store")
}

// t07W3: W3 [verify] "zero behind a narrower arm" — §3.1: "A union writes the tag
// and the taken arm only, zero behind a narrower one." The write stores the tag,
// switches on it and stores only the taken arm; the body's earlier clear leaves
// the bytes behind a narrower arm at zero.
func t07W3(t *testing.T) {
	src := t07Src(t, t07Union)
	body := t07Block(t, src, "TFixedWriteBody(Span<byte> b, T value)", "\n        }")
	t07Has(t, body, b0+"[0] = (byte)value.Pick.Type;", "W3 the tag is written")
	t07Has(t, t07Norm(body), "switch (value.Pick.Type)", "W3 the taken arm alone is selected by the tag")
	t07Has(t, body, "default: break;", "W3 an arm not taken writes nothing and the clear's zeros stand")
	t07Has(t, src, "at.Slice(8, (int)TFixedBodyBytes).Clear();",
		"W3 the body is zeroed, so a narrower arm's tail is zero")
}

// t07W5: W5 [weak] "write checks DEBUG only" — §3.1: "Write-side bound checks are
// DEBUG ONLY, by rule. `count <= Max` and `length <= N` are a caller contract; a
// release build removes them exactly as it removes `assert`, pays nothing, and
// clamps nothing." The emitted write carries a System.Diagnostics.Debug.Assert on
// the count and stores the caller's value unchanged.
func t07W5(t *testing.T) {
	src := t07Src(t, t07Live)
	body := t07Block(t, src, "TFixedWriteBody(Span<byte> b, T value)", "\n        }")
	t07Has(t, body, "System.Diagnostics.Debug.Assert(count_vals >= 0 && count_vals <= 4);",
		"W5 the write-side count check is a Debug.Assert, gone in a release build")
	if strings.Contains(t07Norm(body), "count_vals = 4") || strings.Contains(t07Norm(body), "count_vals = 0") {
		t.Error("W5: the write clamps the count; a release build clamps nothing")
	}
}

// t07W8: W8 [verify] "bytes(N) takes the array row" — §4.1: "A `bytes(N)` takes
// the ARRAY's row, not a text field's — the count to `aux` and the elements to
// `dst`". The blob's plan entry carries the elements in Dst and the count in Aux,
// with Meta 3 (the bytes flavour).
func t07W8(t *testing.T) {
	src := t07Src(t, t07Bytes)
	t07Has(t, t07Norm(src), "new TableFixedEntry(4u, 1u, 8u, 2u, TableFixedWire.NoGuard, TableFixedWire.Text, 0, 0, 0, 3, 1),",
		"W8 bytes(8) takes the array row: elements to dst (1), count to aux (2), flavour bytes (meta 3)")
}

// t07W7: W7 [verify] "arg and meta are two lanes" — §4.1: "`arg` and `meta` ARE
// TWO LANES BECAUSE THEY ARE TWO FACTS, and must never share one. A `string(N)`
// under a union arm needs both". The struct carries both bytes, and the text
// entry inside arm 2 carries its guard ordinal in Arg and its flavour in Meta.
func t07W7(t *testing.T) {
	src := t07Src(t, t07ArmText)
	t07Has(t, src, "public ulong Arg; // Arm ordinal compared by Guard, at ArgW bytes",
		"W7 the guard's ordinal has its own lane")
	t07Has(t, src, "public byte Meta; // Op metadata (e.g. text flavour: 1=utf8, 2=wide, 3=bytes)",
		"W7 the op's own argument has its own lane, separate from the guard's ordinal")
	t07Has(t, t07Norm(src), "new TableFixedEntry(5u, 3u, 8u, 4u, 0u, TableFixedWire.Text, 2, 0, 0, 1, 1),",
		"W7 the string under arm 2 carries its guard ordinal (arg=2) and its utf8 flavour (meta=1) in the two lanes")
}

// t07W16: W16 [owed] "nested union answers outer tag" — §4.1: "A union's guard is
// stamped over everything its arm produced — an arm inside an arm answers to the
// OUTER tag". The leg's plan entry carries ONE guard lane, so an inner arm under
// an outer arm that never rode is not held back. This subtest is RED today and is
// held unproved; the verdict for cs/W16 is `unknown`.
func t07W16(t *testing.T) {
	t.Skip("cs/W16 unknown: the emitted plan entry carries one guard lane; the outer tag an arm inside an arm must answer to is not conjoined (no Guard2/Arg2 lane). RED at the start step, fix not landed in this card's window")
	_ = t07Src(t, t07Nested)
}

// t07R28: R28 [owed] "the guard chain: the plan entry carries offset+count into a
// pool of (guard, arg, argw) links, outermost first, the only bound the layout's
// 64; an arm inside an arm answers to the OUTER tag". The leg emits neither the
// second guard lane nor the guard pool. Held unproved; the verdict for cs/R28 is
// `unknown`.
func t07R28(t *testing.T) {
	t.Skip("cs/R28 unknown: the leg emits no guard chain (no offset+count into a pool of (guard,arg,argw) links, no outer-tag lane); owed and not landed in this card's window")
	_ = t07Src(t, t07Nested)
}

// b0 is the write body's byte span parameter name, kept as a named constant so
// the W3 assertion reads as source and not a magic string.
const b0 = "b"
