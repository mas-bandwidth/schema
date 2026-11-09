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
// The read is THE ONE READ the conformance driver makes (test/conformance/moo
// main.moo, ctx.read): table_refusal.moo's ReadStrict, which answers the root's
// own form (form 3 for a fixed root through its fixed reader, form 1 otherwise)
// and opens an integrity root's trailer; a read that names a refusal returns
// no value. The save is ctx.write's: table_fixed.moo's form 3 for a fixed root,
// table_save.moo's form 1 for any other. This test is the text form's half of
// the pair.
//
// wireOwed is each unit whose wire half is not yet the value mapping's, with
// the defect, owned by the tables layer: the test requires each to still
// fail, so the list cannot outlive the fix.
var wireOwed = map[string]string{}

func TestMooJsonWireSurfaces(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form over the moo wire)")
	t.Parallel()
	bad, total := wireSurfacesRun(t, nil)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("%d instances", total)
}

// TestMooJsonWireSurfacesNegativeControl indents the writer one space a level
// instead of two and requires every json-write row red and every json-read
// row green: the reader takes any whitespace, so the break is the writer's
// and the surfaces localize it.
func TestMooJsonWireSurfacesNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo over the moo wire)")
	t.Parallel()
	src := string(Runtime()["json.moo"])
	const site = `s = "\n" .. string.rep("  ", depth)`
	if strings.Count(src, site) != 1 {
		t.Fatal("the control's site moved: json.moo no longer indents at " + site)
	}
	broken := strings.Replace(src, site, `s = "\n" .. string.rep(" ", depth)`, 1)
	bad, total := wireSurfacesRun(t, map[string][]byte{"json.moo": []byte(broken)})
	writes := 0
	for _, b := range bad {
		if !strings.HasPrefix(b, "json-write ") {
			t.Errorf("the writer's indent broke another surface: %s", b)
			continue
		}
		writes++
	}
	if writes != total {
		t.Fatalf("with the indent broken, %d of %d json-write rows are red: the test cannot see the writer's spelling", writes, total)
	}
}

// wireSurfacesRun runs both surfaces over every instance, runtime overriding
// runtime modules by name, and returns each failure and the instance count.
func wireSurfacesRun(t *testing.T, runtime map[string][]byte) ([]string, int) {
	t.Helper()
	var bad []string
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
		dir := generateUnitDir(t, u, runtime)
		inputs := map[string][]byte{}
		var b strings.Builder
		b.WriteString(probePrelude)
		b.WriteString(`local Save: any = require("./table_save")
local Load: any = require("./table_load")
local Refusal: any = require("./table_refusal")
local Fixed: any = require("./table_fixed")
local function run(mod: any, root: string, text: buffer, wire: buffer)
	local d = mod[root .. "Descriptor"]
	local T: any = if mod[root .. "Fixed"] ~= nil then mod else nil
	local value = mod["New" .. root]()
	local ok = mod[root .. "FromJson"](value, text, {})
	local bytes, why
	if d.fixed and T ~= nil then
		bytes, why = Fixed.save(d, T[root .. "Fixed"], { value })
	else
		bytes, why = Save.save(d, value)
	end
	if not ok then
		hexs("read refused")
	elseif bytes == nil then
		hexs("save refused: " .. tostring(why and why.name))
	else
		hexs(buffer.tostring(bytes))
	end
	local report = Load.newReport()
	local loaded = Refusal.ReadStrict(T, root, wire, report)
	if report.damaged or report.reason ~= nil then
		loaded = nil
	end
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
				bad = append(bad, fmt.Sprintf("unit %s now runs: remove it from wireOwed (%s)", key, why))
			} else {
				t.Logf("owed by the tables layer, unit %s: %s", key, why)
			}
			continue
		}
		if err != nil {
			bad = append(bad, fmt.Sprintf("unit %s: %v", key, err))
			continue
		}
		lines := decodeHexLines(t, out)
		if len(lines) != 2*len(byUnit[key]) {
			bad = append(bad, fmt.Sprintf("unit %s: %d lines for %d instances", key, len(lines), len(byUnit[key])))
			continue
		}
		for i, in := range byUnit[key] {
			total++
			wire, _ := os.ReadFile(filepath.Join(repoRoot, in.wire))
			if lines[2*i] != string(wire) {
				bad = append(bad, fmt.Sprintf("json-read %s (%s): the saved bytes are not the wire golden (%q)", in.name, key, firstLine(lines[2*i])))
			}
			if lines[2*i+1] != string(inputs[fmt.Sprintf("t%d", i)]) {
				bad = append(bad, fmt.Sprintf("json-write %s (%s): the text of the loaded wire is not the golden (%q)", in.name, key, firstLine(lines[2*i+1])))
			}
		}
	}
	return bad, total
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
