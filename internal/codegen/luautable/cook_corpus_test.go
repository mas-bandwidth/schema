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
		// cook-write <instance> <le> <be> [input=<json>] [share=<source>:<alias>]
		// (test/conformance/README.md, "Cook writes use explicit value
		// construction")
		if len(f) < 4 || len(f) > 6 {
			t.Fatalf("invalid cook-write row: %s", line)
		}
		input, share := "", ""
		for _, option := range f[4:] {
			switch {
			case strings.HasPrefix(option, "input=") && input == "":
				input = strings.TrimPrefix(option, "input=")
			case strings.HasPrefix(option, "share=") && share == "":
				share = strings.TrimPrefix(option, "share=")
			default:
				t.Fatalf("invalid cook-write option %q: %s", option, line)
			}
		}
		if share != "" && input == "" {
			t.Fatalf("a sharing cook-write row names no input: %s", line)
		}
		source, alias, _ := strings.Cut(share, ":")
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
			files := map[string][]byte{}
			// the value: the instance's wire through the one read, or the
			// row's explicit JSON input, with the alias slot then holding the
			// very object the source slot holds (one shared blob)
			construct := fmt.Sprintf(`local v = L.load(%q, inputs.wire, {})
assert(v ~= nil, "wire refused")`, row.root)
			if input != "" {
				text, err := os.ReadFile(filepath.Join(root, input))
				if err != nil {
					t.Fatal(err)
				}
				files["text"] = text
				construct = jsonFlags(cu.g) + fmt.Sprintf(`local J = require("./schema_json")
J.SetFlags(SCHEMA_JSON_FLAGS)
local jr: any = {}
local v = J.FromJson(%q, inputs.text, jr)
assert(v ~= nil, "input refused: " .. tostring(jr.reason))`, row.root)
				if source != "" {
					construct += fmt.Sprintf(`
local slots = {}
for _, fd in d.fields do slots[fd.name] = fd.slot end
assert(slots[%[1]q] ~= nil and slots[%[2]q] ~= nil, "share names no such field")
assert(v[slots[%[1]q]] ~= nil, "share source is empty")
v[slots[%[2]q]] = v[slots[%[1]q]]`, source, alias)
				}
			} else {
				wire, err := os.ReadFile(filepath.Join(root, row.wire))
				if err != nil {
					t.Fatal(err)
				}
				files["wire"] = wire
			}
			probe := strings.Join(mods, "\n") + fmt.Sprintf(`
local St = require("./schema_tables")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
local d = St.lookup(%q)
%s
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
`, row.root, construct, cu.build)
			out := runProbe(t, cu.g, probe, files)
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
