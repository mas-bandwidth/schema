package luautable_test

import (
	"strings"
	"testing"
)

// THE KEYED ACCESSOR (docs/PORTING.md M5). An enum-keyed array is a plain Luau
// array with key k at index k, and the array itself answers nil for None
// (index 0) and for any key past E.Max, so refusing both ends is the
// accessor's job, in one place: schema_tables.luau's S.keyed, whose get and
// set take an integer check and one range check of the key against 1..E.Max.
// The probe prints one row per rule on tabledemo's HullConfig.turrets, a
// [Weapon] array of Cannon, Missile, Mine.

const luauKeyedProbe = `require("./KeyedTable")
local S = require("./schema_tables")
local hull = S.lookup("HullConfig").new()
local acc = S.keyed("HullConfig", "turrets")
local function refuses(f, ...)
	local ok, err = pcall(f, ...)
	return not ok and string.find(tostring(err), "keyed_refused", 1, true) ~= nil
end
local function row(name, ...)
	local parts = {}
	for i = 1, select("#", ...) do parts[i] = tostring((select(i, ...))) end
	print(name .. "\t" .. table.concat(parts, "\t"))
end
row("none refused", refuses(acc.get, hull.Turrets, 0), refuses(acc.set, hull.Turrets, 0, {}))
row("past max refused", refuses(acc.get, hull.Turrets, acc.max + 1), refuses(acc.set, hull.Turrets, acc.max + 1, {}))
row("negative and non-integer refused", refuses(acc.get, hull.Turrets, -1), refuses(acc.get, hull.Turrets, 1.5), refuses(acc.get, hull.Turrets, "1"))
row("max is reachable", not refuses(acc.get, hull.Turrets, acc.max), acc.max == 3)
for key = 1, acc.max do acc.set(hull.Turrets, key, { Damage = key * 10 }) end
local placed = {}
for key = 1, acc.max do
	local turret = acc.get(hull.Turrets, key)
	placed[#placed + 1] = if turret then tostring(turret.Damage) else "nil"
end
row("set places by key", table.concat(placed, ","), hull.Turrets[1].Damage == 10 and hull.Turrets[3].Damage == 30)
local seen = {}
for key, turret in acc.each(hull.Turrets) do seen[#seen + 1] = key .. "=" .. tostring(turret.Damage) end
row("each yields key beside element", table.concat(seen, ","))
row("key by variant name", acc.key("Cannon"), acc.key("Mine"), refuses(acc.key, "Railgun"))
row("a field that is not keyed is refused by name", refuses(S.keyed, "HullConfig", "health"))
`

func luauKeyedRows(t *testing.T, g *generatedUnit) map[string]string {
	t.Helper()
	out := runProbe(t, g, luauKeyedProbe, nil)
	rows := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		name, rest, _ := strings.Cut(line, "\t")
		rows[name] = rest
	}
	return rows
}

func keyedUnit(t *testing.T) *generatedUnit {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "tabledemo")
	if !g.loaded {
		t.Fatalf("tabledemo does not generate: %v", g.err)
	}
	return g
}

func TestLuauKeyedAccessorCoversBothEnds(t *testing.T) {
	t.Parallel()
	rows := luauKeyedRows(t, keyedUnit(t))
	want := map[string]string{
		"none refused":                                 "true\ttrue",
		"past max refused":                             "true\ttrue",
		"negative and non-integer refused":             "true\ttrue\ttrue",
		"max is reachable":                             "true\ttrue",
		"set places by key":                            "10,20,30\ttrue",
		"each yields key beside element":               "1=10,2=20,3=30",
		"key by variant name":                          "1\t3\ttrue",
		"a field that is not keyed is refused by name": "true",
	}
	for name, w := range want {
		if rows[name] != w {
			t.Errorf("%s: got %q, want %q", name, rows[name], w)
		}
	}
}

// TestLuauKeyedNegativeControls is the reversed witness, one plant per rule
// of the method, each of which must turn exactly its own rows: the None guard
// removed, the past-Max guard removed, the key misplaced by one.
func TestLuauKeyedNegativeControls(t *testing.T) {
	t.Parallel()
	g := keyedUnit(t)
	clean := luauKeyedRows(t, g)
	const guard = "not (key >= 1 and key <= n)"
	cases := []struct {
		name, from, to string
		red            []string
	}{
		{"none guard removed", guard, "not (key <= n)", []string{"none refused", "negative and non-integer refused"}},
		{"past max guard removed", guard, "not (key >= 1)", []string{"past max refused"}},
		{"key misplaced by one", "\t\treturn array[key]\n", "\t\treturn array[key - 1]\n", []string{"set places by key"}},
	}
	for _, c := range cases {
		src := string(g.files["schema_tables.luau"])
		if !strings.Contains(src, c.from) {
			t.Fatalf("%s: schema_tables.luau has no %q to plant over; the control would be vacuous", c.name, c.from)
		}
		planted := &generatedUnit{files: map[string][]byte{}, loaded: true}
		for k, v := range g.files {
			planted.files[k] = v
		}
		planted.files["schema_tables.luau"] = []byte(strings.Replace(src, c.from, c.to, 1))
		got := luauKeyedRows(t, planted)
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
