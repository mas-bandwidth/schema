package darttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SPEC-TABLES §7 and §19: a malformed trusted accelerator still refuses rather
// than returning neighbouring storage or throwing at the next accessor.
func TestDartBlockAndCookAccessorsRefuseOutOfRange(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nfixed table Row { name string(4) }\nfixed table Root { rows [..2]Row }\ntable Node { next *Node\n value uint64 }\n")
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	block := string(files["ProbeBlock.dart"])
	cook := string(files["ProbeCook.dart"])
	for _, check := range []struct{ source, fragment, what string }{
		{block, "return used < 0 || used > 4 ? 0 : used;", "block string length must refuse values outside its declared capacity"},
		{cook, "int at(int slot, int size)", "cook dereference must take the whole target record size"},
		{cook, "slot - region > length - 8", "cook dereference must bound the slot before reading"},
		{cook, "length - size - relative", "cook dereference must bound the whole target before addition"},
	} {
		if !strings.Contains(check.source, check.fragment) {
			t.Error(check.what)
		}
	}
	if t.Failed() {
		return
	}
	t.Run("dart-refusals", func(t *testing.T) {
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
		row := ir.RecordLayout(u, u.Tables["Row"])
		count := ir.FieldPieces(u, u.Tables["Row"].Fields[0], row.FieldByName("name").Offset)[1].Offset
		source := fmt.Sprintf(accessorBoundsProbe, count)
		if err := os.WriteFile(filepath.Join(dir, "accessor_bounds_probe.dart"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "accessor_bounds_probe.dart")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("accessor refusal probe: %v\n%s", err, out)
		}
	})
}

const accessorBoundsProbe = `import 'dart:typed_data';
import 'ProbeBlock.dart' as b;
import 'ProbeCook.dart' as c;
void check(bool ok,String what){if(!ok)throw StateError(what);}
void main(){
 final bytes=Uint8List(b.RowRow.rowSize+32);
 final view=ByteData.sublistView(bytes);
 final row=b.RowRow(view,0);
 for(final used in [-1,5,0x7fffffff,0,4]){
  view.setInt32(%d,used,Endian.little);
  final expected=used<0||used>4?0:used;
  check(row.nameLength==expected,'length escaped its declared capacity: $used');
  check(row.nameBytes(bytes).length==expected,'bytes escaped their declared capacity: $used');
 }
 // Offset-zero within a backing buffer is different from the region base.
 // Every bound is over the region, even when neighbouring bytes exist.
 final image=Uint8List(128);
 final data=ByteData.sublistView(image);
 final cook=c.NodeCook(image,data,64,32);
 data.setInt64(64,24,Endian.little);
 check(cook.at(64,16)==c.TableCookRef.outside,'a truncated record resolved');
 check(cook.at(64,8)==88,'the record ending exactly at the region end refused');
 for(final slot in [-1,0,63,89,96,0x7fffffffffffffff]){
  check(cook.at(slot,8)==c.TableCookRef.outside,'slot $slot escaped or threw');
 }
 check(cook.at(64,-1)==c.TableCookRef.outside,'negative record size resolved');
 check(cook.at(64,0x7fffffffffffffff)==c.TableCookRef.outside,'oversized record resolved');
 for(final delta in [0x7fffffffffffffff,-0x7fffffffffffffff,-1]){
  data.setInt64(64,delta,Endian.little);
  check(cook.at(64,8)==c.TableCookRef.outside,'delta $delta wrapped into the region');
 }
 data.setInt64(64,0,Endian.little);
 check(cook.at(64,8)==c.TableCookRef.none,'null changed');
 data.setInt64(72,-8,Endian.little);
 check(cook.at(72,16)==64,'valid backwards reference refused');
}
`
