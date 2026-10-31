package jstable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsFloat32CellsAreBitTransparent holds the JavaScript leg to the FINITE
// rule of 2026-10-27 (docs/SPEC-TABLES.md §3.3; Glenn: "Tables go finite-only
// too, same rule as packets. NaN/inf refused on write and read, -0 and
// subnormals kept"). A finite float rides every form as its bits, -0 and the
// subnormals included, and a -0 under a +0 default rides (elision compares
// bits); a NaN or an infinity is refused by every writer and by every read on
// its raw bits as non_finite_float, the field named. The block and record
// accessors (ruling 2) refuse one too, latched on the view, and never return
// it. A retained unknown float is tested like a known one (ruling 3).
//
// The text half holds the NaN payload surgery (docs/PORTING.md M21) gone from
// every generated module; the node half runs the behaviour under SCHEMA_SLOW.
func TestJsFloat32CellsAreBitTransparent(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, jsFloatProbeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for name, data := range files {
		src := string(data)
		for _, retired := range []string{"(payload >>> 3)", "TableVariableWidenF32", "TableVariableNarrowF32", "narrowF32", "0x007fffff) !== 0"} {
			if strings.Contains(src, retired) {
				t.Errorf("%s still carries the retired NaN payload surgery %q (docs/PORTING.md M21)", name, retired)
			}
		}
	}
	for _, want := range []string{"NonFiniteFloat: 18,", `return "non_finite_float";`} {
		if !strings.Contains(string(files["ProbeTable.js"]), want) {
			t.Errorf("ProbeTable.js does not carry %q", want)
		}
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node")
		if fails, out := jsFloatProbe(t, nil); len(fails) != 0 || strings.TrimSpace(out) != "ok" {
			t.Fatalf("node probe failed:\n%s", out)
		}
	})
}

// jsFloatProbeSchema and jsFloatProbeNarrow are the probe's two units: the one it reads and writes,
// and the same Var one version on, with `a` dropped, whose reader meets the
// first unit's `a` as an UNKNOWN kind-10 field and retains it (ruling 3).
const (
	jsFloatProbeSchema = `package probe

type RowCell
{
    x float32
    w float64
}

fixed table Cell
{
    x float32
    y float64
    rows [1]RowCell
}

table Var
{
    a float32
    b float64
    c [2]float32
    d [..4]float64
}
`
	jsFloatProbeNarrow = `package narrow

table Var
{
    b float64
    c [2]float32
    d [..4]float64
}
`
)

// jsFloatProbe writes both units' generated trees into one directory, applies
// `sabotage` to the generated text (nil for none), runs floatProbe under the
// leg's own node and answers its FAIL lines and its whole output.
func jsFloatProbe(t *testing.T, sabotage func(files map[string][]byte)) ([]string, string) {
	t.Helper()
	node := jsNode(t)
	u := unitFrom(t, jsFloatProbeSchema)
	files := generatedTree(u)
	for name, data := range generatedTree(unitFrom(t, jsFloatProbeNarrow)) {
		if _, taken := files[name]; !taken {
			files[name] = data
		}
	}
	maps.DeleteFunc(files, func(name string, _ []byte) bool { return strings.Contains(name, "/") })
	if sabotage != nil {
		sabotage(files)
	}
	dir := t.TempDir()
	files["ann.bin"] = ir.TableAnnouncement(u)
	files["probe.mjs"] = []byte(floatProbe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var fails []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "FAIL: ") {
			fails = append(fails, line)
		}
	}
	if err != nil && len(fails) == 0 {
		t.Fatalf("the probe died rather than failing a row: %v\n%s", err, out)
	}
	return fails, string(out)
}

