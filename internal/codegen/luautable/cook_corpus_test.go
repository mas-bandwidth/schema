package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every committed cook-write row must execute, including maps, nested entry
// extents, pointer numbering, UTF-16 in both orders and sixteen-aligned list
// extents. These used to stop at map_cells before later defects could show.
// Reverse caller-built root maps too: cooking must sort without mutating them.
func TestLuauCookWritesCommittedCorpus(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	instances := map[string]reportRow{}
	for _, row := range manifestRows(t) {
		if row.kind == "instance" {
			instances[row.caseName] = row
		}
	}
	manifest, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cache := map[string]*cookUnit{}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(manifest), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "cook-write" {
			continue
		}
		if len(f) != 4 {
			t.Fatalf("invalid cook-write row: %s", line)
		}
		seen[f[1]] = true
		t.Run(f[1], func(t *testing.T) {
			row, ok := instances[f[1]]
			if !ok {
				t.Fatal("cook-write row has no instance")
			}
			cu := cache[row.unit]
			if cu == nil {
				cu = buildCookUnit(t, units, row.unit)
				cache[row.unit] = cu
			}
			var mods []string
			for name := range cu.g.files {
				if strings.HasSuffix(name, "Table.luau") {
					mods = append(mods, "require("+strconv.Quote("./"+strings.TrimSuffix(name, ".luau"))+")")
				}
			}
			sort.Strings(mods)
			wire, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			probe := strings.Join(mods, "\n") + fmt.Sprintf(`
local St = require("./schema_tables")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
local d = St.lookup(%q)
local r = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = L.loadTolerant(%q, inputs.wire, r)
assert(v ~= nil and not r.malformed and r.refused == 0, "wire refused")
C.BuildVersion = 0x%016xi
for pass = 1, 2 do
  if pass == 2 then
    for _, fd in d.fields do
      if fd.s == "map" then
        local entries = v[fd.slot]
        local reverse = {}
        for i = #entries, 1, -1 do reverse[#reverse + 1] = entries[i] end
        v[fd.slot] = reverse
      end
    end
  end
  local before = {}
  for _, fd in d.fields do
    if fd.s == "map" then before[fd.slot] = table.clone(v[fd.slot]) end
  end
  for order = 1, 2 do
    local n = C.CookMeasure(d, v)
    local b = C.Cook(d, v, order)
    assert(n > 0 and b ~= nil, "cook refused")
    assert(buffer.len(b) == n, "measure differs from write")
    local parts = table.create(n)
    for i = 0, n - 1 do parts[i + 1] = string.format("%%02x", buffer.readu8(b, i)) end
    print(table.concat(parts))
  end
  for slot, entries in before do
    for i, entry in entries do assert(v[slot][i] == entry, "cook mutated map order") end
  end
end
`, row.root, row.root, cu.build)
			out := runProbe(t, cu.g, probe, map[string][]byte{"wire": wire})
			lines := strings.Split(out, "\n")
			if len(lines) != 4 {
				t.Fatalf("expected four writes, got %q", out)
			}
			for i, line := range lines {
				want, err := os.ReadFile(filepath.Join(root, f[2+i%2]))
				if err != nil {
					t.Fatal(err)
				}
				got, err := hex.DecodeString(line)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("pass %d order %d: cook differs from committed %s (%d vs %d bytes)", i/2+1, i%2+1, f[2+i%2], len(got), len(want))
				}
			}
		})
	}
	for _, name := range []string{"map_cells", "map_full", "map_text", "widelist_wide"} {
		if !seen[name] {
			t.Errorf("missing required cook regression %s", name)
		}
	}
}
