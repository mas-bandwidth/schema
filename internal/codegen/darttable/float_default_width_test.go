package darttable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Elision compares the stored width, even though authored Dart values are
// doubles. Text and wire reads and both save paths must agree with the oracle.
func TestFloatDefaultWidthDart(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package floatdefault
enum Slot { A, B }
fixed table Child { value float32 = 0.2 }
fixed table Zero { value float32 }
table Root {
 scalar float32 = 0.2
 keyed [Slot]float32
 samples [2]float32
 children [Slot]Child
 rows [2]Child
 child ?Child
}
`)
	m := tabletext.NewModel(u)
	rows := []struct{ name, text, authored string }{
		{"omitted", `{}`, `w.Root()`},
		{"declared-default", `{"scalar":0.2}`, `w.Root()..scalar=0.2`},
		{"rounded-default", `{"scalar":0.2}`, `w.Root()..scalar=0.20000000298023224`},
		{"nested-defaults", `{"children":{"A":{"value":0.2}},"rows":[{"value":0.2},{"value":0.2}],"child":{"value":0.2}}`, `w.Root()..children=[w.Child()..value=0.2,w.Child()]..rows=[w.Child()..value=0.2,w.Child()..value=0.2]..hasChild=true..child=(w.Child()..value=0.2)`},
		{"positive-underflow", `{"keyed":{"A":1e-50},"samples":[1e-50,0]}`, `w.Root()..keyed=[1e-50,0.0]..samples=[1e-50,0.0]`},
		{"negative-zero", `{"scalar":-0.0,"keyed":{"A":-0.0},"samples":[-0.0,0]}`, `w.Root()..scalar=-0.0..keyed=[-0.0,0.0]..samples=[-0.0,0.0]`},
		{"negative-underflow", `{"keyed":{"A":-1e-50},"samples":[-1e-50,0]}`, `w.Root()..keyed=[-1e-50,0.0]..samples=[-1e-50,0.0]`},
		{"nondefault", `{"scalar":0.3,"keyed":{"A":0.3},"samples":[0.3,0.2]}`, `w.Root()..scalar=0.3..keyed=[0.3,0.0]..samples=[0.3,0.2]`},
	}
	var probe strings.Builder
	probe.WriteString(floatDefaultWidthDartProbe)
	wantBits := math.Float64bits(float64(float32(0.2)))
	fmt.Fprintf(&probe, "check(bits(f.Child().value)==%d,'fixed constructor');\n", wantBits)
	fmt.Fprintf(&probe, "check(bits(w.Root().scalar)==%d,'form1 constructor');\n", wantBits)
	wires := map[string][]byte{}
	for _, row := range rows {
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(value, []byte(row.text), &report) || report != (tabletext.Report{}) {
			t.Fatalf("%s oracle text: %+v", row.name, report)
		}
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		wires[row.name] = wire
		fmt.Fprintf(&probe, "row(%q,%q,(%s),unhex(%q));\n", row.name, row.text, row.authored, hex.EncodeToString(wire))
	}
	if !bytes.Equal(wires["omitted"], wires["declared-default"]) || !bytes.Equal(wires["omitted"], wires["positive-underflow"]) {
		t.Fatal("oracle default width elision")
	}
	if bytes.Equal(wires["positive-underflow"], wires["negative-underflow"]) {
		t.Fatal("oracle signed zero")
	}

	// The fixed constructor is also the message-form value. Compare authored
	// doubles with the oracle after storage-width conversion, not native ==.
	for _, row := range []struct{ root, value string }{
		{"Child", "0.2"}, {"Child", "0.20000000298023224"}, {"Child", "-0.0"}, {"Child", "0.3"},
		{"Zero", "0.0"}, {"Zero", "-0.0"}, {"Zero", "1e-50"}, {"Zero", "-1e-50"},
	} {
		value := m.New(m.Lookup(row.root))
		var report tabletext.Report
		if !m.Read(value, []byte("{\"value\":"+row.value+"}"), &report) || report != (tabletext.Report{}) {
			t.Fatalf("message oracle %s/%s: %+v", row.root, row.value, report)
		}
		want, err := tablewire.EncodeMessage(m, value)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&probe, "{ final value=f.%s()..value=%s; final want=unhex(%q); final out=Uint8List(256); final report=f.TableReport(); final n=fm.%sSaveMessages([value],out,report); check(n==want.length && same(out.sublist(0,n<0?0:n),want),'message %s/%s'); }\n", row.root, row.value, hex.EncodeToString(want), lowerFirst(row.root), row.root, row.value)
	}

	probe.WriteString("if(failed) exit(1);\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			files[name] = src
		}
	}
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("float default width: %v\n%s", err, out)
	}
}

const floatDefaultWidthDartProbe = `import 'dart:io';
import 'dart:typed_data';
import 'FloatdefaultWire.dart' as w;
import 'FloatdefaultFixed.dart' as f;
import 'ProbeFixed.dart' as fm;
bool failed=false;
void check(bool ok,String label){if(!ok){stderr.writeln('FAIL $label');failed=true;}}
int bits(double v)=>(ByteData(8)..setFloat64(0,v,Endian.little)).getUint64(0,Endian.little);
Uint8List unhex(String s)=>Uint8List.fromList([for(var i=0;i<s.length;i+=2) int.parse(s.substring(i,i+2),radix:16)]);
bool same(List<int> a,List<int> b)=>a.length==b.length && [for(var i=0;i<a.length;i++) a[i]==b[i]].every((e)=>e);
w.TableRetain store()=>w.TableRetain(4096,List<int>.filled(256,0));
void row(String name,String text,w.Root authored,Uint8List want){
 final jr=w.TableTextReport();
 final parsed=w.rootFromJson(text,jr);
 check(jr.unknown==0 && jr.kindMismatch==0 && jr.clamped==0 && jr.duplicate==0,'$name JSON report');
 final wr=w.TableWireReport();
 final loaded=w.rootWireLoad(want,wr);
 check(!wr.malformed && !wr.refused,'$name wire report');
 final rr=w.TableWireReport();
 final retainedStore=store();
 final retained=w.rootWireLoadRetain(want,retainedStore,rr);
 check(!rr.malformed && !rr.refused,'$name retained wire report');
 for(final pair in [('authored',authored),('JSON',parsed),('wire',loaded),('retained-wire',retained)]){
  check(same(w.rootWireSave(pair.$2),want),'$name ${pair.$1} plain canonical');
  final sr=w.TableWireReport();
  final out=w.rootWireSaveRetain(pair.$2,pair.$1=='retained-wire'?retainedStore:store(),sr);
  check(same(out,want) && !sr.malformed && !sr.refused,'$name ${pair.$1} retained canonical');
 }
}
void main(){
`
