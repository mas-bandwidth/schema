package mootable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// json-read and json-write as TWO gates over one corpus (docs/SPEC-TABLES.md
// §16.5; test/conformance/README.md): each instance is put in front of moo
// twice, once as the text and once as a value built from the engine's
// reading of it (json_literal_test.go), so the writer is judged on a value
// the reader did not build and the reader on a value the writer did not
// spell.
//
//	json-write   <Root>ToJson(the literal)            == the engine's text
//	json-read    <Root>FromJson(the engine's text)    == the literal, silent

type gateCase struct {
	name, root string
	text       []byte
	inst       *tabletext.Instance
}

type gateResult struct{ write, read string }

// gateRun runs one unit's cases and returns, per case, the text the writer
// wrote (or "refused: why") and the reader's verdict ("ok", or the first
// difference and the report).
func gateRun(t *testing.T, u *ir.Unit, cases []gateCase, runtime map[string][]byte) []gateResult {
	t.Helper()
	dir := generateUnitDir(t, u, runtime)
	lit := newLiteral(u)
	var body strings.Builder
	inputs := map[string][]byte{}
	for i, c := range cases {
		lit.b.Reset()
		value := lit.instance(c.inst)
		inputs[fmt.Sprintf("t%d", i)] = c.text
		fmt.Fprintf(&body, "do\n%scheck(%q, %q, %s, inputs.t%d)\nend\n", lit.b.String(), tableModule(u, c.root), c.root, value, i)
	}
	var b strings.Builder
	b.WriteString(probePrelude)
	b.WriteString(literalPrelude)
	for _, m := range lit.modules() {
		if _, err := os.Stat(filepath.Join(dir, m+".moo")); err == nil {
			fmt.Fprintf(&b, "MOD[%q] = require(\"./%s\")\n", m, m)
		}
	}
	b.WriteString(`local function check(mod: string, root: string, value: any, text: buffer)
	local M = MOD[mod]
	local size = M[root .. "ToJsonMeasure"](value)
	if size < 0 then
		hexs("refused")
	else
		local out = buffer.create(size)
		M[root .. "ToJson"](value, out)
		hexs(buffer.tostring(out))
	end
	local read = M["New" .. root]()
	local report: any = {}
	local ok = M[root .. "FromJson"](read, text, report)
	local verdict = string.format("%d,%d,%d,%d,%d,%s,%s", report.unknown, report.kindMismatch, report.widened,
		report.clamped, report.duplicate, tostring(report.malformed), if ok then "read" else "refused")
	local diff = deepEqual(read, value, root)
	hexs(if diff == nil and verdict == "0,0,0,0,0,false,read" then "ok" else verdict .. " " .. (diff or "equal"))
end
`)
	b.WriteString(body.String())
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := decodeHexLines(t, moorun.Run(t, dir, "main.moo", inputs))
	if len(lines) != 2*len(cases) {
		t.Fatalf("the probe printed %d lines for %d cases", len(lines), len(cases))
	}
	out := make([]gateResult, len(cases))
	for i := range cases {
		out[i] = gateResult{lines[2*i], lines[2*i+1]}
	}
	return out
}

// goldenGateCases is every textual instance of the manifest, by unit: the
// golden text and the engine's own reading of it.
func goldenGateCases(t *testing.T) (map[string]*ir.Unit, map[string][]gateCase) {
	t.Helper()
	m := readTextManifest(t)
	units := map[string]*ir.Unit{}
	cases := map[string][]gateCase{}
	for _, in := range m.instances {
		u := units[in.unit]
		if u == nil {
			u = loadUnit(t, m.units[in.unit])
			units[in.unit] = u
		}
		if !unitCarriesJson(u) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "conformance", "tables", "json", in.name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		model := tabletext.NewModel(u)
		inst := model.New(model.Lookup(in.root))
		var r tabletext.Report
		if !model.Read(inst, text, &r) || !r.Silent() {
			t.Fatalf("the engine does not read %s silently: %+v", in.name, r)
		}
		cases[in.unit] = append(cases[in.unit], gateCase{in.name, in.root, text, inst})
	}
	return units, cases
}

// jsonGates runs both gates over the golden corpus and returns the failures
// of each, by case.
func jsonGates(t *testing.T, runtime map[string][]byte) (writeBad, readBad []string, total int) {
	t.Helper()
	units, cases := goldenGateCases(t)
	keys := make([]string, 0, len(cases))
	for k := range cases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		res := gateRun(t, units[key], cases[key], runtime)
		for i, c := range cases[key] {
			total++
			if res[i].write != string(c.text) {
				writeBad = append(writeBad, fmt.Sprintf("%s (%s): wrote\n%s\nwant\n%s", c.name, key, res[i].write, c.text))
			}
			if res[i].read != "ok" {
				readBad = append(readBad, fmt.Sprintf("%s (%s): %s", c.name, key, res[i].read))
			}
		}
	}
	return writeBad, readBad, total
}

// TestMooJsonWriteAndReadGates: json-write and json-read, both green over
// every textual instance of the corpus.
func TestMooJsonWriteAndReadGates(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated text form)")
	t.Parallel()
	writeBad, readBad, total := jsonGates(t, nil)
	for _, b := range writeBad {
		t.Errorf("json-write: %s", b)
	}
	for _, b := range readBad {
		t.Errorf("json-read: %s", b)
	}
	t.Logf("%d instances, both gates", total)
}

// TestMooJsonWalkNegativeControl is the per-backend walk control
// (docs/SPEC-TABLES.md §16.5): the reader's key lookup is sabotaged by one
// field — one key's value lands in its neighbour — and json-read must go red
// while json-write stays green, which is what says the break is the READER's.
func TestMooJsonWalkNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	src := string(Runtime()["json.moo"])
	const site = "\t\t\tk[fd.json] = i\n"
	if !strings.Contains(src, site) {
		t.Fatal("the control's site moved: json.moo's keyIndex no longer reads " + strings.TrimSpace(site))
	}
	broken := strings.Replace(src, site, "\t\t\tk[fd.json] = if i < #d.fields then i + 1 else i\n", 1)
	writeBad, readBad, total := jsonGates(t, map[string][]byte{"json.moo": []byte(broken)})
	if len(writeBad) != 0 {
		t.Errorf("json-write went red under a READER sabotage (%d of %d): the gates are not independent\n%s", len(writeBad), total, writeBad[0])
	}
	if len(readBad) == 0 {
		t.Fatalf("json-read stayed green under the sabotaged key lookup over %d instances: the gate cannot see the reader", total)
	}
	t.Logf("json-read red on %d of %d instances, json-write green", len(readBad), total)
}
