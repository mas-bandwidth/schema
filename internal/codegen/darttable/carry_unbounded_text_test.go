package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const dartUnboundedTextSchema = `package probe
table Note { lines []string }
table Root {
 capped [..3]string
 maybe_capped [..3]?string
 open []string
 maybe_open []?string
 by_name map[string]int32
 note *Note
}
`

func TestDartUnboundedTextNative(t *testing.T) {
	t.Skip("layer 4 (dart): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	u := unitFrom(t, dartUnboundedTextSchema)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	longA := strings.Repeat("é", 2048) + "alpha"
	longB := strings.Repeat("é", 2048) + "beta"
	payload := map[string]any{
		"capped":       []any{"", "x", longA},
		"maybe_capped": []any{nil, "", "y"},
		"open":         []any{"hello", "", longB},
		"maybe_open":   []any{nil, "world", ""},
		"by_name":      map[string]int{"": 1, longB: 2, longA: 3},
		"note":         map[string]any{"lines": []string{"nested", longA}},
	}
	populated, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`import 'dart:typed_data';
import 'dart:convert';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String s){if(!ok)throw StateError(s);}
String hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List bytes(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2)int.parse(s.substring(i,i+2),radix:16)]);
void main(){
`)
	for i, input := range []string{`{}`, string(populated), `{"open":["\ud800","\udfff","\udc00\ud800","\ud83d\ude00"],"by_name":{"\ud800":1,"\udfff":2,"\udc00\ud800":3,"\ud83d\ude00":4}}`, `{"by_name":{"same":1,"same":9},"maybe_open":[null,"",42],"capped":["a","b","c","tail"]}`} {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) {
			t.Fatalf("neutral input %d: %+v", i, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		txt, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		measured, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["Root"], wire)
		if !whole || refused {
			t.Fatalf("neutral measure %d %v %v", measured, whole, refused)
		}
		fmt.Fprintf(&b, `{final r=w.TableTextReport();final v=w.rootFromJson(%q,r);
check(r.clamped==%d&&r.kindMismatch==%d&&r.duplicate==%d,'JSON reports %d');
check(w.rootToJson(v)==%q,'independent JSON %d');
final wire=w.rootWireSave(v);check(hex(wire)==%q,'independent wire %d');
final wr=w.TableWireReport();final back=w.rootWireLoad(wire,wr);
check(!wr.malformed&&!wr.refused&&hex(w.rootWireSave(back))==hex(wire),'wire load %d');
check(w.rootWireMeasure(wire)==%d,'exact extent measure %d');
final store=w.TableRetain(65536,List.filled(65536,0));final rr=w.TableWireReport();final retained=w.rootWireLoadRetain(wire,store,rr);
check(!rr.malformed&&!rr.refused&&hex(w.rootWireSaveRetain(retained,store,w.TableWireReport()))==hex(wire),'retained unbounded text %d');
`, input, 0, 0, 0, i, string(txt)+"\n", i, hex.EncodeToString(wire), i, i, measured, i, i)
		for _, big := range []bool{false, true} {
			cooked, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := "Endian.little"
			if big {
				order = "Endian.big"
			}
			fmt.Fprintf(&b, "{final out=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,out,%s)&&hex(out)==%q,'independent cooked bytes %d');}\n", order, hex.EncodeToString(cooked), i)
		}
		if i == 1 {
			fmt.Fprintf(&b, `{
final out=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,out,Endian.little),'cook reader setup');
final cooked=c.RootCook.open(out,0,out.length)!;final row=cooked.root(c.RootRow(cooked.view,0));
final item=row.openAt(2,c.RootOpenTextRow(cooked.view,0));
check(item.valueLength==utf8.encode(%q).length&&utf8.decode(item.valueBytes(out))==%q,'list blob cursor');
final entry=row.byNameFind(utf8.encode(%q),c.RootByNameEntryRow(cooked.view,0));
check(entry!=null&&entry.value==3&&utf8.decode(entry.keyBytes(out))==%q,'long map key lookup');
check(row.byNameFind(utf8.encode('missing'),c.RootByNameEntryRow(cooked.view,0))==null,'missing map key');
final at=item.at;
final oldDelta=cooked.view.getInt64(at,Endian.little);
final blob=at+oldDelta;final oldLength=cooked.view.getUint32(blob,Endian.little);
cooked.view.setUint32(blob,0xffffffff,Endian.little);
check(item.valueLength==0&&item.valueBytes(out).isEmpty,'forged blob length bounded');
cooked.view.setUint32(blob,oldLength,Endian.little);
for(final delta in [0x7fffffffffffffff,-0x7fffffffffffffff]){
 cooked.view.setInt64(at,delta,Endian.little);check(item.valueLength==0&&item.valueBytes(out).isEmpty,'forged blob offset bounded');
}
}
`, longB, longB, longA, longA)
		}
		b.WriteString("}\n")
	}

	for _, payload := range [][]byte{
		{12, 1, 1, 0}, {12, 1, 1, 0xff}, {12, 1, 0xff, 0xff, 0xff, 0xff, 0x0f},
		{12, 0xff, 0xff, 0xff, 0xff, 0x07}, {12, 0xff, 0xff, 0xff, 0xff, 0x0f},
	} {
		wire := []byte{1, 1, 14, byte(len(payload))}
		wire = append(wire, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("open"))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		fmt.Fprintf(&b, `{final r=w.TableWireReport();final data=bytes(%q);final v=w.rootWireLoad(data,r);
check(r.malformed||r.refused,'malformed unbounded payload refused');
check(v.open.isEmpty,'malformed whole load discarded');
}
`, hex.EncodeToString(wire))
	}
	for _, bad := range []string{`{"open":["x\u0000y"]}`, `{"by_name":{"x\u0000y":1}}`} {
		fmt.Fprintf(&b, "{var refused=false;try{w.rootFromJson(%q);}on FormatException{refused=true;}check(refused,'invalid JSON text');}\n", bad)
	}
	b.WriteString(`
for(final bad in ['x\u0000y',String.fromCharCode(0xd800)]){
 final v=w.Root()..open=[w.RootOpenText()..value=bad];
 var refused=false;try{w.rootWireSave(v);}on StateError{refused=true;}check(refused,'invalid authored wire text');
 check(c.RootCook.cookMeasure(v)==-1,'invalid text cook measure');
 for(final order in [Endian.little,Endian.big]){
 final out=Uint8List(65536)..fillRange(0,65536,0xa5);check(!c.RootCook.cook(v,out,order)&&out.every((x)=>x==0xa5),'invalid text cook unchanged');
 }
}
{
 final stale=w.Root()..maybeOpen=[w.RootMaybeOpenText()..value='stale'];
 final absent=w.Root()..maybeOpen=[w.RootMaybeOpenText()];
 check(hex(w.rootWireSave(stale))==hex(w.rootWireSave(absent))&&w.rootToJson(stale)==w.rootToJson(absent),'absent text ignores backing on wire and JSON');
 check(c.RootCook.cookMeasure(stale)==-1,'unreached text extent refused');
 final out=Uint8List(65536)..fillRange(0,65536,0xa5);check(!c.RootCook.cook(stale,out,Endian.little)&&out.every((x)=>x==0xa5),'unreached text destination unchanged');
}
}
`)
	runFormsProbe(t, files, b.String())
}

func TestDartUnboundedTextAcrossFiles(t *testing.T) {
	slowtest.Gate(t, "dart")
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "A.schema"), filepath.Join(dir, "Z.schema")}
	for i, src := range []string{`package probe
table Root { child Other }
`, `package probe
table Other { names []string
 lookup map[string]int32 }
`} {
		if err := os.WriteFile(paths[i], []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	u := loadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	runFormsProbe(t, files, `import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ACook.dart' as c;
void main(){
 final v=w.rootFromJson('{"child":{"names":["a"],"lookup":{"key":2}}}');
 final out=Uint8List(c.RootCook.cookMeasure(v));
 if(!c.RootCook.cook(v,out,Endian.little)||c.RootCook.open(out,0,out.length)==null)throw StateError('cross-file unbounded text');
}
`)
}
