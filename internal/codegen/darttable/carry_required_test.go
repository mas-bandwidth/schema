package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartRequiredNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	schema := `package probe
table Root {}
table RequiredValue { a int32 | required
 b int32 | required }
table RequiredDefault { a int32 = 9 | required }
table Exact { ver uint8 | min = 7, max = 7, required }
table ExactFloat { amount float32 | min = 0.5, max = 1.5, resolution = 0.1, required }
table ExactWide { amount int128 | min = 7, max = 8, required }
table ExactGuarded { active bool
 if active { amount uint8 | min = 7, max = 8, required } }
table RequiredArray { values [..4]int32 | required }
table Other { ver uint8 }
table RequiredEmpty { value Other | required }
table Inner { id int32 | required }
table Outer { inner Inner }
table Sequence { children []Inner }
table Mapping { items map[uint8]Inner }
table Graph { child *Inner }
table Guarded { active bool
 if active { req int32 | required } }
enum Key { A, B }
flags Flag { A, B }
union Choice { n int32 }
table Keyed { values [Key]int32 | required }
table Shapes {
 enabled bool | required
 integer int32 | required
 floating float32 | required
 text string(8) | required
 data bytes(8) | required
 wide wstring(8) | required
 huge int128 | min = -100, max = 100, required
 enumeration Key | required
 mask Flag | required
 choice Choice | required
 fixedValues [2]int32 | required
 counted [..2]int32 | required
 list []int32 | required
 mapping map[uint8]int32 | required
 child Other | required
 pointer *Other | required
 blob *string | required
 at timestamp | required
}
`
	schema += "\ntable Many {\n"
	for i := range 512 {
		schema += fmt.Sprintf(" f%d int32\n", i)
	}
	schema += " last int32 | required\n}\n"
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var b strings.Builder
	b.WriteString("import 'dart:typed_data';\nimport 'ProbeWire.dart' as w;\nimport 'ProbeCook.dart' as c;\nvoid check(bool b,String s){if(!b)throw StateError(s);}\nString hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nUint8List bytes(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2)int.parse(s.substring(i,i+2),radix:16)]);\nvoid main(){\n")
	for _, tc := range []struct {
		root, input, missing string
		km                   int
	}{
		{"Many", `{}`, "last", 0}, {"Many", `{"last":0}`, "", 0},
		{"RequiredValue", `{}`, "a, b", 0}, {"RequiredValue", `{"a":0}`, "b", 0},
		{"RequiredValue", `{"a":0,"b":0}`, "", 0}, {"RequiredValue", `{"a":null,"b":0}`, "", 1},
		{"RequiredDefault", `{}`, "a", 0}, {"RequiredDefault", `{"a":9}`, "", 0},
		{"RequiredArray", `{}`, "values", 0}, {"RequiredArray", `{"values":{}}`, "", 1},
		{"RequiredEmpty", `{}`, "value", 0}, {"RequiredEmpty", `{"value":{}}`, "", 0},
		{"Outer", `{"inner":{}}`, "id", 0}, {"Sequence", `{"children":[{}]}`, "id", 0},
		{"Mapping", `{"items":{"1":{}}}`, "id", 0}, {"Graph", `{"child":{}}`, "id", 0},
		{"Graph", `{"child":{"id":0}}`, "", 0}, {"Guarded", `{}`, "", 0},
		{"Guarded", `{"active":true}`, "req", 0}, {"Guarded", `{"req":0,"active":true}`, "", 0},
		{"Keyed", `{}`, "values", 0}, {"Keyed", `{"values":{}}`, "", 0},
		{"Exact", `{}`, "ver", 0}, {"Exact", `{"ver":7}`, "", 0},
	} {
		fmt.Fprintf(&b, `{final r=w.TableTextReport();var reason='';try{w.%sFromJson(%q,r);}on FormatException catch(e){reason=e.message.toString();}
check(reason==%q,%q);}
`, dartName(tc.root), tc.input, func() string {
			if tc.missing != "" {
				return "missing_required: " + tc.missing
			}
			return ""
		}(), tc.root+" "+tc.input)
	}
	m := tabletext.NewModel(u)
	pins := requiredDartGoPins(t, u, []string{"RequiredValue", "RequiredDefault", "RequiredArray", "RequiredEmpty", "Keyed", "Shapes", "Many"})
	for _, root := range []string{"RequiredValue", "RequiredDefault", "RequiredArray", "RequiredEmpty", "Keyed", "Shapes", "Many"} {
		value := m.New(u.Tables[root])
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(root, err)
		}
		if hex.EncodeToString(wire) != pins[root] {
			t.Errorf("independent Go/neutral required %s differs: %x vs %s", root, wire, pins[root])
		}
		fmt.Fprintf(&b, `{final v=w.%s();final wire=w.%sWireSave(v);check(hex(wire)==%q,%q);
final r=w.TableWireReport();final back=w.%sWireLoad(wire,r);check(!r.refused,'required wire read '+%q);
check(hex(w.%sWireSave(back))==hex(wire),'required roundtrip');
final absent=bytes('01000000000000000000');final missing=w.TableWireReport();w.%sWireLoad(absent,missing);check(missing.refused&&missing.reason.startsWith('missing_required:'),'missing wire required');
w.%sWireLoad(wire,missing);check(!missing.refused&&missing.reason=='ok','reused required report');
}
`, root, dartName(root), hex.EncodeToString(wire), root+" zero wire", dartName(root), root, dartName(root), dartName(root), dartName(root))
	}
	for _, tc := range []struct {
		root string
		body []byte
		ids  []uint64
	}{
		{"Outer", []byte{1, 13, 1, 0, 0}, []uint64{ir.TableFieldWireId(u.Tables["Outer"].Fields[0])}},
		{"Sequence", []byte{1, 14, 4, 13, 1, 1, 0, 0}, []uint64{ir.TableFieldWireId(u.Tables["Sequence"].Fields[0])}},
		{"Mapping", []byte{1, 14, 11, 13, 1, 8, 2, 6, 1, 3, 13, 1, 0, 0, 0}, []uint64{ir.TableFieldWireId(u.Tables["Mapping"].Fields[0]), ir.MapKeyWireId, ir.MapValueWireId}},
		{"Graph", []byte{1, 17, 2, 2, 12, 4, 1, 3, 1, 0, 0}, []uint64{ir.TableFieldWireId(u.Tables["Graph"].Fields[0]), ir.TableNodeWireId, ir.TableWireId(u.Tables["Inner"].WireName())}},
	} {
		wire := append([]byte{1}, tc.body...)
		for _, id := range tc.ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(tc.ids)))
		fmt.Fprintf(&b, `{final r=w.TableWireReport();w.%sWireLoad(bytes(%q),r);check(r.refused&&r.reason=='missing_required: id',%q);
final retain=w.TableRetain(4096,List.filled(4096,0));final rr=w.TableWireReport();w.%sWireLoadRetain(bytes(%q),retain,rr);check(rr.refused&&rr.reason==r.reason&&retain.used==0,'nested retained refusal');}
`, dartName(tc.root), hex.EncodeToString(wire), tc.root+" nested required wire", dartName(tc.root), hex.EncodeToString(wire))
	}
	populated := `{"enabled":true,"integer":9,"floating":1.25,"text":"hi","data":"AQI=","wide":"é","huge":17,"enumeration":"A","mask":["B"],"choice":{"n":23},"fixedValues":[1,2],"counted":[3],"list":[4,5],"mapping":{"6":7},"child":{"ver":8},"pointer":{"ver":9},"blob":"hi","at":"2000-01-01T00:00:00Z"}`
	pv := m.New(u.Tables["Shapes"])
	pr := tabletext.Report{}
	if !m.Read(pv, []byte(populated), &pr) || !pr.Silent() {
		t.Fatalf("populated shapes: %+v", pr)
	}
	pw, err := tablewire.Encode(m, pv)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "{final v=w.shapesFromJson(%q);final wire=w.shapesWireSave(v);check(hex(wire)==%q,'populated required shapes');final r=w.TableWireReport();final back=w.shapesWireLoad(wire,r);check(!r.refused&&hex(w.shapesWireSave(back))==hex(wire),'populated required shape read');\n", populated, hex.EncodeToString(pw))
	for _, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(m, pv, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		order := "Endian.little"
		if big {
			order = "Endian.big"
		}
		fmt.Fprintf(&b, "{final out=Uint8List(c.ShapesCook.cookMeasure(v));check(c.ShapesCook.cook(v,out,%s)&&hex(out)==%q,'populated required shape cook');}\n", order, hex.EncodeToString(cooked))
	}
	b.WriteString("}\n")
	// Compatible outer array framing with an incompatible element kind is absent.
	other := unitFrom(t, "package probe\ntable RequiredArray { values [..4]string(8) }\n")
	om := tabletext.NewModel(other)
	ov := om.New(other.Tables["RequiredArray"])
	if !om.Read(ov, []byte(`{"values":["x"]}`), &tabletext.Report{}) {
		t.Fatal("source")
	}
	ow, err := tablewire.Encode(om, ov)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "{final r=w.TableWireReport();w.requiredArrayWireLoad(bytes(%q),r);check(r.refused&&r.reason=='missing_required: values','incompatible array element kind');}\n", hex.EncodeToString(ow))
	for _, root := range []string{"Exact", "ExactFloat", "ExactWide"} {
		class := root + "CookWire"
		if ir.VariableTables(u)[root] {
			class = root + "Cook"
		}
		fmt.Fprintf(&b, `{
var failed=false;try{w.%sWireSave(w.%s());}on StateError{failed=true;}check(failed,'fresh range wire %s');
failed=false;try{w.%sToJson(w.%s());}on StateError{failed=true;}check(failed,'fresh range JSON %s');
check(c.%s.cookMeasure(w.%s())==-1,'fresh range cook measure %s');
for(final order in [Endian.little,Endian.big]){final out=Uint8List(4096)..fillRange(0,4096,0xa5);
check(!c.%s.cook(w.%s(),out,order)&&out.every((x)=>x==0xa5),'fresh range cook writes nothing %s');}
}
`, dartName(root), root, root, dartName(root), root, root, class, root, root, class, root, root)
	}
	b.WriteString(`{final off=w.ExactGuarded();w.exactGuardedWireSave(off);check(!w.exactGuardedToJson(off).contains('amount'),'off guard omits required field');
off.active=true;var failed=false;try{w.exactGuardedWireSave(off);}on StateError{failed=true;}check(failed,'active required zero refuses');
}
`)
	b.WriteString(`{
for(final text in const ['null','42','[]','"x"']){var failed=false;try{w.requiredValueFromJson(text);}on FormatException{failed=true;}check(failed,'invalid root cannot bypass required checks');}
final r=w.TableTextReport();w.manyFromJson('{"last":0,"last":7}',r);check(r.refused,'last field beyond512 repeated key refuses');
}
`)
	b.WriteString(`{
var failed=false;try{w.exactWireSave(w.Exact());}on StateError{failed=true;}check(failed,'fresh required range zero must refuse wire');
failed=false;try{w.exactToJson(w.Exact());}on StateError{failed=true;}check(failed,'fresh required range zero must refuse JSON');
}
}
`)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("required Dart %v: %v\n%s", args, err, out)
		}
	}
}

