package main

import (
	"fmt"
	"strings"
)

func jsSlicesArray(slices []sliceSpec) string {
	var b strings.Builder
	b.WriteString("[")
	for i, s := range slices {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "[\"%s\", %d]", s.TypeName, s.Bytes)
	}
	b.WriteString("]")
	return b.String()
}

func pySlicesList(slices []sliceSpec) string {
	var b strings.Builder
	b.WriteString("[")
	for i, s := range slices {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%q, %d)", s.TypeName, s.Bytes)
	}
	b.WriteString("]")
	return b.String()
}

func luaSlicesTable(slices []sliceSpec) string {
	var b strings.Builder
	b.WriteString("{")
	for i, s := range slices {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "{ %q, %d }", s.TypeName, s.Bytes)
	}
	b.WriteString("}")
	return b.String()
}

func buildJSProbe(serializeJSPin string, mode string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import { readFileSync } from "node:fs";
import { WriteStream, ReadStream } from %q;
import * as Types from "./ex/Types.js";
import * as TypesFlat from "./ex/TypesFlat.js";
import * as Wire from "./ex/Wire.js";
import * as WireFlat from "./ex/WireFlat.js";
import * as Clauses from "./ex/Clauses.js";
import * as ClausesFlat from "./ex/ClausesFlat.js";
import * as Joins from "./ex/Joins.js";
import * as JoinsFlat from "./ex/JoinsFlat.js";
import * as Constants from "./ex/Constants.js";
import * as Ludicrous from "./lud/Ludicrous.js";
import * as LudicrousFlat from "./lud/LudicrousFlat.js";
import * as VH from "./vh/VerifyHostile.js";
import * as VHFlat from "./vh/VerifyHostileFlat.js";
`, serializeJSPin+"/src/index.js")
	if mode != "packet" {
		b.WriteString(`import * as GV from "./ptr/GraphdemoVariable.js";
import * as GView from "./ptr/GraphdemoView.js";
import * as GTab from "./ptr/GraphTable.js";
`)
	}
	b.WriteString(`
function check(ok, msg) {
  if (!ok) {
    console.error("FAILED: " + msg);
    process.exit(1);
  }
}
function loadBin(name) { return new Uint8Array(readFileSync("./inputs/" + name + ".bin")); }
function bytesEq(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
function roundTrip(mod, flatMod, typ, name) {
  const raw = loadBin(name);
  const v = new mod[typ]();
  check(mod["Read" + typ](new ReadStream(raw), v), "read " + name);
  const ws = new WriteStream(new Uint8Array(Math.max(256, raw.length + 64)));
  check(mod["Write" + typ](ws, v), "write " + name);
  ws.flush();
  check(bytesEq(ws.data(), raw), "byte-exact " + name);
  if (flatMod && flatMod["Read" + typ]) {
    const fv = new flatMod[typ]();
    check(flatMod["Read" + typ](new ReadStream(raw), fv), "flat read " + name);
    const fws = new WriteStream(new Uint8Array(Math.max(256, raw.length + 64)));
    check(flatMod["Write" + typ](fws, fv), "flat write " + name);
    fws.flush();
    check(bytesEq(fws.data(), raw), "flat byte-exact " + name);
  }
}
function roundTripSlices(mod, flatMod, name, slices) {
  const raw = loadBin(name);
  let off = 0;
  for (let i = 0; i < slices.length; i++) {
    const [typ, n] = slices[i];
    const sub = raw.subarray(off, off + n);
    const v = new mod[typ]();
    check(mod["Read" + typ](new ReadStream(sub), v), name + "#" + i + " read " + typ);
    const ws = new WriteStream(new Uint8Array(256));
    check(mod["Write" + typ](ws, v), name + "#" + i + " write " + typ);
    ws.flush();
    check(bytesEq(ws.data(), sub), name + "#" + i + " byte-exact " + typ);
    if (flatMod && flatMod["Read" + typ]) {
      const fv = new flatMod[typ]();
      check(flatMod["Read" + typ](new ReadStream(sub), fv), name + "#" + i + " flat read " + typ);
      const fws = new WriteStream(new Uint8Array(256));
      check(flatMod["Write" + typ](fws, fv), name + "#" + i + " flat write " + typ);
      fws.flush();
      check(bytesEq(fws.data(), sub), name + "#" + i + " flat byte-exact " + typ);
    }
    off += n;
  }
  check(off === raw.length, name + " total length");
}
function mustRefuseRead(mod, flatMod, typ, name) {
  const raw = loadBin(name);
  const v = new mod[typ]();
  check(!mod["Read" + typ](new ReadStream(raw), v), "must refuse read " + name);
  if (flatMod && flatMod["Read" + typ]) {
    const fv = new flatMod[typ]();
    check(!flatMod["Read" + typ](new ReadStream(raw), fv), "flat must refuse read " + name);
  }
}
function mustRefuseWrite(fn, what) {
  let accepted = false;
  try {
    const ws = new WriteStream(new Uint8Array(128));
    if (fn(ws) === true) accepted = true;
  } catch (_) {}
  check(!accepted, "must refuse write " + what);
}
`)
	if mode == "all" || mode == "packet" {
		b.WriteString("check(Constants.ProtocolId === 0x2bf51af687ac0abdn, 'examples ProtocolId');\n")
		b.WriteString("check(Ludicrous.ProtocolId === 0xd2ded2cc8a482aa1n, 'ludicrous ProtocolId');\n")
		b.WriteString("check(VH.ProtocolId === 0x962f976c95dcf12en, 'verifypkt ProtocolId');\n")
		for _, g := range canonicalSingleGoldens {
			fmt.Fprintf(&b, "roundTrip(%s, %sFlat, %q, %q);\n", g.Module, g.Module, g.Type, g.Name)
		}
		fmt.Fprintf(&b, "roundTripSlices(Clauses, ClausesFlat, 'clauses', %s);\n", jsSlicesArray(buildClausesSlices()))
		fmt.Fprintf(&b, "roundTripSlices(Joins, JoinsFlat, 'joins', %s);\n", jsSlicesArray(buildJoinsSlices()))
		for _, v := range []struct{ n, t string }{
			{"valid_const_reserved", "ConstReservedProbe"},
			{"valid_align", "AlignProbe"},
			{"valid_trail", "TrailProbe"},
			{"valid_range", "RangeProbe"},
			{"valid_text", "TextProbe"},
			{"valid_float_negzero", "FloatProbe"},
		} {
			fmt.Fprintf(&b, "roundTrip(VH, VHFlat, %q, %q);\n", v.t, v.n)
		}
		for _, h := range hostileReadCases {
			fmt.Fprintf(&b, "mustRefuseRead(VH, VHFlat, %q, %q);\n", h.Type, h.Name)
		}
		b.WriteString(`
// Hostile write-contract refusals: NaN/Inf, malformed UTF-8, embedded NUL, out-of-range int/int128/enum.
for (const bad of [NaN, Infinity, -Infinity]) {
  mustRefuseWrite((ws) => { const p = new VH.FloatProbe(); p.f32 = bad; return VH.WriteFloatProbe(ws, p); }, "f32 " + bad);
  mustRefuseWrite((ws) => { const p = new VH.FloatProbe(); p.f64 = bad; return VH.WriteFloatProbe(ws, p); }, "f64 " + bad);
  mustRefuseWrite((ws) => { const p = new VH.FloatProbe(); p.cf = bad; return VH.WriteFloatProbe(ws, p); }, "cf " + bad);
}
mustRefuseWrite((ws) => { const p = new VH.TextProbe(); p.label[0] = 0xff; p.label[1] = 0x61; p.labelLength = 2; return VH.WriteTextProbe(ws, p); }, "bad utf8");
mustRefuseWrite((ws) => { const p = new VH.TextProbe(); p.label[0] = 0x61; p.label[1] = 0x00; p.labelLength = 2; return VH.WriteTextProbe(ws, p); }, "nul utf8");
mustRefuseWrite((ws) => { const p = new VH.RangeProbe(); p.mode = 3; return VH.WriteRangeProbe(ws, p); }, "bad enum");
mustRefuseWrite((ws) => { const p = new VH.RangeProbe(); p.small = 9; return VH.WriteRangeProbe(ws, p); }, "small < min");
mustRefuseWrite((ws) => { const p = new VH.RangeProbe(); p.small = 21; return VH.WriteRangeProbe(ws, p); }, "small > max");
mustRefuseWrite((ws) => { const p = new VH.RangeProbe(); p.wide = 101n; return VH.WriteRangeProbe(ws, p); }, "wide > max");

// Stateful object & stream reuse:
// 1. RigidBody: moving -> at_rest -> refused -> moving on the SAME object & write buffer.
{
  const rb = new Types.RigidBody();
  const sharedBuf = new Uint8Array(256);
  const mov = loadBin("rigidbody_moving");
  const rest = loadBin("rigidbody_at_rest");
  const bad = loadBin("bad_rb_trunc");
  check(Types.ReadRigidBody(new ReadStream(mov), rb) && !rb.atRest, "reuse rb moving 1");
  let ws = new WriteStream(sharedBuf); check(Types.WriteRigidBody(ws, rb), "write rb 1"); ws.flush();
  check(bytesEq(ws.data(), mov), "reuse rb moving 1 bytes");
  check(Types.ReadRigidBody(new ReadStream(rest), rb) && rb.atRest, "reuse rb rest");
  check(rb.linearVelocity.x === 0 && rb.linearVelocity.y === 0 && rb.linearVelocity.z === 0, "reuse rb zeroed linvel");
  check(rb.angularVelocity.x === 0 && rb.angularVelocity.y === 0 && rb.angularVelocity.z === 0, "reuse rb zeroed angvel");
  ws = new WriteStream(sharedBuf); check(Types.WriteRigidBody(ws, rb), "write rb rest"); ws.flush();
  check(bytesEq(ws.data(), rest), "reuse rb rest bytes");
  check(!Types.ReadRigidBody(new ReadStream(bad), rb), "reuse rb trunc refused");
  check(Types.ReadRigidBody(new ReadStream(mov), rb) && !rb.atRest, "reuse rb moving 2");
  ws = new WriteStream(sharedBuf); check(Types.WriteRigidBody(ws, rb), "write rb 2"); ws.flush();
  check(bytesEq(ws.data(), mov), "reuse rb moving 2 bytes");
}
// 2. LudicrousState: targeted -> untargeted -> targeted on SAME object (128-bit branch zeroing).
{
  const ls = new Ludicrous.LudicrousState();
  const tWire = loadBin("ludicrous_state");
  const uWire = loadBin("ludicrous_state_untargeted");
  check(Ludicrous.ReadLudicrousState(new ReadStream(tWire), ls) && ls.hasTarget && ls.targetId === 42n, "reuse lud targeted");
  check(Ludicrous.ReadLudicrousState(new ReadStream(uWire), ls) && !ls.hasTarget && ls.targetId === 0n, "reuse lud untargeted zeroed");
  const ws = new WriteStream(new Uint8Array(256)); check(Ludicrous.WriteLudicrousState(ws, ls), "write lud untargeted"); ws.flush();
  check(bytesEq(ws.data(), uWire), "reuse lud untargeted bytes");
}
// 3. ReuseEnvelope: 4 branch/union shapes + corrupt read on SAME envelope.
{
  const env = new VH.ReuseEnvelope();
  for (const [k, expAct, expIn, expA, expB, expC] of [
    ["reuse_env_1", true, true, 0xdeadbeef, 0, 0],
    ["reuse_env_2", true, false, 0, 0x4321, 0],
    ["reuse_env_3", false, false, 0, 0, 0xcafebabe],
    ["reuse_env_4", true, true, 7, 0, 0],
  ]) {
    const raw = loadBin(k);
    check(VH.ReadReuseEnvelope(new ReadStream(raw), env), "read " + k);
    check(env.active === expAct && env.inner === expIn && env.a === expA && env.b === expB && env.c === expC, "state " + k);
    const ws = new WriteStream(new Uint8Array(128)); check(VH.WriteReuseEnvelope(ws, env), "write " + k); ws.flush();
    check(bytesEq(ws.data(), raw), "bytes " + k);
    check(!VH.ReadReuseEnvelope(new ReadStream(loadBin("bad_reuse_trunc")), env), "refuse trunc after " + k);
  }
}
`)
	}
	if mode == "all" || mode == "surfaces" {
		b.WriteString(`
// Surfaces: §16.7 &node shared DAG + cycle/forward/dup refusals + Descriptors + UnitView.
{
  const newRep = () => new GV.TableVariableReport();
  const dagJson = '{"name":"dag","version":3,"head":{"&node":1,"value":10,"name":"n1","next":{"&node":2,"value":20,"name":"n2","next":null}},"alias":{"&node":2},"ground":{"depth":4,"head":{"&node":1}}}';
  const rep = newRep();
  const s1 = GV.SceneFromJson(dagJson, rep);
  check(s1 !== null && !rep.refused, "js dag FromJson");
  check(s1.head === s1.ground.head && s1.head.next === s1.alias, "js dag pointer identity");
  const j1 = GV.SceneToJson(s1);
  check(typeof j1 === "string" && j1.includes('"&node": 1') && j1.includes('"&node": 2'), "js dag ToJson &node");
  const w1 = GV.SceneSave(s1);
  check(w1 instanceof Uint8Array && w1.length > 0, "js dag Save");
  const s2 = GV.SceneLoad(w1, newRep());
  check(s2 !== null && s2.head === s2.ground.head && s2.head.next === s2.alias, "js dag wire Load identity");
  check(GV.SceneToJson(s2) === j1, "js dag wire->json match");

  for (const [label, badJson] of [
    ["self-cycle", '{"head":{"&node":1,"value":1,"next":{"&node":1}}}'],
    ["forward-ref", '{"head":{"&node":99}}'],
    ["dup-def", '{"head":{"&node":1,"value":1,"next":null},"alias":{"&node":1,"value":2,"next":null}}'],
  ]) {
    const br = newRep();
    const got = GV.SceneFromJson(badJson, br);
    check(got === null || br.refused, "js must refuse &node " + label);
  }

  const uv = GView.UnitView();
  check(uv.package === "graphdemo" && uv.tables.length === 11 && uv.types.length === 1 && uv.enums.length === 1 && uv.constants.length === 1, "js UnitView counts");
  const metaView = uv.tables.find((t) => t.name === "Meta");
  check(GV.SceneTableType.name === "Scene" && GV.SceneTableType.fields.length === 9, "js Scene descriptor");
  check(metaView && metaView.type.fields.length === 2, "js Meta fixed descriptor");
}
`)
	}
	b.WriteString("console.log('VERIFY_VM_OK');\n")
	return b.String()
}

func buildPythonProbe(mode string) string {
	var b strings.Builder
	b.WriteString(`import math, sys
from pathlib import Path
sys.path[:0] = ["./ex", "./lud", "./vh", "./ptr"]

import example
from example import Types, Wire, Clauses, Joins
from example._schema import InvalidValue as ExInvalid, Refused as ExRefused, wire_input as ex_wire_input
from example._schema.serialize import ReadStream as ExRS, WriteStream as ExWS
import ludicrous
from ludicrous import Ludicrous
from ludicrous._schema.serialize import ReadStream as LudRS
from ludicrous._schema import wire_input as lud_wire_input
import verifypkt
from verifypkt import VerifyHostile as VH
from verifypkt._schema import InvalidValue as VHInvalid, Refused as VHRefused, wire_input as vh_wire_input
from verifypkt._schema.serialize import ReadStream as VHRS, WriteStream as VHWS

def check(ok: bool, msg: str) -> None:
    if not ok:
        sys.stderr.write("FAILED: " + msg + "\n")
        sys.exit(1)

def load_bin(name: str) -> bytes:
    return Path("./inputs/" + name + ".bin").read_bytes()

def round_trip(mod, typ: str, name: str) -> None:
    raw = load_bin(name)
    cls = getattr(mod, typ)
    v = cls.read(raw)
    out = v.write()
    check(out == raw, f"byte-exact {name}: got {out.hex()} want {raw.hex()}")

def round_trip_slices(mod, name: str, slices: list[tuple[str, int]]) -> None:
    raw = load_bin(name)
    off = 0
    for idx, (typ, n) in enumerate(slices):
        sub = raw[off:off + n]
        cls = getattr(mod, typ)
        v = cls.read(sub)
        out = v.write()
        check(out == sub, f"{name}#{idx} {typ}")
        off += n
    check(off == len(raw), f"{name} total len")

def must_refuse_read(typ: str, name: str) -> None:
    raw = load_bin(name)
    cls = getattr(VH, typ)
    try:
        cls.read(raw)
    except Exception:
        return
    check(False, f"must refuse read {name}")

def must_refuse_write(fn, what: str) -> None:
    try:
        fn()
    except Exception:
        return
    check(False, f"must refuse write {what}")
`)
	if mode == "all" || mode == "packet" {
		b.WriteString("check(example.PROTOCOL_ID == 0x2bf51af687ac0abd, 'examples PROTOCOL_ID')\n")
		b.WriteString("check(ludicrous.PROTOCOL_ID == 0xd2ded2cc8a482aa1, 'ludicrous PROTOCOL_ID')\n")
		b.WriteString("check(verifypkt.PROTOCOL_ID == 0x962f976c95dcf12e, 'verifypkt PROTOCOL_ID')\n")
		for _, g := range canonicalSingleGoldens {
			fmt.Fprintf(&b, "round_trip(%s, %q, %q)\n", g.Module, g.Type, g.Name)
		}
		fmt.Fprintf(&b, "round_trip_slices(Clauses, 'clauses', %s)\n", pySlicesList(buildClausesSlices()))
		fmt.Fprintf(&b, "round_trip_slices(Joins, 'joins', %s)\n", pySlicesList(buildJoinsSlices()))
		for _, v := range []struct{ n, t string }{
			{"valid_const_reserved", "ConstReservedProbe"},
			{"valid_align", "AlignProbe"},
			{"valid_trail", "TrailProbe"},
			{"valid_range", "RangeProbe"},
			{"valid_text", "TextProbe"},
			{"valid_float_negzero", "FloatProbe"},
		} {
			fmt.Fprintf(&b, "round_trip(VH, %q, %q)\n", v.t, v.n)
		}
		for _, h := range hostileReadCases {
			fmt.Fprintf(&b, "must_refuse_read(%q, %q)\n", h.Type, h.Name)
		}
		b.WriteString(`
for bad in [math.nan, math.inf, -math.inf]:
    def _w32(x=bad): p = VH.FloatProbe(); p.f32 = x; p.write()
    def _w64(x=bad): p = VH.FloatProbe(); p.f64 = x; p.write()
    def _wcf(x=bad): p = VH.FloatProbe(); p.cf = x; p.write()
    must_refuse_write(_w32, f"f32 {bad}")
    must_refuse_write(_w64, f"f64 {bad}")
    must_refuse_write(_wcf, f"cf {bad}")

def _w_surr(): p = VH.TextProbe(); p.label = "\ud800"; p.write()
def _w_nul(): p = VH.TextProbe(); p.label = "a\x00b"; p.write()
def _w_len(): p = VH.TextProbe(); p.label = "a" * 17; p.write()
def _w_enum(): p = VH.RangeProbe(); p.mode = 3; p.write()
def _w_lo(): p = VH.RangeProbe(); p.small = 9; p.write()
def _w_hi(): p = VH.RangeProbe(); p.small = 21; p.write()
def _w_w128(): p = VH.RangeProbe(); p.wide = 101; p.write()
for fn, w in [(_w_surr, "surrogate"), (_w_nul, "nul"), (_w_len, "len>16"), (_w_enum, "enum"), (_w_lo, "small<min"), (_w_hi, "small>max"), (_w_w128, "wide>max")]:
    must_refuse_write(fn, w)

# Stateful object reuse on SAME Python instance via _s_read + serialize_end:
rb = Types.RigidBody()
mov, rest, bad_rb = load_bin("rigidbody_moving"), load_bin("rigidbody_at_rest"), load_bin("bad_rb_trunc")
s = ExRS(ex_wire_input(mov))
check(rb._s_read(s) and s.serialize_end() and not rb.at_rest and rb.write() == mov, "py reuse rb mov 1")
s = ExRS(ex_wire_input(rest))
check(rb._s_read(s) and s.serialize_end() and rb.at_rest, "py reuse rb rest")
check(rb.linear_velocity.x == 0.0 and rb.angular_velocity.y == 0.0 and rb.write() == rest, "py reuse rb rest zeroed")
s = ExRS(ex_wire_input(bad_rb))
check(not (rb._s_read(s) and s.serialize_end()), "py reuse rb trunc refused")
s = ExRS(ex_wire_input(mov))
check(rb._s_read(s) and s.serialize_end() and rb.write() == mov, "py reuse rb mov 2")

ls = Ludicrous.LudicrousState()
tw, uw = load_bin("ludicrous_state"), load_bin("ludicrous_state_untargeted")
s = LudRS(lud_wire_input(tw))
check(ls._s_read(s) and s.serialize_end() and ls.has_target and ls.target_id == 42, "py reuse lud targeted")
s = LudRS(lud_wire_input(uw))
check(ls._s_read(s) and s.serialize_end() and not ls.has_target and ls.target_id == 0 and ls.write() == uw, "py reuse lud untargeted")

env = VH.ReuseEnvelope()
for k, ea, ei, va, vb, vc in [
    ("reuse_env_1", True, True, 0xdeadbeef, 0, 0),
    ("reuse_env_2", True, False, 0, 0x4321, 0),
    ("reuse_env_3", False, False, 0, 0, 0xcafebabe),
    ("reuse_env_4", True, True, 7, 0, 0),
]:
    raw = load_bin(k)
    s = VHRS(vh_wire_input(raw))
    check(env._s_read(s) and s.serialize_end(), f"py read {k}")
    check((env.active, env.inner, env.a, env.b, env.c) == (ea, ei, va, vb, vc) and env.write() == raw, f"py state {k}")
    sb = VHRS(vh_wire_input(load_bin("bad_reuse_trunc")))
    check(not (env._s_read(sb) and sb.serialize_end()), f"py trunc {k}")
`)
	}
	if mode == "all" || mode == "surfaces" {
		b.WriteString(`
import graphdemo
from graphdemo import Scene, Meta, LoadReport
from graphdemo._schema.descriptor import by_name

dag_json = '{"name":"dag","version":3,"head":{"&node":1,"value":10,"name":"n1","next":{"&node":2,"value":20,"name":"n2","next":null}},"alias":{"&node":2},"ground":{"depth":4,"head":{"&node":1}}}'
s1 = Scene.from_json(dag_json)
check(s1.head is s1.ground.head and s1.head.next is s1.alias, "py dag pointer identity")
j1 = s1.to_json()
check('"&node": 1' in j1 and '"&node": 2' in j1, "py dag ToJson &node")
w1 = s1.save()
s2 = Scene.load(w1)
check(s2.head is s2.ground.head and s2.head.next is s2.alias and s2.to_json() == j1, "py dag wire roundtrip")

for label, bad_json in [
    ("self-cycle", '{"head":{"&node":1,"value":1,"next":{"&node":1}}}'),
    ("forward-ref", '{"head":{"&node":99}}'),
    ("dup-def", '{"head":{"&node":1,"value":1,"next":null},"alias":{"&node":1,"value":2,"next":null}}'),
]:
    refused = False
    try:
        Scene.from_json(bad_json)
    except Exception:
        refused = True
    check(refused, f"py must refuse &node {label}")

if hasattr(Scene, "from_text_form"):
    sexp = b'; comment\n(:name "sexp" :version 7 :head ("&node" 5 :value 42 :name "shared" :next null) :alias ("&node" 5))\n'
    doc = Scene.from_text_form(sexp)
    check(doc.value.version == 7 and doc.value.head is doc.value.alias and doc.value.head.value == 42, "py sexp &node")
    check(doc.source == sexp, "py sexp source preserved")
    bad_sexp_refused = False
    try:
        Scene.from_text_form(b'(:head ("&node" 99))')
    except Exception:
        bad_sexp_refused = True
    check(bad_sexp_refused, "py sexp forward ref refused")

uv = graphdemo.UnitView()
check(uv.package == "graphdemo" and len(uv.tables) == 11 and len(uv.types) == 1 and len(uv.enums) == 1 and len(uv.constants) == 1, "py UnitView")
sd = next((t for t in uv.tables if t.name == "Scene"), None)
md = next((t for t in uv.tables if t.name == "Meta"), None)
check(sd is not None and len(sd.type.fields) == 9 and md is not None and len(md.type.fields) == 2, "py descriptors")
`)
	}
	b.WriteString("print('VERIFY_VM_OK')\n")
	return b.String()
}

func buildRubyProbe(mode string) string {
	var b strings.Builder
	b.WriteString(`# frozen_string_literal: true
require_relative 'ex/example'
require_relative 'lud/ludicrous'
require_relative 'vh/verifypkt'
`)
	if mode != "packet" {
		b.WriteString("require_relative 'ptr/graphdemo'\n")
	}
	b.WriteString(`
def check(ok, msg)
  unless ok
    warn("FAILED: #{msg}")
    exit(1)
  end
end

def load_bin(name)
  File.binread("./inputs/#{name}.bin")
end

def round_trip(mod, typ, name)
  raw = load_bin(name)
  cls = mod.const_get(typ)
  v = cls.read(raw)
  out = v.write
  check(out == raw, "byte-exact #{name}")
end

def round_trip_slices(mod, name, slices)
  raw = load_bin(name)
  off = 0
  slices.each_with_index do |(typ, n), idx|
    sub = raw.byteslice(off, n)
    cls = mod.const_get(typ)
    v = cls.read(sub)
    out = v.write
    check(out == sub, "#{name}##{idx} #{typ}")
    off += n
  end
  check(off == raw.bytesize, "#{name} total len")
end

def must_refuse_read(typ, name)
  raw = load_bin(name)
  cls = Verifypkt.const_get(typ)
  begin
    cls.read(raw)
  rescue StandardError
    return
  end
  check(false, "must refuse read #{name}")
end

def must_refuse_write(what)
  begin
    yield
  rescue StandardError
    return
  end
  check(false, "must refuse write #{what}")
end
`)
	if mode == "all" || mode == "packet" {
		b.WriteString("check(Example::PROTOCOL_ID == 0x2bf51af687ac0abd, 'examples PROTOCOL_ID')\n")
		b.WriteString("check(Ludicrous::PROTOCOL_ID == 0xd2ded2cc8a482aa1, 'ludicrous PROTOCOL_ID')\n")
		b.WriteString("check(Verifypkt::PROTOCOL_ID == 0x962f976c95dcf12e, 'verifypkt PROTOCOL_ID')\n")
		for _, g := range canonicalSingleGoldens {
			mod := "Example"
			if g.Module == "Ludicrous" {
				mod = "Ludicrous"
			}
			fmt.Fprintf(&b, "round_trip(%s, %q, %q)\n", mod, g.Type, g.Name)
		}
		fmt.Fprintf(&b, "round_trip_slices(Example, 'clauses', %s)\n", jsSlicesArray(buildClausesSlices()))
		fmt.Fprintf(&b, "round_trip_slices(Example, 'joins', %s)\n", jsSlicesArray(buildJoinsSlices()))
		for _, v := range []struct{ n, t string }{
			{"valid_const_reserved", "ConstReservedProbe"},
			{"valid_align", "AlignProbe"},
			{"valid_trail", "TrailProbe"},
			{"valid_range", "RangeProbe"},
			{"valid_text", "TextProbe"},
			{"valid_float_negzero", "FloatProbe"},
		} {
			fmt.Fprintf(&b, "round_trip(Verifypkt, %q, %q)\n", v.t, v.n)
		}
		for _, h := range hostileReadCases {
			fmt.Fprintf(&b, "must_refuse_read(%q, %q)\n", h.Type, h.Name)
		}
		b.WriteString(`
[Float::NAN, Float::INFINITY, -Float::INFINITY].each do |bad|
  must_refuse_write("f32 #{bad}") { p = Verifypkt::FloatProbe.new; p.f32 = bad; p.write }
  must_refuse_write("f64 #{bad}") { p = Verifypkt::FloatProbe.new; p.f64 = bad; p.write }
  must_refuse_write("cf #{bad}") { p = Verifypkt::FloatProbe.new; p.cf = bad; p.write }
end
must_refuse_write("bad utf8") { p = Verifypkt::TextProbe.new; p.label = "\xFFa".b; p.write }
must_refuse_write("nul utf8") { p = Verifypkt::TextProbe.new; p.label = "a\x00b"; p.write }
must_refuse_write("len>16") { p = Verifypkt::TextProbe.new; p.label = "a" * 17; p.write }
must_refuse_write("bad enum") { p = Verifypkt::RangeProbe.new; p.mode = 3; p.write }
must_refuse_write("small<min") { p = Verifypkt::RangeProbe.new; p.small = 9; p.write }
must_refuse_write("small>max") { p = Verifypkt::RangeProbe.new; p.small = 21; p.write }
must_refuse_write("wide>max") { p = Verifypkt::RangeProbe.new; p.wide = 101; p.write }

# Stateful reuse on SAME Ruby object via _s_read + serialize_end:
rb = Example::RigidBody.new
mov = load_bin('rigidbody_moving')
rest = load_bin('rigidbody_at_rest')
bad_rb = load_bin('bad_rb_trunc')
s, = Example._s_open_read_stream(mov, 0)
check(rb._s_read(s) && s.serialize_end && !rb.at_rest && rb.write == mov, 'rb reuse mov 1')
s, = Example._s_open_read_stream(rest, 0)
check(rb._s_read(s) && s.serialize_end && rb.at_rest && rb.linear_velocity.x == 0.0 && rb.angular_velocity.y == 0.0 && rb.write == rest, 'rb reuse rest zeroed')
s, = Example._s_open_read_stream(bad_rb, 0)
check(!(rb._s_read(s) && s.serialize_end), 'rb reuse trunc refused')
s, = Example._s_open_read_stream(mov, 0)
check(rb._s_read(s) && s.serialize_end && rb.write == mov, 'rb reuse mov 2')

ls = Ludicrous::LudicrousState.new
tw = load_bin('ludicrous_state')
uw = load_bin('ludicrous_state_untargeted')
s, = Ludicrous._s_open_read_stream(tw, 0)
check(ls._s_read(s) && s.serialize_end && ls.has_target && ls.target_id == 42, 'rb reuse lud targeted')
s, = Ludicrous._s_open_read_stream(uw, 0)
check(ls._s_read(s) && s.serialize_end && !ls.has_target && ls.target_id == 0 && ls.write == uw, 'rb reuse lud untargeted')

env = Verifypkt::ReuseEnvelope.new
[
  ['reuse_env_1', true, true, 0xdeadbeef, 0, 0],
  ['reuse_env_2', true, false, 0, 0x4321, 0],
  ['reuse_env_3', false, false, 0, 0, 0xcafebabe],
  ['reuse_env_4', true, true, 7, 0, 0]
].each do |k, ea, ei, va, vb, vc|
  raw = load_bin(k)
  s, = Verifypkt._s_open_read_stream(raw, 0)
  check(env._s_read(s) && s.serialize_end, "rb read #{k}")
  check([env.active, env.inner, env.a, env.b, env.c] == [ea, ei, va, vb, vc] && env.write == raw, "rb state #{k}")
  sb, = Verifypkt._s_open_read_stream(load_bin('bad_reuse_trunc'), 0)
  check(!(env._s_read(sb) && sb.serialize_end), "rb trunc #{k}")
end
`)
	}
	if mode == "all" || mode == "surfaces" {
		b.WriteString(`
dag_json = '{"name":"dag","version":3,"head":{"&node":1,"value":10,"name":"n1","next":{"&node":2,"value":20,"name":"n2","next":null}},"alias":{"&node":2},"ground":{"depth":4,"head":{"&node":1}}}'
s1 = Graphdemo::Scene.from_json(dag_json)
check(s1.head.equal?(s1.ground.head) && s1.head.next.equal?(s1.alias), 'ruby dag pointer identity')
j1 = s1.to_json
check(j1.include?('"&node": 1') && j1.include?('"&node": 2'), 'ruby dag ToJson &node')
w1 = s1.save
s2 = Graphdemo::Scene.load(w1)
check(s2.head.equal?(s2.ground.head) && s2.head.next.equal?(s2.alias) && s2.to_json == j1, 'ruby dag wire roundtrip')

[
  ['self-cycle', '{"head":{"&node":1,"value":1,"next":{"&node":1}}}'],
  ['forward-ref', '{"head":{"&node":99}}'],
  ['dup-def', '{"head":{"&node":1,"value":1,"next":null},"alias":{"&node":1,"value":2,"next":null}}']
].each do |label, bad_json|
  refused = false
  begin
    Graphdemo::Scene.from_json(bad_json)
  rescue StandardError
    refused = true
  end
  check(refused, "ruby must refuse &node #{label}")
end

sexp = "; comment\n(:name \"sexp\" :version 7 :head (\"&node\" 5 :value 42 :name \"shared\" :next null) :alias (\"&node\" 5))\n".b
doc = Graphdemo::Scene.from_text_form(sexp)
check(doc.value.version == 7 && doc.value.head.equal?(doc.value.alias) && doc.value.head.value == 42, 'ruby sexp &node')
check(doc.source == sexp, 'ruby sexp bytes preserved')
bad_sexp = false
begin
  Graphdemo::Scene.from_text_form('(:head ("&node" 99))'.b)
rescue StandardError
  bad_sexp = true
end
check(bad_sexp, 'ruby sexp forward ref refused')

uv = Graphdemo.unit_view
check(uv.package == 'graphdemo' && uv.tables.length == 11 && uv.types.length == 1 && uv.enums.length == 1 && uv.constants.length == 1, 'ruby UnitView')
sd = uv.tables.find { |t| t.name == 'Scene' }
md = uv.tables.find { |t| t.name == 'Meta' }
check(!sd.nil? && sd.type.fields.length == 9 && !md.nil? && md.type.fields.length == 2, 'ruby descriptors')
`)
	}
	b.WriteString("puts 'VERIFY_VM_OK'\n")
	return b.String()
}

func buildLuaProbe(mode string) string {
	var b strings.Builder
	b.WriteString(`local function check(ok, msg)
  if not ok then
    io.stderr:write("FAILED: " .. tostring(msg) .. "\n")
    os.exit(1)
  end
end

local function load_bin(name)
  local f = assert(io.open("./inputs/" .. name .. ".bin", "rb"))
  local d = f:read("*a")
  f:close()
  return d
end

local function with_dir(dir, fn)
  local saved_path = package.path
  local saved_loaded = {}
  for k, v in pairs(package.loaded) do saved_loaded[k] = v end
  for k in pairs(package.loaded) do
    if k ~= "string" and k ~= "table" and k ~= "math" and k ~= "io" and k ~= "os" and k ~= "package" and k ~= "utf8" and k ~= "coroutine" and k ~= "debug" then
      package.loaded[k] = nil
    end
  end
  package.path = dir .. "/?.lua;" .. saved_path
  fn()
  package.path = saved_path
  for k in pairs(package.loaded) do package.loaded[k] = nil end
  for k, v in pairs(saved_loaded) do package.loaded[k] = v end
end
`)
	if mode == "all" || mode == "packet" {
		b.WriteString(`
with_dir("./ex", function()
  local serialize = require("serialize")
  local Types = require("Types")
  local Wire = require("Wire")
  local Clauses = require("Clauses")
  local Joins = require("Joins")
  local Constants = require("Constants")
  check(Constants.ProtocolId == 0x2bf51af687ac0abd, "lua examples ProtocolId")
  local function rt(mod, typ, name)
    local raw = load_bin(name)
    local v = mod["New" .. typ]()
    local rs = serialize.ReadStream.new(raw)
    check(mod["Read" .. typ](rs, v), "lua read " .. name)
    local ws = serialize.WriteStream.new(math.max(256, #raw + 64))
    check(mod["Write" .. typ](ws, v), "lua write " .. name)
    ws:flush()
    check(ws:data() == raw, "lua byte-exact " .. name)
  end
  local function rt_slices(mod, name, slices)
    local raw = load_bin(name)
    local off = 1
    for i, item in ipairs(slices) do
      local typ, n = item[1], item[2]
      local sub = raw:sub(off, off + n - 1)
      local v = mod["New" .. typ]()
      local rs = serialize.ReadStream.new(sub)
      check(mod["Read" .. typ](rs, v), name .. "#" .. i .. " read " .. typ)
      local ws = serialize.WriteStream.new(256)
      check(mod["Write" .. typ](ws, v), name .. "#" .. i .. " write " .. typ)
      ws:flush()
      check(ws:data() == sub, name .. "#" .. i .. " byte-exact " .. typ)
      off = off + n
    end
    check(off - 1 == #raw, name .. " total len")
  end
`)
		for _, g := range canonicalSingleGoldens {
			if g.Module != "Ludicrous" {
				fmt.Fprintf(&b, "  rt(%s, %q, %q)\n", g.Module, g.Type, g.Name)
			}
		}
		fmt.Fprintf(&b, "  rt_slices(Clauses, 'clauses', %s)\n", luaSlicesTable(buildClausesSlices()))
		fmt.Fprintf(&b, "  rt_slices(Joins, 'joins', %s)\n", luaSlicesTable(buildJoinsSlices()))
		b.WriteString(`
  -- Stateful RigidBody reuse
  local rb = Types.NewRigidBody()
  local mov, rest, bad_rb = load_bin("rigidbody_moving"), load_bin("rigidbody_at_rest"), load_bin("bad_rb_trunc")
  check(Types.ReadRigidBody(serialize.ReadStream.new(mov), rb) and not rb.AtRest, "lua reuse rb mov 1")
  local ws = serialize.WriteStream.new(256); check(Types.WriteRigidBody(ws, rb), "lua write rb 1"); ws:flush(); check(ws:data() == mov, "lua rb mov 1 bytes")
  check(Types.ReadRigidBody(serialize.ReadStream.new(rest), rb) and rb.AtRest, "lua reuse rb rest")
  check(rb.LinearVelocity.X == 0.0 and rb.AngularVelocity.Y == 0.0, "lua reuse rb zeroed")
  ws = serialize.WriteStream.new(256); check(Types.WriteRigidBody(ws, rb), "lua write rb rest"); ws:flush(); check(ws:data() == rest, "lua rb rest bytes")
  check(not Types.ReadRigidBody(serialize.ReadStream.new(bad_rb), rb), "lua reuse rb trunc refused")
  check(Types.ReadRigidBody(serialize.ReadStream.new(mov), rb) and not rb.AtRest, "lua reuse rb mov 2")
  ws = serialize.WriteStream.new(256); check(Types.WriteRigidBody(ws, rb), "lua write rb 2"); ws:flush(); check(ws:data() == mov, "lua rb mov 2 bytes")
end)

with_dir("./lud", function()
  local serialize = require("serialize")
  local Ludicrous = require("Ludicrous")
  check(Ludicrous.ProtocolId == 0xd2ded2cc8a482aa1, "lua ludicrous ProtocolId")
  for _, item in ipairs({
    { "LudicrousState", "ludicrous_state" },
    { "LudicrousState", "ludicrous_state_untargeted" },
    { "DegenerateProbe", "degenerate_probe" },
    { "UnsignedProbe", "unsigned_probe" },
  }) do
    local raw = load_bin(item[2])
    local v = Ludicrous["New" .. item[1]]()
    check(Ludicrous["Read" .. item[1]](serialize.ReadStream.new(raw), v), "lua read " .. item[2])
    local ws = serialize.WriteStream.new(512)
    check(Ludicrous["Write" .. item[1]](ws, v), "lua write " .. item[2])
    ws:flush()
    check(ws:data() == raw, "lua byte-exact " .. item[2])
  end
  local ls = Ludicrous.NewLudicrousState()
  local tw, uw = load_bin("ludicrous_state"), load_bin("ludicrous_state_untargeted")
  check(Ludicrous.ReadLudicrousState(serialize.ReadStream.new(tw), ls) and ls.HasTarget and ls.TargetId.lo == 42, "lua reuse lud targeted")
  check(Ludicrous.ReadLudicrousState(serialize.ReadStream.new(uw), ls) and not ls.HasTarget and ls.TargetId.lo == 0 and ls.TargetId.hi == 0, "lua reuse lud untargeted")
  local ws = serialize.WriteStream.new(512); check(Ludicrous.WriteLudicrousState(ws, ls), "lua write lud untargeted"); ws:flush(); check(ws:data() == uw, "lua reuse lud bytes")
end)

with_dir("./vh", function()
  local serialize = require("serialize")
  local VH = require("VerifyHostile")
  check(VH.ProtocolId == 0x962f976c95dcf12e, "lua verifypkt ProtocolId")
  for _, item in ipairs({
    { "ConstReservedProbe", "valid_const_reserved" },
    { "AlignProbe", "valid_align" },
    { "TrailProbe", "valid_trail" },
    { "RangeProbe", "valid_range" },
    { "TextProbe", "valid_text" },
    { "FloatProbe", "valid_float_negzero" },
  }) do
    local raw = load_bin(item[2])
    local v = VH["New" .. item[1]]()
    check(VH["Read" .. item[1]](serialize.ReadStream.new(raw), v), "lua read " .. item[2])
    local ws = serialize.WriteStream.new(256)
    check(VH["Write" .. item[1]](ws, v), "lua write " .. item[2])
    ws:flush()
    check(ws:data() == raw, "lua byte-exact " .. item[2])
  end
`)
		for _, h := range hostileReadCases {
			fmt.Fprintf(&b, "  check(not VH.Read%s(serialize.ReadStream.new(load_bin(%q)), VH.New%s()), 'lua must refuse %s')\n", h.Type, h.Name, h.Type, h.Name)
		}
		b.WriteString(`
  local function must_refuse_write(fn, what)
    local ok, res = pcall(function()
      local ws = serialize.WriteStream.new(128)
      return fn(ws)
    end)
    check(not ok or res == false, "lua must refuse write " .. what)
  end
  for _, bad in ipairs({ 0/0, math.huge, -math.huge }) do
    must_refuse_write(function(ws) local p = VH.NewFloatProbe(); p.F32 = bad; return VH.WriteFloatProbe(ws, p) end, "f32")
    must_refuse_write(function(ws) local p = VH.NewFloatProbe(); p.F64 = bad; return VH.WriteFloatProbe(ws, p) end, "f64")
    must_refuse_write(function(ws) local p = VH.NewFloatProbe(); p.Cf = bad; return VH.WriteFloatProbe(ws, p) end, "cf")
  end
  must_refuse_write(function(ws) local p = VH.NewTextProbe(); p.Label = "\xffa"; return VH.WriteTextProbe(ws, p) end, "bad utf8")
  must_refuse_write(function(ws) local p = VH.NewTextProbe(); p.Label = "a\0b"; return VH.WriteTextProbe(ws, p) end, "nul utf8")
  must_refuse_write(function(ws) local p = VH.NewTextProbe(); p.Label = string.rep("a", 17); return VH.WriteTextProbe(ws, p) end, "len>16")
  must_refuse_write(function(ws) local p = VH.NewRangeProbe(); p.Mode = 3; return VH.WriteRangeProbe(ws, p) end, "bad enum")
  must_refuse_write(function(ws) local p = VH.NewRangeProbe(); p.Small = 9; return VH.WriteRangeProbe(ws, p) end, "small<min")
  must_refuse_write(function(ws) local p = VH.NewRangeProbe(); p.Small = 21; return VH.WriteRangeProbe(ws, p) end, "small>max")
  must_refuse_write(function(ws) local p = VH.NewRangeProbe(); p.Wide = { lo = 101, hi = 0 }; return VH.WriteRangeProbe(ws, p) end, "wide>max")

  local env = VH.NewReuseEnvelope()
  for _, st in ipairs({
    { "reuse_env_1", true, true, 0xdeadbeef, 0, 0 },
    { "reuse_env_2", true, false, 0, 0x4321, 0 },
    { "reuse_env_3", false, false, 0, 0, 0xcafebabe },
    { "reuse_env_4", true, true, 7, 0, 0 },
  }) do
    local raw = load_bin(st[1])
    check(VH.ReadReuseEnvelope(serialize.ReadStream.new(raw), env), "lua read " .. st[1])
    check(env.Active == st[2] and env.Inner == st[3] and env.A == st[4] and env.B == st[5] and env.C == st[6], "lua state " .. st[1])
    local ws = serialize.WriteStream.new(128); check(VH.WriteReuseEnvelope(ws, env), "lua write " .. st[1]); ws:flush(); check(ws:data() == raw, "lua bytes " .. st[1])
    check(not VH.ReadReuseEnvelope(serialize.ReadStream.new(load_bin("bad_reuse_trunc")), env), "lua trunc " .. st[1])
  end
end)
`)
	}
	if mode == "all" || mode == "surfaces" {
		b.WriteString(`
with_dir("./ptr", function()
  local T = require("GraphdemoTables")
  local J = require("json")
  local L = require("load")
  local S = require("save")
  local W = require("textformwrite")
  local dag_json = '{"name":"dag","version":3,"head":{"&node":1,"value":10,"name":"n1","next":{"&node":2,"value":20,"name":"n2","next":null}},"alias":{"&node":2},"ground":{"depth":4,"head":{"&node":1}}}'
  local rep = {}
  local s1 = J.FromJson(T, "Scene", dag_json, rep)
  check(s1 ~= nil and (rep.refused == 0 or rep.refused == false or rep.refused == nil), "lua dag FromJson")
  check(s1.head == s1.ground.head and s1.head.next == s1.alias, "lua dag pointer identity")
  local j1 = J.ToJson(T, "Scene", s1)
  check(type(j1) == "string" and j1:find('"&node": 1', 1, true) ~= nil and j1:find('"&node": 2', 1, true) ~= nil, "lua dag ToJson &node")
  local w1 = S.Save(T, "Scene", s1)
  check(type(w1) == "string" and #w1 > 0, "lua dag Save")
  local s2 = L.Load(T, "Scene", w1, {})
  check(s2 ~= nil and s2.head == s2.ground.head and s2.head.next == s2.alias and J.ToJson(T, "Scene", s2) == j1, "lua dag wire roundtrip")

  for _, item in ipairs({
    { "self-cycle", '{"head":{"&node":1,"value":1,"next":{"&node":1}}}' },
    { "forward-ref", '{"head":{"&node":99}}' },
    { "dup-def", '{"head":{"&node":1,"value":1,"next":null},"alias":{"&node":1,"value":2,"next":null}}' },
  }) do
    local br = {}
    local got = J.FromJson(T, "Scene", item[2], br)
    check(got == nil or (br.refused ~= 0 and br.refused ~= false and br.refused ~= nil), "lua must refuse &node " .. item[1])
  end

  local sexp = '; comment\n(:name "sexp" :version 7 :head ("&node" 5 :value 42 :name "shared" :next null) :alias ("&node" 5))\n'
  local doc, val = W.LoadTextForm(T, "Scene", sexp)
  check(doc ~= nil and val ~= nil and val.version == 7 and val.head == val.alias and val.head.value == 42, "lua sexp &node")
  check(W.SaveTextForm(doc) == sexp, "lua sexp bytes preserved")
  local ok_bad, bad_doc = pcall(function() return W.LoadTextForm(T, "Scene", '(:head ("&node" 99))') end)
  check(not ok_bad or bad_doc == nil, "lua sexp forward ref refused")

  local tcount = 0
  for _ in pairs(T.Tables) do tcount = tcount + 1 end
  check(T.Package == "graphdemo" and tcount >= 11, "lua UnitView/Tables")
  check(T.Tables["Scene"] ~= nil and #T.Tables["Scene"].fields == 9 and T.Tables["Meta"] ~= nil and #T.Tables["Meta"].fields == 2, "lua descriptors")
end)
`)
	}
	b.WriteString("print('VERIFY_VM_OK')\n")
	return b.String()
}

func buildSquirrelProbe(repoRoot, mode string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `local serialize = dofile(%q, true);
local function check(ok, msg) {
  if (!ok) {
    print("FAILED: " + msg + "\n");
    throw "FAILED: " + msg;
  }
}
local function load_blob(name) {
  local f = file("./inputs/" + name + ".bin", "rb");
  local len = f.len();
  local b = blob(len);
  if (len > 0) b.writeblob(f.readblob(len));
  f.close();
  b.seek(0);
  return b;
}
local function load_str(name) {
  local b = load_blob(name);
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%%c", b[i] & 0xff);
  return s;
}
local function blob_eq(a, b, len) {
  if (b.len() != len) return false;
  for (local i = 0; i < len; i++) if ((a[i] & 0xff) != (b[i] & 0xff)) return false;
  return true;
}
local function slice_blob(src, off, n) {
  local out = blob(n);
  for (local i = 0; i < n; i++) out.writen(src[off + i] & 0xff, 'b');
  out.seek(0);
  return out;
}
`, repoRoot+"/internal/codegen/squirrel/serialize/serialize.nut")
	if mode == "all" || mode == "packet" {
		b.WriteString(`
local exLoader = dofile("./ex/schema_module.nut", true);
local Types = exLoader.require("Types");
local Wire = exLoader.require("Wire");
local Clauses = exLoader.require("Clauses");
local Joins = exLoader.require("Joins");
local Constants = exLoader.require("Constants");
local ludLoader = dofile("./lud/schema_module.nut", true);
local Ludicrous = ludLoader.require("Ludicrous");
local vhLoader = dofile("./vh/schema_module.nut", true);
local VH = vhLoader.require("VerifyHostile");

local function rt(mod, typ, name) {
  local raw = load_blob(name);
  local v = mod[typ]();
  local rs = serialize.ReadStream(raw);
  check(mod["Read" + typ](rs, v), "sq read " + name);
  local wbuf = blob(((raw.len() + 7) & ~7) + 256);
  local ws = serialize.WriteStream(wbuf);
  check(mod["Write" + typ](ws, v), "sq write " + name);
  ws.flush();
  check(blob_eq(wbuf, raw, ws.bytesProcessed()), "sq byte-exact " + name);
}
local function rt_slices(mod, name, slices) {
  local raw = load_blob(name);
  local off = 0;
  for (local i = 0; i < slices.len(); i++) {
    local typ = slices[i][0];
    local n = slices[i][1];
    local sub = slice_blob(raw, off, n);
    local v = mod[typ]();
    local rs = serialize.ReadStream(sub);
    check(mod["Read" + typ](rs, v), name + "#" + i + " read " + typ);
    local wbuf = blob(256);
    local ws = serialize.WriteStream(wbuf);
    check(mod["Write" + typ](ws, v), name + "#" + i + " write " + typ);
    ws.flush();
    check(blob_eq(wbuf, sub, ws.bytesProcessed()), name + "#" + i + " byte-exact " + typ);
    off += n;
  }
  check(off == raw.len(), name + " total len");
}
`)
		for _, g := range canonicalSingleGoldens {
			fmt.Fprintf(&b, "rt(%s, %q, %q);\n", g.Module, g.Type, g.Name)
		}
		fmt.Fprintf(&b, "rt_slices(Clauses, \"clauses\", %s);\n", jsSlicesArray(buildClausesSlices()))
		fmt.Fprintf(&b, "rt_slices(Joins, \"joins\", %s);\n", jsSlicesArray(buildJoinsSlices()))
		for _, v := range []struct{ n, t string }{
			{"valid_const_reserved", "ConstReservedProbe"},
			{"valid_align", "AlignProbe"},
			{"valid_trail", "TrailProbe"},
			{"valid_range", "RangeProbe"},
			{"valid_text", "TextProbe"},
			{"valid_float_negzero", "FloatProbe"},
		} {
			fmt.Fprintf(&b, "rt(VH, %q, %q);\n", v.t, v.n)
		}
		for _, h := range hostileReadCases {
			fmt.Fprintf(&b, "{ local b = load_blob(%q); check(!VH.Read%s(serialize.ReadStream(b), VH.%s()), \"sq refuse %s\"); }\n", h.Name, h.Type, h.Type, h.Name)
		}
		b.WriteString(`
local function f64_bits(lo, hi) {
  local b = blob(8); b.writen(lo, 'i'); b.writen(hi, 'i'); b.seek(0); return b.readn('d');
}
local nan_val = f64_bits(0, 0x7ff80000);
local inf_val = f64_bits(0, 0x7ff00000);
local ninf_val = f64_bits(0, -1048576); // 0xfff00000
local function must_refuse_write(fn, what) {
  local accepted = false;
  try {
    local wb = blob(128);
    local ws = serialize.WriteStream(wb);
    if (fn(ws) == true) accepted = true;
  } catch (e) {}
  check(!accepted, "sq must refuse write " + what);
}
foreach (bad in [nan_val, inf_val, ninf_val]) {
  local x = bad;
  must_refuse_write(function(ws) { local p = VH.FloatProbe(); p.F32 = x; return VH.WriteFloatProbe(ws, p); }, "f32");
  must_refuse_write(function(ws) { local p = VH.FloatProbe(); p.F64 = x; return VH.WriteFloatProbe(ws, p); }, "f64");
  must_refuse_write(function(ws) { local p = VH.FloatProbe(); p.Cf = x; return VH.WriteFloatProbe(ws, p); }, "cf");
}
must_refuse_write(function(ws) { local p = VH.TextProbe(); p.Label[0] = 0xff; p.Label[1] = 0x61; p.LabelLength = 2; return VH.WriteTextProbe(ws, p); }, "bad utf8");
must_refuse_write(function(ws) { local p = VH.TextProbe(); p.Label[0] = 0x61; p.Label[1] = 0x00; p.LabelLength = 2; return VH.WriteTextProbe(ws, p); }, "nul utf8");
must_refuse_write(function(ws) { local p = VH.TextProbe(); p.LabelLength = 17; return VH.WriteTextProbe(ws, p); }, "len>16");
must_refuse_write(function(ws) { local p = VH.RangeProbe(); p.Mode = 3; return VH.WriteRangeProbe(ws, p); }, "bad enum");
must_refuse_write(function(ws) { local p = VH.RangeProbe(); p.Small = 9; return VH.WriteRangeProbe(ws, p); }, "small<min");
must_refuse_write(function(ws) { local p = VH.RangeProbe(); p.Small = 21; return VH.WriteRangeProbe(ws, p); }, "small>max");
must_refuse_write(function(ws) { local p = VH.RangeProbe(); p.Wide = [101, 0]; return VH.WriteRangeProbe(ws, p); }, "wide>max");

// Stateful RigidBody & LudicrousState & ReuseEnvelope reuse:
{
  local rb = Types.RigidBody();
  local mov = load_blob("rigidbody_moving");
  local rest = load_blob("rigidbody_at_rest");
  local bad_rb = load_blob("bad_rb_trunc");
  check(Types.ReadRigidBody(serialize.ReadStream(mov), rb) && !rb.AtRest, "sq rb mov 1");
  check(Types.ReadRigidBody(serialize.ReadStream(rest), rb) && rb.AtRest && rb.LinearVelocity.X == 0.0 && rb.AngularVelocity.Y == 0.0, "sq rb rest zeroed");
  local wb = blob(256); local ws = serialize.WriteStream(wb); check(Types.WriteRigidBody(ws, rb), "sq write rb rest"); ws.flush();
  check(blob_eq(wb, rest, ws.bytesProcessed()), "sq rb rest bytes");
  check(!Types.ReadRigidBody(serialize.ReadStream(bad_rb), rb), "sq rb trunc refused");
  check(Types.ReadRigidBody(serialize.ReadStream(mov), rb) && !rb.AtRest, "sq rb mov 2");

  local ls = Ludicrous.LudicrousState();
  local tw = load_blob("ludicrous_state");
  local uw = load_blob("ludicrous_state_untargeted");
  check(Ludicrous.ReadLudicrousState(serialize.ReadStream(tw), ls) && ls.HasTarget && ls.TargetId[0] == 42, "sq lud targeted");
  check(Ludicrous.ReadLudicrousState(serialize.ReadStream(uw), ls) && !ls.HasTarget && ls.TargetId[0] == 0 && ls.TargetId[1] == 0, "sq lud untargeted");
}
`)
	}
	if mode == "all" || mode == "surfaces" {
		b.WriteString(`
{
  local ptrLoader = dofile("./ptr/schema_module.nut", true);
  local T = ptrLoader.require("GraphdemoTables");
  local J = ptrLoader.require("schema_table_json");
  local L = ptrLoader.require("schema_table_load");
  local S = ptrLoader.require("schema_table_save");
  local D = ptrLoader.require("schema_table_document");
  local dag_json = "{\"name\":\"dag\",\"version\":3,\"head\":{\"&node\":1,\"value\":10,\"name\":\"n1\",\"next\":{\"&node\":2,\"value\":20,\"name\":\"n2\",\"next\":null}},\"alias\":{\"&node\":2},\"ground\":{\"depth\":4,\"head\":{\"&node\":1}}}";
  local rep = {};
  local s1 = J.FromJson(T, "Scene", dag_json, rep);
  check(s1 != null && !("refused" in rep && rep.refused), "sq dag FromJson");
  check(s1.Head == s1.Ground.Head && s1.Head.Next == s1.Alias, "sq dag pointer identity");
  local j1 = J.ToJson(T, "Scene", s1);
  check(typeof j1 == "string" && j1.find("\"&node\": 1") != null && j1.find("\"&node\": 2") != null, "sq dag ToJson &node");
  local w1 = T.SceneSave(s1);
  check(w1 != null && w1.len() > 0, "sq dag Save");
  local s2 = T.SceneLoad(w1, {});
  check(s2 != null && s2.Head == s2.Ground.Head && s2.Head.Next == s2.Alias && J.ToJson(T, "Scene", s2) == j1, "sq dag wire roundtrip");

  foreach (item in [
    ["self-cycle", "{\"head\":{\"&node\":1,\"value\":1,\"next\":{\"&node\":1}}}"],
    ["forward-ref", "{\"head\":{\"&node\":99}}"],
    ["dup-def", "{\"head\":{\"&node\":1,\"value\":1,\"next\":null},\"alias\":{\"&node\":1,\"value\":2,\"next\":null}}"],
  ]) {
    local br = { refused = false };
    local got = null;
    try { got = J.FromJson(T, "Scene", item[1], br); } catch (e) { br.refused = true; }
    check(got == null || br.refused, "sq must refuse &node " + item[0]);
  }

  local sexp = "; comment\n(:name \"sexp\" :version 7 :head (\"&node\" 5 :value 42 :name \"shared\" :next null) :alias (\"&node\" 5))\n";
  local srep = { refused = false };
  local doc = D.LoadTextForm(T, "Scene", sexp, null, srep);
  check(doc != null && !srep.refused && doc.value.Version == 7 && doc.value.Head == doc.value.Alias && doc.value.Head.Value == 42, "sq sexp &node");
  local saved_blob = D.SaveTextForm(T, "Scene", doc);
  local saved_str = "";
  for (local i = 0; i < saved_blob.len(); i++) saved_str += format("%c", saved_blob[i] & 0xff);
  check(saved_str == sexp, "sq sexp bytes preserved");
  local bad_srep = { refused = false };
  local bad_doc = null;
  try { bad_doc = D.LoadTextForm(T, "Scene", "(:head (\"&node\" 99))", null, bad_srep); } catch (e) { bad_srep.refused = true; }
  check(bad_doc == null || bad_srep.refused, "sq sexp forward ref refused");

  local uv = T.UnitView();
  check(uv.package == "graphdemo" && uv.tables.len() == 11 && uv.types.len() == 1 && uv.enums.len() == 1 && uv.constants.len() == 1, "sq UnitView");
  check(T.SceneTableFields.len() == 9 && T.MetaTableFields.len() == 2, "sq descriptors");
}
`)
	}
	b.WriteString("print(\"VERIFY_VM_OK\\n\");\n")
	return b.String()
}
