package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaRetainIdCapacity holds the Lua retaining save to THE RETAINED IDS
// (docs/SPEC-TABLES.md §6.6): the caller's id list is declared by capacity, an
// id this build cannot name takes one entry of it and a repeat takes none, and
// a record whose id has no entry is DROPPED, counted as exactly one
// retain_lost, while the save goes on. The expectation is the manifest's own
// pair of retain rows over one wire (testdata/conformance/tables/MANIFEST.txt):
// retain_round_trip with a full id list and retain_ids_short with two entries,
// their counters and the C++-written saves.
//
// REVERSED WITNESS: a save that ignores retain.idCapacity places every record,
// so the short row's save is the full row's bytes and its save-side loss is 0.
func TestLuaRetainIdCapacity(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// retain <case> <unit> <root> <wire> <capacity> <ids> <counters> <save lost> <saves>
	rows := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 10 && f[0] == "retain" {
			rows[f[1]] = f
		}
	}
	for _, name := range []string{"retain_round_trip", "retain_ids_short"} {
		f, ok := rows[name]
		if !ok {
			t.Fatalf("the manifest has no %s row", name)
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var paths []string
			for _, p := range units[f[2]] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := os.ReadFile(filepath.Join(repo, f[4]))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(repo, f[9]))
			if err != nil {
				t.Fatal(err)
			}
			ids := "nil"
			if f[6] != "full" {
				ids = f[6]
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			probe := fmt.Sprintf(`local T = require(%q)
local retain = require("retain")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local store = { list = {}, used = 0, idCapacity = %s }
local v = retain.LoadRetain(T, %q, inputs.wire, store, report)
assert(v ~= nil, "the retaining load refused")
local loaded = string.format("%%d,%%d,%%d", report.retained, report.retainLost, report.unknown)
local size = retain.MeasureRetain(T, %q, v, store)
local save = { malformed = false, refused = 0, retained = 0, retainLost = 0 }
local out = retain.SaveRetain(T, %q, v, store, save)
assert(out ~= nil, "the retaining save refused")
assert(size == #out, "MeasureRetain " .. tostring(size) .. ", the save wrote " .. #out)
print(loaded .. " " .. save.retainLost .. " " .. tostring(out == inputs.want))
`, ModuleName(u), ids, f[3], f[3], f[3])
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire, "want": want})))
			if expect := f[7] + " " + f[8] + " true"; got != expect {
				t.Errorf("%s: got %q, want %q (counters, save-side loss, the C++ save byte for byte)", name, got, expect)
			}
		})
	}
}
