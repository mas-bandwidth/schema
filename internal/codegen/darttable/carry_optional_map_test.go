package darttable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartOptionalMapNative(t *testing.T) {
	t.Skip("layer 4 (dart): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Child { n int32 }
table Inner { tags ?map[string(8)]int32 }
table Root {
 tags ?map[string(8)]int32
 numbers ?map[int32]int32
 refs ?map[string(8)]*Child
 nested Inner
 deep ?map[uint8]map[uint8]int32
 values ?map[string(8)]?int32
 plain map[string(8)]int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	m := tabletext.NewModel(u)
	cases := []string{
		`{}`,
		`{"tags":null,"numbers":null,"refs":null,"nested":{"tags":null}}`,
		`{"tags":{},"numbers":{},"refs":{},"nested":{"tags":{}},"plain":{}}`,
		`{"tags":{"a":0,"z":5},"numbers":{"-2":8,"7":0},"refs":{"a":{"n":19},"nil":null},"nested":{"tags":{"n":31}},"deep":{"2":{},"3":{"4":12}},"plain":{"p":2}}`,
		`{"tags":[],"numbers":false,"refs":42,"nested":{"tags":9}}`,
		`{"tags":{"a":1},"tags":{},"nested":{"tags":{"q":4},"tags":null}}`,
	}
	var b strings.Builder
	b.WriteString("import 'dart:typed_data';\nimport 'ProbeWire.dart' as w;\nimport 'ProbeCook.dart' as c;\nvoid check(bool v,String s){if(!v)throw StateError(s);}\nString hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid main(){\n")
	for i, input := range cases {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) {
			t.Fatalf("neutral case%d: %+v", i, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final r=w.TableTextReport();final v=w.rootFromJson(%q,r);
check(!r.refused,'JSON report%d');
check(w.rootToJson(v)==%q,'JSON%d');
final wire=w.rootWireSave(v);check(hex(wire)==%q,'wire%d');
final wr=w.TableWireReport();final back=w.rootWireLoad(wire,wr);
check(!wr.refused&&w.rootToJson(back)==w.rootToJson(v),'read%d');
`, input, i, string(canonical)+"\n", i, hex.EncodeToString(wire), i, i)
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := "Endian.little"
			if big {
				order = "Endian.big"
			}
			fmt.Fprintf(&b, "{final out=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,out,%s),'cook%d');check(hex(out)==%q,'cook bytes%d');}\n", order, i, hex.EncodeToString(cooked), i)
		}
		b.WriteString("}\n")
	}
	// Authored stale containers have a named contract distinct from parsed
	// null: wire/text omit them, but cook refuses until emptied (§2.3).
	b.WriteString(`{
final absent=w.Root();
for(final input in const ['{"tags":{"a":3}}','{"refs":{"p":{"n":17}}}','{"tags":{"a":3},"refs":{"p":{"n":17}}}']){
final stale=w.rootFromJson(input);
stale.hasTags=false;stale.hasRefs=false;
check(hex(w.rootWireSave(stale))==hex(w.rootWireSave(absent)),'stale absent maps own no graph node');
check(w.rootToJson(stale)==w.rootToJson(absent),'stale absent maps own no JSON node');
check(c.RootCook.cookMeasure(stale)==-1,'stale absent map cook measure refuses');
for(final order in [Endian.little,Endian.big]){
 final out=Uint8List(4096)..fillRange(0,4096,0xa5);final r=c.TableCookWriteReport();
 check(!c.RootCook.cook(stale,out,order,report:r)&&r.reason=='invalid_value','stale absent map cook refuses');
 check(out.every((x)=>x==0xa5),'refused cook leaves output untouched');
}
stale.tags=[];stale.refs=[];
for(final order in [Endian.little,Endian.big]){
 final a=Uint8List(c.RootCook.cookMeasure(absent)),b=Uint8List(c.RootCook.cookMeasure(stale));
 check(c.RootCook.cook(absent,a,order)&&c.RootCook.cook(stale,b,order)&&hex(a)==hex(b),'emptied optional maps cook as absent');
}
}
}
`)
	// A wrong map entry key is an in-body event, not absent framing (§2.8).
	for _, decl := range []string{"map[int32]int32", "int32", "[]int32"} {
		other := unitFrom(t, "package probe\ntable Root { tags "+decl+" }\n")
		model := tabletext.NewModel(other)
		value := model.New(other.Tables["Root"])
		input := `{"tags":{"1":3}}`
		switch decl {
		case "int32":
			input = `{"tags":3}`
		case "[]int32":
			input = `{"tags":[3]}`
		}
		if !model.Read(value, []byte(input), &tabletext.Report{}) {
			t.Fatal("wrong-kind source")
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		wireList := make([]string, len(wire))
		for i, x := range wire {
			wireList[i] = fmt.Sprint(x)
		}
		fmt.Fprintf(&b, "{final wire=Uint8List.fromList([%s]);final r=w.TableWireReport();final v=w.rootWireLoad(wire,r);check(r.refused,'map mismatch presence %s');}\n", strings.Join(wireList, ","), decl)
	}
	b.WriteString(`{
final r=w.TableTextReport();final v=w.rootFromJson('{"values":{"a":"bad","b":null,"c":7,"c":false}}',r);
check(r.refused,'map value shapes and a repeated key refuse');
check(!v.hasValues&&v.values.isEmpty,'a refused text read lands defaults');
final plainReport=w.TableTextReport();final plain=w.rootFromJson('{"plain":{"a":null,"b":"bad"}}',plainReport);
check(plainReport.refused&&plain.plain.isEmpty,'plain wrong shapes refuse');
}
`)
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Dart optional map %v: %v\n%s", args, err, out)
		}
	}
}

func TestDartOptionalMapSharedPins(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/optional-map"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var b strings.Builder
	b.WriteString("import 'dart:typed_data';\nimport 'ProbeWire.dart' as w;\nvoid check(bool b,String s){if(!b)throw StateError(s);}\nString hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid main(){\n")
	for _, name := range []string{"absent", "empty", "populated"} {
		input, err := os.ReadFile(filepath.Join(fixture, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(fixture, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final v=w.mFromJson(%q);final wire=w.mWireSave(v);check(hex(wire)==%q,%q);
final r=w.TableWireReport();final back=w.mWireLoad(wire,r);check(!r.refused&&back.hasTags==v.hasTags&&w.mToJson(back)==w.mToJson(v),'shared pin read');
for(var n=0;n<wire.length;n++){final r=w.TableWireReport();w.mWireLoad(Uint8List.sublistView(wire,0,n),r);check(r.refused,'truncated map must refuse');}
}
`, string(input), hex.EncodeToString(wire), name)
	}
	b.WriteString(`{final absent=w.mFromJson('{"id":1}');final stale=w.mFromJson('{"id":1,"tags":{"a":3}}');stale.hasTags=false;check(hex(w.mWireSave(stale))==hex(w.mWireSave(absent)),'absent map must not leak backing entries');}
}
`)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shared optional map pins %v: %v\n%s", args, err, out)
		}
	}
}
