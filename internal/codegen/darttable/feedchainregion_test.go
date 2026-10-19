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
		c := c
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
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
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
