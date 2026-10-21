package darttable

// A REPEATED TEXT FIELD WHOSE LATER COPY IS ILL-FORMED (docs/SPEC-TABLES.md
// §3, §4): when a text field appears twice in a record and the second copy's
// payload is ill-formed, the field reads its declared default, one `malformed`
// counts, and the parent reads on past `L`. The Go oracle
// (internal/tablewire/decode.go, readFields) resets the field to its default
// on that second copy; before this card the Dart emitter kept the earlier,
// well-formed value because its malformed branch broke out of the case without
// assigning the default. blobdemo.Catalog's `name string(32)` is the repeated
// field here, a well-formed first copy then an ill-formed second (a zero byte
// in mutant A, invalid UTF-8 in mutant B). Dart must answer the same value, the
// same report counters and the same saved bytes as internal/tablewire.

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

// Mutant A: Catalog.name appears twice; the second copy is kind 12 with L=1
// and the payload byte 0x00 — a zero byte among the payload, ill-formed.
const repeatedTextIllFormedAHex = "01010c03616263010c010000861b638ebaadbcc40100000000000000"

// Mutant B: Catalog.name appears twice; the second copy is kind 12 with L=2
// and the payload 0xff 0xff — not well-formed UTF-8, ill-formed.
const repeatedTextIllFormedBHex = "01010c03616263010c02ffff00861b638ebaadbcc40100000000000000"

func TestWireRepeatedTextIllFormedResetsDart(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		hex  string
	}{
		{name: "mutant-A-zero-byte", hex: repeatedTextIllFormedAHex},
		{name: "mutant-B-invalid-utf8", hex: repeatedTextIllFormedBHex},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mutant, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatalf("the embedded mutant is not hex: %v", err)
			}

			schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "blobs", "*.schema"))
			if err != nil || len(schemas) == 0 {
				t.Fatalf("no schema files under tables/blobs: %v", err)
			}
			u := loadUnit(t, schemas...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Dart must carry blobdemo on the wire wire: %v", err)
			}
			wire, ok := files["BlobdemoWire.dart"]
			if !ok || len(wire) == 0 {
				t.Fatal("Generate emitted no BlobdemoWire.dart")
			}

			// THE GO ORACLE: the wire fuzzer's own read and measure (internal/tablewire).
			model := tabletext.NewModel(u)
			def := model.Lookup("Catalog")
			if def == nil {
				t.Fatal("the blobdemo unit declares no Catalog")
			}
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
			if !report.Malformed || report.Refused {
				t.Fatalf("the oracle is not malformed on the replay mutant: malformed=%v refused=%v", report.Malformed, report.Refused)
			}
			nameCell := ""
			if nfv, ok := inst.FieldByKey("name"); ok && len(nfv.Cell.Str) > 0 {
				nameCell = string(nfv.Cell.Str)
			}

			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "BlobdemoWire.dart"), wire, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), mutant, 0o600); err != nil {
				t.Fatal(err)
			}
			probe := `import 'dart:io';
import 'BlobdemoWire.dart';

String hex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

void main(List<String> args) {
  final data = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final measure = catalogWireMeasure(data);
  final v = catalogWireLoad(data, report);
  final saved = catalogWireSave(v);
  stdout.writeln('${report.unknown},${report.kindMismatch},'
      '${report.widened},${report.clamped},${report.malformed},'
      '${report.refused},$measure,${saved.length},${hex(saved)},${v.name}');
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
				unknown, kindMismatch, widened, clamped int
				malformed, refused                      bool
				measure, savedLen                       int
				saved                                   string
				name                                    string
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
				name:         fields[9],
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
			if leg.name != nameCell {
				t.Fatalf("name: dart %q, oracle %q", leg.name, nameCell)
			}
		})
	}
}
