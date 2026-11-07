package jstable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retain.go carries the VARIABLE FORM's RETAIN-UNKNOWN opt-in
// (docs/SPEC-TABLES.md §6.6) in JavaScript: a retained Load, Measure and Save
// with caller-owned retention storage (bytes and ids, never grown on demand)
// and one report whose `retained` and `retainLost` counters accumulate across
// the load and the save. The retained data re-emits in the same canonical order
// the Go and C backends write — the body's declared fields, then the retained
// tail, then the node table — so a retained save is byte-equal across legs.
//
// THE WALK IS A PORT of the reference's resolving walk (§6.6, "RETENTION FORM:
// the field with every reference resolved"): the capture resolves every
// reference to the sixty-four-bit id it names, and the save resolves them back,
// so re-emitting into an id table ordered differently is still correct. The six
// excluded classes §6.6 names — a node-bearing payload, an unknown enum variant
// and the rest — are reported as `retainLost` and never retained silently.
// `unknown`, `retained` and `widened` alone are not loss.

// emitRetainRuntime emits the shared retain runtime and the known-id set the
// save-side interner uses to tell a nameable id from a retained one.
func (g *variableGen) emitRetainRuntime() {
	g.pf("export const TableRetainKnown = Object.freeze([")
	for _, id := range ir.TableWireIds(g.unit) {
		g.pf("%s,", jsId(id))
	}
	g.pf("]);\n\n")
	g.pf("%s", jsRetainRuntime)
}

// emitRetainMember emits the retain SURFACE of one variable root: LoadRetain,
// MeasureRetain and SaveRetain. The plain Load/Measure/Save are unchanged; the
// retained ones run the same walk (variableruntime.go) with the caller's store.
func (g *variableGen) emitRetainMember(st *ir.Struct) {
	c := "TableVariableCodecs." + st.Name
	g.pf("export function %sLoadRetain(bytes, retain, report) {\n", st.Name)
	g.pf("  if (retain === null) { return %sLoad(bytes, report); }\n", st.Name)
	g.pf("  retain.reset();\n")
	g.pf("  return TableVariableLoad(%s, bytes, report, retain);\n", c)
	g.pf("}\n\n")
	// Each walk starts from a clean interner and an unplaced record set, exactly
	// as the reference's save resets idUsed and clears the placed bytes (§6.6):
	// a measure that ran first must not consume the id list the save will need,
	// and a record the measure placed must not mask one the save never emitted.
	g.pf("export function %sMeasureRetain(v, retain) {\n", st.Name)
	g.pf("  if (retain !== null) { retain.idUsed = 0; retain.clearPlaced(); }\n")
	g.pf("  const b = TableVariableSave(%s, v, retain);\n", c)
	g.pf("  return b === null ? -1 : b.length;\n")
	g.pf("}\n\n")
	g.pf("export function %sSaveRetain(v, retain, report) {\n", st.Name)
	g.pf("  if (report === null) { return null; }\n")
	g.pf("  if (retain === null) { return %sSave(v); }\n", st.Name)
	g.pf("  retain.idUsed = 0;\n")
	g.pf("  retain.clearPlaced();\n")
	g.pf("  const b = TableVariableSave(%s, v, retain);\n", c)
	g.pf("  if (b === null) { return null; }\n")
	g.pf("  TableVariableRetainCountLost(retain, report);\n")
	g.pf("  return b;\n")
	g.pf("}\n\n")
}

