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

// TestFixedPointAndWideTextOnTheTableWireJavaScript is the card's carry
// (docs/SPEC-TABLES.md §2, §3, §4, §15, §16.2; SPEC.md §4.6, §4.12): the
// JavaScript variable form carries fixed/ufixed scalars (the raw integer at
// the declared width, BigInt at 64 and 128 bits) as direct fields, as union
// arms, as map values and inside a nested `type`, BYTE FOR BYTE against the Go
// oracle (internal/tablewire, internal/tabletext):
//
//   - the corpus of testdata/conformance/tables/MANIFEST.txt: scalars_full,
//     scalars_default and scalars_edges (tables/scalars SimState), map_text
//     (tables/maps Text, text and byte-buffer map values) and the messagedemo
//     instances (tables/messages ToolMessage), each loaded and saved back,
//     measured, written as text and read back from the text;
//   - a probe unit whose wires and texts the oracle writes here, covering a
//     fixed/ufixed as an arm and a map value, a fixed as an optional and a
//     nested `type`.
func TestFixedPointAndWideTextOnTheTableWireJavaScript(t *testing.T) {
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
	scalars := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "scalars")})
	maps := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "maps")})
	messages := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "messages")})
	units := []*unit{
		{"scalars", scalars, []*vcCase{
			corpus("scalars_full", "SimState"),
			corpus("scalars_default", "SimState"),
			corpus("scalars_edges", "SimState"),
		}},
		{"mapdemo", maps, []*vcCase{corpus("map_text", "Text")}},
		{"messagedemo", messages, []*vcCase{
			corpus("message_open", "ToolMessage"),
			corpus("message_transaction", "ToolMessage"),
			corpus("message_ping", "ToolMessage"),
			corpus("message_save", "ToolMessage"),
			corpus("message_empty", "ToolMessage"),
			corpus("message_batch", "ToolMessage"),
			corpus("message_trace", "ToolMessage"),
			corpus("message_arms", "ToolMessage"),
			corpus("message_arm_edits", "ToolMessage"),
			corpus("message_ack", "ToolMessage"),
		}},
	}

	// THE CORPUS IS THE ORACLE'S: decode, re-encode the same bytes, and write
	// the committed text; and every root is carried, none refused by name.
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
	probe := unitFrom(t, wideProbeSrc)
	if why := newVariableClass(probe).reason["Probe"]; why != "" {
		t.Fatalf("the probe root is refused: %s", why)
	}
	pm := tabletext.NewModel(probe)
	var probeCases []*vcCase
	for _, pc := range wideProbeTexts {
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
	units = append(units, &unit{"wideprobe", probe, probeCases})

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
		t.Fatalf("the wide probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for line := range strings.SplitSeq(string(out), "\n") {
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

// wideProbeSrc is the probe unit: fixed/ufixed in every place a field line
// rides on the variable wire.
const wideProbeSrc = `package wideprobe

type Inner
{
    n int16
}

// a map value cannot carry a range (internal/check refuses one on a map), and
// a fixed(I, F) must declare one (SPEC.md §4.3), so a fixed map value rides in
// a type
type Gauge
{
    level fixed(16, 16)   | min = -100, max = 100
    mass  ufixed(112, 16) | min = 0, max = 1000000
}

union Pick
{
    fx     fixed(16, 16)  | min = -100, max = 100
    ufx    ufixed(48, 16) | min = 0, max = 1000000
    big    fixed(112, 16) | min = -1000000, max = 1000000
    narrow string(4)
    off
}

table Probe
{
    pick    Pick
    picks   [..3]Pick
    fx_map  map[uint8]Gauge
    ufx_map map[string(4)]Gauge
    o_fx    ?fixed(32, 32)                | min = -1000, max = 1000
    inner   Inner
    inners  [..2]Inner
    past    int32
}
`

// wideProbeTexts are the probe's instances, as the oracle reads them.
var wideProbeTexts = []struct{ name, text string }{
	{"wide_probe_empty", `{}`},
	{"wide_probe_arm_fx", `{"pick": {"fx": -1.5}}`},
	{"wide_probe_arm_fx_zero", `{"pick": {"fx": 0}}`},
	{"wide_probe_arm_ufx", `{"pick": {"ufx": 12345.25}}`},
	{"wide_probe_arm_big", `{"pick": {"big": -99999.0625}}`},
	{"wide_probe_arm_narrow", `{"pick": {"narrow": "ok"}}`},
	{"wide_probe_arm_array", `{"picks": [{"narrow": "中"}, {"fx": 2.25}, {"off": null}]}`},
	{"wide_probe_maps", `{"fx_map": {"2": {"level": -3.25}, "1": {"level": 0.5, "mass": 7.75}}, "ufx_map": {"a": {"mass": 1000.5}, "b": {}}}`},
	{"wide_probe_optionals_present", `{"o_fx": 0}`},
	{"wide_probe_optionals_values", `{"o_fx": -42.125}`},
	{"wide_probe_nested", `{"inner": {"n": -3}, "inners": [{"n": 1}, {}]}`},
}
