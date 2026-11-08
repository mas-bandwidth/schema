package darttable

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestDartDeclaredFormsNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	for mask := range 8 {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			forms := "wire"
			for bit, name := range []string{"json", "block", "cook"} {
				if mask&(1<<bit) != 0 {
					forms += "," + name
				}
			}
			u := unitFrom(t, fmt.Sprintf("package probe\nfixed table Root | forms = %q { value int32 }\n", forms))
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packetFiles(t, u))
			wireSource := string(files["ProbeWire.dart"])
			for _, symbol := range []string{"rootFromJson(", "rootToJson("} {
				if strings.Contains(wireSource, symbol) != (mask&1 != 0) {
					t.Fatalf("mask %d JSON surface %s", mask, symbol)
				}
			}
			for bit, name := range map[int]string{2: "ProbeBlock.dart", 4: "ProbeCook.dart"} {
				if (len(files[name]) > 0) != (mask&bit != 0) {
					t.Fatalf("mask %d file %s", mask, name)
				}
			}
			m := tabletext.NewModel(u)
			v := m.New(u.Tables["Root"])
			if !m.Read(v, []byte(`{"value":7}`), &tabletext.Report{}) {
				t.Fatal("neutral read")
			}
			pin, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			b.WriteString("import 'dart:typed_data';\nimport 'dart:mirrors';\nimport 'ProbeWire.dart' as w;\nimport 'ProbeFixed.dart' as f;\n")
			if mask&2 != 0 {
				b.WriteString("import 'ProbeBlock.dart' as b;\n")
			}
			if mask&4 != 0 {
				b.WriteString("import 'ProbeCook.dart' as c;\n")
			}
			b.WriteString("void check(bool b,String s){if(!b)throw StateError(s);}\nString hex(List<int>b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid main(){\n")
			fmt.Fprintf(&b, `final lib=currentMirrorSystem().libraries.values.singleWhere((l)=>l.uri.path.endsWith('/ProbeWire.dart'));
check(lib.declarations.containsKey(const Symbol('rootFromJson'))==%t,'actual JSON surface');
final v=w.Root()..value=7;final wire=w.rootWireSave(v);check(hex(wire)==%q,'neutral wire pin');
final report=w.TableWireReport();check(w.rootWireLoad(wire,report).value==7&&!report.refused,'wire read');
final fv=f.Root()..value=31;final fixed=Uint8List(f.rootFixedMeasure(1));check(f.rootFixedSave([fv],1,fixed)==fixed.length,'fixed save regardless of forms');
final fd=f.Root();final fr=f.TableFixedReport();check(f.rootFixedLoad([fd],1,fixed,fixed.length,f.rootFixedNewPlan(),fr)==1&&fd.value==31,'fixed read');
final vocab=f.TableVocabulary(entries:List.generate(4096,(i)=>f.TableMessageEntry(0,0)));check(f.announceRead(vocab,f.tableAnnouncement,f.TableReport()),'announcement');
final message=Uint8List(f.rootMeasureMessages([fv],f.TableReport()));check(f.rootSaveMessages([fv],message,f.TableReport())==message.length,'message save regardless of forms');
final md=f.Root();check(f.rootLoadMessages([md],message,vocab,f.TableReport())==1&&md.value==31,'message read');
`, mask&1 != 0, hex.EncodeToString(pin))
			if mask&1 != 0 {
				b.WriteString(`final json=w.rootFromJson('{"value":17}');check(json.value==17&&w.rootFromJson(w.rootToJson(json)).value==17,'JSON active');
var refused=false;try{w.rootFromJson('{"value":');}on FormatException{refused=true;}check(refused,'malformed JSON refuses');
`)
			}
			if mask&2 != 0 {
				offset := ir.Blocks(u).Block("Root").Projection.FieldByName("value").Offset
				fmt.Fprintf(&b, `final blockBytes=Uint8List(b.RootBlock.blockMaxBytes);final block=b.RootBlock.begin(blockBytes,0)!;
ByteData.sublistView(blockBytes).setInt32(%d,23,Endian.host);final opened=b.RootBlock.open(blockBytes,0,block.length)!;check(opened.value==23,'block active');
blockBytes[0]^=1;check(b.RootBlock.open(blockBytes,0,block.length)==null,'damaged block refuses');
`, offset)
			}
			if mask&4 != 0 {
				b.WriteString(`final cv=w.Root()..value=19;
for(final order in [Endian.little,Endian.big]){
 final out=Uint8List(c.RootCookWire.cookMeasure(cv));check(c.RootCookWire.cook(cv,out,order),'cook active');
 if(order==Endian.host){final opened=c.RootCook.open(out,0,out.length)!;check(opened.root(opened.cursor()).value==19,'cook read');}else{check(c.RootCook.openWithReason(out,0,out.length).$2.name=='foreignOrder','foreign order named refusal');}
 out[0]^=1;check(c.RootCook.open(out,0,out.length)==null,'damaged cook refuses');
}
`)
			}
			if mask&4 != 0 {
				cv := m.New(u.Tables["Root"])
				if !m.Read(cv, []byte(`{"value":19}`), &tabletext.Report{}) {
					t.Fatal("neutral cook input")
				}
				for _, big := range []bool{false, true} {
					pin, err := tablecook.Cook(m, cv, tablecook.Options{Big: big})
					if err != nil {
						t.Fatal(err)
					}
					order := "Endian.little"
					if big {
						order = "Endian.big"
					}
					fmt.Fprintf(&b, "{final v=w.Root()..value=19;final out=Uint8List(c.RootCookWire.cookMeasure(v));check(c.RootCookWire.cook(v,out,%s)&&hex(out)==%q,'independent cook pin');}\n", order, hex.EncodeToString(pin))
				}
			}
			b.WriteString("}\n")
			runFormsProbe(t, files, b.String())
		})
	}
}

