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

// SPEC-TABLES §7.6: measure before write, and compare both byte orders with
// schema cook's own production implementation, including nonzero dead defaults.
func TestCookWriteFixedDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
fixed table Row { value uint64 = 7
 flag bool = true }
fixed table Root { stamp uint64 = 5
 rows [..2]Row
 title string(4) = "ok" }
`)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if !strings.Contains(string(files["ProbeCook.dart"]), "static int cookMeasure(") {
		t.Fatal("fixed cook write is missing its measure/write surface (SPEC-TABLES §7.6)")
	}
	t.Run("both-byte-orders", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "cook_fixed_probe.dart"), []byte(cookFixedProbe), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_fixed_probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixed cook probe: %v\n%s", err, out)
		}
		model := tabletext.NewModel(u)
		value := model.New(u.Tables["Root"])
		value.Fields[0].Cell.U = 0x11223344
		value.Fields[1].Count = 1
		value.Fields[1].Elems[0].Tab.Fields[0].Cell.U = 8
		value.Fields[1].Elems[0].Tab.Fields[1].Cell.B = false
		for _, big := range []bool{false, true} {
			key := "cook-le"
			if big {
				key = "cook-be"
			}
			want, err := tablecook.Cook(model, value, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			got, err := hex.DecodeString(fieldOf(t, string(out), key))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s differs from schema cook\n got %x\nwant %x", key, got, want)
			}
		}
	})
}

const cookFixedProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeFixed.dart' as f;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=f.Root();
 value.stamp=0x11223344;
 value.rowsCount=1;
 value.rows[0].value=8;
 value.rows[0].flag=false;
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  final report=c.TableCookWriteReport();
  check(!c.RootCook.cook(value,Uint8List.sublistView(bytes,0,size-1),order,report:report),'short capacity accepted');
  check(report.reason=='invalid_value','short capacity did not name TableRefuseInvalidValue');
  check(bytes.every((x)=>x==0xa5),'short capacity changed output');
  for(final count in [-1,3]){
   value.rowsCount=count;
   check(!c.RootCook.cook(value,bytes,order,report:report),'invalid count accepted');
   check(report.reason=='invalid_value','invalid count lost refusal reason');
   check(bytes.every((x)=>x==0xa5),'invalid count changed output');
  }
  value.rowsCount=1;
  for(final length in [-1,5]){
   value.titleLength=length;
   check(!c.RootCook.cook(value,bytes,order,report:report),'invalid length accepted');
   check(report.reason=='invalid_value','invalid length lost refusal reason');
   check(bytes.every((x)=>x==0xa5),'invalid length changed output');
  }
  value.titleLength=2;
  check(c.RootCook.cook(value,bytes,order,report:report),'valid write refused');
  check(report.reason=='ok','successful write kept a refusal');
  check(bytes.skip(size).every((x)=>x==0xa5),'write passed its measured extent');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`

