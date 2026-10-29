package jstable

import (
	"bytes"
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

// TestOptionalVariableClosureJavaScript holds docs/SPEC-TABLES.md §2.3's `?T`
// and by-value `?[N]T` over a VARIABLE closure (a member whose closure holds a
// pointer) on the JavaScript variable form, BYTE FOR BYTE against the Go oracle
// (internal/tablewire, internal/tablecook): every walk that follows pointer
// edges gates on presence, so an absent value's table is not descended and a
// pointer it would hold costs no record.
//
// In this target an absent optional IS `null` — there is no presence companion
// beside a value that could keep a stale pointer — so the gate is the edge
// walk's `fd.opt && val === null` return and the cook's default record. Each
// probe case is saved from a value the probe BUILDS (not one a load produced),
// measured, loaded back and saved again, written as text, and cooked
// little-endian; every one of those is the oracle's bytes. The oracle's own
// stale case (presence cleared with the pointer left in storage) must write the
// absent case's wire, which is the rule the null representation keeps by
// construction.
//
// THE REVERSED WITNESS plants the old walk back: the generated module with the
// presence gate removed from TableVariableFieldEdges must NOT answer the absent
// cases, so a regression that descends an absent optional is caught here.
func TestOptionalVariableClosureJavaScript(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated modules)")
	node := jsNode(t)

	u := unitFrom(t, optionalVariableProbeSrc)
	if why := newVariableClass(u).reason["Outer"]; why != "" {
		t.Fatalf("the probe root is refused: %s", why)
	}
	m := tabletext.NewModel(u)
	outer := m.Lookup("Outer")

	type ovCase struct {
		Name  string `json:"name"`
		Build string `json:"build"` // the JS expression that builds the value
		Wire  string `json:"wire"`  // hex
		Cook  string `json:"cook"`  // hex, little-endian
		Text  string `json:"text"`
	}
	read := func(name, text string) *tabletext.Instance {
		t.Helper()
		inst := m.New(outer)
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", name, rep)
		}
		return inst
	}
	encode := func(name string, inst *tabletext.Instance) ([]byte, []byte, string) {
		t.Helper()
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", name, err)
		}
		cook, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			t.Fatalf("%s: the oracle did not cook: %v", name, err)
		}
		text, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not write text: %v", name, err)
		}
		return wire, cook, string(text) + "\n"
	}

	var cases []ovCase
	for _, pc := range optionalVariableProbeCases {
		wire, cook, text := encode(pc.name, read(pc.name, pc.text))
		cases = append(cases, ovCase{Name: pc.name, Build: pc.build, Wire: hex.EncodeToString(wire), Cook: hex.EncodeToString(cook), Text: text})
	}

	// THE ORACLE'S STALE CASE IS THE ABSENT CASE: presence cleared on a value
	// whose pointer stays in storage writes no record for it (§2.3).
	stale := read("stale", optionalVariableProbeCases[1].text)
	for _, key := range []string{"inner", "row"} {
		f, ok := stale.FieldByKey(key)
		if !ok || !f.Present {
			t.Fatalf("stale: the oracle holds no present %s to clear", key)
		}
		f.Present = false
	}
	staleWire, err := tablewire.Encode(m, stale)
	if err != nil {
		t.Fatalf("stale: the oracle did not encode: %v", err)
	}
	if hex.EncodeToString(staleWire) != cases[0].Wire {
		t.Fatal("the Go oracle did not elide an absent value's stale pointers: the probe's absent case is not the rule")
	}
	if cases[0].Wire == cases[1].Wire {
		t.Fatal("the oracle wrote the same bytes for absent and present: the probe distinguishes nothing")
	}

	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("the variable form was refused: %v", err)
	}
	home := runtimeHome(u) + "Variable.js"
	module, ok := files[home]
	if !ok {
		t.Fatal("no variable module was generated")
	}
	const gate = "  if (fd.opt && val === null) { return; }\n"
	if !bytes.Contains(module, []byte(gate)) {
		t.Fatalf("the generated edge walk carries no presence gate %q", strings.TrimSpace(gate))
	}

	work := t.TempDir()
	spec, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(work, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, source []byte) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, home), source, 0o600); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(optionalVariableDriver), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script, specPath, filepath.Join(dir, home))
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput()
		return string(out)
	}

	out := run(filepath.Join(work, "carried"), module)
	for _, c := range cases {
		for _, w := range []string{"SAVE", "MEASURE", "RELOAD", "JSONW", "JSONR", "COOK"} {
			if !strings.Contains(out, w+" "+c.Name+" OK\n") {
				t.Errorf("%s: the probe did not print %q", c.Name, w+" "+c.Name+" OK")
			}
		}
	}
	if t.Failed() {
		t.Fatalf("probe output:\n%s", out)
	}

	// THE REVERSED WITNESS: the walk with its presence gate removed descends
	// an absent optional, and the absent cases must stop answering.
	planted := bytes.Replace(module, []byte(gate), nil, 1)
	reversed := run(filepath.Join(work, "planted"), planted)
	// the planted module still loads and still answers a fully present value,
	// so what goes red below is the gate and not a module that did not run
	if !strings.Contains(reversed, "SAVE present OK\n") {
		t.Fatalf("the planted module did not answer the present case, so the witness proves nothing:\n%s", reversed)
	}
	for _, name := range []string{"absent", "row_absent"} {
		if strings.Contains(reversed, "SAVE "+name+" OK\n") {
			t.Errorf("%s: the module with the presence gate removed still saved the oracle's bytes — the gate is not what this test holds\n%s", name, reversed)
		}
	}
}

