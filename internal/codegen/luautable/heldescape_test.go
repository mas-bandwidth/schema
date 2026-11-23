package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauKindHeldEscapeRefuses (R17: the newer reader reads old data, never
// the other way): kind 31 is held by name and never written, so a reader meets
// it only as damage, exactly as 33 and 34. The shared input carries a kind 31
// field the reader cannot name; the walk, the load, the strict read, the
// retaining walk and the retaining load all refuse the whole load. The old
// reader skipped it by its L and read the file.
func TestLuauKindHeldEscapeRefuses(t *testing.T) {
	slowtest.Gate(t, "Luau (it runs the emitted readers)")
	t.Parallel()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["tblvarevo1"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("local L = require(\"./schema_load\")\nlocal R = require(\"./schema_retain\")\nlocal inputs = require(\"./inputs\")\n")
	for _, mod := range mods {
		fmt.Fprintf(&b, "require(%q)\n", "./"+mod)
	}
	b.WriteString(`local function rep() return {malformed=false, refused=0, unknown=0, widened=0} end
local walk = L.loadWalk("EvoDoc", inputs.wire, rep())
local plain = L.load("EvoDoc", inputs.wire, rep())
local okS, strict = pcall(L.ReadStrict, "EvoDoc", inputs.wire, rep())
local rwalk = R.loadRetainWalk("EvoDoc", inputs.wire, rep())
local rload = R.LoadRetain("EvoDoc", inputs.wire, rep())
print("walk=" .. tostring(walk == nil) .. " load=" .. tostring(plain == nil) .. " strict=" .. tostring(not okS or strict == nil) .. " retainwalk=" .. tostring(rwalk == nil) .. " retain=" .. tostring(rload == nil))
`)
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing/varevo_kind31_escape.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire})))
	const want = "walk=true load=true strict=true retainwalk=true retain=true"
	if got != want {
		t.Errorf("kind 31 at an unnamed field: got %q, want every load refused (%q)", got, want)
	}
}
