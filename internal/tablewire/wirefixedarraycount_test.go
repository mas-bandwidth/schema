package tablewire_test

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestEngineWireFixedArrayShortCount pins docs/SPEC-TABLES.md §4 and §6.5 for
// the engine reader: a count short of a fixed [N]T field's N decodes the
// elements the writer sent, leaves the tail at defaults and counts malformed
// naming the field. The bounded [..N]T spelling keeps the silent tolerance.
func TestEngineWireFixedArrayShortCount(t *testing.T) {
	t.Parallel()
	m := listModel(t, `package probe
enum Difficulty { Easy, Normal, Hard }
fixed table GlobalSettings
{
    tick_rate    uint32 = 60
    difficulty   Difficulty = Normal
    build_note   string(48)
    spawn_delays [3]float32
    budget       [..3]float32
}
`)
	inst := place(t, m, "GlobalSettings", `{"spawn_delays":[1.5,2.5,3.5]}`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	pat := []byte{0x0e, 0x0e, 0x0a, 0x03}
	i := bytes.Index(wire, pat)
	if i < 0 || bytes.Index(wire[i+len(pat):], pat) >= 0 {
		t.Fatalf("fixed array header not found once: %x", wire)
	}
	sent := [3]float64{1.5, 2.5, 3.5}
	for _, c := range []byte{0, 1, 2} {
		damaged := append([]byte(nil), wire...)
		damaged[i+3] = c
		back := m.New(m.Lookup("GlobalSettings"))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, back, damaged, &report)
		if !ok || err != nil || !report.Malformed {
			t.Fatalf("count %d: decode ok=%v err=%v report=%+v", c, ok, err, report)
		}
		if report.Reason != "malformed: spawn_delays" {
			t.Fatalf("count %d: reason %q", c, report.Reason)
		}
		delays := fieldByName(t, back, "spawn_delays")
		for slot, cell := range delays.Elems {
			want := 0.0
			if slot < int(c) {
				want = sent[slot]
			}
			if cell.F != want {
				t.Fatalf("count %d: slot %d value %v, want %v", c, slot, cell.F, want)
			}
		}
	}
	// The bounded spelling's short count stays silent.
	bounded := place(t, m, "GlobalSettings", `{"budget":[4.5,5.5]}`)
	bw, err := tablewire.Encode(m, bounded)
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(m.Lookup("GlobalSettings"))
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, back, bw, &report); !ok || err != nil || !report.Silent() {
		t.Fatalf("bounded short count: ok=%v err=%v report=%+v", ok, err, report)
	}
}