func runFormsProbe(t *testing.T, files map[string][]byte, source string) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--enable-asserts", "probe.dart"}, {"probe.dart"}} {
		cmd := exec.Command(dartBinary(t), args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("forms %v: %v\n%s", args, err, out)
		}
	}
}

func TestDartDeclaredFormsMixedClosure(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
 table Child | forms = "wire" { value int32 }
 table Root | forms = "wire,json,cook" { child Child
  entries map[uint8]Child }
 fixed table FixedChild | forms = "wire" { value int32 }
 fixed table BlockRoot | forms = "wire,block" { child FixedChild }
 fixed table FreeRow { value int32 }
 `)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	for _, pair := range [][2]string{{"ProbeWire.dart", "childFromJson("}, {"ProbeWire.dart", "childToJson("}, {"ProbeCook.dart", "class ChildCook "}, {"ProbeCook.dart", "class ChildCookWire "}, {"ProbeCook.dart", "class FixedChildCook "}, {"ProbeBlock.dart", "class FixedChildBlock "}} {
		if strings.Contains(string(files[pair[0]]), pair[1]) {
			t.Fatalf("excluded root emitted: %v", pair)
		}
	}
	for _, pair := range [][2]string{{"ProbeWire.dart", "freeRowFromJson("}, {"ProbeCook.dart", "class FreeRowCook "}, {"ProbeBlock.dart", "class FreeRowBlock "}} {
		if !strings.Contains(string(files[pair[0]]), pair[1]) {
			t.Fatalf("default forms missing: %v", pair)
		}
	}
	runFormsProbe(t, files, `import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
import 'ProbeBlock.dart' as b;
void check(bool ok,String name){if(!ok)throw StateError(name);}
void main(){
 final v=w.rootFromJson('{"child":{"value":41},"entries":{"2":{"value":43}}}');
 check(v.child.value==41&&w.rootFromJson(w.rootToJson(v)).child.value==41,'nested wire-only child JSON');
 final wire=w.rootWireSave(v);final r=w.TableWireReport();check(w.rootWireLoad(wire,r).child.value==41&&!r.refused,'nested wire');
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(c.RootCook.cookMeasure(v));check(c.RootCook.cook(v,bytes,order),'nested child cook');
  check((c.RootCook.open(bytes,0,bytes.length)!=null)==(order==Endian.host),'nested cook open / foreign order');
 }
 final storage=Uint8List(b.BlockRootBlock.blockMaxBytes);final block=b.BlockRootBlock.begin(storage,0)!;
 check(b.BlockRootBlock.open(storage,0,block.length)!=null,'nested wire-only child block storage');
 check(w.freeRowFromJson('{"value":47}').value==47,'undeclared default JSON');
}
`)
}
