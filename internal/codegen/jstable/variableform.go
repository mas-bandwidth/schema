// THE VARIABLE FORM (docs/SPEC-TABLES.md §3.1), form byte 1, in JavaScript: a
// Load and Save that carry a table byte for byte as the Go reference engine
// (internal/tablewire) does, and the text form (§16) beside them, for the
// variable class this leg carries — table roots and pointers *T over the flat
// node table (§2.1, §3.1), tables nested by value, *string and *bytes blobs
// (§2.5), bool and every int/uint/float/bits width, string(N) and bytes(N),
// enums, and unbounded []T and bounded [..N]T arrays of scalar, enum, table and
// pointer elements (§2.9). Reading is wire tolerance (§4): unknown ids are
// skipped and counted, absent fields read their defaults, kind mismatch, clamp
// and widening are reported, and identity rides the name hash (§5). Every other
// variable-class construct is refused by name (§11) in fixedform.go's
// variableFieldRefusal, and a table that reaches one is refused with it.
//
// THE WALK IS A PORT OF THE GO REFERENCE ENGINE (internal/tablewire,
// internal/tabletext), held to it by the reference bytes: the card corpus
// (tables/cards/fuzz) and the variable-class corpus (tables/pointers,
// tables/blobs, tables/lists) in TestVariableClassBeyondCardConstructsJavaScript.
// The emitter writes one CODEC DESCRIPTOR per table (TableVariableCodecs) and
// the walk in variableruntime.go reads it, so every rule is spelled once.
package jstable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsId renders a wire id (docs/SPEC-TABLES.md §5) as a JavaScript BigInt
// literal, the one cell this language holds a sixty-four-bit hash in without
// folding it. Ids are compile-time constants: fnv1a64 runs here, once, never in
// the generated module.
func jsId(id uint64) string { return fmt.Sprintf("0x%016xn", id) }

// closureHoldsFixedTable reports whether the unit's table closure declares a
// `fixed table` (docs/SPEC-TABLES.md §2.2). Such a unit is served by the fixed
// form (and its block and cook surfaces), so
// refuseCardUnsupportedVariableConstructs — the variable form's own boundary —
// does not bind it (§11): a variable table beside a declared fixed one is not
// mis-encoded by anything this leg writes, and refusing the whole unit would
// take the fixed form's surfaces down with it. In such a unit a table the
// variable wire does not carry stays absent and unclaimed.
//
// THE PREDICATE IS THE DECLARATION, NOT THE SERVICE. A variable table is the
// VARIABLE wire whatever its fields happen to be (§2.2), so the predicate
// scopes on !ir.VariableTables(u)[name], the same predicate the Elixir leg
// holds (internal/codegen/elixirtable/variablerefusal.go), so only a unit whose
// closure declares a `fixed table` is the unit the boundary does not bind.
func closureHoldsFixedTable(u *ir.Unit, closure map[string]bool) bool {
	variable := ir.VariableTables(u)
	for name := range closure {
		if st := u.Tables[name]; st != nil && !variable[name] {
			return true
		}
	}
	return false
}

// refuseCardUnsupportedVariableConstructs is docs/SPEC-TABLES.md §11's refusal
// at generation: it refuses, BY NAME, every variable table this backend does
// not carry on the table wire — a table holding a construct outside the class
// this leg carries, or reaching one that does (newVariableClass). The rest of
// the class stays absent and is refused rather than mis-encoded or silently
// dropped. The message is `<table>.<field>: <what>`, which is what a refusal
// by name owes (§11).
func refuseCardUnsupportedVariableConstructs(u *ir.Unit) error {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	vc := newVariableClass(u)
	names := make([]string, 0, len(variable))
	for name := range variable {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if why := vc.reason[name]; why != "" {
			return fmt.Errorf("js variable form: %s (docs/SPEC-TABLES.md §11)", why)
		}
	}
	return nil
}

