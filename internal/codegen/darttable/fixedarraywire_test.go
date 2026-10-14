package darttable

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestDartFixedArrayWireHelpers(t *testing.T) {
	u := unitFrom(t, `package probe
enum Grade { Bronze, Silver }
table Item { value int32 = 7 }
table Root { nums [3]int32
 present ?[3]int32
 rows [2]Item
 refs [2]*Item
 after int32
 flags [2]bool
 masks [2]uint8
 floats [2]float32
 doubles [2]float64
 grades [2]Grade }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes, ok := files["ProbeWire.dart"]
	if !ok {
		t.Fatal("fixed positional array closure must emit form1")
	}
	source := string(sourceBytes)
	packets, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packets)
	for _, want := range []string{"List<Item>.generate(2", "List<int>.filled(3, 0)", "v.nums.any((e) => e != 0)", "final keep=(count<0||count>3)?3:count"} {
		if !strings.Contains(source, want) {
			t.Fatalf("missing helper shape %q", want)
		}
	}
	model := tabletext.NewModel(u)
	root := model.New(u.Tables["Root"])
	for i, x := range []uint64{0, 12, 0} {
		root.Fields[0].Elems[i].U = x
		root.Fields[0].Elems[i].I = int64(x)
	}
	root.Fields[2].Elems[0].Tab.Fields[0].Cell.U = 11
	root.Fields[2].Elems[0].Tab.Fields[0].Cell.I = 11
	node := model.New(u.Tables["Item"])
	node.Fields[0].Cell.U = 23
	node.Fields[0].Cell.I = 23
	root.Fields[3].Elems[0].Node = node
	root.Fields[3].Elems[1].Node = node
	root.Fields[5].Elems[1].B = true
	root.Fields[6].Elems[1].U = 5
	root.Fields[7].Elems[0].F = math.Copysign(0, -1)
	root.Fields[7].Elems[1].F = 1.5
	root.Fields[8].Elems[0].F = math.Copysign(0, -1)
	root.Fields[8].Elems[1].F = 2.25
	root.Fields[9].Elems[1].U = 2
	root.Fields[4].Cell.U = 77
	root.Fields[4].Cell.I = 77
	want, err := tablewire.Encode(model, root)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault, err := tablewire.Encode(model, model.New(u.Tables["Root"]))
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("runtime-production-oracle", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		probe := fmt.Sprintf(fixedArrayProbe, dartU64Literal(fieldId(u.Tables["Root"].Fields[0])), dartU64Literal(fieldId(u.Tables["Root"].Fields[4])), dartU64Literal(fieldId(u.Tables["Root"].Fields[2])), dartU64Literal(fieldId(u.Tables["Root"].Fields[1])))
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixed array: %v\n%s", err, out)
		}
		empty, err := hex.DecodeString(fieldOf(t, string(out), "empty"))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"inert0", "inert1", "slack", "huge", "repeatZero", "repeatShort"} {
			input, e := hex.DecodeString(fieldOf(t, string(out), key))
			if e != nil {
				t.Fatal(e)
			}
			dest := model.New(u.Tables["Root"])
			var report tabletext.Report
			if ok, e := tablewire.Decode(model, dest, input, &report); !ok || e != nil {
				t.Fatalf("%s Go decode: %v %+v", key, e, report)
			}
			expected := []uint64{1, 2, 3}
			if key == "slack" {
				expected = []uint64{4, 0, 0}
			}
			if key == "repeatShort" {
				expected = []uint64{4, 2, 3}
			}
			for i, value := range expected {
				if dest.Fields[0].Elems[i].U != value {
					t.Fatalf("%s Go prefix differs", key)
				}
			}
			if report.Malformed || report.KindMismatch != 0 || report.Clamped != fixedArrayExpectedClamps(key) || dest.Fields[4].Cell.U != 77 {
				t.Fatalf("%s Go report differs %+v", key, report)
			}
		}
		if !bytes.Equal(empty, wantDefault) {
			t.Fatalf("all-default fixed arrays/first-use ids differ from Go\ngot%x\nwant%x", empty, wantDefault)
		}
		got, err := hex.DecodeString(fieldOf(t, string(out), "wire"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("production Go oracle differs\ngot %x\nwant%x", got, want)
		}
		if raw, e := base64.StdEncoding.DecodeString(fieldOf(t, string(out), "json")); e != nil {
			t.Fatal(e)
		} else if got := string(raw); got != string(text) {
			t.Fatalf("production Go text differs\ngot %s\nwant%s", got, text)
		}
	})
}

const fixedArrayProbe = `import 'dart:convert';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
const numsId=%s;const afterId=%s;const rowsId=%s;const presentId=%s;
void check(bool ok,String why){if(!ok)throw StateError(why);}
String hex(Uint8List b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();
Uint8List forged(int count,List<int> values,{int elem=4,int? tiny,bool duplicate=false,bool huge=false,int field=numsId}){
 final state=w.WireState();final out=w.TableWireWriter(state)..putByte(1);final ref=out.refFor(field);
 void array(int n,List<int> xs,int ek){final body=out.sub()..putByte(ek);if(huge){body.putBytes([255,255,255,255,255,255,255,255,255,1]);}else{body.putLeb(n);}for(final x in xs){body.putU32(x);}out.putLeb(ref);out.putByte(14);out.putLeb(body.buf.length);out.putBytes(body.buf);}
 if(duplicate)array(3,[1,2,3],4);
 if(tiny!=null){out.putLeb(ref);out.putByte(14);out.putLeb(tiny);if(tiny==1)out.putByte(4);}else{array(count,values,elem);}
 out.putLeb(out.refFor(afterId));out.putByte(4);out.putU32(77);out.putByte(0);for(final id in state.ids){out.putU64(id);}out.putU64(state.ids.length);return Uint8List.fromList(out.buf);
}
Uint8List forgedRows(){
 final state=w.WireState();final out=w.TableWireWriter(state)..putByte(1);final ref=out.refFor(rowsId);
 final arr=out.sub()..putByte(13)..putLeb(1);final body=arr.sub();w.itemWireSaveBody(body,w.Item()..value=11);arr.putLeb(body.buf.length);arr.putBytes(body.buf);
 out.putLeb(ref);out.putByte(14);out.putLeb(arr.buf.length);out.putBytes(arr.buf);out.putByte(0);for(final id in state.ids){out.putU64(id);}out.putU64(state.ids.length);return Uint8List.fromList(out.buf);
}
void main(){
 check(c.tableCookMagic!=0,'cook reader dependency compiles');
 for(final tiny in [0,1]){final report=w.TableWireReport();final value=w.rootWireLoad(forged(0,[],tiny:tiny,field:presentId),report);check(value.presentPresent&&!report.malformed&&report.kindMismatch==0,'fixed optional inert presence');}
 final foreign=w.TableWireReport();final absent=w.rootWireLoad(forged(0,[],elem:1,field:presentId),foreign);check(!absent.presentPresent&&foreign.kindMismatch==1&&!foreign.malformed,'fixed optional foreign element remains absent');

 final tablePrefix=w.rootWireLoad(forgedRows());check(tablePrefix.rows.length==2&&tablePrefix.rows[0].value==11&&tablePrefix.rows[1].value==7&&!identical(tablePrefix.rows[0],tablePrefix.rows[1]),'short table prefix pads independent declared-default slots');
 final v=w.Root()..nums=[0,12,0]..after=77..flags=[false,true]..masks=[0,5]..floats=[-0.0,1.5]..doubles=[-0.0,2.25]..grades=[0,2];
 check(!identical(v.rows[0],v.rows[1]),'table slots must have independent instances');
 v.rows[0].value=11;check(v.rows[1].value==7,'mutating one table slot must preserve its sibling');
 final node=w.Item()..value=23;v.refs=[node,node];
 final wire=w.rootWireSave(v);print('wire '+hex(wire));print('json '+base64Encode(utf8.encode(w.rootToJson(v).trimRight())));
 final r=w.TableWireReport();final back=w.rootWireLoad(wire,r);check(!r.malformed&&!r.refused&&back.nums.join(',')=='0,12,0','valid read');
 check(hex(w.rootWireSave(back))==hex(wire),'canonical resave');
 final jsonBack=w.rootFromJson(w.rootToJson(v));check(hex(w.rootWireSave(jsonBack))==hex(wire),'JSON negative-zero resave');
 check(identical(back.refs[0],back.refs[1])&&back.refs[0]!.value==23,'pointer slots preserve node identity');
 final d=w.Root();final base=w.rootWireSave(d);print('empty '+hex(base));d.presentPresent=true;final present=w.rootWireSave(d);check(present.length>base.length,'present whole-default rides');
 final p=w.rootWireLoad(present);check(p.presentPresent&&p.present.join(',')=='0,0,0','present defaults retained');
 for(final tiny in [0,1]){final input=forged(0,[],tiny:tiny,duplicate:true);final report=w.TableWireReport();final kept=w.rootWireLoad(input,report);check(!report.malformed&&report.kindMismatch==0&&report.clamped==0&&kept.nums.join(',')=='1,2,3'&&kept.after==77,'inert header preserves previous value');print('inert$tiny '+hex(input));}
 final slack=forged(1,[4,5]);final sr=w.TableWireReport();final sv=w.rootWireLoad(slack,sr);check(!sr.malformed&&sr.clamped==0&&sv.nums.join(',')=='4,0,0'&&sv.after==77,'unread field slack is inert');print('slack '+hex(slack));
 final short=w.rootWireLoad(forged(1,[4]));check(short.nums.join(',')=='4,0,0'&&short.after==77,'short prefix padding');
 final zeroInput=forged(0,[],duplicate:true);print('repeatZero '+hex(zeroInput));final zero=w.rootWireLoad(zeroInput);check(zero.nums.join(',')=='1,2,3','zero fixed repeat keeps unreached positions');
 final shortInput=forged(1,[4],duplicate:true);print('repeatShort '+hex(shortInput));final repeatedShort=w.rootWireLoad(shortInput);check(repeatedShort.nums.join(',')=='4,2,3','short fixed repeat keeps unreached positions');
 final over=w.TableWireReport();final capped=w.rootWireLoad(forged(4,[1,2,3,4]),over);check(over.clamped==1&&!over.malformed&&capped.nums.join(',')=='1,2,3'&&capped.after==77,'count clamp');
 final hugeReport=w.TableWireReport();final hugeInput=forged(0,[1,2,3],huge:true);final hugeValue=w.rootWireLoad(hugeInput,hugeReport);check(!hugeReport.malformed&&hugeReport.clamped==1&&hugeValue.nums.join(',')=='1,2,3'&&hugeValue.after==77,'uint64 count clamps unsigned');print('huge '+hex(hugeInput));
 final bad=w.TableWireReport();final prefix=w.rootWireLoad(forged(3,[4]),bad);check(bad.malformed&&prefix.nums.join(',')=='4,0,0'&&prefix.after==77,'damaged prefix and parent continuation');
 final mismatch=w.TableWireReport();final unchanged=w.rootWireLoad(forged(0,[],elem:1,duplicate:true),mismatch);check(mismatch.kindMismatch==1&&unchanged.nums.join(',')=='1,2,3'&&unchanged.after==77,'mismatch leaves last valid value');
 var refused=false;try{w.rootWireSave(w.Root()..nums=[1]);}catch(_){refused=true;}check(refused,'exact length validation');
}
`

func TestDartFixedArrayWireDefaultExpressions(t *testing.T) {
	u := unitFrom(t, `package probe
enum Grade { Bronze, Silver }
table Item { value int32 }
table Root { truth bool = true
 number int32 = 7
 mask bits(3) = 5
 fraction float64 = 1.5
 text string(8) = "hello"
 raw bytes(8)
 grade Grade = Silver
 child Item
 link *Item }
`)
	g := &wireGen{unit: u}
	wants := []string{"true", "7", "5", "1.5", `"hello"`, "''", "2", "Item()", "null"}
	for i, f := range u.Tables["Root"].Fields {
		if got := g.wireElementDefault(f); got != wants[i] {
			t.Fatalf("%s default %s, want %s", f.Name, got, wants[i])
		}
		copy := *f
		copy.Array = ir.ArrayFixed
		copy.ArrayBound = 2
		if !wirePositionalArray(&copy) {
			t.Fatalf("fixed %s must be positional", f.Name)
		}
		copy.KeyEnum = "Grade"
		if wirePositionalArray(&copy) {
			t.Fatalf("keyed %s must not be positional", f.Name)
		}
		copy.KeyEnum = ""
		copy.Array = ir.ArrayCounted
		if !wirePositionalArray(&copy) {
			t.Fatalf("counted %s must be positional", f.Name)
		}
		copy.Array = ir.ArrayNone
		if wirePositionalArray(&copy) {
			t.Fatalf("scalar %s must not be positional", f.Name)
		}
	}
}

func fixedArrayExpectedClamps(key string) int {
	if key == "huge" {
		return 1
	}
	return 0
}
