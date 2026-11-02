package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartTextListTotalNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/text-list-total"
	read := func(name string) string {
		b, e := os.ReadFile(filepath.Join(fixture, name))
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	schema, input := read("Probe.schema"), read("input.json")
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Totals"])
	var r tabletext.Report
	if !m.Read(v, []byte(input), &r) || r.Clamped != 8 {
		t.Fatalf("independent JSON prefix %+v", r)
	}
	expected := m.New(u.Tables["Totals"])
	if !m.Read(expected, []byte(read("expected.json")), &tabletext.Report{}) {
		t.Fatal("expected JSON")
	}
	expectedWire, err := tablewire.Encode(m, expected)
	if err != nil {
		t.Fatal(err)
	}
	gotWire, err := tablewire.Encode(m, v)
	if err != nil || hex.EncodeToString(gotWire) != hex.EncodeToString(expectedWire) {
		t.Fatal("neutral JSON prefix differs explicit fixture")
	}
	wideSchema := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(schema, "[..3]", "[..8]"), "string(5)", "string(32)"), "| total=5", "")
	wideUnit := unitFrom(t, wideSchema)
	wm := tabletext.NewModel(wideUnit)
	raw := wm.New(wideUnit.Tables["Totals"])
	if !wm.Read(raw, []byte(input), &tabletext.Report{}) {
		t.Fatal("wide input")
	}
	wire, err := tablewire.Encode(wm, raw)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(`import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String s){if(!ok)throw StateError(s);}
String hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List bytes(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2)int.parse(s.substring(i,i+2),radix:16)]);
void main(){
`)
	fmt.Fprintf(&b, `final expected=bytes(%q);final jr=w.TableTextReport();final v=w.totalsFromJson(%q,jr);
check(jr.clamped==8&&jr.clampedField=='Totals.a'&&hex(w.totalsWireSave(v))==hex(expected),'shared JSON total prefix');
final wr=w.TableWireReport();final back=w.totalsWireLoad(bytes(%q),wr);
check(!wr.malformed&&!wr.refused&&wr.clamped==10&&wr.clampedField=='Totals.a'&&hex(w.totalsWireSave(back))==hex(expected),'wire count plus element plus total clamps');
final store=w.TableRetain(4096,List.filled(4096,0));final rr=w.TableWireReport();final retained=w.totalsWireLoadRetain(bytes(%q),store,rr);
check(rr.clamped==10&&!rr.malformed&&hex(w.totalsWireSaveRetain(retained,store,w.TableWireReport()))==hex(expected),'retained prefix');
`, hex.EncodeToString(expectedWire), input, hex.EncodeToString(wire), hex.EncodeToString(wire))
	for _, big := range []bool{false, true} {
		pin, err := tablecook.Cook(m, expected, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		order := "Endian.little"
		if big {
			order = "Endian.big"
		}
		fmt.Fprintf(&b, "{final out=Uint8List(c.TotalsCook.cookMeasure(v));check(c.TotalsCook.cook(v,out,%s)&&hex(out)==%q,'total cooked prefix');}\n", order, hex.EncodeToString(pin))
	}
	for _, field := range []string{"a", "b", "c", "d"} {
		present := ""
		if field == "b" || field == "d" {
			present = "..hasValue=true"
		}
		fmt.Fprintf(&b, `{final bad=w.Totals()..%s=[w.Totals%sText()%s..value='aaaaa',w.Totals%sText()%s..value='b'];
var wireRefused=false,jsonRefused=false,retainRefused=false;
try{w.totalsWireSave(bad);}on StateError{wireRefused=true;}
try{w.totalsToJson(bad);}on StateError{jsonRefused=true;}
try{w.totalsWireSaveRetain(bad,w.TableRetain(4096,List.filled(4096,0)),w.TableWireReport());}on StateError{retainRefused=true;}
check(wireRefused&&jsonRefused&&retainRefused,'all total writers refuse %s');
check(c.TotalsCook.cookMeasure(bad)==-1,'over-total cook measure');
for(final order in [Endian.little,Endian.big]){final out=Uint8List(4096)..fillRange(0,4096,0xa5);check(!c.TotalsCook.cook(bad,out,order)&&out.every((x)=>x==0xa5),'over-total cook leaves destination');}
}
`, field, strings.ToUpper(field), present, strings.ToUpper(field), present, field)
	}
	payload := append([]byte{12, 3, 5}, []byte("aaaaa")...)
	payload = append(payload, 1, 'x', 1, 0)
	tailWire := []byte{1, 1, 14, byte(len(payload))}
	tailWire = append(tailWire, payload...)
	tailWire = append(tailWire, 0)
	tailWire = binary.LittleEndian.AppendUint64(tailWire, ir.TableWireId("c"))
	tailWire = binary.LittleEndian.AppendUint64(tailWire, 1)
	fmt.Fprintf(&b, `{final jr=w.TableTextReport();final v=w.totalsFromJson('{"c":["aaaaa","x","\\u0000"]}',jr);check(jr.clamped==1&&v.c.length==1&&v.c[0].value=='aaaaa','JSON ignores semantic tail after total');
final r=w.TableWireReport();final back=w.totalsWireLoad(bytes(%q),r);check(r.clamped==1&&!r.malformed&&back.c.length==1&&back.c[0].value=='aaaaa','wire ignores semantic tail after total');}
}
`, hex.EncodeToString(tailWire))
	runFormsProbe(t, files, b.String())
}
