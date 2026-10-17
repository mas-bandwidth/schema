package gotable

import (
	"testing"
)

// TestGoTableStringArraysAndJSONArrayRoot holds docs/SPEC-TABLES.md §2.13 (text
// lists) and §16.9 (the array root) in the Go backend, through a generated unit
// compiled and run in a t.TempDir():
//
//   - a table holding [..4]string(8), [..4]?string(8) and []string(8)
//     round-trips through the binary wire, base64 (§16.8) and the JSON text;
//   - its wire is pinned byte for byte (element kind 12 for the plain lists,
//     element kind 13 over the generated element record for the ? list);
//   - its ToJson text, compacted, is exactly the plain shape
//     {"rules":["a","b"],"first":["x",null],"repos":[]};
//   - a `| json = "array"` table over []string(8) writes exactly ["a","b"],
//     reads it back equal, and refuses {"goals":["a"]} by name,
//     json_root_not_array;
//   - five elements against [..4] and a nine-byte element against string(8)
//     each read clamped and counted, and ReadStrict refuses each as clamped,
//     naming the field;
//   - ill-formed UTF-8 in an element is malformed;
//   - retain-unknown (§6.6) and the cooked form (§7) carry the lists as the
//     arrays of element records they are stored as;
//   - an array-root table held by value writes and reads as the array, an
//     object in its place is a kind mismatch, and []?string(M) round-trips
//     its nulls and a present empty element.
func TestGoTableStringArraysAndJSONArrayRoot(t *testing.T) {
	t.Parallel()
	runGenerated(t, stringArraysSchema, stringArraysProbe)
}

const stringArraysSchema = `package probe

table Lists
{
    rules [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
}

table GoalList | json = "array"
{
    goals []string(8)
}

table Holder
{
    g GoalList
    bases []?string(8)
}
`

