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

// TestMapsAndUnionArmsOnTheTableWireJavaScript is the card's carry
// (docs/SPEC-TABLES.md §2.3, §2.4, §2.6, §2.8, §3, §3.2, §16.2): the JavaScript
// variable form carries MAPS, UNIONS (table arms, direct arms of every carried
// field type, payload-free arms, nested unions, arrays of unions), OPTIONAL ?T
// (scalars, strings, enums, tables, arrays) and ENUM-KEYED ARRAYS [E]T, BYTE FOR
// BYTE against the Go oracle (internal/tablewire, internal/tabletext):
//
//   - the map corpus (tables/maps, mapdemo in testdata/conformance/tables/
//     MANIFEST.txt): every instance whose root this leg carries, its committed
//     wire and text;
//   - a probe unit whose wires and texts the oracle writes here from the texts
//     below, covering each arm kind, each optional shape and each keyed element
//     kind, and the edges: None, an empty map, an absent and a present-default
//     optional, a None element in an array of unions, a pointer in a map value
//     and in an arm.
//
// Each wire is loaded and saved back (the bytes must not move and the report
// must be clean), measured, written as text (the oracle's text), and read back
// from the text and saved (the wire again).
//
// Every map corpus root is carried: Cells and Pairs (a fixed array [N]T as a
// map value) are held by TestNestedTypesAndFixedArraysOnTheTableWireJavaScript,
// and Text (a wstring(N) map value) by
// TestFixedPointAndWideTextOnTheTableWireJavaScript.
func TestMapsAndUnionArmsOnTheTableWireJavaScript(t *testing.T) {
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
	maps := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "maps")})
	units := []*unit{
		{"mapdemo", maps, []*vcCase{
			corpus("map_chunks", "Chunks"),
			corpus("map_crews", "Crews"),
			corpus("map_depth", "Depth"),
			corpus("map_docs", "Docs"),
			corpus("map_empty", "Fleet"),
			corpus("map_full", "Fleet"),
			corpus("map_slots", "Slots"),
			corpus("map_spans", "Spans"),
			corpus("map_trails", "Trails"),
			corpus("map_runs", "Runs"),
		}},
	}

	vc := newVariableClass(maps)

	// THE CORPUS IS THE ORACLE'S: decode, re-encode the same bytes, and write
	// the committed text.
	for _, u := range units {
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
	probe := unitFrom(t, mapsArmsProbeSrc)
	if why := newVariableClass(probe).reason["Probe"]; why != "" {
		t.Fatalf("the probe root is refused: %s", why)
	}
	pm := tabletext.NewModel(probe)
	var probeCases []*vcCase
	for _, pc := range mapsArmsProbeTexts {
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
	units = append(units, &unit{"armsprobe", probe, probeCases})

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
		t.Fatalf("the maps and arms probe did not run: %v\n%s", err, out)
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

// mapsArmsProbeSrc is the probe unit: every arm kind, optional shape and keyed
// element kind this card carries, beside maps of each key kind.
const mapsArmsProbeSrc = `package armsprobe

enum Grade { Bronze, Silver, Gold }

table Leaf
{
    id   int32
    name string(8)
}

union Inner
{
    leaf  Leaf
    plain int32
}

union Pick
{
    leaf  Leaf
    num   int32
    wide  uint64
    label string(8)
    grade Grade
    raw   bytes(4)
    many  [..4]int16
    ptr   *Leaf
    blob  *string
    flag  bool
    ratio float32
    inner Inner
    off
}

table Probe
{
    pick    Pick
    picks   []Pick
    capped  [..3]Pick
    o_int   ?int32
    o_bool  ?bool
    o_grade ?Grade
    o_leaf  ?Leaf
    o_list  ?[..4]int32
    scores  [Grade]int32
    ranks   [Grade]Grade
    leaves  [Grade]Leaf
    names   map[string(8)]int32
    by_id   map[uint64]Leaf
    by_key  map[int16]*Leaf
    nested  map[uint8]map[string(4)]Grade
    head    *Leaf
    twig    Inner
    after   int32
}
`

// mapsArmsProbeTexts are the probe's instances, as the oracle reads them.
var mapsArmsProbeTexts = []struct{ name, text string }{
	{"arms_empty", `{}`},
	{"arms_leaf", `{"pick": {"leaf": {"id": 7, "name": "seven"}}, "after": 1}`},
	{"arms_leaf_default", `{"pick": {"leaf": {}}}`},
	{"arms_num", `{"pick": {"num": -5}}`},
	{"arms_num_zero", `{"pick": {"num": 0}}`},
	{"arms_wide", `{"pick": {"wide": 18446744073709551615}}`},
	{"arms_label", `{"pick": {"label": "héllo"}}`},
	{"arms_grade", `{"pick": {"grade": "Gold"}}`},
	{"arms_grade_none", `{"pick": {"grade": "None"}}`},
	{"arms_raw", `{"pick": {"raw": "AQID"}}`},
	{"arms_many", `{"pick": {"many": [1, -2, 300]}}`},
	{"arms_many_empty", `{"pick": {"many": []}}`},
	{"arms_ptr", `{"pick": {"ptr": {"id": 3, "name": "p"}}, "head": {"id": 4}}`},
	{"arms_ptr_null", `{"pick": {"ptr": null}}`},
	{"arms_blob", `{"pick": {"blob": "a blob"}}`},
	{"arms_flag", `{"pick": {"flag": true}}`},
	{"arms_ratio", `{"pick": {"ratio": 0.5}}`},
	{"arms_inner", `{"pick": {"inner": {"leaf": {"id": 9}}}}`},
	{"arms_inner_none", `{"pick": {"inner": {}}}`},
	{"arms_off", `{"pick": {"off": null}}`},
	{"arms_array", `{"picks": [{"num": 1}, {}, {"leaf": {"id": 2}}, {"off": null}, {"ptr": {"id": 5}}], "capped": [{"label": "x"}, {}]}`},
	{"opt_present", `{"o_int": 0, "o_bool": false, "o_grade": "None", "o_leaf": {}, "o_list": []}`},
	{"opt_values", `{"o_int": -9, "o_bool": true, "o_grade": "Silver", "o_leaf": {"id": 1, "name": "o"}, "o_list": [1, 2, 3]}`},
	{"keyed_values", `{"scores": {"Bronze": 0, "Silver": 5, "Gold": -1}, "ranks": {"Gold": "Bronze", "Silver": "Silver"}, "leaves": {"Silver": {"id": 2}, "Gold": {}}}`},
	{"maps_values", `{"names": {"zeta": 1, "alpha": 0, "é": 3, "z": 2}, "by_id": {"18446744073709551615": {"id": 1}, "0": {}, "7": {"name": "seven"}}}`},
	{"maps_pointers", `{"by_key": {"-3": {"&node": 1, "id": 3}, "2": {"&node": 1}}, "head": {"id": 8}}`},
	{"maps_nested", `{"nested": {"2": {"b": "Gold", "a": "None"}, "1": {}}}`},
	{"everything", `{"pick": {"leaf": {"id": 1}}, "picks": [{"grade": "Bronze"}], "o_int": 4, "scores": {"Gold": 9}, "names": {"k": 1}, "by_key": {"1": {"id": 1}}, "after": -1}`},
}

// mapsArmsDriver loads, saves, measures, writes and reads each case.
const mapsArmsDriver = `
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
  const measured = m[c.root + "Measure"](v);
  console.log("MEASURE " + c.name + (saved !== null && measured === saved.length ? " OK" : " DIFF " + measured));
  const want = readFileSync(c.text, "utf8");
  const t = m[c.root + "ToJson"](v);
  if (t === want) { console.log("JSONW " + c.name + " OK"); }
  else { console.log("JSONW " + c.name + " DIFF\n--- got ---\n" + t + "--- want ---\n" + want); }
  const r2 = new m.TableVariableReport();
  const v2 = m[c.root + "FromJson"](want, r2);
  const s2 = m[c.root + "Save"](v2);
  if (same(s2, wire) && clean(r2)) { console.log("JSONR " + c.name + " OK"); }
  else { console.log("JSONR " + c.name + " DIFF " + firstDiff(s2, wire) + " report " + JSON.stringify(r2)); }
}
`
