package jstable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY JAVASCRIPT WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero). The variable save and its retaining twin, the
// text writer, the cook, the fixed save and the message form's elision stored
// a compressed -0 as held. The plain float `p` is the control: its -0 is a
// value and rides bit-exact. The Go reference engine's corpus,
// testdata/conformance/feature-witnesses/compressed-negzero-go, is the
// worked example; its 15 rows run here too.
const jsNegZeroCorpus = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// TestJavaScriptCompressedFloatStoresAreGuarded is the fast half: it reads the
// generated source rather than running it. Every fixed-form store of a
// compressed float is a +0 test, the plain float's is not; every cook
// descriptor of a compressed float carries c:1 and the plain one does not; the
// message form elides a compressed float by value and the plain one by its
// bits; and the variable runtime routes every store, elision and text value
// through TableVariableStoredFloat.
func TestJavaScriptCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join(jsNegZeroCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	guarded, plain := 0, 0
	cookC, cookPlain := 0, 0
	cookLeaf := regexp.MustCompile(`\{k:"f32",n:"(\w+)",at:\d+(,c:1)?\}`)
	for name, data := range files {
		all.Write(data)
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "view.setFloat32(") && strings.Contains(line, "value.") {
				isPlain := strings.Contains(line, "value.p,")
				isGuarded := strings.Contains(line, " === 0 ? 0 : ")
				switch {
				case isPlain && isGuarded:
					t.Errorf("%s:%d: the plain float's -0 is normalized: %s", name, i+1, strings.TrimSpace(line))
				case isPlain:
					plain++
				case !isGuarded:
					t.Errorf("%s:%d: a compressed float is stored as held: %s", name, i+1, strings.TrimSpace(line))
				default:
					guarded++
				}
			}
			for _, m := range cookLeaf.FindAllStringSubmatch(line, -1) {
				switch {
				case m[1] == "p" && m[2] != "":
					t.Errorf("%s:%d: the plain float's cook descriptor is marked compressed: %s", name, i+1, m[0])
				case m[1] == "p":
					cookPlain++
				case m[2] == "":
					t.Errorf("%s:%d: a compressed float's cook descriptor is not marked: %s", name, i+1, m[0])
				default:
					cookC++
				}
			}
		}
	}
	if guarded == 0 || plain == 0 {
		t.Errorf("the fixed writers have %d guarded compressed stores and %d plain ones; want both", guarded, plain)
	}
	if cookC == 0 || cookPlain == 0 {
		t.Errorf("the cook descriptors have %d compressed leaves and %d plain ones; want both", cookC, cookPlain)
	}
	src := all.String()
	for _, want := range []string{
		"if (fd.c === 1 && value === 0) { value = 0; }",
		"function TableVariableStoredFloat(d, v) {",
		"  v = TableVariableStoredFloat(d, v);\n  if (d.f32) { w.f32(v); } else { w.f64(v); }",
		"if (!TableVariableSameFloat(fd.f32, TableVariableStoredFloat(fd, val), fd.def)) {",
		"return TableVariableSameFloat(e.f32, TableVariableStoredFloat(e, x), e.def);",
		"const t = TableVariableFloatText(TableVariableStoredFloat(d, val), d.f32);",
		"if (globalThis.Math.fround(value.z) !== 0) {",
		"if (!globalThis.Object.is(globalThis.Math.fround(value.p), 0)) {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the generated module does not carry %q", want)
		}
	}
	if strings.Contains(src, "Object.is(value.z, 0)") {
		t.Errorf("the message form still elides the compressed z by its bits")
	}
}

