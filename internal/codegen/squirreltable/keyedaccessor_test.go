package squirreltable_test

// M5 of docs/PORTING.md, the Squirrel cell: an enum-keyed array [E]T stays a
// plain array with key k at slot k-1 (docs/SPEC-TABLES.md §2.4), and the
// generated module's Keyed(table, field) is the one place a KEY is checked.
// get and set refuse None (key 0), a negative or non-integer key and any key
// past E.Max by name (`keyed_refused`), each(array, fn) hands the key beside
// the element, and key(name) is the key of a variant name.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const keyedAccessorSchema = `package keyedaccessor

enum Team { Red, Blue, Green }

table Root {
	scores [Team]int32
}
`

// keyedAccessorMain drives the accessor: every key in range round-trips
// through the wire at its slot, each yields the keys in order, and each
// refusal is printed by whether its message names keyed_refused.
const keyedAccessorMain = `local T = dofile("keyedaccessor/schema_module.nut", true).require("%MOD%");
local K = T.Keyed("Root", "scores");
local v = T.Root();
for (local key = 1; key <= 3; key++) K.set(v.Scores, key, key * 10);
local back = T.RootLoad(T.RootSave(v), {});
for (local key = 1; key <= 3; key++) print("key " + key + " " + K.get(back.Scores, key) + " slot " + back.Scores[key - 1] + "\n");
K.each(back.Scores, function(key, x) { print("each " + key + " " + x + "\n"); });
print("name Blue " + K.key("Blue") + "\n");
foreach (key in [0, -1, 4, 1.5, "1", null]) {
  local label = key == null ? "null" : (typeof key == "string" ? "\"" + key + "\"" : key.tostring());
  try { K.get(v.Scores, key); print("get " + label + " answered\n"); }
  catch (e) print("get " + label + (e.find("keyed_refused") != null ? " refused\n" : " threw " + e + "\n"));
  try { K.set(v.Scores, key, 1); print("set " + label + " answered\n"); }
  catch (e) print("set " + label + (e.find("keyed_refused") != null ? " refused\n" : " threw " + e + "\n"));
}
try { K.key("Purple"); print("name Purple answered\n"); }
catch (e) print("name Purple" + (e.find("keyed_refused") != null ? " refused\n" : " threw\n"));
`

const keyedAccessorWant = "key 1 10 slot 10\nkey 2 20 slot 20\nkey 3 30 slot 30\n" +
	"each 1 10\neach 2 20\neach 3 30\nname Blue 2\n" +
	"get 0 refused\nset 0 refused\nget -1 refused\nset -1 refused\nget 4 refused\nset 4 refused\n" +
	"get 1.5 refused\nset 1.5 refused\nget \"1\" refused\nset \"1\" refused\nget null refused\nset null refused\n" +
	"name Purple refused\n"

// runKeyedAccessor emits the probe unit, replaces the descriptor runtime with
// descriptor when it is not empty, and returns the probe's output.
func runKeyedAccessor(t *testing.T, descriptor string) string {
	t.Helper()
	u := probeUnit(t, keyedAccessorSchema)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "keyedaccessor", u)
	if descriptor != "" {
		writeFile(t, filepath.Join(dir, "keyedaccessor", "schema_table_descriptor.nut"), []byte(descriptor))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(strings.ReplaceAll(keyedAccessorMain, "%MOD%", mod)))
	return string(sqrun.Run(t, dir, "main.nut", nil))
}

func TestSquirrelKeyedAccessorCoversBothEnds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	if got := runKeyedAccessor(t, ""); got != keyedAccessorWant {
		t.Errorf("keyed accessor:\n got %q\nwant %q", got, keyedAccessorWant)
	}
}

// TestSquirrelKeyedNegativeControls plants three faults in a scratch copy of
// the descriptor runtime, each matched exactly once: the None guard removed,
// the past-Max guard removed, and the key misplaced by one. Each must turn the
// accessor probe red on its own lines.
func TestSquirrelKeyedNegativeControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	runtime := string(squirreltable.Runtime()["schema_table_descriptor.nut"])
	plants := []struct {
		name, anchor, plant, mustSee string
	}{
		{"the None guard removed", `key < 1 || key > max`, `key < 0 || key > max`, "get 0 threw"},
		{"the past-Max guard removed", `key < 1 || key > max`, `key < 1 || key > max + 1`, "get 4 threw"},
		{"the key misplaced by one", `return key - 1;`, `return key % max;`, "key 1 10 slot 30"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			if n := strings.Count(runtime, p.anchor); n != 1 {
				t.Fatalf("the anchor %q appears %d times in the descriptor runtime, want exactly once", p.anchor, n)
			}
			got := runKeyedAccessor(t, strings.Replace(runtime, p.anchor, p.plant, 1))
			if got == keyedAccessorWant {
				t.Fatalf("the plant left the accessor probe green")
			}
			if !strings.Contains(got, p.mustSee) {
				t.Fatalf("the plant turned the probe but not on its own line %q:\n%s", p.mustSee, got)
			}
		})
	}
}

