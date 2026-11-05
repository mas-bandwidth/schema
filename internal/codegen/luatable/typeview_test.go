package luatable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaTypeView generates every unit of the tables conformance manifest and
// runs the emitted <Package>View.lua under the pinned interpreter. For every
// `type` the unit declares — a type no table closure reaches included — the
// view\x27s type list and its field columns are the ones internal/viewlisting
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
			t.Errorf("unit %s: the Lua type view is not the IR\x27s listing.\n%s", key, firstLine(got, want))
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

// typeViewPin is the compiler\x27s half of the type view: the `type` lines the
// IR pins (docs/SPEC-TABLES.md §8.7), with every field\x27s columns.
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

// typeViewProbe is the Lua half: it walks the view\x27s registry type list in
// name order and prints one line per type and one per field, in the listing\x27s
// shape (docs/SPEC-TABLES.md §8.7). A missing doc is "", and a missing json
// key prints as NULL\x27s "-" (§8.2).
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

type luaViewUnit struct {
	key   string
	paths []string
}

func luaViewUnits(t *testing.T) []luaViewUnit {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	makefile, err := os.ReadFile(filepath.Join(repo, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(makefile))
	if len(entries) == 0 {
		t.Fatal("the Makefile declares no VIEW_CORPUS entries")
	}
	var out []luaViewUnit
	for _, entry := range entries {
		dir, _, ok := strings.Cut(entry, ":")
		if !ok {
			t.Fatalf("VIEW_CORPUS entry %q is not dir:package", entry)
		}
		source := filepath.Join(repo, "tables", dir)
		if dir == "wide" {
			source = filepath.Join(repo, "examples-wide")
		}
		out = append(out, luaViewUnit{"corpus-" + dir, []string{source}})
	}
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var paths []string
		for _, p := range units[key] {
			paths = append(paths, filepath.Join(repo, p))
		}
		out = append(out, luaViewUnit{"manifest-" + key, paths})
	}
	return out
}

func unitViewProbe(module string) string {
	return `local V = require("` + module + `")
local v = V.UnitView()
assert(v == V.Registry, "UnitView() must return Registry")
local out = {}
local function put(...)
  local args = { ... }
  for i = 1, #args do out[#out + 1] = tostring(args[i]) end
end
local function hex16(id)
  return string.format("%016x", id)
end
local function ann(doc, tags)
  local flat = string.gsub(doc or "", "\\", "\\\\")
  flat = string.gsub(flat, "\n", "\\n")
  put(" tags=[", table.concat(tags or {}, ","), "] doc=", flat, "\n")
end
local function yes(b)
  return b and "yes" or "no"
end
local function vocabulary(what, row, set)
  for _, e in ipairs(set) do
    put(what, " ", e.name, " file=", e.file, " max=", e.max, " bits=", e.storage_bits, " variants=", #e.variants)
    ann(e.doc, e.tags)
    for _, r in ipairs(e.variants) do
      put(what, " ", e.name, " ", row, " ", r.value, " ", r.name, " id=", hex16(r.id))
      ann(r.doc, r.tags)
    end
  end
end
local function held(name)
  for _, tname in ipairs(v.tables) do
    for _, f in ipairs(V.Tables[tname].fields) do
      if f.target == name then return true end
    end
  end
  for _, tname in ipairs(v.types) do
    for _, f in ipairs(V.Types[tname].fields) do
      if f.target == name then return true end
    end
  end
  return false
end
local function unions()
  for _, u in ipairs(v.unions) do
    put("union ", u.name, " file=", u.file, " max=", u.max, " bits=", u.storage_bits, " variants=", #u.variants)
    ann(u.doc, u.tags)
    local h = held(u.name)
    for _, a in ipairs(u.variants) do
      put("union ", u.name, " arm ", a.value, " ", a.name, " id=", hex16(a.id), " payload=", a.payload_name or "-")
      put(" record=", yes(a.payload ~= nil), " field=", yes(a.field ~= nil))
      local f = a.field
      if f ~= nil then
        put(" kind=", f.kind, " bound=", f.bound or 0, " overlay=")
        if h then
          put(f.arm_offset - u.payload_offset)
        else
          put("-")
        end
      else
        put(" kind=- bound=- overlay=-")
      end
      ann(a.doc, a.tags)
    end
  end
end
local function declarations(what, names, dict)
  for _, name in ipairs(names) do
    local d = dict[name]
    put(what, " ", d.name, " file=", d.file, " fields=", #d.fields)
    ann(d.doc, d.tags)
    for _, f in ipairs(d.fields) do
      local json = f.key
      if json == nil or json == "" then json = "-" end
      put(what, " ", d.name, " field ", f.name, " json=", json)
      put(" type=", f.type_name, " id=", hex16(f.id), " optional=", tostring(f.optional == true))
      ann(f.doc, f.tags)
    end
  end
end
put("unit package=", v.package, " protocol=", hex16(v.protocol_id), "\n")
for _, c in ipairs(v.constants) do
  put("constant ", c.name, " file=", c.file, " type=", c.type_name, " float=", tostring(c.is_float == true), " int=", string.format("%d", c.int_value), " real=", hex16(c.float_bits))
  ann(c.doc, c.tags)
end
vocabulary("enum", "variant", v.enums)
vocabulary("flags", "bit", v.flags)
unions()
declarations("type", v.types, V.Types)
declarations("table", v.tables, V.Tables)
io.write(table.concat(out))
`
}

