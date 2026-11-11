package luatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLuaFileSaveLoadAgreesWithTheEngine holds the plain file save and load
// of every root of tables/examples to the engine (docs/SPEC-TABLES.md §3.4,
// §6.5), through the generated module alone. Each root's testgen samples
// (§16.11) are read from their JSON, saved by save.Save, and the bytes must be
// tablewire.Encode's for the same text: form 3 at a fixed-class root, form 1
// at a variable one, and a refusal where the engine refuses (a fixed table
// past the record ceiling has no file form). Every saved file must load back
// through load.Load and save again to the same bytes.
func TestLuaFileSaveLoadAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")
	repo := filepath.Join("..", "..", "..")
	unit := luarun.LoadUnit(t, filepath.Join(repo, "tables", "examples"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(unit)

	var names []string
	for name, st := range unit.Tables {
		if st.IsMapEntry() || ltgUnreadableUint64(st, map[*ir.Struct]bool{}) != "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var main strings.Builder
	main.WriteString(`local T = require("` + ModuleName(unit) + `")
local load = require("load")
local save = require("save")
local json = require("json")
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
local function row(root, name, text)
  local r = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
  local v = json.FromJson(T, root, text, r)
  if v == nil then print("J", root, name, "-", "-", "-"); return end
  local wire, why = save.Save(T, root, v)
  if wire == nil then print("J", root, name, hex(text), "-", tostring(why)); return end
  local lr = {}
  local back = load.Load(T, root, wire, lr)
  local again = back ~= nil and save.Save(T, root, back) or nil
  print("J", root, name, hex(text), hex(wire), again ~= nil and hex(again) or ("load:" .. tostring(lr.reason)))
end
`)
	samples := 0
	for _, name := range names {
		ss, err := ltgSamples(unit.Tables[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, s := range ss {
			fmt.Fprintf(&main, "row(%q, %q, %s)\n", name, s.name, s.expr)
			samples++
		}
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), main.String())
	out := string(luarun.Run(t, dir, "main.lua", nil))

	fixedRoots, rows, refusals := 0, 0, 0
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[0] != "J" {
			t.Fatalf("the probe printed %q", line)
		}
		root, sample := f[1], f[2]
		if f[3] == "-" {
			t.Errorf("%s/%s: FromJson refused the sample", root, sample)
			continue
		}
		text, _ := hex.DecodeString(f[3])
		def := m.Lookup(root)
		inst := m.New(def)
		var rep tabletext.Report
		if !m.Read(inst, text, &rep) {
			t.Errorf("%s/%s: the engine does not read the sample", root, sample)
			continue
		}
		want, eerr := tablewire.Encode(m, inst)
		rows++
		if eerr != nil {
			if f[4] != "-" {
				t.Errorf("%s/%s: the engine refuses the file save (%v); Lua saved %d bytes", root, sample, eerr, len(f[4])/2)
				continue
			}
			if !strings.Contains(eerr.Error(), f[5]) {
				t.Errorf("%s/%s: Lua refused the save as %s, the engine as %v", root, sample, f[5], eerr)
			}
			refusals++
			continue
		}
		if f[4] == "-" {
			t.Errorf("%s/%s: Lua refused the file save (%s); the engine wrote %d bytes", root, sample, f[5], len(want))
			continue
		}
		got, _ := hex.DecodeString(f[4])
		if !bytes.Equal(got, want) {
			t.Errorf("%s/%s: Lua saves\n    %x\nthe engine\n    %x", root, sample, got, want)
			continue
		}
		if !m.IsVariable(root) {
			fixedRoots++
		}
		if f[5] != f[4] {
			t.Errorf("%s/%s: the saved file does not load back to the same bytes: %s", root, sample, f[5])
		}
	}
	if rows != samples || fixedRoots == 0 || refusals == 0 {
		t.Fatalf("compared %d of %d samples, %d at a fixed-class root, %d refusals", rows, samples, fixedRoots, refusals)
	}
	t.Logf("%d samples over %d roots: %d form-3 files, %d refused saves", rows, len(names), fixedRoots, refusals)
}
