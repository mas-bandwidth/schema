package luatable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuaUnitStandsAlone holds docs/PORTING.md M4b for the Lua leg: a
// generated unit stands alone on the platform library. Every unit the tables
// conformance manifest names is generated the way `schema generate --lang lua`
// writes it (the packet modules, the table module and the runtime files the
// unit carries), and one lua process then requires EVERY module in that
// directory with package.path set to the directory alone and package.cpath
// empty: no default search path, no sibling runtime, no C module. A require
// that names anything the unit did not emit fails the load by name.
//
// REVERSED WITNESS: the same unit with one runtime file it requires removed
// must NOT load, so the probe is shown to see a dependency outside the unit
// (a probe that loaded the missing module from anywhere else would pass it).
func TestLuaUnitStandsAlone(t *testing.T) {
	slowtest.Gate(t, "the pinned lua")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	units := manifestUnits(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	probeDir := t.TempDir()
	probe := filepath.Join(probeDir, "standalone_probe.lua")
	if err := os.WriteFile(probe, []byte(standaloneProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(units))
	for key := range units {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		paths := units[key]
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			dir := generateWholeUnit(t, repo, paths)
			out, err := requireEveryModule(t, probe, dir)
			if err != nil {
				t.Fatalf("unit %s does not stand alone: %v\n%s", key, err, out)
			}
			if !strings.Contains(out, "standalone ok") {
				t.Fatalf("unit %s: the probe printed no verdict:\n%s", key, out)
			}
		})
	}

	t.Run("a unit missing one runtime file does not load", func(t *testing.T) {
		t.Parallel()
		dir := generateWholeUnit(t, repo, units["graphdemo"])
		if err := os.Remove(filepath.Join(dir, "descriptor.lua")); err != nil {
			t.Fatalf("the reversed witness needs descriptor.lua in the unit: %v", err)
		}
		out, err := requireEveryModule(t, probe, dir)
		if err == nil {
			t.Fatalf("a unit with descriptor.lua removed still loaded: the probe reaches outside the unit\n%s", out)
		}
		if !strings.Contains(out, "module 'descriptor' not found") {
			t.Fatalf("the unit failed to load, but not by naming the missing module:\n%s", out)
		}
	})
}

// generateWholeUnit writes what compiler/target_lua.go's luaGenerate writes:
// the packet modules and the table module with its runtime, one directory.
func generateWholeUnit(t *testing.T, repo string, paths []string) string {
	t.Helper()
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join(repo, filepath.FromSlash(p)))
	}
	u := luarun.LoadUnit(t, full...)
	files, err := lua.Generate(u)
	if err != nil {
		t.Fatalf("lua.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("luatable.Generate: %v", err)
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			t.Fatalf("generated file %s is claimed twice", name)
		}
		files[name] = data
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	return dir
}

// requireEveryModule runs the probe over dir with the search path confined to
// dir: LUA_PATH has no ";;" (so the default path is not appended), LUA_CPATH
// is empty, and LUA_INIT is cleared so nothing runs before the probe.
func requireEveryModule(t *testing.T, probe, dir string) (string, error) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{probe, dir}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".lua"); ok && !e.IsDir() {
			args = append(args, name)
		}
	}
	cmd := exec.Command(luarun.Interpreter(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LUA_PATH="+filepath.Join(dir, "?.lua"),
		"LUA_PATH_5_4="+filepath.Join(dir, "?.lua"),
		"LUA_CPATH=", "LUA_CPATH_5_4=", "LUA_INIT=", "LUA_INIT_5_4=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// standaloneProbe requires every module named on its command line (every .lua
// file in the unit directory, listed by the test), with the search confined to that directory once more from inside the
// process (package.path and package.cpath are set, not appended to).
const standaloneProbe = `local dir = assert(arg[1], "usage: standalone_probe.lua <unit dir>")
package.path = dir .. "/?.lua"
package.cpath = ""
local names = {}
for i = 2, #arg do names[#names + 1] = arg[i] end
assert(#names > 0, "the unit directory holds no module")
for _, mod in ipairs(names) do
  local ok, err = pcall(require, mod)
  if not ok then
    io.stderr:write("require ", mod, ": ", tostring(err), "\n")
    os.exit(1)
  end
end
print("standalone ok " .. #names)
`

// TestLuaTextWalkIsOneFileAcrossUnits holds docs/PORTING.md M9 for the Lua leg:
// the text form is ONE generic walk over the descriptors, emitted once per unit
// and byte-identical across every unit of the corpus, never a codec per table.
// Every unit the tables conformance manifest names is generated and the text
// walk's files (json.lua, the JSON text form; textform.lua and
// textformwrite.lua, the hand-editable text form) are compared byte for byte
// across all of them.
//
// REVERSED WITNESS: a copy with one byte changed in one unit's json.lua must be
// seen as a second walk by the same comparison.
func TestLuaTextWalkIsOneFileAcrossUnits(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	units := manifestUnits(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	walk := []string{"json.lua", "textform.lua", "textformwrite.lua"}
	generated := map[string]map[string][]byte{}
	for key, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, filepath.FromSlash(p)))
		}
		files, err := Generate(luarun.LoadUnit(t, full...))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		generated[key] = files
	}
	if bad := textWalkCopies(generated, walk); bad != "" {
		t.Fatal(bad)
	}

	// the reversed witness: one changed byte in one unit's copy
	planted := map[string]map[string][]byte{}
	maps.Copy(planted, generated)
	keys := make([]string, 0, len(planted))
	for key := range planted {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	victim := map[string][]byte{}
	maps.Copy(victim, planted[keys[len(keys)-1]])
	changed := append([]byte(nil), victim["json.lua"]...)
	changed[len(changed)-1] ^= 0x20
	victim["json.lua"] = changed
	planted[keys[len(keys)-1]] = victim
	if textWalkCopies(planted, walk) == "" {
		t.Fatal("a json.lua with one byte changed in one unit was not seen as a second text walk")
	}
}

// textWalkCopies names the first walk file that is missing from a unit or
// differs between two units, or returns "" when every unit carries one copy.
func textWalkCopies(generated map[string]map[string][]byte, walk []string) string {
	keys := make([]string, 0, len(generated))
	for key := range generated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, name := range walk {
		var first []byte
		var firstKey string
		for _, key := range keys {
			data, ok := generated[key][name]
			if !ok {
				return "unit " + key + " carries no " + name
			}
			if first == nil {
				first, firstKey = data, key
				continue
			}
			if string(data) != string(first) {
				return name + " differs between units " + firstKey + " and " + key + ": the text form is not one walk"
			}
		}
	}
	return ""
}