func runUnitView(t *testing.T, files map[string][]byte, printer string) string {
	t.Helper()
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(printer), 0o644); err != nil {
		t.Fatal(err)
	}
	return string(luarun.Run(t, dir, "main.lua", nil))
}

// TestLuaUnitViewCorpusFunctional holds UnitView() over all 50 units (15 VIEW_CORPUS +
// 35 tables manifest) byte-for-byte against internal/viewlisting.
func TestLuaUnitViewCorpusFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	units := luaViewUnits(t)
	listed := 0
	for _, vu := range units {
		u := luarun.LoadUnit(t, vu.paths...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: %v", vu.key, err)
		}
		src, ok := ViewFile(u)
		if !ok {
			t.Fatalf("%s: ViewFile returned false", vu.key)
		}
		files[ViewName(u)+".lua"] = []byte(src)
		listed++
		t.Run(vu.key, func(t *testing.T) {
			t.Parallel()
			pin := viewlisting.Listing(u)
			got := runUnitView(t, files, unitViewProbe(ViewName(u)))
			if got != pin {
				t.Errorf("unit view listing differs from internal/viewlisting.\n%s", firstLine(got, pin))
			}
		})
	}
	if listed != len(units) {
		t.Fatalf("listed %d of %d units", listed, len(units))
	}
	t.Logf("%d of %d units listed from UnitView()", listed, len(units))
}