// jsRetainRuntime is the JavaScript runtime for the opt-in (§6.6). The record
// layout is READER-PRIVATE — nothing compares two ports' buffers — and carries
// what §6.6 requires: the body it belongs to (as a step path), the field's
// identity and bytes with every reference resolved, and a bounded cost.
const jsRetainRuntime = `
// ---- RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6) -----------------------------

// The caller owns both stores; the codec never grows either. 'bytes' is the
// record buffer, 'ids' the retained-id entries (an id and its slot), and
// 'used'/'count'/'idUsed' are what has been consumed of each.
export class TableVariableRetain {
  constructor(bytes, ids) {
    this.bytes = bytes;
    this.ids = ids;
    this.used = 0;
    this.count = 0;
    this.idUsed = 0;
  }
  reset() { this.used = 0; this.count = 0; this.idUsed = 0; }
  // clearPlaced resets every retained record's placed byte to 0, so a walk
  // that does not re-emit a record leaves it countable as a loss (§6.6).
  clearPlaced() {
    let at = 0;
    while (at < this.used) {
      const total = tableRetainU32(this.bytes, at);
      const depth = this.bytes[at + 4];
      this.bytes[at + 5 + depth * 8 + 9] = 0;
      at += total;
    }
  }
}

function tableRetainU32(b, at) { return (b[at] | (b[at + 1] << 8) | (b[at + 2] << 16) | (b[at + 3] << 24)) >>> 0; }
function tableRetainU64(b, at) { let v = 0n; for (let i = 0; i < 8; i++) { v |= BigInt(b[at + i]) << BigInt(8 * i); } return v; }
function tableRetainWidth(kind) {
  switch (kind) {
    case 1: case 2: case 6: case 20: case 25: return 1;
    case 3: case 7: case 21: case 26: return 2;
    case 4: case 8: case 10: case 22: case 27: return 4;
    case 5: case 9: case 11: case 23: case 28: case 36: return 8;
    case 18: case 19: case 24: case 29: return 16;
    default: return 0;
  }
}
function tableRetainNameable(id) {
  let lo = 0, hi = TableRetainKnown.length - 1;
  while (lo <= hi) { const m = (lo + hi) >> 1; if (TableRetainKnown[m] === id) { return true; } if (TableRetainKnown[m] < id) { lo = m + 1; } else { hi = m - 1; } }
  return false;
}
function tableRetainReserved(id) { return id === 0n || id === TableNodeTableId || id === TableBuildVersionId || id === TableMessageVocabularyId; }

// THE RESOLVING WALK, capture half (§6.6): read a field's payload by kind and
// write its RESOLVED form — every reference widened to a 64-bit id, every
// framing length recomputed. Kinds 13 and 14 store a resolved body as
// [u32 len][content]; kind 30 a variant reference as [u64 id]; a scalar and a
// string copy verbatim. Kind 17 (a node index) is an excluded class and returns
// false, as does any framing the walk cannot take.
function TableVariableRetainResolveBody(r, table, out, depth, end) {
  if (depth > 64) { return false; }
  // THE BODY IS ITS OWN CURSOR: a reference or length must not read past the
  // bytes its framing gave it, exactly as the reference's sub-cursor bounds it
  // (internal/tablewire retain.go walkBody).
  const rd = new TableVariableReader(r.b, r.at, end);
  while (true) {
    if (rd.at > end) { return false; }
    const ref = rd.leb(); if (rd.bad) { return false; }
    if (ref === 0) { out.u64(0n); if (rd.at !== end) { return false; } r.at = rd.at; return true; }
    if (ref > table.length || rd.at >= end) { return false; }
    const id = table[ref - 1];
    if (tableRetainReserved(id)) { return false; }
    out.u64(id);
    if (rd.at >= end) { return false; }
    const k = rd.u8(); out.u8(k);
    if (!TableVariableRetainResolve(rd, table, k, out, depth + 1)) { return false; }
    if (rd.at > end) { return false; }
  }
}

// TableVariableRetainResolveContent is the private-format content of one
// framed payload whose length the caller has already consumed, the twin of the
// reference's tableRetainIn.content (internal/codegen/gotable/retain.go):
// kind 13 is a body, 14 and 16 the counted elements of a list and a keyed
// array, 15 and 30 carry a resolved reference, and every other kind copies the
// framed bytes as they stand.
function TableVariableRetainResolveContent(r, table, kind, out, depth, end) {
  if (depth > 64) { return false; }
  const rd = new TableVariableReader(r.b, r.at, end);
  let ok = false;
  switch (kind) {
    case 13: ok = TableVariableRetainResolveBody(rd, table, out, depth, end); break;
    case 14: case 16: ok = TableVariableRetainResolveElements(rd, table, kind, out, depth, end); break;
    case 15: case 30: ok = TableVariableRetainResolve(rd, table, kind, out, depth); break;
    case 17: return false;
    default:
      if (rd.at > end) { return false; }
      out.raw(rd.b.subarray(rd.at, end));
      rd.at = end;
      ok = true;
  }
  if (ok) { r.at = rd.at; }
  return ok;
}

// TableVariableRetainResolveElements is the [element kind][count] header and
// its elements: a kind-14 list element is the element kind's own payload, a
// kind-16 keyed element is a resolved key id, then its length and body
// (§6.6, internal/tablewire retain.go's keyed arm).
function TableVariableRetainResolveElements(r, table, kind, out, depth, end) {
  // the element span is its own cursor, as the reference's framed content is
  const rd = new TableVariableReader(r.b, r.at, end);
  if (rd.at + 1 > end) { return false; }
  const ek = rd.u8(); out.u8(ek);
  const n = rd.leb(); if (rd.bad) { return false; }
  out.leb(n);
  for (let i = 0; i < n; i++) {
    if (kind === 16) {
      const keyRef = rd.leb(); if (rd.bad || keyRef === 0 || keyRef > table.length) { return false; }
      const keyId = table[keyRef - 1];
      if (tableRetainReserved(keyId)) { return false; }
      out.u64(keyId);
      const elen = rd.leb(); if (rd.bad || rd.at + elen > end) { return false; }
      const eend = rd.at + elen;
      const mark = out.len;
      out.u32(0);
      if (!TableVariableRetainResolveContent(rd, table, ek, out, depth + 1, eend)) { return false; }
      if (rd.at !== eend) { return false; }
      const bytes = out.len - mark - 4;
      out.b[mark] = bytes & 0xff; out.b[mark + 1] = (bytes >>> 8) & 0xff; out.b[mark + 2] = (bytes >>> 16) & 0xff; out.b[mark + 3] = (bytes >>> 24) & 0xff;
    } else if (!TableVariableRetainResolve(rd, table, ek, out, depth + 1)) {
      return false;
    }
    if (rd.at > end) { return false; }
  }
  if (rd.at !== end) { return false; }
  r.at = rd.at;
  return true;
}

function TableVariableRetainResolve(r, table, kind, out, depth) {
  if (depth > 64) { return false; }
  const width = tableRetainWidth(kind);
  if (width > 0) { if (r.at + width > r.end) { return false; } out.raw(r.b.subarray(r.at, r.at + width)); r.at += width; return true; }
  switch (kind) {
    case 12: case 31: case 32: case 33: {
      // a string and a wide string (§6.6, internal/tablewire retain.go): the
      // payload copies verbatim under its L
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      out.leb(L); out.raw(r.b.subarray(r.at, r.at + L)); r.at += L; return true;
    }
    case 30: {
      const ref = r.leb(); if (r.bad) { return false; }
      if (ref === 0) { out.u64(0n); return true; }
      if (ref > table.length) { return false; }
      const id = table[ref - 1];
      if (tableRetainReserved(id)) { return false; }
      out.u64(id); return true;
    }
    case 13: {
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      const end = r.at + L;
      const mark = out.len;
      out.u32(0);
      if (!TableVariableRetainResolveBody(r, table, out, depth + 1, end)) { return false; }
      const bytes = out.len - mark - 4;
      out.b[mark] = bytes & 0xff; out.b[mark + 1] = (bytes >>> 8) & 0xff; out.b[mark + 2] = (bytes >>> 16) & 0xff; out.b[mark + 3] = (bytes >>> 24) & 0xff;
      return true;
    }
    case 14: case 16: {
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      const end = r.at + L;
      return TableVariableRetainResolveElements(r, table, kind, out, depth + 1, end);
    }
    case 15: {
      // a union whose arm carries a payload (§6.6): the arm's resolved id, the
      // arm's kind byte, then the arm's framed content.
      const ref = r.leb(); if (r.bad) { return false; }
      if (ref === 0) { out.u64(0n); return true; }
      if (ref > table.length || r.at >= r.end) { return false; }
      const id = table[ref - 1];
      if (tableRetainReserved(id)) { return false; }
      out.u64(id);
      const k = r.u8(); out.u8(k);
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      const end = r.at + L;
      const mark = out.len;
      out.u32(0);
      if (!TableVariableRetainResolveContent(r, table, k, out, depth + 1, end)) { return false; }
      if (r.at !== end) { return false; }
      const bytes = out.len - mark - 4;
      out.b[mark] = bytes & 0xff; out.b[mark + 1] = (bytes >>> 8) & 0xff; out.b[mark + 2] = (bytes >>> 16) & 0xff; out.b[mark + 3] = (bytes >>> 24) & 0xff;
      return true;
    }
    case 17: return false;
    default: return false;
  }
}

// Capture one unknown field (id, kind) whose payload the plain read has already
// skipped (bytes[start:end]). The framing was validated by that skip, so the
// resolving walk here can only fail on inner damage or an excluded class, which
// is one retainLost and never 'malformed' (§6.6).
function TableVariableRetainCapture(bytes, start, end, id, kind, table, retain, path, report) {
  const src = new TableVariableReader(bytes, start, end);
  const out = new TableVariableWriter();
  if (!TableVariableRetainResolve(src, table, kind, out, 0) || src.at !== end) { report.retainLost++; return; }
  const depth = path.length;
  const total = 4 + 1 + 8 * depth + 8 + 1 + 1 + 4 + out.len;
  if (retain.used + total > retain.bytes.length) { report.retainLost++; return; }
  const rec = new TableVariableWriter();
  rec.u32(total); rec.u8(depth);
  for (const s of path) { rec.u32(s.o); rec.u32(s.i); }
  rec.u64(id); rec.u8(kind); rec.u8(0); rec.u32(out.len); rec.raw(out.b);
  retain.bytes.set(rec.b, retain.used);
  retain.used += total;
  retain.count++;
  report.retained++;
}

// THE RESOLVING WALK, save half (§6.6): read a record's resolved payload and
// write its wire form, re-interned into the id table in first-use order.
function TableVariableRetainEmitBody(r, ids, w) {
  while (true) {
    if (r.at + 8 > r.end) { return false; }
    const id = r.u64();
    if (id === 0n) { w.u8(0); return true; }
    if (r.at + 1 > r.end) { return false; }
    const kind = r.u8();
    const ref = ids.ref(id, true);
    if (ref === -1) { return false; }
    w.leb(ref); w.u8(kind);
    if (!TableVariableRetainEmit(r, ids, kind, w)) { return false; }
  }
}

// TableVariableRetainEmitContent is the wire form of one private content whose
// length the caller writes, the twin of tableRetainOut.content: the same six
// shapes the capture half kept (internal/codegen/gotable/retain.go).
function TableVariableRetainEmitContent(r, ids, kind, w) {
  switch (kind) {
    case 13: return TableVariableRetainEmitBody(r, ids, w);
    case 14: case 16: return TableVariableRetainEmitElements(r, ids, kind, w);
    case 15: case 30: return TableVariableRetainEmit(r, ids, kind, w);
    case 17: return false;
    default:
      if (r.at > r.end) { return false; }
      w.raw(r.b.subarray(r.at, r.end));
      r.at = r.end;
      return true;
  }
}

function TableVariableRetainEmitElements(r, ids, kind, w) {
  if (r.at + 1 > r.end) { return false; }
  const ek = r.u8(); w.u8(ek);
  const n = r.leb(); if (r.bad) { return false; }
  w.leb(n);
  for (let i = 0; i < n; i++) {
    if (kind === 16) {
      if (r.at + 8 > r.end) { return false; }
      const keyId = r.u64();
      const keyRef = ids.ref(keyId, true);
      if (keyRef === -1) { return false; }
      w.leb(keyRef);
      const L = r.u32(); if (r.at + L > r.end) { return false; }
      const sub = new TableVariableWriter();
      if (!TableVariableRetainEmitContent(new TableVariableReader(r.b, r.at, r.at + L), ids, ek, sub)) { return false; }
      r.at += L;
      w.leb(sub.len); w.raw(sub.b);
    } else {
      // a list element is the element kind's OWN payload (tableRetainOut's
      // payload(k)): a table carries its length, a scalar only its width.
      if (!TableVariableRetainEmit(r, ids, ek, w)) { return false; }
    }
  }
  return true;
}

function TableVariableRetainEmit(r, ids, kind, w) {
  const width = tableRetainWidth(kind);
  if (width > 0) { if (r.at + width > r.end) { return false; } w.raw(r.b.subarray(r.at, r.at + width)); r.at += width; return true; }
  switch (kind) {
    case 12: case 31: case 32: case 33: {
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      w.leb(L); w.raw(r.b.subarray(r.at, r.at + L)); r.at += L; return true;
    }
    case 30: {
      if (r.at + 8 > r.end) { return false; }
      const id = r.u64();
      // a None variant is the one zero reference (§3): it names no id at all
      if (id === 0n) { w.leb(0); return true; }
      const ref = ids.ref(id, true);
      if (ref === -1) { return false; }
      w.leb(ref); return true;
    }
    case 13: {
      const L = r.u32(); if (r.at + L > r.end) { return false; }
      const sub = new TableVariableWriter();
      if (!TableVariableRetainEmitBody(new TableVariableReader(r.b, r.at, r.at + L), ids, sub)) { return false; }
      r.at += L;
      w.leb(sub.len); w.raw(sub.b); return true;
    }
    case 14: case 16: {
      // the private form of a list carries no outer length: its [element kind]
      // and count are followed by each element's own length and body.
      const sub = new TableVariableWriter();
      if (!TableVariableRetainEmitElements(r, ids, kind, sub)) { return false; }
      w.leb(sub.len); w.raw(sub.b); return true;
    }
    case 15: {
      if (r.at + 8 > r.end) { return false; }
      const id = r.u64();
      // None is the one zero reference and carries no kind byte or length
      if (id === 0n) { w.leb(0); return true; }
      if (r.at + 1 > r.end) { return false; }
      const ref = ids.ref(id, true);
      if (ref === -1) { return false; }
      w.leb(ref);
      const k = r.u8(); w.u8(k);
      const L = r.u32(); if (r.at + L > r.end) { return false; }
      const sub = new TableVariableWriter();
      if (!TableVariableRetainEmitContent(new TableVariableReader(r.b, r.at, r.at + L), ids, k, sub)) { return false; }
      r.at += L;
      w.leb(sub.len); w.raw(sub.b); return true;
    }
    default: return false;
  }
}

// Append the records that belong to this body, in the order retained, before the
// body's terminator (§6.6). A record whose id has no entry is dropped (its
// 'placed' byte stays 0 and CountLost reports it).
function TableVariableRetainTail(w, ids, retain, path) {
  if (retain === null) { return true; }
  let at = 0;
  while (at < retain.used) {
    const b = retain.bytes;
    const total = tableRetainU32(b, at);
    const depth = b[at + 4];
    let match = depth === path.length;
    for (let k = 0; match && k < depth; k++) {
      if (tableRetainU32(b, at + 5 + k * 8) !== path[k].o || tableRetainU32(b, at + 5 + k * 8 + 4) !== path[k].i) { match = false; }
    }
    if (match) {
      const idOff = at + 5 + depth * 8;
      const id = tableRetainU64(b, idOff);
      const kind = b[idOff + 8];
      const plen = tableRetainU32(b, idOff + 10);
      const ref = ids.ref(id, true);
      if (ref === -1) { at += total; continue; }
      w.leb(ref); w.u8(kind);
      if (TableVariableRetainEmit(new TableVariableReader(b, idOff + 14, idOff + 14 + plen), ids, kind, w)) {
        b[idOff + 9] = 1;
      }
    }
    at += total;
  }
  return true;
}

// A record still unplaced after the save lost its body (§6.6): one retainLost
// each, counted once at the end.
// TableVariableRetainDiscard drops the records at or under a path, exactly as
// the reference's TableRetain.discard (§6.6): a body re-read replaces what an
// earlier read retained there, so a duplicate map entry or a re-selected union
// arm does not leave two records behind. The field flag narrows the pass to records
// whose first step under the path is the field's own ordinal.
function TableVariableRetainDiscard(retain, path, field, ordinal) {
  if (retain === null) { return; }
  const depth = path.length;
  let at = 0, out = 0, count = 0;
  while (at < retain.used) {
    const b = retain.bytes;
    const total = tableRetainU32(b, at);
    const rdepth = b[at + 4];
    let drop = false;
    if (rdepth >= depth) {
      let match = true;
      for (let k = 0; k < depth; k++) {
        if (tableRetainU32(b, at + 5 + k * 8) !== path[k].o || tableRetainU32(b, at + 9 + k * 8) !== path[k].i) { match = false; break; }
      }
      if (match && (!field || (rdepth > depth && tableRetainU32(b, at + 5 + depth * 8) === ordinal))) { drop = true; }
    }
    if (!drop) {
      if (out !== at) { b.copyWithin(out, at, at + total); }
      out += total;
      count++;
    }
    at += total;
  }
  retain.used = out;
  retain.count = count;
}

function TableVariableRetainCountLost(retain, report) {
  if (retain === null || report === null) { return; }
  let at = 0;
  while (at < retain.used) {
    const total = tableRetainU32(retain.bytes, at);
    const depth = retain.bytes[at + 4];
    const idOff = at + 5 + depth * 8;
    if (retain.bytes[idOff + 9] === 0) { report.retainLost++; }
    at += total;
  }
}
`
