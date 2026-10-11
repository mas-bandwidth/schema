package jstable

// fixedroadmap_t09 — the js leg's unions-and-optionals rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the
// capability assertions no other test of this leg makes. Law: docs/SPEC-TABLES.md
// §3.4 (the fixed form's kind table and its constant-size rows) and
// docs/FIXED-FORM-ALGORITHM.md §2 (the walk) and §5 (the lineage). One subtest
// per task this card proves: the leg WRITES AND READS the construct through the
// generated module under the leg's own node, with non-default and boundary
// values, asserted on the value that comes back.
//
// A payload-free-arms row carries a void (payload-free) union arm: it is a value
// of the tag enum with no storage in the overlay and no bytes to land (§3.4,
// kind 32), and the layout names it with a zero-size TableKindNoPayload entry
// (ir/fixedform.go's tableFixedWalkElement), so the closure test now admits it
// and the subtest below writes and reads one. A union-field-arms row carries a
// NAMED struct arm whose payload holds a bounded string and a bounded array —
// the shape the go sibling proves green (internal/codegen/gotable/fixedroadmap_t11_test.go)
// and the one docs/roadmap.sexp:6731 calls "a separate supported shape" — so the
// union-field-arms subtest below writes and reads text and an array inside an
// arm's struct payload.

import (
	"testing"
)

// t09Probe is one capability probe: generate the fixture's module, run the
// probe body under the leg's node, and assert the module answers `ok`. It rides
// the versioning harness's own runner (jsRunVersionProbe with no older lineage),
// so the emitted files and the node the leg already uses are the ones under
// test and no new helper module is introduced.
func t09Probe(t *testing.T, src, table, body string) {
	t.Helper()
	node := jsNode(t)
	out, err := jsRunVersionProbe(t, node, src, nil, 0, table, body)
	if err != nil {
		t.Fatalf("capability probe: %v\n%s", err, out)
	}
}

// TestFixedRoadmapJsT09UnionsOptionals holds one subtest per task this card
// proves, each the clause its task title states, written and read back through
// the emitted module.
func TestFixedRoadmapJsT09UnionsOptionals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"union-field-arms/js/valid-data", t09UnionFieldArms},
		{"payload-free-arms/js/valid-data", t09PayloadFreeArms},
		{"union-arrays/js/valid-data", t09UnionArrays},
		{"optional-arrays/js/valid-data", t09OptionalArrays},
		{"scalar-defaults/js/valid-data", t09ScalarDefaults},
		{"text-bytes-flags-defaults/js/valid-data", t09TextBytesFlagsDefaults},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t09UnionFieldArms: union-field-arms [capability] "Union arms holding scalar,
// text or array fields" (§3.4: an arm's payload is a by-value record at its
// bound, so a scalar field, a bounded text field and a bounded array each land
// under their own tag). The text arm writes its full declared width and the
// array arm its declared maximum, so a payload that only carried an empty text
// or array does not pass.
func t09UnionFieldArms(t *testing.T) {
	const src = `package probe

type ScalarArm { n int32 }
type TextArm { s string(8) }
type ArrayArm
{
    a [..3]int32
}

union Body
{
    sa ScalarArm
    ta TextArm
    aa ArrayArm
}

fixed table T { body Body }
`
	const probe = `
const { BodyType } = await import("./Probe.js");
const v = new T(); InitT(v);
v.Body.Type = BodyType.Sa;
v.Body.Sa.N = -5;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("union-field-arms: save scalar"); }
const back = [new T()]; InitT(back[0]);
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, 1, buf, buf.length, plan, report);
if (!(n === 1 && !report.malformed)) { fail("union-field-arms: load scalar " + reason(report)); }
if (!(back[0].Body.Type === BodyType.Sa && back[0].Body.Sa.N === -5)) {
  fail("union-field-arms: the scalar arm did not round trip");
}

const t2 = new T(); InitT(t2);
t2.Body.Type = BodyType.Ta;
const text = "abcdef";
for (let i = 0; i < text.length; i++) { t2.Body.Ta.S[i] = text.charCodeAt(i); }
t2.Body.Ta.SLength = text.length;
const buf2 = new Uint8Array(TMeasure(1));
if (TSave([t2], 1, buf2) !== buf2.length) { fail("union-field-arms: save text"); }
const back2 = [new T()]; InitT(back2[0]);
const report2 = new TableFixedReport();
const n2 = TLoad(back2, 1, buf2, buf2.length, TNewPlan(4096, 4096), report2);
if (!(n2 === 1 && !report2.malformed)) { fail("union-field-arms: load text " + reason(report2)); }
let got = "";
for (let i = 0; i < back2[0].Body.Ta.SLength; i++) { got += String.fromCharCode(back2[0].Body.Ta.S[i]); }
if (!(back2[0].Body.Type === BodyType.Ta && got === "abcdef")) {
  fail("union-field-arms: the text arm did not round trip, got " + JSON.stringify(got));
}

const a3 = new T(); InitT(a3);
a3.Body.Type = BodyType.Aa;
a3.Body.Aa.ACount = 3;
a3.Body.Aa.A[0] = -2147483648;
a3.Body.Aa.A[1] = 0;
a3.Body.Aa.A[2] = 2147483647;
const buf3 = new Uint8Array(TMeasure(1));
if (TSave([a3], 1, buf3) !== buf3.length) { fail("union-field-arms: save array"); }
const back3 = [new T()]; InitT(back3[0]);
const report3 = new TableFixedReport();
const n3 = TLoad(back3, 1, buf3, buf3.length, TNewPlan(4096, 4096), report3);
if (!(n3 === 1 && !report3.malformed)) { fail("union-field-arms: load array " + reason(report3)); }
if (!(back3[0].Body.Type === BodyType.Aa && back3[0].Body.Aa.ACount === 3 &&
      back3[0].Body.Aa.A[0] === -2147483648 && back3[0].Body.Aa.A[2] === 2147483647)) {
  fail("union-field-arms: the array arm did not round trip");
}
`
	t09Probe(t, src, "T", probe)
}

