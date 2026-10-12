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
	g.pf("export const TableRetainKnown = [")
	for _, id := range ir.TableWireIds(g.unit) {
		g.pf("%s,", jsId(id))
	}
	g.pf("];\n\n")
	g.pf("%s", jsRetainRuntime)
}

// emitRetainMember emits the retain SURFACE of one variable root: LoadRetain,
// MeasureRetain and SaveRetain. The plain Load/Measure/Save are unchanged.
func (g *variableGen) emitRetainMember(st *ir.Struct) {
	g.pf("export function %sLoadRetain(bytes, retain, report) {\n", st.Name)
	g.pf("  if (retain === null) { return %sLoad(bytes, report); }\n", st.Name)
	g.pf("  retain.reset();\n")
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
	g.pf("  const records = TableVariableNodeTableScan(bytes, table, entriesStart, report);\n")
	g.pf("  const r = new TableVariableReader(bytes, 1, entriesStart);\n")
	g.pf("  const path = [];\n")
	g.pf("  %sBodyRead(r, ids, table, v, report, pending, records, false, retain, path);\n", st.Name)
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
	g.emitRetainMeasure(st)
	g.emitRetainSave(st)
}

func (g *variableGen) emitRetainMeasure(st *ir.Struct) {
	g.pf("export function %sMeasureRetain(v, retain) {\n", st.Name)
	g.pf("  const w = new TableVariableWriter();\n")
	g.pf("  w.u8(TableVariableForm);\n")
	g.pf("  const ids = new TableVariableIds(retain);\n")
	g.pf("  const nodes = new TableVariableNodes();\n")
	g.pf("  const path = [];\n")
	g.pf("  if (!%sFieldsWrite(w, ids, nodes, v, retain, path, null)) { return -1; }\n", st.Name)
	g.pf("  if (!TableVariableRetainTail(w, ids, retain, path, null)) { return -1; }\n")
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
}

func (g *variableGen) emitRetainSave(st *ir.Struct) {
	g.pf("export function %sSaveRetain(v, retain, report) {\n", st.Name)
	g.pf("  if (report === null) { return null; }\n")
	g.pf("  if (retain === null) { return %sSave(v); }\n", st.Name)
	g.pf("  const n = %sMeasureRetain(v, retain);\n", st.Name)
	g.pf("  if (n < 0) { return null; }\n")
	g.pf("  const w = new TableVariableWriter();\n")
	g.pf("  w.u8(TableVariableForm);\n")
	g.pf("  const ids = new TableVariableIds(retain);\n")
	g.pf("  const nodes = new TableVariableNodes();\n")
	g.pf("  const path = [];\n")
	g.pf("  if (!%sFieldsWrite(w, ids, nodes, v, retain, path, report)) { return null; }\n", st.Name)
	g.pf("  if (!TableVariableRetainTail(w, ids, retain, path, report)) { return null; }\n")
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
	g.pf("  TableVariableRetainCountLost(retain, report);\n")
	g.pf("  return w.toArray();\n")
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
}

function tableRetainU32(b, at) { return (b[at] | (b[at + 1] << 8) | (b[at + 2] << 16) | (b[at + 3] << 24)) >>> 0; }
function tableRetainU64(b, at) { let v = 0n; for (let i = 0; i < 8; i++) { v |= BigInt(b[at + i]) << BigInt(8 * i); } return v; }
function tableRetainWidth(kind) {
  switch (kind) {
    case 1: case 2: case 6: case 20: case 25: return 1;
    case 3: case 7: case 21: case 26: return 2;
    case 4: case 8: case 10: case 22: case 27: return 4;
    case 5: case 9: case 11: case 23: case 28: return 8;
    case 18: case 19: case 24: case 29: return 16;
    default: return 0;
  }
}
function tableRetainNameable(id) {
  let lo = 0, hi = TableRetainKnown.length - 1;
  while (lo <= hi) { const m = (lo + hi) >> 1; if (TableRetainKnown[m] === id) { return true; } if (TableRetainKnown[m] < id) { lo = m + 1; } else { hi = m - 1; } }
  return false;
}
function tableRetainReserved(id) { return id === TableNodeTableId || id === TableBuildVersionId || id === TableMessageVocabularyId; }

