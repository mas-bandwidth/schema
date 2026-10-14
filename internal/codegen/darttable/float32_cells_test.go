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

// SPEC-TABLES §3 preserves every IEEE-754 payload, including signalling NaNs
// and signed zero, through a consumer that receives and sends the same cell.
func TestDartFloat32CellsAreBitTransparent(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nfixed table Cell { value float32 }\nfixed table Root { cells [..2]Cell }\n")
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tableFiles, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tableFiles)
	block := string(files["ProbeBlock.dart"])
	message := string(files["ProbeFixed.dart"])
	for _, check := range []struct{ source, fragment, what string }{
		{block, "_doubleFromFloat32Bits(view.getUint32(", "block read must widen raw float32 bits in software"},
		{message, "w.put(_float32BitsFromDouble(value.value), 32)", "message write must narrow NaN bits in software"},
		{message, "_doubleFromFloat32Bits(r.bits.get(32))", "message read must widen raw float32 bits in software"},
	} {
		if !strings.Contains(check.source, check.fragment) {
			t.Errorf("%s", check.what)
		}
	}
	if t.Failed() {
		return
	}
	t.Run("dart-roundtrip", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		slot := 0
		entry := ir.TableFieldEntry(u.Tables["Cell"].Fields[0])
		for i, v := range ir.TableVocabulary(u) {
			if entry.Key() == v.Key() {
				slot = i + 1
			}
		}
		if slot == 0 {
			t.Fatal("float field has no vocabulary slot")
		}
		source := fmt.Sprintf(float32CellsProbe, ir.RecordLayout(u, u.Tables["Cell"]).FieldByName("value").Offset, slot, slot)
		if err := os.WriteFile(filepath.Join(dir, "float_cells_probe.dart"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(dartBinary(t), "--enable-asserts", "float_cells_probe.dart")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("float32 bit transparency: %v\n%s", err, out)
		}
	})
}

const float32CellsProbe = `import 'dart:typed_data';
import 'ProbeBlock.dart' as b;
import 'ProbeFixed.dart' as m;

void check(bool ok, String what) { if (!ok) throw StateError(what); }
int raw(double value) {
 final d=ByteData(8)..setFloat64(0,value,Endian.little);
 final bits=d.getUint64(0,Endian.little);
 if(value.isNaN) return ((bits >>> 63)<<31) | 0x7f800000 | ((bits >>>29)&0x7fffff);
 return (ByteData(4)..setFloat32(0,value,Endian.little)).getUint32(0,Endian.little);
}
void main() {
 for(final bits in [0x7f800001,0xff800001,0x7fc0dead,0x80000000,0x3f800000]) {
  final data=ByteData(b.CellRow.rowSize)..setUint32(%d,bits,Endian.little);
  final row=b.CellRow(data,0);
  check(raw(row.value)==bits,'block changed $bits');
  // Build the reference body directly from raw bits: the input does not use
  // either conversion under test, including for the negative-zero payload.
  final wire=Uint8List(32);
  final writer=m.TableBitWriter(wire);
  writer.put(%d,m.tableMessageRefBits);writer.put(bits,32);writer.put(0,m.tableMessageRefBits);writer.align();
  final reader=m.TableMessageReader()..bits=m.TableBitReader(Uint8List.sublistView(wire,0,writer.bits>>3));
  final value=m.Cell();
  check(m.cellLoadMessageBody(reader,value),'message load refused $bits');
  check(raw(value.value)==bits,'message load changed $bits');
  final saved=Uint8List(32);
  final output=m.TableBitWriter(saved);
  check(m.cellSaveMessageBody(output,value),'message save refused $bits');
  output.align();
  final parsed=m.TableBitReader(Uint8List.sublistView(saved,0,output.bits>>3));
  check(parsed.get(m.tableMessageRefBits)==%d,'message elided $bits');
  check(parsed.get(32)==bits,'message save changed $bits');
  check(parsed.get(m.tableMessageRefBits)==0,'message terminator changed');
 }
}
`