// t09PayloadFreeArms: payload-free-arms [capability] "Payload-free union arms"
// (§3.4 and docs/FIXED-FORM-ALGORITHM.md §2 kind 32: a payload-free arm's layout
// entry is the tag ordinals and nothing lands). The subtest writes and reads TWO
// payload-free arms — one BEFORE and one AFTER a payload arm, so the arm list's
// order is exercised — and proves a named payload-free arm is DISTINCT from the
// union's None: a default-constructed record reads back None, and each named arm
// reads back as itself.
func t09PayloadFreeArms(t *testing.T) {
	const src = `package probe

type Circle { radius float32 }

union Body
{
    ping
    circle Circle
    pong
}

fixed table T { body Body }
`
	const probe = `
const { BodyType } = await import("./Probe.js");

const none = new T(); InitT(none);
if (!(none.Body.Type === BodyType.None)) { fail("payload-free-arms: a default union is not None"); }
const noneBuf = new Uint8Array(TMeasure(1));
if (TSave([none], 1, noneBuf) !== noneBuf.length) { fail("payload-free-arms: save none"); }
const noneBack = [new T()]; InitT(noneBack[0]);
const noneReport = new TableFixedReport();
const noneN = TLoad(noneBack, 1, noneBuf, noneBuf.length, TNewPlan(4096, 4096), noneReport);
if (!(noneN === 1 && !noneReport.malformed)) { fail("payload-free-arms: load none " + reason(noneReport)); }
if (!(noneBack[0].Body.Type === BodyType.None)) { fail("payload-free-arms: None did not round trip"); }

function roundtrip(arm, setup) {
  const v = new T(); InitT(v);
  v.Body.Type = arm;
  if (setup) { setup(v); }
  const buf = new Uint8Array(TMeasure(1));
  if (TSave([v], 1, buf) !== buf.length) { fail("payload-free-arms: save " + arm); }
  const back = [new T()]; InitT(back[0]);
  const report = new TableFixedReport();
  const n = TLoad(back, 1, buf, buf.length, TNewPlan(4096, 4096), report);
  if (!(n === 1 && !report.malformed)) { fail("payload-free-arms: load " + arm + " " + reason(report)); }
  return back[0];
}

const pingBack = roundtrip(BodyType.Ping, null);
if (!(pingBack.Body.Type === BodyType.Ping)) { fail("payload-free-arms: the arm before the payload arm did not round trip"); }

const circleBack = roundtrip(BodyType.Circle, (v) => { v.Body.Circle.Radius = -3.4028234663852886e38; });
if (!(circleBack.Body.Type === BodyType.Circle && circleBack.Body.Circle.Radius === -3.4028234663852886e38)) {
  fail("payload-free-arms: the payload arm did not round trip");
}

const pongBack = roundtrip(BodyType.Pong, null);
if (!(pongBack.Body.Type === BodyType.Pong)) { fail("payload-free-arms: the arm after the payload arm did not round trip"); }
if (BodyType.Ping === BodyType.Pong || BodyType.Ping === BodyType.None || BodyType.Pong === BodyType.None) {
  fail("payload-free-arms: a named payload-free arm collides with None or its sibling");
}
`
	t09Probe(t, src, "T", probe)
}

