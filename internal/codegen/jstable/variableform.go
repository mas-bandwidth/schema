// THE VARIABLE FORM (docs/SPEC-TABLES.md §3.1), form byte 1, in JavaScript:
// the codec for exactly the card constructs. This file is the WORK of the
// slice js-carries-the-variable-form-for-card-constructs: a JavaScript Load and
// Save that carry a card byte for byte as the Go, C, C++ and C# backends do,
// for the card set — plain `table` roots and tables as list elements; bool,
// int32 and int64 scalars with their defaults and min/max; enums; string(N);
// *string (docs/SPEC-TABLES.md §2.5, the string kind); and unbounded lists of
// tables []T (§2.9). Reading is wire tolerance (§4): unknown ids are skipped
// and counted, absent fields read their defaults, kind mismatch and clamp are
// reported, and identity rides the name hash (§5). Every other variable-class
// construct is refused by name (§11) in fixedform.go's variableFieldRefusal.
//
// THE WALK IS DUPLICATED FROM THE GO REFERENCE (internal/codegen/gotable) ON
// PURPOSE, as this package's wire kinds already are (jstable.go): a port that
// derived the framing from the reference emitter's private helpers would break
// the day the two files disagree, and this way a disagreement shows up in the
// SHARED GOLDEN BYTES instead — card-populated.wire (tables/cards/fuzz).
package jstable

import (
	"fmt"
	"sort"
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
// take the fixed form's surfaces down with it. In such a unit the rest of the
// variable class stays absent and unclaimed.
//
// THE PREDICATE IS THE DECLARATION, NOT THE SERVICE. A plain `table` is the
// VARIABLE wire whatever its fields happen to be (§2.2), so a `?T`, a `[E]T`
// or a union arm it holds is a variable-class construct this leg does not
// carry — but fixedRefusal(st) == "" names a table the fixed form COULD serve
// (a scalar-and-string body, §3.4), not a table the DECLARATION serves, so
// that predicate is true for almost every plain table and would swallow the
// refusal. The predicate scopes on !ir.VariableTables(u)[name], the same
// predicate the Elixir leg holds (internal/codegen/elixirtable/
// variablerefusal.go), so only a unit whose closure declares a `fixed table`
// is the unit the boundary does not bind.
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
// at generation: it refuses, BY NAME, every variable-class construct this
// backend does not carry on the table wire, while the card record's slice
// passes. The rest of the class stays absent and is refused rather than
// mis-encoded or silently dropped.
//
// A construct this backend does not carry is named as `<table>.<field>: <what>`
// (variableFieldRefusal), so the message names the table, the field and the
// construct together, which is what a refusal by name owes (§11).
func refuseCardUnsupportedVariableConstructs(u *ir.Unit) error {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	names := make([]string, 0, len(variable))
	for name := range variable {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			continue
		}
		if refusals := tableVariableRefusals(st); len(refusals) != 0 {
			return fmt.Errorf("js variable form: %s (docs/SPEC-TABLES.md §11)", refusals[0])
		}
	}
	return nil
}

