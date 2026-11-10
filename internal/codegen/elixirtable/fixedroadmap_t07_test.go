package elixirtable

// THE ROADMAP'S LIVE-EXTENTS, WRITING AND UNION-GUARD ROWS ON THE ELIXIR LEG
// (docs/roadmap.sexp node `fixed-tables`, work-sets `live-extents/elixir`,
// `writing/elixir` and `union-guards/elixir`; ROADMAP.md "NEW Fixed Tables").
// Law: docs/FIXED-FORM-ALGORITHM.md §3.1 (the write), §4.1 (the plan), §4.3
// (the prefill), §4.6 (the bounds pass) and docs/SPEC-TABLES.md §3.4. One
// subtest per task id this card proves, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling runtime checkout.
//
// elixir/C14 [verify]: the bounds pass walks LIVE elements only — the runtime's
// `:live` gate and `:present` gate, and the identity projection stops at the
// count and gates an optional on its present byte.
//
// elixir/W4 [verify]: read slack is unspecified — the read binds a text field's
// used span and never reads, refuses or counts the bytes behind it.
//
// elixir/W9 [verify]: prefill covers the unwritten ranges only — the plan runs
// against one image born from the prefill, and the assembler fills the gaps the
// plan leaves.
//
// elixir/W1 [verify]: write slack is template zeros — a short text, a short
// counted array and a union's unnamed arm are each padded with a zero segment.
//
// elixir/W3 [owed]: zero behind a narrower arm — a union's narrower arm is
// padded with zeros out to the widest arm's width.
//
// elixir/W5 [weak]: write checks are a debug-assert equivalent — the C++
// reference's `schema_assert`/NDEBUG rule, this port raising because the BEAM
// has no compile-out assert.
//
// elixir/W8 [verify]: bytes(N) takes the array row — Dst the buffer, Aux the
// count; a text field's row is the other way round.
//
// elixir/W7 [verify]: arg and meta are two lanes — a guarded text entry carries
// the arm ordinal and the text flavour in two distinct tuple positions.
//
// elixir/W16 [owed]: nested union answers outer tag — an arm that is itself a
// union writes and reads through that union's own helper, reached only when the
// OUTER tag names the arm, and its own arm selection survives the outer stamp.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t07Filter is the one emitted <Base>Fixed.ex module of a unit.
func t07Filter(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// t07Module is the <Base>Fixed.ex module of one schema FILE of a unit.
func t07Module(t *testing.T, tables map[string][]byte, base string) string {
	t.Helper()
	name := base + FixedModuleSuffix + ".ex"
	data, ok := tables[name]
	if !ok {
		t.Fatalf("no %s module was emitted", name)
	}
	return string(data)
}

// t07Has is a required substring of a source, named by rule.
func t07Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the source does not carry %q", what, want)
	}
}

// t07Files builds a unit from two in-memory schema files, the shape a fixed
// form crosses when a union arm names a union declared in ANOTHER file. Each
// file is its own check.SourceFile with its own Base, so a declaration's file
// of record (unit.DeclFile) keeps the two sides apart the way a real unit does.
func t07Files(t *testing.T, xa, xb string) *ir.Unit {
	t.Helper()
	mk := func(base, src string) check.SourceFile {
		name := base + ".schema"
		ast, errs := parser.Parse(name, []byte(src))
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", name, errs)
		}
		return check.SourceFile{Path: name, Name: name, Base: base, Bytes: []byte(src), AST: ast}
	}
	u, cerrs := check.Unit([]check.SourceFile{mk("xa", xa), mk("xb", xb)})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

// t07LiveSource is the shared fixture for the live-extents and writing rows: a
// counted array, a string, a bytes(N), a union on a narrow and a wide arm, an
// optional and a ranged scalar.
const t07LiveSource = `package probe

enum Grade { Bronze, Gold }

union Pick
{
    a int32
    b int64
}

fixed table Root
{
    lead  uint32 = 1
    tiers [..4]Grade
    name  string(8)
    blob  bytes(6)
    pick  Pick
    opt   ?int32 | min = 0, max = 10
    n     int32 | min = 0, max = 10
    tail  uint32 = 2
}
`

// ---- elixir/C14 -------------------------------------------------------------