// t09UnionArrays: union-arrays [capability] "Arrays of unions" (§3.4: an array
// of a union type is the tag and the widest arm per element). A fixed-length
// array carries every element and a counted array carries up to its bound, so
// both an unselected arm and each selected arm round trip, with an element at a
// float32 boundary.
func t09UnionArrays(t *testing.T) {
	const src = `package probe

type Circle { radius float32 }
type Square { side float32 }

union Shape
{
    circle Circle
    square Square
}

fixed table T
{
    pending [2]Shape
    history [..3]Shape
}
`
	const probe = `
const { ShapeType } = await import("./Probe.js");
const v = new T(); InitT(v);
v.Pending[0].Type = ShapeType.Circle;
v.Pending[0].Circle.Radius = 1.25;
v.Pending[1].Type = ShapeType.Square;
v.Pending[1].Square.Side = 3.4028234663852886e38;
v.HistoryCount = 3;
v.History[0].Type = ShapeType.Square;
v.History[0].Square.Side = -3.4028234663852886e38;
v.History[1].Type = ShapeType.Circle;
v.History[1].Circle.Radius = 0.5;
v.History[2].Type = ShapeType.None;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("union-arrays: save"); }
const back = [new T()]; InitT(back[0]);
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, 1, buf, buf.length, plan, report);
if (!(n === 1 && !report.malformed)) { fail("union-arrays: load " + reason(report)); }
if (!(back[0].Pending[0].Type === ShapeType.Circle && back[0].Pending[0].Circle.Radius === 1.25)) {
  fail("union-arrays: the first fixed-array arm did not round trip");
}
if (!(back[0].Pending[1].Type === ShapeType.Square && back[0].Pending[1].Square.Side === 3.4028234663852886e38)) {
  fail("union-arrays: the second fixed-array arm did not round trip");
}
if (!(back[0].HistoryCount === 3)) { fail("union-arrays: the counted array's count did not round trip"); }
if (!(back[0].History[0].Type === ShapeType.Square && back[0].History[0].Square.Side === -3.4028234663852886e38)) {
  fail("union-arrays: the counted array's first arm did not round trip");
}
if (!(back[0].History[1].Type === ShapeType.Circle && back[0].History[1].Circle.Radius === 0.5)) {
  fail("union-arrays: the counted array's second arm did not round trip");
}
if (!(back[0].History[2].Type === ShapeType.None)) { fail("union-arrays: the counted array's last arm is not None"); }
`
	t09Probe(t, src, "T", probe)
}

// t09ScalarDefaults: scalar-defaults [capability] "Scalar and enum defaults"
// (§3.4: a `fixed table`'s scalar and enum fields carry their declared default
// at construction and on every reset). The value is read back at the type's
// BOUNDARY — int32 min and a float32 well past the mantissa — so a default
// carried by luck over small numbers does not pass.
func t09ScalarDefaults(t *testing.T) {
	const src = `package probe

enum Grade { First, Second }

fixed table T
{
    a int32 = 7
    r float32 = 1.5
    g Grade = Second
}
`
	const probe = `
const { Grade } = await import("./Probe.js");
const v = new T(); InitT(v);
if (!(v.A === 7)) { fail("scalar-defaults: the declared int32 default is not in the class"); }
if (!(v.R === 1.5)) { fail("scalar-defaults: the declared float32 default is not in the class"); }
if (!(v.G === Grade.Second)) { fail("scalar-defaults: the declared enum default is not in the class"); }
v.A = -2147483648;
v.R = 3.4028234663852886e38;
v.G = Grade.First;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("scalar-defaults: save"); }
const back = [new T()]; InitT(back[0]);
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, 1, buf, buf.length, plan, report);
if (!(n === 1 && !report.malformed)) { fail("scalar-defaults: load " + reason(report)); }
if (!(back[0].A === -2147483648)) { fail("scalar-defaults: int32 boundary did not round trip, got " + back[0].A); }
if (!(back[0].R === 3.4028234663852886e38)) { fail("scalar-defaults: float32 boundary did not round trip, got " + back[0].R); }
if (!(back[0].G === Grade.First)) { fail("scalar-defaults: enum value did not round trip, got " + back[0].G); }
`
	t09Probe(t, src, "T", probe)
}

