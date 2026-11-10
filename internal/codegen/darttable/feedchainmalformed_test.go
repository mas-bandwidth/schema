package darttable

// THE STREAM CHAIN'S MALFORMED NODE RECORD (docs/SPEC-TABLES.md §3.1, §4).
//
// streamdemo.Feed seed stream_chain, reference pass #41 at run seed 24845619678,
// mutated a node record's body so its zero reference is not the last byte of
// the record's length. The Go reference (internal/tablewire/decodenodes.go,
// decodeVariable pass two) reads a node body to its terminator and never looks
// at what follows it inside the record; the Dart emitter added a
// `sub.pos != sub.limit` check there and reported malformed where the oracle
// read clean. A node record's body is an ordinary table body (§3.1), and the
// reference is the wire fuzzer's judge, so Dart must answer the same report,
// the same measure and the same saved bytes.

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The mutant the harness printed for streamdemo.Feed seed stream_chain,
// reference pass #41, run seed 24845619678.
const streamChainPass41Hex = "01010803000000020f030d0a040e0306010105110200060c1602070a000e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000"

// The oracle's answer for those bytes: a clean read, the framing's exact
// region, and the 94 bytes a re-save writes back.
const (
	streamChainPass41Measure    = 264
	streamChainPass41SavedBytes = 94
)

func TestWireFeedChainMalformedDart(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	t.Parallel()

	mutant, err := hex.DecodeString(streamChainPass41Hex)
	if err != nil {
		t.Fatalf("the embedded mutant is not hex: %v", err)
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

	// THE GO ORACLE: the wire fuzzer's own read and measure (internal/tablewire).
	model := tabletext.NewModel(u)
	def := model.Lookup("Feed")
	if def == nil {
		t.Fatal("the streamdemo unit declares no Feed")
	}
	measured, whole, refused := tablewire.FileRegionMeasure(u, def, mutant)
	inst := model.New(def)
	var report tabletext.Report
	// STRICT VERDICT: the oracle refuses this mutant whole (wire_damaged);
	// the Dart read must answer the same refusal.
	_, _ = tablewire.DecodeRegion(model, inst, mutant, &report)
	encoded, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("the oracle could not save its read: %v", err)
	}
	_ = whole
	_ = refused
	if measured != streamChainPass41Measure {
		t.Fatalf("the oracle's measure moved: %d, want %d", measured, streamChainPass41Measure)
	}
	if !report.Refused {
		t.Fatalf("the oracle does not refuse the replay mutant: %s", report.Reason)
	}
	if report.Unknown != 0 || report.Widened != 0 {
		t.Fatalf("the oracle's counters moved: %+v", report)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "StreamdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mutant.bin"), mutant, 0o600); err != nil {
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
  stdout.writeln('${report.unknown},${report.widened},'
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
	if len(fields) != 6 {
		t.Fatalf("the probe did not answer six fields: %q", out)
	}
	num := func(i int) int {
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			t.Fatalf("probe field %d is not a number: %q", i, fields[i])
		}
		return n
	}
	leg := struct {
		unknown, widened  int
		refused           bool
		measure, savedLen int
		saved             string
	}{
		unknown:  num(0),
		widened:  num(1),
		refused:  fields[2] == "true",
		measure:  num(3),
		savedLen: num(4),
		saved:    fields[5],
	}

	// FIELD BY FIELD against the oracle, not against a remembered answer.
	if leg.unknown != report.Unknown {
		t.Fatalf("unknown: dart %d, oracle %d", leg.unknown, report.Unknown)
	}
	if leg.widened != report.Widened {
		t.Fatalf("widened: dart %d, oracle %d", leg.widened, report.Widened)
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

	// AND THE VERDICT THE CARD PINS: a whole-load refusal.
	if !leg.refused {
		t.Fatalf("the replay mutant's verdict is no longer the card's: %+v", leg)
	}
}
