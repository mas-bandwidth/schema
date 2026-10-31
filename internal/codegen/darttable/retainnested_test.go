package darttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRetainedRecordsReturnToTheirOwnBody pins docs/SPEC-TABLES.md §6.6 at
// depth: RT2's wire (testdata/wire/tables/retain_rt2.bin) carries fields RT1
// cannot name inside the root, a nested table by value, array and keyed-array
// elements, a union arm, a map value, a list element and a pointer's node.
// RT1's retaining load keeps them and its retaining save must re-emit each one
// at the end of the body it was read from, byte-equal to the C++ reference's
// pinned save (testdata/wire/tables/retain_rt1_save.bin).
//
// The REVERSED WITNESS plants the old matcher back: a record keyed by the
// field walk alone, which every by-value body shares with its parent, so the
// nested bodies' records pile into the first body that walks that path. The
// same probe over that runtime must NOT reproduce the reference's bytes.
func TestRetainedRecordsReturnToTheirOwnBody(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "RT1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := buildUnit(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should accept RT1: %v", err)
	}
	wire := files["Tblrt1Wire.dart"]
	if len(wire) == 0 {
		t.Fatal("Generate emitted no Tblrt1Wire.dart for RT1")
	}
	input, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "retain_rt2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "retain_rt1_save.bin"))
	if err != nil {
		t.Fatal(err)
	}

	// the wire file imports RT1's enum from the packet file beside it
	packets := packetFiles(t, u)

	got := retainNestedSave(t, packets, string(wire), input)
	if string(got) != string(want) {
		t.Fatalf("RT1's retaining save is %d bytes, the reference's %d: a retained record left its body", len(got), len(want))
	}

	// the witness: the old matcher, a record keyed by its field walk
	const keyed = "    if (!identical(rec.owner, owner)) continue;\n"
	const walked = `    if (rec.path.length != w.path.length) continue;
    var same = true;
    for (var i = 0; i < w.path.length; i++) {
      if (rec.path[i] != w.path[i]) {
        same = false;
        break;
      }
    }
    if (!same) continue;
`
	if strings.Count(string(wire), keyed) != 1 {
		t.Fatal("the witness cannot find the owner match in the generated runtime")
	}
	planted := strings.Replace(string(wire), keyed, walked, 1)
	if old := retainNestedSave(t, packets, planted, input); string(old) == string(want) {
		t.Fatal("NEGATIVE CONTROL FAILED: a record keyed by its field walk reproduced the reference's bytes")
	}
}

// retainNestedSave writes RT1's packet files, one Tblrt1Wire.dart and a probe
// beside them, loads the input with retention at a roomy capacity, and returns
// the retaining save.
func retainNestedSave(t *testing.T, packets map[string][]byte, wire string, input string) []byte {
	t.Helper()
	const probe = `import 'dart:io';
import 'Tblrt1Wire.dart';
void main(List<String> args) {
  final store = TableRetain(1 << 20, List<int>.filled(1 << 17, 0));
  final report = TableWireReport();
  final v = nodeWireLoadRetain(File(args[0]).readAsBytesSync(), store, report);
  if (report.malformed || report.refused || report.retained != 9 || report.retainLost != 2) {
    stderr.writeln('load: ${report.retained} retained, ${report.retainLost} lost');
    exit(1);
  }
  final save = TableWireReport();
  final out = nodeWireSaveRetain(v, store, save);
  if (out.length != nodeWireMeasureRetain(v, store)) {
    stderr.writeln('measure and save disagree');
    exit(1);
  }
  File(args[1]).writeAsBytesSync(out);
}
`
	dir := t.TempDir()
	writeFiles(t, dir, packets)
	if err := os.WriteFile(filepath.Join(dir, "Tblrt1Wire.dart"), []byte(wire), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "save.wire")
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", input, output)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("retain probe: %v\n%s", err, out)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
