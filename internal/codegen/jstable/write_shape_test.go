package jstable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsTableWritersRefuseWrongShapes holds the JavaScript table writers to
// docs/SPEC-CONFORMANCE.md DW-TABLE-SHAPE: a value of the WRONG KIND (null or
// a number where a record, a union or an arm's payload belongs, a string where
// a float32 belongs, a number or null where a bool belongs, anything but the
// root's record as the whole value) is refused by the writer's own answer —
// the variable Save's null with MeasureReason [-1, "invalid_value"], the
// fixed FixedSave's -1 — never thrown, never coerced. A lawful value beside
// each is written.
func TestJsTableWritersRefuseWrongShapes(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsWriteShapeSchema)
	jsDiscardRun(t, u, nil, jsWriteShapeProbe)
	jsFixedShapeRun(t, u)
}

// jsFixedShapeRun writes the unit's fixed modules and runs the fixed probe
// beside them under the leg's own node; the probe imports the module carrying
// SlotFixedSave as `m` and the one carrying the class Slot as `c`.
func jsFixedShapeRun(t *testing.T, u *ir.Unit) {
	t.Helper()
	slowtest.Gate(t, "node (it runs the generated fixed writer)")
	base, err := js.Generate(u)
	if err != nil {
		t.Fatalf("js.Generate: %v", err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(base, files)
	dir := t.TempDir()
	module, classes := "", ""
	for name, data := range base {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "export function SlotFixedSave(") {
			module = name
		}
		if strings.Contains(string(data), "export class Slot ") {
			classes = name
		}
	}
	if classes == "" {
		t.Fatal("no generated module carries the class Slot")
	}
	if module == "" {
		t.Fatal("no generated module carries SlotFixedSave")
	}
	src := "import * as m from \"./" + module + "\";\nimport * as c from \"./" + classes + "\";\nlet failures = 0;\nfunction fail(why) { console.log(\"FAIL: \" + why); failures++; }\n" +
		jsWriteShapeFixedProbe + "\nif (failures !== 0) { process.exit(1); }\nconsole.log(\"ok\");\n"
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, jsNode(t), "probe.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

const jsWriteShapeSchema = `package shapeprobe

type Vec
{
    x float32
}

union Step
{
    idle
    shift Vec
    hop  int32
}

table Root
{
    flag  bool
    f32   float32
    f64   float64
    rec   Vec
    step  Step
    steps [..4]Step
    vecs  []Vec
}

fixed table Slot
{
    on   bool
    rate float32
    at   Vec
}
`

const jsWriteShapeProbe = `
function attempt(fn) {
  try { return fn(); } catch (e) { return "threw " + (e && e.name ? e.name : e); }
}

function lawful() {
  const v = new m.Root();
  v.flag = true;
  v.f32 = 1.5;
  v.f64 = 2.5;
  v.rec = { x: 1 };
  v.step = { arm: "shift", value: { x: 2 } };
  v.steps = [{ arm: "hop", value: 3 }, { arm: "shift", value: { x: 4 } }];
  v.vecs = [{ x: 5 }];
  return v;
}

function at(mut) { const v = lawful(); mut(v); return v; }

const variableCases = [
  ["record field as null", at((v) => { v.rec = null; })],
  ["record field as a number", at((v) => { v.rec = 7; })],
  ["record field as text", at((v) => { v.rec = "x"; })],
  ["union as a number", at((v) => { v.step = 1; })],
  ["union as text", at((v) => { v.step = "x"; })],
  ["union element as a number", at((v) => { v.steps = [1]; })],
  ["arm payload as null", at((v) => { v.step = { arm: "shift", value: null }; })],
  ["arm payload as a number", at((v) => { v.step = { arm: "shift", value: 7 }; })],
  ["record element as null", at((v) => { v.vecs = [null]; })],
  ["record element as a number", at((v) => { v.vecs = [7]; })],
  ["float32 as text", at((v) => { v.f32 = "1.5"; })],
  ["float32 in a record as text", at((v) => { v.rec = { x: "1.5" }; })],
  ["float64 as text", at((v) => { v.f64 = "1.5"; })],
  ["bool as a number", at((v) => { v.flag = 1; })],
  ["whole value as null", null],
  ["whole value as a number", 7],
  ["whole value as text", "x"],
  ["whole value as a bool", true],
];

for (const [name, value] of variableCases) {
  const save = attempt(() => m.RootSave(value));
  const why = attempt(() => m.RootMeasureReason(value));
  if (save !== null) { fail("variable " + name + ": Save answered " + save); }
  if (!Array.isArray(why) || why[0] !== -1 || why[1] !== "invalid_value") {
    fail("variable " + name + ": MeasureReason answered " + JSON.stringify(why));
  }
}
{
  const wire = attempt(() => m.RootSave(lawful()));
  if (!(wire instanceof Uint8Array)) { fail("variable lawful value: Save answered " + wire); }
}

`

const jsWriteShapeFixedProbe = `
function attempt(fn) {
  try { return fn(); } catch (e) { return "threw " + (e && e.name ? e.name : e); }
}

function slot() { const v = new c.Slot(); v.on = true; v.rate = 1.5; v.at.x = 2; return v; }
function fixedSave(value) {
  const b = new Uint8Array(m.SlotFixedMeasure(1));
  return attempt(() => m.SlotFixedSave([value], 1, b));
}
const fixedCases = [
  ["bool as a number", (v) => { v.on = 1; }],
  ["bool as null", (v) => { v.on = null; }],
  ["float32 as text", (v) => { v.rate = "1.5"; }],
  ["record as null", (v) => { v.at = null; }],
  ["record as a number", (v) => { v.at = 7; }],
];
for (const [name, mut] of fixedCases) {
  const v = slot();
  mut(v);
  const got = fixedSave(v);
  if (got !== -1) { fail("fixed " + name + ": FixedSave answered " + got); }
}
for (const [name, value] of [["whole value as null", null], ["whole value as a number", 7], ["whole value as text", "x"]]) {
  const got = fixedSave(value);
  if (got !== -1) { fail("fixed " + name + ": FixedSave answered " + got); }
}
{
  const got = fixedSave(slot());
  if (got !== m.SlotFixedMeasure(1)) { fail("fixed lawful value: FixedSave answered " + got); }
}
`
