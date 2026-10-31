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

// SPEC-TABLES §3 carries every FINITE IEEE-754 pattern bit for bit — -0 and
// the subnormals included — and §3.3 refuses a NaN or an infinity in every
// form (Glenn, 2026-10-27, "tables go finite-only too, same rule as packets"):
// the block accessor never returns one and latches non_finite_float naming
// the field (ruling 2), the message read refuses one on its raw bits, and the
// message write refuses one before the quantizer or the bits. -0 under a +0
// default RIDES: elision compares bits.
func TestDartFloat32CellsAreBitTransparent(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\nfixed table Cell { value float32 = 2 }\nfixed table Root { cells [..2]Cell }\nfixed table _float32Cell { n uint8 }\nfixed table ZeroCell { value float32 }\n")
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
		{block, "if ((bits & 0x7f800000) != 0x7f800000) {", "block read must test the raw float32 bits before it widens them"},
		{block, "nonFiniteFloat = 'Cell.value';", "block read must latch non_finite_float naming the field"},
		{message, "final f = value.value;", "message write must narrow the field's own double"},
		{message, "if (!f.isFinite) return false;", "message write must refuse a NaN or an infinity"},
		{message, "if ((b & 0x7f800000) == 0x7f800000) return r.report.RefuseNonFiniteFloat('Cell.value');", "message read must refuse a NaN or an infinity on its raw bits"},
		{message, "if (value.value != 0 || value.value.isNegative) {", "message write must elide a plain float by its bits"},
	} {
		if !strings.Contains(check.source, check.fragment) {
			t.Errorf("%s", check.what)
		}
	}
	for _, gone := range []string{"0x400000", "<< 29", "0x7ff0000000000000;"} {
		if strings.Contains(message, gone) {
			t.Errorf("message form still carries NaN-payload surgery (%q); the finite rule retired it", gone)
		}
	}
	// A schema may declare a leading-underscore type (SPEC §4.6). Helpers
	// belong to an already-claimed runtime class, not that library namespace.
	if strings.Contains(message, "\nfinal Float32List _float32Cell") || !strings.Contains(message, "static final Float32List _float32Cell") {
		t.Error("message float helpers must be members of TableBitWriter")
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
int raw(double value) => (ByteData(4)..setFloat32(0,value,Endian.little)).getUint32(0,Endian.little);
Uint8List body(int bits) {
 final wire=Uint8List(32);
 final writer=m.TableBitWriter(wire);
 writer.put(%[2]d,m.tableMessageRefBits);writer.put(bits,32);writer.put(0,m.tableMessageRefBits);writer.align();
 return Uint8List.sublistView(wire,0,writer.bits>>3);
}
void main() {
 // EVERY FINITE PATTERN rides bit for bit: -0, the smallest and the largest
 // subnormal, and ordinary values, through the block read and the message
 // form both ways. A nonzero default keeps -0 on the wire (§3).
 for(final bits in [0x80000000,0x00000001,0x007fffff,0x3f800000,0xc0490fdb]) {
  final data=ByteData(b.CellRow.rowSize)..setUint32(%[1]d,bits,Endian.little);
  final row=b.CellRow(data,0);
  check(raw(row.value)==bits,'block changed $bits');
  check(row.nonFiniteFloat.isEmpty,'block latched a finite $bits');
  final reader=m.TableMessageReader()..bits=m.TableBitReader(body(bits));
  final value=m.Cell();
  check(m.cellLoadMessageBody(reader,value),'message load refused $bits');
  check(raw(value.value)==bits,'message load changed $bits');
  final saved=Uint8List(32);
  final output=m.TableBitWriter(saved);
  check(m.cellSaveMessageBody(output,value),'message save refused $bits');
  output.align();
  final parsed=m.TableBitReader(Uint8List.sublistView(saved,0,output.bits>>3));
  check(parsed.get(m.tableMessageRefBits)==%[3]d,'message elided $bits');
  check(parsed.get(32)==bits,'message save changed $bits');
  check(parsed.get(m.tableMessageRefBits)==0,'message terminator changed');
 }
 // A NaN OR AN INFINITY IS REFUSED (§3.3): any sign, any payload, quiet or
 // signalling. The block accessor never returns one — it answers 0 and
 // latches non_finite_float naming the field — the message read refuses it on
 // its raw bits, and the message write refuses the value.
 for(final bits in [0x7f800001,0xff800001,0x7fc0dead,0xffc00000,0x7f800000,0xff800000]) {
  final data=ByteData(b.CellRow.rowSize)..setUint32(%[1]d,bits,Endian.little);
  final row=b.CellRow(data,0);
  check(row.value==0.0,'block returned the non-finite $bits');
  check(row.nonFiniteFloat=='Cell.value','block did not latch the non-finite $bits: "${row.nonFiniteFloat}"');
  final reader=m.TableMessageReader()..bits=m.TableBitReader(body(bits));
  check(!m.cellLoadMessageBody(reader,m.Cell()),'message load kept the non-finite $bits');
  check(reader.report.reason=='non_finite_float: Cell.value','message load reason for $bits: "${reader.report.reason}"');
 }
 for(final v in [double.nan,double.infinity,double.negativeInfinity]) {
  final saved=Uint8List(32);
  check(!m.cellSaveMessageBody(m.TableBitWriter(saved),m.Cell()..value=v),'message save wrote $v');
 }
 // -0 IS A VALUE AND IT RIDES (§3.3): elision compares bits, so -0 under the
 // numeric zero default is written, and +0 is not.
 final zero=m.ZeroCell()..value=-0.0;
 final bytes=Uint8List(32);
 final writer=m.TableBitWriter(bytes);
 check(m.zeroCellSaveMessageBody(writer,zero),'default-zero save refused');
 final reader=m.TableBitReader(bytes);
 check(reader.get(m.tableMessageRefBits)!=0,'negative zero must ride against the numeric zero default');
 check(reader.get(32)==0x80000000,'negative zero must ride as its bits');
 final plus=Uint8List(32);
 check(m.zeroCellSaveMessageBody(m.TableBitWriter(plus),m.ZeroCell()),'+0 save refused');
 check(m.TableBitReader(plus).get(m.tableMessageRefBits)==0,'+0 must elide against the numeric zero default');
}
`
