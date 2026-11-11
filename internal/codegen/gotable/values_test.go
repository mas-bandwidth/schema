package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func runGenerated(t *testing.T, schema, testSource string) {
	t.Helper()
	if out, err := runGeneratedResult(t, schema, testSource); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}

func runGeneratedResult(t *testing.T, schema, testSource string, flags ...string) ([]byte, error) {
	t.Helper()
	return runGeneratedEdited(t, schema, testSource, nil, flags...)
}

func runGeneratedEdited(t *testing.T, schema, testSource string, edit func(map[string][]byte), flags ...string) ([]byte, error) {
	t.Helper()
	return runGeneratedUnit(t, unitFrom(t, schema), testSource, edit, flags...)
}

func runGeneratedUnit(t *testing.T, u *ir.Unit, testSource string, edit func(map[string][]byte), flags ...string) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := append([]string{"test", "-count=1"}, flags...)
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func TestDefaultsAndOptionalArrays(t *testing.T) {
	runGenerated(t, `package probe
flags Caps { Jump, Fly }
enum Grade {
 Gold | was = "OldGold"
}
type Badge {
 title string(8) = "fresh" | was = "label"
}
fixed table Root {
 title string(12) = "default"
 tag bytes(4) = "ab"
 caps Caps = { Jump, Fly }
 badge Badge
 values ?[..3]int32
 grades ?[2]Grade
}
`, `package probe
import("testing";"encoding/binary";"hash/fnv";"bytes")

func rootForm1(value *Root, data []byte, report *TableReport) bool {
	// Load refuses a form-1 file at a fixed-class root. The body still answers the probe.
	var gate TableReport
	if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
		if report != nil {
			*report = gate
		}
		return false
	}
	out := report
	if out == nil {
		out = &TableReport{}
	} else {
		*out = TableReport{}
	}
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		tableRefused(out)
		RootReset(value)
		if report != nil {
			*report = *out
		}
		return false
	}
	return true
}
// rootForm1Measure is the variable form's file size (form byte, body, id
// trailer): RootMeasure answers the fixed form a fixed root's Save writes.
func rootForm1Measure(value *Root) int64 {
	var ids TableIds
	body := RootMeasureBody(value, &ids)
	if body < 0 || ids.Overflow {
		return -1
	}
	return 1 + body + int64(ids.Count)*8 + 8
}
func rootForm1Save(value *Root) []byte {
	n := rootForm1Measure(value)
	buf := make([]byte, n)
	var ids TableIds
	w := TableWriter{Buffer: buf, Ids: &ids}
	w.Put8(1)
	if !RootSaveBody(&w, value) {
		return nil
	}
	w.Trailer()
	if w.Overflow || ids.Overflow {
		return nil
	}
	return buf[:w.Offset]
}
func TestDefaults(t *testing.T) {
 var v Root; RootReset(&v)
 if string(v.Title[:v.TitleLength]) != "default" || string(v.Tag[:v.TagLength]) != "ab" || v.Caps != 3 || string(v.Badge.Title[:v.Badge.TitleLength]) != "fresh" { t.Fatalf("defaults: %+v",v) }
 if n:=rootForm1Measure(&v);n!=10 {t.Fatalf("default file: %d",n)}
 v.TitleLength=0;v.TagLength=0;v.ValuesPresent=true;v.GradesPresent=true
 v.Grades[1]=GradeGold
 b:=rootForm1Save(&v);if b==nil{t.Fatal("save")}
 var got Root;var r TableReport
 if !rootForm1(&got,b,&r)||r!=(TableReport{})||!got.ValuesPresent||got.ValuesCount!=0||!got.GradesPresent||got.Grades[1]!=GradeGold||got.TitleLength!=0||got.TagLength!=0{t.Fatalf("load: %+v %+v",got,r)}
 h:=fnv.New64a();h.Write([]byte("OldGold"));needle:=binary.LittleEndian.AppendUint64(nil,h.Sum64());if !bytes.Contains(b,needle){t.Fatal("renamed variant lost its id")}
 text:=[]byte("{\"values\":[],\"grades\":[\"Gold\",\"None\"]}")
 r=TableReport{};if !RootFromJSON(&got,text,&r)||r!=(TableReport{})||!got.ValuesPresent||!got.GradesPresent {t.Fatalf("optional text: %+v %+v",got,r)}
 if n:=testing.AllocsPerRun(20,func(){rootForm1Measure(&v);var ids TableIds;w:=TableWriter{Buffer:b,Ids:&ids};w.Put8(1);if !RootSaveBody(&w,&v){return};w.Trailer();if w.Overflow||ids.Overflow{return};rootForm1(&got,b,&r)});n!=0{t.Fatalf("allocations: %v",n)}
}
`)
}

