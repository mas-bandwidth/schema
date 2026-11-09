package luatable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func parseRetainManifestRows(t *testing.T, manifest string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Accept both the 7-field derived manifest line:
	//   retain <case> <unit> <root> <wire> <capacity> <ids>
	// and the 8-field MANIFEST.txt line:
	//   retain <case> <unit> <root> <wire> <capacity> <ids> <save|->
	rows := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if (len(f) == 7 || len(f) == 8) && f[0] == "retain" {
			rows[f[1]] = f
		}
	}
	return rows
}

func runRetainIdCapacityRow(t *testing.T, repo string, units map[string][]string, f []string, patch func(map[string][]byte)) string {
	t.Helper()
	var paths []string
	for _, p := range units[f[2]] {
		paths = append(paths, filepath.Join(repo, p))
	}
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		patch(files)
	}
	wire, err := os.ReadFile(filepath.Join(repo, f[4]))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repo, f[7]))
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
local report = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0, retained = 0, retainLost = 0 }
local store = { list = {}, used = 0, idCapacity = %s }
local v = retain.LoadRetain(T, %q, inputs.wire, store, report)
assert(v ~= nil, "the retaining load refused")
local size = retain.MeasureRetain(T, %q, v, store)
local save = { damaged = false, refused = 0, retained = 0, retainLost = 0 }
local out = retain.SaveRetain(T, %q, v, store, save)
assert(out ~= nil, "the retaining save refused")
assert(size == #out, "MeasureRetain " .. tostring(size) .. ", the save wrote " .. #out)
print(tostring(out == inputs.want))
`, ModuleName(u), ids, f[3], f[3], f[3])
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire, "want": want})))
}

// TestLuaRetainIdCapacity holds the Lua retaining save to THE RETAINED IDS
// (docs/SPEC-TABLES.md §6.6): the caller's id list is declared by capacity, an
// id this build cannot name takes one entry of it and a repeat takes none, and
// a record whose id has no entry is DROPPED, counted as exactly one
// retain_lost, while the save goes on. The expectation is the manifest's own
// pair of retain rows over one wire (testdata/conformance/tables/MANIFEST.txt):
// retain_round_trip with a full id list and retain_ids_short with two entries,
// and the C++-written saves (the verdict and the bytes are the contract; the
// counters are diagnostics, testdata/conformance/tables/FORMAT.md).
func TestLuaRetainIdCapacity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := parseRetainManifestRows(t, manifest)
	for _, name := range []string{"retain_round_trip", "retain_ids_short"} {
		f, ok := rows[name]
		if !ok {
			t.Fatalf("the manifest has no %s row", name)
		}
		// Verify that a 7-field derived manifest line parses identically before
		// pairing with its save path.
		derived := filepath.Join(t.TempDir(), "DERIVED.txt")
		if err := os.WriteFile(derived, []byte(strings.Join(f[:7], " ")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := parseRetainManifestRows(t, derived)[name]; len(got) != 7 {
			t.Fatalf("7-field retain line parsed %d fields, want 7", len(got))
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := runRetainIdCapacityRow(t, repo, units, f, nil)
			if got != "true" {
				t.Errorf("%s: the retaining save is not the C++ save %s byte for byte (%s)", name, f[7], got)
			}
		})
	}
}

// TestLuaRetainIdCapacityNegativeControl is the reversed witness for
// TestLuaRetainIdCapacity: a save that ignores retain.idCapacity places every
// record, so retain_ids_short writes the full row's bytes instead of the
// short save and goes red.
func TestLuaRetainIdCapacityNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := parseRetainManifestRows(t, manifest)
	f, ok := rows["retain_ids_short"]
	if !ok {
		t.Fatal("the manifest has no retain_ids_short row")
	}
	const anchor = "  if retain ~= nil then ids.idCapacity = retain.idCapacity end\n"
	got := runRetainIdCapacityRow(t, repo, units, f, func(files map[string][]byte) {
		src := string(files["retain.lua"])
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("retain.lua matched anchor %d times, want 1", n)
		}
		cloned := maps.Clone(files)
		files["retain.lua"] = []byte(strings.Replace(src, anchor, "", 1))
		_ = cloned
	})
	if got == "true" {
		t.Fatal("negative control: retain_ids_short stayed green with idCapacity ignored")
	}
}