// optionalVariableProbeSrc is the probe unit: `?Inner` and `?[2]Inner` where
// Inner holds a pointer, so both are optionals over a variable closure (§2.3).
const optionalVariableProbeSrc = `package ovprobe

table Inner
{
    text *string
    n    int32
}

table Outer
{
    inner ?Inner
    row   ?[2]Inner
    tail  int32
}
`

// optionalVariableProbeCases are the probe's values: the oracle's text and the
// JavaScript expression that builds the same value directly.
var optionalVariableProbeCases = []struct{ name, text, build string }{
	{"absent", `{"tail":5}`,
		`(() => { const v = new m.Outer(); v.tail = 5; return v; })()`},
	{"present", `{"inner":{"text":"hi","n":3},"row":[{"text":"a"},{"n":7}],"tail":5}`,
		`(() => { const v = new m.Outer(); v.inner = new m.Inner(); v.inner.text = "hi"; v.inner.n = 3; const a = new m.Inner(); a.text = "a"; const b = new m.Inner(); b.n = 7; v.row = [a, b]; v.tail = 5; return v; })()`},
	{"empty", `{"inner":{"text":""},"tail":5}`,
		`(() => { const v = new m.Outer(); v.inner = new m.Inner(); v.inner.text = ""; v.tail = 5; return v; })()`},
	{"row_absent", `{"inner":{"text":"x"},"tail":1}`,
		`(() => { const v = new m.Outer(); v.inner = new m.Inner(); v.inner.text = "x"; v.tail = 1; return v; })()`},
}

// optionalVariableDriver runs one spec over one module: build, save, measure,
// load back and save, write text, read text and save, cook little-endian.
const optionalVariableDriver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
const m = await import(pathToFileURL(process.argv[3]).href);
const hex = (b) => b === null || b === undefined ? "null" : Buffer.from(b).toString("hex");
const clean = (r) => r.unknown === 0 && r.kindMismatch === 0 && r.widened === 0 && r.clamped === 0 && r.duplicate === 0 && !r.malformed;
const say = (what, c, ok, detail) => console.log(what + " " + c.name + (ok ? " OK" : " DIFF " + detail));

for (const c of spec) {
  try {
    const v = eval(c.build);
    const saved = m.OuterSave(v);
    say("SAVE", c, hex(saved) === c.wire, hex(saved) + " want " + c.wire);
    const measured = m.OuterMeasure(v);
    say("MEASURE", c, measured * 2 === c.wire.length, measured);
    const rep = new m.TableVariableReport();
    const back = m.OuterLoad(new Uint8Array(Buffer.from(c.wire, "hex")), rep);
    const again = back === null ? null : m.OuterSave(back);
    say("RELOAD", c, hex(again) === c.wire && clean(rep), hex(again) + " " + JSON.stringify(rep));
    const text = m.OuterToJson(v);
    say("JSONW", c, text === c.text, JSON.stringify(text) + " want " + JSON.stringify(c.text));
    const r2 = new m.TableVariableReport();
    const v2 = m.OuterFromJson(c.text, r2);
    const s2 = v2 === null ? null : m.OuterSave(v2);
    say("JSONR", c, hex(s2) === c.wire && clean(r2), hex(s2) + " " + JSON.stringify(r2));
    const size = m.OuterCookMeasure(v);
    const buf = new Uint8Array(size > 0 ? size : 0);
    const cooked = size > 0 && m.OuterCookFrom(v, buf, 1) === true;
    say("COOK", c, cooked && hex(buf) === c.cook, hex(buf) + " want " + c.cook);
  } catch (e) {
    console.log("THROW " + c.name + " " + e);
  }
}
`
