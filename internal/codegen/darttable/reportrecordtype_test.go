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

func TestWireReportSceneRecordTypeDart(t *testing.T) {
	t.Parallel()

	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "pointers", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/pointers: %v", err)
	}
	u := loadUnit(t, schemas...)
	root := u.Tables["Scene"]
	if root == nil {
		t.Fatal("unit has no root table Scene")
	}
	data, err := hex.DecodeString(sceneRecordTypeMutant)
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
	if goReport.KindMismatch != 0 {
		t.Fatalf("oracle kind_mismatch = %d, want 0 on the record-type mutant: %+v", goReport.KindMismatch, goReport)
	}
	goSaved, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle save: %v", err)
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, okw := files["GraphdemoWire.dart"]
	if !okw || len(wire) == 0 {
		t.Fatal("Generate emitted no GraphdemoWire.dart")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "GraphdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'GraphdemoWire.dart';

void main(List<String> args) {
  final data = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final v = sceneWireLoad(data, report);
  stdout.writeln(
      'unknown=${report.unknown} kind=${report.kindMismatch} '
      'widened=${report.widened} clamped=${report.clamped} '
      'malformed=${report.malformed} refused=${report.refused}');
  final out = sceneWireSave(v);
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
