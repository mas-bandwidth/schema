package darttable

// THE MAP DECODE AND REPORT (docs/SPEC-TABLES.md §2.8, §6.6) on the Dart leg
// must agree with the Go oracle byte for byte and counter for counter. Before
// this card the generated Dart map reader counted ONE `widened` per ENTRY
// whose key scan widened (wireunion.go emitLoadMap), while the Go reference
// counts one per MAP (internal/tablewire mapField: `if entryWidened &&
// !widened`). The receipt below is the corpus `map_key_widen` vector, a map
// whose two entries both ride a key kind the int32 key widens from: the Go
// oracle reports widened=1 while the old Dart reported 2.

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestWireMapRootsDecodeAndReportDart(t *testing.T) {
	t.Parallel()

	schemas, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "maps", "*.schema"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no schema files under tables/maps: %v", err)
	}
	u := loadUnit(t, schemas...)
	root := u.Tables["Chunks"]
	if root == nil {
		t.Fatal("the mapdemo unit has no Chunks root")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wire, ok := files["MapdemoWire.dart"]
	if !ok || len(wire) == 0 {
		t.Fatal("Generate emitted no MapdemoWire.dart")
	}

	cases := []struct {
		name string
		hex  string
	}{
		{
			// map_key_widen: two entries whose keys ride a kind the int32
			// key widens from (corpus seed map_chunks, pass kind #142). The
			// Go oracle counts ONE widened per MAP, the old Dart counted per
			// entry and reported 2.
			name: "widened once per map",
			hex:  "01010e150d02070202fd031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000",
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
			inst := model.New(root)
			var report tabletext.Report
			if _, err := tablewire.DecodeRegion(model, inst, data, &report); err != nil {
				t.Fatalf("Go DecodeRegion on %s: %v", c.name, err)
			}
			if report.Malformed || report.Refused {
				t.Fatalf("Go DecodeRegion on %s: malformed=%v refused=%v; the receipt is a clean read", c.name, report.Malformed, report.Refused)
			}
			wantSaved, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("Go Encode on %s: %v", c.name, err)
			}

			got := mapProbe(t, wire, c.hex)
			if got.unknown != report.Unknown || got.kindMismatch != report.KindMismatch ||
				got.widened != report.Widened || got.clamped != report.Clamped ||
				got.duplicate != report.Duplicate {
				t.Errorf("Dart counters = %d,%d,%d,%d,%d; the Go oracle = %d,%d,%d,%d,%d on %s",
					got.unknown, got.kindMismatch, got.widened, got.clamped, got.duplicate,
					report.Unknown, report.KindMismatch, report.Widened, report.Clamped, report.Duplicate, c.name)
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

type mapAnswer struct {
	unknown      int
	kindMismatch int
	widened      int
	clamped      int
	duplicate    int
	malformed    bool
	refused      bool
	saved        string
}

// mapProbe runs the generated MapdemoWire chunks load and save over one mutant,
// one answer a line.
func mapProbe(t *testing.T, wire []byte, mutantHex string) mapAnswer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MapdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import 'dart:convert';
import 'dart:io';
import 'MapdemoWire.dart';

void main(List<String> args) {
  final data = base64Decode(args[0]);
  final report = TableWireReport();
  final v = chunksWireLoad(data, report);
  final saved = chunksWireSave(v);
  stdout.writeln('unknown=${report.unknown}');
  stdout.writeln('kind=${report.kindMismatch}');
  stdout.writeln('widened=${report.widened}');
  stdout.writeln('clamped=${report.clamped}');
  stdout.writeln('duplicate=${report.duplicate}');
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
		t.Fatalf("MapdemoWire chunks load/save over the generated library: %v\n%s", err, out)
	}
	var got mapAnswer
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		switch k {
		case "unknown":
			got.unknown = int(parseInt64(t, v))
		case "kind":
			got.kindMismatch = int(parseInt64(t, v))
		case "widened":
			got.widened = int(parseInt64(t, v))
		case "clamped":
			got.clamped = int(parseInt64(t, v))
		case "duplicate":
			got.duplicate = int(parseInt64(t, v))
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
