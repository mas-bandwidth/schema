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
	// STRICT VERDICT: the Go oracle refuses the over-total input whole
	// (length_over_bound), as the Dart read below must.
	if m.Read(v, []byte(input), &r) || !r.Refused {
		t.Fatalf("independent JSON total must refuse: %+v", r)
	}
	expected := m.New(u.Tables["Totals"])
	if !m.Read(expected, []byte(read("expected.json")), &tabletext.Report{}) {
		t.Fatal("expected JSON")
	}
	expectedWire, err := tablewire.Encode(m, expected)
	if err != nil {
		t.Fatal(err)
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
check(jr.refused&&jr.reason=='length_over_bound'&&v.a.isEmpty&&v.b.isEmpty&&v.c.isEmpty&&v.d.isEmpty,'shared JSON total refusal: ${jr.reason}');
final wr=w.TableWireReport();final back=w.totalsWireLoad(bytes(%q),wr);
check(wr.refused&&(wr.reason=='count_over_bound'||wr.reason=='length_over_bound')&&back.a.isEmpty&&back.c.isEmpty,'wire total refusal: ${wr.reason}');
final store=w.TableRetain(4096,List.filled(4096,0));final rr=w.TableWireReport();final retained=w.totalsWireLoadRetain(bytes(%q),store,rr);
check(rr.refused&&(rr.reason=='count_over_bound'||rr.reason=='length_over_bound')&&retained.a.isEmpty&&store.used==0,'retained total refusal: ${rr.reason}');
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
		fmt.Fprintf(&b, "{final ev=w.totalsWireLoad(expected,w.TableWireReport());final out=Uint8List(c.TotalsCook.cookMeasure(ev));check(c.TotalsCook.cook(ev,out,%s)&&hex(out)==%q,'total cooked prefix');}\n", order, hex.EncodeToString(pin))
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
	fmt.Fprintf(&b, `{final jr=w.TableTextReport();final v=w.totalsFromJson('{"c":["aaaaa","x","\\u0000"]}',jr);check(jr.refused&&v.c.isEmpty,'JSON total refusal lands nothing: ${jr.reason}');
final r=w.TableWireReport();final back=w.totalsWireLoad(bytes(%q),r);check(r.refused&&back.c.isEmpty,'wire total refusal lands nothing: ${r.reason}');}
}
`, hex.EncodeToString(tailWire))
	runFormsProbe(t, files, b.String())
}
