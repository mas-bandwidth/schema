package mootable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE CONFORMANCE SURFACES, OVER THE MOO TABLE WIRE (test/conformance/README.md):
//
//	json-read    FromJson json/<name>.json, Save, the bytes   == the wire golden
//	json-write   Load the wire golden, ToJson, the text       == json/<name>.json
//
// Load and Save are the tables layer's (runtime/table_load.moo, table_save.moo);
// this test is the text form's half of the pair.
//
// wireOwed is each unit whose wire half is not yet the value mapping's, with
// the defect, owned by the tables layer: the test requires each to still
// fail, so the list cannot outlive the fix.
var wireOwed = map[string]string{}

func TestMooJsonWireSurfaces(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form over the moo wire)")
	t.Parallel()
	m := readTextManifest(t)
	byUnit := map[string][]textInstance{}
	for _, in := range m.instances {
		byUnit[in.unit] = append(byUnit[in.unit], in)
	}
	total := 0
	for _, key := range sortedKeys(byUnit) {
		u := loadUnit(t, m.units[key])
		if !unitCarriesJson(u) {
			continue
		}
		dir := generateUnitDir(t, u, nil)
		inputs := map[string][]byte{}
		var b strings.Builder
		b.WriteString(probePrelude)
		b.WriteString(`local Save: any = require("./table_save")
local Load: any = require("./table_load")
local function run(mod: any, root: string, text: buffer, wire: buffer)
	local d = mod[root .. "Descriptor"]
	local value = mod["New" .. root]()
	local ok = mod[root .. "FromJson"](value, text, {})
	local bytes, why = Save.save(d, value)
	if not ok then
		hexs("read refused")
	elseif bytes == nil then
		hexs("save refused: " .. tostring(why and why.name))
	else
		hexs(buffer.tostring(bytes))
	end
	local loaded = Load.loadTolerant(root, wire, Load.newReport())
	if loaded == nil then
		hexs("load refused")
		return
	end
	local size = mod[root .. "ToJsonMeasure"](loaded)
	if size < 0 then
		hexs("write refused")
		return
	end
	local out = buffer.create(size)
	mod[root .. "ToJson"](loaded, out)
	hexs(buffer.tostring(out))
end
`)
		for i, in := range byUnit[key] {
			wire, err := os.ReadFile(filepath.Join(repoRoot, in.wire))
			if err != nil {
				t.Fatal(err)
			}
			inputs[fmt.Sprintf("t%d", i)] = readGolden(t, in.name)
			inputs[fmt.Sprintf("w%d", i)] = wire
			mod := tableModule(u, in.root)
			fmt.Fprintf(&b, "run(require(\"./%s\"), %q, inputs.t%d, inputs.w%d)\n", mod, in.root, i, i)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := moorun.RunErr(t, dir, "main.moo", inputs)
		if why, owed := wireOwed[key]; owed {
			if err == nil {
				t.Errorf("unit %s now runs: remove it from wireOwed (%s)", key, why)
			} else {
				t.Logf("owed by the tables layer, unit %s: %s", key, why)
			}
			continue
		}
		if err != nil {
			t.Errorf("unit %s: %v", key, err)
			continue
		}
		lines := decodeHexLines(t, out)
		if len(lines) != 2*len(byUnit[key]) {
			t.Errorf("unit %s: %d lines for %d instances", key, len(lines), len(byUnit[key]))
			continue
		}
		for i, in := range byUnit[key] {
			total++
			wire, _ := os.ReadFile(filepath.Join(repoRoot, in.wire))
			if lines[2*i] != string(wire) {
				t.Errorf("json-read %s (%s): the saved bytes are not the wire golden (%q)", in.name, key, firstLine(lines[2*i]))
			}
			if lines[2*i+1] != string(inputs[fmt.Sprintf("t%d", i)]) {
				t.Errorf("json-write %s (%s): the text of the loaded wire is not the golden (%q)", in.name, key, firstLine(lines[2*i+1]))
			}
		}
	}
	t.Logf("%d instances", total)
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	if len(s) > 60 {
		return s[:60]
	}
	return s
}
