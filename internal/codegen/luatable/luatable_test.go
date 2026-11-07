package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaTableDescriptorsLoad generates every unit of the tables conformance
// manifest (testdata/conformance/tables/MANIFEST.txt) for Lua, loads each
// table module under the pinned interpreter, and for every table in the
// unit's closure checks the descriptor the module carries against the IR
// (type id; each field's wire id and wire kind, in declaration order) and
// constructs the table, walking the value against its own descriptor: every
// field present in the shape its descriptor names, at its declared default
// (docs/PORT-LUA.md, THE TYPE MAPPING; docs/SPEC-TABLES.md §8.1). Every root a
// manifest line names must be in its unit's registry. A unit the target
// refuses is listed with its refusal, which must name the unit's fields.
func TestLuaTableDescriptorsLoad(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	units, roots := manifest(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	loaded, tables := 0, 0
	var refused []string
	for _, key := range keys {
		var paths []string
		for _, p := range units[key] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		if len(ir.TableWideFields(u)) > 0 {
			refused = append(refused, fmt.Sprintf("%s: carries the wide kinds the descriptor walk does not spell yet", key))
			continue
		}
		files, err := Generate(u)
		if err != nil {
			if !strings.Contains(err.Error(), "Lua") {
				t.Errorf("unit %s: a refusal that does not name the Lua backend: %v", key, err)
			}
			refused = append(refused, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe, want := probeUnit(u, roots[key])
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
		if out != want {
			t.Errorf("unit %s:\nlua:\n%s\nwant:\n%s", key, out, want)
			continue
		}
		loaded++
		tables += len(u.Tables)
	}
	for _, r := range refused {
		t.Logf("refused by name: %s", r)
	}
	if loaded == 0 {
		t.Fatalf("no manifest unit loaded (%d refused)", len(refused))
	}
	t.Logf("%d of %d manifest units load; %d declared tables constructed; %d refused by name", loaded, len(keys), tables, len(refused))
}

// manifest reads the unit lines (key -> paths) and, per unit, every root a
// row names (instance, report, cook and retain rows put it fourth).
func manifest(t *testing.T, path string) (map[string][]string, map[string]map[string]bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	roots := map[string]map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "unit":
			units[f[1]] = f[2:]
		case "instance", "report", "cook", "retain":
			if len(f) >= 4 {
				if roots[f[2]] == nil {
					roots[f[2]] = map[string]bool{}
				}
				roots[f[2]][f[3]] = true
			}
		}
	}
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	return units, roots
}

// probeUnit writes the Lua probe for one unit and the output it must print.
func probeUnit(u *ir.Unit, roots map[string]bool) (string, string) {
	var p strings.Builder
	var want []string
	p.WriteString(walker)
	fmt.Fprintf(&p, "local T = require(%q)\n", ModuleName(u))
	fmt.Fprintf(&p, "assert(T.RegionAlign == %d and T.Registry.region_align == %d, \"region_align\")\n", ir.TableRegionAlign(u), ir.TableRegionAlign(u))
	closure, _ := closure(u)
	for _, st := range closure {
		var fields []string
		for _, f := range st.Fields {
			fields = append(fields, fmt.Sprintf("%016x:%d", ir.TableFieldWireId(f), ir.TableWireFieldKind(f)))
		}
		layout := ir.RecordLayout(u, st)
		fmt.Fprintf(&p, "describe(T, %q, %d, %d)\n", st.Name, layout.Size, layout.Align)
		want = append(want, fmt.Sprintf("table %s %016x %d:%d [%s] ok", st.Name, ir.TableWireId(st.WireName()), layout.Size, layout.Align, strings.Join(fields, " ")))
	}
	names := make([]string, 0, len(roots))
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	for _, r := range names {
		fmt.Fprintf(&p, "root(T, %q)\n", r)
		want = append(want, "root "+r+" registered")
	}
	return p.String(), strings.Join(want, "\n")
}

// walker is the probe's Lua half: describe prints a table's descriptor facts
// and constructs it, checking the value with the runtime's own descriptor walk
// (runtime/descriptor.lua); root checks the registry names a manifest root.
const walker = `
local D = require("descriptor")
function describe(mod, name, size, align)
  local d = mod.Tables[name]
  assert(d ~= nil and d.name == name, "no descriptor for " .. name)
  assert(d.size == size and d.align == align and D.record_size(d) == size, name .. ": record size/align")
  assert(D.region_align(mod, d) == mod.RegionAlign, name .. ": region_align")
  local ids = {}
  for i, f in ipairs(d.fields) do
    assert(f.ordinal == i, name .. "." .. f.name .. ": ordinal out of place")
    assert(D.field(d, f.name) == f and D.by_id(d, f.id) ~= nil, name .. "." .. f.name .. ": the walk does not find it")
    ids[#ids + 1] = string.format("%016x:%d", f.id, f.kind)
  end
  local err = D.check_defaults(mod, name, mod["New" .. name]())
  if err then error(err) end
  print(string.format("table %s %016x %d:%d [%s] ok", name, d.type_id, d.size, d.align, table.concat(ids, " ")))
end
function root(mod, name)
  local found = false
  for _, n in ipairs(mod.Registry.tables) do if n == name then found = true end end
  assert(found, "root " .. name .. " is not in the registry")
  print("root " .. name .. " registered")
end
`
