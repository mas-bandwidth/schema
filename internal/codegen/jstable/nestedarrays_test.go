package jstable

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestNestedTypesAndFixedArraysOnTheTableWireJavaScript is the card's carry
// (docs/SPEC-TABLES.md §2, §2.3, §2.6, §2.8, §3, §16.2): the JavaScript variable
// form carries a NESTED `type` held by value (§2: laid out as a type by value),
// a FIXED-LENGTH ARRAY [N]T (§3: N elements, no count) wherever a field line
// holds one — a field, an optional ?[N]T, a map's value and a union's arm — and
// a FLAGS union arm, BYTE FOR BYTE against the Go oracle (internal/tablewire,
// internal/tabletext):
//
//   - the corpus instances testdata/conformance/tables/MANIFEST.txt names for
//     them: keyed_config and keyed_default (tabledemo KeyedConfig, whose scores
//     field is a nested type holding a keyed array), map_cells (mapdemo Cells,
//     a map whose value is [3]int32) and map_pairs (mapdemo Pairs, a map whose
//     value is [2]*Item);
//   - a probe unit whose wires and texts the oracle writes here from the texts
//     below: a nested type by value, an [N]T of scalars and of tables, an
//     optional ?[N]T absent, present at zero and present with values, and a
//     union whose arms are a nested type, an [N]T and a flags, alone and as the
//     elements of an array of unions.
//
// Each wire is loaded and saved back (the bytes must not move and the report
// must be clean), measured, written as text (the oracle's text), and read back
// from the text and saved (the wire again).
func TestNestedTypesAndFixedArraysOnTheTableWireJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type unit struct {
		name  string
		unit  *ir.Unit
		cases []*vcCase
	}
	corpus := func(name, root string) *vcCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return &vcCase{Name: name, Root: root, wire: wire, text: text}
	}
	examples := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "examples")})
	maps := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "maps")})
	units := []*unit{
		{"tabledemo", examples, []*vcCase{
			corpus("keyed_config", "KeyedConfig"),
			corpus("keyed_default", "KeyedConfig"),
		}},
		{"mapdemo", maps, []*vcCase{
			corpus("map_cells", "Cells"),
			corpus("map_pairs", "Pairs"),
		}},
	}

	// THE CORPUS IS THE ORACLE'S: decode, re-encode the same bytes, and write
	// the committed text; and this leg's variable class carries every root.
	for _, u := range units {
		vc := newVariableClass(u.unit)
		m := tabletext.NewModel(u.unit)
		for _, c := range u.cases {
			if !vc.carried(c.Root) {
				t.Fatalf("%s: this leg refuses %s: %s", c.Name, c.Root, vc.reason[c.Root])
			}
			inst := m.New(m.Lookup(c.Root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
				t.Fatalf("%s: the oracle did not decode its own wire: %v", c.Name, err)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire (%v)", c.Name, err)
			}
			text, err := m.Write(inst)
			if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
				t.Fatalf("%s: the oracle does not write the committed text (%v)", c.Name, err)
			}
		}
	}

	// THE PROBE UNIT: its wires and texts made by the oracle from the texts.
	probe := unitFrom(t, nestedArraysProbeSrc)
	if why := newVariableClass(probe).reason["Probe"]; why != "" {
		t.Fatalf("the probe root is refused: %s", why)
	}
	pm := tabletext.NewModel(probe)
	var probeCases []*vcCase
	for _, pc := range nestedArraysProbeTexts {
		inst := pm.New(pm.Lookup("Probe"))
		var rep tabletext.Report
		if !pm.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(pm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		text, err := pm.Write(inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not write: %v", pc.name, err)
		}
		probeCases = append(probeCases, &vcCase{Name: pc.name, Root: "Probe", wire: wire, text: append(text, '\n')})
	}
	units = append(units, &unit{"nestprobe", probe, probeCases})

	var all []*vcCase
	var want []string
	for _, u := range units {
		files, err := generateVariableFiles(u.unit, nil)
		if err != nil {
			t.Fatalf("%s: the variable form was refused: %v", u.name, err)
		}
		home := runtimeHome(u.unit) + "Variable.js"
		data, ok := files[home]
		if !ok {
			t.Fatalf("%s: no variable module was generated", u.name)
		}
		for _, c := range u.cases {
			if !strings.Contains(string(data), "export function "+c.Root+"Save(") {
				t.Fatalf("%s: %s carries no %sSave", c.Name, home, c.Root)
			}
		}
		module := filepath.Join(work, u.name, home)
		if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(module, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, c := range u.cases {
			c.Module = module
			c.Wire = filepath.Join(work, c.Name+".wire")
			c.Text = filepath.Join(work, c.Name+".json")
			if err := os.WriteFile(c.Wire, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(c.Text, c.text, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "WIRE "+c.Name+" OK", "MEASURE "+c.Name+" OK", "JSONW "+c.Name+" OK", "JSONR "+c.Name+" OK")
			all = append(all, c)
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
	if err := os.WriteFile(script, []byte(mapsArmsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, specPath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the nested types and fixed arrays probe did not run: %v\n%s", err, out)
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
		t.Logf("probe output (%d expected lines, all present):\n%s", len(want), out)
	}
}

// nestedArraysProbeSrc is the probe unit: a nested type by value, fixed-length
// arrays of scalars and of tables, an optional fixed-length array, and a union
// whose arms are a nested type, a fixed-length array and a flags.
const nestedArraysProbeSrc = `package nestprobe

enum Grade { Bronze, Silver, Gold }

flags Perms { Read, Write, Exec }

type Grid
{
    cells [3]int16
    grade Grade
}

table Leaf
{
    id int32
}

union Shape
{
    grid  Grid
    trio  [3]int32
    perms Perms
    off
}

table Probe
{
    grid   Grid
    trio   [3]int32
    o_trio ?[3]int32
    leaves [2]Leaf
    shape  Shape
    shapes []Shape
    after  int32
}
`

// nestedArraysProbeTexts are the probe's instances, as the oracle reads them.
var nestedArraysProbeTexts = []struct{ name, text string }{
	{"nest_empty", `{}`},
	{"nest_grid", `{"grid": {"cells": [1, -2, 3], "grade": "Gold"}, "after": 1}`},
	{"nest_grid_default", `{"grid": {"cells": [0, 0, 0]}}`},
	{"nest_trio", `{"trio": [0, 5, 0]}`},
	{"nest_leaves", `{"leaves": [{"id": 1}, {}]}`},
	{"opt_trio_absent", `{"after": 2}`},
	{"opt_trio_zero", `{"o_trio": [0, 0, 0]}`},
	{"opt_trio_values", `{"o_trio": [1, -2, 3]}`},
	{"shape_grid", `{"shape": {"grid": {"cells": [0, 0, 7], "grade": "Silver"}}}`},
	{"shape_grid_default", `{"shape": {"grid": {}}}`},
	{"shape_trio", `{"shape": {"trio": [1, 2, 3]}}`},
	{"shape_trio_zero", `{"shape": {"trio": [0, 0, 0]}}`},
	{"shape_perms", `{"shape": {"perms": ["Read", "Exec"]}}`},
	{"shape_perms_empty", `{"shape": {"perms": []}}`},
	{"shape_off", `{"shape": {"off": null}}`},
	{"shapes", `{"shapes": [{"trio": [0, 0, 9]}, {}, {"perms": ["Write"]}, {"grid": {"grade": "Bronze"}}, {"off": null}]}`},
	{"everything", `{"grid": {"cells": [4, 5, 6]}, "trio": [7, 8, 9], "o_trio": [0, 1, 0], "leaves": [{}, {"id": -1}], "shape": {"perms": ["Write", "Exec"]}, "shapes": [{"grid": {"cells": [1, 1, 1]}}], "after": -1}`},
}
