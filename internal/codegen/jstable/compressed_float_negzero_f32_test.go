package jstable

import (
	"encoding/hex"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A compressed float under a +0 default (z), one under a non-zero default
// (nz), and one whose declared top is 0.1, a decimal float32 cannot hold:
// its float32 end is 0.10000000149011612.
const jsCompressedVariableSchema = `package probe

table Probe
{
    z float32 | min = -1, max = 1, resolution = 0.01
    nz float32 = 0.5 | min = -1, max = 1, resolution = 0.01
    tenth float32 | min = 0, max = 0.1, resolution = 0.01
}
`

const jsCompressedFixedSchema = `package probe

fixed table Msg
{
    z float32 | min = -1, max = 1, resolution = 0.01
    nz float32 = 0.5 | min = -1, max = 1, resolution = 0.01
    tenth float32 | min = 0, max = 0.1, resolution = 0.01
}
`

// The message writer under another build: the same fields, uncompressed, so
// its batch can carry a raw -0 and the raw float32 0.1f.
const jsCompressedMessageWriterSchema = `package probe

fixed table Msg
{
    z float32
    tenth float32
}
`

// TestJsCompressedFloatNegZeroAndFloat32Ends pins two rules every form of
// the JavaScript leg must keep for a compressed float (SPEC-TABLES §3.3,
// SPEC §4.6, ruling fixed-compressed-negzero):
//
//   - its -0 IS +0: a read lands it as +0 uncounted (variable, JSON and
//     message reads), a write stores and prints +0 (variable save, JSON
//     write, fixed save), and a -0 under a +0 default is the default, so the
//     variable save and the message save elide it;
//   - its range is held at float32: the float32 0.1f is inside max = 0.1 on
//     the variable read, the variable write and the JSON read, where a
//     compare against the double 0.1 refused or clamped it.
//
// The pure half reads the emitted code; the gated half runs it under node.
func TestJsCompressedFloatNegZeroAndFloat32Ends(t *testing.T) {
	t.Parallel()
	vu := unitFrom(t, jsCompressedVariableSchema)
	vfiles, err := generateVariableFiles(vu, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(vu) + "Variable.js"
	var vall strings.Builder
	for _, data := range vfiles {
		vall.Write(data)
	}
	vsrc := vall.String()
	if !strings.Contains(vsrc, "hi: 0.10000000149011612") {
		t.Errorf("the variable descriptor of tenth must carry its float32 top 0.10000000149011612, not the double 0.1")
	}
	rt := string(vfiles[home])
	if !strings.Contains(runtimeFunction(t, rt, "TableVariableWriteField"), "TableVariableSameFloatField(fd, val, fd.def)") {
		t.Errorf("the variable save must compare a compressed float to its default by value")
	}
	if !strings.Contains(runtimeFunction(t, rt, "TableVariableScalar"), "else if (v === 0) { v = 0; }") {
		t.Errorf("the variable read must land a compressed -0 as +0")
	}
	if !strings.Contains(runtimeFunction(t, rt, "TableVariableWriteFloat"), "if (d.lo !== null && v === 0) { v = 0; }") {
		t.Errorf("the variable write must store a compressed -0 as +0")
	}

	fu := unitFrom(t, jsCompressedFixedSchema)
	ffiles, err := Generate(fu)
	if err != nil {
		t.Fatal(err)
	}
	fsrc := string(ffiles["ProbeTable.js"])
	save := jsFn(fsrc, "MsgSaveMessageBody")
	if strings.Contains(save, "Object.is(value.z") || strings.Contains(save, "Object.is(value.nz") {
		t.Errorf("the message save compares a compressed float's bits, so a -0 under a +0 default rides:\n%s", save)
	}
	if !strings.Contains(jsFn(fsrc, "MsgLoadMessageBody"), "else if (x === 0) { x = 0; }") {
		t.Errorf("the message read must land a compressed -0 as +0")
	}
	if !strings.Contains(fsrc, " === 0 ? 0 : ") {
		t.Errorf("the fixed save must store a compressed -0 as +0")
	}

	t.Run("node", func(t *testing.T) {
		slowtest.Gate(t, "node (it runs the generated reads and writes)")
		got := strings.TrimSpace(runJSConstructProbe(t, vfiles, home, jsCompressedVariableProbe, []int{0}))
		if got != "OK" {
			t.Errorf("variable and JSON forms:\n%s", got)
		}

		node := jsNode(t)
		dir := t.TempDir()
		base, err := js.Generate(fu)
		if err != nil {
			t.Fatal(err)
		}
		for _, set := range []map[string][]byte{base, ffiles} {
			for name, data := range set {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		writer := unitFrom(t, jsCompressedMessageWriterSchema)
		input := hex.EncodeToString(tablewire.Announce(writer)) + "\n" + strings.Join(jsCompressedMessageBatches(t, writer), "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, "cases.txt"), []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(jsCompressedFixedProbe), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, "probe.mjs", "cases.txt")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "OK" {
			t.Errorf("fixed and message forms (%v):\n%s", err, out)
		}
	})
}

// jsCompressedMessageBatches builds two batches of one message under the
// uncompressed writer: z carrying the raw float32 -0, and tenth carrying the
// raw float32 0.1f.
func jsCompressedMessageBatches(t *testing.T, writer *ir.Unit) []string {
	t.Helper()
	vocab := tablewire.MessageVocabulary(writer)
	refBits := ir.TableMessageRefBits(len(vocab))
	msg := writer.Tables["Msg"]
	one := func(name string, bits uint64) string {
		var f *ir.Field
		for _, x := range msg.Fields {
			if x.Name == name {
				f = x
			}
		}
		e := ir.TableFieldEntry(f)
		slot := uint64(0)
		for i, v := range vocab {
			if v.Key() == e.Key() {
				slot = uint64(i + 1)
			}
		}
		if slot == 0 {
			t.Fatalf("%s outside the writer's vocabulary", name)
		}
		vb := ir.TableMessageValueBits(e.Kind, e.Shape)
		if vb != 32 {
			t.Fatalf("%s rides %d bits, want a raw float32", name, vb)
		}
		w := &jsMessageBits{}
		w.put(slot, refBits)
		w.put(bits, 32)
		w.put(0, refBits)
		return name + " " + hex.EncodeToString(append([]byte{2, 0}, w.out...))
	}
	return []string{
		one("z", uint64(math.Float32bits(float32(math.Copysign(0, -1))))),
		one("tenth", uint64(math.Float32bits(float32(0.1)))),
	}
}

const jsCompressedVariableProbe = `import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[3]));
let failed = false;
function fail(why) { failed = true; console.log("FAILED: " + why); }
function same(a, b) { return a !== null && b !== null && a.length === b.length && a.every((x, i) => x === b[i]); }
function save(mut) { const v = new m.Probe(); mut(v); return m.ProbeSave(v); }
function text(mut) { const v = new m.Probe(); mut(v); return m.ProbeToJson(v); }
// patch rewrites the one float32 holding from to hold to
function patch(bytes, from, to) {
  const out = Uint8Array.from(bytes);
  const dv = new DataView(out.buffer);
  const fb = new DataView(new ArrayBuffer(4));
  fb.setFloat32(0, from, true);
  const want = fb.getUint32(0, true);
  let at = -1;
  for (let i = 0; i + 4 <= out.length; i++) {
    if (dv.getUint32(i, true) === want) { if (at >= 0) { return null; } at = i; }
  }
  if (at < 0) { return null; }
  dv.setFloat32(at, to, true);
  return out;
}
const tenth = Math.fround(0.1);

// THE VARIABLE READ: a -0 on the wire lands as +0, uncounted; 0.1f is inside max = 0.1
{
  const w = patch(save((v) => { v.z = 0.25; }), 0.25, -0);
  const r = new m.TableVariableReport();
  const x = w === null ? null : m.ProbeLoad(w, r);
  if (x === null || !Object.is(x.z, 0) || r.oor !== 0) { fail("variable read of -0: z=" + (x && Object.is(x.z, -0) ? "-0" : x && x.z) + " oor=" + r.oor); }
}
{
  const w = patch(save((v) => { v.tenth = 0.0625; }), 0.0625, tenth);
  const r = new m.TableVariableReport();
  const x = w === null ? null : m.ProbeLoad(w, r);
  if (x === null || x.tenth !== tenth || r.oor !== 0) { fail("variable read of 0.1f under max = 0.1: tenth=" + (x && x.tenth) + " oor=" + r.oor); }
}
// THE VARIABLE SAVE: -0 under a +0 default elides; under 0.5 it is stored as +0; 0.1f is written
if (!same(save((v) => { v.z = -0; }), save(() => {}))) { fail("variable save of -0 under a +0 default did not elide"); }
{
  const neg = save((v) => { v.nz = -0; });
  const pos = save((v) => { v.nz = 0; });
  if (!same(neg, pos) || same(pos, save(() => {}))) { fail("variable save of -0 under 0.5 is not the +0 save"); }
}
{
  const s = save((v) => { v.tenth = tenth; });
  const x = s === null ? null : m.ProbeLoad(s, new m.TableVariableReport());
  if (x === null || x.tenth !== tenth) { fail("variable save of 0.1f under max = 0.1 was refused"); }
}
// THE JSON READ: the full spelling of 0.1f is inside max = 0.1; -0 lands as +0
{
  const r = new m.TableVariableReport();
  const x = m.ProbeFromJson('{"tenth":0.10000000149011612}', r);
  if (x === null || x.tenth !== tenth || r.oor !== 0) { fail("JSON read of 0.1f under max = 0.1: tenth=" + (x && x.tenth) + " oor=" + r.oor); }
}
for (const [doc, key] of [['{"z":-0}', "z"], ['{"nz":-0.0}', "nz"]]) {
  const r = new m.TableVariableReport();
  const x = m.ProbeFromJson(doc, r);
  if (x === null || !Object.is(x[key], 0) || r.oor !== 0) { fail("JSON read of " + doc + " did not land +0"); }
}
// THE JSON WRITE: -0 is printed as +0
if (text((v) => { v.z = -0; }) !== text((v) => { v.z = 0; })) { fail("JSON write of z = -0: " + text((v) => { v.z = -0; })); }
if (text((v) => { v.nz = -0; }) !== text((v) => { v.nz = 0; })) { fail("JSON write of nz = -0: " + text((v) => { v.nz = -0; })); }
if (!failed) { console.log("OK"); }
`

const jsCompressedFixedProbe = `import { readFileSync } from "node:fs";
import { Msg, InitMsg, MsgFixedSave, MsgFixedMeasure, MsgMeasureMessages, MsgSaveMessages, MsgLoadMessages, TableVocabulary, AnnounceRead } from "./ProbeTable.js";
let failed = false;
function fail(why) { failed = true; console.log("FAILED: " + why); }
function same(a, b) { return a !== null && b !== null && a.length === b.length && a.every((x, i) => x === b[i]); }
function fresh(mut) { const v = new Msg(); InitMsg(v); mut(v); return v; }
function fixed(mut) {
  const n = MsgFixedMeasure(1);
  const b = new Uint8Array(n);
  return MsgFixedSave([fresh(mut)], 1, b) === n ? b : null;
}
function message(mut) {
  const v = fresh(mut);
  const n = MsgMeasureMessages([v], {});
  if (n < 0) { return null; }
  const b = new Uint8Array(n);
  return MsgSaveMessages([v], b, {}) === n ? b : null;
}
const tenth = Math.fround(0.1);
// THE FIXED SAVE: -0 is stored as +0
if (!same(fixed((v) => { v.z = -0; }), fixed((v) => { v.z = 0; }))) { fail("fixed save of -0 is not the +0 save"); }
if (fixed((v) => { v.tenth = 0.1; }) === null) { fail("fixed save of 0.1 under max = 0.1 was refused"); }
// THE MESSAGE SAVE: -0 under a +0 default elides; under 0.5 it is the +0 save
if (!same(message((v) => { v.z = -0; }), message(() => {}))) { fail("message save of -0 under a +0 default did not elide"); }
if (!same(message((v) => { v.nz = -0; }), message((v) => { v.nz = 0; }))) { fail("message save of -0 under 0.5 is not the +0 save"); }
// THE MESSAGE READ: a raw -0 lands as +0, uncounted; the raw 0.1f is inside max = 0.1
const lines = readFileSync(process.argv[2], "utf8").trim().split("\n");
const voc = new TableVocabulary();
voc.Init(new Array(16));
const arep = {};
if (!AnnounceRead(voc, Uint8Array.from(Buffer.from(lines[0], "hex")), arep)) {
  fail("the announcement was refused " + JSON.stringify(arep));
} else {
  for (const line of lines.slice(1)) {
    const [name, hexBatch] = line.split(" ");
    const out = [fresh(() => {})];
    const rep = {};
    const n = MsgLoadMessages(out, voc, Uint8Array.from(Buffer.from(hexBatch, "hex")), rep);
    const ok = n === 1 && !rep.Damaged && (rep.Oor | 0) === 0 && (rep.Mismatch | 0) === 0 && !rep.Reason;
    const held = name === "z" ? Object.is(out[0].z, 0) : out[0].tenth === tenth;
    if (!ok || !held) { fail("message read of " + name + ": n=" + n + " " + JSON.stringify(rep) + " z=" + (Object.is(out[0].z, -0) ? "-0" : out[0].z) + " tenth=" + out[0].tenth); }
  }
}
if (!failed) { console.log("OK"); }
`
