package goosetable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGooseMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Goose's LoginRequestLoadMessages. Storage for one body
// whose ClientBuild is the sentinel 17 owes "A refused batch returns a count
// of `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md §3.3):
// 0, batch_too_large, report.count 0, and (R4, §6.5) every value handed in at
// its declared default, the sentinel gone. Storage for two owes 2 and 40 in
// both. A caller's capacity is a resource limit (§6.5), so this row runs here,
// in the leg's own tests, never in the shared suites.
func TestGooseMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := gooseMsgUnitDir(t, "tables/backend")
	sentinel := [][]string{{".ClientBuild = 17"}}
	// the sentinel must show: a body at 17 saves differently from a default one
	seventeen := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", values: [][]string{{".ClientBuild = 17"}}})
	dflt := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", values: [][]string{{}}})
	if seventeen.saved == "" || seventeen.saved == dflt.saved {
		t.Fatalf("the sentinel 17 is the declared default of client_build, so a reset would not show:\n%s\n%s", seventeen.raw, dflt.raw)
	}
	one := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", data: fixture, capacity: 1, back: sentinel})
	if one.loadN != 0 || one.loadReason != "batch_too_large" || one.loadCount != 0 {
		t.Fatalf("storage for one: n=%d %s count=%d, want 0 batch_too_large 0:\n%s", one.loadN, one.loadReason, one.loadCount, one.raw)
	}
	if len(one.slots) != 1 || one.fresh == "" || one.slots[0] != one.fresh {
		t.Fatalf("storage for one: the refused load left the slot off its declared default (R4)\n got  %q\n want %q", one.slots, one.fresh)
	}

	// THREE slots, all the sentinel. Every refusal owes count 0 and ALL THREE
	// slots at their declared default: slot 2 lies past the batch's count of 2,
	// so a reset of 0..count would leave it at 17.
	three := [][]string{{".ClientBuild = 17"}, {".ClientBuild = 17"}, {".ClientBuild = 17"}}
	wrongForm := append([]byte(nil), fixture...)
	wrongForm[0] ^= 0x01 // a framing refusal: the form byte is not the message form's
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"wrong_form_byte", wrongForm},
		{"dropped_last_byte", fixture[:len(fixture)-1]},
		{"empty", []byte{}},
	} {
		r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", data: c.data, capacity: 3, back: three})
		if r.loadN != 0 || (r.loadReason == "ok" && !r.loadMalformed) || r.loadCount != 0 {
			t.Fatalf("%s: n=%d %s count=%d, want a refusal (a reason, or damaged) with count 0:\n%s", c.name, r.loadN, r.loadReason, r.loadCount, r.raw)
		}
		if len(r.slots) != 3 || r.fresh == "" {
			t.Fatalf("%s: the probe printed %d slots and fresh %q:\n%s", c.name, len(r.slots), r.fresh, r.raw)
		}
		for k, s := range r.slots {
			if s != r.fresh {
				t.Fatalf("%s: slot %d is not at its declared default after the refusal (R4)\n got  %s\n want %s", c.name, k, s, r.fresh)
			}
		}
	}

	forty := []string{".ClientBuild = 40"}
	two := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", values: [][]string{forty, forty}, data: fixture, capacity: 2})
	if two.loadN != 2 || two.loadReason != "ok" || len(two.got) != 2 || len(two.want) != 2 {
		t.Fatalf("storage for two: n=%d %s:\n%s", two.loadN, two.loadReason, two.raw)
	}
	for k := range two.got {
		if two.got[k] != two.want[k] {
			t.Fatalf("storage for two: body %d is not client_build 40\n got  %s\n want %s", k, two.got[k], two.want[k])
		}
	}
	if !strings.EqualFold(two.saved, hex.EncodeToString(fixture)) {
		t.Fatalf("two bodies of client_build 40 save to %s, not the fixture %s", two.saved, hex.EncodeToString(fixture))
	}
}
