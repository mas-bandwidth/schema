package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaTypeView generates every unit of the tables conformance manifest and
// runs the emitted <Package>View.lua under the pinned interpreter. For every
// `type` the unit declares — a type no table closure reaches included — the
// view's type list and its field columns are the ones internal/viewlisting
// pins from the IR (docs/SPEC-TABLES.md §8.2): name, file, doc and tags on the
// type, and json key, declared type, wire id and optional on every field. A
// type no table reaches carries id 0 and no json key (§8.2).
func TestLuaTypeView(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	keys := make([]string, 0, len(units))
	for key := range units {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	loaded, types, outside := 0, 0, 0
	var refused []string
	for _, key := range keys {
		var paths []string
		for _, p := range units[key] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			if !strings.Contains(err.Error(), "Lua") {
				t.Errorf("unit %s: a refusal that does not name the Lua backend: %v", key, err)
			}
			refused = append(refused, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		view := ViewName(u) + ".lua"
		src, ok := ViewFile(u)
		if !ok {
			t.Errorf("unit %s: the view pass emitted no %s", key, view)
			continue
		}
		files[view] = []byte(src)
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(typeViewProbe(ViewName(u))), 0o644); err != nil {
			t.Fatal(err)
		}
		got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
		want := typeViewPin(u)
		if got != want {
			t.Errorf("unit %s: the Lua type view is not the IR's listing.\n%s", key, firstLine(got, want))
			continue
		}
		loaded++
		types += len(u.Structs)
		closure, _ := closure(u)
		reached := map[string]bool{}
		for _, st := range closure {
			reached[st.Name] = true
		}
		for name := range u.Structs {
			if !reached[name] {
				outside++
			}
		}
	}
	for _, r := range refused {
		t.Logf("refused by name: %s", r)
	}
	if loaded == 0 {
		t.Fatalf("no manifest unit loaded (%d refused)", len(refused))
	}
	t.Logf("%d of %d manifest units load; %d declared types viewed (%d outside every table closure); %d refused by name", loaded, len(keys), types, outside, len(refused))
}

// typeViewPin is the compiler's half of the type view: the `type` lines the
// IR pins (docs/SPEC-TABLES.md §8.7), with every field's columns.
func typeViewPin(u *ir.Unit) string {
	var lines []string
	for line := range strings.SplitSeq(viewlisting.Listing(u), "\n") {
		if strings.HasPrefix(line, "type ") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// firstLine names the first line the probe and the pin disagree about.
func firstLine(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		gl, wl := "", ""
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d:\n  view: %s\n  IR:   %s", i+1, gl, wl)
		}
	}
	return "the listings differ in trailing bytes alone"
}

// typeViewProbe is the Lua half: it walks the view's registry type list in
// name order and prints one line per type and one per field, in the listing's
// shape (docs/SPEC-TABLES.md §8.7). A missing doc is "", and a missing json
// key prints as NULL's "-" (§8.2).
func typeViewProbe(module string) string {
	return `local V = require("` + module + `")
local function flatten(s)
  s = s:gsub("\\", "\\\\")
  s = s:gsub("\n", "\\n")
  return s
end
local function tags(t)
  if t == nil then return "" end
  return table.concat(t, ",")
end
for _, name in ipairs(V.Registry.types) do
  local d = V.Types[name]
  assert(d ~= nil and d.name == name, "no descriptor for " .. name)
  print(string.format("type %s file=%s fields=%d tags=[%s] doc=%s",
    d.name, d.file, #d.fields, tags(d.tags), flatten(d.doc)))
  for _, f in ipairs(d.fields) do
    local json = f.key
    if json == nil or json == "" then json = "-" end
    print(string.format("type %s field %s json=%s type=%s id=%016x optional=%s tags=[%s] doc=%s",
      d.name, f.name, json, f.type, f.id, tostring(f.optional == true), tags(f.tags), flatten(f.doc)))
  end
end
`
}
