package darttable

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartOptionalPresenceOnVariableWire(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
 enum Level { low, high }
 table Child { value int32 = 7 }
 table Maybe { value ?int32 }
 table Root { count ?int32
 enabled ?bool
 child ?Child
 items ?[..3]int32
 hidden ?Child
 body *string
 level ?Level
 maybe ?Maybe }
 `)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["ProbeWire.dart"]; !ok {
		t.Fatal("optional closure must carry form 1")
	}
	packets, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packets)
	model := tabletext.NewModel(u)
	root := model.New(u.Tables["Root"])
	for i := 0; i < 4; i++ {
		root.Fields[i].Present = true
	}
	root.Fields[6].Present = true
	root.Fields[7].Present = true
	root.Fields[4].Cell.Tab.Fields[0].Cell.I = 99
	root.Fields[4].Cell.Tab.Fields[0].Cell.U = 99 // absent content must not ride
	want, err := tablewire.Encode(model, root)
	if err != nil {
		t.Fatal(err)
	}
	wantText, err := model.Write(root)
	if err != nil {
		t.Fatal(err)
	}
	wantText = append(wantText, '\n')
	t.Run("runtime", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		source := fmt.Sprintf(optionalWireProbe, dartU64Literal(fieldId(u.Tables["Root"].Fields[3])), dartU64Literal(fieldId(u.Tables["Root"].Fields[0])), dartU64Literal(fieldId(u.Tables["Root"].Fields[2])), dartU64Literal(fieldId(u.Tables["Child"].Fields[0])))
		if err := os.WriteFile(filepath.Join(dir, "optional_probe.dart"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		analyze := exec.Command(dartBinary(t), "analyze", "--fatal-infos", "optional_probe.dart")
		analyze.Dir = dir
		if out, err := analyze.CombinedOutput(); err != nil {
			t.Fatalf("optional analyze: %v\n%s", err, out)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "optional_probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("optional probe: %v\n%s", err, out)
		}
		got, err := hex.DecodeString(fieldOf(t, string(out), "wire"))
		if err != nil {
			t.Fatal(err)
		}
		gotText, err := base64.StdEncoding.DecodeString(fieldOf(t, string(out), "json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotText, wantText) {
			t.Fatalf("optional JSON oracle\ngot %s\nwant %s", gotText, wantText)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("optional wire oracle\ngot  %x\nwant %x", got, want)
		}
	})
}

const optionalWireProbe = `import 'dart:typed_data';
import 'dart:convert';
import 'ProbeWire.dart' as w;
const itemsId=%s;
const countId=%s;
const childId=%s;
const valueId=%s;
String hex(Uint8List b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
void check(bool b,String why){if(!b)throw StateError(why);}
Uint8List field(int id,int kind,List<int> bytes){
 final s=w.WireState();final out=w.TableWireWriter(s)..putByte(1);
 out.putLeb(out.refFor(id));out.putByte(kind);out.putBytes(bytes);out.putByte(0);
 for(final id in s.ids){out.putU64(id);}out.putU64(s.ids.length);return Uint8List.fromList(out.buf);
}
Uint8List nestedMismatch(){
 final s=w.WireState();final out=w.TableWireWriter(s)..putByte(1);final ref=out.refFor(childId);final body=out.sub();
 body.putLeb(body.refFor(valueId));body.putByte(1);body.putByte(1);body.putByte(0);
 out.putLeb(ref);out.putByte(13);out.putLeb(body.buf.length);out.putBytes(body.buf);out.putByte(0);
 for(final id in s.ids){out.putU64(id);}out.putU64(s.ids.length);return Uint8List.fromList(out.buf);
}
Uint8List repeatArray(List<int> body){
 final s=w.WireState();final out=w.TableWireWriter(s)..putByte(1);final ref=out.refFor(itemsId);
 out.putLeb(ref);out.putByte(14);out.putLeb(6);out.putBytes([4,1,99,0,0,0]);
 out.putLeb(ref);out.putByte(14);out.putLeb(body.length);out.putBytes(body);out.putByte(0);
 for(final id in s.ids){out.putU64(id);}out.putU64(s.ids.length);return Uint8List.fromList(out.buf);
}
Uint8List mismatch(){
 final s=w.WireState();final out=w.TableWireWriter(s)..putByte(1);
 out.putLeb(out.refFor(itemsId));out.putByte(14);out.putLeb(2);out.putByte(12);out.putLeb(0);out.putByte(0);
 for(final id in s.ids){out.putU64(id);}out.putU64(s.ids.length);return Uint8List.fromList(out.buf);
}
void main(){
 final root=w.Root()..countPresent=true..enabledPresent=true..childPresent=true..itemsPresent=true..levelPresent=true..maybePresent=true;
 root.hidden.value=99;
 final data=w.rootWireSave(root);print('wire ${hex(data)}');
 final r=w.TableWireReport();final back=w.rootWireLoad(data,r);
 check(!r.malformed&&r.kindMismatch==0,'valid optional report');
 check(back.levelPresent&&back.level==0&&back.countPresent&&back.count==0&&back.enabledPresent&&!back.enabled,'present default scalar');
 check(back.maybePresent&&!back.maybe.valuePresent&&back.childPresent&&back.child.value==7&&back.itemsPresent&&back.items.isEmpty,'present empty aggregate');
 check(!back.hiddenPresent&&back.hidden.value==7,'absent dirty value leaked');
 check(hex(w.rootWireSave(back))==hex(data),'optional round trip');
 final text=w.rootToJson(root);print('json ${base64Encode(utf8.encode(text))}');final parsed=w.rootFromJson(text);
 check(parsed.countPresent&&parsed.enabledPresent&&parsed.childPresent&&parsed.itemsPresent&&!parsed.hiddenPresent,'JSON presence');
 final nulls=w.rootFromJson('{"count":null,"enabled":null,"child":null,"items":null}');
 check(!nulls.countPresent&&!nulls.enabledPresent&&!nulls.childPresent&&!nulls.itemsPresent,'null means absence');
 final rr=w.TableWireReport();final skipped=w.rootWireLoad(mismatch(),rr);
 check(rr.kindMismatch==1&&!rr.malformed&&!skipped.itemsPresent,'foreign element kind optional stays absent');
 final wr=w.TableWireReport();final wide=w.rootWireLoad(field(countId,2,[0]),wr);
 check(wide.countPresent&&wide.count==0&&wr.widened==1,'widened optional scalar presence');
 final nr=w.TableWireReport();final nested=w.rootWireLoad(nestedMismatch(),nr);
 check(nested.childPresent&&nested.child.value==7&&nr.kindMismatch==1&&!nr.malformed,'nested mismatch must keep outer presence');
 final cr=w.TableWireReport();final capped=w.rootWireLoad(field(itemsId,14,[18,4,4,1,0,0,0,2,0,0,0,3,0,0,0,4,0,0,0]),cr);
 check(capped.itemsPresent&&capped.items.join(',')=='1,2,3'&&cr.clamped==1&&!cr.malformed,'bounded optional prefix');
 final mr=w.TableWireReport();final damaged=w.rootWireLoad(field(itemsId,14,[4,4,2,1,0]),mr);
 check(damaged.itemsPresent&&mr.malformed,'accepted array framing retains presence after damaged elements');
 for(final bytes in [<int>[0],<int>[1,4]]){
  final ir=w.TableWireReport();final inert=w.rootWireLoad(field(itemsId,14,bytes),ir);
  check(inert.itemsPresent&&inert.items.isEmpty&&!ir.malformed&&ir.kindMismatch==0,'inert array framing still present');
 }
 final fr=w.TableWireReport();final foreign=w.rootWireLoad(field(itemsId,1,[1]),fr);
 check(!foreign.itemsPresent&&!fr.malformed&&fr.kindMismatch==1,'foreign field kind remains absent');
 final lr=w.TableWireReport();final badLength=w.rootWireLoad(field(itemsId,14,[7,4,1,1,0]),lr);
 check(!badLength.itemsPresent&&lr.malformed,'incomplete outer length is not accepted');
 final kr=w.TableWireReport();final repeatKind=w.rootWireLoad(repeatArray([12,0]),kr);
 check(repeatKind.itemsPresent&&repeatKind.items.single==99&&kr.kindMismatch==1&&!kr.malformed,'incompatible repeated array preserves accepted value');
 final qr=w.TableWireReport();final repeatCount=w.rootWireLoad(repeatArray([4,128]),qr);
 check(repeatCount.itemsPresent&&repeatCount.items.single==99&&qr.malformed,'damaged repeated count preserves accepted value');
 final dr=w.TableWireReport();final brokenCount=w.rootWireLoad(field(itemsId,14,[2,4,128]),dr);
 check(brokenCount.itemsPresent&&dr.malformed,'damaged count still present inside field framing');
 final hugeBody=[4,...List<int>.filled(9,255),1,1,0,0,0,2,0,0,0,3,0,0,0];
 final hr=w.TableWireReport();final huge=w.rootWireLoad(field(itemsId,14,[hugeBody.length,...hugeBody]),hr);
 check(huge.itemsPresent&&huge.items.join(',')=='1,2,3'&&hr.clamped==1&&!hr.malformed,'unsigned count cap keeps bounded prefix');
 final tr=w.TableTextReport();final clipped=w.rootFromJson('{"items":[1,2,3,4]}',tr);
 check(clipped.itemsPresent&&clipped.items.length==3&&tr.clamped==1,'JSON bounded optional prefix');
 root.items=[1,2,3,4];var refused=false;try{w.rootWireSave(root);}on StateError{refused=true;}check(refused,'writer must reject oversized bounded array');
 root.itemsPresent=false;w.rootWireSave(root);

}
`
