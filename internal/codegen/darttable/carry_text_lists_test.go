package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const dartTextListSchema = `package probe
table Root {
 names [..3]string(8)
 maybe []?string(8)
 loose []string(8)
 bounded [..3]?string(8)
}
`

func TestDartTextListsNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, dartTextListSchema)
	m := tabletext.NewModel(u)
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
	for i, input := range []string{`{}`, `{"names":["a","é",""],"maybe":["x",null,""],"loose":["u",""],"bounded":[null,"v",""]}`, `{"names":["123456789","ééééé","z","tail","tail2"],"maybe":[null,"123456789"]}`, `{"names":["a",42,null],"maybe":[false,{},[]]}`, `{"names":["x\u0000y"]}`, `{"maybe":["\ud800"]}`} {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		ok := m.Read(v, []byte(input), &r)
		// SPEC §16.3 refuses lone surrogate escapes; the old neutral reader
		// still replaces them, so it cannot supply this verdict.
		if !ok || input == `{"maybe":["\ud800"]}` {
			fmt.Fprintf(&b, "{var refused=false;try{w.rootFromJson(%q);}on FormatException{refused=true;}check(refused,'invalid JSON text%d');}\n", input, i)
			continue
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final r=w.TableTextReport();final v=w.rootFromJson(%q,r);
check(r.clamped==%d&&r.kindMismatch==%d,'JSON event counts%d');
check(w.rootToJson(v)==%q,'flat JSON%d');final wire=w.rootWireSave(v);check(hex(wire)==%q,'independent wire%d');
final wr=w.TableWireReport();final back=w.rootWireLoad(wire,wr);check(!wr.malformed&&!wr.refused&&hex(w.rootWireSave(back))==hex(wire),'wire roundtrip');
final store=w.TableRetain(4096,List.filled(4096,0));final rr=w.TableWireReport();final retained=w.rootWireLoadRetain(wire,store,rr);
check(!rr.malformed&&hex(w.rootWireSaveRetain(retained,store,w.TableWireReport()))==hex(wire),'retained text list');
check(w.rootWireMeasure(wire)>0,'text list region measure');
`, input, r.Clamped, r.KindMismatch, i, string(text)+"\n", i, hex.EncodeToString(wire), i)
		for _, big := range []bool{false, true} {
			pin, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := "Endian.little"
			if big {
				order = "Endian.big"
			}
			fmt.Fprintf(&b, "{final out=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,out,%s)&&hex(out)==%q,'independent text list cook');}\n", order, hex.EncodeToString(pin))
		}
		b.WriteString("}\n")
	}
	for _, payload := range [][]byte{append([]byte{12, 1, 9}, []byte("123456789")...), append([]byte{12, 1, 10}, []byte("ééééé")...), {12, 1, 1, 0}, {12, 1, 1, 0xff}, {12, 127}} {
		wire := []byte{1, 1, 14, byte(len(payload))}
		wire = append(wire, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("names"))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		damaged := len(payload) < 4 || payload[3] == 0 || payload[3] == 0xff
		fmt.Fprintf(&b, `{final r=w.TableWireReport();final v=w.rootWireLoad(bytes(%q),r);check(r.malformed==%t,'handframed text validity');
if(!r.malformed){check(v.names.length==1&&v.names[0].value.length>0&&r.clamped==1&&r.clampedField=='Root.names','bounded UTF8 clamp named');}}
`, hex.EncodeToString(wire), damaged)
	}
	b.WriteString(`{
final authored=w.Root();
authored.names=[w.RootNamesText()..value='a',w.RootNamesText()..value='é',w.RootNamesText()];
authored.maybe=[w.RootMaybeText()..hasValue=true..value='x',w.RootMaybeText(),w.RootMaybeText()..hasValue=true];
check(w.rootFromJson(w.rootToJson(authored)).maybe[1].hasValue==false&&w.rootFromJson(w.rootToJson(authored)).maybe[2].hasValue,'absent versus present empty');
final r=w.TableTextReport();w.rootFromJson('{"names":["123456789"]}',r);check(r.clampedField=='Root.names','named JSON clamp');
for(final bad in ['123456789','a\u0000b',String.fromCharCode(0xd800)]){
 final v=w.Root()..names=[w.RootNamesText()..value=bad];
 var wireRefused=false;
 try{w.rootWireSave(v);}on StateError{wireRefused=true;}
 check(wireRefused,'invalid authored wire text refused');
 final output=w.rootToJson(v);
 if(bad.contains('\u0000'))check(output.contains(r'\u0000'),'authored NUL JSON escaped');
 if(bad==String.fromCharCode(0xd800))check(output.contains('\uFFFD')&&!output.contains(String.fromCharCode(0xd800)),'authored surrogate JSON replacement');
 final out=Uint8List(4096)..fillRange(0,4096,0xa5);final cr=c.TableCookWriteReport();
 check(!c.RootCook.cook(v,out,Endian.little,report:cr)&&out.every((x)=>x==0xa5),'invalid authored cook untouched');
}
final over=w.Root()..names=List.generate(4,(i)=>w.RootNamesText()..value='x');
var countRefused=false;try{w.rootWireSave(over);}on StateError{countRefused=true;}check(countRefused,'authored count bound');
}
}
`)
	runFormsProbe(t, files, b.String())
}
