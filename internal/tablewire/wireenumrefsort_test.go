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
// reference resolves BY ENTRY (docs/SPEC-TABLES.md §3): reference 0 is None,
// entry 3 is a field-name id so it counts one unknown, and the first-use
// renumbering of a None spelling is not a decode event (§16.11's carve-out).
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
	m := tabletext.NewModel(u)
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
	grade := func(t *testing.T, inst *tabletext.Instance) uint64 {
		t.Helper()
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == "grade" {
				return inst.Fields[i].Cell.U
			}
		}
		t.Fatal("no grade field")
		return 0
	}
	entries := func(w []byte) [][]byte {
		n := int(binary.LittleEndian.Uint64(w[len(w)-8:]))
		start := len(w) - 8 - n*8
		out := make([][]byte, n)
		for i := range out {
			out[i] = w[start+i*8 : start+i*8+8]
		}
		return out
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
			if ok, err := tablewire.Decode(m, noneBack, none, &silent); !ok || err != nil || !silent.Silent() {
				t.Fatalf("None reference: %v %v %+v", ok, err, silent)
			}
			if got := grade(t, noneBack); got != 0 {
				t.Fatalf("None reference read as grade %d, want None", got)
			}
			noneAgain, err := tablewire.Encode(m, noneBack)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(noneAgain, none) || len(noneAgain) != len(none) {
				t.Fatalf("None reference re-encoded to %d bytes, want the same %d and a first-use difference", len(noneAgain), len(none))
			}
			before, after := entries(none), entries(noneAgain)
			if !bytes.Equal(before[1], after[2]) || !bytes.Equal(before[2], after[1]) {
				t.Fatalf("the id table did not swap entries 2 and 3: before %x after %x", before, after)
			}

			other := bytes.Clone(base)
			other[3] = 0x03
			otherBack := m.New(root)
			var unknown tabletext.Report
			if ok, err := tablewire.Decode(m, otherBack, other, &unknown); !ok || err != nil {
				t.Fatalf("entry-3 reference: %v %v %+v", ok, err, unknown)
			}
			if unknown.Unknown != 1 {
				t.Fatalf("entry-3 reference report %+v, want exactly one unknown", unknown)
			}
			if got := grade(t, otherBack); got != 0 {
				t.Fatalf("entry-3 reference read as grade %d, want None", got)
			}

			over := bytes.Clone(base)
			count := byte(binary.LittleEndian.Uint64(over[len(over)-8:]))
			over[3] = count + 1
			overBack := m.New(root)
			var damaged tabletext.Report
			if ok, _ := tablewire.Decode(m, overBack, over, &damaged); ok || !damaged.Malformed {
				t.Fatalf("reference past the table: ok=%v report=%+v, want malformed", ok, damaged)
			}
		})
	}
}