const stringArraysProbe = `package probe

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unsafe"
)

// listsGolden is the pinned wire of rules ["a","b"], first ["x",null] and an
// empty repos: form byte 1; rules as kind 14 over element kind 12, two
// elements each its own L; first as kind 14 over element kind 13, a present
// element holding value under kind 12 and an absent one only the zero
// reference; repos elided; the zero reference; then the id table.
const listsGolden = "01010e060c0201610162020e0a0d0205030c017800010000c05351286a4f1267411d6f997fedd789ea0ce83094fde47c0300000000000000"

func compact(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}

func toJson(t *testing.T, v *Lists) []byte {
	t.Helper()
	n := ListsToJsonMeasure(v)
	if n < 0 {
		t.Fatal("ToJsonMeasure refused")
	}
	buf := make([]byte, n)
	if ListsToJson(v, buf) != n {
		t.Fatal("ToJson")
	}
	return buf
}

func save(t *testing.T, v *Lists, b *ListsBuilder) []byte {
	t.Helper()
	n := ListsMeasure(v, &b.Arena)
	if n < 0 {
		t.Fatal("Measure refused")
	}
	buf := make([]byte, n)
	if ListsSave(v, buf, &b.Arena) != n {
		t.Fatal("Save")
	}
	return buf
}

func saveIn(t *testing.T, v *Lists) []byte {
	t.Helper()
	n := ListsMeasure(v)
	if n < 0 {
		t.Fatal("Measure refused")
	}
	buf := make([]byte, n)
	if ListsSave(v, buf) != n {
		t.Fatal("Save")
	}
	return buf
}

func text(t *testing.T, v *Lists, field string, i int32, arena *TableArena) (string, bool) {
	t.Helper()
	var p []byte
	var ok bool
	switch field {
	case "rules":
		p, ok = ListsRulesTextAt(v, i)
	case "first":
		p, ok = ListsFirstTextAt(v, i)
	default:
		p, ok = ListsReposTextAt(v, i, arena)
	}
	return string(p), ok
}

func TestGoTableStringArraysAndJSONArrayRoot(t *testing.T) {
	var b ListsBuilder
	if !b.Init() {
		t.Fatal("init")
	}
	defer b.Shutdown()
	v := b.GetRoot()
	if !ListsRulesAppend(v, "a") || !ListsRulesAppend(v, "b") {
		t.Fatal("rules append")
	}
	if !ListsFirstAppend(v, "x") || !ListsFirstAppendAbsent(v) {
		t.Fatal("first append")
	}
	// THE WRITE-SIDE BOUNDS (§2.13): the element past M and the element past N
	// are refused at the append, and the stored value does not move
	if ListsRulesAppend(v, "123456789") {
		t.Fatal("a nine-byte element appended to string(8)")
	}
	if ListsRulesAppend(v, "a\x00") || ListsRulesAppend(v, "\xff") {
		t.Fatal("a zero byte or ill-formed UTF-8 appended")
	}
	if v.RulesCount != 2 || v.FirstCount != 2 || v.Repos.Count != 0 {
		t.Fatalf("counts %d %d %d", v.RulesCount, v.FirstCount, v.Repos.Count)
	}
	if s, ok := text(t, v, "first", 1, &b.Arena); ok || s != "" {
		t.Fatalf("absent element read %q present=%v", s, ok)
	}

	// THE PINNED WIRE
	wire := save(t, v, &b)
	if got := hex.EncodeToString(wire); got != listsGolden {
		t.Fatalf("wire\n got %s\nwant %s", got, listsGolden)
	}
	if !bytes.Contains(wire, []byte{14, 6, 12, 2, 1, 'a', 1, 'b'}) {
		t.Fatalf("rules is not kind 14 over element kind 12: %x", wire)
	}

	// THE PLAIN JSON SHAPE (§16.2)
	if got := compact(t, toJson(t, v)); got != ` + "`" + `{"rules":["a","b"],"first":["x",null],"repos":[]}` + "`" + ` {
		t.Fatalf("ToJson %s", got)
	}

	// ROUND TRIPS: wire, base64, JSON, with repos holding two elements
	if !ListsReposAppend(&b.Main, v, "r1") || !ListsReposAppend(&b.Main, v, "") {
		t.Fatal("repos append")
	}
	full := save(t, v, &b)
	var report TableReport
	region := make([]byte, 1<<16)
	got := ListsLoad(region, full, &report)
	if got == nil || report != (TableReport{}) {
		t.Fatalf("load %+v", report)
	}
	if again := saveIn(t, got); !bytes.Equal(again, full) {
		t.Fatalf("wire round trip\n%x\n%x", again, full)
	}
	for i, want := range []string{"r1", ""} {
		if s, ok := text(t, got, "repos", int32(i), nil); !ok || s != want {
			t.Fatalf("repos[%d] = %q %v", i, s, ok)
		}
	}
	if s, ok := text(t, got, "first", 0, nil); !ok || s != "x" {
		t.Fatalf("first[0] %q %v", s, ok)
	}
	if _, ok := text(t, got, "first", 1, nil); ok {
		t.Fatal("first[1] present after the round trip")
	}
	// RETAIN-UNKNOWN (§6.6) carries the list as the variable wire does: the
	// retaining load and save give back the same bytes
	var retain TableRetain
	report = TableReport{}
	kept := ListsLoadRetain(make([]byte, 1<<16), full, &retain, &report)
	if kept == nil || report.Malformed || report.Clamped != 0 {
		t.Fatalf("retain load %+v", report)
	}
	rbuf := make([]byte, len(full)+64)
	if n := ListsSaveRetain(kept, &retain, rbuf, &report); n < 0 || !bytes.Equal(rbuf[:n], full) {
		t.Fatalf("retain round trip %d\n%x\n%x", n, rbuf[:max(n, 0)], full)
	}
	// THE COOKED FORM (§7) carries the list as the array of element records
	// it is stored as: a cook of the loaded value opens and reads back
	cn := ListsCookMeasure(got)
	if cn < 0 {
		t.Fatal("cook measure refused")
	}
	cooked := make([]byte, cn+64)
	at := 0
	for uintptr(unsafe.Pointer(&cooked[at]))%64 != 0 {
		at++
	}
	cooked = cooked[at : at+int(cn)]
	if !ListsCookFrom(got, cooked, TableByteOrderLittle) {
		t.Fatal("cook")
	}
	var cook ListsCook
	if !ListsOpen(&cook, unsafe.Pointer(&cooked[0]), cn) {
		t.Fatal("cook open")
	}
	croot := (*Lists)(cook.Region)
	if s, ok := text(t, croot, "rules", 1, nil); !ok || s != "b" {
		t.Fatalf("cooked rules[1] %q", s)
	}
	if s, ok := text(t, croot, "repos", 0, nil); !ok || s != "r1" {
		t.Fatalf("cooked repos[0] %q", s)
	}
	if _, ok := text(t, croot, "first", 1, nil); ok {
		t.Fatal("cooked first[1] present")
	}
	b64, err := ListsSaveText(v, &b.Arena)
	if err != nil || b64 != base64.StdEncoding.EncodeToString(full) {
		t.Fatalf("SaveText %v", err)
	}
	if back, err := ListsLoadText(b64, nil); err != nil || !bytes.Equal(saveIn(t, back), full) {
		t.Fatalf("LoadText %v", err)
	}
	jtext := toJson(t, got)
	if got := compact(t, jtext); got != ` + "`" + `{"rules":["a","b"],"first":["x",null],"repos":["r1",""]}` + "`" + ` {
		t.Fatalf("ToJson %s", got)
	}
	var jb ListsBuilder
	jb.Init()
	defer jb.Shutdown()
	report = TableReport{}
	if !ListsFromJson(&jb, jtext, &report) || report != (TableReport{}) {
		t.Fatalf("FromJson %+v", report)
	}
	if !bytes.Equal(save(t, jb.GetRoot(), &jb), full) {
		t.Fatal("JSON round trip moved the wire")
	}
	// the JSON rows' refusals: a number element and a null in a string(M) list
	// are kind mismatches, an element past M and past N clamp
	var kb ListsBuilder
	kb.Init()
	defer kb.Shutdown()
	report = TableReport{}
	if !ListsFromJson(&kb, []byte(` + "`" + `{"rules":["a",7,null,"123456789","b","c"],"first":[null,"y"]}` + "`" + `), &report) {
		t.Fatalf("FromJson %+v", report)
	}
	if report.KindMismatch != 2 || report.Clamped != 3 {
		t.Fatalf("report %+v", report)
	}
	if s, ok := text(t, kb.GetRoot(), "rules", 3, nil); !ok || s != "12345678" {
		t.Fatalf("clamped element %q", s)
	}

	// THE ARRAY ROOT (§16.9)
	var gb GoalListBuilder
	gb.Init()
	defer gb.Shutdown()
	gv := gb.GetRoot()
	if !GoalListGoalsAppend(&gb.Main, gv, "a") || !GoalListGoalsAppend(&gb.Main, gv, "b") {
		t.Fatal("goals append")
	}
	if !gb.Lock() {
		t.Fatal("lock")
	}
	gv = gb.AsConst()
	n := GoalListToJsonMeasure(gv)
	gtext := make([]byte, n)
	GoalListToJson(gv, gtext)
	if compact(t, gtext) != ` + "`" + `["a","b"]` + "`" + ` {
		t.Fatalf("array root %s", gtext)
	}
	var rb GoalListBuilder
	rb.Init()
	defer rb.Shutdown()
	report = TableReport{}
	if !GoalListFromJson(&rb, []byte(` + "`" + `["a","b"]` + "`" + `), &report) || report != (TableReport{}) {
		t.Fatalf("array root read %+v", report)
	}
	if p, ok := GoalListGoalsTextAt(rb.GetRoot(), 1, &rb.Arena); !ok || string(p) != "b" || rb.GetRoot().Goals.Count != 2 {
		t.Fatal("array root read back")
	}
	var bad GoalListBuilder
	bad.Init()
	defer bad.Shutdown()
	report = TableReport{}
	if GoalListFromJson(&bad, []byte(` + "`" + `{"goals":["a"]}` + "`" + `), &report) || !report.Malformed || report.Reason != "json_root_not_array: GoalList" {
		t.Fatalf("object at an array root: %+v", report)
	}

	// THE ARRAY ROOT NESTED (§16.9): a by-value field's text is the array
	// too, and an object there is the ordinary kind mismatch; and the
	// unbounded ? list, []?string(M), round-trips its nulls
	var hd HolderBuilder
	hd.Init()
	defer hd.Shutdown()
	report = TableReport{}
	if !HolderFromJson(&hd, []byte(` + "`" + `{"g":["x","y"],"bases":["a",null,""]}` + "`" + `), &report) || report != (TableReport{}) {
		t.Fatalf("nested array root read %+v", report)
	}
	if !hd.Lock() {
		t.Fatal("holder lock")
	}
	hn := HolderToJsonMeasure(hd.AsConst())
	htext := make([]byte, hn)
	HolderToJson(hd.AsConst(), htext)
	if got := compact(t, htext); got != ` + "`" + `{"g":["x","y"],"bases":["a",null,""]}` + "`" + ` {
		t.Fatalf("nested array root write %s", got)
	}
	if p, ok := HolderBasesTextAt(hd.AsConst(), 2); !ok || len(p) != 0 {
		t.Fatal("a present empty element")
	}
	if _, ok := HolderBasesTextAt(hd.AsConst(), 1); ok {
		t.Fatal("an absent element read present")
	}
	hwire := make([]byte, HolderMeasure(hd.AsConst()))
	HolderSave(hd.AsConst(), hwire)
	report = TableReport{}
	if back := HolderLoad(make([]byte, 1<<12), hwire, &report); back == nil || report != (TableReport{}) || back.Bases.Count != 3 || back.G.Goals.Count != 2 {
		t.Fatalf("holder wire round trip %+v", report)
	}
	var ho HolderBuilder
	ho.Init()
	defer ho.Shutdown()
	report = TableReport{}
	if !HolderFromJson(&ho, []byte(` + "`" + `{"g":{"goals":["a"]}}` + "`" + `), &report) || report.KindMismatch != 1 {
		t.Fatalf("an object where the array root is: %+v", report)
	}

	// THE READ-SIDE BOUNDS (§2.13, §4): built by hand, five elements against
	// [..4] and a nine-byte element against string(8)
	rulesId := ListsTableFields[0].Id
	file := func(elements ...string) []byte {
		var ids TableIds
		out := make([]byte, 256)
		w := TableWriter{Buffer: out, Ids: &ids}
		w.Put8(1)
		w.Id(rulesId)
		w.Put8(14)
		n := 2
		for _, e := range elements {
			n += 1 + len(e)
		}
		w.PutLeb(uint64(n))
		w.Put8(12)
		w.PutLeb(uint64(len(elements)))
		for _, e := range elements {
			w.PutLeb(uint64(len(e)))
			w.Raw([]byte(e))
		}
		w.Put8(0)
		w.Trailer()
		return out[:w.Offset]
	}
	for _, c := range []struct {
		name     string
		elements []string
		count    int32
	}{
		{"count", []string{"a", "b", "c", "d", "e"}, 4},
		{"length", []string{"123456789"}, 1},
	} {
		report = TableReport{}
		got := ListsLoad(make([]byte, 1<<12), file(c.elements...), &report)
		if got == nil || report.Clamped != 1 || report.Malformed || got.RulesCount != c.count {
			t.Fatalf("%s: %+v", c.name, report)
		}
		if _, err := ListsReadStrict(file(c.elements...)); !errors.Is(err, TableRefuseClamped) || !strings.Contains(err.Error(), "Lists.rules") {
			t.Fatalf("%s: ReadStrict %v", c.name, err)
		}
	}
	if s, _ := text(t, ListsLoad(make([]byte, 1<<12), file("123456789"), nil), "rules", 0, nil); s != "12345678" {
		t.Fatalf("length clamp %q", s)
	}
	report = TableReport{}
	ListsLoad(make([]byte, 1<<12), file("ok", "\xff\xfe"), &report)
	if !report.Malformed {
		t.Fatalf("ill-formed UTF-8 element: %+v", report)
	}
	if _, err := ListsReadStrict(file("\xc3")); !errors.Is(err, TableRefuseMalformed) {
		t.Fatalf("ReadStrict ill-formed: %v", err)
	}
}
`