// TestLuaUnitViewNegativeControlFunctional breaks each claim the corpus gate holds, one
// at a time in a copy of the generated text, and requires the gate RED on the
// line that claim owns (docs/SPEC-TABLES.md §8.7).
func TestLuaUnitViewNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	load := func(t *testing.T, rel string) (*ir.Unit, map[string][]byte) {
		t.Helper()
		u := luarun.LoadUnit(t, filepath.Join(repo, rel))
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		src, ok := ViewFile(u)
		if !ok {
			t.Fatal("no ViewFile")
		}
		files[ViewName(u)+".lua"] = []byte(src)
		return u, files
	}
	red := func(t *testing.T, u *ir.Unit, files map[string][]byte, printer, expect string) {
		t.Helper()
		pin := viewlisting.Listing(u)
		got := runUnitView(t, files, printer)
		if got == pin {
			t.Fatal("NEGATIVE CONTROL FAILED: the sabotage left the listing equal to the pin")
		}
		diff := firstLine(got, pin)
		if !strings.Contains(diff, expect) {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on %q:\n%s", expect, diff)
		}
	}
	patch := func(t *testing.T, files map[string][]byte, file string, re *regexp.Regexp, repl func(string) string) {
		t.Helper()
		data, ok := files[file]
		if !ok {
			t.Fatalf("no generated %s", file)
		}
		loc := re.FindIndex(data)
		if loc == nil {
			t.Fatalf("the sabotage anchor %s is not in %s", re, file)
		}
		files[file] = []byte(string(data[:loc[0]]) + repl(string(data[loc[0]:loc[1]])) + string(data[loc[1]:]))
	}
	t.Run("unflattened", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		printer := unitViewProbe(ViewName(u))
		anchor := `flat = string.gsub(flat, "\n", "\\n")`
		if !strings.Contains(printer, anchor) {
			t.Fatal("the flattening anchor is not in the printer")
		}
		red(t, u, files, strings.Replace(printer, anchor, "", 1), `\n`)
	})
	t.Run("drop", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		patch(t, files, ViewName(u)+".lua", regexp.MustCompile(`tables = \{\s*"[^"]+",?`), func(string) string {
			return "tables = {"
		})
		red(t, u, files, unitViewProbe(ViewName(u)), "table ")
	})
	t.Run("arm-offset", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/arms")
		patch(t, files, ViewName(u)+".lua", regexp.MustCompile(`name = "plain",[\s\S]*?arm_offset = \d+`), func(s string) string {
			i := strings.LastIndex(s, " ")
			n, _ := strconv.Atoi(s[i+1:])
			return s[:i+1] + strconv.Itoa(n+1)
		})
		red(t, u, files, unitViewProbe(ViewName(u)), "overlay=1")
	})
	t.Run("doc", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "tables/examples")
		patch(t, files, ViewName(u)+".lua", regexp.MustCompile(`name = "damage",[\s\S]*?doc = "[^"]+"`), func(s string) string {
			return s[:strings.LastIndex(s, `doc = "`)] + "doc = _M.DocNone"
		})
		red(t, u, files, unitViewProbe(ViewName(u)), "doc=")
	})
	t.Run("id", func(t *testing.T) {
		t.Parallel()
		u, files := load(t, "test/tables/CsView.schema")
		patch(t, files, ViewName(u)+".lua", regexp.MustCompile(`name = "grade",\s*id = 0x0000000000000000`), func(s string) string {
			return strings.Replace(s, "0x0000000000000000", "0x0000000000000001", 1)
		})
		red(t, u, files, unitViewProbe(ViewName(u)), "Orphan field grade")
	})
}

