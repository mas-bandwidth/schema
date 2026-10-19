package darttable

import (
	"bytes"
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// SPEC-TABLES §7.2, §7.6 over the two storages this card adds to the variable
// cook: a GUARDED REFERENCE and a GUARDED RECORD (a field under a guard, the
// guard's presence bit and the guarded slot, §2). A guard is a fact of the
// WIRE (§2.2, §16.2): it decides what rides, while the region the cook writes
// is the storage, every slot of it, whatever the guard holds — so a guarded
// reference whose target a false guard leaves unnumbered (§3.1) cooks as null,
// and a guarded record cooks as its own storage. Each root is cooked twice,
// once with the guard set and once with it clear, in both byte orders, and
// each result is byte-identical to `schema cook` (tablecook.Cook).
func TestCookWriteGuardedReferenceAndRecordDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, cookGuardedReferenceRecordSchema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if !strings.Contains(string(files["ProbeCook.dart"]), "final class GuardCook {") ||
		strings.Contains(string(files["ProbeCook.dart"]), "// Guard has no ") {
		t.Fatal("Guard has no variable cook writer (SPEC-TABLES §7.6)")
	}
	slowtest.Gate(t, "dart")
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "cook_guarded_ref_probe.dart"), []byte(cookGuardedReferenceRecordProbe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_guarded_ref_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("guarded reference and record cook probe: %v\n%s", err, out)
	}
	model := tabletext.NewModel(u)
	field := func(inst *tabletext.Instance, name string) *tabletext.Field {
		t.Helper()
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == name {
				return &inst.Fields[i]
			}
		}
		t.Fatalf("%s has no field %s", inst.Def.Name, name)
		return nil
	}
	text := func(fv *tabletext.Field, s string) {
		fv.Cell.Str = []byte(s)
		fv.Count = len(s)
	}
	guard := func(active bool) *tabletext.Instance {
		g := model.New(u.Tables["Guard"])
		field(g, "active").Cell.B = active
		det := field(g, "detail").Cell.Tab
		if active {
			child := model.New(u.Tables["Guard"])
			field(child, "active").Cell.B = true
			cd := field(child, "detail").Cell.Tab
			field(cd, "id").Cell.U = 8
			text(field(cd, "tag"), "ch")
			field(g, "link").Cell.Node = child
			field(det, "id").Cell.U = 7
			text(field(det, "tag"), "hi")
		} else {
			field(det, "id").Cell.U = 9
			text(field(det, "tag"), "xx")
			text(field(g, "note"), "zz")
		}
		return g
	}
	tests := []struct {
		name  string
		value *tabletext.Instance
	}{{"guard-on", guard(true)}, {"guard-off", guard(false)}}
	for _, c := range tests {
		for _, big := range []bool{false, true} {
			key := c.name + "-le"
			if big {
				key = c.name + "-be"
			}
			want, err := tablecook.Cook(model, c.value, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			got, err := hex.DecodeString(fieldOf(t, string(out), key))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from schema cook\n got %x\nwant %x", key, got, want)
			}
		}
	}
}

const cookGuardedReferenceRecordSchema = `package probe
table Detail { id int32
  tag string(8) }
table Guard { active bool
  if active { link *Guard
   detail Detail } else { note string(8) } }
`

const cookGuardedReferenceRecordProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void emit(String name,int size,bool Function(Uint8List,Endian,c.TableCookWriteReport) cook){
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  final report=c.TableCookWriteReport();
  check(cook(bytes,order,report),'$name: valid write refused');
  check(report.reason=='ok','$name: successful write kept a refusal');
  check(bytes.skip(size).every((x)=>x==0xa5),'$name: write passed its measured extent');
  stdout.writeln('$name-${order==Endian.little?'le':'be'} ${hex(bytes,size)}');
 }
}
void main(){
 final child=w.Guard()..active=true;
 child.detail.id=8;child.detail.tag='ch';
 final on=w.Guard()..active=true..link=child;
 on.detail.id=7;on.detail.tag='hi';
 final off=w.Guard()..active=false..note='zz';
 off.detail.id=9;off.detail.tag='xx';
 emit('guard-on',c.GuardCook.cookMeasure(on),(b,o,r)=>c.GuardCook.cook(on,b,o,report:r));
 emit('guard-off',c.GuardCook.cookMeasure(off),(b,o,r)=>c.GuardCook.cook(off,b,o,report:r));
}
`
