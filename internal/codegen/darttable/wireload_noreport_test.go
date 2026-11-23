package darttable

// A WIRE LOAD THAT PASSES NO REPORT GETS ITS REFUSAL AS A THROW (docs/SPEC-TABLES.md
// §4, §6.5; docs/silent-failure-audit-1122.md SF-16). <root>WireLoad(data) with no
// report used to hand back the type defaults on a refusal and no signal, where
// <root>FromJson throws in the same case. Its lawful twin: a wire the load accepts
// loads with no report and does not throw.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// sceneRefOverflow is the damaged graphdemo.Scene wire of bodydamage_refused_test.go
// (the oracle refuses it).
const sceneRefOverflow = "01010c0474726565020402000000031102ffffffffffffffffff011104051107060c4d06070e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"

func TestWireLoadWithNoReportThrowsOnRefusal(t *testing.T) {
	t.Parallel()
	u := loadDirUnit(t, filepath.Join("..", "..", "..", "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	home := runtimeHome(u)
	src, found := files[home+"Wire.dart"]
	if !found {
		t.Fatalf("no generated wire library %sWire.dart", home)
	}
	// the emitted shape, held without the SDK: the no-report path throws the reason
	if !strings.Contains(string(src), "if (report == null && read.reason != \x27ok\x27) throw FormatException(read.reason);") {
		t.Fatalf("sceneWireLoad does not throw on a refusal when no report is passed")
	}
	slowtest.Gate(t, "the Dart SDK (it runs the generated Dart)")
	dart := dartBinary(t)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bad, err := hex.DecodeString(sceneRefOverflow)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.bin"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`import "dart:io";
import "dart:typed_data";
import "%[1]sWire.dart";

void main(List<String> args) {
  final bad = Uint8List.fromList(File(args[0]).readAsBytesSync());
  final r = TableWireReport();
  sceneWireLoad(bad, r);
  if (!r.refused) { print("FAIL: the report did not refuse"); exit(1); }
  try {
    sceneWireLoad(bad);
    print("FAIL: a refused load with no report did not throw");
    exit(1);
  } on FormatException catch (e) {
    if (e.message != r.reason) { print("FAIL: thrown ${e.message}, report says ${r.reason}"); exit(1); }
  }
  final good = sceneWireSave(Scene());
  sceneWireLoad(good);
  print("ok");
}
`, home)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dart, "--enable-asserts", "probe.dart", "bad.bin")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("dart probe: %v\n%s", err, out)
	}
}
