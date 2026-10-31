package darttable

// TestWireMapRepeatKeepsEarlierDart pins the repeated-map header rule
// (docs/SPEC-TABLES.md §2.8, §3, §4; internal/tablewire/decode.go mapField):
// a repeated map field commits to replacing the earlier map only AFTER its
// array header is readable and compatible (len >= 2, canonical count LEB128,
// and element kind 13). An inert repeat (len < 2) or a repeat whose element
// kind is not 13 leaves the earlier decoded map standing. A repeat whose count
// LEB128 cannot be read is damage, and DAMAGE DISCARDS THE WHOLE LOAD (§4):
// the Dart leg owes the refusal alone.

import (
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestWireMapRepeatKeepsEarlierDart(t *testing.T) {
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
	}{
		{
			// mapdemo.Slots random #1883: valid slots map followed by a repeat
			// whose element kind is 0x69 != 13 (kindMismatch=1, earlier stands).
			name: "Slots random #1883 bad element kind on repeat",
			root: "Slots",
			hex:  "01010e340d02180204feffffff03100e0402040402000000050465000000001802040600000003100e040204040100000005046400000000010e346902180204feffffff03100e0402040402000000050465000000001802040600000003100e040204040100000005046400c800000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762ffa0e9ea716f0f0182bf0600000000000000",
		},
		{
			// mapdemo.Chunks random #317: valid chunks map followed by an inert
			// repeat 01 0e 00 (len = 0 < 2; earlier stands, no counter raised).
			name: "Chunks random #317 short header on repeat",
			root: "Chunks",
			hex:  "01010e180d020a0204fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff010e005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000",
		},
		{
			// Constructed: valid chunks map followed by a repeat (01 0e 0b 0d
			// plus ten 0x80 continuation bytes) whose count LEB128 is
			// unterminated (malformed=true, earlier stands).
			name: "Chunks constructed unterminated count LEB on repeat",
			root: "Chunks",
			hex:  "01010e180d020a0204fdffffff031102000a02040200000003110300040408000000050c0d020604deadbeef0604000180ff010e0b0d80808080808080808080005b92de9cabeae214ec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffffe44f1c4f47c02e2f0600000000000000",
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
