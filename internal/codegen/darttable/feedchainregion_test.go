package darttable

// THE MEASURE WALKS THE ROOT BODY THE REFERENCE WALKS, NOT THE TRAILER BESIDE
// IT. On streamdemo.Feed the reference's measure hands extentScan and
// nodeTableBytes the body between the form byte and the trailer
// (internal/tablewire/extent.go FileRegionMeasure, internal/tablewire/
// nodetable.go nodeTableBytes, docs/SPEC-TABLES.md §3 and §6.2). Before this
// card the generated Dart <root>WireMeasure read the same bytes with a reader
// whose limit was the whole file, and it kept walking after an unnamed
// reference or an unskippable kind where nodeTableBytes stops: on the
// stream_chain mutant below it found a node table made of trailer bytes and
// counted two Chunk records, asking for 264 region bytes where the framing
// commands exactly 136 (corpus seed stream_chain, pass reference #2, run seed
// 24845619678).
//
// The mutants are the fuzzer's receipt, restored with `xxd -r -p`. The oracle
// is the Go reference: FileRegionMeasure for the measure and the refusal,
// DecodeRegion's report for every counter, and Encode for the bytes a clean
// read saves.

import (
	"encoding/base64"
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

func TestWireFeedChainRegionBytesDart(t *testing.T) {
	t.Parallel()

	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "stream", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/stream: %v", err)
	}
	u := loadUnit(t, schemas...)
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

	cases := []struct {
		name string
		hex  string
	}{
		{
			// stream_chain reference #2: the first reference is already
			// unnamed, and the walk must end there.
			name: "reference #2",
			hex:  "01080803000000020f030d0a040e0306010105110200060c1602070a040e03060102051103000707040e030601030000c03a5cb5072eb7082652b3285633d4d8228e3c877681830f054aa33067555b8528f025a0ba6c31e5ffffffffffffffff82e051f9dc6843cf0700000000000000",
		},
	}

	model := tabletext.NewModel(u)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			data, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("decode the receipt mutant: %v", err)
			}
			wantMeasure, whole, refused := tablewire.FileRegionMeasure(u, root, data)
			if refused || !whole {
				t.Fatalf("Go FileRegionMeasure on %s: measure=%d whole=%v refused=%v; the receipt is a clean read", c.name, wantMeasure, whole, refused)
			}
			if wantMeasure != 136 {
				t.Fatalf("Go FileRegionMeasure = %d, want 136 on %s", wantMeasure, c.name)
			}
			inst := model.New(root)
			var report tabletext.Report
			if _, err := tablewire.DecodeRegion(model, inst, data, &report); err != nil {
				t.Fatalf("Go DecodeRegion on %s: %v", c.name, err)
			}
			wantSaved, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("Go Encode on %s: %v", c.name, err)
			}

			got := feedChainProbe(t, wire, c.hex)
			if got.measure != wantMeasure {
				t.Errorf("Dart feedWireMeasure = %d, tablewire.FileRegionMeasure = %d on %s", got.measure, wantMeasure, c.name)
			}
			if got.unknown != report.Unknown || got.kindMismatch != report.KindMismatch ||
				got.widened != report.Widened || got.clamped != report.Clamped {
				t.Errorf("Dart counters = %d,%d,%d,%d; the Go oracle = %d,%d,%d,%d on %s",
					got.unknown, got.kindMismatch, got.widened, got.clamped,
					report.Unknown, report.KindMismatch, report.Widened, report.Clamped, c.name)
			}
			if got.malformed != report.Malformed {
				t.Errorf("Dart malformed = %v, the Go oracle = %v on %s", got.malformed, report.Malformed, c.name)
			}
			if got.refused != report.Refused {
				t.Errorf("Dart refused = %v, the Go oracle = %v on %s", got.refused, report.Refused, c.name)
			}
			if got.saved != hex.EncodeToString(wantSaved) {
				t.Errorf("Dart saved %s, the Go oracle saves %s on %s", got.saved, hex.EncodeToString(wantSaved), c.name)
			}
		})
	}
}

type feedChainAnswer struct {
	measure      int64
	unknown      int
	kindMismatch int
	widened      int
	clamped      int
	malformed    bool
	refused      bool
	saved        string
}

// feedChainProbe runs the generated StreamdemoWire measure, a tolerant load
// and a save over one mutant, one answer a line.
func feedChainProbe(t *testing.T, wire []byte, mutantHex string) feedChainAnswer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "StreamdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:convert';
import 'dart:io';
import 'StreamdemoWire.dart';

void main(List<String> args) {
  final data = base64Decode(args[0]);
  final measure = feedWireMeasure(data);
  final report = TableWireReport();
  final v = feedWireLoad(data, report);
  final saved = feedWireSave(v);
  stdout.writeln('measure=$measure');
  stdout.writeln('unknown=${report.unknown}');
  stdout.writeln('kind=${report.kindMismatch}');
  stdout.writeln('widened=${report.widened}');
  stdout.writeln('clamped=${report.clamped}');
  stdout.writeln('malformed=${report.malformed ? 1 : 0}');
  stdout.writeln('refused=${report.refused ? 1 : 0}');
  stdout.writeln('saved=${saved.map((b) => b.toRadixString(16).padLeft(2, '0')).join()}');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "probe.dart", base64OfHex(mutantHex))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Streamdemo WireMeasure/Load/Save over the generated library: %v\n%s", err, out)
	}
	var got feedChainAnswer
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		switch k {
		case "measure":
			got.measure = parseInt64(t, v)
		case "unknown":
			got.unknown = int(parseInt64(t, v))
		case "kind":
			got.kindMismatch = int(parseInt64(t, v))
		case "widened":
			got.widened = int(parseInt64(t, v))
		case "clamped":
			got.clamped = int(parseInt64(t, v))
		case "malformed":
			got.malformed = v == "1"
		case "refused":
			got.refused = v == "1"
		case "saved":
			got.saved = v
		default:
			t.Fatalf("probe printed an unknown key %q", k)
		}
	}
	return got
}