// TestJsFiniteFloatNegativeControl is the reversed witness of the finite rule's
// READ checks (docs/SPEC-TABLES.md §3.3; docs/CONTRIBUTING.md, Negative
// controls): each sabotage turns one read's raw-bits test off in the generated
// text, and the probe must go red on EXACTLY that read's rows and on no other.
// A sabotage that patches nothing refuses, so a drifted anchor cannot read as
// a pass. Run as make tables-js-finite-float-negative-control.
func TestJsFiniteFloatNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	for _, c := range []struct {
		name, file, from, to string
		rows                 []string // every FAIL line names one of these, and each is named
	}{
		{"variable read", "ProbeVariable.js",
			"if ((b & 0x7f800000) === 0x7f800000) { TableVariableNonFinite(report, d);",
			"if (false) { TableVariableNonFinite(report, d);",
			[]string{"VarLoad did not refuse Var.a", "VarLoad did not refuse Var.c", "VarLoad with no report landed Var.a", "VarLoad with no report landed Var.c"}},
		{"retained read", "NarrowVariable.js",
			"if (!TableVariableRetainedFinite(r.b, from, r.at, kind)) {",
			"if (false) {",
			[]string{"the narrow reader retained a NaN"}},
		{"block accessor", "ProbeBlock.js",
			"if ((view.getUint32(at, true) & 0x7f800000) === 0x7f800000) { TableBlockRefuseFloat(view, field); return 0; }",
			"",
			[]string{"RowCellRow.X returned", "RowCellRow.X latched", "CellBlock.X did not refuse", "a second refusal replaced the latched name"}},
		{"fixed content pass", "ProbeTable.js",
			"& 0x7f800000) === 0x7f800000) { report.refused = TableFixedRefusal.NonFiniteFloat;",
			"& 0x7f800000) === 0x1) { report.refused = TableFixedRefusal.NonFiniteFloat;",
			[]string{"CellFixedDecode landed"}},
		{"message read", "ProbeTable.js",
			"(b & 0x7f800000) === 0x7f800000) { if (!r.Report.NonFiniteFloat)",
			"(b & 0x7f800000) === 0x1) { if (!r.Report.NonFiniteFloat)",
			[]string{"CellLoadMessages did not refuse the NaN"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fails, out := jsFloatProbe(t, func(files map[string][]byte) {
				src := string(files[c.file])
				if !strings.Contains(src, c.from) {
					t.Fatalf("the sabotage patches nothing: %s does not carry %q", c.file, c.from)
				}
				files[c.file] = []byte(strings.ReplaceAll(src, c.from, c.to))
			})
			if len(fails) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the probe green:\n%s", c.name, out)
			}
			named := map[string]bool{}
			for _, line := range fails {
				hit := false
				for _, row := range c.rows {
					if strings.Contains(line, row) {
						named[row], hit = true, true
					}
				}
				if !hit {
					t.Errorf("the %s sabotage reddened a row it does not own: %s", c.name, line)
				}
			}
			for _, row := range c.rows {
				if !named[row] {
					t.Errorf("the %s sabotage left its row %q green:\n%s", c.name, row, out)
				}
			}
		})
	}
}