// THE RESOLVING WALK, capture half (§6.6): read a field's payload by kind and
// write its RESOLVED form — every reference widened to a 64-bit id, every
// framing length recomputed. Kinds 13 and 14 store a resolved body as
// [u32 len][content]; kind 30 a variant reference as [u64 id]; a scalar and a
// string copy verbatim. Kind 17 (a node index) is an excluded class and returns
// false, as does any framing the walk cannot take.
function TableVariableRetainResolveBody(r, table, out, depth, end) {
  if (depth > 64) { return false; }
  while (true) {
    if (r.at > end) { return false; }
    const ref = r.leb(); if (r.bad) { return false; }
    if (ref === 0) { out.u64(0n); if (r.at !== end) { return false; } return true; }
    if (ref > table.length || r.at >= end) { return false; }
    const id = table[ref - 1];
    if (tableRetainReserved(id)) { return false; }
    out.u64(id);
    if (r.at >= end) { return false; }
    const k = r.u8(); out.u8(k);
    if (!TableVariableRetainResolve(r, table, k, out, depth + 1)) { return false; }
    if (r.at > end) { return false; }
  }
}

function TableVariableRetainResolve(r, table, kind, out, depth) {
  if (depth > 64) { return false; }
  const width = tableRetainWidth(kind);
  if (width > 0) { if (r.at + width > r.end) { return false; } out.raw(r.b.subarray(r.at, r.at + width)); r.at += width; return true; }
  switch (kind) {
    case 12: {
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
    case 14: {
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      const end = r.at + L;
      if (r.at + 1 > end) { return false; }
      const ek = r.u8(); out.u8(ek);
      const n = r.leb(); if (r.bad) { return false; } out.leb(n);
      for (let i = 0; i < n; i++) {
        const elen = r.leb(); if (r.bad || r.at + elen > end) { return false; }
        const eend = r.at + elen;
        const mark = out.len;
        out.u32(0);
        if (!TableVariableRetainResolveBody(r, table, out, depth + 1, eend)) { return false; }
        const bytes = out.len - mark - 4;
        out.b[mark] = bytes & 0xff; out.b[mark + 1] = (bytes >>> 8) & 0xff; out.b[mark + 2] = (bytes >>> 16) & 0xff; out.b[mark + 3] = (bytes >>> 24) & 0xff;
        if (r.at > end) { return false; }
      }
      if (r.at !== end) { return false; }
      return true;
    }
    case 17: return false;
    default: return false;
  }
}

// Capture one unknown field (id, kind) at the reader's payload start into the
// caller's buffer. On success the reader is advanced past the payload and the
// record is committed; on failure the reader is untouched and one retainLost is
// counted, never 'malformed' (§6.6).
function TableVariableRetainCapture(r, id, kind, table, retain, path, report) {
  if (retain === null || retain.bytes === null) { return false; }
  const src = new TableVariableReader(r.b, r.at, r.end);
  const out = new TableVariableWriter();
  if (!TableVariableRetainResolve(src, table, kind, out, 0)) { report.retainLost++; return false; }
  const depth = path.length;
  const total = 4 + 1 + 8 * depth + 8 + 1 + 1 + 4 + out.len;
  if (retain.used + total > retain.bytes.length) { report.retainLost++; return false; }
  const rec = new TableVariableWriter();
  rec.u32(total); rec.u8(depth);
  for (const s of path) { rec.u32(s.o); rec.u32(s.i); }
  rec.u64(id); rec.u8(kind); rec.u8(0); rec.u32(out.len); rec.raw(out.b);
  retain.bytes.set(rec.b, retain.used);
  retain.used += total;
  retain.count++;
  report.retained++;
  r.at = src.at;
  return true;
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

function TableVariableRetainEmit(r, ids, kind, w) {
  const width = tableRetainWidth(kind);
  if (width > 0) { if (r.at + width > r.end) { return false; } w.raw(r.b.subarray(r.at, r.at + width)); r.at += width; return true; }
  switch (kind) {
    case 12: {
      const L = r.leb(); if (r.bad || r.at + L > r.end) { return false; }
      w.leb(L); w.raw(r.b.subarray(r.at, r.at + L)); r.at += L; return true;
    }
    case 30: {
      if (r.at + 8 > r.end) { return false; }
      const id = r.u64();
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
    case 14: {
      if (r.at + 1 > r.end) { return false; }
      const ek = r.u8();
      const n = r.leb(); if (r.bad) { return false; }
      const sub = new TableVariableWriter();
      sub.u8(ek); sub.leb(n);
      for (let i = 0; i < n; i++) {
        const elen = r.u32(); if (r.at + elen > r.end) { return false; }
        const esub = new TableVariableWriter();
        if (!TableVariableRetainEmitBody(new TableVariableReader(r.b, r.at, r.at + elen), ids, esub)) { return false; }
        r.at += elen;
        sub.leb(esub.len); sub.raw(esub.b);
      }
      w.leb(sub.len); w.raw(sub.b); return true;
    }
    default: return false;
  }
}

// Append the records that belong to this body, in the order retained, before the
// body's terminator (§6.6). A record whose id has no entry is dropped (its
// 'placed' byte stays 0 and CountLost reports it).
function TableVariableRetainTail(w, ids, retain, path, report) {
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
