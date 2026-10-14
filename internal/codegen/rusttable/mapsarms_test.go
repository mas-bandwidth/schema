package rusttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMapsAndUnionArmsOnTheTableWireRust pins the Rust leg's carry of the
// maps, union-arm, optional, enum-keyed array, fixed-length array and 128-bit
// scalar variable-class constructs (docs/SPEC-TABLES.md §2.2, §2.3, §2.4, §2.6,
// §2.8, §3, §3.2, §16.2), every case held BYTE FOR BYTE against the Go oracle
// (internal/tablewire, internal/tabletext):
//
//   - the map corpus (tables/maps): map[K]V with scalar, enum, table, pointer,
//     nested-map and fixed-length array values, empty and full (12 instances;
//     Text is refused by name for wstring(N));
//   - the union-arm corpus (tables/arms): unions whose arms are tables holding
//     lists, pointers, blobs, arrays of pointers and nested unions, and bounded
//     and unbounded arrays of unions (9 instances);
//   - the armsprobe unit: every arm kind, optional shape (?T, ?[..N]T, ?[N]T),
//     enum-keyed array shape ([E]T of scalars, enums and tables), maps, and
//     128-bit signed and unsigned integers.
//
// Each wire is loaded and saved back (the bytes must not move and the report
// must be clean), written as text (the text must be the oracle's), and read
// back from the text and saved (the bytes must be the wire again). Every corpus
// wire is first held to the oracle itself.
func TestMapsAndUnionArmsOnTheTableWireRust(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type maCase struct {
		name, root string
		wire, text []byte
	}
	corpus := func(name, root string, text bool) maCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		c := maCase{name: name, root: root, wire: wire}
		if text {
			c.text, err = os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	mapsUnit := vcLoadUnit(t, repo, "tables/maps")
	vcMaps := newVariableClass(mapsUnit)
	if why := vcMaps.reason["Text"]; !strings.Contains(why, "wide:") || !strings.Contains(why, "wstring") {
		t.Errorf("Text: refusal %q does not name wide and wstring", why)
	}

	armsUnit := vcLoadUnit(t, repo, "tables/arms")
	units := []struct {
		module string
		unit   *ir.Unit
		cases  []maCase
	}{
		{"mapdemo", mapsUnit, []maCase{
			corpus("map_cells", "Cells", true),
			corpus("map_chunks", "Chunks", true),
			corpus("map_crews", "Crews", true),
			corpus("map_depth", "Depth", true),
			corpus("map_docs", "Docs", true),
			corpus("map_empty", "Fleet", true),
			corpus("map_full", "Fleet", true),
			corpus("map_pairs", "Pairs", true),
			corpus("map_slots", "Slots", true),
			corpus("map_spans", "Spans", true),
			corpus("map_trails", "Trails", true),
			corpus("map_runs", "Runs", true),
		}},
		{"armdemo", armsUnit, []maCase{
			corpus("arms_chain", "Chain", false),
			corpus("arms_gate", "Gate", false),
			corpus("arms_gate_text", "Gate", false),
			corpus("arms_hand", "Hand", false),
			corpus("arms_holder", "Holder", false),
			corpus("arms_nest", "Nest", false),
			corpus("arms_rack", "Rack", false),
			corpus("arms_ring", "Ring", false),
			corpus("arms_tray", "Tray", false),
		}},
	}

	for ui := range units {
		u := &units[ui]
		vc := newVariableClass(u.unit)
		m := tabletext.NewModel(u.unit)
		for ci := range u.cases {
			c := &u.cases[ci]
			if !vc.carried(c.root) {
				t.Fatalf("%s: this leg refuses %s: %s", c.name, c.root, vc.reason[c.root])
			}
			inst := m.New(m.Lookup(c.root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
				t.Fatalf("%s: the oracle did not decode its own wire: %v", c.name, err)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire (%v)", c.name, err)
			}
			text, err := m.Write(inst)
			if err != nil {
				t.Fatalf("%s: the oracle did not write text: %v", c.name, err)
			}
			if c.text != nil {
				if !bytes.Equal(append(text, '\n'), c.text) {
					t.Fatalf("%s: the oracle does not write the committed text", c.name)
				}
			} else {
				c.text = append(text, '\n')
			}
		}
	}

	probeUnit := unitFrom(t, mapsArmsProbeSrc)
	if why := newVariableClass(probeUnit).reason["Probe"]; why != "" {
		t.Fatalf("the probe root is refused: %s", why)
	}
	pm := tabletext.NewModel(probeUnit)
	var probeCases []maCase
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
		probeCases = append(probeCases, maCase{name: pc.name, root: "Probe", wire: wire, text: append(text, '\n')})
	}
	units = append(units, struct {
		module string
		unit   *ir.Unit
		cases  []maCase
	}{"armsprobe", probeUnit, probeCases})

	var mods, main strings.Builder
	main.WriteString(variableClassProbeHead)
	var want []string
	for _, u := range units {
		modules, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("%s: the maps/union-arm class was refused: %v", u.module, err)
		}
		dir := filepath.Join(work, u.module)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		var names []string
		for name, data := range modules {
			if name == VariableRuntimeModule+".rs" || strings.HasSuffix(name, "_table.rs") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
				names = append(names, strings.TrimSuffix(name, ".rs"))
			}
		}
		sort.Strings(names)
		fmt.Fprintf(&mods, "#[allow(dead_code)]\nmod %s {\n", u.module)
		for _, mod := range names {
			fmt.Fprintf(&mods, "    #[path = %q]\n    pub mod %s;\n", filepath.Join(dir, mod+".rs"), mod)
		}
		mods.WriteString("}\n")
		for _, c := range u.cases {
			table := ""
			for _, f := range u.unit.Files {
				for _, st := range f.Tables {
					if st.Name == c.root {
						table = strings.ToLower(f.Base) + "_table"
					}
				}
			}
			if table == "" {
				t.Fatalf("%s: no file declares %s", c.name, c.root)
			}
			wirePath := filepath.Join(work, c.name+".wire")
			if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			jsonPath := "None"
			want = append(want, "WIRE "+c.name+" OK")
			if c.text != nil {
				p := filepath.Join(work, c.name+".json")
				if err := os.WriteFile(p, c.text, 0o600); err != nil {
					t.Fatal(err)
				}
				jsonPath = fmt.Sprintf("Some(%q)", p)
				want = append(want, "JSONW "+c.name+" OK", "JSONR "+c.name+" OK")
			}
			snake := ir.RustSnake(c.root)
			fmt.Fprintf(&main, "    run_case!(%q, %q, %s, crate::%s::%s, %s_load, %s_save, %s_to_json_checked, %s_from_json);\n",
				c.name, wirePath, jsonPath, u.module, table, snake, snake, snake, snake)
		}
	}
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(mods.String()+main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the maps/union-arm codec did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the maps/union-arm probe did not run: %v\n%s", err, out)
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
	t.Logf("probe output (%d expected lines):\n%s", len(want), out)
}

const mapsArmsProbeSrc = `package armsprobe

enum Grade { Bronze, Silver, Gold }

fixed table Anchor
{
    x int32
}

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
    huge  uint128
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
    u128_v  uint128
    i128_v  int128 | min = -99999999999999999999, max = 99999999999999999999
    head    *Leaf
    twig    Inner
    after   int32
}
`

var mapsArmsProbeTexts = []struct{ name, text string }{
	{"arms_empty", `{}`},
	{"arms_leaf", `{"pick": {"leaf": {"id": 7, "name": "seven"}}, "after": 1}`},
	{"arms_leaf_default", `{"pick": {"leaf": {}}}`},
	{"arms_num", `{"pick": {"num": -5}}`},
	{"arms_num_zero", `{"pick": {"num": 0}}`},
	{"arms_wide", `{"pick": {"wide": 18446744073709551615}}`},
	{"arms_huge", `{"pick": {"huge": 340282366920938463463374607431768211455}, "u128_v": 340282366920938463463374607431768211455, "i128_v": -99999999999999999999}`},
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
