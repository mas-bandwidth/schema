package jstable

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func runJSConstructProbe(t *testing.T, files map[string][]byte, home, script string, cases any) string {
	t.Helper()
	node := jsNode(t)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"), filepath.Join(dir, "cases.json"), filepath.Join(dir, home))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native probe: %v\n%s", err, out)
	}
	return string(out)
}

func TestJavaScriptOptionalMapConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes populated optional maps)")
	t.Parallel()
	u := unitFrom(t, `package probe

table Child { n int32 }
table Outer {
    tags ?map[string(8)]int32
    by_id ?map[uint32]*Child
    nested ?map[uint8]map[uint8]int32
    tail int32
}
`)
	m := tabletext.NewModel(u)
	type row struct {
		Name  string `json:"name"`
		Build string `json:"build"`
		Wire  string `json:"wire"`
		Cook  string `json:"cook"`
		Text  string `json:"text"`
	}
	var cases []row
	for _, tc := range []struct{ name, input, build string }{
		{"absent", `{"tail":7}`, `Object.assign(new m.Outer(), {tail:7})`},
		{"present-empty", `{"tags":{},"by_id":{},"nested":{},"tail":7}`, `Object.assign(new m.Outer(), {tags:new Map(),by_id:new Map(),nested:new Map(),tail:7})`},
		{"populated", `{"tags":{"b":3,"a":2},"by_id":{"4":{"n":11}},"nested":{"2":{"3":17}},"tail":7}`, `Object.assign(new m.Outer(), {tags:new Map([["b",3],["a",2]]),by_id:new Map([[4,Object.assign(new m.Child(), {n:11})]]),nested:new Map([[2,new Map([[3,17]])]]),tail:7})`},
	} {
		inst := m.New(u.Tables["Outer"])
		var rep tabletext.Report
		if !m.Read(inst, []byte(tc.input), &rep) || !rep.Silent() {
			t.Fatalf("%s oracle read: %+v", tc.name, rep)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		cook, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatal(err)
		}
		txt, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, row{tc.name, tc.build, hex.EncodeToString(wire), hex.EncodeToString(cook), string(txt) + "\n"})
	}
	if cases[0].Wire == cases[1].Wire {
		t.Fatal("oracle erased optional-map presence")
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", optionalVariableDriver, cases)
	for _, c := range cases {
		for _, surface := range []string{"SAVE", "MEASURE", "RELOAD", "JSONW", "JSONR", "COOK"} {
			if !strings.Contains(out, surface+" "+c.Name+" OK\n") {
				t.Errorf("missing %s %s\n%s", surface, c.Name, out)
			}
		}
	}
}

func TestJavaScriptOneSidedRangeConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes one-sided integer clamps)")
	t.Parallel()
	u := unitFrom(t, `package probe

table Probe {
    a int64 | min = 0
    b uint16 | max = 9
    c int8 | min = -3
    d uint64 | max = 18446744073709551610
}
`)
	m := tabletext.NewModel(u)
	type row struct {
		Name        string `json:"name"`
		Wire        string `json:"wire"`
		Saved       string `json:"saved"`
		Clamped     int    `json:"clamped"`
		Input       string `json:"input"`
		JSONSaved   string `json:"jsonSaved"`
		JSONClamped int    `json:"jsonClamped"`
	}
	var cases []row
	for _, tc := range []struct {
		name  string
		index int
		raw   uint64
		input string
	}{
		{"lower-clamp", 0, ^uint64(0), `{"a":-1}`},
		{"lower-edge", 0, 0, `{"a":0}`},
		{"signed-upper", 0, 1<<63 - 1, `{"a":9223372036854775807}`},
		{"upper-clamp", 1, 65535, `{"b":65535}`},
		{"upper-edge", 1, 9, `{"b":9}`},
		{"signed-small-clamp", 2, 128, `{"c":-128}`},
		{"unsigned-upper", 3, ^uint64(0), `{"d":18446744073709551615}`},
	} {
		inst := m.New(u.Tables["Probe"])
		inst.Fields[tc.index].Cell.I = int64(tc.raw)
		inst.Fields[tc.index].Cell.U = tc.raw
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		back := m.New(u.Tables["Probe"])
		var rep tabletext.Report
		if ok, err := tablewire.Decode(m, back, wire, &rep); !ok || err != nil || rep.Malformed {
			t.Fatalf("oracle decode: %+v %v", rep, err)
		}
		saved, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatal(err)
		}
		var jr tabletext.Report
		jsonBack := m.New(u.Tables["Probe"])
		if !m.Read(jsonBack, []byte(tc.input), &jr) || jr.Malformed {
			t.Fatalf("oracle JSON: %+v", jr)
		}
		jsaved, err := tablewire.Encode(m, jsonBack)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, row{tc.name, hex.EncodeToString(wire), hex.EncodeToString(saved), rep.Clamped, tc.input, hex.EncodeToString(jsaved), jr.Clamped})
	}
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := runJSConstructProbe(t, files, runtimeHome(u)+"Variable.js", oneSidedJSProbe, cases)
	for _, c := range cases {
		if !strings.Contains(out, c.Name+" OK\n") {
			t.Errorf("case missing: %s\n%s", c.Name, out)
		}
	}
}

