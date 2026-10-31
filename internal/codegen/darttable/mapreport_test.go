package darttable

// THE MAP DECODE AND REPORT (docs/SPEC-TABLES.md §2.8, §4, §6.6) on the Dart
// leg must agree with the Go oracle (internal/tablewire mapField and mapKey)
// byte for byte and counter for counter on a clean read, and refuse on a
// damaged one: DAMAGE DISCARDS THE WHOLE LOAD (§4). Each mutant below is one
// the Dart wire fuzzer (seed 24845619678) or the card's own construction
// found:
//
//   - A widened key AHEAD OF DAMAGE in the same entry: damage, refused.
//   - A DESCENDING entry: damage, refused.
//   - Two widened keys in ONE map: Go counts one `widened` per MAP, and so
//     must Dart.

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
		root string
		hex  string
		// twoWidened marks the constructed case: the oracle must read it
		// clean with one widened, or it pins nothing about symptom C
		twoWidened bool
	}{
		{
			// SYMPTOM A: corpus seed map_chunks, pass kind #72 (mutant sha256
			// 02fc7c8e...). The first entry's int32 key rides kind 2 (int8),
			// a kind it widens from, and the entry's framing breaks after it:
			// Go 0,0,1,0,0 malformed, the old Dart 0,0,0,0,0 malformed.
			name: "widened key ahead of damage, Chunks",
			root: "Chunks",
			hex:  "01010e180d020a0202fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000",
		},
		{
			// SYMPTOM A on a second root: corpus seed map_crews, pass kind #76
			// (mutant sha256 d9da8be9...), a uint32 key under kind 6 (uint8).
			name: "widened key ahead of damage, Crews",
			root: "Crews",
			hex:  "01010e1f0d020e020604000000030e0411020203000d020809000000030e0311010400040402000000050c1c03060707042800000000060707042900000000060707045a00000000009010395e6794d579ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff06684798d1a1cf5274a279448ee2e5b10700000000000000",
		},
		{
			// SYMPTOM B: corpus seed map_text, pass reference #28 (mutant
			// sha256 4540b18f...). The second `names` entry's key reference
			// points at another id, so the entry has no key (the default "",
			// below "alpha": DESCENDING) and carries one field the entry
			// cannot name. Go 0,0,0,0,0 malformed, the old Dart 1,0,0,0,0.
			name: "descending entry counts nothing inside it, Text",
			root: "Text",
			hex:  "01010e260d0211020c05616c706861030c0566697273740011060c0462657461030c067365636f6e6400040e150d01120207050003210a4100e9002d4e3dd800de00050e240d02100204fdffffff030e060604deadbeef0010020402000000030e060604000180ff00060409000000004f8168f2ff28b7afec105b36194ac93dea0ce83094fde47cca5c7155f6f133a65b92de9cabeae214e9ea716f0f0182bf0600000000000000",
		},
		{
			// SYMPTOM C: the map_chunks seed with BOTH entries' int32 keys
			// under kind 2 (int8), a clean read. Go counts one widened for
			// the map, the old Dart counted one per entry and said 2.
			name:       "two widened keys in one map count one, Chunks",
			root:       "Chunks",
			hex:        "01010e120d02070202fd031102000702020203110300040408000000050c0d020604deadbeef0604000180ff005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000",
			twoWidened: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			def := u.Tables[c.root]
			if def == nil {
				t.Fatalf("the mapdemo unit has no %s root", c.root)
			}
			data, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("decode the mutant: %v", err)
			}
			model := tabletext.NewModel(u)
			inst := model.New(def)
			var report tabletext.Report
			if _, err := tablewire.DecodeRegion(model, inst, data, &report); err != nil {
				t.Fatalf("Go DecodeRegion on %s: %v", c.name, err)
			}
			if c.twoWidened && (report.Malformed || report.Widened != 1) {
				t.Fatalf("the Go oracle reads %s as malformed=%v widened=%d; the case needs a clean read with one widened", c.name, report.Malformed, report.Widened)
			}
			wantSaved, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("Go Encode on %s: %v", c.name, err)
			}

			got := mapProbe(t, wire, c.root, c.hex)
			if report.Malformed {
				// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the
				// leg owes the refusal, and no counter or saved byte is compared
				if !got.malformed {
					t.Errorf("the Dart load did not refuse a damaged map on %s", c.name)
				}
				return
			}
			if got.unknown != report.Unknown || got.kindMismatch != report.KindMismatch ||
				got.widened != report.Widened || got.clamped != report.Clamped ||
				got.duplicate != report.Duplicate || got.malformed != report.Malformed ||
				got.refused != report.Refused {
				t.Errorf("Dart report = %d,%d,%d,%d,%d,%v,%v; the Go oracle = %d,%d,%d,%d,%d,%v,%v on %s",
					got.unknown, got.kindMismatch, got.widened, got.clamped, got.duplicate, got.malformed, got.refused,
					report.Unknown, report.KindMismatch, report.Widened, report.Clamped, report.Duplicate, report.Malformed, report.Refused, c.name)
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

// mapProbe runs the generated MapdemoWire load and save of one root over one
// mutant, one answer a line.
func mapProbe(t *testing.T, wire []byte, root, mutantHex string) mapAnswer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MapdemoWire.dart"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	fn := strings.ToLower(root[:1]) + root[1:]
	probe := `import 'dart:convert';
import 'dart:io';
import 'MapdemoWire.dart';

void main(List<String> args) {
  final data = base64Decode(args[0]);
  final report = TableWireReport();
  final v = ` + fn + `WireLoad(data, report);
  final saved = ` + fn + `WireSave(v);
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
		t.Fatalf("MapdemoWire %s load/save over the generated library: %v\n%s", root, err, out)
	}
	var got mapAnswer
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
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
