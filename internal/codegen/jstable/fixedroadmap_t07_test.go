package jstable

// fixedroadmap_t07 — the js leg's live-extents, fixed-image-writes and
// union-guards rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"): the clauses no other test of this leg asserts. Law:
// docs/FIXED-FORM-ALGORITHM.md §3.1 (the write: a template then stores, the
// slack rule, A union writes the tag and the taken arm only, write-side checks
// are DEBUG only), §4.6 (the bounds pass walks only what a read can have
// written), docs/SPEC-TABLES.md §3.4 (slack is unspecified on read), and the
// fixed-form runtime this leg emits. One subtest per task id, each clause read
// off the JavaScript this leg emits.
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE: every clause is the
// compiler's emitted module, so the suite RUNS on a tree that never built the
// corpus.

import (
	"strings"
	"testing"
)

// t07Table is the fixture the live-extents and writing clauses read: a counted
// array, a bytes(N), a string(N), an optional and a two-arm union in one body.
const t07Table = `package probe

type Boost { power int32 = 1 }
type Ward  { charge int32 = 2 }

union Effect
{
    boost Boost
    ward  Ward
}

fixed table T
{
    items [..2]int32 | min = 0, max = 1000
    d     bytes(4)
    name  string(8)
    e     Effect
    number   ?int32
    tail  int32 = 7
}
`

// t07Bytes is the smallest table whose field is a bytes(N): its dst row is the
// counted array's and nothing else sits in front to shift the offsets.
const t07Bytes = `package probe

fixed table B
{
    d bytes(4)
}
`

// t07Narrow is W3's fixture: a union whose two arms have DIFFERENT widths, so
// the narrower arm (int8, one byte) is followed by the wider arm's bonus bytes
// (int32, four) — three bytes of declared slack the writer never stores. The
// earlier fixture's arms were both int32, so the slot had no slack and the
// "narrower" clause could not be observed.
const t07Narrow = `package probe

type NarrowArm { a int8 = 0 }
type WideArm  { b int32 = 0 }

union Pick
{
    narrow NarrowArm
    wide   WideArm
}

fixed table Root
{
    pick Pick
    tail int32 = 7
}
`

// t07Nested is W16's fixture: an arm of `Outer` is itself the union `Inner`, so
// the inner union's tag is only reachable once the OUTER tag has selected the
// arm — an arm inside an arm answers to the outer tag.
const t07Nested = `package probe

type Leaf { x int32 = 0 }
type Other { y int32 = 0 }

union Inner
{
    leaf  Leaf
    other Other
}

union Outer
{
    inner Inner
    other Other
}

fixed table Root
{
    pick Outer
    tail int32 = 7
}
`

