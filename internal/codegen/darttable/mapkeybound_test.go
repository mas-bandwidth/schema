package darttable

// KEYS NEVER CLAMP (docs/SPEC-TABLES.md §2.8): a map entry whose `string(N)`
// key is over the reader's bound N is dropped WHOLE — the body never decodes,
// nothing inside it is counted, and the entry never becomes the last landed
// key the order check compares against. The Go oracle is
// internal/tablewire mapField's `if !tabletext.MapKeyFits(f, key) {
// r.report.Clamped++; continue }`, and this test pins the Dart map read to it.
//
// Three cases:
//
//   - The Text wire-fuzz random pass #1222 mutant (seed 24845619678), which
//     the oracle reads as damaged: DAMAGE DISCARDS THE WHOLE LOAD (§4), so the
//     Dart leg owes the refusal alone.
//   - A constructed mutant whose dropped entry ALSO carries an over-bound
//     string(16) VALUE: because the entry is dropped before the body decodes,
//     that value's clamp is never counted, pinning the body-events rule and
//     not just the entry count.
//   - A constructed mutant with an over-bound key between two landed keys that
//     would be out of order against it: the drop must not make the over-bound
//     key the last landed key, or the later "c" would read as descending.

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestWireMapStringKeyOverBoundDart(t *testing.T) {
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
	def := u.Tables["Text"]
	if def == nil {
		t.Fatal("the mapdemo unit has no Text root")
	}

	textHex := func(keys, vals []string) string {
		mp := model.New(def)
		nf := &mp.Fields[0]
		for i := range keys {
			en := model.NewMapEntry(nf.Def)
			en.Fields[0].Cell.Str = []byte(keys[i])
			en.Fields[1].Cell.Str = []byte(vals[i])
			nf.Entries = append(nf.Entries, tabletext.Cell{Tab: en})
		}
		data, err := tablewire.Encode(model, mp)
		if err != nil {
			t.Fatalf("Go Encode of the constructed Text: %v", err)
		}
		return hex.EncodeToString(data)
	}

	overKey := strings.Repeat("z", 9)
	overVal := strings.Repeat("v", 17)

	cases := []struct {
		name string
		hex  string
	}{
		{
			// Text wire-fuzz random pass #1222 (mutant sha256
			// 8b51848688d213d9a52c95fd3d2cec8f45676f89c0716ee94be123d9cb4d1c4b):
			// the first `names` entry's key is 13 bytes against string(8), so Go
			// drops it whole (one clamped) and saves one entry; the pre-fix Dart
			// landed it and saved two.
			name: "random 1222 over-bound key drops whole",
			hex:  "01010e260d0211020c0d616c70686103047566697273740011020c0462657461030c067365636f6e6400040e150d01120207050003210a4100e9002d4e3dd800da00050e240d02100604fdffffff030e040604deadbeef0010020402000000030e060604000180ff00060c09000000004f8168f2ff28b7afec105b36194ac93dea0ce83094fde47cca5c7157f6f133a65b92de9cabeae214e9ea716f0f0182bf0600000000000000",
		},
		{
			// A dropped entry whose body also holds an over-bound string(16) value:
			// the drop happens before the body decodes, so the value's clamp never
			// counts (Dart clamped=1, not 2), and the value's bytes never save.
			name: "over-bound value in a dropped entry counts nothing",
			hex:  textHex([]string{overKey, "beta"}, []string{overVal, "gamma"}),
		},
		{
			// An over-bound key between two landed keys: "a" lands, the 9-byte
			// "zzzzzzzzz" is dropped (one clamped), then "c" is ascending against
			// "a" and lands. If the over-bound key had become the last landed key,
			// "c" < "zzzzzzzzz" would read as descending malformed instead.
			name: "over-bound key between landed keys never judged",
			hex:  textHex([]string{"a", overKey, "c"}, []string{"1", "2", "3"}),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			data, err := hex.DecodeString(c.hex)
			if err != nil {
				t.Fatalf("decode the mutant: %v", err)
			}
			inst := model.New(def)
			var report tabletext.Report
			if _, err := tablewire.DecodeRegion(model, inst, data, &report); err != nil {
				t.Fatalf("Go DecodeRegion on %s: %v", c.name, err)
			}
			wantSaved, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("Go Encode on %s: %v", c.name, err)
			}

			got := mapProbe(t, wire, "Text", c.hex)
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
