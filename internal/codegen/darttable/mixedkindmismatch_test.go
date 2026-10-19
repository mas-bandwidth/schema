package darttable

// THE ROOT BODY'S KIND BYTE AT A LIST FIELD (docs/SPEC-TABLES.md §3, §4, §6.2).
//
// The wire fuzzer's kind-swap pass on listdemo.Mixed (run seed 24845619678,
// kind pass #0) wrote kind `0` at the field id of the `grades` list, where the
// declaration says kind `14`. The Go reference (internal/tablewire/decode.go,
// bodyAt) resolves the id, counts one `kind_mismatch`, fails its skip on the
// unskippable kind, and reports `malformed`. The generated Dart reader's open
// pass treated the same unskippable kind as file-wide damage and returned
// before the value pass, so it reported the damage but counted no mismatch.
// §4's event belongs to the body that carries it: the value pass counts it,
// and open must only find the root terminator.

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

// The mutant the harness printed for listdemo.Mixed, run seed 24845619678,
// kind pass #0: the first field's kind byte is 0 instead of 14.
const mixedKindPass0Hex = "010100051e03020304050e12090203000000000000000400000000000000060e1a0f03070d0d080401000000090402000000000a040407000000000b0e0e040300000000320000006400000000c599f782764e0ad9ec2202864cfc63af522602864cfe63af052802864cff63afa82e867084edf24abbf00c9bccfb2d73b145c34435abfe73071702864cf563af541502864cf463afc07fb38abe08637f68b7f0ae4c0cf6520b00000000000000"

func TestWireMixedKindMismatchCountDart(t *testing.T) {
	t.Parallel()

	mutant, err := hex.DecodeString(mixedKindPass0Hex)
	if err != nil {
		t.Fatalf("the embedded mutant is not hex: %v", err)
	}

	u := buildUnit(t, listdemoMixedSchema)
	root := u.Tables["Mixed"]
	if root == nil {
		t.Fatal("the listdemo unit has no Mixed root")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files["ListdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no ListdemoWire.dart")
	}

	// THE GO ORACLE: the wire fuzzer's own read and measure (internal/tablewire).
	model := tabletext.NewModel(u)
	def := model.Lookup("Mixed")
	if def == nil {
		t.Fatal("the listdemo unit declares no Mixed")
	}
	measured, whole, refused := tablewire.FileRegionMeasure(u, def, mutant)
	if refused || !whole {
		t.Fatalf("the oracle's measure is not exact: refused=%v whole=%v", refused, whole)
	}
	inst := model.New(def)
	var report tabletext.Report
	if _, err := tablewire.DecodeRegion(model, inst, mutant, &report); err != nil {
		t.Fatalf("the oracle refused the replay mutant: %v", err)
	}
	encoded, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("the oracle could not save its read: %v", err)
	}
	if report.KindMismatch != 1 || !report.Malformed {
		t.Fatalf("the oracle's answer moved: %+v", report)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ListdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), mutant, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'ListdemoWire.dart';

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main(List<String> args) {
  final data = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final measure = mixedWireMeasure(data);
  final v = mixedWireLoad(data, report);
  final saved = mixedWireSave(v);
  stdout.writeln('${report.unknown},${report.kindMismatch},'
      '${report.widened},${report.clamped},${report.malformed},'
      '${report.refused},$measure,${saved.length},${hex(saved)}');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", "mutant.bin")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart read of the replay mutant: %v\n%s", err, out)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(fields) != 9 {
		t.Fatalf("the probe did not answer nine fields: %q", out)
	}
	num := func(i int) int {
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			t.Fatalf("probe field %d is not a number: %q", i, fields[i])
		}
		return n
	}
	leg := struct {
		unknown, kindMismatch, widened, clamped int
		malformed, refused                      bool
		measure, savedLen                       int
		saved                                   string
	}{
		unknown:      num(0),
		kindMismatch: num(1),
		widened:      num(2),
		clamped:      num(3),
		malformed:    fields[4] == "true",
		refused:      fields[5] == "true",
		measure:      num(6),
		savedLen:     num(7),
		saved:        fields[8],
	}

	// FIELD BY FIELD against the oracle, not against a remembered answer.
	if leg.unknown != report.Unknown {
		t.Fatalf("unknown: dart %d, oracle %d", leg.unknown, report.Unknown)
	}
	if leg.kindMismatch != report.KindMismatch {
		t.Fatalf("kindMismatch: dart %d, oracle %d", leg.kindMismatch, report.KindMismatch)
	}
	if leg.widened != report.Widened {
		t.Fatalf("widened: dart %d, oracle %d", leg.widened, report.Widened)
	}
	if leg.clamped != report.Clamped {
		t.Fatalf("clamped: dart %d, oracle %d", leg.clamped, report.Clamped)
	}
	if leg.malformed != report.Malformed {
		t.Fatalf("malformed: dart %v, oracle %v", leg.malformed, report.Malformed)
	}
	if leg.refused != report.Refused {
		t.Fatalf("refused: dart %v, oracle %v", leg.refused, report.Refused)
	}
	if leg.measure != int(measured) {
		t.Fatalf("measure: dart %d, oracle %d", leg.measure, measured)
	}
	if leg.savedLen != len(encoded) {
		t.Fatalf("saved length: dart %d, oracle %d", leg.savedLen, len(encoded))
	}
	if leg.saved != hex.EncodeToString(encoded) {
		t.Fatalf("saved bytes differ:\n dart   %s\n oracle %s", leg.saved, hex.EncodeToString(encoded))
	}
}