// TestJavaScriptCompressedNegZeroWriters runs the corpus's 15 rows and every
// writer's -0/+0 twin under node.
func TestJavaScriptCompressedNegZeroWriters(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	schema, err := os.ReadFile(filepath.Join(jsNegZeroCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := filepath.Abs(jsNegZeroCorpus)
	if err != nil {
		t.Fatal(err)
	}
	files := generatedTree(unitFrom(t, string(schema)))
	dir := t.TempDir()
	files["probe.mjs"] = []byte(jsNegZeroProbe)
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := testproc.Command(t, node, filepath.Join(dir, "probe.mjs"), corpus)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "\nPASS rows=15 twins=84\n") {
		t.Fatalf("node probe: %v\n%s", err, out)
	}
}

const jsNegZeroProbe = `import { readFileSync } from "node:fs";
import { Var, Fix, VarSave, FixSave, VarToJson, FixToJson, VarFromJson, FixFromJson, VarCookMeasure, VarCookFrom, FixCookMeasure, FixCookFrom, VarMeasureRetain, VarSaveRetain, TableVariableRetain, TableVariableReport } from "./NzgoVariable.js";
import { Var as FVar, Fix as FFix, TableFixedReport } from "./NzgoTable.js";
import { VarFixedMeasure, VarFixedSave, FixFixedMeasure, FixFixedSave, FixFixedLoad, FixFixedNewPlan, FixMeasureMessages, FixSaveMessages } from "./ProbeTable.js";
import { Level, PickType } from "./Probe.js";

const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
let bad = 0;
const fail = (s) => { console.log("FAIL " + s); bad++; };
const enc = (s) => s === null ? null : new TextEncoder().encode(s);
const cook = (measure, from, v, order) => { const n = measure(v); if (n < 0) { return null; } const b = new Uint8Array(n); return from(v, b, order) ? b : null; };
const fixed = (measure, save, v) => { const b = new Uint8Array(measure(1)); return save([v], 1, b) === b.length ? b : null; };
const message = (v) => { const r = {}; const n = FixMeasureMessages([v], r); if (n < 0) { return null; } const b = new Uint8Array(n); return FixSaveMessages([v], b, r) === n ? b : null; };

// THE CORPUS: load <base>, set <path> to -0 in the native value, save <form>
// and owe <expect>.<form>.bin. A fixed row loads <base>.fixed.bin, which the
// Go reference engine pins as its encoding of <base>.json.
function setPath(v, path, fixedForm) {
  const dot = path.indexOf(".");
  if (dot >= 0) {
    const f = path.slice(0, dot), arm = path.slice(dot + 1);
    if (fixedForm) {
      if (v[f].type !== PickType[arm.toUpperCase()]) { throw new Error(path + ": the base does not set arm " + arm); }
      v[f][arm] = -0;
    } else {
      if (v[f] === null || v[f].arm !== arm) { throw new Error(path + ": the base does not set arm " + arm); }
      v[f].value = -0;
    }
    return;
  }
  const br = path.indexOf("[");
  if (br < 0) { v[path] = -0; return; }
  const f = path.slice(0, br), index = path.slice(br + 1, -1);
  const slot = /^[0-9]+$/.test(index) ? Number(index) : Level[index] - 1;
  if (!(slot >= 0 && slot < v[f].length)) { throw new Error(path + ": slot " + slot + " of " + v[f].length); }
  v[f][slot] = -0;
}
const dir = process.argv[2];
let rows = 0;
for (const line of readFileSync(dir + "/cases.tsv", "utf8").split("\n")) {
  if (line === "" || line.startsWith("#")) { continue; }
  const [name, form, root, base, path, expect] = line.split("\t");
  const want = readFileSync(dir + "/" + expect + "." + form + ".bin");
  let got = null;
  try {
    if (form === "variable") {
      const r = new TableVariableReport();
      const v = (root === "Var" ? VarFromJson : FixFromJson)(readFileSync(dir + "/" + base + ".json", "utf8"), r);
      if (v === null) { fail(name + ": " + base + ".json does not read: " + JSON.stringify(r)); continue; }
      setPath(v, path, false);
      got = (root === "Var" ? VarSave : FixSave)(v);
    } else {
      if (root !== "Fix") { fail(name + ": a fixed row reads Fix"); continue; }
      const data = readFileSync(dir + "/" + base + ".fixed.bin");
      const back = []; for (let k = 0; k < 8; k++) { back.push(new FFix()); }
      const r = new TableFixedReport();
      if (FixFixedLoad(back, back.length, data, data.length, FixFixedNewPlan(4096, 4096), r) !== 1) { fail(name + ": " + base + ".fixed.bin does not load"); continue; }
      setPath(back[0], path, true);
      got = fixed(FixFixedMeasure, FixFixedSave, back[0]);
    }
  } catch (e) { fail(name + ": " + e.message); continue; }
  rows++;
  if (got === null) { fail(name + ": the " + form + " save refused"); continue; }
  if (hex(got) !== hex(want)) { fail(name + ": the " + form + " save of -0 at " + path + " is not " + expect + "." + form + ".bin:\n have " + hex(got) + "\n want " + hex(want)); }
}

// THE TWINS: every writer saves a value holding -0 in one cell and owes the
// bytes its +0 twin writes; the plain float p owes different bytes.
const cases = [
  ["leaf_nonzero_default", false, (v, z) => { v.a = z; }, (v, z) => { v.a = z; }],
  ["leaf_zero_default", false, (v, z) => { v.z = z; }, (v, z) => { v.z = z; }],
  ["array_element", false, (v, z) => { v.xs[0] = z; v.xs[1] = 0.05; }, (v, z) => { v.xs[0] = z; v.xs[1] = 0.05; }],
  ["counted_element", false, (v, z) => { v.ys = [0.05, z]; }, (v, z) => { v.ys[0] = 0.05; v.ys[1] = z; v.ysCount = 2; }],
  ["keyed_slot", false, (v, z) => { v.ks[0] = z; v.ks[1] = 0.05; }, (v, z) => { v.ks[0] = z; v.ks[1] = 0.05; }],
  ["union_arm", false, (v, z) => { v.pick = { arm: "v", value: z }; }, (v, z) => { v.pick.type = PickType.V; v.pick.v = z; }],
  ["plain_control", true, (v, z) => { v.p = z; }, (v, z) => { v.p = z; }],
];
const writers = [
  ["Var", "save", false, () => new Var(), (v) => VarSave(v)],
  ["Var", "save_retain", false, () => new Var(), (v) => { const r = new TableVariableRetain(null, null); VarMeasureRetain(v, r); return VarSaveRetain(v, r, {}); }],
  ["Var", "json", false, () => new Var(), (v) => enc(VarToJson(v))],
  ["Var", "cook_le", false, () => new Var(), (v) => cook(VarCookMeasure, VarCookFrom, v, 1)],
  ["Var", "cook_be", false, () => new Var(), (v) => cook(VarCookMeasure, VarCookFrom, v, 2)],
  ["Var", "fixed", true, () => new FVar(), (v) => fixed(VarFixedMeasure, VarFixedSave, v)],
  ["Fix", "save", false, () => new Fix(), (v) => FixSave(v)],
  ["Fix", "json", false, () => new Fix(), (v) => enc(FixToJson(v))],
  ["Fix", "cook_le", false, () => new Fix(), (v) => cook(FixCookMeasure, FixCookFrom, v, 1)],
  ["Fix", "cook_be", false, () => new Fix(), (v) => cook(FixCookMeasure, FixCookFrom, v, 2)],
  ["Fix", "fixed", true, () => new FFix(), (v) => fixed(FixFixedMeasure, FixFixedSave, v)],
  ["Fix", "message", true, () => new FFix(), message],
];
let twins = 0;
for (const [root, writer, fixedForm, make, write] of writers) {
  for (const [name, plain, setV, setF] of cases) {
    const set = fixedForm ? setF : setV;
    const plus = make(), minus = make();
    set(plus, 0); set(minus, -0);
    const tag = root + "/" + writer + "/" + name;
    let p, m;
    try { p = write(plus); m = write(minus); } catch (e) { fail(tag + ": " + e.message); continue; }
    twins++;
    if (p === null || m === null) { fail(tag + ": refused"); continue; }
    const same = hex(p) === hex(m);
    if (plain && same) { fail(tag + ": the plain float's -0 did not ride"); }
    if (!plain && !same) { fail(tag + ": a compressed float's -0 was not written as +0:\n -0 " + hex(m) + "\n +0 " + hex(p)); }
  }
}
console.log(bad === 0 ? "\nPASS rows=" + rows + " twins=" + twins : "\nBAD " + bad);
`
