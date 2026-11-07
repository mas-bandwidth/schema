package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The doc-tags clause of the variable-table row (docs/SPEC-TABLES.md §8.1,
// §8.3): the `///` block and the tags of every table, every table field, every
// table-side union and every union arm ride in the descriptors as metadata a
// tool reads, and they agree with the IR the engine checked, byte for byte.
// The expected lines come from the IR; the observed lines from a Lua probe
// over the generated module.

func docTagsHex(s string) string { return hex.EncodeToString([]byte(s)) }

func docTagsList(tags []string) string {
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = docTagsHex(tag)
	}
	return strings.Join(parts, ",")
}

// docTagsWant is the expected dump of one unit, one line per annotated thing.
func docTagsWant(structs []*ir.Struct, unions []*ir.Union) []string {
	var out []string
	for _, st := range structs {
		name := st.Name
		out = append(out, fmt.Sprintf("table %s doc=%s tags=%s", name, docTagsHex(st.Doc), docTagsList(st.Tags)))
		for _, f := range st.Fields {
			out = append(out, fmt.Sprintf("table %s field %s doc=%s tags=%s", name, f.Name, docTagsHex(f.Doc), docTagsList(f.Tags)))
		}
	}
	for _, un := range unions {
		name := un.Name
		out = append(out, fmt.Sprintf("union %s doc=%s tags=%s", name, docTagsHex(un.Doc), docTagsList(un.Tags)))
		for _, v := range un.Variants {
			out = append(out, fmt.Sprintf("union %s arm %s doc=%s tags=%s", name, v.Name, docTagsHex(v.Doc), docTagsList(v.Tags)))
		}
	}
	return out
}

func docTagsProbe(module string) string {
	return `local T = require("` + module + `")
local function hex(s)
  return (string.gsub(s or "", ".", function(c) return string.format("%02x", string.byte(c)) end))
end
local function tags(list)
  local parts = {}
  for _, tag in ipairs(list or {}) do
    parts[#parts + 1] = hex(tag)
  end
  return table.concat(parts, ",")
end
for _, name in ipairs(T.Registry.tables) do
  local d = T.Tables[name]
  print(string.format("table %s doc=%s tags=%s", name, hex(d.doc), tags(d.tags)))
  for _, f in ipairs(d.fields) do
    print(string.format("table %s field %s doc=%s tags=%s", name, f.name, hex(f.doc), tags(f.tags)))
  end
end
local unions = {}
for name in pairs(T.Unions) do
  unions[#unions + 1] = name
end
table.sort(unions)
for _, name in ipairs(unions) do
  local d = T.Unions[name]
  print(string.format("union %s doc=%s tags=%s", name, hex(d.doc), tags(d.tags)))
  for _, a in ipairs(d.arms) do
    print(string.format("union %s arm %s doc=%s tags=%s", name, a.name, hex(a.doc), tags(a.tags)))
  end
end
`
}

// docTagsDiff runs the probe over files and returns the first difference
// between the IR's expected lines and the observed ones, "" when they agree.
func docTagsDiff(t *testing.T, u *ir.Unit, files map[string][]byte) (irLine, luaLine string) {
	t.Helper()
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(docTagsProbe(ModuleName(u))), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n"), "\n")
	// The tables and unions the module must describe are the closure the
	// emitter walks (every table, its map entries and list elements, and every
	// table-side union); their annotations are the IR's.
	structs, unions := closure(u)
	want := docTagsWant(structs, unions)
	for i := 0; i < len(want) || i < len(got); i++ {
		var w, g string
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return w, g
		}
	}
	return "", ""
}

// TestLuaTableDocAndTagsMatchTheIRFunctional generates every unit of the
// tables conformance manifest and holds the doc and tags of every table,
// field, union and arm to the IR. It runs the pinned interpreter, so it is a
// functional test. A corpus with no annotation at all would pass vacuously,
// so the run must also have seen a table doc, a field tag, a union doc and an
// arm tag.
func TestLuaTableDocAndTagsMatchTheIRFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	units := manifestUnits(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	keys := make([]string, 0, len(units))
	for key := range units {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	compared := 0
	var sawTableDoc, sawFieldTag, sawUnionDoc, sawArmTag bool
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
			continue
		}
		if w, g := docTagsDiff(t, u, files); w != g {
			t.Errorf("unit %s: descriptor doc or tags differ from the IR\n  ir:  %s\n  lua: %s", key, w, g)
		}
		structs, unions := closure(u)
		for _, st := range structs {
			sawTableDoc = sawTableDoc || st.Doc != ""
			for _, f := range st.Fields {
				sawFieldTag = sawFieldTag || len(f.Tags) > 0
			}
		}
		for _, un := range unions {
			sawUnionDoc = sawUnionDoc || un.Doc != ""
			for _, v := range un.Variants {
				sawArmTag = sawArmTag || len(v.Tags) > 0
			}
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("no manifest unit generated a table module")
	}
	if !sawTableDoc || !sawFieldTag || !sawUnionDoc || !sawArmTag {
		t.Errorf("the corpus carries no annotation to hold (table doc %v, field tag %v, union doc %v, arm tag %v)", sawTableDoc, sawFieldTag, sawUnionDoc, sawArmTag)
	}
	t.Logf("%d of %d manifest units compared", compared, len(keys))
}

// TestLuaTableDocAndTagsNegativeControlFunctional drifts one annotation at a
// time in a copy of the generated tabledemo module and requires the IR
// comparison red on that line.
func TestLuaTableDocAndTagsNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	for _, tc := range []struct{ name, from, to, line, column string }{
		{"table-doc", "One weapon's tuning.", "One weapon's drift.", "table WeaponConfig", "doc="},
		{"table-tag", `"designer_facing"`, `"designer_drift"`, "table WeaponConfig", "tags="},
		{"field-doc", " Damage per hit, before armor.", " Damage per hit.", "table WeaponConfig field damage", "doc="},
		{"field-tag", `"ui_slider"`, `"ui_drift"`, "table WeaponConfig field damage", "tags="},
		{"union-doc", "What a weapon does to a target beyond its damage.", "What a weapon does.", "union Effect", "doc="},
		{"arm-tag", `"positive"`, `"negative"`, "union Effect arm buff", "tags="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			mod := ModuleName(u) + ".lua"
			text := string(files[mod])
			if !strings.Contains(text, tc.from) {
				t.Fatalf("the sabotage anchor %q is not in %s", tc.from, mod)
			}
			files[mod] = []byte(strings.Replace(text, tc.from, tc.to, 1))
			w, g := docTagsDiff(t, u, files)
			if w == g {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s drift left the comparison green", tc.name)
			}
			if !strings.HasPrefix(w, tc.line+" doc=") {
				t.Fatalf("NEGATIVE CONTROL FAILED: the comparison went red on the wrong line, not %q:\n  ir:  %s\n  lua: %s", tc.line, w, g)
			}
			col := func(line string) string {
				_, rest, _ := strings.Cut(line, " "+tc.column)
				if tc.column == "doc=" {
					rest, _, _ = strings.Cut(rest, " tags=")
				}
				return rest
			}
			if col(w) == col(g) {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s column is the same on both sides:\n  ir:  %s\n  lua: %s", tc.column, w, g)
			}
		})
	}
}
