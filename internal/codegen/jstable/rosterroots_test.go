package jstable

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsCodecOpensEveryRosterRootJavaScript pins that the JavaScript variable
// leg opens the five form-1 roster roots the gate card found without a JS
// codec (docs/SPEC-TABLES.md §11): tbla2.Root, tblg1.Guarded, tblk1.Root,
// tblk2.Root and tblp2.Chain. Each unit generates, its module loads in node,
// and the JS Load/Save carry a wire byte for byte as the Go oracle
// (internal/tablewire) does, with a clean report.
func TestJsCodecOpensEveryRosterRootJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type rootCase struct {
		name  string
		unit  *ir.Unit
		root  string
		texts []string
	}
	loadSchema := func(path string) *ir.Unit {
		return loadUnit(t, filepath.Join(repo, path))
	}
	cases := []*rootCase{
		{"tbla2", loadSchema("test/tables/A2.schema"), "Root", []string{
			`{"id": 1, "value": {"a": 5}, "tail": 2}`,
			`{"id": 2, "value": {"b": 1.5}, "tail": 3}`,
			`{"id": 3, "value": {"c": "héllo"}, "tail": 4}`,
			`{"id": 4, "value": {"d": {"n": 9}}, "tail": 5}`,
			`{"id": 5, "value": {"e": {"n": 7}}, "tail": 6}`,
			`{"id": 6, "value": {}, "tail": 0}`,
		}},
		{"tblg1", loadSchema("test/tables/G1.schema"), "Guarded", []string{
			`{"at_rest": true, "always": {"value": 3}, "resting": {"value": 4}}`,
			`{"at_rest": false, "always": {"value": 1}}`,
			`{"at_rest": true, "always": {"value": 0}}`,
		}},
		{"tblk1", loadSchema("test/tables/K1.schema"), "Root", []string{
			`{"grade": "Gold", "raw": 7}`,
			`{"grade": "None", "raw": 0}`,
			`{"grade": "Bronze", "raw": 65535}`,
		}},
		{"tblk2", loadSchema("test/tables/K2.schema"), "Root", []string{
			`{"grade": 7, "raw": "Silver"}`,
			`{"grade": 0, "raw": "None"}`,
			`{"grade": 65535, "raw": "Bronze"}`,
		}},
		{"tblp2", loadSchema("test/tables/P2.schema"), "Chain", []string{
			`{"name": "chain", "link": {"value": 10, "tag": "t", "next": {"value": 20}}}`,
			`{"name": "solo"}`,
			`{"name": "s", "link": {"value": 0, "tag": "", "next": null}}`,
		}},
	}

	type vcCase struct {
		Name   string `json:"name"`
		Root   string `json:"root"`
		Module string `json:"module"`
		Wire   string `json:"wire"`
	}
	var all []*vcCase
	var want []string

	for _, c := range cases {
		m := tabletext.NewModel(c.unit)
		if !newVariableClass(c.unit).carried(c.root) {
			t.Fatalf("%s: the variable wire does not carry %s: %s", c.name, c.root, newVariableClass(c.unit).reason[c.root])
		}
		files, err := generateVariableFiles(c.unit, nil)
		if err != nil {
			t.Fatalf("%s: the variable form was refused: %v", c.name, err)
		}
		home := runtimeHome(c.unit) + "Variable.js"
		data, ok := files[home]
		if !ok {
			t.Fatalf("%s: no variable module was generated", c.name)
		}
		module := filepath.Join(work, c.name, home)
		if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(module, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for i, text := range c.texts {
			inst := m.New(m.Lookup(c.root))
			var rep tabletext.Report
			if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
				t.Fatalf("%s %d: the oracle did not read %q cleanly: %+v", c.name, i, text, rep)
			}
			wire, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Fatalf("%s %d: the oracle did not encode: %v", c.name, i, err)
			}
			name := c.name + "_" + strconv.Itoa(i)
			wirePath := filepath.Join(work, name+".wire")
			if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
				t.Fatal(err)
			}
			all = append(all, &vcCase{Name: name, Root: c.root, Module: module, Wire: wirePath})
			want = append(want, "WIRE "+name+" OK")
		}
	}

	spec, err := json.Marshal(map[string]any{"cases": all})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(work, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(rosterRootsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, specPath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the roster roots probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		got[strings.TrimSpace(line)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("the probe did not print %q", w)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	} else {
		t.Logf("probe output (%d expected lines, all present)", len(want))
	}
}

// rosterRootsDriver loads, saves and measures each case, byte for byte against
// the oracle wire.
const rosterRootsDriver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
const same = (a, b) => a !== null && a.length === b.length && a.every((x, i) => x === b[i]);
const clean = (r) => r.unknown === 0 && r.kindMismatch === 0 && r.widened === 0 && r.clamped === 0 && r.duplicate === 0 && !r.malformed;
const firstDiff = (a, b) => { if (a === null) { return "null"; } for (let i = 0; i < Math.max(a.length, b.length); i++) { if (a[i] !== b[i]) { return "at " + i + " got " + a[i] + " want " + b[i] + " (len " + a.length + " vs " + b.length + ")"; } } return "equal"; };

for (const c of spec.cases) {
  const m = await import(pathToFileURL(c.module).href);
  const wire = new Uint8Array(readFileSync(c.wire));
  const rep = new m.TableVariableReport();
  const v = m[c.root + "Load"](wire, rep);
  if (v === null) { console.log("WIRE " + c.name + " REFUSED " + JSON.stringify(rep)); continue; }
  const saved = m[c.root + "Save"](v);
  if (same(saved, wire) && clean(rep)) { console.log("WIRE " + c.name + " OK"); }
  else { console.log("WIRE " + c.name + " DIFF " + firstDiff(saved, wire) + " report " + JSON.stringify(rep)); }
}
`
