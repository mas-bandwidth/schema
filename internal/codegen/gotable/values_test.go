package gotable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
)

func runGenerated(t *testing.T, schema, testSource string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
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
table Root {
 title string(12) = "default"
 tag bytes(4) = "ab"
 caps Caps = { Jump, Fly }
 badge Badge
 values ?[..3]int32
 grades ?[2]Grade
}
`, `package probe
import("testing";"encoding/binary";"hash/fnv";"bytes")
func TestDefaults(t *testing.T) {
 var v Root; RootReset(&v)
 if string(v.Title[:v.TitleLength]) != "default" || string(v.Tag[:v.TagLength]) != "ab" || v.Caps != 3 || string(v.Badge.Title[:v.Badge.TitleLength]) != "fresh" { t.Fatalf("defaults: %+v",v) }
 if n:=RootMeasure(&v);n!=10 {t.Fatalf("default file: %d",n)}
 v.TitleLength=0;v.TagLength=0;v.ValuesPresent=true;v.GradesPresent=true
 v.Grades[1]=GradeGold
 b:=make([]byte,RootMeasure(&v));if RootSave(&v,b)!=int64(len(b)){t.Fatal("save")}
 var got Root;var r TableReport
 if !RootLoad(&got,b,&r)||r!=(TableReport{})||!got.ValuesPresent||got.ValuesCount!=0||!got.GradesPresent||got.Grades[1]!=GradeGold||got.TitleLength!=0||got.TagLength!=0{t.Fatalf("load: %+v %+v",got,r)}
 h:=fnv.New64a();h.Write([]byte("OldGold"));needle:=binary.LittleEndian.AppendUint64(nil,h.Sum64());if !bytes.Contains(b,needle){t.Fatal("renamed variant lost its id")}
 text:=[]byte("{\"values\":[],\"grades\":[\"Gold\",\"None\"]}")
 r=TableReport{};if !RootFromJson(&got,text,&r)||r!=(TableReport{})||!got.ValuesPresent||!got.GradesPresent {t.Fatalf("optional text: %+v %+v",got,r)}
 if n:=testing.AllocsPerRun(20,func(){RootMeasure(&v);RootSave(&v,b);RootLoad(&got,b,&r)});n!=0{t.Fatalf("allocations: %v",n)}
}
`)
}

func TestUnionArmPayloads(t *testing.T) {
	runGenerated(t, `package probe
enum Mode { Fast, Slow }
type Point { x int32 }
table Child { score int32 = 7 }
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
table Root {
 value Value
 entries [..3]Value
 other Inner
 pinned [1]Inner
}
`, `package probe
import("testing";"bytes";"encoding/binary";"hash/fnv")
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
  if !RootFromJson(&v,[]byte(text),&r)||r!=(TableReport{}) {t.Fatalf("json %s: %+v",text,r)}
  n:=RootMeasure(&v);if n<0 {t.Fatalf("measure %s",text)}
  b:=make([]byte,n);if RootSave(&v,b)!=n {t.Fatalf("save %s",text)}
  if !RootLoad(&got,b,&r)||r!=(TableReport{}) {t.Fatalf("load %s: %+v",text,r)}
  a,c:=make([]byte,4096),make([]byte,4096)
  na,nc:=RootToJson(&v,a),RootToJson(&got,c)
  if na<0||nc!=na||!bytes.Equal(a[:na],c[:nc]) {t.Fatalf("roundtrip %s:\n%s\n%s",text,a[:max(na,0)],c[:max(nc,0)])}
  if n:=testing.AllocsPerRun(10,func(){RootMeasure(&v);RootSave(&v,b);RootLoad(&got,b,&r)});n!=0{t.Fatalf("allocations: %v",n)}
 }
}
// The element resets after its arm reference, before its kind and L. A
// repeated array field must not retain an earlier selected arm when L is bad.
func TestRepeatedUnionElement(t *testing.T) {
 wire:=[]byte{1,1,14,9,15,1,2,4,4,7,0,0,0,1,14,5,15,1,2,4,127,0}
 for _,name:=range []string{"pinned","n"} {h:=fnv.New64a();h.Write([]byte(name));wire=binary.LittleEndian.AppendUint64(wire,h.Sum64())}
 wire=binary.LittleEndian.AppendUint64(wire,2)
 var v Root;var r TableReport
 if !RootLoad(&v,wire,&r)||!r.Malformed||v.Pinned[0].Type!=InnerTypeNone {t.Fatalf("stale element: %+v %+v",v.Pinned,r)}
}
`)
}

func TestWideValues(t *testing.T) {
	runGenerated(t, `package probe
 table Root {
  n int128 = -3 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
  u uint128
  q fixed(4,4) | min = -8, max = 7
  w fixed(1,127) | min = -1, max = 0
  many [..2]uint128
 }
 `, `package probe
 import("testing";"bytes")
 func TestWide(t *testing.T) {
  var v,got Root;var r TableReport
  text:=[]byte("{\"n\":-170141183460469231731687303715884105728,\"u\":340282366920938463463374607431768211455,\"q\":-0.25,\"w\":-0.5,\"many\":[18446744073709551616]}")
  if !RootFromJson(&v,text,&r)||r!=(TableReport{}) {t.Fatalf("json %+v",r)}
  n:=RootMeasure(&v);b:=make([]byte,n);if RootSave(&v,b)!=n||!RootLoad(&got,b,&r)||got!=v||r!=(TableReport{}) {t.Fatalf("roundtrip %+v %+v %+v",v,got,r)}
  a,c:=make([]byte,2048),make([]byte,2048);na,nc:=RootToJson(&v,a),RootToJson(&got,c);if na<0||nc!=na||!bytes.Equal(a[:na],c[:nc]) {t.Fatal("text mismatch")}
  for _,want:=range [][]byte{[]byte("-170141183460469231731687303715884105728"),[]byte("340282366920938463463374607431768211455"),[]byte("-0.25"),[]byte("-0.5"),[]byte("18446744073709551616")} {if !bytes.Contains(a[:na],want){t.Fatalf("lost precision: %s",a[:na])}}
  if n:=testing.AllocsPerRun(10,func(){RootMeasure(&v);RootSave(&v,b);RootLoad(&got,b,&r);RootToJson(&v,a);RootFromJson(&got,text,&r)});n!=0 {t.Fatalf("allocations %v",n)}
 }
 `)
}
