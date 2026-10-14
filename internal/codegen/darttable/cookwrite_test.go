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
