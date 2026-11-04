package darttable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sharedDartFixture(t *testing.T, suffix string) (string, []byte) {
	t.Helper()
	base := filepath.Join("../../../testdata/conformance/feature-witnesses", suffix)
	schema, err := os.ReadFile(filepath.Join(base, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile(filepath.Join(base, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(schema), cases
}

const dartClosurePrelude = `import 'ProbeWire.dart' as w;
import 'dart:typed_data';
void check(bool ok,String s){if(!ok)throw StateError(s);}
String hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List bytes(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2)int.parse(s.substring(i,i+2),radix:16)]);
void main(){
`

func TestDartArrayRootSharedClosure(t *testing.T) {
	t.Skip("layer 4 (dart): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	schema, raw := sharedDartFixture(t, "array-root/closure")
	var cases []struct {
		Root, Text, Reason string
		Mismatch, Clamped  int
		Malformed          bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 12 {
		t.Fatalf("closure corpus rows=%d, want12", len(cases))
	}
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for i, tc := range cases {
		v := m.New(u.Tables[tc.Root])
		var r tabletext.Report
		ok := m.Read(v, []byte(tc.Text), &r)
		if ok == tc.Malformed || tc.Mismatch != 0 || tc.Clamped != 0 {
			t.Fatalf("independent corpus row%d: ok=%v report=%+v", i, ok, r)
		}
		fn := dartName(tc.Root)
		if tc.Malformed {
			fmt.Fprintf(&b, "{var refused=false;try{w.%sFromJson(%q);}on FormatException catch(e){refused=true;check(e.message.toString().contains(%q),'named root refusal %d');}check(refused,'root refusal %d');}\n", fn, tc.Text, tc.Reason, i, i)
			continue
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		txt, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final r=w.TableTextReport();final v=w.%sFromJson(%q,r);
check(r.kindMismatch==%d&&r.clamped==%d,'explicit closure report %d');
check(hex(w.%sWireSave(v))==%q,'independent closure wire %d: '+hex(w.%sWireSave(v))+' JSON '+w.%sToJson(v));
check(w.%sToJson(v)==%q,'independent closure JSON %d');}
`, fn, tc.Text, tc.Mismatch, tc.Clamped, i, fn, hex.EncodeToString(wire), i, fn, fn, fn, string(txt)+"\n", i)
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}

func TestDartRequiredNestedGuardsSharedClosure(t *testing.T) {
	slowtest.Gate(t, "dart")
	schema, raw := sharedDartFixture(t, "required/nested-guards")
	var cases []struct {
		Root, Input, Missing string
		Fields               []string
		Value                int
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 12 {
		t.Fatalf("nested guard corpus rows=%d, want12", len(cases))
	}
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for i, tc := range cases {
		fn := dartName(tc.Root)
		v := m.New(u.Tables[tc.Root])
		var r tabletext.Report
		ok := m.Read(v, []byte(tc.Input), &r)
		if tc.Missing != "" {
			if ok || !r.Refused {
				t.Fatalf("independent missing row%d: %+v", i, r)
			}
			fmt.Fprintf(&b, `{final r=w.TableTextReport();var refused=false;try{w.%sFromJson(%q,r);}on FormatException{refused=true;}
check(refused&&r.missingRequired==1&&r.missingFields==%q,'nested guard missing %d');}
`, fn, tc.Input, tc.Missing, i)
			continue
		}
		if !ok {
			t.Fatalf("independent valid row%d: %+v", i, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		txt, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final r=w.TableTextReport();final v=w.%sFromJson(%q,r);
check(r.missingRequired==0&&hex(w.%sWireSave(v))==%q,'nested guard writer %d');
check(w.%sToJson(v)==%q,'nested guard JSON %d');
final wr=w.TableWireReport();final back=w.%sWireLoad(bytes(%q),wr);
check(!wr.refused&&!wr.malformed&&wr.missingRequired==0&&hex(w.%sWireSave(back))==%q,'nested guard wire read %d');}
`, fn, tc.Input, fn, hex.EncodeToString(wire), i, fn, string(txt)+"\n", i, fn, hex.EncodeToString(wire), fn, hex.EncodeToString(wire), i)
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}

func TestDartJSONListWrongShapeKeepsPosition(t *testing.T) {
	t.Skip("layer 4 (dart): tolerant-read test, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Item { n int32 = 7 }
table Root { rows [..2]Item
 flags [..3]bool }
`)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Root"])
	var r tabletext.Report
	input := `{"rows":[{},7,{"n":9}],"flags":[true,7,false,true]}`
	if !m.Read(v, []byte(input), &r) {
		t.Fatalf("explicit wrong-shape positions: %+v", r)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	txt, err := m.Write(v)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	source := dartClosurePrelude + fmt.Sprintf(`final r=w.TableTextReport();final v=w.rootFromJson(%q,r);
check(v.rows.length==2&&v.rows[1].n==7&&v.flags.length==3&&!v.flags[1]&&!v.flags[2],'wrong-shape positions/defaults');
check(r.kindMismatch==2&&r.clamped==2,'wrong-shape counts');
check(hex(w.rootWireSave(v))==%q&&w.rootToJson(v)==%q,'independent wrong-shape bytes/text');
}
`, input, hex.EncodeToString(wire), string(txt)+"\n")
	runFormsProbe(t, files, source)
}

func TestDartPopulatedFeatureWitnesses(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/full"
	u := loadUnit(t, filepath.Join(fixture, "Features.schema"))
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(strings.Replace(dartClosurePrelude, "ProbeWire.dart", "FeaturewitnessesWire.dart", 1))
	// Imports precede main; cook accessors live with the declaring source file.
	source := "import 'FeaturesCook.dart' as c;\n" + b.String()
	b.Reset()
	b.WriteString(source)
	for _, tc := range []struct{ root, name string }{{"Root", "feature_full"}, {"ArrayRoot", "feature_array_root"}} {
		input, err := os.ReadFile("../../../testdata/conformance/tables/json/" + tc.name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		pin, err := os.ReadFile(filepath.Join(fixture, tc.root+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		v := m.New(u.Tables[tc.root])
		if !m.Read(v, input, &tabletext.Report{}) {
			t.Fatal("feature oracle read")
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil || hex.EncodeToString(wire) != hex.EncodeToString(pin) {
			t.Fatalf("independent feature wire %s %v", tc.root, err)
		}
		measure, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[tc.root], pin)
		if !whole || refused {
			t.Fatal("feature extent oracle")
		}
		fn := dartName(tc.root)
		fmt.Fprintf(&b, `{final v=w.%sFromJson(%q);final expected=bytes(%q);
check(hex(w.%sWireSave(v))==hex(expected),'active feature writer');
check(w.%sToJson(v)==%q,'active feature JSON');
final store=w.TableRetain(65536,List.filled(65536,0));final r=w.TableWireReport();final back=w.%sWireLoadRetain(expected,store,r);
check(!r.malformed&&!r.refused&&hex(w.%sWireSaveRetain(back,store,w.TableWireReport()))==hex(expected),'active feature retained');
check(w.%sWireMeasure(expected)==%d,'active feature extent');
`, fn, string(input), hex.EncodeToString(pin), fn, fn, string(input), fn, fn, fn, measure)
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := "Endian.little"
			if big {
				order = "Endian.big"
			}
			fmt.Fprintf(&b, "{final out=Uint8List(c.%sCook.cookMeasure(v));check(c.%sCook.cook(v,out,%s)&&hex(out)==%q,'active feature independent cooked bytes');}\n", tc.root, tc.root, order, hex.EncodeToString(cooked))
		}
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