// variableEmit generates the WHOLE variable-form module for a unit: the shared
// runtime, then one class and its Load/Measure/Save per variable root, plus the
// body codecs of every table in the root's closure (the []T list elements).
// Everything lands in ONE self-contained module, <Package>Variable.js, so a
// consumer imports one name and the module never imports another generated file.
func variableEmit(u *ir.Unit) map[string][]byte {
	roots := []*ir.Struct{}
	for _, f := range u.Files {
		for _, st := range variableRoots(u, f.Tables) {
			// THE FILTER IS THE REFUSAL ITSELF (docs/SPEC-TABLES.md §11), as
			// fixedRoots filters on fixedRefusal: a table the card set does not
			// include carries no codec, and the refusal names it in fixedform.go's
			// variableFieldRefusal. It is never mis-encoded and never dropped
			// silently.
			if len(tableVariableRefusals(st)) != 0 {
				continue
			}
			roots = append(roots, st)
		}
	}
	if len(roots) == 0 {
		return map[string][]byte{}
	}
	// the closure, dependency-first, so a list element's codec exists before the
	// root that names it
	seen := map[string]bool{}
	var closure []*ir.Struct
	for _, r := range roots {
		variableCollectTypes(r, seen, &closure)
	}
	g := &variableGen{unit: u}
	g.pf("// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", u.Package)
	g.pf("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	g.pf("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	g.pf("// AGPL-3.0, its output is not.\n")
	g.pf("// package %s — the VARIABLE FORM (docs/SPEC-TABLES.md §3.1), form byte 1.\n", u.Package)
	g.pf("//\n")
	g.pf("// A saved table is a form byte, a root body of fields (id reference, kind,\n")
	g.pf("// payload), and an id table as the trailer (docs/SPEC-TABLES.md §3). A\n")
	g.pf("// *string field rides as a node index into a node table of blob records\n")
	g.pf("// (docs/SPEC-TABLES.md §3.1). Reading is tolerant: an unknown id is skipped\n")
	g.pf("// and counted, an absent field reads its declared default, and a kind\n")
	g.pf("// mismatch is reported (§4).\n")
	g.pf("\n")
	g.emitRuntime()
	for _, st := range closure {
		g.emitTableClass(st)
	}
	for _, st := range closure {
		g.emitBodyCodec(st)
	}
	for _, st := range closure {
		g.emitExtentWalk(st)
	}
	for _, st := range closure {
		g.emitJsonCodec(st)
	}
	for _, r := range roots {
		g.emitRootCodec(r)
	}
	return map[string][]byte{runtimeHome(u) + "Variable.js": []byte(g.body.String())}
}

type variableGen struct {
	unit *ir.Unit
	body strings.Builder
}

func (g *variableGen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// fieldKindName is the wire kind byte a field's own header carries on this form
// (docs/SPEC-TABLES.md §3): a list is kind 14, a pointer (*string) kind 17, an
// enum kind 30, and every other card scalar its own kind.
func fieldKindName(f *ir.Field) int { return ir.TableWireFieldKind(f) }

// enumFields lists the enum-typed fields of a table, in declaration order.
func enumOf(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	if e, ok := f.Type.Ref.(*ir.Enum); ok {
		return e
	}
	return nil
}

// listElement returns the table a []T field's elements are, or nil.
func listElement(f *ir.Field) *ir.Struct {
	if !f.IsList() || f.Type.Kind != ir.TNamed {
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
export class TableVariableReport {
  constructor() {
    this.malformed = false;
    this.refused = 0;
    this.unknown = 0;
    this.kindMismatch = 0;
    this.clamped = 0;
    this.widened = 0;
    this.duplicate = 0;
  }
}

// Ids intern a writer's ids in FIRST-USE order (docs/SPEC-TABLES.md §3), each a
// BigInt, returning the 1-based reference. First-use order follows the write
// order, so byte-identical output against the reference requires the same walk.
class TableVariableIds {
  constructor() { this.list = []; this.index = new Map(); }
  ref(id) {
    const r = this.index.get(id);
    if (r !== undefined) { return r; }
    this.list.push(id);
    const n = this.list.length;
    this.index.set(id, n);
    return n;
  }
}

// THE NODE NUMBERING (docs/SPEC-TABLES.md §3.1): one record per unique blob, in
// first-use order. Two *string fields holding the same text share one record and
// one node index, exactly as the reference's Number interns a shared blob.
class TableVariableNodes {
  constructor() { this.list = []; this.index = new Map(); }
  ref(content) {
    let idx = this.index.get(content);
    if (idx === undefined) {
      this.list.push(TableVariableUtf8Encode(content));
      idx = this.list.length + 1;
      this.index.set(content, idx);
    }
    return idx;
  }
}

// THE WRITER: a growable byte list. Offsets and lengths are LEB128 (§3).
class TableVariableWriter {
  constructor() { this.b = []; }
  get len() { return this.b.length; }
  u8(v) { this.b.push(v & 0xff); }
  leb(v) {
    v = Number(v);
    while (v >= 128) { this.b.push((v & 0x7f) | 0x80); v = Math.floor(v / 128); }
    this.b.push(v & 0x7f);
  }
  raw(bytes) { for (let i = 0; i < bytes.length; i++) { this.b.push(bytes[i]); } }
  u32(v) { this.b.push(v & 0xff, (v >>> 8) & 0xff, (v >>> 16) & 0xff, (v >>> 24) & 0xff); }
  i32(v) { this.u32(v | 0); }
  u64(v) { for (let i = 0; i < 8; i++) { this.b.push(Number(v & 0xffn)); v = v >> 8n; } }
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
  i64() {
    let v = 0n;
    for (let i = 0; i < 8; i++) { v |= BigInt(this.b[this.at + i]) << BigInt(8 * i); }
    this.at += 8;
    return (this.b[this.at - 1] & 0x80) !== 0 ? v - (1n << 64n) : v;
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

// ---- skipping a field the reader cannot name (docs/SPEC-TABLES.md §3) ------

function TableVariableSkip(r, kind, report) {
  switch (kind) {
    case 1: case 2: case 6: case 20: case 25: { if (r.at + 1 > r.end) return false; r.at += 1; return true; }
    case 3: case 7: case 21: case 26: { if (r.at + 2 > r.end) return false; r.at += 2; return true; }
    case 4: case 8: case 10: case 22: case 27: { if (r.at + 4 > r.end) return false; r.at += 4; return true; }
    case 5: case 9: case 11: case 23: case 28: { if (r.at + 8 > r.end) return false; r.at += 8; return true; }
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

// ---- the node-table FIRST PASS (docs/SPEC-TABLES.md §3.1) -----------------

// TableVariableNodeTableScan reads the root body's reserved node-table field
// BEFORE any value decode, so a body that stops on damage still holds the
// pointer slots' records in hand — the numbering is positional and read once,
// never re-derived mid-decode. A record whose type id this reader cannot name
// keeps its index and counts ONE unknown at the record; every pointer naming it
// reads null (§3.1). A *string blob's bytes are validated here too, one
// malformed per bad record. The table is whole or it is nothing: a scan that
// cannot frame the records leaves them empty and every pointer null.
function TableVariableNodeTableScan(bytes, table, end, report) {
  const records = [];
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
      if (kind !== 12) { report.malformed = true; bad = true; break; }
      const L = w.leb();
      if (w.bad || w.at + L > w.end) { bad = true; break; }
      payload = bytes.slice(w.at, w.at + L);
      w.at += L;
      continue;
    }
    if (!TableVariableSkip(w, kind)) { break; }
  }
  if (present && bad) { report.malformed = true; return records; }
  if (!present) { return records; }
  const rec = new TableVariableReader(payload, 0, payload.length);
  const count = rec.leb();
  if (rec.bad) { report.malformed = true; return records; }
  const raw = [];
  let good = true;
  for (let i = 0; i < count && good; i++) {
    const tref = rec.leb();
    if (rec.bad || tref === 0 || tref > table.length) { good = false; break; }
    const tlen = rec.leb();
    if (rec.bad || rec.at + tlen > rec.end) { good = false; break; }
    raw.push({ ref: tref, body: payload.slice(rec.at, rec.at + tlen) });
    rec.at += tlen;
  }
  if (good && rec.at !== rec.end) { good = false; }
  if (!good) { report.malformed = true; return records; }
  for (const r of raw) {
    const tid = table[r.ref - 1];
    if (tid !== TableStringBlobId) { report.unknown++; records.push(null); continue; }
    const dec = TableVariableUtf8Decode(r.body);
    if (!dec.ok) { report.malformed = true; records.push(null); continue; }
    records.push(dec.s);
  }
  return records;
}

// ---- the text form's string escaping (docs/SPEC-TABLES.md §16) -------------

function TableVariableJsonString(s) {
  let out = '"';
  for (const c of s) {
    switch (c) {
      case '"': out += '\\"'; break;
      case '\\': out += '\\\\'; break;
      case '\n': out += '\\n'; break;
      case '\r': out += '\\r'; break;
      case '\t': out += '\\t'; break;
      default: {
        const cp = c.codePointAt(0);
        if (cp < 0x20) { out += '\\u' + cp.toString(16).padStart(4, '0'); }
        else { out += c; }
      }
    }
  }
  return out + '"';
}
`)
}

// ---------------------------------------------------------------------------
// THE STORAGE CLASS
// ---------------------------------------------------------------------------

func (g *variableGen) emitTableClass(st *ir.Struct) {
	g.pf("export class %s {\n  constructor() {\n", st.Name)
	for _, f := range st.Fields {
		g.pf("    this.%s = %s;\n", f.Name, g.defaultExpr(f))
	}
	g.pf("  }\n}\n\n")
}

// defaultExpr is a field's declared default in JavaScript storage.
func (g *variableGen) defaultExpr(f *ir.Field) string {
	if e := enumOf(f); e != nil {
		if f.HasDefault && f.DefVariant != "" {
			return `"` + f.DefVariant + `"`
		}
		return "null"
	}
	if f.Type.Pointer && f.Type.Blob() {
		return "null"
	}
	if f.IsList() {
		return "[]"
	}
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TInt:
		if f.Type.Width == 64 {
			return "0n"
		}
		return "0"
	case ir.TString:
		return `""`
	}
	return "0"
}

// ---------------------------------------------------------------------------
// THE BODY CODEC: one walk shared by a root and a list element
// ---------------------------------------------------------------------------

// emitBodyCodec emits <T>BodyWrite and <T>BodyRead, which carry a table's fields
// as a body (docs/SPEC-TABLES.md §3): fields in declaration order, a zero
// reference the terminator. A root's body is identical; the root codec adds the
// form byte and the id table around it.
func (g *variableGen) emitBodyCodec(st *ir.Struct) {
	g.emitBodyWrite(st)
	g.emitBodyRead(st)
}

func (g *variableGen) emitBodyWrite(st *ir.Struct) {
	g.pf("function %sFieldsWrite(w, ids, nodes, v) {\n", st.Name)
	for _, f := range st.Fields {
		g.emitWriteField(st, f)
	}
	g.pf("  return true;\n}\n\n")
	g.pf("function %sBodyWrite(w, ids, nodes, v) {\n", st.Name)
	g.pf("  if (!%sFieldsWrite(w, ids, nodes, v)) { return false; }\n", st.Name)
	g.pf("  w.u8(0);\n")
	g.pf("  return true;\n}\n\n")
}

// emitWriteField emits the header and payload of ONE field of a body's write
// walk, eliding the field at its default (docs/SPEC-TABLES.md §3, §4).
func (g *variableGen) emitWriteField(st *ir.Struct, f *ir.Field) {
	name := "v." + f.Name
	kind := fieldKindName(f)
	if e := enumOf(f); e != nil {
		g.pf("  if (%s !== null) {\n", name)
		g.pf("    const ref = ids.ref(%s);\n", jsId(ir.TableFieldWireId(f)))
		g.pf("    w.leb(ref); w.u8(30);\n")
		g.pf("    let vref;\n")
		g.pf("    switch (%s) {\n", name)
		for i := range e.Variants {
			g.pf("      case %q: vref = ids.ref(%s); break;\n", e.Variants[i], jsId(ir.TableWireId(e.VariantWireName(i))))
		}
		g.pf("      default: return false;\n")
		g.pf("    }\n")
		g.pf("    w.leb(vref);\n")
		g.pf("  }\n")
		return
	}
	if f.Type.Pointer && f.Type.Blob() {
		g.pf("  if (%s !== null) {\n", name)
		g.pf("    const ref = ids.ref(%s);\n", jsId(ir.TableFieldWireId(f)))
		g.pf("    w.leb(ref); w.u8(17);\n")
		g.pf("    w.leb(nodes.ref(%s));\n", name)
		g.pf("  }\n")
		return
	}
	if el := listElement(f); el != nil {
		g.pf("  if (%s.length > 0) {\n", name)
		g.pf("    const ref = ids.ref(%s);\n", jsId(ir.TableFieldWireId(f)))
		g.pf("    w.leb(ref); w.u8(14);\n")
		g.pf("    const sub = new TableVariableWriter();\n")
		g.pf("    sub.u8(13);\n")
		g.pf("    sub.leb(%s.length);\n", name)
		g.pf("    for (const e of %s) {\n", name)
		g.pf("      const body = new TableVariableWriter();\n")
		g.pf("      if (!%sBodyWrite(body, ids, nodes, e)) { return false; }\n", el.Name)
		g.pf("      sub.leb(body.len);\n")
		g.pf("      sub.raw(body.b);\n")
		g.pf("    }\n")
		g.pf("    w.leb(sub.len);\n")
		g.pf("    w.raw(sub.b);\n")
		g.pf("  }\n")
		return
	}
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("  if (%s !== false) {\n", name)
		g.pf("    const ref = ids.ref(%s);\n", jsId(ir.TableFieldWireId(f)))
		g.pf("    w.leb(ref); w.u8(%d);\n", kind)
		g.pf("    w.u8(%s ? 1 : 0);\n", name)
		g.pf("  }\n")
	case ir.TInt:
		ride := " !== 0"
		if f.Type.Width == 64 {
			ride = " !== 0n"
		}
		g.pf("  if (%s%s) {\n", name, ride)
		g.pf("    const ref = ids.ref(%s);\n", jsId(ir.TableFieldWireId(f)))
		g.pf("    w.leb(ref); w.u8(%d);\n", kind)
		if f.Type.Width == 64 {
			g.pf("    w.u64(BigInt.asUintN(64, %s));\n", name)
		} else {
			g.pf("    w.i32(%s);\n", name)
		}
		g.pf("  }\n")
	case ir.TString:
		g.pf("  if (%s.length > 0) {\n", name)
		g.pf("    const ref = ids.ref(%s);\n", jsId(ir.TableFieldWireId(f)))
		g.pf("    w.leb(ref); w.u8(12);\n")
		g.pf("    const bytes = TableVariableUtf8Encode(%s);\n", name)
		g.pf("    w.leb(bytes.length);\n")
		g.pf("    w.raw(bytes);\n")
		g.pf("  }\n")
	}
}

func (g *variableGen) emitBodyRead(st *ir.Struct) {
	g.pf("function %sBodyRead(r, ids, table, v, report, pending, records, nested) {\n", st.Name)
	g.pf("  while (true) {\n")
	g.pf("    const ref = r.leb();\n")
	g.pf("    if (r.bad) { report.malformed = true; break; }\n")
	g.pf("    if (ref === 0) { break; }\n")
	g.pf("    if (ref > table.length) { report.malformed = true; break; }\n")
	g.pf("    const id = table[ref - 1];\n")
	g.pf("    if (r.at >= r.end) { report.malformed = true; break; }\n")
	g.pf("    const kind = r.u8();\n")
	// THE RESERVED BUILD-VERSION AND VOCABULARY IDS ride in the ANNOUNCEMENT
	// and nowhere else (docs/SPEC-TABLES.md §3.3): a reference that resolves to
	// one in any body but the announcement's own is malformed, the walk stops
	// there and no field after it is read, never an unknown (§3.1).
	g.pf("    if (id === TableBuildVersionId || id === TableMessageVocabularyId) { report.malformed = true; break; }\n")
	g.pf("    let named = false;\n")
	for _, f := range st.Fields {
		g.emitReadField(st, f)
	}
	// the reserved node-table field (docs/SPEC-TABLES.md §3.1): it rides in a
	// VARIABLE root body and nowhere else — a nested body naming it is damage.
	// The records themselves were read by <T>Load's first pass (nodeTableScan),
	// so this walk steps over the field and never counts it unknown.
	g.pf("    if (id === TableNodeTableId) {\n")
	g.pf("      named = true;\n")
	g.pf("      if (nested) { report.malformed = true; break; }\n")
	g.pf("      if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; }\n")
	g.pf("      continue;\n")
	g.pf("    }\n")
	g.pf("    if (!named) {\n")
	g.pf("      report.unknown++;\n")
	g.pf("      if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; }\n")
	g.pf("    }\n")
	g.pf("  }\n")
	g.pf("  return true;\n}\n\n")
}

// emitReadField emits the decode of ONE field of a body's read walk, matching
// the id resolved from the id table against the field's compile-time id
// (docs/SPEC-TABLES.md §3, §4).
func (g *variableGen) emitReadField(st *ir.Struct, f *ir.Field) {
	name := "v." + f.Name
	kind := fieldKindName(f)
	g.pf("    if (id === %s) {\n", jsId(ir.TableFieldWireId(f)))
	g.pf("      named = true;\n")
	if e := enumOf(f); e != nil {
		g.pf("      if (kind !== 30) { report.kindMismatch++; if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; } continue; }\n")
		g.pf("      const vref = r.leb();\n")
		g.pf("      if (r.bad) { report.malformed = true; break; }\n")
		g.pf("      if (vref === 0) { %s = null; }\n", name)
		g.pf("      else if (vref > table.length) { report.malformed = true; break; }\n")
		g.pf("      else {\n")
		g.pf("        const eid = table[vref - 1];\n")
		g.pf("        %s = null;\n", name)
		for i := range e.Variants {
			g.pf("        if (eid === %s) { %s = %q; }\n", jsId(ir.TableWireId(e.VariantWireName(i))), name, e.Variants[i])
		}
		g.pf("        if (%s === null) { report.unknown++; }\n", name)
		g.pf("      }\n")
		g.pf("      continue;\n")
		g.pf("    }\n")
		return
	}
	if f.Type.Pointer && f.Type.Blob() {
		g.pf("      if (kind !== 17) { report.kindMismatch++; if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; } continue; }\n")
		g.pf("      const index = r.leb();\n")
		g.pf("      if (r.bad) { report.malformed = true; break; }\n")
		g.pf("      pending.push([v, %q, index]);\n", f.Name)
		g.pf("      continue;\n")
		g.pf("    }\n")
		return
	}
	if el := listElement(f); el != nil {
		g.pf("      if (kind !== 14) { report.kindMismatch++; if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; } continue; }\n")
		g.pf("      const L = r.leb();\n")
		g.pf("      if (r.bad || r.at + L > r.end) { report.malformed = true; break; }\n")
		g.pf("      const sub = new TableVariableReader(r.b, r.at, r.at + L);\n")
		g.pf("      if (sub.at + 2 > sub.end) { report.malformed = true; r.at += L; continue; }\n")
		g.pf("      const ek = sub.u8();\n")
		g.pf("      const n = sub.leb();\n")
		g.pf("      if (sub.bad) { report.malformed = true; r.at += L; continue; }\n")
		g.pf("      if (ek !== 13) { report.kindMismatch++; r.at += L; continue; }\n")
		g.pf("      %s = [];\n", name)
		g.pf("      for (let i = 0; i < n; i++) {\n")
		g.pf("        if (sub.at >= sub.end) { report.malformed = true; break; }\n")
		g.pf("        const elen = sub.leb();\n")
		g.pf("        if (sub.bad || sub.at + elen > sub.end) { report.malformed = true; break; }\n")
		g.pf("        let e = new %s();\n", el.Name)
		g.pf("        const er = new TableVariableReader(r.b, sub.at, sub.at + elen);\n")
		g.pf("        const pstart = pending.length;\n")
		g.pf("        %sBodyRead(er, ids, table, e, report, pending, records, true);\n", el.Name)
		g.pf("        if (er.at !== er.end) { report.malformed = true; e = new %s(); pending.length = pstart; }\n", el.Name)
		g.pf("        sub.at += elen;\n")
		g.pf("        %s.push(e);\n", name)
		g.pf("      }\n")
		g.pf("      r.at += L;\n")
		g.pf("      continue;\n")
		g.pf("    }\n")
		return
	}
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("      if (kind !== 1) { report.kindMismatch++; if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; } continue; }\n")
		g.pf("      %s = r.u8() !== 0;\n", name)
		g.pf("      continue;\n")
		g.pf("    }\n")
	case ir.TInt:
		// A WIDENED integer (§4): a wire kind below this field's on its own
		// signedness ladder decodes at its own width, sign-extended, and one
		// `widened` counts. Down the ladder or across the two ladders is a kind
		// mismatch, skipped whole.
		base := 2
		if !f.Type.Signed {
			base = 6
		}
		g.pf("      if (kind !== %d) {\n", kind)
		g.pf("        if (kind >= %d && kind < %d) {\n", base, kind)
		g.pf("          report.widened++;\n")
		if f.Type.Width == 64 {
			g.pf("          %s = kind === 4 ? BigInt(r.i32()) : kind === 3 ? BigInt(r.i16()) : BigInt(r.i8());\n", name)
		} else {
			g.pf("          %s = kind === 3 ? r.i16() : r.i8();\n", name)
		}
		g.pf("          continue;\n")
		g.pf("        }\n")
		g.pf("        report.kindMismatch++; if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; } continue;\n")
		g.pf("      }\n")
		if f.Type.Width == 64 {
			g.pf("      %s = r.i64();\n", name)
		} else {
			g.pf("      %s = r.i32();\n", name)
		}
		g.pf("      continue;\n")
		g.pf("    }\n")
	case ir.TString:
		g.pf("      if (kind !== 12) { report.kindMismatch++; if (!TableVariableSkip(r, kind, report)) { report.malformed = true; break; } continue; }\n")
		g.pf("      const L = r.leb();\n")
		g.pf("      if (r.bad || r.at + L > r.end) { report.malformed = true; break; }\n")
		g.pf("      const bytes = r.b.slice(r.at, r.at + L);\n")
		g.pf("      r.at += L;\n")
		g.pf("      const dec = TableVariableUtf8Decode(bytes);\n")
		g.pf("      if (!dec.ok) { report.malformed = true; %s = %s; continue; }\n", name, g.defaultExpr(f))
		g.pf("      if (bytes.length > %d) {\n", f.Type.Size)
		g.pf("        report.clamped++;\n")
		g.pf("        let cut = %d;\n", f.Type.Size)
		g.pf("        while (cut > 0 && (bytes[cut] & 0xc0) === 0x80) { cut--; }\n")
		g.pf("        %s = TableVariableUtf8Decode(bytes.slice(0, cut)).s;\n", name)
		g.pf("      } else { %s = dec.s; }\n", name)
		g.pf("      continue;\n")
		g.pf("    }\n")
	}
}

// ---------------------------------------------------------------------------
// THE TEXT FORM (docs/SPEC-TABLES.md §16): JSON in and out, card constructs only
// ---------------------------------------------------------------------------

// jsonValueExpr is the JavaScript expression that renders a field's JSON value,
// `d` the key's indentation depth. Every field is written, defaults included
// (§16.2: "a text that elides is a text a reader has to know the schema to
// complete").
func (g *variableGen) jsonValueExpr(f *ir.Field, expr, d string) string {
	if e := enumOf(f); e != nil {
		return "(" + expr + " === null ? '\"None\"' : TableVariableJsonString(" + expr + "))"
	}
	if f.Type.Pointer && f.Type.Blob() {
		return "(" + expr + " === null ? \"null\" : TableVariableJsonString(" + expr + "))"
	}
	if el := listElement(f); el != nil {
		return el.Name + "ListJson(" + expr + ", " + d + ")"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "(" + expr + " ? \"true\" : \"false\")"
	case ir.TInt:
		return "String(" + expr + ")"
	case ir.TString:
		return "TableVariableJsonString(" + expr + ")"
	}
	return "\"null\""
}

func (g *variableGen) emitJsonCodec(st *ir.Struct) {
	// <T>JsonValue renders one instance as a JSON object, its closing brace at
	// `depth` indentation levels.
	g.pf("function %sJsonValue(v, depth) {\n", st.Name)
	g.pf("  let s = \"{\";\n")
	// fields, joined by a comma and a newline at the key's indentation
	g.pf("  const ind = \"  \".repeat(depth + 1);\n")
	g.pf("  let first = true;\n")
	for _, f := range st.Fields {
		key := ir.TableFieldJsonKey(f)
		val := g.jsonValueExpr(f, "v."+f.Name, "depth + 1")
		g.pf("  if (!first) { s += \",\"; }\n")
		g.pf("  first = false;\n")
		g.pf("  s += \"\\n\" + ind + %s + \": \" + %s;\n", jsJsonKey(key), val)
	}
	g.pf("  s += \"\\n\" + \"  \".repeat(depth) + \"}\";\n")
	g.pf("  return s;\n")
	g.pf("}\n\n")

	// <T>ListJson renders a []T, its closing bracket at `d` indentation levels.
	g.pf("function %sListJson(list, d) {\n", st.Name)
	g.pf("  if (list.length === 0) { return \"[]\"; }\n")
	g.pf("  let s = \"[\";\n")
	g.pf("  for (let i = 0; i < list.length; i++) {\n")
	g.pf("    if (i > 0) { s += \",\"; }\n")
	g.pf("    s += \"\\n\" + \"  \".repeat(d + 1) + %sJsonValue(list[i], d + 1);\n", st.Name)
	g.pf("  }\n")
	g.pf("  s += \"\\n\" + \"  \".repeat(d) + \"]\";\n")
	g.pf("  return s;\n")
	g.pf("}\n\n")

	// <T>FromObject places a parsed JSON object into a fresh instance, key by
	// key; a key the table cannot name is skipped and counted (§16.2).
	g.pf("function %sFromObject(data, report) {\n", st.Name)
	g.pf("  const v = new %s();\n", st.Name)
	g.pf("  for (const key of Object.keys(data)) {\n")
	g.pf("    const value = data[key];\n")
	for i, f := range st.Fields {
		g.emitFromJsonField(st, f, i == 0)
	}
	g.pf("    else { report.unknown++; }\n")
	g.pf("  }\n")
	g.pf("  return v;\n")
	g.pf("}\n\n")
}

// emitFromJsonField emits the placement of ONE field from a parsed JSON object.
// `first` emits `if`; the rest emit `else if`, so the chain closes on the
// caller's trailing `else { report.unknown++; }`.
func (g *variableGen) emitFromJsonField(st *ir.Struct, f *ir.Field, first bool) {
	key := ir.TableFieldJsonKey(f)
	prefix := "if"
	if !first {
		prefix = "else if"
	}
	g.pf("    %s (key === %s) {\n", prefix, jsString(key))
	name := "v." + f.Name
	if e := enumOf(f); e != nil {
		g.pf("      if (value === null) { %s = null; }\n", name)
		g.pf("      else if (typeof value !== \"string\") { report.kindMismatch++; }\n")
		g.pf("      else { %s = value; }\n", name)
		g.pf("    }\n")
		return
	}
	if f.Type.Pointer && f.Type.Blob() {
		g.pf("      if (value === null) { %s = null; }\n", name)
		g.pf("      else if (typeof value !== \"string\") { report.kindMismatch++; }\n")
		g.pf("      else { %s = value; }\n", name)
		g.pf("    }\n")
		return
	}
	if el := listElement(f); el != nil {
		g.pf("      if (!Array.isArray(value)) { report.kindMismatch++; }\n")
		g.pf("      else { for (const item of value) { %s.push(%sFromObject(item, report)); } }\n", name, el.Name)
		g.pf("    }\n")
		return
	}
	switch f.Type.Kind {
	case ir.TBool:
		g.pf("      if (typeof value !== \"boolean\") { report.kindMismatch++; }\n")
		g.pf("      else { %s = value; }\n", name)
		g.pf("    }\n")
	case ir.TInt:
		g.pf("      if (typeof value !== \"number\") { report.kindMismatch++; }\n")
		if f.Type.Width == 64 {
			g.pf("      else { %s = BigInt(value); }\n", name)
		} else {
			g.pf("      else { %s = value | 0; }\n", name)
		}
		g.pf("    }\n")
	case ir.TString:
		g.pf("      if (typeof value !== \"string\") { report.kindMismatch++; }\n")
		g.pf("      else { %s = value; }\n", name)
		g.pf("    }\n")
	}
}

func jsString(s string) string {
	return fmt.Sprintf("%q", s)
}

// jsJsonKey renders a JSON object key as a JavaScript string literal WITH its
// surrounding quotes (docs/SPEC-TABLES.md §16), so the emitted text is `"key"`.
func jsJsonKey(s string) string {
	return fmt.Sprintf("%q", fmt.Sprintf("%q", s))
}

// emitRootJson emits <T>ToJson and <T>FromJson, the root's text-form surface.
func (g *variableGen) emitRootJson(st *ir.Struct) {
	g.pf("export function %sToJson(v) {\n", st.Name)
	g.pf("  return %sJsonValue(v, 0) + \"\\n\";\n", st.Name)
	g.pf("}\n\n")
	g.pf("export function %sFromJson(text, report) {\n", st.Name)
	g.pf("  let data;\n")
	g.pf("  try { data = JSON.parse(text); } catch (e) { report.malformed = true; return new %s(); }\n", st.Name)
	g.pf("  if (data === null || typeof data !== \"object\") { report.kindMismatch++; return new %s(); }\n", st.Name)
	g.pf("  return %sFromObject(data, report);\n", st.Name)
	g.pf("}\n\n")
}

// emitRootCodec emits a variable root's whole surface: the wire Save/Measure,
// the tolerant Load, the LoadMeasure the oracle fuzzer compares, and the text
// ToJson/FromJson (docs/SPEC-TABLES.md §3, §16).
func (g *variableGen) emitRootCodec(st *ir.Struct) {
	g.emitRootSave(st)
	g.emitRootLoad(st)
	g.emitRootLoadMeasure(st)
	g.emitRootJson(st)
}

// emitRootSave emits <T>Save and <T>Measure: form byte 1, the body, the node
// table field, then the id table in first-use order (docs/SPEC-TABLES.md §3).
func (g *variableGen) emitRootSave(st *ir.Struct) {
	g.pf("export function %sMeasure(v) {\n", st.Name)
	g.pf("  const w = new TableVariableWriter();\n")
	g.pf("  w.u8(TableVariableForm);\n")
	g.pf("  const ids = new TableVariableIds();\n")
	g.pf("  const nodes = new TableVariableNodes();\n")
	g.pf("  if (!%sFieldsWrite(w, ids, nodes, v)) { return -1; }\n", st.Name)
	g.pf("  if (nodes.list.length > 0) {\n")
	g.pf("    const ref = ids.ref(TableNodeTableId);\n")
	g.pf("    w.leb(ref); w.u8(12);\n")
	g.pf("    const sub = new TableVariableWriter();\n")
	g.pf("    sub.leb(nodes.list.length);\n")
	g.pf("    for (const b of nodes.list) {\n")
	g.pf("      const tref = ids.ref(TableStringBlobId);\n")
	g.pf("      sub.leb(tref); sub.leb(b.length); sub.raw(b);\n")
	g.pf("    }\n")
	g.pf("    w.leb(sub.len); w.raw(sub.b);\n")
	g.pf("  }\n")
	g.pf("  w.u8(0);\n")
	g.pf("  for (const id of ids.list) { w.u64(id); }\n")
	g.pf("  w.u64(BigInt(ids.list.length));\n")
	g.pf("  return w.len;\n")
	g.pf("}\n\n")
	g.pf("export function %sSave(v) {\n", st.Name)
	g.pf("  const n = %sMeasure(v);\n", st.Name)
	g.pf("  if (n < 0) { return null; }\n")
	g.pf("  const w = new TableVariableWriter();\n")
	g.pf("  w.u8(TableVariableForm);\n")
	g.pf("  const ids = new TableVariableIds();\n")
	g.pf("  const nodes = new TableVariableNodes();\n")
	g.pf("  if (!%sFieldsWrite(w, ids, nodes, v)) { return null; }\n", st.Name)
	g.pf("  if (nodes.list.length > 0) {\n")
	g.pf("    const ref = ids.ref(TableNodeTableId);\n")
	g.pf("    w.leb(ref); w.u8(12);\n")
	g.pf("    const sub = new TableVariableWriter();\n")
	g.pf("    sub.leb(nodes.list.length);\n")
	g.pf("    for (const b of nodes.list) {\n")
	g.pf("      const tref = ids.ref(TableStringBlobId);\n")
	g.pf("      sub.leb(tref); sub.leb(b.length); sub.raw(b);\n")
	g.pf("    }\n")
	g.pf("    w.leb(sub.len); w.raw(sub.b);\n")
	g.pf("  }\n")
	g.pf("  w.u8(0);\n")
	g.pf("  for (const id of ids.list) { w.u64(id); }\n")
	g.pf("  w.u64(BigInt(ids.list.length));\n")
	g.pf("  return w.toArray();\n")
	g.pf("}\n\n")
}

// emitRootLoad emits <T>Load: the form byte, the id table resolved from the
// trailer, the tolerant body read, then the deferred *string node indices
// resolved against the node records (docs/SPEC-TABLES.md §3, §3.1, §4). The
// refusal and damage verdicts match the engine's decode (internal/tablewire):
// a form byte this reader does not carry is a refusal, and any framing the
// id table or the terminator cannot close is damage, both returning no root.
func (g *variableGen) emitRootLoad(st *ir.Struct) {
	g.pf("export function %sLoad(bytes, report) {\n", st.Name)
	g.pf("  const v = new %s();\n", st.Name)
	g.pf("  if (bytes.length < 1) { report.malformed = true; return null; }\n")
	g.pf("  if (bytes[0] !== TableVariableForm) { report.refused = 1; return null; }\n")
	g.pf("  if (bytes.length < 9) { report.malformed = true; return null; }\n")
	g.pf("  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);\n")
	g.pf("  const count = dv.getBigUint64(bytes.length - 8, true);\n")
	g.pf("  if (count > BigInt(Math.floor((bytes.length - 9) / 8))) { report.malformed = true; return null; }\n")
	g.pf("  const entriesStart = bytes.length - 8 - Number(count) * 8;\n")
	g.pf("  const table = [];\n")
	g.pf("  const seenIds = new Set();\n")
	g.pf("  for (let i = 0; i < Number(count); i++) {\n")
	g.pf("    const id = dv.getBigUint64(entriesStart + i * 8, true);\n")
	g.pf("    if (seenIds.has(id)) { report.malformed = true; return null; }\n")
	g.pf("    seenIds.add(id);\n")
	g.pf("    table.push(id);\n")
	g.pf("  }\n")
	g.pf("  {\n")
	g.pf("    const w = new TableVariableReader(bytes, 1, entriesStart);\n")
	g.pf("    while (true) {\n")
	g.pf("      const ref = w.leb();\n")
	g.pf("      if (w.bad) { break; }\n")
	g.pf("      if (ref === 0) { if (w.at !== w.end) { report.malformed = true; return null; } break; }\n")
	g.pf("      if (ref > table.length) { break; }\n")
	g.pf("      if (w.at >= w.end) { break; }\n")
	g.pf("      const kind = w.u8();\n")
	g.pf("      if (!TableVariableSkip(w, kind)) { break; }\n")
	g.pf("    }\n")
	g.pf("  }\n")
	g.pf("  const ids = new TableVariableIds();\n")
	g.pf("  const pending = [];\n")
	// the node table is read FIRST, before any value decode (docs/SPEC-TABLES.md
	// §3.1): a body that stops on damage still has its pointer slots' records.
	g.pf("  const records = TableVariableNodeTableScan(bytes, table, entriesStart, report);\n")
	g.pf("  const r = new TableVariableReader(bytes, 1, entriesStart);\n")
	g.pf("  %sBodyRead(r, ids, table, v, report, pending, records, false);\n", st.Name)
	g.pf("  for (const p of pending) {\n")
	g.pf("    const [obj, slot, index] = p;\n")
	g.pf("    if (index === 0) { continue; }\n")
	g.pf("    if (index === 1) { report.kindMismatch++; continue; }\n")
	g.pf("    const rec = records[index - 2];\n")
	g.pf("    if (rec === undefined) { report.malformed = true; continue; }\n")
	g.pf("    if (rec === null) { continue; }\n")
	g.pf("    obj[slot] = rec;\n")
	g.pf("  }\n")
	g.pf("  return v;\n")
	g.pf("}\n\n")
}

// emitExtentWalk emits <T>Extent, the region-storage walk a LoadMeasure runs
// before any value decode (docs/SPEC-TABLES.md §6.5, internal/tablewire/
// FileRegionMeasure): each unbounded list of tables adds its live elements'
// storage to the walk's running `at`, aligned as the list is laid. A count that
// cannot fit the framing is the one refusal this walk can name.
func (g *variableGen) emitExtentWalk(st *ir.Struct) {
	g.pf("function %sExtent(r, ids, at) {\n", st.Name)
	g.pf("  while (true) {\n")
	g.pf("    if (r.at >= r.end) { break; }\n")
	g.pf("    const ref = r.leb();\n")
	g.pf("    if (r.bad || ref === 0 || ref > ids.length) { break; }\n")
	g.pf("    if (r.at >= r.end) { break; }\n")
	g.pf("    const id = ids[ref - 1];\n")
	g.pf("    const kind = r.u8();\n")
	g.pf("    let named = false;\n")
	for _, f := range st.Fields {
		if el := listElement(f); el != nil {
			g.emitExtentListField(st, f, el)
		}
	}
	g.pf("    if (!named) {\n")
	g.pf("      if (!TableVariableSkip(r, kind)) { break; }\n")
	g.pf("    }\n")
	g.pf("  }\n")
	g.pf("  return at;\n")
	g.pf("}\n\n")
}

func (g *variableGen) emitExtentListField(st *ir.Struct, f *ir.Field, el *ir.Struct) {
	size, align := ir.ListElementLayout(g.unit, f)
	elemKind := ir.TableWireScalarKind(f)
	g.pf("    if (id === %s) {\n", jsId(ir.TableFieldWireId(f)))
	g.pf("      named = true;\n")
	g.pf("      if (kind !== 14) { if (!TableVariableSkip(r, kind)) { break; } continue; }\n")
	g.pf("      const L = r.leb();\n")
	g.pf("      if (r.at + L > r.end) { break; }\n")
	g.pf("      const sub = new TableVariableReader(r.b, r.at, r.at + L);\n")
	g.pf("      r.at += L;\n")
	g.pf("      if (sub.at + 2 > sub.end) { continue; }\n")
	g.pf("      const ek = sub.u8();\n")
	g.pf("      const n = sub.leb();\n")
	g.pf("      if (ek !== %d) { continue; }\n", elemKind)
	g.pf("      if (n > 0x7fffffff) { return -1; }\n")
	g.pf("      if (n > Math.floor((sub.end - sub.at) / 2)) { return -1; }\n")
	g.pf("      at = TableVariableRound(at, %d);\n", align)
	g.pf("      at += n * %d;\n", size)
	g.pf("      for (let i = 0; i < n; i++) {\n")
	g.pf("        if (sub.at >= sub.end) { break; }\n")
	g.pf("        const elen = sub.leb();\n")
	g.pf("        if (sub.at + elen > sub.end) { break; }\n")
	g.pf("        const er = new TableVariableReader(r.b, sub.at, sub.at + elen);\n")
	g.pf("        sub.at += elen;\n")
	g.pf("        const rr = %sExtent(er, ids, at);\n", el.Name)
	g.pf("        if (rr < 0) { return -1; }\n")
	g.pf("        at = rr;\n")
	g.pf("      }\n")
	g.pf("      continue;\n")
	g.pf("    }\n")
}

// emitRootLoadMeasure emits <T>LoadMeasure: the region size a caller owes a
// root, derived from framing alone (docs/SPEC-TABLES.md §6.5) and equal, for a
// whole wire, to internal/tablewire's FileRegionMeasure answer. A refusal to
// frame (unknown form, damaged id table, an early terminator, a list count that
// cannot fit, or a blob past the size cap) answers -1.
func (g *variableGen) emitRootLoadMeasure(st *ir.Struct) {
	align := ir.TableRegionAlign(g.unit)
	rootSize := (ir.RecordLayout(g.unit, st).Size + align - 1) & -align
	_, strBlob := ir.PointerReachableBlobs(st)
	g.pf("export function %sLoadMeasure(bytes) {\n", st.Name)
	g.pf("  if (bytes.length < 9) { return -1; }\n")
	g.pf("  if (bytes[0] !== TableVariableForm) { return -1; }\n")
	g.pf("  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);\n")
	g.pf("  const count = dv.getBigUint64(bytes.length - 8, true);\n")
	g.pf("  if (count > BigInt(Math.floor((bytes.length - 9) / 8))) { return -1; }\n")
	g.pf("  const first = bytes.length - 8 - Number(count) * 8;\n")
	g.pf("  const ids = [];\n")
	g.pf("  const seen = new Set();\n")
	g.pf("  for (let i = 0; i < Number(count); i++) {\n")
	g.pf("    const id = dv.getBigUint64(first + i * 8, true);\n")
	g.pf("    if (seen.has(id)) { return -1; }\n")
	g.pf("    seen.add(id);\n")
	g.pf("    ids.push(id);\n")
	g.pf("  }\n")
	g.pf("  {\n")
	g.pf("    const w = new TableVariableReader(bytes, 1, first);\n")
	g.pf("    while (true) {\n")
	g.pf("      const ref = w.leb();\n")
	g.pf("      if (w.bad) { break; }\n")
	g.pf("      if (ref === 0) { if (w.at !== w.end) { return -1; } break; }\n")
	g.pf("      if (ref > ids.length) { break; }\n")
	g.pf("      if (w.at >= w.end) { break; }\n")
	g.pf("      const kind = w.u8();\n")
	g.pf("      if (!TableVariableSkip(w, kind)) { break; }\n")
	g.pf("    }\n")
	g.pf("  }\n")
	g.pf("  const r = new TableVariableReader(bytes, 1, first);\n")
	g.pf("  const at = %sExtent(r, ids, 0);\n", st.Name)
	g.pf("  if (at < 0) { return -1; }\n")
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
	g.pf("  let records = 0;\n")
	g.pf("  if (present && !bad) { payload.leb(); }\n")
	g.pf("  while (!bad && present && payload !== null && payload.at < payload.end) {\n")
	g.pf("    const tref = payload.leb();\n")
	g.pf("    if (tref === 0 || tref > ids.length) { break; }\n")
	g.pf("    const tlen = payload.leb();\n")
	g.pf("    if (payload.at + tlen > payload.end) { break; }\n")
	g.pf("    const tid = ids[tref - 1];\n")
	g.pf("    payload.at += tlen;\n")
	if strBlob {
		g.pf("    if (tid === TableStringBlobId) {\n")
		g.pf("      if (tlen > 0xffffffff) { return -1; }\n")
		g.pf("      data += TableVariableRound(8 + tlen + 1, %d);\n", align)
		g.pf("    }\n")
	}
	g.pf("    records++;\n")
	g.pf("  }\n")
	g.pf("  return data + (records + 1) * 16;\n")
	g.pf("}\n\n")
}
