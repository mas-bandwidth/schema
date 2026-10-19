package darttable

// TestWireReportSceneRecordTypeDart pins the Dart load report against the Go
// reference (internal/tablewire/decode.go, decodenodes.go resolveCell) on a
// graphdemo.Scene file whose node record carries a type id this root cannot
// place (docs/SPEC-TABLES.md §3.1, §4): the record counts one unknown at the
// node table, and every pointer naming it reads null WITHOUT a kind mismatch,
// because a record the reader cannot name is not a record of another type.
// Before this card the Dart reader counted both unknown and kind_mismatch, so
// the fuzzer's record-type pass on graphdemo.Scene diverged from the oracle
// while both saved the same bytes.
//
// TestWireReportCatalogBlobRecordTypeDart pins the other side of that rule: a
// STRING or BYTES blob record is a node the reader placed (§2.5), not an
// unknown, so a *Table pointer naming one counts one kind_mismatch, as
// resolveCell does for a node whose Blob is set. The first fix of this card
// silenced it, and the cold read caught the regression on blobdemo.Catalog.

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// sceneRecordTypeMutant is the fuzzer's record-type pass #1 mutant (seed
// 24845619678): the 232-byte graphdemo.Scene file whose node record's type id
// was rewritten to a type no pointer below Scene targets.
const sceneRecordTypeMutant = "01010c0474726565020402000000031102041104051107060c4d060f0e080401000000010c0161091103000707080402000000000a0d0b0c03746f700c11050d1106000a080b0c046c656674000a090b0c057269676874000e0d0f04030000000b0c036d69640000861b638ebaadbcc437ea08982cc662bb030c9a5fcc128f0a95b31e51efb8255be84db4487b6d5feeffffffffffffffffa99fa6a599c80ef6ea0ce83094fde47c28f025a0ba6c31e51d434c78a3907eb93d62cb8fecfcf739b01c04a2ad70b02405d8145753aaaa76c8c204b4a28a8b9d66fe00649160807a0f00000000000000"

// catalogBlobRecordTypeMutant is the regression mutant the cold read
// (dart-fix-scene-record-type-kind-mismatch r1) measured: corpus seed
// blob_small, record-type pass index #2, run seed 24845619678, a 191-byte
// blobdemo.Catalog file whose *Table pointer names a blob record. The oracle
// reports 0,1,0,0,0,false,read.
const catalogBlobRecordTypeMutant = "01010c03617274021106031106041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000"

func TestWireReportSceneRecordTypeDart(t *testing.T) {
	t.Parallel()
	checkRecordTypeMutantDart(t, "pointers", "Scene", "Graphdemo", "scene", sceneRecordTypeMutant, 0)
}

func TestWireReportCatalogBlobRecordTypeDart(t *testing.T) {
	t.Parallel()
	checkRecordTypeMutantDart(t, "blobs", "Catalog", "Blobdemo", "catalog", catalogBlobRecordTypeMutant, 1)
}

// checkRecordTypeMutantDart loads a record-type mutant with the Go reference
// (internal/tablewire Decode, then Encode) and with the generated Dart reader,
// and requires the same report counters and the same saved bytes. wantKind
// pins the oracle's own kind_mismatch, so the vector cannot drift silently.
func checkRecordTypeMutantDart(t *testing.T, tablesDir, rootName, home, fn, mutantHex string, wantKind int) {
	t.Helper()
	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", tablesDir, "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/%s: %v", tablesDir, err)
	}
	u := loadUnit(t, schemas...)
	root := u.Tables[rootName]
	if root == nil {
		t.Fatalf("unit has no root table %s", rootName)
	}
	data, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatalf("decode the mutant: %v", err)
	}

	// The oracle: the Go reference's read report and save of the same bytes.
	m := tabletext.NewModel(u)
	inst := m.New(root)
	var goReport tabletext.Report
	ok, err := tablewire.Decode(m, inst, data, &goReport)
	if err != nil {
		t.Fatalf("oracle refused the mutant: %v", err)
	}
	if !ok {
		t.Fatalf("oracle could not read the mutant: %+v", goReport)
	}
	if goReport.KindMismatch != wantKind {
		t.Fatalf("oracle kind_mismatch = %d, want %d on the record-type mutant: %+v", goReport.KindMismatch, wantKind, goReport)
	}
	goSaved, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle save: %v", err)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wireName := home + "Wire.dart"
	wire, okw := files[wireName]
	if !okw || len(wire) == 0 {
		t.Fatalf("Generate emitted no %s", wireName)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wireName), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import '` + wireName + `';

void main(List<String> args) {
  final data = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final v = ` + fn + `WireLoad(data, report);
  stdout.writeln(
      'unknown=${report.unknown} kind=${report.kindMismatch} '
      'widened=${report.widened} clamped=${report.clamped} '
      'malformed=${report.malformed} refused=${report.refused}');
  final out = ` + fn + `WireSave(v);
  stdout.writeln('saved=${out.length}');
  File(args[1]).writeAsBytesSync(out);
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "mutant.wire")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "dart.wire")
	cmd := exec.Command(dartBinary(t), "probe.dart", input, output)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart reader over the mutant: %v\n%s", err, out)
	}
	got := string(out)

	want := "unknown=" + strconv.Itoa(goReport.Unknown) + " kind=" + strconv.Itoa(goReport.KindMismatch) +
		" widened=" + strconv.Itoa(goReport.Widened) + " clamped=" + strconv.Itoa(goReport.Clamped) +
		" malformed=" + strconv.FormatBool(goReport.Malformed) +
		" refused=" + strconv.FormatBool(goReport.Refused)
	if !strings.Contains(got, want) {
		t.Errorf("Dart report differs from the Go oracle:\nDart: %s\nwant: %s\n%s", strings.TrimSpace(got), want, got)
	}
	if !strings.Contains(got, "saved="+strconv.Itoa(len(goSaved))) {
		t.Errorf("Dart saved bytes differ from the oracle's %d:\n%s", len(goSaved), got)
	}
	dartSaved, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(dartSaved) != string(goSaved) {
		t.Errorf("Dart save differs from the oracle: got %d bytes, want %d bytes", len(dartSaved), len(goSaved))
	}
}

// catalogBlobSlotTableMutant is the same run's index pass #12 mutant (191
// bytes, blob_small): a blob slot of blobdemo.Catalog names a table record.
// resolveCell counts one kind_mismatch for a *bytes or *string slot naming a
// node whose Blob is nil (docs/SPEC-TABLES.md §3.1); the Dart blob read
// counted none, the mirror of the *Table case above.
const catalogBlobSlotTableMutant = "01010c03617274021102031102041107050c43060618010c05627269636b0708070000000811030911040a1105000b030102030c036361700608010c047461696c000b05deadbeef7f0c0b68656c6c6f20626c6f627300861b638ebaadbcc4030c9a5fcc128f0a03b2f40f72193b61dd7c58d1bafbf83bfffffffffffffffffb645ffecc6d4e8e43326721d7969cef054aa33067555b85a5024c86eb28aa5d28f025a0ba6c31e5e44f1c4f47c02e2f58fcaffad8e04b700c00000000000000"

func TestWireReportCatalogBlobSlotTableDart(t *testing.T) {
	t.Parallel()
	checkRecordTypeMutantDart(t, "blobs", "Catalog", "Blobdemo", "catalog", catalogBlobSlotTableMutant, 1)
}
