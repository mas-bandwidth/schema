package darttable

// LOAD IS A SCAN (docs/SPEC-TABLES.md §3.1): the reference decodes EVERY placed
// node record's body in PASS TWO, in index order and before the root body,
// reachable by a pointer or not (internal/tablewire/decodenodes.go
// decodeVariable). The generated Dart decoded a node record lazily, only when a
// pointer resolved it (wireform.go resolveTable), so a record no pointer names
// contributed none of its events. Each mutant below is loaded through the
// generated Dart and through the Go oracle (internal/tablewire) and must hold
// the same six counters and saved bytes:
//
//   - mapdemo.Crews, wire fuzzer seed 24845619678 pass random #23 (mutant
//     sha256 85186b4d...): the `members` field id and the third record's type id
//     are unknown, and the two Item records no pointer names each carry an
//     unknown `count` id. Go counts 4 unknowns; the lazy Dart counted 2.
//   - A clean Crews file whose node table carries a third Item record no
//     pointer names, holding `count` under int8 (a kind int32 widens from).
//     Go counts one widened in PASS TWO; the lazy Dart counted none.

import (
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestWireUnreachedNodeRecordsCountDart(t *testing.T) {
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
	model := tabletext.NewModel(u)

	cases := []struct {
		name string
		root string
		hex  string
	}{
		{
			// mapdemo.Crews, seed 24845619678 pass random #23: the root's
			// `members` field id is unknown (so the whole map is one unknown
			// root field), the third record's type id is unknown (one unknown
			// record), and each of the two unreachable Item records carries
			// the unknown `count` id. Go: unknown=4, everything else 0.
			name: "unreachable Item records' unknown fields, Crews",
			root: "Crews",
			hex:  "01010e1f0d020e020804000000030e0411020203000d020809000000030e0311010402040402000000050c1c03060707042800000000060707042900000000020707045a04000000008010395e6794d579ec105b36184ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff06684798d1a1cf52748279448ee2e5b10700000000000000",
		},
		{
			// THE CONSTRUCTED CASE: a clean Crews file (no unknown, no damage)
			// whose node table holds a third Item record that no pointer names,
			// its `count` under int8 (kind 2), a kind int32 (kind 4) widens
			// from. Go's PASS TWO reads it and counts one widened; the saved
			// bytes never carry the unreachable record.
			name: "unreachable Item record's widened field, Crews",
			root: "Crews",
			hex:  "01010e1f0d020e020804000000030e0411020203000d020809000000030e0311010200040402000000050c1903060707042800000000060707042900000000060407025a00009010395e6794d579ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff06684798d1a1cf5274a279448ee2e5b10700000000000000",
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
			inst := model.New(def)
			var report tabletext.Report
			if _, err := tablewire.DecodeRegion(model, inst, data, &report); err != nil {
				t.Fatalf("Go DecodeRegion on %s: %v", c.name, err)
			}
			if report.Malformed || report.Refused {
				t.Fatalf("the Go oracle reads %s as malformed=%v refused=%v; the case must be clean past its own events", c.name, report.Malformed, report.Refused)
			}
			wantSaved, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("Go Encode on %s: %v", c.name, err)
			}

			got := mapProbe(t, wire, c.root, c.hex)
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
