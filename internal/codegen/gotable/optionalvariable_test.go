package gotable

import "testing"

// TestGoOptionalVariableClosureTable holds docs/SPEC-TABLES.md §2.3's rule for
// `?T` over a variable closure in the Go backend, through a generated unit
// compiled and run in a t.TempDir(): every walk that follows pointer edges
// visits T's edges only when the presence companion is set, and an absent
// value owns no node.
//
//   - absent, present with its text set, and present with its text empty
//     each round-trip the binary wire, base64 (§16.8) and the JSON text;
//   - an absent value leaves no node in the measured size: the absent wire
//     is pinned byte for byte, and its measure and region measure are the
//     same whether or not the absent value still holds a pointer;
//   - a present value then cleared and saved writes no stale node, through
//     save, measure, the retaining save, the cooked form, Lock's pack and
//     the JSON writer.
func TestGoOptionalVariableClosureTable(t *testing.T) {
	t.Parallel()
	runGenerated(t, optionalVariableSchema, optionalVariableProbe)
}

const optionalVariableSchema = `package probe

table Inner
{
    text *string
    n int32
}

table Outer
{
    inner ?Inner
    tail int32
}
`

const optionalVariableProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"unsafe"
)

// absentGolden is the pinned wire of Outer{tail: 5} with inner absent: form
// byte 1, tail under kind 4, the zero reference, then the id table. No node
// payload rides: an absent value owns no node.
const absentGolden = "010104050000000023d79807ef564cd90100000000000000"

func compactText(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}

func saveOuter(t *testing.T, v *Outer, arena ...*TableArena) []byte {
	t.Helper()
	var n int64
	if len(arena) > 0 {
		n = OuterMeasure(v, arena[0])
	} else {
		n = OuterMeasure(v)
	}
	if n < 0 {
		t.Fatal("Measure refused")
	}
	buf := make([]byte, n)
	var got int64
	if len(arena) > 0 {
		got = OuterSave(v, buf, arena[0])
	} else {
		got = OuterSave(v, buf)
	}
	if got != n {
		t.Fatalf("Save %d want %d", got, n)
	}
	return buf
}

func outerJson(t *testing.T, v *Outer) []byte {
	t.Helper()
	n := OuterToJsonMeasure(v)
	if n < 0 {
		t.Fatal("ToJsonMeasure refused")
	}
	buf := make([]byte, n)
	if OuterToJson(v, buf) != n {
		t.Fatal("ToJson")
	}
	return buf
}

