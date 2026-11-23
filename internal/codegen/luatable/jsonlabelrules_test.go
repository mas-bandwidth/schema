package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuaJsonLabelRules holds docs/SPEC-TABLES.md §16.7's label rules in the
// Lua JSON read, placed and skipped, against the conformance corpus's
// graphdemo rows (testdata/conformance/tables/MANIFEST.txt, json-hostile): a
// label is followed by a comma or the object's close, never the next key; a
// skipped `{"&node": N}` alone refers, so N must be defined earlier and
// closed; a skipped `&node` with fields defines, so N must be new to the
// whole text. The verdict is the contract, so no reason is compared.
func TestLuaJsonLabelRules(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ name, root, verdict string }
	var rows []row
	inputs := map[string][]byte{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "json-hostile" || f[2] != "graphdemo" {
			continue
		}
		if !strings.HasPrefix(f[1], "label-") && !strings.HasPrefix(f[1], "skipped-") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repo, f[4], f[3]+".json"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[f[1]] = body
		rows = append(rows, row{name: f[1], root: f[3], verdict: f[5]})
	}
	for _, must := range []string{"label-missing-comma", "skipped-reference-undefined", "skipped-label-defined-twice"} {
		if _, ok := inputs[must]; !ok {
			t.Fatalf("the manifest has no graphdemo json-hostile row %s", must)
		}
	}

	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate tables/pointers: %v", err)
	}
	runDir := t.TempDir()
	luarun.WriteFiles(t, runDir, files)
	var probe strings.Builder
	probe.WriteString(`local T = require("` + ModuleName(u) + `")
local J = require("json")
local inputs = require("inputs")
local function verdict(name, root)
  local report = {}
  local ok, v = pcall(J.FromJson, T, root, inputs[name], report)
  if not ok then return "error: " .. tostring(v) end
  if v ~= nil and report.refused == 0 then return "read" end
  return "refused"
end
`)
	for _, r := range rows {
		probe.WriteString(`print("` + r.name + `=" .. verdict("` + r.name + `", "` + r.root + `"))` + "\n")
	}
	if err := os.WriteFile(filepath.Join(runDir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luarun.Run(t, runDir, "main.lua", inputs)), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	for _, r := range rows {
		if got[r.name] != r.verdict {
			t.Errorf("%s (root %s): the Lua JSON read answered %q, want %q", r.name, r.root, got[r.name], r.verdict)
		}
	}
}