const floatProbe = `import { readFileSync } from "node:fs";
import { CellBlock, RowCellRow, TableBlockNonFiniteFloat } from "./ProbeBlock.js";
import {
  Cell, CellFixedWriteBody, CellFixedDecode, CellFixedSave, CellFixedLoad, CellFixedMeasure, CellFixedNewPlan,
  TableFixedRun, TableFixedReport, TableFixedLanes, TableFixedRefusal, TableFixedRefusalName,
  TableVocabulary, AnnounceRead, TableMessageEntriesHere,
} from "./ProbeTable.js";
import { CellSaveMessages, CellLoadMessages } from "./ProbeTable.js";
import { Var, VarSave, VarLoad, VarMeasureReason, TableVariableReport } from "./ProbeVariable.js";
import * as narrow from "./NarrowVariable.js";

let failures = 0;
function fail(why) { console.error("FAIL: " + why); failures++; }
const scratch = new DataView(new ArrayBuffer(8));
function f32bits(v) { scratch.setFloat32(0, v, true); return scratch.getUint32(0, true); }
function f32of(b) { scratch.setUint32(0, b, true); return scratch.getFloat32(0, true); }
function f64bits(v) { scratch.setFloat64(0, v, true); return scratch.getBigUint64(0, true); }
function f64of(b) { scratch.setBigUint64(0, b, true); return scratch.getFloat64(0, true); }
const hex = (n) => "0x" + n.toString(16);

// -0, the smallest and largest subnormals, 1, the largest finite, its negative
const finite32 = [0x80000000, 0x00000001, 0x80000001, 0x007fffff, 0x3f800000, 0x7f7fffff, 0xff7fffff];
// a signalling and a quiet NaN of both signs, a payload, both infinities
const nonFinite32 = [0x7f800001, 0xff800001, 0x7fc00000, 0xffc00000, 0x7fc0dead, 0x7f800000, 0xff800000];
const finite64 = [0x8000000000000000n, 0x0000000000000001n, 0x000fffffffffffffn, 0x3ff0000000000000n, 0x7fefffffffffffffn];
const nonFinite64 = [0x7ff0000000000001n, 0xfff8000000000000n, 0x7ff0000000000000n, 0xfff0000000000000n];

// ---- 1) the block and record accessors (ruling 2): latched on the view ----
for (const want of finite32) {
  const buf = new Uint8Array(64);
  const view = new DataView(buf.buffer);
  view.setUint32(0, want, true);
  const v = RowCellRow.X(view, 0);
  if (f32bits(v) !== want) { fail("RowCellRow.X moved " + hex(want) + " to " + hex(f32bits(v))); }
  view.setUint32(24, want, true);
  const bv = CellBlock.X({ View: view, Bytes: buf });
  if (f32bits(bv) !== want) { fail("CellBlock.X moved " + hex(want) + " to " + hex(f32bits(bv))); }
  if (TableBlockNonFiniteFloat(view) !== "") { fail("a finite read latched " + TableBlockNonFiniteFloat(view)); }
}
for (const bad of nonFinite32) {
  const buf = new Uint8Array(64);
  const view = new DataView(buf.buffer);
  view.setUint32(0, bad, true);
  const v = RowCellRow.X(view, 0);
  if (!Object.is(v, 0)) { fail("RowCellRow.X returned " + v + " for " + hex(bad) + ", want +0 and never the value"); }
  if (TableBlockNonFiniteFloat(view) !== "RowCell.x") { fail("RowCellRow.X latched '" + TableBlockNonFiniteFloat(view) + "' for " + hex(bad)); }
  // the first name wins
  const other = new Uint8Array(64);
  const ov = new DataView(other.buffer);
  ov.setUint32(24, bad, true);
  if (!Object.is(CellBlock.X({ View: ov, Bytes: other }), 0) || TableBlockNonFiniteFloat(ov) !== "Cell.x") {
    fail("CellBlock.X did not refuse " + hex(bad) + " as Cell.x: '" + TableBlockNonFiniteFloat(ov) + "'");
  }
  ov.setUint32(0, bad, true);
  RowCellRow.X(ov, 0);
  if (TableBlockNonFiniteFloat(ov) !== "Cell.x") { fail("a second refusal replaced the latched name"); }
}
for (const want of finite64) {
  const buf = new Uint8Array(64);
  const view = new DataView(buf.buffer);
  for (let at = 0; at < 64; at += 8) { view.setBigUint64(at, want, true); }
  const v = RowCellRow.W(view, 0);
  if (f64bits(v) !== want) { fail("RowCellRow.W moved " + hex(want) + " to " + hex(f64bits(v))); }
  if (TableBlockNonFiniteFloat(view) !== "") { fail("a finite float64 read latched"); }
}
for (const bad of nonFinite64) {
  const buf = new Uint8Array(64);
  const view = new DataView(buf.buffer);
  for (let at = 0; at < 64; at += 8) { view.setBigUint64(at, bad, true); }
  if (!Object.is(RowCellRow.W(view, 0), 0) || TableBlockNonFiniteFloat(view) !== "RowCell.w") {
    fail("RowCellRow.W did not refuse " + hex(bad) + ": '" + TableBlockNonFiniteFloat(view) + "'");
  }
}

// ---- 2) the fixed form: the content pass refuses, the writer refuses ----
for (const want of finite32) {
  const inBuf = new Uint8Array(64);
  const inView = new DataView(inBuf.buffer);
  inView.setUint32(0, want, true);
  const cell = new Cell();
  const rep = new TableFixedReport();
  CellFixedDecode(cell, inView, 0, rep);
  if (rep.refused !== 0) { fail("CellFixedDecode refused the finite " + hex(want)); }
  const outBuf = new Uint8Array(64);
  const outView = new DataView(outBuf.buffer);
  CellFixedWriteBody(outView, 0, cell);
  if (outView.getUint32(0, true) !== want) { fail("CellFixedDecode/WriteBody moved " + hex(want) + " to " + hex(outView.getUint32(0, true))); }
}
for (const bad of nonFinite32) {
  const inBuf = new Uint8Array(64);
  const inView = new DataView(inBuf.buffer);
  inView.setUint32(0, bad, true);
  const cell = new Cell();
  cell.x = 7;
  const rep = new TableFixedReport();
  CellFixedDecode(cell, inView, 0, rep);
  if (rep.refused !== TableFixedRefusal.NonFiniteFloat || cell.x !== 7) {
    fail("CellFixedDecode landed " + hex(bad) + ": refused=" + rep.refused + " x=" + cell.x);
  }
}
for (const bad of nonFinite64) {
  const inBuf = new Uint8Array(64);
  const inView = new DataView(inBuf.buffer);
  inView.setBigUint64(4, bad, true);
  const rep = new TableFixedReport();
  CellFixedDecode(new Cell(), inView, 0, rep);
  if (TableFixedRefusalName(rep.refused) !== "non_finite_float") { fail("CellFixedDecode did not refuse the float64 " + hex(bad)); }
}
for (const [field, value] of [["x", NaN], ["x", Infinity], ["x", -Infinity], ["x", 1e39], ["y", NaN], ["y", -Infinity]]) {
  const cell = new Cell();
  cell[field] = value;
  const bytes = new Uint8Array(CellFixedMeasure(1)).fill(0xa5);
  if (CellFixedSave([cell], 1, bytes) !== -1 || !bytes.every((b) => b === 0xa5)) {
    fail("CellFixedSave wrote " + field + "=" + value + ", want -1 and the buffer untouched");
  }
  const rowCell = new Cell();
  rowCell.rows[0][field === "x" ? "x" : "w"] = value;
  if (CellFixedSave([rowCell], 1, new Uint8Array(CellFixedMeasure(1))) !== -1) {
    fail("CellFixedSave wrote a nested " + field + "=" + value);
  }
}
{
  const cell = new Cell();
  cell.x = -0; cell.y = -0;
  const bytes = new Uint8Array(CellFixedMeasure(1));
  if (CellFixedSave([cell], 1, bytes) < 0) { fail("CellFixedSave refused -0"); }
  const back = [new Cell()];
  const rep = new TableFixedReport();
  if (CellFixedLoad(back, 1, bytes, bytes.length, CellFixedNewPlan(), rep) !== 1 || !Object.is(back[0].x, -0) || !Object.is(back[0].y, -0)) {
    fail("the fixed form lost -0: x=" + back[0].x + " y=" + back[0].y + " refused=" + rep.refused);
  }
}

// ---- 3) the fixed plan's float rung: finite exact by the ordinary conversion ----
for (const want of finite32) {
  const src = new Uint8Array(4);
  new DataView(src.buffer).setUint32(0, want, true);
  const dst = new Uint8Array(8);
  const dstView = new DataView(dst.buffer);
  const plan = new Int32Array(TableFixedLanes);
  plan[0] = 6; plan[1] = 0; plan[2] = 0; plan[3] = 4; plan[5] = -1; plan[8] = 1;
  TableFixedRun(plan, 1, src, new DataView(src.buffer), 0, dst, dstView, null, new TableFixedReport());
  if (f32bits(dstView.getFloat64(0, true)) !== want || f64bits(dstView.getFloat64(0, true)) !== f64bits(f32of(want))) {
    fail("TableFixedOpWidenF moved " + hex(want));
  }
}

// ---- 4) the variable form ----
function variable(a, b, c, d) { const v = new Var(); v.a = a; v.b = b; v.c = c; v.d = d; return v; }
{
  // -0 RIDES under a +0 default, as a scalar, a [2] array and a list element
  const wire = VarSave(variable(-0, -0, [-0, -0], [-0]));
  const rep = new TableVariableReport();
  const back = VarLoad(wire, rep);
  if (back === null || !Object.is(back.a, -0) || !Object.is(back.b, -0) || !Object.is(back.c[0], -0) || !Object.is(back.c[1], -0) || !Object.is(back.d[0], -0)) {
    fail("the variable form lost -0: " + JSON.stringify(rep));
  }
  // and the subnormals ride exact
  const sub = VarLoad(VarSave(variable(f32of(1), 5e-324, [f32of(0x807fffff), 0], [])), new TableVariableReport());
  if (sub === null || f32bits(sub.a) !== 1 || sub.b !== 5e-324 || f32bits(sub.c[0]) !== 0x807fffff) { fail("the variable form moved a subnormal"); }
}
for (const [what, v] of [
  ["a NaN", variable(NaN, 0, [0, 0], [])], ["an infinity", variable(0, Infinity, [0, 0], [])],
  ["a float32 past its range", variable(1e39, 0, [0, 0], [])], ["an element", variable(0, 0, [0, -Infinity], [])],
  ["a list element", variable(0, 0, [0, 0], [1, NaN])],
]) {
  if (VarSave(v) !== null) { fail("VarSave wrote " + what); }
  const [n, why] = VarMeasureReason(v);
  if (n !== -1 || why !== "non_finite_float") { fail("VarMeasureReason answered [" + n + ", " + why + "] for " + what); }
}
// a READ refuses one on its raw bits, the tolerant read included, and names it
function patch32(wire, from, to) {
  const out = Uint8Array.from(wire);
  const dv = new DataView(out.buffer);
  let hits = 0;
  for (let i = 0; i + 4 <= out.length; i++) { if (dv.getUint32(i, true) === from) { dv.setUint32(i, to, true); hits++; } }
  if (hits !== 1) { fail("the pattern " + hex(from) + " is in the wire " + hits + " times"); }
  return out;
}
function patch64(wire, from, to) {
  const out = Uint8Array.from(wire);
  const dv = new DataView(out.buffer);
  let hits = 0;
  for (let i = 0; i + 8 <= out.length; i++) { if (dv.getBigUint64(i, true) === from) { dv.setBigUint64(i, to, true); hits++; } }
  if (hits !== 1) { fail("the pattern " + hex(from) + " is in the wire " + hits + " times"); }
  return out;
}
const marked = VarSave(variable(1.2345678, 2.718281828459045, [0, 3.1415927], [6.02214076e23]));
for (const [field, wire] of [
  ["Var.a", patch32(marked, f32bits(1.2345678), 0x7f800001)],
  ["Var.b", patch64(marked, f64bits(2.718281828459045), 0x7ff0000000000000n)],
  ["Var.c", patch32(marked, f32bits(3.1415927), 0xffc00000)],
  ["Var.d", patch64(marked, f64bits(6.02214076e23), 0xfff8000000000001n)],
]) {
  const rep = new TableVariableReport();
  if (VarLoad(wire, rep) !== null || rep.refused !== 1 || rep.reason !== "non_finite_float: " + field) {
    fail("VarLoad did not refuse " + field + ": " + JSON.stringify(rep));
  }
  if (VarLoad(wire) !== null) { fail("VarLoad with no report landed " + field); }
}
// RULING 3: a retained unknown float is tested like a known one
{
  const store = () => new narrow.TableVariableRetain(new Uint8Array(1 << 16), new Array(1 << 10));
  const rep = new narrow.TableVariableReport();
  const ok = narrow.VarLoadRetain(marked, store(), rep);
  if (ok === null || rep.retained !== 1) { fail("the narrow reader did not retain the finite a: " + JSON.stringify(rep)); }
  const bad = new narrow.TableVariableReport();
  const wire = patch32(marked, f32bits(1.2345678), 0x7fc00000);
  if (narrow.VarLoadRetain(wire, store(), bad) !== null || !bad.reason.startsWith("non_finite_float: Var.0x")) {
    fail("the narrow reader retained a NaN: " + JSON.stringify(bad));
  }
  if (narrow.VarLoad(wire, new narrow.TableVariableReport()) === null) { fail("a plain read refused an unknown float it does not retain"); }
}

// ---- 5) the message form ----
{
  const v = new TableVocabulary();
  v.Init(new Array(TableMessageEntriesHere));
  if (!AnnounceRead(v, new Uint8Array(readFileSync("./ann.bin")), {})) { fail("AnnounceRead"); }
  for (const [field, value] of [["x", NaN], ["x", 1e39], ["y", Infinity]]) {
    const cell = new Cell();
    cell[field] = value;
    if (CellSaveMessages([cell], new Uint8Array(256), {}) !== -1) { fail("CellSaveMessages wrote " + field + "=" + value); }
  }
  // -0 rides the message form too
  const zero = new Cell();
  zero.x = -0;
  const zbuf = new Uint8Array(256);
  const zn = CellSaveMessages([zero], zbuf, {});
  const zback = [new Cell()];
  if (zn < 0 || CellLoadMessages(zback, v, zbuf.subarray(0, zn), {}) !== 1 || !Object.is(zback[0].x, -0)) { fail("the message form lost -0"); }
  // a NaN on the wire refuses the batch whole: find the marked pattern's bit offset
  const cell = new Cell();
  cell.x = 1.2345678;
  const buf = new Uint8Array(256);
  const n = CellSaveMessages([cell, new Cell()], buf, {});
  const wire = buf.slice(0, n);
  const want = f32bits(1.2345678);
  const bit = (o) => (wire[o >> 3] >> (o & 7)) & 1;
  const at = [];
  for (let o = 0; o + 32 <= wire.length * 8; o++) {
    let x = 0;
    for (let i = 31; i >= 0; i--) { x = (x * 2) + bit(o + i); }
    if (x === want) { at.push(o); }
  }
  if (at.length !== 1) { fail("the marked float is at " + at.length + " bit offsets"); }
  else {
    const o = at[0] + 30;
    wire[o >> 3] |= 1 << (o & 7);
    const storage = [new Cell(), new Cell()];
    storage[1].y = 9;
    const report = {};
    const got = CellLoadMessages(storage, v, wire, report);
    if (got !== -1 || report.Reason !== "non_finite_float: Cell.x" || storage[0].x !== 0) {
      fail("CellLoadMessages did not refuse the NaN: " + got + " " + JSON.stringify(report));
    }
  }
}

if (failures !== 0) { process.exit(1); }
console.log("ok");
`
