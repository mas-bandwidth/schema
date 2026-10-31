package darttable

// THE STREAM LIST COUNT PAST ITS BODY ON THE DART LEG (docs/SPEC-TABLES.md §3,
// §4). The representative is the pinned vector stream_count_past_body
// (testdata/wire/tables/fuzz-vectors/INDEX.txt, streamdemo.Feed): a
// `bytes(8)` count spelled wide enough that the count LEB128 runs past the end
// of the body that framed it, a count the body cannot cover. That is damage,
// and DAMAGE DISCARDS THE WHOLE LOAD (§4): the Dart leg owes the refusal,
// malformed, and no counter or saved byte is compared.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestWireFeedCountPastBodyRefusedDart(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Dart SDK (it runs the generated Dart)")

	u := loadUnit(t, filepath.Join("..", "..", "..", "tables", "stream", "Stream.schema"))
	root := u.Tables["Feed"]
	if root == nil {
		t.Fatal("the streamdemo unit has no Feed root")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files["StreamdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no StreamdemoWire.dart")
	}

	// THE MUTANT IS THE PINNED VECTOR, embedded by its receipt bytes, so this
	// test does not depend on a generated file having survived a checkout.
	const mutantHex = "01010803000000020f030d0a040e03068fa99e02de62cf010105110200060c1602070a040e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"
	data, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatalf("decode the pinned vector: %v", err)
	}
	if len(data) != 119 {
		t.Fatalf("the pinned stream_count_past_body vector is %d bytes, want the card's 119", len(data))
	}
	if got, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", "stream_count_past_body.bin")); err == nil {
		if string(got) != string(data) {
			t.Fatalf("the embedded mutant is not the pinned stream_count_past_body vector")
		}
	}

	m := tabletext.NewModel(u)
	var rep tabletext.Report
	if _, err := tablewire.DecodeRegion(m, m.New(root), data, &rep); err != nil || !rep.Malformed {
		t.Fatalf("the Go oracle does not read the pinned vector as damaged: %v %+v", err, rep)
	}
	if gotRep, _ := feedReportProbe(t, wire, data); !gotRep.malformed {
		t.Errorf("Dart report = %s on stream_count_past_body: the damaged load is not refused", gotRep)
	}
}

// dartReport is the six counters every leg prints, in the manifest's order,
// and the refusal.
type dartReport struct {
	unknown      int
	kindMismatch int
	widened      int
	clamped      int
	duplicate    int
	malformed    bool
	refused      bool
}

func (r dartReport) String() string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t,%t", r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused)
}

// feedReportProbe generates the Dart reader, loads the bytes through it with a
// report, saves what it loaded, and answers the six counters and the saved
// bytes. A bare `go test` skips by name when the SDK is missing, through
// dartBinary.
func feedReportProbe(t *testing.T, wire []byte, data []byte) (dartReport, []byte) {
	t.Helper()
	dart := dartBinary(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "StreamdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:io';

import 'StreamdemoWire.dart';

void main(List<String> args) {
  final wire = File(args[0]).readAsBytesSync();
  final report = TableWireReport();
  final value = feedWireLoad(wire, report);
  final saved = feedWireSave(value);
  final hex = saved.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  stdout.writeln('${report.unknown},${report.kindMismatch},${report.widened},'
      '${report.clamped},0,${report.malformed},${report.refused} $hex');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dart, "probe.dart", filepath.Join(dir, "mutant.bin"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("feed report probe over the generated library: %v\n%s", err, out)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		t.Fatalf("probe did not print a report and saved bytes: %q", out)
	}
	var rep dartReport
	if _, err := fmt.Sscanf(fields[0], "%d,%d,%d,%d,%d,%t,%t", &rep.unknown, &rep.kindMismatch, &rep.widened, &rep.clamped, &rep.duplicate, &rep.malformed, &rep.refused); err != nil {
		t.Fatalf("probe report %q: %v", fields[0], err)
	}
	saved, err := hex.DecodeString(fields[1])
	if err != nil {
		t.Fatalf("probe saved bytes %q: %v", fields[1], err)
	}
	return rep, saved
}