// SPEC-TABLES §7.6 over the two storages the fixed-root writer used to name as
// a separate slice: a GUARDED root and a TEXT-ARRAY root, each cooked by the
// Dart runtime writer and compared in both byte orders with `schema cook`'s
// own production implementation (internal/tablecook).
//
// A guard is a fact of the WIRE (§2.2, §16.2): it decides what rides, and the
// region the cook writes is the storage, every slot of it, whatever the guard
// holds — so the elided branch's stored values are written, as tablecook's
// region writer and the Go leg's tableCookWriter both write them. Patrol is
// cooked twice, once down each arm, with non-default values parked in the
// branch the guard elides. A guarded field puts its table in the variable
// class (`fixed` refuses a lookback branch, §2.2), so a guarded root is a
// variable root and its writer is emitCookWriteVariable's.
//
// A text array is spelled the one way v1 admits it: an array of a record that
// holds string(N) and bytes(N), since a direct array of string(N), bytes(N) or
// wstring(N) is a compile error ("wrap the element in a type"). Shelf is the
// fixed-class root, Rack the variable-class root with a reference beside it.
func TestCookWriteGuardedAndTextArrayRootsDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, cookGuardedTextArraySchema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for _, root := range []string{"Shelf", "Patrol", "Rack"} {
		if !strings.Contains(string(files["ProbeCook.dart"]), "final class "+root+"Cook {") ||
			strings.Contains(string(files["ProbeCook.dart"]), "// "+root+" has no ") {
			t.Errorf("%s has no cook writer (SPEC-TABLES §7.6)", root)
		}
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
	if err := os.WriteFile(filepath.Join(dir, "cook_guarded_text_probe.dart"), []byte(cookGuardedTextArrayProbe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_guarded_text_probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("guarded and text-array cook probe: %v\n%s", err, out)
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
	shelf := model.New(u.Tables["Shelf"])
	field(shelf, "stamp").Cell.U = 0x55
	items := field(shelf, "items")
	items.Count = 2
	text(field(items.Elems[0].Tab, "label"), "ab")
	text(field(items.Elems[0].Tab, "code"), "\x01\x02\x03")
	text(field(items.Elems[1].Tab, "label"), "wxyz1234")
	patrol := func(active bool) *tabletext.Instance {
		p := model.New(u.Tables["Patrol"])
		field(p, "active").Cell.B = active
		field(p, "has_target").Cell.B = true
		field(p, "target_id").Cell.U = 77
		field(p, "wander").Cell.F = 0.25
		if active {
			field(p, "speed").Cell.F = 2.5
			text(field(p, "note"), "zz")
		} else {
			field(p, "speed").Cell.F = 3.5
			text(field(p, "note"), "hi")
		}
		return p
	}
	rack := model.New(u.Tables["Rack"])
	tags := field(rack, "items")
	tags.Count = 2
	text(field(tags.Elems[0].Tab, "label"), "one")
	text(field(tags.Elems[0].Tab, "code"), "x")
	next := model.New(u.Tables["Rack"])
	field(next, "items").Count = 1
	text(field(field(next, "items").Elems[0].Tab, "label"), "two")
	field(rack, "next").Cell.Node = next
	for _, c := range []struct {
		name  string
		value *tabletext.Instance
	}{{"shelf", shelf}, {"patrol-on", patrol(true)}, {"patrol-off", patrol(false)}, {"rack", rack}} {
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

const cookGuardedTextArraySchema = `package probe
fixed table Item { label string(8)
 code bytes(4) }
fixed table Shelf { items [..3]Item
 stamp uint32 = 9 }
table Patrol { active bool
 if active { speed float32 = 1.0
  has_target bool
  if has_target { target_id int32 = 0 | min = 0, max = 1000 } else { wander float32 = 0.5 } }
 else { note string(8) } }
table Tag { label string(8)
 code bytes(4) }
table Rack { items [..3]Tag
 next *Rack }
`

const cookGuardedTextArrayProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeFixed.dart' as f;
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void text(Uint8List into,String s){for(var i=0;i<s.length;i++){into[i]=s.codeUnitAt(i);}}
void emit(String name,int size,bool Function(Uint8List,Endian,c.TableCookWriteReport) cook){
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  final report=c.TableCookWriteReport();
  check(!cook(Uint8List.sublistView(bytes,0,size-1),order,report),'$name: short capacity accepted');
  check(report.reason=='invalid_value','$name: short capacity named ${report.reason}, not TableRefuseInvalidValue');
  check(bytes.every((x)=>x==0xa5),'$name: short capacity changed output');
  check(cook(bytes,order,report),'$name: valid write refused');
  check(report.reason=='ok','$name: successful write kept a refusal');
  check(bytes.skip(size).every((x)=>x==0xa5),'$name: write passed its measured extent');
  stdout.writeln('$name-${order==Endian.little?'le':'be'} ${hex(bytes,size)}');
 }
}
w.Patrol patrol(bool active){
 final p=w.Patrol()..active=active..hasTarget=true..targetId=77..wander=0.25;
 p.speed=active?2.5:3.5;
 p.note=active?'zz':'hi';
 return p;
}
void main(){
 final shelf=f.Shelf()..stamp=0x55..itemsCount=2;
 text(shelf.items[0].label,'ab');shelf.items[0].labelLength=2;
 text(shelf.items[0].code,'\x01\x02\x03');shelf.items[0].codeLength=3;
 text(shelf.items[1].label,'wxyz1234');shelf.items[1].labelLength=8;
 emit('shelf',c.ShelfCook.cookMeasure(shelf),(b,o,r)=>c.ShelfCook.cook(shelf,b,o,report:r));
 for(final active in [true,false]){
  final p=patrol(active);
  final name=active?'patrol-on':'patrol-off';
  emit(name,c.PatrolCook.cookMeasure(p),(b,o,r)=>c.PatrolCook.cook(p,b,o,report:r));
 }
 final rack=w.Rack()..items=[w.Tag()..label='one'..code='x',w.Tag()];
 rack.next=w.Rack()..items=[w.Tag()..label='two'];
 emit('rack',c.RackCook.cookMeasure(rack),(b,o,r)=>c.RackCook.cook(rack,b,o,report:r));
}
`

// Legal table names cannot collide with the cook writer's float scratch.
func TestCookWriteDartFloatHelpersDoNotClaimTableNames(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
fixed table Float32 { value float32 }
fixed table Float32Bits { value float32 }
`)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	const probe = `import 'dart:typed_data';
import 'ProbeFixed.dart' as f;
import 'ProbeCook.dart' as c;
void main(){
 final value=f.Float32()..value=1.5;
 final bytes=Uint8List(c.Float32Cook.cookMeasure(value));
 if(!c.Float32Cook.cook(value,bytes,Endian.big))throw StateError('float cook refused');
 if(ByteData.sublistView(bytes).getUint32(64,Endian.big)!=0x3fc00000)throw StateError('float payload changed');
 final other=f.Float32Bits()..value=-0.0;
 final out=Uint8List(c.Float32BitsCook.cookMeasure(other));
 if(!c.Float32BitsCook.cook(other,out,Endian.little))throw StateError('float bits cook refused');
 if(ByteData.sublistView(out).getUint32(64,Endian.little)!=0x80000000)throw StateError('signed zero changed');
}
`
	if err := os.WriteFile(filepath.Join(dir, "cook_float_names_probe.dart"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_float_names_probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cook float names: %v\n%s", err, out)
	}
}
