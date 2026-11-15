package goosetable_test

// THE ENUM-KEYED ARRAY's indexing guard (docs/SPEC-TABLES.md §2.4,
// docs/PORTING.md M5), the Goose half of test/go-tables/keyed_test.go.
//
// The storage holds one slot per NAMED variant, the key k at slot k-1, and
// nothing for None. So TWO keys are program errors and not values: 0, which
// would index the element BEFORE the array, and anything past E.Max, which
// would index past its end. schema_rt::table_keyed (runtime/schema_tables.goose)
// refuses both by name with `abort`, which stands in every build — the probe
// is a `goose build -O2` executable, the release build. Goose's own index
// check aborts too, but it names an INDEX, so each row requires the guard's
// own refusal, which names the KEY, and a runtime with either compare
// dropped is red on that row.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const keyedProbeSchema = `package probe

enum Weapon { Cannon, Missile, Mine }

fixed table TurretConfig
{
    damage float32 = 10.0
}

fixed table HullConfig
{
    turrets [Weapon]TurretConfig
}
`

// keyedProbeMain takes one argument, the row: `place` fills every slot BY
// KEY and prints each slot with the key table_keyed_each hands beside it,
// `max` reaches E.Max, and `none` and `past` index by key 0 and E.Max+1.
const keyedProbeMain = `import os;
import .conformance_unit;
import .schema_tables;
fn main() {
    var hull = probe::HullConfig { .. };
    let row = args()[1];
    if row == "place" {
        var key: i64 = 1;
        while key <= probe::WeaponMax {
            schema_rt::table_keyed(hull.Turrets, key).Damage = (key * 10) as f32;
            key += 1;
        }
        for t, i in hull.Turrets { print("slot ", i, " ", t.Damage); }
        schema_rt::table_keyed_each(hull.Turrets) { k, t => print("key ", k, " ", t.Damage); };
        print("missile ", schema_rt::table_keyed(hull.Turrets, probe::WeaponMissile).Damage);
    }
    if row == "max" { schema_rt::table_keyed(hull.Turrets, probe::WeaponMax).Damage = 1.0; }
    if row == "none" { schema_rt::table_keyed(hull.Turrets, probe::WeaponNone).Damage = 1.0; }
    if row == "past" { schema_rt::table_keyed(hull.Turrets, probe::WeaponMax + 1).Damage = 1.0; }
    print("returned");
}
`

// keyedRun is one row's run: its stdout, stderr and whether it exited 0.
type keyedRun struct {
	stdout, stderr string
	ok             bool
}

// keyedProbe builds the probe once (release, -O2) over the runtime with
// `override` applied, and runs every row.
func keyedProbe(t *testing.T, override map[string][]byte) map[string]keyedRun {
	t.Helper()
	files := probeFiles(t, probeUnit(t, keyedProbeSchema), override)
	files["main.goose"] = []byte(keyedProbeMain)
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goose, why := gooserun.Compiler()
	if goose == "" {
		t.Fatal(why)
	}
	build := gooserun.Command(t, goose, "build", "-O2", "main.goose", "-o", "keyed")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the keyed probe: %v\n%s", err, out)
	}
	runs := map[string]keyedRun{}
	for _, row := range []string{"place", "max", "none", "past"} {
		cmd := gooserun.Command(t, filepath.Join(dir, "keyed"), row)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		runs[row] = keyedRun{stdout.String(), stderr.String(), err == nil}
	}
	return runs
}

// keyedRefused reports whether a run was refused by the KEY GUARD: it did not
// return, and its stderr carries the guard's message naming the key — not
// Goose's index check, which names an index.
func keyedRefused(r keyedRun, key int) bool {
	return !r.ok && !strings.Contains(r.stdout, "returned") &&
		strings.Contains(r.stderr, "enum-keyed array") && strings.Contains(r.stderr, fmt.Sprintf("key %d keys no slot", key))
}

// keyedRows is the gate's red list, one line per row that is red, each
// starting with its row's name.
func keyedRows(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	runs := keyedProbe(t, override)
	var bad []string
	if !keyedRefused(runs["none"], 0) {
		bad = append(bad, fmt.Sprintf("none: None keyed a slot, or was refused by the index check rather than by the key guard — it names no variant and the storage holds nothing for it (stderr %q)", runs["none"].stderr))
	}
	if !keyedRefused(runs["past"], 4) {
		bad = append(bad, fmt.Sprintf("past: key 4 keyed a slot of three, or was refused by the index check rather than by the key guard — nothing is stored past E.Max (stderr %q)", runs["past"].stderr))
	}
	if r := runs["max"]; !r.ok || !strings.Contains(r.stdout, "returned") {
		bad = append(bad, fmt.Sprintf("max: E.Max is a named variant and keys the last slot — it was refused (stderr %q)", r.stderr))
	}
	want := "slot 0 10.0\nslot 1 20.0\nslot 2 30.0\nkey 1 10.0\nkey 2 20.0\nkey 3 30.0\nmissile 20.0\nreturned\n"
	if r := runs["place"]; !r.ok || r.stdout != want {
		bad = append(bad, fmt.Sprintf("place: the key k must live at slot k-1 and walk beside its key; got %q (stderr %q), want %q", r.stdout, r.stderr, want))
	}
	return bad
}

func TestGooseKeyedAccessorCoversBothEnds(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the keyed probe)")
	t.Parallel()
	for _, b := range keyedRows(t, nil) {
		t.Error(b)
	}
}

// TestGooseKeyedNegativeControl plants each half of the guard out, and the
// shift, in turn, and requires each plant to turn EXACTLY its own row red:
//
//   - none: the None compare removed — key 0 reaches slots[-1], which Goose's
//     index check refuses by index, not by key;
//   - max: the past-E.Max compare removed — key 4 reaches the index check;
//   - shift: the key indexes slot k, not k-1 — place goes red (E.Max then
//     runs past the end, so the max row turns with it; none and past stay green).
func TestGooseKeyedNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the keyed probe)")
	t.Parallel()
	const guard = "    if key <= 0 || key > slots.len {\n"
	for _, ctl := range []struct {
		name, from, to string
		red            []string
	}{
		{"none", guard, "    if key > slots.len { // SABOTAGED: None is not refused\n", []string{"none"}},
		{"max", guard, "    if key <= 0 { // SABOTAGED: past E.Max is not refused\n", []string{"past"}},
		{"shift", "    return slots[key - 1];\n", "    return slots[key]; // SABOTAGED: no shift\n", []string{"max", "place"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := keyedRows(t, sabotage(t, "schema_tables.goose", ctl.from, ctl.to))
			var rows []string
			for _, b := range bad {
				rows = append(rows, strings.SplitN(b, ":", 2)[0])
			}
			if strings.Join(rows, " ") != strings.Join(ctl.red, " ") {
				t.Fatalf("%s: the plant turned rows %v red, want exactly %v:\n%s", ctl.name, rows, ctl.red, strings.Join(bad, "\n"))
			}
			t.Logf("%s: red on %v as planted", ctl.name, rows)
		})
	}
}
