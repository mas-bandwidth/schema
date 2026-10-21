package darttable

// THE NODE TABLE NAMED TWICE (docs/SPEC-TABLES.md §3.1, §3.3, §4).
//
// streamdemo.Feed, run seed 24845619678, form 1: each mutant carries the root
// body's node-table field (the reserved id, kind 12) twice. The Go reference
// (internal/tablewire/nodetable.go nodeTableBytes) replaces the payload it holds
// at every node-table occurrence, so the LAST well-framed occurrence is the
// table and an earlier occurrence's damage does not survive a later good one.
// The Dart scanNodeTable kept a different copy, reporting malformed or a kind
// mismatch where Go read clean. Dart must answer the oracle's report, measure
// and saved bytes byte for byte (§3.1, §3.3, §4).

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

// The three replay mutants the wire fuzzer printed for streamdemo.Feed.
const (
	feedNodeTableRepeated7476  = "01010806000000020f03110102040e0411020302050c13020607070e03060107000607070e0306010825050c13020607070e03060107000607070e030601080000c03a5cb5072eb7082652b3285633d4d809484f69ad9b4bbfc558f9a1a79d510cffffffffffffffff82e051f9dc6843cf054aa33067555b850700000000000000"
	feedNodeTableRepeated10507 = "01010803000000020f030d10040e03060101051102060e0311010400070e051103020004080e0411020004090c26030a11040e03060102051103060e5611020300000a07040e03060103000a07040e0306010900090c26030a11040e03060102051103060e0411020300000a07040e03060103000a07040e030601090000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e54e27b8f1a901c25cc558f9a1a79d510c319a88eb0d256903ffffffffffffffff82e051f9dc6843cf0a00000000000000"
	feedNodeTableRepeated14331 = "01010803000000020f030d0a040e0306010105110200060c1602070a04aa03060102051103000707040e0306010300060c1602070a040e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"
)

func TestWireFeedNodeTableRepeatedDart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		hex  string
	}{
		{name: "7476", hex: feedNodeTableRepeated7476},
		{name: "10507", hex: feedNodeTableRepeated10507},
		{name: "14331", hex: feedNodeTableRepeated14331},
	}

	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "stream", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/stream: %v", err)
	}
	u := loadUnit(t, schemas...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart must carry streamdemo on the variable wire: %v", err)
	}
	wire, ok := files["StreamdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no StreamdemoWire.dart")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "StreamdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';
import 'StreamdemoWire.dart';

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main(List<String> args) {
  final data = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final measure = feedWireMeasure(data);
  final v = feedWireLoad(data, report);
  final saved = feedWireSave(v);
  stdout.writeln('${report.unknown},${report.kindMismatch},'
      '${report.widened},${report.clamped},${report.duplicate},'
      '${report.malformed},${report.refused},$measure,${saved.length},'
      '${hex(saved)}');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Feed")
	if def == nil {
		t.Fatal("the streamdemo unit declares no Feed")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mutant, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatalf("the embedded mutant is not hex: %v", err)
			}

			// THE GO ORACLE: the wire fuzzer's own read and measure.
			measured, whole, refused := tablewire.FileRegionMeasure(u, def, mutant)
			inst := model.New(def)
			var report tabletext.Report
			if _, err := tablewire.DecodeRegion(model, inst, mutant, &report); err != nil {
				t.Fatalf("the oracle refused the replay mutant: %v", err)
			}
			encoded, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("the oracle could not save its read: %v", err)
			}
			if refused || !whole {
				t.Fatalf("the oracle's measure is not exact: refused=%v whole=%v", refused, whole)
			}

			path := filepath.Join(dir, "mutant-"+tc.name+".bin")
			if err := os.WriteFile(path, mutant, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart", filepath.Base(path))
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the generated Dart read of the replay mutant: %v\n%s", err, out)
			}
			fields := strings.Split(strings.TrimSpace(string(out)), ",")
			if len(fields) != 10 {
				t.Fatalf("the probe did not answer ten fields: %q", out)
			}
			num := func(i int) int {
				n, err := strconv.Atoi(fields[i])
				if err != nil {
					t.Fatalf("probe field %d is not a number: %q", i, fields[i])
				}
				return n
			}
			leg := struct {
				unknown, kindMismatch, widened, clamped, duplicate int
				malformed, refused                                 bool
				measure, savedLen                                  int
				saved                                              string
			}{
				unknown:      num(0),
				kindMismatch: num(1),
				widened:      num(2),
				clamped:      num(3),
				duplicate:    num(4),
				malformed:    fields[5] == "true",
				refused:      fields[6] == "true",
				measure:      num(7),
				savedLen:     num(8),
				saved:        fields[9],
			}

			// FIELD BY FIELD against the oracle, not against a remembered answer.
			if leg.unknown != report.Unknown {
				t.Fatalf("%s unknown: dart %d, oracle %d", tc.name, leg.unknown, report.Unknown)
			}
			if leg.kindMismatch != report.KindMismatch {
				t.Fatalf("%s kindMismatch: dart %d, oracle %d", tc.name, leg.kindMismatch, report.KindMismatch)
			}
			if leg.widened != report.Widened {
				t.Fatalf("%s widened: dart %d, oracle %d", tc.name, leg.widened, report.Widened)
			}
			if leg.clamped != report.Clamped {
				t.Fatalf("%s clamped: dart %d, oracle %d", tc.name, leg.clamped, report.Clamped)
			}
			if leg.duplicate != report.Duplicate {
				t.Fatalf("%s duplicate: dart %d, oracle %d", tc.name, leg.duplicate, report.Duplicate)
			}
			if leg.malformed != report.Malformed {
				t.Fatalf("%s malformed: dart %v, oracle %v", tc.name, leg.malformed, report.Malformed)
			}
			if leg.refused != report.Refused {
				t.Fatalf("%s refused: dart %v, oracle %v", tc.name, leg.refused, report.Refused)
			}
			if leg.measure != int(measured) {
				t.Fatalf("%s measure: dart %d, oracle %d", tc.name, leg.measure, measured)
			}
			if leg.savedLen != len(encoded) {
				t.Fatalf("%s saved length: dart %d, oracle %d", tc.name, leg.savedLen, len(encoded))
			}
			if leg.saved != hex.EncodeToString(encoded) {
				t.Fatalf("%s saved bytes differ:\n dart   %s\n oracle %s", tc.name, leg.saved, hex.EncodeToString(encoded))
			}
		})
	}
}
