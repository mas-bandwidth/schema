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
// the VARIABLE FORM too (docs/SPEC-TABLES.md §2.2, §3, §3.1, §4.2, §5), so a
// closure that holds a `fixed table` gets the form-1 codec — Load, Save,
// Measure and the text verbs — BYTE FOR BYTE against the Go leg's form-1 wire
// (internal/tablewire is the oracle; internal/codegen/gotable/wire.go is the Go
// leg's emitter of the same bytes). Form 3 (§3.4) still stands beside it: the
// fixed form is not displaced, and schema#516's "a fixed table rides form 3
// only" is what this test retires.
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

	demo := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "examples")})
	if !closureHoldsFixedTable(demo, ir.TableClosure(demo)) {
		t.Fatal("tables/examples holds no fixed table, so this test proves nothing")
	}
	v1 := loadUnitPaths(t, []string{filepath.Join(repo, "test", "tables", "V1.schema")})
	v2 := loadUnitPaths(t, []string{filepath.Join(repo, "test", "tables", "V2.schema")})
	w1 := loadUnitPaths(t, []string{filepath.Join(repo, "test", "tables", "W1.schema")})
	w2 := loadUnitPaths(t, []string{filepath.Join(repo, "test", "tables", "W2.schema")})
	r1 := loadUnitPaths(t, []string{filepath.Join(repo, "test", "tables", "R1.schema")})
	r2 := loadUnitPaths(t, []string{filepath.Join(repo, "test", "tables", "R2.schema")})

	units := []*unit{
		{"tabledemo", demo, []*vcCase{
			corpus("root_full", "RootConfig"),
			corpus("root_default", "RootConfig"),
			corpus("profile_elide", "ProfileConfig"),
			corpus("loadout_full", "LoadoutConfig"),
			corpus("wide_blob", "WideBlob"),
			corpus("archive", "ArchiveConfig"),
			corpus("keyed_config", "KeyedConfig"),
			corpus("keyed_default", "KeyedConfig"),
		}},
		{"tblv1", v1, []*vcCase{
			corpus("v1_cfg", "Cfg"),
			corpus("v1_seams", "Cfg"),
		}},
		{"tblv2", v2, []*vcCase{
			corpus("v2_cfg", "Cfg"),
			corpus("v2_seams", "Cfg"),
		}},
		{"tblw1", w1, []*vcCase{
			corpus("w1_fleet", "Fleet"),
		}},
		{"tblw2", w2, []*vcCase{
			corpus("w2_fleet", "Fleet"),
		}},
		{"tblr1", r1, []*vcCase{
			corpus("r1_cfg", "Cfg"),
		}},
		{"tblr2", r2, []*vcCase{
			corpus("r2_cfg", "Cfg"),
		}},
	}

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

	// TABLE RENAME AND CROSS-VERSION RENAME (§5): a renamed table (`table NewBox
	// was OldBox`) and the R1->R2 / V1->V2 / W1->W2 hops read across the rename
	// equal to the Go oracle.
	t.Run("table_and_was_renames", func(t *testing.T) {
		oldU := unitFrom(t, `package renameprobe
table OldChild
{
    x int32 = 5
}
table OldRoot
{
    child *OldChild
    items [..2]*OldChild
}
`)
		newU := unitFrom(t, `package renameprobe
table NewChild | was = "OldChild"
{
    x int32 = 5
}
table NewRoot | was = "OldRoot"
{
    child *NewChild
    items [..2]*NewChild
}
`)
		om := tabletext.NewModel(oldU)
		oi := om.New(om.Lookup("OldRoot"))
		var orep tabletext.Report
		if !om.Read(oi, []byte(`{"child":{"x":19},"items":[{"x":23}]}`), &orep) {
			t.Fatal("oracle read OldRoot failed")
		}
		owire, err := tablewire.Encode(om, oi)
		if err != nil {
			t.Fatal(err)
		}
		nm := tabletext.NewModel(newU)
		ni := nm.New(nm.Lookup("NewRoot"))
		var nrep tabletext.Report
		if ok, err := tablewire.Decode(nm, ni, owire, &nrep); !ok || err != nil || nrep != (tabletext.Report{}) {
			t.Fatalf("oracle decode NewRoot from OldRoot wire: ok=%v err=%v rep=%+v", ok, err, nrep)
		}
		ntext, err := nm.Write(ni)
		if err != nil {
			t.Fatal(err)
		}
		files, err := generateVariableFiles(newU, nil)
		if err != nil {
			t.Fatal(err)
		}
		modPath := filepath.Join(work, "RenameprobeVariable.js")
		if err := os.WriteFile(modPath, files["RenameprobeVariable.js"], 0o600); err != nil {
			t.Fatal(err)
		}
		wirePath := filepath.Join(work, "oldroot.wire")
		if err := os.WriteFile(wirePath, owire, 0o600); err != nil {
			t.Fatal(err)
		}
		rscript := filepath.Join(work, "rename_probe.mjs")
		rsrc := `import { readFileSync } from "node:fs";
import { NewRootLoad, NewRootToJson, TableVariableReport } from "./RenameprobeVariable.js";
const wire = new Uint8Array(readFileSync(process.argv[2]));
const rep = new TableVariableReport();
const v = NewRootLoad(wire, rep);
if (v === null || rep.malformed || rep.unknown !== 0 || rep.kindMismatch !== 0) {
  console.error("bad report", rep);
  process.exit(1);
}
process.stdout.write(NewRootToJson(v));
`
		if err := os.WriteFile(rscript, []byte(rsrc), 0o600); err != nil {
			t.Fatal(err)
		}
		rcmd := exec.Command(node, rscript, wirePath)
		rcmd.Dir = work
		rout, err := rcmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rename probe failed: %v\n%s", err, rout)
		}
		if string(rout) != string(ntext)+"\n" {
			t.Fatalf("rename probe text mismatch:\ngot:\n%s\nwant:\n%s\n", rout, ntext)
		}
	})
}

// fixedClassProbeSrc is the probe unit: `fixed table` roots whose fields carry
// every declared default the variable form's storage class owes
// (docs/SPEC-TABLES.md §4.2), including flags, and an elided instance beside a
// full one so a dropped default shows as a wire difference and not as a silent
// zero.
const fixedClassProbeSrc = `package fixedclassprobe

enum Grade { Bronze, Silver, Gold }
flags Perms { Read, Write, Exec }

fixed table Leaf
{
    id    int32   = 7
    name  string(8) = "leaf"
    raw   bytes(4) = "ab"
    on    bool    = true
    ratio float64 = 1.5
    grade Grade   = Silver
    perms Perms   = { Read, Exec }
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
	{"fc_leaf_values", "Leaf", `{"id": 41, "name": "x", "raw": "AAECAw==", "on": false, "ratio": 2.25, "grade": "Gold", "perms": ["Write"]}`},
	{"fc_config_default", "Config", `{}`},
	{"fc_config_values", "Config", `{"level": 9, "name": "there", "leaf": {"id": 1, "name": "l", "on": false, "ratio": 0.5, "grade": "Bronze", "perms": []}, "many": [1, 2, 3]}`},
}