// TestLuaUnitViewPacketStorageFunctional holds a type no table reaches to §8.2: it has a
// descriptor in the view module, its fields carry no table-wire identity (id 0,
// no json key), an unreached enum carries id 0 on every variant, a reached type
// keeps its ids, and nothing outside the view module requires it (§8.4).
func TestLuaUnitViewPacketStorageFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Probe.schema")
	const src = `package probe

enum Hidden { A, B }

type Child
{
    n int32 = 7
}

union Choice
{
    a Child
    b Child
}

type Shared
{
    choice Choice
    past uint32 = 19
}

type Packet
{
    nested Shared
    hidden Hidden
    text string(8) = "ready"
}

table Root
{
    shared Shared
    next *Root
}
`
	if err := os.WriteFile(schemaPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schemaPath)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	viewSrc, ok := ViewFile(u)
	if !ok {
		t.Fatal("the unit has no ProbeView.lua")
	}
	for name, data := range files {
		if strings.Contains(string(data), "NewPacket") {
			t.Errorf("%s: a viewed type gained a table constructor", name)
		}
		if strings.Contains(string(data), "ProbeView") {
			t.Errorf("%s: names the view module, which is on the side (docs/SPEC-TABLES.md §8.4)", name)
		}
	}
	files["ProbeView.lua"] = []byte(viewSrc)
	luarun.WriteFiles(t, dir, files)
	probe := `local V = require("ProbeView")
local v = V.UnitView()
local p = V.Types["Packet"]
assert(p ~= nil and #p.fields == 3, "Packet has three fields")
assert(p.type_id == 0, "an outside type carries type_id 0")
assert(p.fields[1].id == 0 and p.fields[1].key == nil, "an outside field carries no wire identity")
assert(p.fields[3].id == 0 and p.fields[3].key == nil, "an outside text field carries no wire identity")
local s = V.Types["Shared"]
assert(s ~= nil and s.type_id ~= 0, "a reached type keeps its type_id")
assert(s.fields[2].id ~= 0 and s.fields[2].key ~= nil, "a reached type keeps its wire identity")
assert(#v.types == 3 and #v.tables == 1 and #v.enums == 1 and #v.unions == 1, "registry counts")
assert(#v.enums[1].variants == 3 and v.enums[1].variants[2].id == 0 and v.enums[1].variants[3].id == 0, "an unreached enum registry rows carry id 0")
assert(v.unions[1].variants[2].id ~= 0, "a reached union arms keep their ids")
assert(v.unions[1].variants[2].payload == V.Types["Child"], "a record arm names its payload descriptor")
print("OK")
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	if out != "OK" {
		t.Fatalf("packet storage probe printed:\n%s", out)
	}
}

// TestLuaEmptyDocIsOneDefinitionFunctional holds §8.7\x27s ABSENCE IS ONE SHARED EMPTY
// STRING on the emitted text: every module defines _M.DocNone = "" once, every
// row with no doc names _M.DocNone, and no generated module writes an inline
// doc = "" or tags = {}.
func TestLuaEmptyDocIsOneDefinitionFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	scan := func(files map[string][]byte) (refs int, inline []string) {
		for _, name := range sortedKeys(mapStrings(files)) {
			if !strings.HasSuffix(name, "Tables.lua") && !strings.HasSuffix(name, "View.lua") {
				continue
			}
			data := string(files[name])
			if n := strings.Count(data, `_M.DocNone = ""`); n != 1 {
				inline = append(inline, fmt.Sprintf("%s: defines _M.DocNone %d times", name, n))
			}
			refs += strings.Count(data, "doc = _M.DocNone")
			desc := strings.Replace(data, `_M.DocNone = ""`, "", 1)
			if start := strings.Index(desc, "\nfunction _M.New"); start >= 0 {
				if end := strings.Index(desc[start:], "\n-- the unit registry"); end >= 0 {
					desc = desc[:start] + desc[start+end:]
				}
			}
			if strings.Contains(desc, `doc = ""`) || strings.Contains(desc, "tags = {  }") || strings.Contains(desc, "tags = {}") {
				inline = append(inline, name)
			}
		}
		return refs, inline
	}
	total := 0
	var sample map[string][]byte
	for _, vu := range luaViewUnits(t) {
		u := luarun.LoadUnit(t, vu.paths...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: %v", vu.key, err)
		}
		if viewSrc, ok := ViewFile(u); ok {
			files[ViewName(u)+".lua"] = []byte(viewSrc)
		}
		refs, inline := scan(files)
		if len(inline) > 0 {
			t.Errorf("unit %s: %v write an empty doc or tag list inline rather than the one definition", vu.key, inline)
		}
		total += refs
		if sample == nil && refs > 0 {
			sample = files
		}
	}
	if total == 0 || sample == nil {
		t.Fatal("no row names _M.DocNone; the scan is reading the wrong text")
	}
	broken := map[string][]byte{}
	maps.Copy(broken, sample)
	for name, data := range broken {
		if strings.Contains(string(data), "doc = _M.DocNone") {
			broken[name] = []byte(strings.Replace(string(data), "doc = _M.DocNone", `doc = ""`, 1))
			break
		}
	}
	if _, inline := scan(broken); len(inline) != 1 {
		t.Fatalf("NEGATIVE CONTROL FAILED: an inline empty doc was not seen (%v)", inline)
	}
	t.Logf("%d rows name the one empty doc", total)
}

func mapStrings(m map[string][]byte) map[string]string {
	out := make(map[string]string, len(m))
	for k := range m {
		out[k] = ""
	}
	return out
}
