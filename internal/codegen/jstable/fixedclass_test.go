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

// TestFixedClassOnTableWireJavaScript is the card's carry: a FIXED TABLE rides
// the VARIABLE FORM too (docs/SPEC-TABLES.md §2.2, §3, §3.1, §4.2), so a
// closure that holds a `fixed table` gets the form-1 codec — Load, Save,
// Measure and the text verbs — BYTE FOR BYTE against the Go leg's form-1 wire
// (internal/tablewire is the oracle; internal/codegen/gotable/wire.go is the Go
// leg's emitter of the same bytes). Form 3 (§3.4) still stands beside it: the
// fixed form is not displaced, and schema#516's "a fixed table rides form 3
// only" is what this test retires.
//
// The cases are:
//
//   - the tabledemo corpus (tables/examples, the MANIFEST's root_full,
//     root_default, profile_elide and loadout_full instances of RootConfig,
//     ProfileConfig and LoadoutConfig), whose committed wire and text this leg
//     must load, re-save unchanged, measure and write as the oracle writes;
//   - a probe unit of `fixed table` roots carrying every declared default the
//     variable form's storage class owes (§4.2): int, string, bytes, bool,
//     float and enum, so a dropped default is a red subtest and not a silent
//     zero on the wire.
//
// A fixed table this leg's variable wire does NOT carry is refused by name
// (§11) and stays absent, never mis-encoded.
func TestFixedClassOnTableWireJavaScript(t *testing.T) {
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
	demo := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "examples")})
	// THE PREMISE: this closure holds a declared `fixed table`, so it is the
	// closure the form-1 codec was absent for (§2.2).
	if !closureHoldsFixedTable(demo, ir.TableClosure(demo)) {
		t.Fatal("tables/examples holds no fixed table, so this test proves nothing")
	}

	// THE BOUNDARY: a fixed table whose own fields this leg's variable wire does
	// not carry is refused BY NAME and stays absent (§11) — never mis-encoded and
	// never silently dropped. tabledemo's roots hold such a construct.
	dvc := newVariableClass(demo)
	for _, root := range []string{"RootConfig", "ProfileConfig", "LoadoutConfig"} {
		why := dvc.reason[root]
		if why == "" {
			t.Errorf("%s: this leg now carries %s on the variable wire, so this test's boundary is stale: add its instances to the cases", "tabledemo", root)
			continue
		}
		if !strings.Contains(why, root+".") {
			t.Errorf("%s: the refusal %q does not name the table and field (§11)", root, why)
		}
		t.Logf("%s is refused by name: %s", root, why)
	}

	units := []*unit{}

	// THE PROBE UNIT: fixed tables whose declared defaults the storage class
	// must carry (§4.2), and whose writer elides against them (§3).
	probe := unitFrom(t, fixedClassProbeSrc)
	if !closureHoldsFixedTable(probe, ir.TableClosure(probe)) {
		t.Fatal("the probe unit holds no fixed table, so this test proves nothing")
	}
	pm := tabletext.NewModel(probe)
	var probeCases []*vcCase
	for _, pc := range fixedClassProbeTexts {
		inst := pm.New(pm.Lookup(pc.root))
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
		probeCases = append(probeCases, &vcCase{Name: pc.name, Root: pc.root, wire: wire, text: append(text, '\n')})
	}
	units = append(units, &unit{"fixedclassprobe", probe, probeCases})

	// THE ORACLE FIRST: every committed wire decodes, re-encodes to the same
	// bytes and writes the committed text, so a difference below is this leg's
	// and not the corpus's.
	for _, u := range units {
		m := tabletext.NewModel(u.unit)
		for _, c := range u.cases {
			inst := m.New(m.Lookup(c.Root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
				t.Fatalf("%s: the oracle did not decode its own wire: %v", c.Name, err)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire (%v)", c.Name, err)
			}
			if c.text != nil {
				text, err := m.Write(inst)
				if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
					t.Fatalf("%s: the oracle does not write the committed text (%v)", c.Name, err)
				}
			}
		}
	}

	var all []*vcCase
	var want []string
	for _, u := range units {
		files, err := generateVariableFiles(u.unit, nil)
		if err != nil {
			t.Fatalf("%s: the variable form was refused for a closure holding a fixed table: %v", u.name, err)
		}
		home := runtimeHome(u.unit) + "Variable.js"
		data, ok := files[home]
		if !ok {
			t.Fatalf("%s: no variable module was generated for a closure holding a fixed table", u.name)
		}
		module := filepath.Join(work, u.name, home)
		if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(module, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, c := range u.cases {
			// THE FORM-1 SURFACE IS PRESENT: a fixed table's root gets the
			// form-1 verbs, which is the surface schema#516 said was absent.
			for _, verb := range []string{"Save(", "Load(", "Measure(", "ToJson(", "FromJson("} {
				if !strings.Contains(string(data), "export function "+c.Root+verb) {
					t.Errorf("%s: the variable module carries no form-1 %s for the fixed table %s", u.name, strings.TrimSuffix(verb, "("), c.Root)
				}
			}
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
		t.Fatalf("the fixed-class probe did not run: %v\n%s", err, out)
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

// fixedClassProbeSrc is the probe unit: `fixed table` roots whose fields carry
// every declared default the variable form's storage class owes
// (docs/SPEC-TABLES.md §4.2), and an elided instance beside a full one so a
// dropped default shows as a wire difference and not as a silent zero.
const fixedClassProbeSrc = `package fixedclassprobe

enum Grade { Bronze, Silver, Gold }

fixed table Leaf
{
    id    int32   = 7
    name  string(8) = "leaf"
    raw   bytes(4) = "ab"
    on    bool    = true
    ratio float64 = 1.5
    grade Grade   = Silver
}

fixed table Config
{
    level int32 = 3
    name  string(16) = "hi"
    leaf  Leaf
    many  [..4]int32
}
`

type fixedClassProbeText struct {
	name string
	root string
	text string
}

// fixedClassProbeTexts are the probe instances: every field at its declared
// default (so the wire elides it and a dropped default reads back wrong), and
// every field carrying a value.
var fixedClassProbeTexts = []fixedClassProbeText{
	{"fc_leaf_default", "Leaf", `{}`},
	{"fc_leaf_values", "Leaf", `{"id": 41, "name": "x", "raw": "AAECAw==", "on": false, "ratio": 2.25, "grade": "Gold"}`},
	{"fc_config_default", "Config", `{}`},
	{"fc_config_values", "Config", `{"level": 9, "name": "there", "leaf": {"id": 1, "name": "l", "on": false, "ratio": 0.5, "grade": "Bronze"}, "many": [1, 2, 3]}`},
}
