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
// 0, batch_too_large, report.count 0, the sentinel untouched. Storage for two
// owes 2 and 40 in both. A caller's capacity is a resource limit (§6.5), so
// this row runs here, in the leg's own tests, never in the shared suites.
func TestGooseMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := gooseMsgUnitDir(t, "tables/backend")
	sentinel := [][]string{{".ClientBuild = 17"}}
	one := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", data: fixture, capacity: 1, back: sentinel})
	keep := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "LoginRequest", data: []byte{}, capacity: 1, back: sentinel})
	if len(keep.slots) != 1 || keep.slots[0] == keep.fresh {
		t.Fatalf("the sentinel does not show in the slot image (an empty wire, refused before any slot, left %q, fresh %q):\n%s", keep.slots, keep.fresh, keep.raw)
	}
	if one.loadN != 0 || one.loadReason != "batch_too_large" || one.loadCount != 0 {
		t.Fatalf("storage for one: n=%d %s count=%d, want 0 batch_too_large 0:\n%s", one.loadN, one.loadReason, one.loadCount, one.raw)
	}
	if len(one.slots) != 1 || one.slots[0] != keep.slots[0] {
		t.Fatalf("storage for one: the refused load wrote the slot\n got  %q\n want %q", one.slots, keep.slots)
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