func parseInt64(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

func base64OfHex(h string) string {
	raw, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// TestCardWireFuzzMutantRefusedDart pins the cards-dart-wire-fuzz receipt
// (#1211 card-populated, pass length #76, seed 24845619678): a ten-byte LEB128
// length of 2^64-1 (read as -1 in a signed 64-bit Dart int) on a counted/list
// field in Card.wire is root-body framing damage. The Dart measure matches
// tablewire.FileRegionMeasure, and DAMAGE DISCARDS THE WHOLE LOAD
// (docs/SPEC-TABLES.md §4): the Dart load owes the refusal, and no counter or
// saved byte is compared.
func TestCardWireFuzzMutantRefusedDart(t *testing.T) {
	t.Parallel()

	u := cardsUnit(t)
	root := u.Tables["Card"]
	if root == nil {
		t.Fatal("the cards unit has no Card root")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files["CardsWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no CardsWire.dart")
	}

	const mutantHex = "01010c09706f70756c61746564020c2830313233343536373839303132333435363738393031323334353637383930313233343536373839031e04050c047265706f060c0462617365070c046b696e64081e090a0c0673747265616d0b0effffffffffffffffff010d020c0c0c08706174682f6f6e65000c0c0c08706174682f74776f000d0e0f0d010c0c0c086e65772f70617468000e0e120d010f0c0c0b7368617265642f70617468000f0e1c0d020c0c0c086e6565642d6f6e65000c0c0c086e6565642d74776f00100c0c66697874757265207465737411110212042d000000131103140407000000151e16170c06686f6c646572180401000000190c28303132333435363738393031323334353637383930313233343536373839303132333435363738391a0c0b6272616e63682f6e616d651b11041c0c5d031d09646f6e65207768656e1d18736f757263652072c3a973756dc3a9e28692e4b896e7958c1d35626f64792077697468206d756c74696279746520e28692e4b896e7958c20616e64202265736361706573220a616e6420746162730900c03a5cb5072eb708eda4ea5c19ae45828959b62eef846f1e49cd1886a9b4cb173d66acbf1f58ac4c93ddbbb4d8894fce43326721d7969cef891eac358ceab6f50891d32acc487738857990e8d8366a4f7f3a5fbea793113f220b02864cee63af9863ee5a43c2b6dbf47dd8e76e020a08ba67af861e81de4d252b7c19efe6e6f91277b5a5667b67dccb04c763da612350b82d788f22dcdb76a66b6e19d19796fc87f28b628712535fa1d33d0b9518970e4f3d725cb5530863ee3d302bbd18e663030c9a5fcc128f0aa58b2751f6c09e919532c9c69be74dcdffffffffffffffff58fcaffad8e04b701d00000000000000"

	data, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatalf("decode the receipt mutant: %v", err)
	}
	wantMeasure, whole, refused := tablewire.FileRegionMeasure(u, root, data)
	if refused || !whole {
		t.Fatalf("Go FileRegionMeasure: measure=%d whole=%v refused=%v; expected a clean read", wantMeasure, whole, refused)
	}
	model := tabletext.NewModel(u)
	var report tabletext.Report
	if _, err := tablewire.DecodeRegion(model, model.New(root), data, &report); err != nil || !report.Malformed {
		t.Fatalf("Go DecodeRegion does not read the mutant as damaged: %v %+v", err, report)
	}

	got := cardWireProbe(t, wire, mutantHex, packetFiles(t, u))
	if got.measure != wantMeasure {
		t.Errorf("Dart cardWireMeasure = %d, tablewire.FileRegionMeasure = %d", got.measure, wantMeasure)
	}
	if !got.malformed {
		t.Errorf("the Dart load did not refuse a damaged card")
	}
}

func cardWireProbe(t *testing.T, wire []byte, mutantHex string, packet map[string][]byte) feedChainAnswer {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, packet)
	if err := os.WriteFile(filepath.Join(dir, "CardsWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:convert';
import 'dart:io';
import 'CardsWire.dart';

void main(List<String> args) {
  final data = base64Decode(args[0]);
  final measure = cardWireMeasure(data);
  final report = TableWireReport();
  final v = cardWireLoad(data, report);
  final saved = cardWireSave(v);
  stdout.writeln('measure=$measure');
  stdout.writeln('unknown=${report.unknown}');
  stdout.writeln('kind=${report.kindMismatch}');
  stdout.writeln('widened=${report.widened}');
  stdout.writeln('clamped=${report.clamped}');
  stdout.writeln('malformed=${report.malformed ? 1 : 0}');
  stdout.writeln('refused=${report.refused ? 1 : 0}');
  stdout.writeln('saved=${saved.map((b) => b.toRadixString(16).padLeft(2, '0')).join()}');
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "probe.dart", base64OfHex(mutantHex))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Card WireMeasure/Load/Save over the generated library: %v\n%s", err, out)
	}
	var got feedChainAnswer
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		switch k {
		case "measure":
			got.measure = parseInt64(t, v)
		case "unknown":
			got.unknown = int(parseInt64(t, v))
		case "kind":
			got.kindMismatch = int(parseInt64(t, v))
		case "widened":
			got.widened = int(parseInt64(t, v))
		case "clamped":
			got.clamped = int(parseInt64(t, v))
		case "malformed":
			got.malformed = v == "1"
		case "refused":
			got.refused = v == "1"
		case "saved":
			got.saved = v
		default:
			t.Fatalf("probe printed an unknown key %q", k)
		}
	}
	return got
}
