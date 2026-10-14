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

// Sharing, backward references, a null, a shared blob and a container use
// production schema cook's bytes as their independent oracle (§7.6).
func TestCookWriteGraphDart(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
table Node { value uint32
 next *Node }
table Layer { value uint32 = 17
 values []uint16 }
table Root { head *Node
 alias *Node
 tail *Node
 blob *bytes
 again *bytes
 items []uint16
 layers [..2]Layer
 groups []Layer
 title string(8) }
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
	if !strings.Contains(string(files["ProbeCook.dart"]), "_cookPlan(") {
		t.Fatal("variable cook measure/write surface is missing (SPEC-TABLES §7.6)")
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
		if err := os.WriteFile(filepath.Join(dir, "cook_graph_probe.dart"), []byte(cookGraphProbe), 0600); err != nil {
			t.Fatal(err)
		}
		analyze := exec.Command(dartBinary(t), "analyze", "cook_graph_probe.dart")
		analyze.Dir = dir
		if output, err := analyze.CombinedOutput(); err != nil {
			t.Fatalf("graph cook analysis: %v\n%s", err, output)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "cook_graph_probe.dart")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("graph cook probe: %v\n%s", err, out)
		}
		model := tabletext.NewModel(u)
		root := model.New(u.Tables["Root"])
		child := model.New(u.Tables["Node"])
		child.Fields[0].Cell.U = 9
		head := model.New(u.Tables["Node"])
		head.Fields[0].Cell.U = 7
		head.Fields[1].Cell.Node = child
		tail := model.New(u.Tables["Node"])
		tail.Fields[0].Cell.U = 11
		tail.Fields[1].Cell.Node = child
		root.Fields[0].Cell.Node = head
		root.Fields[1].Cell.Node = child
		root.Fields[2].Cell.Node = tail
		blob := &tabletext.Blob{Data: []byte{1, 2, 255}}
		root.Fields[3].Cell.Blob = blob
		root.Fields[4].Cell.Blob = blob
		root.Fields[5].Count = 3
		root.Fields[5].Elems = []tabletext.Cell{{U: 3}, {U: 8}, {U: 65535}}
		root.Fields[6].Count = 1
		root.Fields[6].Elems[0].Tab.Fields[0].Cell.U = 4
		root.Fields[6].Elems[0].Tab.Fields[1].Count = 2
		root.Fields[6].Elems[0].Tab.Fields[1].Elems = []tabletext.Cell{{U: 2}, {U: 5}}
		group := model.New(u.Tables["Layer"])
		group.Fields[0].Cell.U = 6
		group.Fields[1].Count = 1
		group.Fields[1].Elems = []tabletext.Cell{{U: 9}}
		root.Fields[7].Count = 1
		root.Fields[7].Elems = []tabletext.Cell{{Tab: group}}
		root.Fields[8].Cell.Str = []byte("hi")
		chainRoot := model.New(u.Tables["Root"])
		var chain *tabletext.Instance
		for i := 259; i >= 0; i-- {
			node := model.New(u.Tables["Node"])
			node.Fields[0].Cell.U = uint64(i)
			node.Fields[1].Cell.Node = chain
			chain = node
		}
		chainRoot.Fields[0].Cell.Node = chain
		for _, item := range []struct {
			name  string
			value *tabletext.Instance
		}{{"cook", root}, {"empty", model.New(u.Tables["Root"])}, {"chain", chainRoot}} {
			for _, big := range []bool{false, true} {
				key := item.name + "-le"
				if big {
					key = item.name + "-be"
				}
				want, err := tablecook.Cook(model, item.value, tablecook.Options{Big: big})
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
		}
	})
}

const cookGraphProbe = `import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
void check(bool ok,String message){if(!ok)throw StateError(message);}
String hex(Uint8List b,int n)=>b.take(n).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final child=w.Node()..value=9;
 final head=w.Node()..value=7..next=child;
 final tail=w.Node()..value=11..next=child;
 final blob=Uint8List.fromList([1,2,255]);
 final layer=w.Layer()..value=4..values=[2,5];
 final group=w.Layer()..value=6..values=[9];
 final value=w.Root()..head=head..alias=child..tail=tail..blob=blob..again=blob..items=[3,8,65535]..layers=[layer]..groups=[group]..title='hi';
 final size=c.RootCook.cookMeasure(value);
 check(size>0,'valid measure refused');
 for(final order in [Endian.little,Endian.big]){
  final out=Uint8List(size+4)..fillRange(0,size+4,0xa5);
  final report=c.TableCookWriteReport();
  check(!c.RootCook.cook(value,Uint8List.sublistView(out,0,size-1),order,report:report),'short output accepted');
  check(report.reason=='invalid_value'&&out.every((x)=>x==0xa5),'short output changed');
  child.next=head;
  check(c.RootCook.cookMeasure(value)==-1,'cycle measured');
  check(!c.RootCook.cook(value,out,order,report:report),'cycle written');
  check(report.reason=='data_cycle'&&out.every((x)=>x==0xa5),'cycle refusal changed output');
  child.next=null;
  check(c.RootCook.cook(value,out,order,report:report),'graph refused');
  check(report.reason=='ok'&&out.skip(size).every((x)=>x==0xa5),'write exceeded measure');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(out,size)}');
 }
 w.Node? chain;
 for(var i=259;i>=0;i--){chain=w.Node()..value=i..next=chain;}
 final chainRoot=w.Root()..head=chain;
 for(final item in [('empty',w.Root()),('chain',chainRoot)]){
  final n=c.RootCook.cookMeasure(item.$2);
  check(n>=0,'extra fixture measure refused');
  for(final order in [Endian.little,Endian.big]){
   final out=Uint8List(n);
   check(c.RootCook.cook(item.$2,out,order),'extra fixture cook refused');
   stdout.writeln('${item.$1}-${order==Endian.little?'le':'be'} ${hex(out,n)}');
  }
 }
}
`
