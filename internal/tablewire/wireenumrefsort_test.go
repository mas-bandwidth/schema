package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const lowLoadoutWireSortJSON = `{"grade":"Bronze","grades":[],"podium":["Bronze","Bronze","Bronze"],"perks":[],"primary":{"damage":-1.5,"speed":-1.5,"penetration":0,"channel":0,"homing":false,"effect":{"buff":{"multiplier":-1.5}}},"backups":[{"damage":-1.5,"speed":-1.5,"penetration":0,"channel":0,"homing":false,"effect":{"buff":{"multiplier":-1.5}}},{"damage":-1.5,"speed":-1.5,"penetration":0,"channel":0,"homing":false,"effect":{"buff":{"multiplier":-1.5}}}],"attachments":[]}`

const highLoadoutWireSortJSON = `{"grade":"Gold","grades":["Gold","Gold","Gold","Gold"],"podium":["Gold","Gold","Gold"],"perks":["Shielded","Cloaked","Turbo"],"primary":{"damage":1.5,"speed":1.5,"penetration":10,"channel":63,"homing":true,"effect":{"buff":{"multiplier":1.5}}},"backups":[{"damage":1.5,"speed":1.5,"penetration":10,"channel":63,"homing":true,"effect":{"buff":{"multiplier":1.5}}},{"damage":1.5,"speed":1.5,"penetration":10,"channel":63,"homing":true,"effect":{"buff":{"multiplier":1.5}}}],"attachments":[{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5},{"slot":7,"power":1.5}]}`

// TestEngineWireEnumVariantRefSort is the engine's half of the Go test above:
// the same two crashers through internal/tablewire's Decode. A variant
// reference resolves BY ENTRY (docs/SPEC-TABLES.md §3), and a reference past
// the table is damage. A reference moved to another entry or to None reorders
// the id table's first use, which the read refuses (docs/rulings.json
// reads-refuse-non-canonical), rather than re-encoding on the next save.
func TestEngineWireEnumVariantRefSort(t *testing.T) {
	t.Parallel()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := variableRoots(tabletext.NewModel(u))
	root := m.Lookup("LoadoutConfig")
	build := func(t *testing.T, text string) []byte {
		t.Helper()
		inst := m.New(root)
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) || !r.Silent() {
			t.Fatalf("sample text: %+v", r)
		}
		w, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	for _, tc := range []struct {
		name   string
		text   string
		length int
	}{
		{"low_212", lowLoadoutWireSortJSON, 212},
		{"high_422", highLoadoutWireSortJSON, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := build(t, tc.text)
			if len(base) != tc.length {
				t.Fatalf("sample wire is %d bytes, want %d", len(base), tc.length)
			}
			back := m.New(root)
			var clean tabletext.Report
			if ok, err := tablewire.Decode(m, back, base, &clean); !ok || err != nil || !clean.Silent() {
				t.Fatalf("clean decode: %v %v %+v", ok, err, clean)
			}
			again, err := tablewire.Encode(m, back)
			if err != nil || !bytes.Equal(again, base) {
				t.Fatalf("clean sample re-encodes to %x, want %x (%v)", again, base, err)
			}

		none := bytes.Clone(base)
		none[3] = 0x00
		noneBack := m.New(root)
		var silent tabletext.Report
		if ok, _ := tablewire.Decode(m, noneBack, none, &silent); ok || !silent.Refused {
			t.Fatalf("a None reference that leaves the variant's first use later did not refuse: ok=%t report=%+v", ok, silent)
		}

		other := bytes.Clone(base)
		other[3] = 0x03
		otherBack := m.New(root)
		var unknown tabletext.Report
		if ok, _ := tablewire.Decode(m, otherBack, other, &unknown); ok || !unknown.Refused {
			t.Fatalf("a reference that reorders the variant's first use did not refuse: ok=%t report=%+v", ok, unknown)
		}

		over := bytes.Clone(base)
		count := byte(binary.LittleEndian.Uint64(over[len(over)-8:]))
		over[3] = count + 1
		overBack := m.New(root)
		var damaged tabletext.Report
		if ok, _ := tablewire.Decode(m, overBack, over, &damaged); ok || !damaged.Refused {
			t.Fatalf("reference past the table: ok=%v report=%+v, want malformed", ok, damaged)
		}
	})
	}
}