const oneSidedJSProbe = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const cases = JSON.parse(readFileSync(process.argv[2],"utf8"));
const m = await import(pathToFileURL(process.argv[3]).href);
const hex = b => b == null ? "null" : Buffer.from(b).toString("hex");
for (const c of cases) {
  const rep = new m.TableVariableReport();
  const value = m.ProbeLoad(new Uint8Array(Buffer.from(c.wire,"hex")),rep);
  const jr = new m.TableVariableReport();
  const jvalue = m.ProbeFromJson(c.input,jr);
  if (value === null || rep.malformed || rep.clamped !== c.clamped || hex(m.ProbeSave(value)) !== c.saved
      || jvalue === null || jr.malformed || jr.clamped !== c.jsonClamped || hex(m.ProbeSave(jvalue)) !== c.jsonSaved) {
    throw new Error(c.name + " mismatch " + JSON.stringify({rep,jr,wire:hex(m.ProbeSave(value)),json:hex(m.ProbeSave(jvalue))}));
  }
  console.log(c.name + " OK");
}
`

func TestJavaScriptOneSidedRangeFixedConstruct(t *testing.T) {
	slowtest.Gate(t, "node (it executes fixed-form one-sided clamps)")
	t.Parallel()
	const schema = `package probe
fixed table T {
    a int64 | min = 0
    b uint16 | max = 9
    c int8 | min = -3
    d uint64 | max = 18446744073709551610
}
`
	const script = `
// THE WRITER REFUSES THE VALUES THE READER CLAMPS (docs/SPEC.md §5.1): the
// save answers -1 and stores nothing, so the off-range record is forged from
// a lawful one, its four fields overwritten at their declared offsets in the
// body that ends the file (a int64, b uint16, c int8, d uint64).
const value = new T(); InitT(value);
value.a = -1n; value.b = 65535; value.c = -128; value.d = 18446744073709551615n;
const raw = new Uint8Array(TMeasure(1));
if (TSave([value],1,raw) !== -1 || raw.some((x) => x !== 0)) { fail("the fixed writer wrote an off-range value"); }
value.a = 0n; value.b = 9; value.c = -3; value.d = 18446744073709551610n;
if (TSave([value],1,raw) !== raw.length) { fail("raw fixed write failed"); }
const body = new DataView(raw.buffer, raw.length - 19, 19);
body.setBigInt64(0, -1n, true); body.setUint16(8, 65535, true); body.setInt8(10, -128); body.setBigUint64(11, 18446744073709551615n, true);
const back = [new T()]; InitT(back[0]);
const plan = TNewPlan(4096,4096); const report = new TableFixedReport();
const count = TLoad(back,1,raw,raw.length,plan,report);
if (count !== 1 || report.refused !== 0 || report.malformed || report.clamped !== 4
    || back[0].a !== 0n || back[0].b !== 9 || back[0].c !== -3 || back[0].d !== 18446744073709551610n) {
  fail("fixed one-sided clamps: " + show(back) + " " + reason(report));
}
`
	if out, err := jsRunVersionProbe(t, jsNode(t), schema, nil, 0, "T", script); err != nil {
		t.Fatalf("fixed one-sided probe: %v\n%s", err, out)
	}
}