// t07C14 holds elixir/C14 to §4.6: the bounds pass walks only what a read can
// have written — a counted array's LIVE elements and never its slack, an
// optional's payload only when the present byte says so. On a compiled plan the
// runtime's `:live` and `:present` gates carry it; on the identity plan the
// generated projection stops at the count and gates the optional's clamp.
func t07C14(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "defp step([{:live, count_dst, index, inner} | rest]", "C14 the live gate's step")
	t07Has(t, fixedRuntimeBody, "defp live_at(writes, dst) do", "C14 the live gate reads the landed count")
	t07Has(t, fixedRuntimeBody, "defp live_wrap_entry(e, dst, i), do: {:live, dst, i, e}", "C14 a counted run is wrapped live")
	t07Has(t, fixedRuntimeBody, "defp step([{:present, src, inner} | rest]", "C14 an optional's payload is gated on its present byte")

	out, err := Generate(unitFrom(t, t07LiveSource))
	if err != nil {
		t.Fatalf("C14: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "root_tiers_fixed_list(_bin, 0, c, m), do: {[], c, m}", "C14 the identity walk stops at the count")
	t07Has(t, body, "R.past(c, f_e_0, 2)", "C14 an enum ordinal's bound runs only over live elements")
	t07Has(t, body, "c = if(p_opt != 0, do: c_opt, else: c)", "C14 an optional's payload is held to a bound only when present")
}

// ---- elixir/W4 --------------------------------------------------------------

// t07W4 holds elixir/W4 to §3.1: slack is UNSPECIFIED on read and not a
// refusal — a reader validates the USED UNITS only. The read binds a text
// field's whole span, decodes the used units, and never refuses or counts the
// bytes behind them; the writer's own `text_slack` is the only site that
// touches the slack.
func t07W4(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "def text(used, _default, m, present) do", "W4 the read decodes the used bytes")
	t07Has(t, fixedRuntimeBody, "def text_slack(bytes, n) when is_binary(bytes) and byte_size(bytes) <= n,", "W4 the writer owns the slack")

	out, err := Generate(unitFrom(t, t07LiveSource))
	if err != nil {
		t.Fatalf("W4: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "R.text(binary_part(b_name, 0, min(max(n_name, 0), 8))", "W4 the read decodes the used bytes of a string(N)")
	t07Has(t, body, "binary_part(b_blob, 0, min(max(n_blob, 0), 6))", "W4 the read lands the used bytes of a bytes(N)")
}

// ---- elixir/W9 --------------------------------------------------------------

// t07W9 holds elixir/W9 to §4.3: the plan runs against ONE image born from the
// prefill, and the assembler fills only the ranges the plan did not write from
// that same prefill, so an unwritten range is the declared default.
func t07W9(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "def run(entries, body, prefill, report) do", "W9 the plan runs against the prefill")
	t07Has(t, fixedRuntimeBody, "defp assemble(writes, prefill) do", "W9 the assembler fills the unwritten ranges")

	out, err := Generate(unitFrom(t, t07LiveSource))
	if err != nil {
		t.Fatalf("W9: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "R.run(plan, R.detach(body, copy), @", "W9 the read runs the plan against the prefill constant")
	t07Has(t, body, "_prefill, report)", "W9 the prefill constant is the image's seed")
}

// ---- elixir/W1 --------------------------------------------------------------

// t07W1 holds elixir/W1 to §3.1: the write lays the live extent onto the zeroed
// template and stops — a short text pads its slack with a zero segment, a short
// counted array pads its unused slots, and a union's unnamed arm is zeros at
// the widest width.
func t07W1(t *testing.T) {
	out, err := Generate(unitFrom(t, t07LiveSource))
	if err != nil {
		t.Fatalf("W1: Generate: %v", err)
	}
	body := t07Filter(t, out)
	t07Has(t, body, "0::size(R.text_slack(v_name, 8))-unit(8)", "W1 a short string pads its slack with zeros")
	t07Has(t, body, "0::size((4 - n_tiers) * 1)-unit(8)", "W1 a short counted array pads its unused slots with zeros")
	t07Has(t, body, "0::size(8)-unit(8)", "W1 a union's unnamed arm is zeros at the widest width")
}

// ---- elixir/W3 --------------------------------------------------------------

// t07W3 holds elixir/W3 to §3.1: a union writes the tag and the taken arm only,
// zero behind a narrower one. The arm `a int32` (4 bytes) behind the widest
// `b int64` (8 bytes) is padded with a four-byte zero segment.
func t07W3(t *testing.T) {
	out, err := Generate(unitFrom(t, t07LiveSource))
	if err != nil {
		t.Fatalf("W3: Generate: %v", err)
	}
	body := t07Filter(t, out)
	if !strings.Contains(body, "{:a, v_a} -> <<acc::binary, R.fits(v_a, 32, true)::little-signed-32, 0::size(4)-unit(8)>>") {
		t.Error("W3: the narrower arm `a int32` leaves the four bytes behind it unzeroed")
	}
}

// ---- elixir/W5 --------------------------------------------------------------

// t07W5 holds elixir/W5 to §3.1 and the C++ reference's NDEBUG rule: the
// write-side bound check is a debug-assert equivalent. The BEAM has no
// compile-out assert, so this port raises, exactly as its packet codec does for
// the same field; the read side still clamps in every build.
func t07W5(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "NDEBUG", "W5 the C++ reference's NDEBUG assert is the rule")
	t07Has(t, fixedRuntimeBody, "def ranged(value, {lo, hi}) when is_integer(value) and value >= lo and value <= hi, do: value", "W5 the write check passes an in-range value")
	t07Has(t, fixedRuntimeBody, "raise ArgumentError, \"#{inspect(value)} is outside the declared range #{lo}..#{hi}\"", "W5 the write check raises on a caller's bug")
}

// ---- elixir/W8 --------------------------------------------------------------

// t07W8 holds elixir/W8 to §4.1: a bytes(N) takes the ARRAY's row — Dst the
// buffer, Aux the count — where a text field's row is the other way round (Dst
// the length, Aux the buffer). The image destination rows carry it.
func t07W8(t *testing.T) {
	u := unitFrom(t, t07LiveSource)
	st := u.Tables["Root"]
	w := ir.TableFixedWalkRoot(st)
	dst := imageDstRows(u, w)
	var blob, name *fixedDst
	for i, e := range w {
		switch e.Note {
		case "blob":
			blob = &dst[i]
		case "name":
			name = &dst[i]
		}
	}
	if blob == nil || name == nil {
		t.Fatal("W8: the walk carries no blob/name destination row")
	}
	if blob.counted != 1 || blob.stride != 1 || blob.arg != fixedTextBytes {
		t.Errorf("W8: bytes(N)'s row is (counted %d, stride %d, arg %d), want the ARRAY row (1, 1, %d)", blob.counted, blob.stride, blob.arg, fixedTextBytes)
	}
	if blob.dst != blob.aux+fixedCountBytes {
		t.Errorf("W8: bytes(N)'s Dst (%d) is not the buffer past the count field (Aux %d + 4)", blob.dst, blob.aux)
	}
	if name.counted != 0 || name.arg != fixedTextUtf8 {
		t.Errorf("W8: the text row is (counted %d, arg %d), want the TEXT row (0, %d)", name.counted, name.arg, fixedTextUtf8)
	}
	if name.dst != name.aux-fixedCountBytes {
		t.Errorf("W8: the text Dst (%d) is not the length field before the buffer (Aux %d - 4)", name.dst, name.aux)
	}
}

// ---- elixir/W7 --------------------------------------------------------------

// t07W7 holds elixir/W7 to §4.1: arg is the guard's ordinal and meta is the
// op's own argument (a text flavour), TWO LANES and never one. The runtime's
// guard tuple carries the ordinal in its own lane and its text tuple carries the
// flavour in two distinct positions, so a byte string under an arm keeps its own
// flavour.
func t07W7(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "defp step([{:guard, gsrc, tag, argw, inner} | rest]", "W7 the guard carries the arm ordinal in its own lane")
	t07Has(t, fixedRuntimeBody, "defp step([{:text, src, dst, aux, size, flavour} | rest]", "W7 the text op carries the flavour in its own lane")
	t07Has(t, fixedRuntimeBody, "if text_ok?(flavour, binary_part(take, 0, v)) do", "W7 the flavour decides the content rule, never the guard")
}

// ---- elixir/W16 -------------------------------------------------------------

// t07W16 holds elixir/W16 to §4.1: an arm that is itself a union answers the
// OUTER tag — the outer union's dispatch names the arm, and inside that arm the
// inner union's own projection runs, so its arm selection survives the outer
// stamp. Before the emitter knew a nested union arm, arm 1 here was a zero-width
// leaf (`R.fits(value.c, 0, false)::little-unsigned-0`) and the record could not
// round-trip.
func t07W16(t *testing.T) {
	out, err := Generate(unitFrom(t, `package probe

union Inner
{
    a A
    b B
}

union Outer
{
    c Inner
    d D
}

type A { v int32 }
type B { v int64 }
type D { v int32 }

fixed table Root
{
    pick  Outer
    tail  uint32 = 2
}
`))
	if err != nil {
		t.Fatalf("W16: Generate: %v", err)
	}
	body := t07Filter(t, out)
	if !strings.Contains(body, "{:c, v_c} -> inner_fixed_write_into(v_c, acc)") {
		t.Error("W16: the nested union arm is not written through its own union writer")
	}
	if !strings.Contains(body, "{v, c, m} = inner_fixed_decode(arm, c, m)") {
		t.Error("W16: the nested union arm is not read through its own union projection")
	}
	// THE CALLEE MUST EXIST, not only the call site: an arm that is itself a
	// union is a call to THAT union's own helper, so the nested union's writer,
	// its projection and its own arm structs must all be emitted beside the
	// call (§4.1 "an arm inside an arm answers to the OUTER tag").
	t07Has(t, body, "def inner_fixed_write_into(value, acc) do", "W16 the nested union's own writer is emitted")
	t07Has(t, body, "def inner_fixed_decode(<<tag::little-unsigned-8, arm::binary>>, c, m", "W16 the nested union's own projection is emitted")
	t07Has(t, body, "def a_fixed_write_into(value, acc) do", "W16 the inner arm `a`'s writer is emitted")
	t07Has(t, body, "def b_fixed_write_into(value, acc) do", "W16 the inner arm `b`'s writer is emitted")

	// THE SAME HOLD ACROSS A FILE BOUNDARY. The outer union lives in one file,
	// the union-in-union arm in another; the nested union's own helper must be
	// emitted in the ARM'S OWN MODULE (the file that declares it), reached by
	// the caller through its qualifier — not only the call site, and not only
	// when both unions share a file.
	cross, err := Generate(t07Files(t, `package probe

union Outer
{
    c Inner
    d D
}

type D { v int32 }

fixed table Root
{
    pick  Outer
    tail  uint32 = 2
}
`, `package probe

union Inner
{
    a A
    b B
}

type A { v int32 }
type B { v int64 }
`))
	if err != nil {
		t.Fatalf("W16: cross-file Generate: %v", err)
	}
	xaBody := t07Module(t, cross, "xa")
	xbBody := t07Module(t, cross, "xb")
	t07Has(t, xaBody, "Probe.XbFixed.inner_fixed_write_into(v_c, acc)", "W16 the cross-file caller reaches the nested union through its own module")
	t07Has(t, xbBody, "def inner_fixed_write_into(value, acc) do", "W16 the nested union's own writer is emitted in the arm's own module")
	t07Has(t, xbBody, "def inner_fixed_decode(<<tag::little-unsigned-8, arm::binary>>, c, m", "W16 the nested union's own projection is emitted in the arm's own module")
	t07Has(t, xbBody, "def a_fixed_write_into(value, acc) do", "W16 the cross-file inner arm `a`'s writer is emitted")
	t07Has(t, xbBody, "def b_fixed_write_into(value, acc) do", "W16 the cross-file inner arm `b`'s writer is emitted")
}

// ---- elixir/R28 -------------------------------------------------------------

// t07R28 holds elixir/R28 to §4.1 and §5.8 row 6: the plan entry carries a guard
// CHAIN — offset+width links, OUTERMOST first — and an arm inside an arm answers
// to the OUTER tag too, not only its own. The runtime's `guarded` folds a list
// of {at, argw} links (with the parallel ordinals) into nested `:guard` tuples
// outermost-first, and `compile_union` CONJOINS each inner arm's guard onto the
// chain it inherited, so a THIRD nested union answers to every tag that rode
// above it. The chain's only bound is the layout's own 64 (`@max_depth`).
func t07R28(t *testing.T) {
	t07Has(t, fixedRuntimeBody, "defp guarded(acc, [], _tags, entry), do: push(acc, entry)", "R28 the empty guard chain pushes the bare entry")
	t07Has(t, fixedRuntimeBody, "defp guarded(acc, guards, tags, entry) do", "R28 the guard chain folds the links")
	t07Has(t, fixedRuntimeBody, "guard ++ [{their_at, argw}], tag ++ [j + 1]", "R28 an inner arm conjoins the OUTER tag it rode in on")
	t07Has(t, fixedRuntimeBody, "@max_depth 64", "R28 the chain's only bound is the layout's own 64")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapElixirT07Writes(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/C14", check: t07C14},
		{id: "elixir/W4", check: t07W4},
		{id: "elixir/W9", check: t07W9},
		{id: "elixir/W1", check: t07W1},
		{id: "elixir/W3", check: t07W3},
		{id: "elixir/W5", check: t07W5},
		{id: "elixir/W8", check: t07W8},
		{id: "elixir/W7", check: t07W7},
		{id: "elixir/W16", check: t07W16},
		{id: "elixir/R28", check: t07R28},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