func aligned(n int64) []byte {
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

func innerText(v *Outer) (string, bool) {
	if v.Inner.Text == 0 {
		return "", false
	}
	s := TableStringAt(&v.Inner.Text)
	return string(s.Data), true
}

// roundTrip saves v, then proves the binary wire, base64 and the JSON text
// each give the same wire back, and that the loaded value's presence and text
// are what the case set.
func roundTrip(t *testing.T, name string, v *Outer, arena *TableArena, present bool, text string, hasText bool) []byte {
	t.Helper()
	wire := saveOuter(t, v, arena)
	size := OuterLoadMeasure(wire)
	if size < 0 {
		t.Fatalf("%s: LoadMeasure refused", name)
	}
	var report TableReport
	got := OuterLoad(aligned(size), wire, &report)
	if got == nil || report != (TableReport{}) {
		t.Fatalf("%s: load %+v", name, report)
	}
	if got.InnerPresent != present || got.Tail != 5 {
		t.Fatalf("%s: loaded present=%v tail=%d", name, got.InnerPresent, got.Tail)
	}
	if s, ok := innerText(got); ok != hasText || s != text {
		t.Fatalf("%s: loaded text %q %v", name, s, ok)
	}
	if !present && got.Inner.Text != 0 {
		t.Fatalf("%s: an absent value loaded an edge", name)
	}
	if again := saveOuter(t, got); !bytes.Equal(again, wire) {
		t.Fatalf("%s: wire round trip\n%x\n%x", name, again, wire)
	}
	// BASE64 (§16.8)
	b64, err := OuterSaveText(v, arena)
	if err != nil || b64 != base64.StdEncoding.EncodeToString(wire) {
		t.Fatalf("%s: SaveText %v", name, err)
	}
	back, err := OuterLoadText(b64, nil)
	if err != nil || back.InnerPresent != present || !bytes.Equal(saveOuter(t, back), wire) {
		t.Fatalf("%s: LoadText %v", name, err)
	}
	// THE JSON TEXT (§16)
	jtext := outerJson(t, got)
	var jb OuterBuilder
	if !jb.Init() {
		t.Fatal("init")
	}
	defer jb.Shutdown()
	report = TableReport{}
	if !OuterFromJson(&jb, jtext, &report) || report != (TableReport{}) {
		t.Fatalf("%s: FromJson %+v: %s", name, report, jtext)
	}
	if jb.GetRoot().InnerPresent != present {
		t.Fatalf("%s: JSON presence %v: %s", name, jb.GetRoot().InnerPresent, jtext)
	}
	if !bytes.Equal(saveOuter(t, jb.GetRoot(), &jb.Arena), wire) {
		t.Fatalf("%s: JSON round trip moved the wire: %s", name, jtext)
	}
	// RETAIN-UNKNOWN (§6.6) gives back the same bytes
	var retain TableRetain
	report = TableReport{}
	kept := OuterLoadRetain(aligned(1<<12), wire, &retain, &report)
	if kept == nil || report.Malformed {
		t.Fatalf("%s: retain load %+v", name, report)
	}
	rbuf := make([]byte, len(wire)+64)
	if n := OuterSaveRetain(kept, &retain, rbuf, &report); n < 0 || !bytes.Equal(rbuf[:n], wire) {
		t.Fatalf("%s: retain round trip %d", name, n)
	}
	// THE COOKED FORM (§7) opens and reads back the presence and the text
	cooked := cookOuter(t, got)
	var cook OuterCook
	if !OuterOpen(&cook, unsafe.Pointer(&cooked[0]), int64(len(cooked))) {
		t.Fatalf("%s: cook open", name)
	}
	croot := (*Outer)(cook.Region)
	if croot.InnerPresent != present {
		t.Fatalf("%s: cooked presence", name)
	}
	if s, ok := innerText(croot); ok != hasText || s != text {
		t.Fatalf("%s: cooked text %q %v", name, s, ok)
	}
	return wire
}

func cookOuter(t *testing.T, v *Outer) []byte {
	t.Helper()
	cn := OuterCookMeasure(v)
	if cn < 0 {
		t.Fatal("cook measure refused")
	}
	cooked := aligned(cn)
	if !OuterCookFrom(v, cooked, TableByteOrderLittle) {
		t.Fatal("cook")
	}
	return cooked
}

func TestOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	// ABSENT, FRESH: the baseline every absent measure is pinned against
	var fresh OuterBuilder
	if !fresh.Init() {
		t.Fatal("init")
	}
	defer fresh.Shutdown()
	fv := fresh.GetRoot()
	fv.Tail = 5
	absent := roundTrip(t, "absent", fv, &fresh.Arena, false, "", false)
	if got := hex.EncodeToString(absent); got != absentGolden {
		t.Fatalf("absent wire\n got %s\nwant %s", got, absentGolden)
	}
	if got := compactText(t, outerJson(t, fv)); got != ` + "`" + `{"tail":5}` + "`" + ` {
		t.Fatalf("absent ToJson %s", got)
	}
	// the JSON reader leaves T absent, at its defaults, on null (§16.2)
	var nb OuterBuilder
	if !nb.Init() {
		t.Fatal("init")
	}
	defer nb.Shutdown()
	nreport := TableReport{}
	if !OuterFromJson(&nb, []byte(` + "`" + `{"inner":null,"tail":5}` + "`" + `), &nreport) || nreport != (TableReport{}) {
		t.Fatalf("null FromJson %+v", nreport)
	}
	if nb.GetRoot().InnerPresent || nb.GetRoot().Inner.Text != 0 || !bytes.Equal(saveOuter(t, nb.GetRoot(), &nb.Arena), absent) {
		t.Fatal("inner null did not read absent")
	}
	absentRegion := OuterLoadMeasure(absent)
	absentCook := OuterCookMeasure(fv, &fresh.Arena)
	absentPack := fresh.PackMeasure()

	// PRESENT, TEXT SET
	var b OuterBuilder
	if !b.Init() {
		t.Fatal("init")
	}
	defer b.Shutdown()
	v := b.GetRoot()
	v.Tail = 5
	v.InnerPresent = true
	TableStringEmplace(&b.Main, &v.Inner.Text, []byte("hi"))
	present := roundTrip(t, "present", v, &b.Arena, true, "hi", true)
	if !bytes.Contains(present, []byte("hi")) || len(present) <= len(absent) {
		t.Fatalf("present wire carries no node: %x", present)
	}

	// PRESENT, TEXT EMPTY: the pointer is set and names an empty string
	var e OuterBuilder
	if !e.Init() {
		t.Fatal("init")
	}
	defer e.Shutdown()
	report := TableReport{}
	if !OuterFromJson(&e, []byte(` + "`" + `{"inner":{"text":""},"tail":5}` + "`" + `), &report) || report != (TableReport{}) {
		t.Fatalf("empty text FromJson %+v", report)
	}
	ev := e.GetRoot()
	if !ev.InnerPresent || ev.Inner.Text == 0 {
		t.Fatalf("empty text read present=%v text=%d", ev.InnerPresent, ev.Inner.Text)
	}
	roundTrip(t, "present empty", ev, &e.Arena, true, "", true)

	// PRESENT, ALL DEFAULT: rides even so (§2.3), and owns no node
	var d OuterBuilder
	if !d.Init() {
		t.Fatal("init")
	}
	defer d.Shutdown()
	dv := d.GetRoot()
	dv.Tail = 5
	dv.InnerPresent = true
	roundTrip(t, "present default", dv, &d.Arena, true, "", false)

	// A PRESENT VALUE THEN CLEARED writes no stale node: the presence bool is
	// cleared and Inner.Text still names the "hi" node
	v.InnerPresent = false
	if v.Inner.Text == 0 {
		t.Fatal("the stale edge is gone; the case proves nothing")
	}
	if n := OuterMeasure(v, &b.Arena); n != int64(len(absent)) {
		t.Fatalf("cleared measure %d want %d (the absent size, pinned)", n, len(absent))
	}
	cleared := roundTrip(t, "cleared", v, &b.Arena, false, "", false)
	if !bytes.Equal(cleared, absent) || bytes.Contains(cleared, []byte("hi")) {
		t.Fatalf("cleared wire writes a stale node\n%x\n%x", cleared, absent)
	}
	if OuterLoadMeasure(cleared) != absentRegion {
		t.Fatalf("cleared region %d want %d", OuterLoadMeasure(cleared), absentRegion)
	}
	if got := compactText(t, outerJson(t, v)); got != ` + "`" + `{"tail":5}` + "`" + ` {
		t.Fatalf("cleared ToJson %s", got)
	}
	if n := OuterCookMeasure(v, &b.Arena); n != absentCook {
		t.Fatalf("cleared cook measure %d want %d", n, absentCook)
	}
	// the cooked writer itself, on the cleared builder value: it opens absent,
	// and the reference the cleared T still holds is written null (§7)
	ccooked := aligned(absentCook)
	if !OuterCookFrom(v, ccooked, TableByteOrderLittle, &b.Arena) {
		t.Fatal("cleared cook")
	}
	var ccook OuterCook
	if !OuterOpen(&ccook, unsafe.Pointer(&ccooked[0]), int64(len(ccooked))) {
		t.Fatal("cleared cook open")
	}
	if croot := (*Outer)(ccook.Region); croot.InnerPresent || croot.Inner.Text != 0 || croot.Tail != 5 {
		t.Fatalf("cleared cooked present=%v text=%d tail=%d", croot.InnerPresent, croot.Inner.Text, croot.Tail)
	}
	if n := b.PackMeasure(); n != absentPack {
		t.Fatalf("cleared pack measure %d want %d", n, absentPack)
	}
	if !b.Lock() || b.AsConst().InnerPresent || int64(len(b.Region())) != absentPack {
		t.Fatalf("cleared Lock packed %d want %d", len(b.Region()), absentPack)
	}
	// Lock's pack writes the absent T at its defaults, every pointer null,
	// never the stale arena reference the cleared storage still held
	if b.AsConst().Inner.Text != 0 {
		t.Fatalf("the locked cleared value holds a stale reference %d", b.AsConst().Inner.Text)
	}
	if !bytes.Equal(saveOuter(t, b.AsConst()), absent) {
		t.Fatal("the locked cleared value writes a stale node")
	}
}
`