func TestUnionArmPayloads(t *testing.T) {
	runGenerated(t, `package probe
enum Mode { Fast, Slow }
type Point { x int32 }
fixed table Child { score int32 = 7 }
union Inner {
 n int32
 text string(8)
 ping
}
union Value {
 tally int32 | min = 0, max = 100
 label string(8)
 blob bytes(8)
 samples [..3]float32
 pair [2]int16
 mode Mode
 point Point
 child Child
 origin Inner
 nested [..2]Inner
 ping | was = "old_ping"
}
fixed table Root {
 value Value
 entries [..3]Value
 other Inner
 pinned [1]Inner
}
`, `package probe
import("testing";"bytes";"encoding/binary";"hash/fnv")

func rootForm1(value *Root, data []byte, report *TableReport) bool {
	// Load refuses a form-1 file at a fixed-class root. The body still answers the probe.
	var gate TableReport
	if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
		if report != nil {
			*report = gate
		}
		return false
	}
	out := report
	if out == nil {
		out = &TableReport{}
	} else {
		*out = TableReport{}
	}
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		tableRefused(out)
		RootReset(value)
		if report != nil {
			*report = *out
		}
		return false
	}
	return true
}
func TestArms(t *testing.T) {
 texts:=[]string{
 "{\"value\":{\"tally\":32}}",
 "{\"value\":{\"label\":\"hi\"}}",
 "{\"value\":{\"blob\":\"YWJj\"}}",
 "{\"value\":{\"samples\":[1.25,2.5]}}",
 "{\"value\":{\"pair\":[7,9]}}",
 "{\"value\":{\"mode\":\"Slow\"}}",
 "{\"value\":{\"point\":{\"x\":7}}}",
 "{\"value\":{\"child\":{\"score\":9}}}",
 "{\"value\":{\"origin\":{\"text\":\"hello\"}}}",
 "{\"value\":{\"nested\":[{\"n\":42},{\"ping\":null}]}}",
 "{\"value\":{\"ping\":null}}",
 "{\"entries\":[{\"tally\":1},{\"label\":\"two\"},{\"ping\":null}],\"other\":{\"n\":7}}",
 }
 for _,text:=range texts {
  var v,got Root;var r TableReport
  if !RootFromJSON(&v,[]byte(text),&r)||r!=(TableReport{}) {t.Fatalf("json %s: %+v",text,r)}
  n:=RootFixedMeasure(1);if n<0 {t.Fatalf("measure %s",text)}
  b:=make([]byte,n);if RootSave(&v,b)!=n {t.Fatalf("save %s",text)}
  if !RootLoad(&got,b,&r)||r!=(TableReport{}) {t.Fatalf("load %s: %+v",text,r)}
  a,c:=make([]byte,4096),make([]byte,4096)
  na,nc:=RootToJSON(&v,a),RootToJSON(&got,c)
  if na<0||nc!=na||!bytes.Equal(a[:na],c[:nc]) {t.Fatalf("roundtrip %s:\n%s\n%s",text,a[:max(na,0)],c[:max(nc,0)])}
  back:=make([]Root,1);plan:=make([]TableFixedEntry,RootFixedPlanMax)
  if RootFixedLoad(back,b,plan,&r)!=1 {t.Fatalf("warmup %s: %+v",text,r)}
  if n:=testing.AllocsPerRun(10,func(){RootFixedMeasure(1);RootSave(&v,b);RootFixedLoad(back,b,plan,&r)});n!=0{t.Fatalf("allocations: %v",n)}
 }
}
// The element resets after its arm reference, before its kind and L. A
// repeated array field must not retain an earlier selected arm when L is bad.
func TestRepeatedUnionElement(t *testing.T) {
 wire:=[]byte{1,1,14,9,15,1,2,4,4,7,0,0,0,1,14,5,15,1,2,4,127,0}
 for _,name:=range []string{"pinned","n"} {h:=fnv.New64a();h.Write([]byte(name));wire=binary.LittleEndian.AppendUint64(wire,h.Sum64())}
 wire=binary.LittleEndian.AppendUint64(wire,2)
 var v Root;var r TableReport
 if rootForm1(&v,wire,&r)||!(r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed")||v.Pinned[0].Type!=InnerTypeNone {t.Fatalf("stale element: damage must discard the whole load: %+v %+v",v.Pinned,r)}
}
`)
}

