package luatable

import (
	"strings"
	"testing"
)

// THE KEYED ACCESSOR (docs/PORTING.md M5). An enum-keyed array is a plain Lua
// sequence with key k at index k, and the sequence itself answers nil for None
// (index 0) and for any key past E.Max, so refusing both ends is the
// accessor's job, in one place: D.keyed's get and set take an unsigned compare
// of key-1 against E.Max. The probe prints one row per rule on tabledemo's
// HullConfig.turrets, a [Weapon] array of Cannon, Missile, Mine.

const keyedProbe = `local T = require("TabledemoTables")
local hull = T.NewHullConfig()
local acc = T.Keyed("HullConfig", "turrets")
local function refuses(f, ...)
  local ok, err = pcall(f, ...)
  return not ok and tostring(err):find("keyed_refused", 1, true) ~= nil
end
print("none refused", refuses(acc.get, hull.turrets, 0), refuses(acc.set, hull.turrets, 0, {}))
print("past max refused", refuses(acc.get, hull.turrets, acc.max + 1), refuses(acc.set, hull.turrets, acc.max + 1, {}))
print("negative and non-integer refused", refuses(acc.get, hull.turrets, -1), refuses(acc.get, hull.turrets, 1.5), refuses(acc.get, hull.turrets, "1"))
print("max is reachable", not refuses(acc.get, hull.turrets, acc.max), acc.max == 3)
for key = 1, acc.max do acc.set(hull.turrets, key, { damage = key * 10.0 }) end
local placed = {}
for key = 1, acc.max do
  local turret = acc.get(hull.turrets, key)
  placed[#placed + 1] = turret and turret.damage or "nil"
end
print("set places by key", table.concat(placed, ","), hull.turrets[1].damage == 10.0 and hull.turrets[3].damage == 30.0)
local seen = {}
for key, turret in acc.each(hull.turrets) do seen[#seen + 1] = key .. "=" .. turret.damage end
print("each yields key beside element", table.concat(seen, ","))
print("key by variant name", acc.key("Cannon"), acc.key("Mine"), refuses(acc.key, "Railgun"))
print("a field that is not keyed is refused by name", not pcall(T.Keyed, "HullConfig", "health"))
`

// keyedRows runs the probe over the (possibly sabotaged) files and answers
// its rows by name.
func keyedRows(t *testing.T, files map[string][]byte) map[string]string {
	t.Helper()
	out, stderr, err := try(t, files, keyedProbe, nil)
	if err != nil {
		t.Fatalf("keyed probe: %v\n%s", err, stderr)
	}
	rows := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		name, rest, _ := strings.Cut(line, "\t")
		rows[name] = rest
	}
	return rows
}

func TestLuaKeyedAccessorCoversBothEnds(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	rows := keyedRows(t, b.files)
	want := map[string]string{
		"none refused":                                 "true\ttrue",
		"past max refused":                             "true\ttrue",
		"negative and non-integer refused":             "true\ttrue\ttrue",
		"max is reachable":                             "true\ttrue",
		"set places by key":                            "10.0,20.0,30.0\ttrue",
		"each yields key beside element":               "1=10.0,2=20.0,3=30.0",
		"key by variant name":                          "1\t3\ttrue",
		"a field that is not keyed is refused by name": "true",
	}
	for name, w := range want {
		if rows[name] != w {
			t.Errorf("%s: got %q, want %q", name, rows[name], w)
		}
	}
}

// TestLuaKeyedNegativeControls is the reversed witness, one plant per rule of
// the method, each of which must turn exactly its own rows: the None guard
// removed, the past-Max guard removed, the shift misplaced.
func TestLuaKeyedNegativeControls(t *testing.T) {
	t.Parallel()
	b := buildManifestUnit(t, "tabledemo")
	clean := keyedRows(t, b.files)
	const guard = "not math.ult(key - 1, n)"
	cases := []struct {
		name string
		from string
		to   string
		red  []string
	}{
		{"none guard removed", guard, "key > n", []string{"none refused", "negative and non-integer refused"}},
		{"past max guard removed", guard, "key < 1", []string{"past max refused"}},
		{"key misplaced by one", "return array[key]\n", "return array[key - 1]\n", []string{"set places by key"}},
	}
	for _, c := range cases {
		files := b.patched(t, "descriptor.lua", c.from, c.to)
		got := keyedRows(t, files)
		red := map[string]bool{}
		for name := range clean {
			if got[name] != clean[name] {
				red[name] = true
			}
		}
		want := map[string]bool{}
		for _, r := range c.red {
			want[r] = true
		}
		for name := range want {
			if !red[name] {
				t.Errorf("%s: row %q stayed green", c.name, name)
			}
		}
		for name := range red {
			if !want[name] {
				t.Errorf("%s: row %q turned and is not this plant's", c.name, name)
			}
		}
	}
}