// variableEmit generates the WHOLE variable-form module for a unit: the shared
// runtime, one class and one codec descriptor per carried table, and the
// Load/Measure/Save/LoadMeasure/ToJson/FromJson surface of every carried root.
// Everything lands in ONE self-contained module, <Package>Variable.js, so a
// consumer imports one name and the module never imports another generated
// file.
func variableEmit(u *ir.Unit) map[string][]byte {
	vc := newVariableClass(u)
	roots := []*ir.Struct{}
	for _, f := range u.Files {
		for _, st := range variableRoots(u, f.Tables) {
			// THE FILTER IS THE REFUSAL ITSELF (docs/SPEC-TABLES.md §11), as
			// fixedRoots filters on fixedRefusal: a table this leg does not carry
			// gets no codec, and the refusal names it. It is never mis-encoded
			// and never dropped silently.
			if !vc.carried(st.Name) {
				continue
			}
			roots = append(roots, st)
		}
	}
	if len(roots) == 0 {
		return map[string][]byte{}
	}
	// the closure: every table a root reaches by value, by element or by
	// pointer, dependency-first
	seen := map[string]bool{}
	var closure []*ir.Struct
	for _, r := range roots {
		variableCollectTypes(r, seen, &closure)
	}
	g := &variableGen{unit: u, variable: ir.VariableTables(u)}
	g.pf("// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", u.Package)
	g.pf("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	g.pf("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	g.pf("// AGPL-3.0, its output is not.\n")
	g.pf("// package %s — the VARIABLE FORM (docs/SPEC-TABLES.md §3.1), form byte 1.\n", u.Package)
	g.pf("//\n")
	g.pf("// A saved table is a form byte, a root body of fields (id reference, kind,\n")
	g.pf("// payload), and an id table as the trailer (docs/SPEC-TABLES.md §3). A pointer\n")
	g.pf("// *T, a *string and a *bytes ride as a node index into the flat node table the\n")
	g.pf("// root body carries last (docs/SPEC-TABLES.md §3.1). Reading is tolerant: an\n")
	g.pf("// unknown id is skipped and counted, an absent field reads its declared\n")
	g.pf("// default, and a kind mismatch is reported (§4).\n")
	g.pf("\n")
	g.emitRuntime()
	g.emitRetainRuntime()
	g.pf("%s", jsVariableCodecRuntime)
	g.pf("%s", jsVariableArmsRuntime)
	// THE ONE TEXT WALK (docs/PORTING.md M9): the text form and the four
	// constructs' text arms, between markers `make tables-js-json-walk`
	// compares byte for byte across every generated unit.
	g.pf("\n%s\n", jsJsonWalkBegin)
	g.pf("%s", jsVariableTextRuntime)
	g.pf("%s", jsVariableArmsTextRuntime)
	g.pf("%s\n", jsJsonWalkEnd)
	g.emitDescriptors(closure)
	for _, st := range closure {
		g.emitTableClass(st)
	}
	g.emitCodecs(closure)
	// Cook write runs after the codecs: variableMeasure calls
	// TableVariableNumber and TableVariableCodecs (docs/SPEC-TABLES.md §7.6).
	g.emitVariableCookWrite(roots)
	for _, r := range roots {
		g.emitRootCodec(r)
		g.emitRetainMember(r)
	}
	return map[string][]byte{runtimeHome(u) + "Variable.js": []byte(g.body.String())}
}

type variableGen struct {
	unit     *ir.Unit
	variable map[string]bool
	body     strings.Builder
}

func (g *variableGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// enumOf returns the enum a field's values come from, or nil.
func enumOf(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	if e, ok := f.Type.Ref.(*ir.Enum); ok {
		return e
	}
	return nil
}

// tableOf returns the table a field names — by value, by pointer or as an
// array's element — or nil.
func tableOf(f *ir.Field) *ir.Struct {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	if st, ok := f.Type.Ref.(*ir.Struct); ok {
		return st
	}
	return nil
}

// ---------------------------------------------------------------------------
// THE SHARED RUNTIME
// ---------------------------------------------------------------------------

func (g *variableGen) emitRuntime() {
	g.pf("%s", `// ---------------------------------------------------------------------------
// THE VARIABLE FORM RUNTIME (docs/SPEC-TABLES.md §3)
// ---------------------------------------------------------------------------

export const TableVariableForm = 1;

// THE RESERVED IDS the language holds back (docs/SPEC-TABLES.md §3.1, §5).
export const TableNodeTableId = 0xffffffffffffffffn;
export const TableBuildVersionId = 0xfffffffffffffffen;
export const TableMessageVocabularyId = 0xfffffffffffffffdn;
export const TableStringBlobId = `+jsId(ir.StringWireTypeId)+`;
export const TableBytesBlobId = `+jsId(ir.BytesWireTypeId)+`;

// The unit's canonical region alignment (docs/SPEC-TABLES.md §7, §6.5): the
// greatest record alignment in the table closure, never below eight. LoadMeasure
// rounds every node extent to it, exactly as FileRegionMeasure does.
export const TableVariableRegionAlign = `+fmt.Sprintf("%d", ir.TableRegionAlign(g.unit))+`;

// Arithmetic round-up to a (a power of two), so a region size past 2^31 does
// not fold through the language's 32-bit bitwise operators.
function TableVariableRound(n, a) {
  return n + (a - 1) - ((n + a - 1) % a);
}

// THE REPORT (docs/SPEC-TABLES.md §4): the six counters and the refusal. The
// caller owns it; the codec never makes one.
//
// reason is WHY A READ WAS REFUSED, spelled as the reference leg's report spells
// it (Go's TableReport.Reason, docs/SPEC-TABLES.md §3, §6.5): a refused form
// byte answers message_form_as_file for form 2 and newer_form for any other.
// A read that WIDENS is not a refusal: the value lands in the wider field, the
// widened counter counts it (§4.1), and reason stays empty, exactly as Go's
// does for a file read. Damage is the malformed flag, never a reason.
export class TableVariableReport {
  constructor() {
    this.reason = "";
    this.malformed = false;
    this.refused = 0;
    this.unknown = 0;
    this.kindMismatch = 0;
    this.clamped = 0;
    this.widened = 0;
    this.duplicate = 0;
    this.retained = 0;
    this.retainLost = 0;
    // the first float a read found to be a NaN or an infinity, <Table>.<field>
    // (docs/SPEC-TABLES.md §3.3): the read refuses, reason non_finite_float
    this.nonFiniteFloat = "";
  }
}

// Ids intern a writer's ids in FIRST-USE order (docs/SPEC-TABLES.md §3), each a
// BigInt, returning the 1-based reference. First-use order follows the write
// order, so byte-identical output against the reference requires the same walk.
// With a retention store attached, a NON-NAMEABLE id (docs/SPEC-TABLES.md §6.6)
// first used here takes an entry from the caller's id list, and a full list
// answers -1 rather than growing. mark and rollback are how a writer decides
// ELISION without spending an entry (internal/tablewire ids.go).
class TableVariableIds {
  constructor(retain) { this.list = []; this.index = new Map(); this.retain = retain || null; }
  ref(id, retained) {
    const r = this.index.get(id);
    if (r !== undefined) { return r; }
    const isRetained = !!retained && !tableRetainNameable(id);
    if (isRetained && this.retain !== null && this.retain.idUsed >= this.retain.ids.length) { return -1; }
    this.list.push(id);
    const n = this.list.length;
    this.index.set(id, n);
    if (isRetained && this.retain !== null) { this.retain.ids[this.retain.idUsed++] = { id, slot: n }; }
    return n;
  }
  mark() { return { n: this.list.length, used: this.retain === null ? 0 : this.retain.idUsed }; }
  rollback(m) {
    while (this.list.length > m.n) { this.index.delete(this.list.pop()); }
    if (this.retain !== null) { this.retain.idUsed = m.used; }
  }
}

// THE WRITER: a growable byte list. Offsets and lengths are LEB128 (§3).
class TableVariableWriter {
  constructor() { this.b = []; }
  get len() { return this.b.length; }
  u8(v) { this.b.push(v & 0xff); }
  u16(v) { this.b.push(v & 0xff, (v >>> 8) & 0xff); }
  leb(v) {
    v = Number(v);
    while (v >= 128) { this.b.push((v & 0x7f) | 0x80); v = Math.floor(v / 128); }
    this.b.push(v & 0x7f);
  }
  raw(bytes) { for (let i = 0; i < bytes.length; i++) { this.b.push(bytes[i]); } }
  u32(v) { this.b.push(v & 0xff, (v >>> 8) & 0xff, (v >>> 16) & 0xff, (v >>> 24) & 0xff); }
  i32(v) { this.u32(v | 0); }
  u64(v) { for (let i = 0; i < 8; i++) { this.b.push(Number(v & 0xffn)); v = v >> 8n; } }
  u128(v) { for (let i = 0; i < 16; i++) { this.b.push(Number(v & 0xffn)); v = v >> 8n; } }
  f32(v) {
    const d = TableVariableScratch;
    d.setFloat32(0, v, true);
    this.u32(d.getUint32(0, true));
  }
  f64(v) {
    const d = TableVariableScratch;
    d.setFloat64(0, v, true);
    for (let i = 0; i < 8; i++) { this.b.push(d.getUint8(i)); }
  }
  toArray() { return new Uint8Array(this.b); }
}

// THE READER: a cursor over the wire, everything little-endian (§3).
class TableVariableReader {
  constructor(bytes, at, end) { this.b = bytes; this.at = at; this.end = end; this.bad = false; }
  get ok() { return this.at <= this.end; }
  u8() { return this.b[this.at++]; }
  leb() {
    // A FAILED LEB LEAVES THE CURSOR WHERE IT STARTED (docs/SPEC-TABLES.md §3),
    // as the engine's readLeb does: one value has one spelling, and a number
    // that is not one is not consumed.
    this.bad = false;
    let r = 0, s = 0;
    const start = this.at;
    for (let i = 0; i < 10; i++) {
      if (this.at >= this.end) { this.bad = true; this.at = start; return 0; }
      const x = this.b[this.at++];
      if (i === 9 && x > 1) { this.bad = true; this.at = start; return 0; }
      r += (x & 0x7f) * Math.pow(2, s);
      if ((x & 0x80) === 0) {
        if (i > 0 && x === 0) { this.bad = true; this.at = start; return 0; }
        return r;
      }
      s += 7;
    }
    this.bad = true;
    this.at = start;
    return 0;
  }
  u16() { const v = this.b[this.at] | (this.b[this.at + 1] << 8); this.at += 2; return v; }
  u32() {
    const b = this.b, a = this.at; this.at += 4;
    return (b[a] | (b[a + 1] << 8) | (b[a + 2] << 16) | (b[a + 3] << 24)) >>> 0;
  }
  i32() { return this.u32() | 0; }
  i8() { const v = this.b[this.at]; this.at += 1; return (v & 0x80) ? (v - 0x100) : v; }
  i16() { const v = this.b[this.at] | (this.b[this.at + 1] << 8); this.at += 2; return (v & 0x8000) ? (v - 0x10000) : v; }
  u64() {
    let v = 0n;
    for (let i = 0; i < 8; i++) { v |= BigInt(this.b[this.at + i]) << BigInt(8 * i); }
    this.at += 8;
    return v;
  }
  i64() { return BigInt.asIntN(64, this.u64()); }
  u128() {
    let v = 0n;
    for (let i = 0; i < 16; i++) { v |= BigInt(this.b[this.at + i]) << BigInt(8 * i); }
    this.at += 16;
    return v;
  }
  i128() { return BigInt.asIntN(128, this.u128()); }
  f64() {
    const d = TableVariableScratch;
    for (let i = 0; i < 8; i++) { d.setUint8(i, this.b[this.at + i]); }
    this.at += 8;
    return d.getFloat64(0, true);
  }
}

// ---- UTF-8, byte-exact and validating (docs/SPEC-TABLES.md §3) -------------

function TableVariableUtf8Encode(s) {
  const out = [];
  for (let i = 0; i < s.length; i++) {
    let cp = s.codePointAt(i);
    if (cp > 0xffff) { i++; }
    if (cp < 0x80) { out.push(cp); }
    else if (cp < 0x800) { out.push(0xc0 | (cp >> 6), 0x80 | (cp & 0x3f)); }
    else if (cp < 0x10000) { out.push(0xe0 | (cp >> 12), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f)); }
    else { out.push(0xf0 | (cp >> 18), 0x80 | ((cp >> 12) & 0x3f), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f)); }
  }
  return out;
}

function TableVariableUtf8Decode(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length;) {
    const b0 = bytes[i];
    if (b0 === 0) { return { s, ok: false }; }
    if (b0 < 0x80) { s += String.fromCharCode(b0); i++; continue; }
    let need, cp;
    if ((b0 & 0xe0) === 0xc0) { need = 1; cp = b0 & 0x1f; }
    else if ((b0 & 0xf0) === 0xe0) { need = 2; cp = b0 & 0x0f; }
    else if ((b0 & 0xf8) === 0xf0) { need = 3; cp = b0 & 0x07; }
    else { return { s, ok: false }; }
    if (i + need >= bytes.length) { return { s, ok: false }; }
    for (let k = 1; k <= need; k++) {
      const b = bytes[i + k];
      if ((b & 0xc0) !== 0x80) { return { s, ok: false }; }
      cp = (cp << 6) | (b & 0x3f);
    }
    const min = need === 1 ? 0x80 : need === 2 ? 0x800 : 0x10000;
    if (cp < min || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff)) { return { s, ok: false }; }
    s += String.fromCodePoint(cp);
    i += 1 + need;
  }
  return { s, ok: true };
}

// ---- WIDE TEXT, kind 33 (docs/SPEC-TABLES.md §3, §4; SPEC.md §4.12) --------
//
// A wstring(N) is held as a JavaScript string, which IS a sequence of UTF-16
// code units, so the wire's units are the string's own char codes: two bytes
// each, LITTLE-ENDIAN, under a byte length L that is twice the unit count
// (internal/tablewire text.go wideUnits, wideBytes).

// the units onto the wire, or false for a value past its N units: wide text is
// never truncated on the way out (SPEC.md §4.12) — the save is refused (null)
function TableVariableWideWrite(w, s, bound) {
  if (s.length > bound) { return false; }
  for (let i = 0; i < s.length; i++) { const u = s.charCodeAt(i); w.u8(u & 0xff); w.u8(u >>> 8); }
  return true;
}

// a kind 33 payload's text (wideUnits + wideValid): null for an odd L or for
// content that is not paired UTF-16 with no zero unit — the reader's damage
function TableVariableWideDecode(bytes) {
  if ((bytes.length & 1) !== 0) { return null; }
  let s = "";
  for (let i = 0; i < bytes.length; i += 2) {
    const u = bytes[i] | (bytes[i + 1] << 8);
    if (u === 0) { return null; }
    if (u >= 0xd800 && u <= 0xdbff) {
      if (i + 3 >= bytes.length) { return null; } // a high surrogate with no low half
      const lo = bytes[i + 2] | (bytes[i + 3] << 8);
      if (lo < 0xdc00 || lo > 0xdfff) { return null; }
    } else if (u >= 0xdc00 && u <= 0xdfff) {
      const hi = i >= 2 ? bytes[i - 2] | (bytes[i - 1] << 8) : 0;
      if (hi < 0xd800 || hi > 0xdbff) { return null; } // a low surrogate first
    }
    s += String.fromCharCode(u);
  }
  return s;
}

// THE CLAMP (wideBoundary, §3, §16.2): the first N units, and where the last
// kept unit is a high surrogate whose low half did not fit, that unit goes too,
// so a clamp never invents an unpaired surrogate. Over well-formed text this is
// the longest prefix of whole code points within N, which is the text form's
// rule as well (internal/tabletext read.go scanWString).
function TableVariableWideClamp(s, bound) {
  if (s.length <= bound) { return s; }
  let cut = bound;
  if (cut > 0) { const u = s.charCodeAt(cut - 1); if (u >= 0xd800 && u <= 0xdbff) { cut--; } }
  return s.slice(0, cut);
}

// ---- skipping a field the reader cannot name (docs/SPEC-TABLES.md §3) ------

// Kind 36, the timestamp, skips its eight bytes like any fixed-width kind
// (§3: kinds 1-11, 18-29 and 36 skip their fixed width), so a field this
// reader cannot name under it is counted, never malformed.
function TableVariableSkip(r, kind) {
  switch (kind) {
    case 1: case 2: case 6: case 20: case 25: { if (r.at + 1 > r.end) return false; r.at += 1; return true; }
    case 3: case 7: case 21: case 26: { if (r.at + 2 > r.end) return false; r.at += 2; return true; }
    case 4: case 8: case 10: case 22: case 27: { if (r.at + 4 > r.end) return false; r.at += 4; return true; }
    case 5: case 9: case 11: case 23: case 28: case 36: { if (r.at + 8 > r.end) return false; r.at += 8; return true; }
    case 18: case 19: case 24: case 29: { if (r.at + 16 > r.end) return false; r.at += 16; return true; }
    case 12: case 13: case 14: case 16: case 31: case 32: case 33: {
      const L = r.leb(); if (r.bad) return false; if (r.at + L > r.end) return false; r.at += L; return true;
    }
    case 15: {
      const arm = r.leb(); if (r.bad) return false;
      if (arm !== 0) { if (r.at + 1 > r.end) return false; r.u8(); const L = r.leb(); if (r.bad) return false; if (r.at + L > r.end) return false; r.at += L; }
      return true;
    }
    case 17: case 30: { r.leb(); return !r.bad; }
    default: return false;
  }
}

// ---- the node table's records (docs/SPEC-TABLES.md §3.1) ------------------

// TableVariableNodeTableScan finds the root body's reserved node-table field and
// scans its records, BEFORE any value decode: the numbering is positional and
// read once. The table is whole or it is nothing — a field under another kind,
// a length past the body, a record that cannot be framed or a count the scan
// does not match is one malformed and answers null, and every pointer in the
// save then reads null (internal/tablewire nodetable.go). No node table at all
// is an empty numbering.
function TableVariableNodeTableScan(bytes, table, end, report) {
  const w = new TableVariableReader(bytes, 1, end);
  let payload = null, present = false, bad = false;
  while (!bad) {
    const ref = w.leb();
    if (w.bad) { break; }
    if (ref === 0) { break; }
    if (ref > table.length) { break; }
    if (w.at >= w.end) { break; }
    const id = table[ref - 1];
    const kind = w.u8();
    if (id === TableNodeTableId) {
      present = true;
      if (kind !== 12) { bad = true; break; }
      const L = w.leb();
      if (w.bad || w.at + L > w.end) { bad = true; break; }
      payload = bytes.slice(w.at, w.at + L);
      w.at += L;
      continue;
    }
    if (!TableVariableSkip(w, kind)) { break; }
  }
  if (present && bad) { report.malformed = true; return null; }
  if (!present) { return []; }
  const rec = new TableVariableReader(payload, 0, payload.length);
  const count = rec.leb();
  if (rec.bad) { report.malformed = true; return null; }
  const raw = [];
  while (rec.at < rec.end) {
    const tref = rec.leb();
    if (rec.bad || tref === 0 || tref > table.length) { report.malformed = true; return null; }
    const tlen = rec.leb();
    if (rec.bad || rec.at + tlen > rec.end) { report.malformed = true; return null; }
    raw.push({ tid: table[tref - 1], body: payload.slice(rec.at, rec.at + tlen) });
    rec.at += tlen;
  }
  if (count !== raw.length) { report.malformed = true; return null; }
  return raw;
}

// ---- the text form's string (docs/SPEC-TABLES.md §16.3; tabletext writeString)

function TableVariableJsonString(s) {
  let out = '"';
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
      const lo = s.charCodeAt(i + 1);
      if (lo >= 0xdc00 && lo <= 0xdfff) { out += s[i] + s[i + 1]; i++; continue; }
    }
    if (c >= 0xd800 && c <= 0xdfff) { out += "�"; continue; }
    switch (c) {
      case 0x22: out += '\\"'; break;
      case 0x5c: out += '\\\\'; break;
      case 0x08: out += '\\b'; break;
      case 0x0c: out += '\\f'; break;
      case 0x0a: out += '\\n'; break;
      case 0x0d: out += '\\r'; break;
      case 0x09: out += '\\t'; break;
      default:
        if (c < 0x20) { out += '\\u00' + (c < 16 ? '0' : '') + c.toString(16); }
        else { out += s[i]; }
    }
  }
  return out + '"';
}
`)
}

// ---------------------------------------------------------------------------
// THE DESCRIPTORS (docs/SPEC-TABLES.md §8.1) and the UNIT REGISTRY (§8.3)
// ---------------------------------------------------------------------------

// fieldShape is a field's STORAGE SHAPE (docs/SPEC-TABLES.md §8.1): "value"
// for a scalar, a string, a bytes(N), an enum or a table nested by value;
// "list" for an array ([]T and [..N]T, §2.9); "blob" for a *string or *bytes
// byte buffer (§2.5); and "pointer" for a *T (§2.1). A walker reads the shape
// beside the kind to know whether a field holds one value, a counted sequence,
// a node-indexed blob or a node index, without a per-version struct.
func fieldShape(f *ir.Field) string {
	switch {
	case f.Array != ir.ArrayNone:
		return "list"
	case f.Type.Pointer && f.Type.Blob():
		return "blob"
	case f.Type.Pointer:
		return "pointer"
	}
	return "value"
}

// fieldBound is a field's DECLARED BOUND (docs/SPEC-TABLES.md §8.1): a
// string(N) or bytes(N) declares N, a bounded array [..N]T declares N, and
// every other construct is unbounded or bounded by its own kind, so it
// declares 0.
func fieldBound(f *ir.Field) int64 {
	if f.Array == ir.ArrayCounted {
		return f.ArrayBound
	}
	if f.Array == ir.ArrayNone && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Blob() {
		return f.Type.Size
	}
	return 0
}

// emitDescriptors emits one table type descriptor per table in the variable
// closure (docs/SPEC-TABLES.md §8.1) — field name, wire kind, storage shape and
// declared bound, in declaration order — then the unit registry (§8.3) that
// lists them. Enough to walk a record by name and kind without a per-version
// struct; nothing else of §8 is claimed.
func (g *variableGen) emitDescriptors(closure []*ir.Struct) {
	for _, st := range closure {
		layout := ir.RecordLayout(g.unit, st)
		g.pf("export const %sTableType = TableVariableFreeze({\n", st.Name)
		g.pf("  name: %q,\n", st.Name)
		g.pf("  sz: %d, al: %d,\n", layout.Size, layout.Align)
		g.pf("  fields: [\n")
		for _, f := range st.Fields {
			g.pf("    { name: %q, kind: %d, shape: %q, bound: %d },\n",
				f.Name, ir.TableWireFieldKind(f), fieldShape(f), fieldBound(f))
		}
		g.pf("  ],\n")
		g.pf("});\n\n")
	}
	g.pf("export const %sUnitRegistry = TableVariableFreeze({\n", capitalize(g.unit.Package))
	g.pf("  package: %q,\n", g.unit.Package)
	g.pf("  tables: [\n")
	for _, st := range closure {
		g.pf("    %sTableType,\n", st.Name)
	}
	g.pf("  ],\n")
	g.pf("});\n\n")
}

// ---------------------------------------------------------------------------
// THE STORAGE CLASS
// ---------------------------------------------------------------------------

// emitTableClass emits a table's storage class: every field at its DECLARED
// DEFAULT (SPEC.md §4.2), which is also the value the writer elides against and
// the value an absent field reads (docs/SPEC-TABLES.md §3, §4).
func (g *variableGen) emitTableClass(st *ir.Struct) {
	g.pf("export class %s {\n  constructor() {\n", st.Name)
	for _, f := range st.Fields {
		g.pf("    this.%s = %s;\n", f.Name, g.defaultExpr(f))
	}
	g.pf("  }\n")
	for _, f := range st.Fields {
		if f.KeyEnum != "" {
			emitKeyedStatics(g.pf, st.Name, f.Name, f)
		}
	}
	g.pf("}\n\n")
}

// defaultExpr is a field's declared default in JavaScript storage.
func (g *variableGen) defaultExpr(f *ir.Field) string {
	switch {
	case f.Type.Optional:
		// ABSENT is null (docs/SPEC-TABLES.md §2.3): presence is the value
		return "null"
	case f.IsMap():
		// a map is a JavaScript Map from key to value (§2.8)
		return "new Map()"
	case unionOf(f) != nil && f.Array == ir.ArrayNone:
		// None is null; an arm is { arm: "<name>", value } (§2.6)
		return "null"
	case f.KeyEnum != "" || f.Array == ir.ArrayFixed:
		// one slot per keyed variant or fixed bound, each at the element's default (§2.4, §2.9)
		return fmt.Sprintf("Array.from({ length: %d }, () => %s)", f.ArrayBound, g.elemDefaultExpr(f))
	case f.Array != ir.ArrayNone:
		return "[]"
	case f.Type.Pointer:
		return "null"
	case enumOf(f) != nil:
		return enumDefault(f)
	case tableOf(f) != nil:
		return "new " + tableOf(f).Name + "()"
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TString:
		return jsStr(string(f.DefBytes))
	case ir.TWString:
		// a JavaScript string IS UTF-16 code units, so the storage is one; wide
		// text takes no declared default (SPEC.md §4.12), so it is empty
		return `""`
	case ir.TBytes:
		return "new Uint8Array(" + jsByteList(f.DefBytes) + ")"
	case ir.TFloat32, ir.TFloat64:
		return floatDefault(f)
	}
	return intDefault(f)
}

// elemDefaultExpr is one keyed or fixed-array slot's declared default: the
// field's own default, which every slot holds until a value is placed (§2.4).
func (g *variableGen) elemDefaultExpr(f *ir.Field) string {
	if !f.Type.Pointer && tableOf(f) != nil {
		return "new " + tableOf(f).Name + "()"
	}
	plain := *f
	plain.KeyEnum, plain.KeyEnumRef = "", nil
	plain.Array, plain.ArrayBound = ir.ArrayNone, 0
	return g.defaultExpr(&plain)
}

// unionOf returns the union a field names — as a field or as an array's
// element — or nil.
func unionOf(f *ir.Field) *ir.Union {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	if un, ok := f.Type.Ref.(*ir.Union); ok {
		return un
	}
	return nil
}

// flagsOf returns the flags declaration a field names, or nil.
func flagsOf(f *ir.Field) *ir.Flags {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	if fl, ok := f.Type.Ref.(*ir.Flags); ok {
		return fl
	}
	return nil
}

func flagsVariants(fl *ir.Flags) string {
	parts := make([]string, len(fl.Variants))
	for i, v := range fl.Variants {
		parts[i] = jsStr(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type guardStep struct {
	Name string
	Want bool
}

func structGuards(st *ir.Struct) map[string][]guardStep {
	out := map[string][]guardStep{}
	for _, f := range st.Fields {
		if f.Guard == "" {
			continue
		}
		var chain []guardStep
		cur := f.Guard
		for cur != "" {
			want := true
			if strings.HasPrefix(cur, "!") {
				want = false
				cur = cur[1:]
			}
			chain = append([]guardStep{{Name: cur, Want: want}}, chain...)
			var gf *ir.Field
			for _, cand := range st.Fields {
				if cand.Name == cur {
					gf = cand
					break
				}
			}
			if gf == nil {
				break
			}
			cur = gf.Guard
		}
		out[f.Name] = chain
	}
	return out
}

func jsGuards(steps []guardStep) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = fmt.Sprintf("[%s, %v]", jsStr(s.Name), s.Want)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// jsStr renders a JavaScript string literal: JSON's spelling, which is a
// subset of JavaScript's.
func jsStr(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func jsByteList(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = strconv.Itoa(int(c))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func enumDefault(f *ir.Field) string {
	if f.HasDefault && f.DefVariant != "" && f.DefVariant != "None" {
		return jsStr(f.DefVariant)
	}
	return "null"
}

func floatDefault(f *ir.Field) string {
	if !f.HasDefault {
		return "0"
	}
	return jsFloat(f.DefFloat)
}

// floatProps renders what a float needs: its width, the compressed range a
// read clamps to, and nf, the <Owner>.<field> name a non-finite read refuses
// as (docs/SPEC-TABLES.md §3.3).
func (g *variableGen) floatProps(f *ir.Field) string {
	lo, hi := "null", "null"
	if f.HasFloatRange {
		lo, hi = jsFloat(f.FMin), jsFloat(f.FMax)
	}
	return fmt.Sprintf("f32: %v, lo: %s, hi: %s, nf: %s", f.Type.Kind == ir.TFloat32, lo, hi, jsStr(g.floatOwner(f)))
}

// floatOwner names a float field <Owner>.<field> for the non_finite_float
// refusal (docs/SPEC-TABLES.md §3.3), the owner being the table, type or union
// that declares it, as the Go leg's floatOwner names it.
func (g *variableGen) floatOwner(f *ir.Field) string { return jsFloatOwner(g.unit, f) }

// jsFloatOwner is floatOwner over a unit, for every form that names one.
func jsFloatOwner(u *ir.Unit, f *ir.Field) string {
	for _, m := range []map[string]*ir.Struct{u.Tables, u.Structs} {
		for _, st := range m {
			if slices.Contains(st.Fields, f) {
				return st.DeclaredName() + "." + f.Name
			}
		}
	}
	for _, m := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range m {
			for i := range un.Variants {
				if un.Variants[i].F == f {
					return un.Name + "." + f.Name
				}
			}
		}
	}
	return f.Name
}

func jsFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "-Infinity"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// intIsBig reports whether an integer-shaped field (an int or a bits(N)) is
// stored as a BigInt: exactly when its wire kind is eight bytes wide, the one
// width a JavaScript number cannot hold whole.
func intIsBig(f *ir.Field) bool { return ir.TableKindWidth(ir.TableWireScalarKind(f)) >= 8 }

// intLit renders an integer literal in the field's storage: a BigInt for an
// eight-byte kind and a number otherwise.
func intLit(v *big.Int, isBig bool) string {
	if isBig {
		return v.String() + "n"
	}
	return v.String()
}

func intDefault(f *ir.Field) string {
	v := big.NewInt(0)
	if f.HasDefault && f.DefInt != nil {
		v = f.DefInt
	}
	return intLit(v, intIsBig(f))
}

// ---------------------------------------------------------------------------
// THE CODEC DESCRIPTORS (variableruntime.go reads them)
// ---------------------------------------------------------------------------

// emitCodecs emits TableVariableCodecs, one descriptor per carried table: its
// name, its constructor, its wire type id (docs/SPEC-TABLES.md §3.1), whether
// it is VARIABLE by declaration (§2.2), its fields in declaration order, and
// the node types a load rooted at it can PLACE — the tables a pointer below it
// targets and the blob kinds below it (decodenodes.go's placeable set).
func (g *variableGen) emitCodecs(closure []*ir.Struct) {
	g.emitUnionDescriptors(closure)
	g.pf("const TableVariableCodecs = {\n")
	for _, st := range closure {
		guards := structGuards(st)
		g.pf("  %s: {\n", st.Name)
		g.pf("    name: %s,\n", jsStr(st.Name))
		g.pf("    make: () => new %s(),\n", st.Name)
		g.pf("    typeId: %s,\n", jsId(ir.TableWireId(st.WireName())))
		g.pf("    variable: %v,\n", g.variable[st.Name])
		layout := ir.RecordLayout(g.unit, st)
		g.pf("    sz: %d, al: %d,\n", layout.Size, layout.Align)
		// a map's generated entry (§2.8): its key's widening is the MAP's one
		// count, never the entry's (internal/tablewire bodyAt)
		g.pf("    entry: %v,\n", st.IsMapEntry())
		g.pf("    fields: [\n")
		for i, f := range st.Fields {
			g.pf("      %s,\n", g.fieldDesc(f, i, guards[f.Name]))
		}
		g.pf("    ],\n")
		var placeable []string
		for _, t := range ir.PointerReachable(st) {
			placeable = append(placeable, jsStr(t.Name))
		}
		sort.Strings(placeable)
		bytesEdge, stringEdge := ir.PointerReachableBlobs(st)
		g.pf("    place: { tables: [%s], str: %v, bytes: %v },\n", strings.Join(placeable, ", "), stringEdge, bytesEdge)
		g.pf("  },\n")
	}
	g.pf("};\n")
	g.pf("TableVariableLink(TableVariableCodecs, TableVariableUnions);\n")
	g.pf("TableVariableFreeze(TableVariableCodecs);\n")
	g.pf("TableVariableFreeze(TableVariableUnions);\n\n")
}

// fieldDesc renders one field's descriptor: its property name, its text key,
// its wire id, its ordinal (the retain path's step, §6.6), the kind its header
// carries, its shape and the JSON shape its text takes (§16.2), and what the
// shape needs. An OPTIONAL ?T (§2.3) is its value's descriptor with `opt`.
func (g *variableGen) fieldDesc(f *ir.Field, ordinal int, guards []guardStep) string {
	return g.fieldDescNamed(f, f.Name, ordinal, guards)
}

// fieldDescNamed is fieldDesc under the property name the storage holds the
// value at: the field's own, or "value" for a union arm's field line, whose
// holder is the arm object { arm, value } (§2.6).
func (g *variableGen) fieldDescNamed(f *ir.Field, prop string, ordinal int, guards []guardStep) string {
	var b strings.Builder
	tail := g.fieldDescTail(f)
	fmt.Fprintf(&b, "{ n: %s, key: %s, id: %s, o: %d, ",
		jsStr(prop), jsStr(ir.TableFieldJsonKey(f)), jsId(ir.TableFieldWireId(f)), ordinal)
	// An integer's descriptor carries its scalar kind among intProps; a second
	// `k` here would be a duplicate key whose value the later one overwrites
	// (eslint no-dupe-keys), so only the other shapes name the field kind here.
	if !strings.HasPrefix(tail, intShapePrefix) {
		fmt.Fprintf(&b, "k: %d, ", ir.TableWireFieldKind(f))
	}
	if f.Type.Optional {
		b.WriteString("opt: true, ")
	}
	if len(guards) > 0 {
		fmt.Fprintf(&b, "g: %s, ", jsGuards(guards))
	}
	b.WriteString(tail)
	return b.String()
}

// intShapePrefix opens an integer field's descriptor tail (fieldDescTail's
// default arm), whose intProps carry the scalar kind `k` themselves.
const intShapePrefix = "s: \"int\", "

// fieldDescTail renders a field descriptor's shape-specific properties, from
// `s:` to the closing brace.
func (g *variableGen) fieldDescTail(f *ir.Field) string {
	var b strings.Builder
	switch {
	case f.IsMap():
		// kind 14 over element kind 13, an array of the generated entry (§2.8)
		key := ir.MapKeyField(f)
		ml := ir.RecordLayout(g.unit, f.MapEntry)
		fmt.Fprintf(&b, "s: \"map\", js: \"o\", t: %s, ks: %v, kb: %d, esz: %d, eal: %d }",
			jsStr(f.MapEntry.Name), key.Type.Kind == ir.TString, key.Type.Size, ml.Size, ml.Align)
	case f.KeyEnum != "":
		// kind 16: (key reference, L, element) per present slot (§2.4, §3.2)
		fmt.Fprintf(&b, "s: \"keyed\", js: \"o\", ek: %d, slots: %s, e: %s }",
			ir.TableWireElemKind(f), keyedSlots(f), g.keyedElemDesc(f))
	case unionOf(f) != nil && f.Array == ir.ArrayNone:
		fmt.Fprintf(&b, "s: \"union\", js: \"o\", un: %s }", jsStr(unionOf(f).Name))
	case f.Array != ir.ArrayNone:
		lsize, lalign := ir.ListElementLayout(g.unit, f)
		fmt.Fprintf(&b, "s: \"arr\", js: \"a\", list: %v, fixed: %v, bound: %d, ek: %d, esz: %d, eal: %d, e: %s }",
			f.Array == ir.ArrayList, f.Array == ir.ArrayFixed, f.ArrayBound, ir.TableWireElemKind(f), lsize, lalign, g.keyedElemDesc(f))
	case f.Type.Pointer && f.Type.Blob() && f.Type.Kind == ir.TString:
		b.WriteString("s: \"sblob\", js: \"s\" }")
	case f.Type.Pointer && f.Type.Blob():
		b.WriteString("s: \"bblob\", js: \"s\" }")
	case f.Type.Pointer:
		fmt.Fprintf(&b, "s: \"ptr\", js: \"o\", t: %s }", jsStr(tableOf(f).Name))
	case enumOf(f) != nil:
		fmt.Fprintf(&b, "s: \"enum\", js: \"s\", vs: %s, def: %s }", enumVariants(enumOf(f)), enumDefault(f))
	case flagsOf(f) != nil:
		fmt.Fprintf(&b, "s: \"flags\", js: \"a\", big: true, kw: 8, lo: null, hi: null, bits: 0, vs: %s, def: %s }",
			flagsVariants(flagsOf(f)), intDefault(f))
	case tableOf(f) != nil:
		fmt.Fprintf(&b, "s: \"tab\", js: \"o\", t: %s }", jsStr(tableOf(f).Name))
	case f.Type.Kind == ir.TString:
		fmt.Fprintf(&b, "s: \"str\", js: \"s\", bound: %d, def: %s }", f.Type.Size, jsStr(string(f.DefBytes)))
	case f.Type.Kind == ir.TWString:
		// kind 33 (docs/SPEC-TABLES.md §3; SPEC.md §4.12): the bound is N CODE
		// UNITS, and the empty text is the default an absent field reads
		fmt.Fprintf(&b, "s: \"wstr\", js: \"s\", bound: %d, def: \"\" }", f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		fmt.Fprintf(&b, "s: \"bytes\", js: \"s\", list: false, bound: %d, ek: %d, def: new Uint8Array(%s) }",
			f.Type.Size, ir.TableKindU8, jsByteList(f.DefBytes))
	case f.Type.Kind == ir.TBool:
		def := "false"
		if f.HasDefault && f.DefBool {
			def = "true"
		}
		fmt.Fprintf(&b, "s: \"bool\", js: \"b\", def: %s }", def)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		fmt.Fprintf(&b, "s: \"float\", js: \"n\", %s, def: %s }", g.floatProps(f), floatDefault(f))
	default:
		fmt.Fprintf(&b, "s: \"int\", js: \"n\", %s, def: %s }", intProps(f), intDefault(f))
	}
	return b.String()
}

// elemDesc renders an array's ELEMENT descriptor: the same facts a field of
// the element's type carries, at its zero (§2.9; tabletext elementZero).
func (g *variableGen) elemDesc(f *ir.Field) string {
	switch {
	case unionOf(f) != nil:
		// an element of an array of unions is an arm header in its place (§3)
		return fmt.Sprintf("{ s: \"union\", js: \"o\", un: %s }", jsStr(unionOf(f).Name))
	case f.Type.Pointer:
		return fmt.Sprintf("{ s: \"ptr\", js: \"o\", t: %s }", jsStr(tableOf(f).Name))
	case enumOf(f) != nil:
		return fmt.Sprintf("{ s: \"enum\", js: \"s\", vs: %s }", enumVariants(enumOf(f)))
	case flagsOf(f) != nil:
		return fmt.Sprintf("{ s: \"flags\", js: \"a\", big: true, kw: 8, lo: null, hi: null, bits: 0, vs: %s }",
			flagsVariants(flagsOf(f)))
	case tableOf(f) != nil:
		return fmt.Sprintf("{ s: \"tab\", js: \"o\", t: %s }", jsStr(tableOf(f).Name))
	case f.Type.Kind == ir.TBool:
		return "{ s: \"bool\", js: \"b\" }"
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return fmt.Sprintf("{ s: \"float\", js: \"n\", %s }", g.floatProps(f))
	}
	return fmt.Sprintf("{ s: \"int\", js: \"n\", %s }", intProps(f))
}

// enumVariants renders an enum's variants as [name, wire id] pairs: identity
// is the variant NAME's hash (docs/SPEC-TABLES.md §5).
func enumVariants(e *ir.Enum) string {
	parts := make([]string, len(e.Variants))
	for i := range e.Variants {
		parts[i] = fmt.Sprintf("[%s, %s]", jsStr(e.Variants[i]), jsId(ir.TableWireId(e.VariantWireName(i))))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// intProps renders what an integer-shaped value needs: its storage (big), its
// kind's width (kw), the declared range a wire read clamps to (lo/hi, §4), the
// bits(N) clamp (bits/bmax, §15), and for the text form the range it clamps to
// first (tlo/thi: the declared range, or bits(N)'s implied one) and the storage
// domain after it (dlo/dhi), the reader's order (tabletext placeInteger).
func intProps(f *ir.Field) string {
	isBig := intIsBig(f)
	kind := ir.TableWireScalarKind(f)
	lo, hi := "null", "null"
	tlo, thi := "null", "null"
	if rlo, rhi, ok := ir.TableRawRange(f); ok {
		lo, hi = intLit(rlo, isBig), intLit(rhi, isBig)
		tlo, thi = rlo.String()+"n", rhi.String()+"n"
	}
	bits, bmax := 0, "0"
	if f.Type.Kind == ir.TBits {
		bits = f.Type.Width
		if bits < 64 {
			m := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
			bmax = intLit(m, isBig)
			if !f.HasIntRange {
				tlo, thi = "0n", m.String()+"n"
			}
		}
	}
	// THE STORAGE DOMAIN (tabletext StorageBytes): an integer's own width, and a
	// bits(N)'s four or eight bytes, unsigned
	storage, signed := f.Type.Width/8, f.Type.Signed
	if f.Type.Kind == ir.TBits {
		storage, signed = 4, false
		if f.Type.Width > 32 {
			storage = 8
		}
	}
	var dlo, dhi *big.Int
	if signed {
		dhi = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(storage*8-1)), big.NewInt(1))
		dlo = new(big.Int).Neg(new(big.Int).Add(dhi, big.NewInt(1)))
	} else {
		dlo = big.NewInt(0)
		dhi = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(storage*8)), big.NewInt(1))
	}
	return fmt.Sprintf("k: %d, big: %v, kw: %d, lo: %s, hi: %s, bits: %d, bmax: %s, frac: %d, tlo: %s, thi: %s, dlo: %sn, dhi: %sn",
		kind, isBig, ir.TableKindWidth(kind), lo, hi, bits, bmax, f.Type.FracBits, tlo, thi, dlo.String(), dhi.String())
}

// ---------------------------------------------------------------------------
// THE ROOT SURFACE
// ---------------------------------------------------------------------------

// emitRootCodec emits a variable root's whole surface: the wire Save/Measure,
// the tolerant Load, the LoadMeasure the oracle fuzzer compares, and the text
// ToJson/FromJson (docs/SPEC-TABLES.md §3, §16). Save answers null and Measure
// -1 for a value the wire cannot carry: a data cycle (§3.1) or an enum value no
// variant names (§5). ToJson answers null for a value the text cannot spell: a
// cycle, a non-finite float, a blob two slots name, a nesting past the depth
// cap (§16.3, §16.7).
func (g *variableGen) emitRootCodec(st *ir.Struct) {
	c := "TableVariableCodecs." + st.Name
	g.pf("export function %sMeasure(v) {\n", st.Name)
	g.pf("  const b = TableVariableSave(%s, v, null);\n", c)
	g.pf("  return b === null ? -1 : b.length;\n")
	g.pf("}\n\n")
	g.pf("export function %sMeasureReason(v) {\n", st.Name)
	g.pf("  return TableVariableMeasureReason(%s, v);\n", c)
	g.pf("}\n\n")
	g.pf("export function %sSave(v) {\n", st.Name)
	g.pf("  return TableVariableSave(%s, v, null);\n", c)
	g.pf("}\n\n")
	g.pf("export function %sLoad(bytes, report) {\n", st.Name)
	g.pf("  return TableVariableLoad(%s, bytes, report, null);\n", c)
	g.pf("}\n\n")
	g.pf("export function %sToJson(v) {\n", st.Name)
	g.pf("  return TableVariableToJson(%s, v);\n", c)
	g.pf("}\n\n")
	g.pf("export function %sFromJson(text, report) {\n", st.Name)
	g.pf("  return TableVariableFromJson(%s, text, report);\n", c)
	g.pf("}\n\n")
	g.emitRootLoadMeasure(st)
}

// emitRootLoadMeasure emits <T>LoadMeasure: the region size a caller owes a
// root, derived from framing alone (docs/SPEC-TABLES.md §6.5) and equal, for a
// whole wire of the card constructs, to internal/tablewire's FileRegionMeasure
// answer. A refusal to frame (unknown form, damaged id table, an early
// terminator, a list count that cannot fit, or a blob past the size cap)
// answers -1. The region a pointer node, a byte buffer or a non-table array
// takes is not measured here: that is the cook's (§7), not this card's.
func (g *variableGen) emitRootLoadMeasure(st *ir.Struct) {
	align := ir.TableRegionAlign(g.unit)
	rootSize := (ir.RecordLayout(g.unit, st).Size + align - 1) & -align
	// A fixed-class root needs no variable region: even on a wire whose framing
	// is damaged or refused before any field decodes, a non-variable read starts
	// at its declared defaults and answers that root (docs/SPEC-TABLES.md §3, §4).
	refused := "-1"
	if !g.variable[st.Name] {
		refused = fmt.Sprintf("%d", rootSize+16)
	}
	g.pf("export function %sLoadMeasure(bytes) {\n", st.Name)
	g.pf("  if (bytes.length < 9) { return %s; }\n", refused)
	g.pf("  if (bytes[0] !== TableVariableForm) { return %s; }\n", refused)
	g.pf("  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);\n")
	g.pf("  const count = dv.getBigUint64(bytes.length - 8, true);\n")
	g.pf("  if (count > BigInt(Math.floor((bytes.length - 9) / 8))) { return %s; }\n", refused)
	g.pf("  const first = bytes.length - 8 - Number(count) * 8;\n")
	g.pf("  const ids = [];\n")
	g.pf("  const seen = new Set();\n")
	g.pf("  for (let i = 0; i < Number(count); i++) {\n")
	g.pf("    const id = dv.getBigUint64(first + i * 8, true);\n")
	g.pf("    if (seen.has(id)) { return %s; }\n", refused)
	g.pf("    seen.add(id);\n")
	g.pf("    ids.push(id);\n")
	g.pf("  }\n")
	g.pf("  {\n")
	g.pf("    const w = new TableVariableReader(bytes, 1, first);\n")
	g.pf("    while (true) {\n")
	g.pf("      const ref = w.leb();\n")
	g.pf("      if (w.bad) { break; }\n")
	g.pf("      if (ref === 0) { if (w.at !== w.end) { return %s; } break; }\n", refused)
	g.pf("      if (ref > ids.length) { break; }\n")
	g.pf("      if (w.at >= w.end) { break; }\n")
	g.pf("      const kind = w.u8();\n")
	g.pf("      if (!TableVariableSkip(w, kind)) { break; }\n")
	g.pf("    }\n")
	g.pf("  }\n")
	g.pf("  const r = new TableVariableReader(bytes, 1, first);\n")
	g.pf("  const at = TableVariableExtent(TableVariableCodecs.%s, r, ids, 0);\n", st.Name)
	g.pf("  if (at < 0) { return %s; }\n", refused)
	g.pf("  let data = TableVariableRound(%d + at, %d);\n", rootSize, align)
	g.pf("  const nr = new TableVariableReader(bytes, 1, first);\n")
	g.pf("  let present = false, bad = false, payload = null;\n")
	g.pf("  while (true) {\n")
	g.pf("    if (nr.at >= nr.end) { break; }\n")
	g.pf("    const ref = nr.leb();\n")
	g.pf("    if (nr.bad || ref === 0 || ref > ids.length) { break; }\n")
	g.pf("    if (nr.at >= nr.end) { break; }\n")
	g.pf("    const id = ids[ref - 1];\n")
	g.pf("    const kind = nr.u8();\n")
	g.pf("    if (id === TableNodeTableId) {\n")
	g.pf("      present = true;\n")
	g.pf("      if (kind !== 12) { bad = true; break; }\n")
	g.pf("      const L = nr.leb();\n")
	g.pf("      if (nr.at + L > nr.end) { bad = true; break; }\n")
	g.pf("      payload = new TableVariableReader(bytes, nr.at, nr.at + L);\n")
	g.pf("      nr.at += L;\n")
	g.pf("      continue;\n")
	g.pf("    }\n")
	g.pf("    if (!TableVariableSkip(nr, kind)) { break; }\n")
	g.pf("  }\n")
	g.pf("  const place = TableVariableCodecs.%s.place;\n", st.Name)
	g.pf("  const byType = place.byType;\n")
	g.pf("  let records = 0;\n")
	g.pf("  if (present && !bad) { payload.leb(); }\n")
	g.pf("  while (!bad && present && payload !== null && payload.at < payload.end) {\n")
	g.pf("    const tref = payload.leb();\n")
	g.pf("    if (tref === 0 || tref > ids.length) { break; }\n")
	g.pf("    const tlen = payload.leb();\n")
	g.pf("    if (payload.at + tlen > payload.end) { break; }\n")
	g.pf("    const tid = ids[tref - 1];\n")
	g.pf("    const cc = byType.get(tid);\n")
	g.pf("    if (cc !== undefined) {\n")
	g.pf("      const br = new TableVariableReader(bytes, payload.at, payload.at + tlen);\n")
	g.pf("      const ba = TableVariableExtent(cc, br, ids, 0);\n")
	g.pf("      if (ba < 0) { return -1; }\n")
	g.pf("      data += TableVariableRound(cc.sz + ba, %d);\n", align)
	g.pf("    } else if (tid === TableStringBlobId && place.str) {\n")
	g.pf("      if (tlen > 0xffffffff) { return -1; }\n")
	g.pf("      data += TableVariableRound(8 + tlen + 1, %d);\n", align)
	g.pf("    } else if (tid === TableBytesBlobId && place.bytes) {\n")
	g.pf("      if (tlen > 0xffffffff) { return -1; }\n")
	g.pf("      data += TableVariableRound(8 + tlen, %d);\n", align)
	g.pf("    }\n")
	g.pf("    payload.at += tlen;\n")
	g.pf("    records++;\n")
	g.pf("  }\n")
	g.pf("  return data + (records + 1) * 16;\n")
	g.pf("}\n\n")
}