// TestUnionArmArrayOfUnionsCompiles pins the smallest form of the red: a union
// arm whose payload is an array of a union. The read-side bounds pass
// (docs/SPEC-TABLES.md §3.4) must walk the arm as the field line it is — a
// counted array to its LIVE count, a fixed array to its bound, each element
// clamped — rather than treat the arm's storage as one element. Red first:
// the emitter wrote value.Value.Nested.Type for a [2]Inner arm.
func TestUnionArmArrayOfUnionsCompiles(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
union Inner { n int32 }
union Value {
	nested [..2]Inner
	pinned [2]Inner
}
fixed table Root {
	value Value
	other Inner
}
`, t11Prelude+`
func TestUnionArmArrayOfUnions(t *testing.T) {
	var v Root
	RootReset(&v)
	v.Value.Type = ValueTypeNested
	v.Value.NestedCount = 2
	v.Value.Nested[0].Type = InnerTypeN
	v.Value.Nested[0].N = 11
	v.Value.Nested[1].Type = InnerTypeN
	v.Value.Nested[1].N = -22
	got := roundtrip(t, v)
	if got.Value.Type != ValueTypeNested || got.Value.NestedCount != 2 ||
		got.Value.Nested[0].N != 11 || got.Value.Nested[1].N != -22 {
		t.Fatalf("counted arm: %+v", got.Value)
	}
	v = Root{}
	RootReset(&v)
	v.Value.Type = ValueTypePinned
	v.Value.Pinned[0].Type = InnerTypeN
	v.Value.Pinned[0].N = 5
	v.Value.Pinned[1].Type = InnerTypeN
	v.Value.Pinned[1].N = 6
	got = roundtrip(t, v)
	if got.Value.Type != ValueTypePinned ||
		got.Value.Pinned[0].N != 5 || got.Value.Pinned[1].N != 6 {
		t.Fatalf("fixed arm: %+v", got.Value)
	}
}
`)
}

func TestWideValues(t *testing.T) {
	runGenerated(t, `package probe
 fixed table Root {
  n int128 = -3 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
  u uint128
  q fixed(4,4) | min = -8, max = 7
  w fixed(1,127) | min = -1, max = 0
  many [..2]uint128
 }
 `, `package probe
 import("testing";"bytes")

func rootForm1(value *Root, data []byte, report *TableReport) bool {
	// Load refuses a form-1 file at a fixed-class root. The body still answers the probe.
	var gate TableReport
	if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
		if report != nil {
			*report = gate
		}
		return false
	}
	out := report
	if out == nil {
		out = &TableReport{}
	} else {
		*out = TableReport{}
	}
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		tableRefused(out)
		RootReset(value)
		if report != nil {
			*report = *out
		}
		return false
	}
	return true
}
 func TestWide(t *testing.T) {
  var v,got Root;var r TableReport
  text:=[]byte("{\"n\":-170141183460469231731687303715884105728,\"u\":340282366920938463463374607431768211455,\"q\":-0.25,\"w\":-0.5,\"many\":[18446744073709551616]}")
  if !RootFromJSON(&v,text,&r)||r!=(TableReport{}) {t.Fatalf("json %+v",r)}
  n:=RootFixedMeasure(1);b:=make([]byte,n);if RootSave(&v,b)!=n||!RootLoad(&got,b,&r)||got!=v||r!=(TableReport{}) {t.Fatalf("roundtrip %+v %+v %+v",v,got,r)}
  a,c:=make([]byte,2048),make([]byte,2048);na,nc:=RootToJSON(&v,a),RootToJSON(&got,c);if na<0||nc!=na||!bytes.Equal(a[:na],c[:nc]) {t.Fatal("text mismatch")}
  for _,want:=range [][]byte{[]byte("-170141183460469231731687303715884105728"),[]byte("340282366920938463463374607431768211455"),[]byte("-0.25"),[]byte("-0.5"),[]byte("18446744073709551616")} {if !bytes.Contains(a[:na],want){t.Fatalf("lost precision: %s",a[:na])}}
  back:=make([]Root,1);plan:=make([]TableFixedEntry,RootFixedPlanMax)
  if RootFixedLoad(back,b,plan,&r)!=1 {t.Fatalf("warmup %+v",r)}
  if n:=testing.AllocsPerRun(10,func(){RootFixedMeasure(1);RootSave(&v,b);RootFixedLoad(back,b,plan,&r);RootToJSON(&v,a);RootFromJSON(&got,text,&r)});n!=0 {t.Fatalf("allocations %v",n)}
 }
 `)
}