func TestFixedRoadmapJsT07Writes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/C14", t07C14},
		{"js/W4", t07W4},
		{"js/W9", t07W9},
		{"js/W1", t07W1},
		{"js/W3", t07W3},
		{"js/W5", t07W5},
		{"js/W8", t07W8},
		{"js/W7", t07W7},
		{"js/W16", t07W16},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t07C14: C14 [verify] "bounds pass walks live only" (§4.6: "It walks only
// what a read can have written — a counted array's LIVE elements and never its
// slack, an optional's payload only when the present byte says so"). The
// emitted decode drives a counted array's loop from the live count and gates
// an optional's payload behind its present byte.
func t07C14(t *testing.T) {
	_, src := t02Table(t, t07Table)
	dec := t02Norm(jsFn(src, "TFixedDecode"))
	t02Has(t, dec, "for (let i = 0; i < value.itemsCount; i++) {",
		"C14 the counted array's bounds pass walks its live count")
	t02Has(t, dec, "if (value.numberPresent) {",
		"C14 an optional's payload is walked only when its present byte says so")
	// The live count, not the declared bound, is the loop bound.
	if strings.Contains(dec, "for (let i = 0; i < 2; i++) {") {
		t.Error("C14: the counted array's pass walks its declared bound, not its live count")
	}
}

// t07W4: W4 [weak] "read slack unspecified" (§3.1: "Slack is UNSPECIFIED on
// read and not a refusal: a reader validates the USED UNITS only, and non-zero
// slack is not malformed, not a refusal, and moves no counter."). The emitted
// decode never looks past the used units: the range clamp is inside the live
// loop and no content check sets malformed.
func t07W4(t *testing.T) {
	_, src := t02Table(t, t07Table)
	dec := t02Norm(jsFn(src, "TFixedDecode"))
	live := t02Block(t, dec, "for (let i = 0; i < value.itemsCount; i++) {", "}")
	if !strings.Contains(live, "report.clamped++") {
		t.Error("W4: the range clamp does not stand inside the live loop, so slack would be clamped")
	}
	if strings.Contains(dec, "malformed") {
		t.Error("W4: the decode refuses on body content; non-zero slack is not malformed")
	}
}

// t07W9: W9 [verify] "prefill unwritten ranges only" (§3.1's reader: the
// prefill is the declared defaults over exactly the ranges the plan does not
// write). The load lays the prefill over the hole list TableFixedHoles
// computes, and that list skips every guarded row so its defaults survive.
func t07W9(t *testing.T) {
	_, src := t02Table(t, t07Table)
	t02Has(t, t02Norm(src), "const holeN = TableFixedHoles(entries, entryCount, plan.cover, plan.holes);",
		"W9 the prefill covers the plan's hole list")
	t02Has(t, src, "for (let i = 0; i < z; i++) { image[o + i] = TFixedPrefill[o + i]; }",
		"W9 the load lays the prefill over a hole range only")
	holes := t02Norm(jsFn(src, "TableFixedHoles"))
	t02Has(t, holes, "if (e[b + TableFixedLaneGuard] !== TableFixedNoGuard) { continue; }",
		"W9 a guarded row stays a hole, so an untaken arm keeps its declared default")
}

// t07W1: W1 [verify] "write slack is template zeros" (§3.1: "The template is a
// compile-time constant — the hash, then zero everywhere a value lands — which
// zero-fills every byte of declared slack without the writer touching it."). The
// writer zero-fills each record's body from the template before the stores, and
// writes only the live array count and text length, never all Max.
func t07W1(t *testing.T) {
	_, src := t02Table(t, t07Table)
	save := jsFn(src, "TFixedSave")
	t02Has(t, save, "bytes.fill(0, at + 8, at + TFixedRecordBytes); // the template's zeros",
		"W1 each record's declared slack is the template's zeros")
	write := t02Norm(jsFn(src, "TFixedWriteBody"))
	t02Has(t, write, "for (let i = 0; i < value.itemsCount; i++) {",
		"W1 a counted array writes its live count and stops")
	t02Has(t, write, "for (let i = 0; i < value.nameLength; i++) {",
		"W1 text writes its used length, never the whole declared span")
}

// t07W3: W3 [owed] "zero behind a narrower arm" (§3.1: "A union writes the tag
// and the taken arm only, zero behind a narrower one."). The arms DIFFER in
// width, so the narrower arm leaves bytes of declared slack the writer never
// stores. This case stains the whole destination with 0x5A before the save and
// reads the wire back: a writer that copied storage instead of laying the
// template (whose slack is zero) would carry the stain onto the wire, and the
// control writes the same slot with the WIDER arm, whose bytes are legitimately
// nonzero, so the zero check is a fact about the narrower arm and not a slot
// that is universally zero.
func t07W3(t *testing.T) {
	node := jsNode(t)
	table := jsFixedRootName(t, t07Narrow)
	if out, err := jsRunVersionProbe(t, node, t07Narrow, nil, 0, table, `
const { PickType } = await import("./Probe.js");
const { RootFixedRecordBytes } = await import("./ProbeTable.js");
const v = new T(); InitT(v);
v.pick.type = PickType.Narrow;
v.pick.narrow.a = 0x7E;
v.tail = 7;
const buf = new Uint8Array(TMeasure(1));
buf.fill(0x5A); // THE CANARY: a writer that copied storage would carry it
if (TSave([v], 1, buf) !== buf.length) { fail("W3: the narrow-arm save"); }
const body = buf.subarray(buf.length - RootFixedRecordBytes + 8);
if (body[0] !== PickType.Narrow) { fail("W3: the tag is " + body[0]); }
if (body[1] !== 0x7E) { fail("W3: the narrow arm's live byte is " + body[1]); }
for (let i = 2; i < 5; i++) {
  if (body[i] !== 0) { fail("W3: the slack behind the narrower arm is not the template's zero at body[" + i + "]=" + body[i] + "; the 0x5A stain reached the wire"); }
}
if (body[5] !== 7 || body[6] !== 0 || body[7] !== 0 || body[8] !== 0) {
  fail("W3: the field past the union is not the writer's 7: " + Array.from(body.subarray(5, 9)));
}
const w = new T(); InitT(w);
w.pick.type = PickType.Wide;
w.pick.wide.b = 0x11223344;
w.tail = 7;
const wbuf = new Uint8Array(TMeasure(1));
wbuf.fill(0x5A);
if (TSave([w], 1, wbuf) !== wbuf.length) { fail("W3: the wide-arm save"); }
const wbody = wbuf.subarray(wbuf.length - RootFixedRecordBytes + 8);
for (let i = 2; i < 5; i++) {
  if (wbody[i] === 0) { fail("W3 CONTROL: the wider arm's byte " + i + " is zero, so the slot is not universally zero"); }
}
`); err != nil {
		t.Fatalf("W3, a narrow-arm save over a 0x5A-stained destination: %v\n%s", err, out)
	}
}

// t07W5: W5 [owed] "write checks DEBUG only" (§3.1: "Write-side bound checks
// are DEBUG ONLY, by rule. `count <= Max` and `length <= N` are a caller
// contract; a release build removes them exactly as it removes `assert`, pays
// nothing, and clamps nothing."). A JavaScript build has no debug tier, so the
// emitted write path carries no bound check and no counter.
func t07W5(t *testing.T) {
	_, src := t02Table(t, t07Table)
	for _, fn := range []string{"TFixedWriteBody", "TFixedSave"} {
		body := jsFn(src, fn)
		if strings.Contains(body, "report.") {
			t.Errorf("W5: %s carries a write-side check or counter; write checks are DEBUG only, and this build clamps nothing", fn)
		}
	}
}

// t07W8: W8 [weak] "bytes(N) takes the array row" (§3.4; fixedform.go's bytes
// case: "the row is the COUNTED ARRAY's row and not the text row: on an array
// entry `dst` is the ELEMENT BASE and `aux` is where the count lands"). A
// bytes(4) field's dst row is dst = 4 (the element base, behind the u32 count),
// stride 1, aux = 0 (the count), counted, and its flavour is bytes.
func t07W8(t *testing.T) {
	_, src := t02Table(t, t07Bytes)
	t02Has(t, src, "  4, 1, 0, 1, 3, // 1 d",
		"W8 bytes(N)'s dst row is the counted array's: element base, aux at the count, counted, bytes flavour")
	t02Has(t, src, "  0, 0, 0, 0, 0, // 2 u8",
		"W8 the bytes(N) row is followed by its u8 element, the array shape")
}

// t07W7: W7 [verify] "arg and meta are two lanes" (§3.4; the runtime's text
// op: "THE FLAVOUR RIDES IN META, NOT IN ARG. Arg is the GUARD'S VALUE ... A
// text field under a union arm needs BOTH, so they cannot share a lane."). The
// plan push writes the field's flavour to meta while leaving arg the arm's
// guard value.
func t07W7(t *testing.T) {
	_, src := t02Table(t, t07Table)
	n := t02Norm(src)
	t02Has(t, n, "TableFixedPush(plan, TableFixedOpText, theirAt, at, units, auxAt, guard, arg, dst[row + TableFixedDstArg]);",
		"W7 the text push leaves arg the guard's value and writes the flavour to meta")
}

// t07W16: W16 [owed] "nested union answers outer tag" (§5.8 row 6, §4.1: "an
// arm inside an arm answers to the OUTER tag"). `Outer`'s `inner` arm is the
// union `Inner`, so the inner union is derivable only through the outer tag.
// Both the emitted writer and the emitted reader NEST the inner union's switch
// inside the OUTER arm's `case 1`, which is exactly "an arm inside an arm
// answers to the outer tag": the inner tag is only consulted once the outer tag
// has selected the arm that holds it. An emitter that lifted the inner union
// out of the outer case — or reached it under another arm — fails here.
//
// THE RUNTIME HALF IS NOT PROVABLE ON THIS LEG: the shared JS type emitter
// (internal/codegen/js, outside this card's PATHS) does not emit the class for
// a union whose arm is itself a union, so `<home>Table.js` references an
// `Outer` no module exports and the fixture cannot be constructed or run. The
// verdict for W16 therefore stays `unknown`; this case pins only the nesting the
// jstable emitter writes.
func t07W16(t *testing.T) {
	_, src := t02Table(t, t07Nested)
	write := t02Norm(jsFn(src, "RootFixedWriteBody"))
	dec := t02Norm(jsFn(src, "RootFixedDecode"))
	t02Has(t, write,
		"case 1: { view.setUint8(at + 1, value.pick.inner.type); switch (value.pick.inner.type) { case 1: {",
		"W16 the inner union's WRITE opens inside the outer arm's case 1, so it answers the outer tag")
	t02Has(t, dec,
		"case 1: { value.pick.inner.type = view.getUint8(at + 1); if (value.pick.inner.type > 2) {",
		"W16 the inner union's READ opens inside the outer arm's case 1, so it answers the outer tag")
}
