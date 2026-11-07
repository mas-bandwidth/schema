package darttable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds a record past 4 GiB (docs/SPEC-TABLES.md §11): every
// leg generates and compiles it, and holds each stored type size, field
// offset and element size at 64 bits. Huge is three 2 GiB parts and a field
// past 4 GiB.
const wideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// wideRecordWire is a Root whose items list holds one empty Huge body: its
// region measure commands one Huge, past 4 GiB.
func wideRecordWire(u *ir.Unit) []byte {
	root := u.Tables["Root"]
	list := []byte{byte(ir.TableKindTable), 1, 1, 0}
	wire := []byte{ir.TableWireForm, 1, byte(ir.TableKindArray)}
	wire = binary.AppendUvarint(wire, uint64(len(list)))
	wire = append(append(wire, list...), 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(root.Fields[0]))
	return binary.LittleEndian.AppendUint64(wire, 1)
}

// TestWideRecordDart: every library the unit generates compiles, and the
// emitted facts of the record past 4 GiB are exact: HugeRow.rowSize is
// Huge's size, HugeRow.tail reads at Huge.tail's offset, and the region
// measure of a Root holding one Huge is the oracle's. No Huge is built: tail
// reads a small view at a base that puts the offset at 0.
func TestWideRecordDart(t *testing.T) {
	slowtest.Gate(t, "dart")
	memceiling.Guard(t, memceiling.Default)
	if out, err := runWideRecordDart(t, nil); err != nil {
		t.Fatalf("wide record probe: %v\n%s", err, out)
	}
}

// TestWideRecordDartNegativeControl narrows Huge's row size to 32 bits in the
// generated cook library and requires the probe to go red.
func TestWideRecordDartNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart")
	memceiling.Guard(t, memceiling.Default)
	const size = "static const int rowSize = 6442450952;"
	out, err := runWideRecordDart(t, func(name, src string) string {
		if !strings.Contains(src, size) {
			return src
		}
		return strings.Replace(src, size, "static const int rowSize = 6442450952 & 0xffffffff;", 1)
	})
	if err == nil || !strings.Contains(string(out), "HugeRow.rowSize") {
		t.Fatalf("a 32-bit Huge size stayed green: %v\n%s", err, out)
	}
}

func runWideRecordDart(t *testing.T, edit func(name, src string) string) ([]byte, error) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(wideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := buildUnit(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range packetFiles(t, u) {
		if !strings.Contains(name, "/") {
			files[name] = b
		}
	}
	huge := ir.RecordLayout(u, u.Tables["Huge"])
	var tail int64 = -1
	for _, f := range huge.Fields {
		if f.Field.Name == "tail" {
			tail = f.Offset
		}
	}
	wire := wideRecordWire(u)
	measure, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["Root"], wire)
	if !whole || refused || huge.Size <= 1<<32 || tail <= 1<<32 || measure <= huge.Size {
		t.Fatalf("the oracle's facts are not past 4 GiB: size %d tail %d measure %d", huge.Size, tail, measure)
	}
	var names []string
	cook, wireLib := "", ""
	for name, b := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".dart") {
			delete(files, name)
			continue
		}
		text := string(b)
		if edit != nil {
			text = edit(name, text)
		}
		files[name] = []byte(text)
		names = append(names, name)
		if strings.Contains(text, "final class HugeRow ") {
			cook = name
		}
		if strings.Contains(text, "(int, String) rootWireMeasureReason(") {
			wireLib = name
		}
	}
	if cook == "" || wireLib == "" {
		t.Fatalf("no generated library defines HugeRow (%q) or rootWireMeasureReason (%q)", cook, wireLib)
	}
	sort.Strings(names)
	var probe strings.Builder
	probe.WriteString("import 'dart:io';\nimport 'dart:typed_data';\n")
	for i, name := range names {
		fmt.Fprintf(&probe, "import '%s' as l%d;\n", name, i)
	}
	fmt.Fprintf(&probe, `import '%s' show HugeRow;
import '%s' show rootWireMeasureReason;
void main() {
  var failures = 0;
  void check(bool ok, String what) { if (!ok) { print('FAIL: ' + what); failures++; } }
  check(HugeRow.rowSize == %d, 'HugeRow.rowSize ${HugeRow.rowSize}');
  final view = ByteData(8)..setInt32(0, 0x12345678, Endian.little);
  check(HugeRow(view, -%d).tail == 0x12345678, 'HugeRow.tail does not read at Huge.tail offset');
  final (size, why) = rootWireMeasureReason(File('wide.bin').readAsBytesSync());
  check(size == %d && why == 'ok', 'measure $size $why');
  if (failures != 0) exit(1);
  print('ok');
}
`, cook, wireLib, huge.Size, tail, measure)
	work := t.TempDir()
	files["probe.dart"] = []byte(probe.String())
	files["wide.bin"] = wire
	writeFiles(t, work, files)
	cmd := exec.Command(dartBinary(t), "--old_gen_heap_size=3072", "--enable-asserts", "probe.dart")
	cmd.Dir = work
	return cmd.CombinedOutput()
}
