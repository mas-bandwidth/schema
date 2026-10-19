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

func TestScratchUnbounded(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Inner { v int32 }
table Root { xs []int32
  ys []Inner }
`
	u := unitFrom(t, schema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	slowtest.Gate(t, "dart")
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	const probe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final r=w.Root()..xs=[1,2,3]..ys=[w.Inner()..v=5,w.Inner()..v=-6];
 for(final order in [Endian.little,Endian.big]){
  final size=c.RootCook.cookMeasure(r);
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  check(c.RootCook.cook(r,bytes,order),'cook refused');
  check(bytes.skip(size).every((x)=>x==0xa5),'wrote past measure');
  stdout.writeln('${order==Endian.little?'le':'be'} ${hex(bytes,size)}');
 }
}
`
	if err := os.WriteFile(filepath.Join(dir, "cook_scratch.dart"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_scratch.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	// xs = [1,2,3]
	xs := fieldOfInstance(value, "xs")
	xs.Count = 3
	xs.Elems = make([]tabletext.Cell, 3)
	xs.Elems[0].U = 1
	xs.Elems[1].U = 2
	xs.Elems[2].U = 3
	ys := fieldOfInstance(value, "ys")
	ys.Count = 2
	ys.Elems = make([]tabletext.Cell, 2)
	ys.Elems[0].Tab = model.New(u.Tables["Inner"])
	ys.Elems[1].Tab = model.New(u.Tables["Inner"])
	ys.Elems[0].Tab.Fields[0].Cell.U = 5
	ys.Elems[1].Tab.Fields[0].Cell.U = uint64(0xFFFFFFFFFFFFFFFA)
	for _, big := range []bool{false, true} {
		key := "le"
		if big {
			key = "be"
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
			t.Errorf("%s differs\n got %x\nwant %x", key, got, want)
		}
	}
}

func fieldOfInstance(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	return nil
}

func TestScratchUnboundedNested(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Inner { v int32 }
table Holder { xs []int32 }
table Root { ys []Inner
  h *Holder }
`
	u := unitFrom(t, schema)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	slowtest.Gate(t, "dart")
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	const probe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final h=w.Holder()..xs=[7,8];
 final r=w.Root()..ys=[w.Inner()..v=5,w.Inner()..v=-6]..h=h;
 for(final order in [Endian.little,Endian.big]){
  final size=c.RootCook.cookMeasure(r);
  final bytes=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  check(c.RootCook.cook(r,bytes,order),'cook refused');
  check(bytes.skip(size).every((x)=>x==0xa5),'wrote past measure');
  stdout.writeln('${order==Endian.little?'le':'be'} ${hex(bytes,size)}');
 }
}
`
	if err := os.WriteFile(filepath.Join(dir, "cook_scratch2.dart"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_scratch2.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	ys := fieldOfInstance(value, "ys")
	ys.Count = 2
	ys.Elems = make([]tabletext.Cell, 2)
	ys.Elems[0].Tab = model.New(u.Tables["Inner"])
	ys.Elems[1].Tab = model.New(u.Tables["Inner"])
	ys.Elems[0].Tab.Fields[0].Cell.U = 5
	ys.Elems[1].Tab.Fields[0].Cell.U = uint64(0xFFFFFFFFFFFFFFFA)
	holder := model.New(u.Tables["Holder"])
	hx := fieldOfInstance(holder, "xs")
	hx.Count = 2
	hx.Elems = make([]tabletext.Cell, 2)
	hx.Elems[0].U = 7
	hx.Elems[1].U = 8
	fieldOfInstance(value, "h").Cell.Node = holder
	for _, big := range []bool{false, true} {
		key := "le"
		if big {
			key = "be"
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
			t.Errorf("%s differs\n got %x\nwant %x", key, got, want)
		}
	}
}