// t09TextBytesFlagsDefaults: text-bytes-flags-defaults [capability] "String,
// byte-buffer and flags defaults" (§3.4: a string/bytes field's declared value
// rides behind its length and a flags field's declared set rides as its mask).
// The write is the field's full declared width and the flags set is the mask's
// top bit, so a default that only held over an empty set does not pass.
func t09TextBytesFlagsDefaults(t *testing.T) {
	const src = `package probe

flags Marks { One, Two, Three }

fixed table T
{
    s string(8) = "hi"
    b bytes(4) = "ab"
    m Marks = { One, Three }
}
`
	const probe = `
const { MarksOne, MarksTwo, MarksThree } = await import("./Probe.js");
const v = new T(); InitT(v);
if (!(v.SLength === 2 && v.S[0] === 104 && v.S[1] === 105)) { fail("text-defaults: the declared string default is not in the class"); }
if (!(v.BLength === 2 && v.B[0] === 97 && v.B[1] === 98)) { fail("bytes-defaults: the declared bytes default is not in the class"); }
if (!(v.M === 5n)) { fail("flags-defaults: the declared flags set { One, Three } is not in the class, got " + v.M); }
const text = "abcdefgh";
for (let i = 0; i < text.length; i++) { v.S[i] = text.charCodeAt(i); }
v.SLength = text.length;
const bytes = [0x00, 0xff, 0x7f, 0x80];
for (let i = 0; i < bytes.length; i++) { v.B[i] = bytes[i]; }
v.BLength = bytes.length;
v.M = MarksOne | MarksTwo | MarksThree;
const buf = new Uint8Array(TMeasure(1));
if (TSave([v], 1, buf) !== buf.length) { fail("text-bytes-flags-defaults: save"); }
const back = [new T()]; InitT(back[0]);
const report = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, 1, buf, buf.length, plan, report);
if (!(n === 1 && !report.malformed)) { fail("text-bytes-flags-defaults: load " + reason(report)); }
let got = "";
for (let i = 0; i < back[0].SLength; i++) { got += String.fromCharCode(back[0].S[i]); }
if (!(got === "abcdefgh")) { fail("text-defaults: the full declared width did not round trip, got " + JSON.stringify(got)); }
if (!(back[0].BLength === 4 && back[0].B[0] === 0x00 && back[0].B[1] === 0xff && back[0].B[2] === 0x7f && back[0].B[3] === 0x80)) {
  fail("bytes-defaults: every byte of the buffer did not round trip");
}
if (!(back[0].M === (MarksOne | MarksTwo | MarksThree))) { fail("flags-defaults: the full mask did not round trip, got " + back[0].M); }
`
	t09Probe(t, src, "T", probe)
}

// t09OptionalArrays: optional-arrays [capability] "Optional arrays" (§3.4: an
// optional array's present byte precedes a count and payload, so both ABSENT and
// PRESENT-at-boundary must round trip). One record carries no array at all and
// the other carries the declared maximum of it, with the element at int32's
// boundary.
func t09OptionalArrays(t *testing.T) {
	const src = `package probe

type Cell { x int32 = 9 }

fixed table T
{
    opt ?[..2]Cell
    tail uint32
}
`
	const probe = `
const absent = new T(); InitT(absent); absent.Tail = 1;
const absentBuf = new Uint8Array(TMeasure(1));
if (TSave([absent], 1, absentBuf) !== absentBuf.length) { fail("optional-arrays: save absent"); }
const absentBack = [new T()]; InitT(absentBack[0]);
let report = new TableFixedReport();
let plan = TNewPlan(4096, 4096);
let n = TLoad(absentBack, 1, absentBuf, absentBuf.length, plan, report);
if (!(n === 1 && !report.malformed)) { fail("optional-arrays: load absent " + reason(report)); }
if (!(absentBack[0].OptPresent === false && absentBack[0].OptCount === 0)) { fail("optional-arrays: an absent optional came back present"); }
if (!(absentBack[0].Tail === 1)) { fail("optional-arrays: the field behind the absent optional did not land"); }

const present = new T(); InitT(present);
present.OptPresent = true;
present.OptCount = 2;
present.Opt[0].X = -2147483648;
present.Opt[1].X = 2147483647;
present.Tail = 2;
const presentBuf = new Uint8Array(TMeasure(1));
if (TSave([present], 1, presentBuf) !== presentBuf.length) { fail("optional-arrays: save present"); }
const presentBack = [new T()]; InitT(presentBack[0]);
report = new TableFixedReport();
plan = TNewPlan(4096, 4096);
n = TLoad(presentBack, 1, presentBuf, presentBuf.length, plan, report);
if (!(n === 1 && !report.malformed)) { fail("optional-arrays: load present " + reason(report)); }
if (!(presentBack[0].OptPresent === true && presentBack[0].OptCount === 2)) { fail("optional-arrays: the boundary-length array came back wrong"); }
if (!(presentBack[0].Opt[0].X === -2147483648 && presentBack[0].Opt[1].X === 2147483647)) {
  fail("optional-arrays: the array elements did not round trip");
}
if (!(presentBack[0].Tail === 2)) { fail("optional-arrays: the field behind the present optional did not land"); }
`
	t09Probe(t, src, "T", probe)
}