func requiredDartGoPins(t *testing.T, u *ir.Unit, roots []string) map[string]string {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	output := filepath.Join(dir, "pins.json")
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	var b strings.Builder
	b.WriteString("package probe\nimport(\"encoding/hex\";\"encoding/json\";\"os\";\"testing\")\nfunc TestRequiredPins(t *testing.T){pins:=map[string]string{}\n")
	for _, root := range roots {
		fmt.Fprintf(&b, "{var v %s;%sReset(&v);n:=%sMeasure(&v);if n<0{t.Fatal(%q)};out:=make([]byte,n);if %sSave(&v,out)!=n{t.Fatal(\"save\")};pins[%q]=hex.EncodeToString(out)}\n", root, root, root, root, root, root)
	}
	fmt.Fprintf(&b, "out,err:=json.Marshal(pins);if err!=nil{t.Fatal(err)};if err=os.WriteFile(%q,out,0600);err!=nil{t.Fatal(err)}}\n", output)
	files["oracle_test.go"] = []byte(b.String())
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "-run", "^TestRequiredPins$", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("independent required Go: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]string
	if err = json.Unmarshal(data, &pins); err != nil {
		t.Fatal(err)
	}
	return pins
}

func TestDartRequiredSharedPins(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/required"
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
	b.WriteString("import 'dart:typed_data';\nimport 'ProbeWire.dart' as w;\nvoid check(bool b,String s){if(!b)throw StateError(s);}\nString hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nUint8List bytes(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2)int.parse(s.substring(i,i+2),radix:16)]);\nvoid main(){\n")
	for _, tc := range []struct{ name, missing string }{{"missing", "b, c"}, {"missing-b", "b"}, {"zero", ""}, {"populated", ""}} {
		input, err := os.ReadFile(filepath.Join(fixture, tc.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(fixture, tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		reason := "ok"
		if tc.missing != "" {
			reason = "missing_required: " + tc.missing
		}
		fmt.Fprintf(&b, `{final wire=bytes(%q);final r=w.TableWireReport();final v=w.rootWireLoad(wire,r);
check(r.reason==%q,%q);
final retained=w.TableRetain(4096,List.filled(4096,0));final rr=w.TableWireReport();final back=w.rootWireLoadRetain(wire,retained,rr);
check(rr.reason==r.reason,'retained Required');
if(r.refused){check(v.a==0&&v.b==0&&v.c==0&&back.a==0&&back.b==0&&back.c==0&&retained.used==0,'refused read discards values and retained data');}
else{check(hex(w.rootWireSave(v))==hex(wire)&&hex(w.rootWireSaveRetain(back,retained,w.TableWireReport()))==hex(wire),'independent pin save');}
final jr=w.TableTextReport();var textReason='ok';try{final parsed=w.rootFromJson(%q,jr);check(hex(w.rootWireSave(parsed))==hex(wire),'JSON pin');}on FormatException catch(e){textReason=e.message.toString();}
check(textReason==%q,'JSON required pin');
}
`, hex.EncodeToString(wire), reason, tc.name, string(input), reason)
	}
	b.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("required shared pins %v: %v\n%s", args, err, out)
		}
	}
}
