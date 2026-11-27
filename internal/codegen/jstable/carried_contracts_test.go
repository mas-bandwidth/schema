package jstable

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
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
	cmd := testproc.Command(t, node, filepath.Join(dir, "probe.mjs"), filepath.Join(dir, "cases.json"), filepath.Join(dir, home))
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
	slowtest.Gate(t, "node (it executes one-sided integer bounds)")
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
		Name, Wire, Saved, Input, JSONSaved string
		WireOK, JSONOK                      bool
	}
	var cases []row
	for _, tc := range []struct {
		name  string
		index int
		raw   uint64
		input string
		valid bool
	}{
		{"lower-edge", 0, 0, `{"a":"0"}`, true},
		{"lower-outside", 0, ^uint64(0), `{"a":"-1"}`, false},
		{"signed-upper", 0, 1<<63 - 1, `{"a":"9223372036854775807"}`, true},
		{"upper-edge", 1, 9, `{"b":9}`, true},
		{"upper-outside", 1, 10, `{"b":10}`, false},
		{"signed-small-edge", 2, 253, `{"c":-3}`, true},
		{"signed-small-outside", 2, 252, `{"c":-4}`, false},
		{"unsigned-edge", 3, 18446744073709551610, `{"d":"18446744073709551610"}`, true},
		{"unsigned-outside", 3, 18446744073709551611, `{"d":"18446744073709551611"}`, false},
	} {
		f := u.Tables["Probe"].Fields[tc.index]
		kind := ir.TableWireFieldKind(f)
		wire := []byte{1, 1, byte(kind)}
		for i := 0; i < ir.TableKindWidth(kind); i++ {
			wire = append(wire, byte(tc.raw>>(8*i)))
		}
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		value := m.New(u.Tables["Probe"])
		var report tabletext.Report
		ok, err := tablewire.Decode(m, value, wire, &report)
		if ok != tc.valid || (ok && err != nil) || (!ok && !tablewire.ReadRefusal(err)) {
			t.Fatalf("%s wire oracle: ok=%v err=%v report=%+v", tc.name, ok, err, report)
		}
		c := row{Name: tc.name, Wire: hex.EncodeToString(wire), Input: tc.input, WireOK: ok}
		if ok {
			saved, err := tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
			c.Saved = hex.EncodeToString(saved)
		}
		jv := m.New(u.Tables["Probe"])
		var jr tabletext.Report
		c.JSONOK = m.Read(jv, []byte(tc.input), &jr)
		if c.JSONOK != tc.valid {
			t.Fatalf("%s JSON oracle: ok=%v report=%+v", tc.name, c.JSONOK, jr)
		}
		if c.JSONOK {
			saved, err := tablewire.Encode(m, jv)
			if err != nil {
				t.Fatal(err)
			}
			c.JSONSaved = hex.EncodeToString(saved)
		}
		cases = append(cases, c)
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
 const value = m.ProbeLoad(new Uint8Array(Buffer.from(c.Wire,"hex")),rep);
 const jr = new m.TableVariableReport();
 const jvalue = m.ProbeFromJson(c.Input,jr);
 if ((value!==null)!==c.WireOK || (jvalue!==null)!==c.JSONOK ||
     (value!==null && hex(m.ProbeSave(value))!==c.Saved) ||
     (jvalue!==null && hex(m.ProbeSave(jvalue))!==c.JSONSaved)) {
   throw new Error(c.Name + " mismatch " + JSON.stringify({rep,jr}));
 }
 console.log(c.Name + " OK");
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
const plan = TNewPlan(4096,4096);
for (const [a, b, c, d] of [
  [0n, 9, -3, 18446744073709551610n],
  [9223372036854775807n, 0, 127, 0n],
]) {
  const value = new T(); InitT(value);
  value.a = a; value.b = b; value.c = c; value.d = d;
  const raw = new Uint8Array(TMeasure(1));
  if (TSave([value],1,raw) !== raw.length) { fail("raw fixed write failed"); }
  const back = [new T()]; InitT(back[0]);
  const report = new TableFixedReport();
  const count = TLoad(back,1,raw,raw.length,plan,report);
  if (count !== 1 || report.refused !== 0 || report.damaged || report.oor !== 0
      || back[0].a !== a || back[0].b !== b || back[0].c !== c || back[0].d !== d) {
    fail("fixed one-sided in-range read: " + show(back) + " " + reason(report));
  }
}
// THE WRITER REFUSES A VALUE OFF A ONE-SIDED RANGE BEFORE A BYTE (§5.1 row 1).
for (const [a, b, c, d] of [
  [-1n, 9, -3, 18446744073709551610n],
  [0n, 65535, -3, 18446744073709551610n],
  [0n, 9, -128, 18446744073709551610n],
  [0n, 9, -3, 18446744073709551615n],
]) {
  const value = new T(); InitT(value);
  value.a = a; value.b = b; value.c = c; value.d = d;
  const raw = new Uint8Array(TMeasure(1) + 32);
  raw.fill(0xa5);
  let wrote;
  try { wrote = TSave([value],1,raw); }
  catch (e) { fail("one-sided writer threw: " + e.name); continue; }
  if (wrote !== -1) { fail("one-sided value was written: " + String(a) + "," + b + "," + c + "," + String(d)); continue; }
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] !== 0xa5) { fail("a refused one-sided write changed the buffer at " + i); break; }
  }
}
// THE READER STILL REFUSES THOSE BYTES out_of_range: patch one field at a time
// into a lawful record and hold the read to the named refusal.
const { TFixedRecordBytes: recordBytes } = await import("./ProbeTable.js");
const rec = TMeasure(1) - recordBytes;
const lawful = () => {
  const value = new T(); InitT(value);
  value.a = 0n; value.b = 9; value.c = -3; value.d = 18446744073709551610n;
  const raw = new Uint8Array(TMeasure(1));
  if (TSave([value],1,raw) !== raw.length) { fail("the control one-sided write failed"); }
  return raw;
};
for (const [name, patch] of [
  ["a", (view) => view.setBigInt64(rec + 8, -1n, true)],
  ["b", (view) => view.setUint16(rec + 16, 65535, true)],
  ["c", (view) => view.setInt8(rec + 18, -128)],
  ["d", (view) => view.setBigUint64(rec + 19, 18446744073709551615n, true)],
]) {
  const raw = lawful();
  patch(new DataView(raw.buffer, raw.byteOffset, raw.byteLength));
  const back = [new T()]; InitT(back[0]);
  const report = new TableFixedReport();
  const count = TLoad(back,1,raw,raw.length,plan,report);
  if (count !== -1 || report.refused !== TableFixedRefusal.OutOfRange || report.damaged || report.oor !== 0) {
    fail("fixed one-sided refusal (" + name + "): " + show(back) + " " + reason(report));
  }
}
`
	if out, err := jsRunVersionProbe(t, jsNode(t), schema, nil, 0, "T", script); err != nil {
		t.Fatalf("fixed one-sided probe: %v\n%s", err, out)
	}
}
