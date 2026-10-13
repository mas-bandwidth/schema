package jstable

// variableruntime.go is the VARIABLE FORM's codec walk in JavaScript
// (docs/SPEC-TABLES.md §3, §3.1, §16), emitted once into every
// <Package>Variable.js. The walk is DRIVEN BY THE UNIT'S CODEC DESCRIPTORS
// (TableVariableCodecs, emitted by variableform.go): one descriptor per table,
// one entry per field naming its id, its kind, its shape and the facts the
// shape needs. Every rule lives once, here, and is a port of the Go reference
// engine — internal/tablewire (encode.go, nodes.go, nodetable.go, decode.go,
// decodenodes.go) for the wire and internal/tabletext (write.go, read.go) for
// the text — which is what the reference bytes in
// TestVariableClassBeyondCardConstructsJavaScript hold it to.
//
// THE SHAPES a field descriptor carries (`s`):
//
//	bool, int, float   a scalar at its kind's width (§3); `int` covers bits(N)
//	str                string(N), kind 12 (§3)
//	bytes              bytes(N), kind 14 over element kind 6 (§2.5, §3)
//	enum               kind 30, the reference to the variant name's id (§3)
//	ptr                *T, kind 17, a node index into the flat node table (§2.1, §3.1)
//	sblob, bblob       *string and *bytes, kind 17 into a blob record (§2.5, §3.1)
//	tab                a table nested by value, kind 13 (§3)
//	arr                []T or [..N]T, kind 14 over its element kind (§2.9, §3)
//
// IDENTITY (§2.5, §3.1). A node is numbered by OBJECT IDENTITY, as the
// reference numbers it: a table instance and a Uint8Array are objects. A
// *string held as a primitive string has no identity, so each slot holding one
// is its own blob record — exactly what the reference's text reader builds,
// one blob per slot — and a String OBJECT is the spelling of a blob two slots
// share. A load hands a *string slot a primitive when one slot names the
// record, and one shared String object when two or more do, so a load-save of
// a shared blob writes it once again.
const jsVariableCodecRuntime = `
// ---------------------------------------------------------------------------
// THE CODEC WALK (docs/SPEC-TABLES.md §3, §3.1), driven by TableVariableCodecs
// ---------------------------------------------------------------------------

const TableVariableScratch = new DataView(new ArrayBuffer(8));

// A FLOAT RIDES AS ITS IEEE-754 BIT PATTERN (§3, internal/tablewire NarrowF32
// and WidenF32): a float32 NaN's payload rides in the top of the double's
// mantissa, by bit surgery rather than through a conversion that would set the
// quiet bit.
function TableVariableNarrowF32(v) {
  const d = TableVariableScratch;
  if (v !== v) {
    d.setFloat64(0, v, true);
    const lo = d.getUint32(0, true), hi = d.getUint32(4, true);
    let payload = (((hi & 0xfffff) << 3) | (lo >>> 29)) & 0x7fffff;
    if (payload === 0) { payload = 0x400000; }
    return ((hi & 0x80000000) | 0x7f800000 | payload) >>> 0;
  }
  d.setFloat32(0, v, true);
  return d.getUint32(0, true);
}

function TableVariableWidenF32(bits) {
  const d = TableVariableScratch;
  if ((bits & 0x7f800000) === 0x7f800000 && (bits & 0x007fffff) !== 0) {
    const payload = bits & 0x007fffff;
    d.setUint32(4, ((bits & 0x80000000) | 0x7ff00000 | (payload >>> 3)) >>> 0, true);
    d.setUint32(0, ((payload & 7) << 29) >>> 0, true);
    return d.getFloat64(0, true);
  }
  d.setUint32(0, bits >>> 0, true);
  return d.getFloat32(0, true);
}

// §4's WIDENING RUNGS: a narrower kind on its own signedness ladder, and f32
// into f64.
function TableVariableWidens(kind, declared) {
  switch (declared) {
    case 3: case 4: case 5: return kind >= 2 && kind < declared;
    case 7: case 8: case 9: return kind >= 6 && kind < declared;
    case 11: return kind === 10;
  }
  return false;
}

function TableVariableSameBytes(a, b) {
  if (a.length !== b.length) { return false; }
  for (let i = 0; i < a.length; i++) { if (a[i] !== b[i]) { return false; } }
  return true;
}

// THE LINK: each descriptor learns its fields by id and by text key, and each
// table reference its codec, once, when the module loads.
function TableVariableLink(codecs) {
  for (const name of Object.keys(codecs)) {
    const c = codecs[name];
    c.byId = new Map();
    c.byKey = new Map();
    for (const fd of c.fields) {
      c.byId.set(fd.id, fd);
      c.byKey.set(fd.key, fd);
      fd.q = TableVariableJsonString(fd.key);
      if (fd.t !== undefined) { fd.c = codecs[fd.t]; }
      if (fd.e !== undefined && fd.e.t !== undefined) { fd.e.c = codecs[fd.e.t]; }
    }
  }
  for (const name of Object.keys(codecs)) {
    const p = codecs[name].place;
    p.byType = new Map();
    for (const t of p.tables) { p.byType.set(codecs[t].typeId, codecs[t]); }
  }
}

// the live count of an array: a bounded array never writes past its bound
function TableVariableCount(fd, val) { return fd.list ? val.length : Math.min(val.length, fd.bound); }

// ---- THE NUMBERING (docs/SPEC-TABLES.md §3.1; internal/tablewire/nodes.go) --
//
// The first-visit order of a depth-first pre-order walk from the root over
// POINTER EDGES, descending through every by-value edge to reach them. Node
// index 1 is the root and record k is index k + 1. A reference to a node whose
// descent is still open is a DATA CYCLE, and the save refuses it.
//
// THE WALK HOLDS ITS OWN STACK. A pointer chain is as deep as it is long, and
// a wire that loads a hundred thousand nodes must save them, or answer null,
// and never throw: the language's call stack is not the bound. Only the
// BY-VALUE closure of one node is walked by recursion (TableVariableEdges),
// and that nesting is the schema's, never the data's.
class TableVariableNodes {
  constructor() { this.list = []; this.index = new Map(); this.slots = new Map(); }
  slot(holder, name) { return this.slots.get(holder).get(name); }
}

// the pointer and blob edges of one node's by-value closure, in the walk's
// order: fields in declaration order, array elements in index order, a nested
// table or a table element descended in place
function TableVariableEdges(c, v, out) {
  for (const fd of c.fields) {
    const val = v[fd.n];
    switch (fd.s) {
      case "sblob": case "bblob":
        if (val !== null) { out.push({ blob: true, fd, holder: v, val }); }
        break;
      case "ptr":
        if (val !== null) { out.push({ blob: false, c: fd.c, val }); }
        break;
      case "tab":
        TableVariableEdges(fd.c, val, out);
        break;
      case "arr": {
        const n = TableVariableCount(fd, val);
        if (fd.e.s === "ptr") {
          for (let i = 0; i < n; i++) { if (val[i] !== null) { out.push({ blob: false, c: fd.e.c, val: val[i] }); } }
        } else if (fd.e.s === "tab") {
          for (let i = 0; i < n; i++) { TableVariableEdges(fd.e.c, val[i], out); }
        }
        break;
      }
    }
  }
  return out;
}

function TableVariableNumber(c, root) {
  const nodes = new TableVariableNodes();
  nodes.index.set(root, 1);
  const open = new Set([root]);
  const stack = [{ v: root, edges: TableVariableEdges(c, root, []), i: 0 }];
  while (stack.length > 0) {
    const top = stack[stack.length - 1];
    if (top.i >= top.edges.length) { open.delete(top.v); stack.pop(); continue; }
    const e = top.edges[top.i++];
    if (e.blob) { TableVariableBlobNode(nodes, e.holder, e.fd, e.val); continue; }
    if (open.has(e.val)) { return null; }
    if (nodes.index.has(e.val)) { continue; }
    nodes.list.push({ typeId: e.c.typeId, codec: e.c, v: e.val });
    nodes.index.set(e.val, nodes.list.length + 1);
    open.add(e.val);
    stack.push({ v: e.val, edges: TableVariableEdges(e.c, e.val, []), i: 0 });
  }
  return nodes;
}

function TableVariableBlobNode(nodes, holder, fd, val) {
  const typeId = fd.s === "sblob" ? TableStringBlobId : TableBytesBlobId;
  if (fd.s === "sblob" && typeof val === "string") {
    let m = nodes.slots.get(holder);
    if (m === undefined) { m = new Map(); nodes.slots.set(holder, m); }
    if (!m.has(fd.n)) {
      nodes.list.push({ typeId, body: TableVariableUtf8Encode(val) });
      m.set(fd.n, nodes.list.length + 1);
    }
    return;
  }
  if (nodes.index.has(val)) { return; }
  nodes.list.push({ typeId, body: fd.s === "sblob" ? TableVariableUtf8Encode(String(val)) : val });
  nodes.index.set(val, nodes.list.length + 1);
}

// ---- THE SAVE WALK (docs/SPEC-TABLES.md §3; internal/tablewire/encode.go) ---

function TableVariableHeader(w, ids, fd, kind) { w.leb(ids.ref(fd.id)); w.u8(kind); }

function TableVariableWriteInt(w, width, v) {
  switch (width) {
    case 1: w.u8(v); break;
    case 2: w.u16(v); break;
    case 4: w.u32(v); break;
    default: w.u64(BigInt.asUintN(64, v));
  }
}

// an enum value's reference: the variant name's id, 0 for None, and -1 for a
// value no variant names, which the save refuses (§5)
function TableVariableEnumRef(e, ids, val) {
  if (val === null) { return 0; }
  for (const [name, id] of e.vs) { if (name === val) { return ids.ref(id); } }
  return -1;
}

function TableVariableWriteFields(c, w, ids, nodes, v, retain, path) {
  for (const fd of c.fields) {
    if (!TableVariableWriteField(fd, w, ids, nodes, v, retain, path)) { return false; }
  }
  return true;
}

// one body: its fields in declaration order, the retained tail (§6.6), then
// the one-byte zero reference that terminates it
function TableVariableWriteBody(c, w, ids, nodes, v, retain, path) {
  if (!TableVariableWriteFields(c, w, ids, nodes, v, retain, path)) { return false; }
  if (!TableVariableRetainTail(w, ids, retain, path, null)) { return false; }
  w.u8(0);
  return true;
}

// ONE FIELD, elided at its declared default (§3): a pointer and a blob at
// null, an array when empty, a nested table when its body is its terminator
// alone — the field's id rolled back with it, so an elided field costs the id
// table nothing either.
function TableVariableWriteField(fd, w, ids, nodes, v, retain, path) {
  const val = v[fd.n];
  switch (fd.s) {
    case "bool":
      if (val !== fd.def) { TableVariableHeader(w, ids, fd, 1); w.u8(val ? 1 : 0); }
      return true;
    case "int":
      if (val !== fd.def) { TableVariableHeader(w, ids, fd, fd.k); TableVariableWriteInt(w, fd.kw, val); }
      return true;
    case "float":
      if (fd.f32 ? Math.fround(val) !== Math.fround(fd.def) : val !== fd.def) {
        TableVariableHeader(w, ids, fd, fd.k);
        if (fd.f32) { w.u32(TableVariableNarrowF32(val)); } else { w.f64(val); }
      }
      return true;
    case "str":
      if (val !== fd.def) {
        const b = TableVariableUtf8Encode(val);
        TableVariableHeader(w, ids, fd, 12);
        w.leb(b.length);
        w.raw(b);
      }
      return true;
    case "bytes":
      if (!TableVariableSameBytes(val, fd.def)) {
        // bytes ride as an array of u8 (§2.5), framed exactly as the reference
        // frames them (internal/tablewire encode.go: L = 2 + the byte count)
        TableVariableHeader(w, ids, fd, 14);
        w.leb(2 + val.length);
        w.u8(6);
        w.leb(val.length);
        w.raw(val);
      }
      return true;
    case "enum":
      if (val !== fd.def) {
        const ref = ids.ref(fd.id);
        const vref = TableVariableEnumRef(fd, ids, val);
        if (vref < 0) { return false; }
        w.leb(ref); w.u8(30); w.leb(vref);
      }
      return true;
    case "ptr":
      if (val !== null) { TableVariableHeader(w, ids, fd, 17); w.leb(nodes.index.get(val)); }
      return true;
    case "sblob": case "bblob":
      if (val !== null) {
        const index = fd.s === "sblob" && typeof val === "string" ? nodes.slot(v, fd.n) : nodes.index.get(val);
        TableVariableHeader(w, ids, fd, 17);
        w.leb(index);
      }
      return true;
    case "tab": {
      const mark = ids.mark();
      const ref = ids.ref(fd.id);
      const body = new TableVariableWriter();
      path.push({ o: fd.o, i: 0 });
      const ok = TableVariableWriteBody(fd.c, body, ids, nodes, val, retain, path);
      path.pop();
      if (!ok) { return false; }
      if (body.len <= 1) { ids.rollback(mark); return true; }
      w.leb(ref); w.u8(13); w.leb(body.len); w.raw(body.b);
      return true;
    }
    case "arr": {
      const n = TableVariableCount(fd, val);
      if (n === 0) { return true; }
      w.leb(ids.ref(fd.id));
      w.u8(14);
      const body = new TableVariableWriter();
      body.u8(fd.ek);
      body.leb(n);
      for (let i = 0; i < n; i++) {
        if (!TableVariableWriteElement(fd, body, ids, nodes, val[i], retain, path, i)) { return false; }
      }
      w.leb(body.len);
      w.raw(body.b);
      return true;
    }
  }
  return false;
}

// one array element at its element kind's own framing (§2.9, §3): a table
// element carries its own length, a pointer its node index (0 is null), an
// enum its variant reference, a scalar its width
function TableVariableWriteElement(fd, w, ids, nodes, val, retain, path, i) {
  const e = fd.e;
  switch (e.s) {
    case "bool": w.u8(val ? 1 : 0); return true;
    case "int": TableVariableWriteInt(w, e.kw, val); return true;
    case "float":
      if (e.f32) { w.u32(TableVariableNarrowF32(val)); } else { w.f64(val); }
      return true;
    case "enum": {
      const vref = TableVariableEnumRef(e, ids, val);
      if (vref < 0) { return false; }
      w.leb(vref);
      return true;
    }
    case "ptr": w.leb(val === null ? 0 : nodes.index.get(val)); return true;
    case "tab": {
      const body = new TableVariableWriter();
      path.push({ o: fd.o, i: i });
      const ok = TableVariableWriteBody(e.c, body, ids, nodes, val, retain, path);
      path.pop();
      if (!ok) { return false; }
      w.leb(body.len);
      w.raw(body.b);
      return true;
    }
  }
  return false;
}

// THE SAVE (docs/SPEC-TABLES.md §3, §3.1): the form byte, the root's fields,
// the retained tail, the node table LAST in the root body under the reserved
// id at kind 12, the terminator, then the id table and its count. A data cycle
// or a value with no wire identity refuses the save (null).
function TableVariableSave(c, v, retain, report) {
  const nodes = TableVariableNumber(c, v);
  if (nodes === null) { return null; }
  const w = new TableVariableWriter();
  w.u8(TableVariableForm);
  const ids = new TableVariableIds(retain);
  const path = [];
  if (!TableVariableWriteFields(c, w, ids, nodes, v, retain, path)) { return null; }
  if (!TableVariableRetainTail(w, ids, retain, path, report)) { return null; }
  if (nodes.list.length > 0) {
    w.leb(ids.ref(TableNodeTableId));
    w.u8(12);
    const sub = new TableVariableWriter();
    sub.leb(nodes.list.length);
    for (const n of nodes.list) {
      sub.leb(ids.ref(n.typeId));
      if (n.codec === undefined) {
        // a BYTE BUFFER's record is the bytes themselves (§2.5)
        sub.leb(n.body.length);
        sub.raw(n.body);
        continue;
      }
      const body = new TableVariableWriter();
      if (!TableVariableWriteBody(n.codec, body, ids, nodes, n.v, null, [])) { return null; }
      sub.leb(body.len);
      sub.raw(body.b);
    }
    w.leb(sub.len);
    w.raw(sub.b);
  }
  w.u8(0);
  for (const id of ids.list) { w.u64(id); }
  w.u64(BigInt(ids.list.length));
  return w.toArray();
}

// ---- THE LOAD WALK (docs/SPEC-TABLES.md §3, §3.1, §4; internal/tablewire
// decode.go and decodenodes.go) ----------------------------------------------

// the form byte, the trailer and the id table, and the root body's own framing
// (an early terminator is damage): null when the wire is refused or damaged
function TableVariableOpen(bytes, report) {
  if (bytes.length < 1) { report.malformed = true; return null; }
  if (bytes[0] !== TableVariableForm) { report.refused = 1; return null; }
  if (bytes.length < 9) { report.malformed = true; return null; }
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const count = dv.getBigUint64(bytes.length - 8, true);
  if (count > BigInt(Math.floor((bytes.length - 9) / 8))) { report.malformed = true; return null; }
  const end = bytes.length - 8 - Number(count) * 8;
  const table = [];
  const seen = new Set();
  for (let i = 0; i < Number(count); i++) {
    const id = dv.getBigUint64(end + i * 8, true);
    if (seen.has(id)) { report.malformed = true; return null; }
    seen.add(id);
    table.push(id);
  }
  const w = new TableVariableReader(bytes, 1, end);
  while (true) {
    const ref = w.leb();
    if (w.bad) { break; }
    if (ref === 0) { if (w.at !== w.end) { report.malformed = true; return null; } break; }
    if (ref > table.length) { break; }
    if (w.at >= w.end) { break; }
    const kind = w.u8();
    if (!TableVariableSkip(w, kind)) { break; }
  }
  return { table, end };
}

// THE LOAD: the node table first — its records' framing, then every record
// this root can place created, then each table record's body decoded into its
// own storage — and the root body last. A pointer slot holds its index until
// every body is read, then resolves against the ONE numbering (§3.1). A record
// whose type id this root cannot place keeps its index and counts one unknown;
// every pointer naming it reads null.
function TableVariableLoad(c, bytes, report, retain) {
  const open = TableVariableOpen(bytes, report);
  if (open === null) { return null; }
  const table = open.table;
  const v = c.make();
  const raw = TableVariableNodeTableScan(bytes, table, open.end, report);
  const st = { good: raw !== null, nodes: [], root: v, rootCodec: c, pending: [] };
  if (raw !== null) {
    const place = c.place;
    for (const rec of raw) {
      if (rec.tid === TableStringBlobId && place.str) {
        // a text blob's content is refused on the terms a kind 12 payload is:
        // the record is malformed, and every slot naming it reads null
        const dec = TableVariableUtf8Decode(rec.body);
        if (!dec.ok) { report.malformed = true; st.nodes.push(null); continue; }
        st.nodes.push({ kind: "s", blob: dec.s, refs: 0, shared: null });
        continue;
      }
      if (rec.tid === TableBytesBlobId && place.bytes) {
        st.nodes.push({ kind: "b", blob: Uint8Array.from(rec.body) });
        continue;
      }
      const nc = place.byType.get(rec.tid);
      if (nc === undefined) {
        report.unknown++;
        if (retain !== null) { report.retainLost++; }
        st.nodes.push(null);
        continue;
      }
      st.nodes.push({ kind: "t", codec: nc, v: nc.make(), body: rec.body });
    }
    for (const n of st.nodes) {
      if (n === null || n.kind !== "t") { continue; }
      // RETENTION INSIDE A NODE RECORD IS NOT KEPT by this leg: an unknown field
      // there counts retainLost (path null), never a record re-emitted elsewhere
      TableVariableBodyRead(n.codec, new TableVariableReader(n.body, 0, n.body.length), table, n.v, report, st, true, retain, null);
    }
  }
  TableVariableBodyRead(c, new TableVariableReader(bytes, 1, open.end), table, v, report, st, false, retain, []);
  TableVariableResolve(st, report);
  return v;
}

// RESOLUTION (§3.1, decodenodes.go resolveCell): 0 is null, 1 is the root
// (checked against the reader's own root type), k + 1 is record k. A record of
// another kind than the slot declares is a kind mismatch, an index past the
// numbering is malformed, and a numbering that could not be read resolves
// nothing. A *string record named by two or more slots lands as ONE String
// object in each, so the save numbers it once again.
function TableVariableResolve(st, report) {
  const strings = new Map();
  for (const [holder, key, index, d] of st.pending) {
    let value = null, str = null;
    if (st.good && index !== 0) {
      if (index === 1) {
        if (d.s === "ptr" && d.c === st.rootCodec) { value = st.root; } else { report.kindMismatch++; }
      } else if (index - 2 >= st.nodes.length) {
        report.malformed = true;
      } else {
        const n = st.nodes[index - 2];
        if (n !== null) {
          if (d.s === "sblob") {
            if (n.kind === "s") { str = n; } else { report.kindMismatch++; }
          } else if (d.s === "bblob") {
            if (n.kind === "b") { value = n.blob; } else { report.kindMismatch++; }
          } else if (n.kind === "t" && n.codec === d.c) {
            value = n.v;
          } else {
            report.kindMismatch++;
          }
        }
      }
    }
    holder[key] = value;
    if (d.s === "sblob") {
      let m = strings.get(holder);
      if (m === undefined) { m = new Map(); strings.set(holder, m); }
      if (str === null) { m.delete(key); } else { m.set(key, str); }
    }
  }
  for (const m of strings.values()) { for (const n of m.values()) { n.refs++; } }
  for (const [holder, m] of strings) {
    for (const [key, n] of m) {
      if (n.refs > 1) {
        if (n.shared === null) { n.shared = new String(n.blob); }
        holder[key] = n.shared;
      } else {
        holder[key] = n.blob;
      }
    }
  }
}

// ONE BODY (§3, §4): fields by id in any order, a field this table cannot name
// skipped by its kind and counted unknown (and retained when the caller opted
// in, §6.6), a kind that widens decoded at its own width and counted widened,
// any other kind a kind mismatch skipped whole. Damage stops the body.
function TableVariableBodyRead(c, r, table, v, report, st, nested, retain, path) {
  for (;;) {
    const ref = r.leb();
    if (r.bad) { report.malformed = true; return false; }
    if (ref === 0) { return true; }
    if (ref > table.length) { report.malformed = true; return false; }
    if (r.at >= r.end) { report.malformed = true; return false; }
    const kind = r.u8();
    const id = table[ref - 1];
    // THE RESERVED BUILD-VERSION AND VOCABULARY IDS ride in the announcement
    // and nowhere else (§3.3): in any body they are malformed
    if (id === TableBuildVersionId || id === TableMessageVocabularyId) { report.malformed = true; return false; }
    if (id === TableNodeTableId) {
      // the node table rides in a VARIABLE root body and nowhere else (§3.1):
      // a nested body naming it is damage; the root steps over it, the records
      // having been read first
      if (nested) { report.malformed = true; return false; }
      if (c.variable) {
        if (!TableVariableSkip(r, kind)) { report.malformed = true; return false; }
        continue;
      }
    }
    const fd = c.byId.get(id);
    if (fd === undefined) {
      report.unknown++;
      const from = r.at;
      if (!TableVariableSkip(r, kind)) { report.malformed = true; return false; }
      if (retain !== null) {
        if (path === null) { report.retainLost++; }
        else { TableVariableRetainCapture(r.b, from, r.at, id, kind, table, retain, path, report); }
      }
      continue;
    }
    if (kind !== fd.k) {
      if (TableVariableWidens(kind, fd.k)) {
        const x = TableVariableScalar(r, fd, kind, report);
        if (x === undefined) { return false; }
        v[fd.n] = x;
        report.widened++;
        continue;
      }
      // the escape kind 31 and the payload-free kind 32 at a named position take
      // this same rule and no other (§3)
      report.kindMismatch++;
      if (!TableVariableSkip(r, kind)) { report.malformed = true; return false; }
      continue;
    }
    if (!TableVariableReadField(fd, r, table, v, report, st, retain, path)) { return false; }
  }
}

// a scalar decoded at the WIRE kind (the declared kind or one it widens),
// sign-extended where the kind is signed, then the declared range and the
// bits(N) clamp (§4, decode.go scalarAt); undefined is a short payload
function TableVariableScalar(r, d, kind, report) {
  const width = tableRetainWidth(kind);
  if (r.at + width > r.end) { report.malformed = true; return undefined; }
  let x;
  switch (kind) {
    case 1: return r.u8() !== 0;
    case 10: return TableVariableWidenF32(r.u32());
    case 11: return r.f64();
    case 2: x = r.i8(); break;
    case 3: x = r.i16(); break;
    case 4: x = r.i32(); break;
    case 5: x = r.i64(); break;
    case 6: x = r.u8(); break;
    case 7: x = r.u16(); break;
    case 8: x = r.u32(); break;
    case 9: x = r.u64(); break;
    default: report.malformed = true; return undefined;
  }
  if (d.big && typeof x !== "bigint") { x = BigInt(x); }
  if (d.lo !== null) {
    if (x < d.lo) { x = d.lo; report.clamped++; }
    else if (x > d.hi) { x = d.hi; report.clamped++; }
  }
  if (d.bits > 0 && d.bits < width * 8 && x > d.bmax) { x = d.bmax; report.clamped++; }
  return x;
}

// an enum reference (§3): 0 is None, a reference past the id table is damage
// (undefined), and a variant this enum cannot name reads None and counts unknown
function TableVariableEnumRead(e, r, table, report, retain) {
  const ref = r.leb();
  if (r.bad) { report.malformed = true; return undefined; }
  if (ref === 0) { return null; }
  if (ref > table.length) { report.malformed = true; return undefined; }
  const id = table[ref - 1];
  for (const [name, vid] of e.vs) { if (vid === id) { return name; } }
  report.unknown++;
  if (retain !== null) { report.retainLost++; }
  return null;
}

function TableVariableReadField(fd, r, table, v, report, st, retain, path) {
  switch (fd.s) {
    case "bool": case "int": case "float": {
      const x = TableVariableScalar(r, fd, fd.k, report);
      if (x === undefined) { return false; }
      v[fd.n] = x;
      return true;
    }
    case "str": {
      const L = r.leb();
      if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
      const bytes = r.b.subarray(r.at, r.at + L);
      r.at += L;
      const dec = TableVariableUtf8Decode(bytes);
      // ILL-FORMED TEXT IS DAMAGE (§3, §4): the field reads its default
      if (!dec.ok) { report.malformed = true; v[fd.n] = fd.def; return true; }
      if (bytes.length > fd.bound) {
        // A CLAMP CUTS AT A CODE POINT BOUNDARY (§3)
        report.clamped++;
        let cut = fd.bound;
        while (cut > 0 && (bytes[cut] & 0xc0) === 0x80) { cut--; }
        v[fd.n] = TableVariableUtf8Decode(bytes.subarray(0, cut)).s;
      } else {
        v[fd.n] = dec.s;
      }
      return true;
    }
    case "enum": {
      const x = TableVariableEnumRead(fd, r, table, report, retain);
      if (x === undefined) { return false; }
      v[fd.n] = x;
      return true;
    }
    case "ptr": case "sblob": case "bblob": {
      // a pointer's payload is a NUMBER: bounds-checked at resolution, never
      // followed (§3.1)
      const index = r.leb();
      if (r.bad) { report.malformed = true; return false; }
      st.pending.push([v, fd.n, index, fd]);
      return true;
    }
    case "tab": {
      const L = r.leb();
      if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
      const sub = new TableVariableReader(r.b, r.at, r.at + L);
      const inst = fd.c.make();
      v[fd.n] = inst;
      if (path !== null) { path.push({ o: fd.o, i: 0 }); }
      TableVariableBodyRead(fd.c, sub, table, inst, report, st, true, retain, path);
      if (path !== null) { path.pop(); }
      // a body whose terminator is not the last byte of its L is damage: the
      // field reads its declared defaults and the parent continues past L
      if (sub.at !== sub.end) { report.malformed = true; v[fd.n] = fd.c.make(); }
      r.at += L;
      return true;
    }
    case "bytes": case "arr":
      return TableVariableReadArray(fd, r, table, v, report, st, retain, path);
  }
  return false;
}

// AN ARRAY BODY (§2.9, §3, decode.go arrayBody): a body shorter than its own
// header is inert; a damaged count is malformed and keeps the field; an element
// kind that is not this array's (and does not widen) is a kind mismatch and
// keeps the field; a bounded array's excess is clamped; elements are bounded by
// the body and a count the body cannot cover keeps the decoded prefix.
function TableVariableReadArray(fd, r, table, v, report, st, retain, path) {
  const L = r.leb();
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  const end = r.at + L;
  if (L >= 2) {
    const sub = new TableVariableReader(r.b, r.at, end);
    const ek = sub.u8();
    const count = sub.leb();
    if (sub.bad) { report.malformed = true; r.at = end; return true; }
    if (ek !== fd.ek) {
      if (!TableVariableWidens(ek, fd.ek)) { report.kindMismatch++; r.at = end; return true; }
      report.widened++;
    }
    let keep = count;
    if (fd.list) {
      if (count > 0x7fffffff) { v[fd.n] = []; report.malformed = true; r.at = end; return true; }
    } else if (count > fd.bound) {
      keep = fd.bound;
      report.clamped++;
    }
    if (fd.s === "bytes") {
      const out = [];
      for (let i = 0; i < keep; i++) {
        if (sub.at >= sub.end) { report.malformed = true; break; }
        out.push(sub.u8());
      }
      v[fd.n] = new Uint8Array(out);
    } else {
      const out = [];
      for (let i = 0; i < keep; i++) {
        if (!TableVariableReadElement(fd, sub, table, out, report, st, retain, path, ek)) { break; }
      }
      v[fd.n] = out;
    }
  }
  r.at = end;
  return true;
}

function TableVariableReadElement(fd, r, table, out, report, st, retain, path, ek) {
  const e = fd.e, i = out.length;
  switch (e.s) {
    case "ptr": {
      const index = r.leb();
      if (r.bad) { report.malformed = true; return false; }
      out.push(null);
      st.pending.push([out, i, index, e]);
      return true;
    }
    case "enum": {
      const x = TableVariableEnumRead(e, r, table, report, retain);
      if (x === undefined) { return false; }
      out.push(x);
      return true;
    }
    case "tab": {
      const L = r.leb();
      if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
      const sub = new TableVariableReader(r.b, r.at, r.at + L);
      let inst = e.c.make();
      if (path !== null) { path.push({ o: fd.o, i: i }); }
      TableVariableBodyRead(e.c, sub, table, inst, report, st, true, retain, path);
      if (path !== null) { path.pop(); }
      if (sub.at !== sub.end) { report.malformed = true; inst = e.c.make(); }
      r.at += L;
      out.push(inst);
      return true;
    }
  }
  const x = TableVariableScalar(r, e, ek, report);
  if (x === undefined) { return false; }
  out.push(x);
  return true;
}
`

// jsVariableTextRuntime is the TEXT FORM (docs/SPEC-TABLES.md §16), a port of
// internal/tabletext's write.go and read.go: every field in declaration order,
// defaults included, two-space indent, a float at the shortest precision that
// reads back at its own width, bytes as base64, and a node two slots name
// defined once with `&node` and referenced by its label after that (§16.7).
const jsVariableTextRuntime = `
// ---------------------------------------------------------------------------
// THE TEXT FORM (docs/SPEC-TABLES.md §16)
// ---------------------------------------------------------------------------

const TableVariableBase64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function TableVariableBase64Text(data) {
  let out = "\"";
  let i = 0;
  for (; data.length - i >= 3; i += 3) {
    const t = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
    out += TableVariableBase64[(t >> 18) & 63] + TableVariableBase64[(t >> 12) & 63] + TableVariableBase64[(t >> 6) & 63] + TableVariableBase64[t & 63];
  }
  if (i < data.length) {
    const left = data.length - i;
    let t = data[i] << 16;
    if (left === 2) { t |= data[i + 1] << 8; }
    out += TableVariableBase64[(t >> 18) & 63] + TableVariableBase64[(t >> 12) & 63];
    out += left === 2 ? TableVariableBase64[(t >> 6) & 63] : "=";
    out += "=";
  }
  return out + "\"";
}

// C's %.*g for a finite double (tabletext formatG): the exponent form when the
// decimal exponent is below -4 or at least the precision, trailing zeros and a
// trailing point removed, an exponent of at least two digits
function TableVariableFormatG(v, prec) {
  const neg = v < 0 || Object.is(v, -0);
  const a = Math.abs(v);
  const sci = a.toExponential(prec - 1);
  const at = sci.indexOf("e");
  const exp = parseInt(sci.slice(at + 1), 10);
  let s;
  if (exp < -4 || exp >= prec) {
    let m = sci.slice(0, at);
    if (m.indexOf(".") >= 0) { m = m.replace(/0+$/, "").replace(/\.$/, ""); }
    const e = Math.abs(exp);
    s = m + "e" + (exp < 0 ? "-" : "+") + (e < 10 ? "0" + e : String(e));
  } else {
    s = a.toFixed(prec - 1 - exp);
    if (s.indexOf(".") >= 0) { s = s.replace(/0+$/, "").replace(/\.$/, ""); }
  }
  return (neg ? "-" : "") + s;
}

// the SHORTEST precision that reads back as the same value at the field's own
// width; a non-finite value has no JSON spelling and the writer refuses (null)
function TableVariableFloatText(v, single) {
  if (!Number.isFinite(v)) { return null; }
  const low = single ? 6 : 15, high = single ? 9 : 17;
  for (let digits = low; ; digits++) {
    const text = TableVariableFormatG(v, digits);
    if (digits >= high) { return text; }
    const back = Number(text);
    if (single ? Math.fround(back) === v : back === v) { return text; }
  }
}

// PASS ONE of the write (§16.7): every slot naming each node counted, so a
// node's first occurrence knows whether it will be named again; a reference to
// a node whose descent is still open is a cycle, refused as the wire refuses
// it. A blob object two slots name is counted too: it has no spelling. Like the
// numbering, the walk holds its own stack: a chain longer than the depth cap is
// counted here and refused by the write (null), never a thrown stack overflow.
function TableVariableTextCount(c, root, g) {
  const open = new Set([root]);
  const stack = [{ v: root, edges: TableVariableEdges(c, root, []), i: 0 }];
  while (stack.length > 0) {
    const top = stack[stack.length - 1];
    if (top.i >= top.edges.length) { open.delete(top.v); stack.pop(); continue; }
    const e = top.edges[top.i++];
    if (e.blob) {
      if (typeof e.val === "object") { g.blobs.set(e.val, (g.blobs.get(e.val) || 0) + 1); }
      continue;
    }
    const n = (g.count.get(e.val) || 0) + 1;
    g.count.set(e.val, n);
    if (open.has(e.val)) { return false; }
    if (n > 1) { continue; }
    open.add(e.val);
    stack.push({ v: e.val, edges: TableVariableEdges(e.c, e.val, []), i: 0 });
  }
  return true;
}

function TableVariableToJson(c, v) {
  const g = { count: new Map(), labels: new Map(), next: 0, blobs: new Map() };
  if (!TableVariableTextCount(c, v, g)) { return null; }
  const out = [];
  if (!TableVariableJsonValue(c, v, 0, g, out)) { return null; }
  return out.join("") + "\n";
}

function TableVariableJsonValue(c, v, depth, g, out) {
  // the write path carries the read path's cap of 128: a pointer chain nests as
  // deep as it is long (§16.7)
  if (depth > 128) { return false; }
  const state = { any: false };
  if (!TableVariableJsonFields(c, v, depth, g, out, state)) { return false; }
  if (!state.any) { out.push("{}"); return true; }
  out.push("\n" + "  ".repeat(depth) + "}");
  return true;
}

function TableVariableJsonFields(c, v, depth, g, out, state) {
  const ind = "\n" + "  ".repeat(depth + 1);
  for (const fd of c.fields) {
    out.push(state.any ? "," : "{");
    state.any = true;
    out.push(ind + fd.q + ": ");
    if (!TableVariableJsonField(fd, v[fd.n], depth + 1, g, out)) { return false; }
  }
  return true;
}

// a node named once is its object in place; a node named more than once is
// DEFINED at its first occurrence, "&node" first, and REFERENCED by "&node"
// alone after that, labels from 1 in first-write order (§16.7)
function TableVariableJsonPointer(c, node, depth, g, out) {
  if (node === null) { out.push("null"); return true; }
  if ((g.count.get(node) || 0) <= 1) { return TableVariableJsonValue(c, node, depth, g, out); }
  if (depth > 128) { return false; }
  const label = g.labels.get(node);
  if (label !== undefined) {
    out.push("{\n" + "  ".repeat(depth + 1) + "\"&node\": " + label + "\n" + "  ".repeat(depth) + "}");
    return true;
  }
  g.next++;
  g.labels.set(node, g.next);
  out.push("{\n" + "  ".repeat(depth + 1) + "\"&node\": " + g.next);
  const before = out.length;
  if (!TableVariableJsonFields(c, node, depth, g, out, { any: true })) { return false; }
  // a definition carries at least one field: a label alone is a reference
  if (out.length === before) { return false; }
  out.push("\n" + "  ".repeat(depth) + "}");
  return true;
}

function TableVariableJsonField(fd, val, depth, g, out) {
  switch (fd.s) {
    case "sblob": case "bblob":
      if (val === null) { out.push("null"); return true; }
      // a blob's text is a string, which has no first key to carry "&node": a
      // blob two slots name has no spelling, and the writer refuses (§16.7)
      if (typeof val === "object" && (g.blobs.get(val) || 0) > 1) { return false; }
      out.push(fd.s === "sblob" ? TableVariableJsonString(String(val)) : TableVariableBase64Text(val));
      return true;
    case "str": out.push(TableVariableJsonString(val)); return true;
    case "bytes": out.push(TableVariableBase64Text(val)); return true;
    case "arr": {
      const n = TableVariableCount(fd, val);
      if (n === 0) { out.push("[]"); return true; }
      out.push("[");
      const ind = "\n" + "  ".repeat(depth + 1);
      for (let i = 0; i < n; i++) {
        if (i > 0) { out.push(","); }
        out.push(ind);
        if (!TableVariableJsonScalar(fd.e, val[i], depth + 1, g, out)) { return false; }
      }
      out.push("\n" + "  ".repeat(depth) + "]");
      return true;
    }
  }
  return TableVariableJsonScalar(fd, val, depth, g, out);
}

function TableVariableJsonScalar(d, val, depth, g, out) {
  switch (d.s) {
    case "ptr": return TableVariableJsonPointer(d.c, val, depth, g, out);
    case "tab": return TableVariableJsonValue(d.c, val, depth, g, out);
    case "enum":
      if (val === null) { out.push("\"None\""); return true; }
      for (const [name] of d.vs) { if (name === val) { out.push(TableVariableJsonString(name)); return true; } }
      return false;
    case "bool": out.push(val ? "true" : "false"); return true;
    case "float": {
      const t = TableVariableFloatText(val, d.f32);
      if (t === null) { return false; }
      out.push(t);
      return true;
    }
    case "int": out.push(String(val)); return true;
  }
  return false;
}

// ---- the READ half (tabletext read.go) --------------------------------------

class TableVariableTextIn {
  constructor(text) { this.t = text; this.p = 0; this.bad = false; }
  // whitespace, and comments: accepted on read, never written (§16.2)
  space() {
    const t = this.t;
    while (this.p < t.length) {
      const c = t.charCodeAt(this.p);
      if (c === 32 || c === 9 || c === 10 || c === 13) { this.p++; continue; }
      if (c === 47 && t.charCodeAt(this.p + 1) === 47) {
        this.p += 2;
        while (this.p < t.length && t.charCodeAt(this.p) !== 10) { this.p++; }
        continue;
      }
      if (c === 47 && t.charCodeAt(this.p + 1) === 42) {
        const end = t.indexOf("*/", this.p + 2);
        if (end < 0) { this.bad = true; this.p = t.length; return; }
        this.p = end + 2;
        continue;
      }
      if (c === 47) { this.bad = true; }
      return;
    }
  }
  peek() { this.space(); return this.p < this.t.length ? this.t[this.p] : ""; }
  shape() {
    switch (this.peek()) {
      case "{": return "o";
      case "[": return "a";
      case "\"": return "s";
      case "t": case "f": return "b";
      case "n": return "z";
      case "": return "";
    }
    return "n";
  }
  literal(word) {
    if (this.t.startsWith(word, this.p)) { this.p += word.length; return true; }
    this.bad = true;
    return false;
  }
  hex4() {
    const s = this.t.substr(this.p, 4);
    if (!/^[0-9a-fA-F]{4}$/.test(s)) { return -1; }
    this.p += 4;
    return parseInt(s, 16);
  }
  str() {
    if (this.peek() !== "\"") { this.bad = true; return null; }
    this.p++;
    const t = this.t;
    let out = "";
    for (;;) {
      if (this.p >= t.length) { this.bad = true; return null; }
      const c = t.charCodeAt(this.p);
      if (c === 34) { this.p++; return out; }
      if (c === 92) {
        const e = t[this.p + 1];
        this.p += 2;
        switch (e) {
          case "\"": out += "\""; break;
          case "\\": out += "\\"; break;
          case "/": out += "/"; break;
          case "b": out += "\b"; break;
          case "f": out += "\f"; break;
          case "n": out += "\n"; break;
          case "r": out += "\r"; break;
          case "t": out += "\t"; break;
          case "u": {
            let code = this.hex4();
            if (code < 0) { this.bad = true; return null; }
            if (code >= 0xd800 && code <= 0xdbff && t[this.p] === "\\" && t[this.p + 1] === "u") {
              const mark = this.p;
              this.p += 2;
              const low = this.hex4();
              if (low >= 0xdc00 && low <= 0xdfff) { code = 0x10000 + ((code - 0xd800) << 10) + (low - 0xdc00); }
              else { this.p = mark; }
            }
            // a surrogate half that never found its partner reads as U+FFFD (§16.3)
            if (code >= 0xd800 && code <= 0xdfff) { code = 0xfffd; }
            out += String.fromCodePoint(code);
            break;
          }
          default: this.bad = true; return null;
        }
        continue;
      }
      if (c < 0x20) { this.bad = true; return null; }
      if (c >= 0xd800 && c <= 0xdbff) {
        const lo = t.charCodeAt(this.p + 1);
        if (lo >= 0xdc00 && lo <= 0xdfff) { out += t[this.p] + t[this.p + 1]; this.p += 2; continue; }
      }
      if (c >= 0xd800 && c <= 0xdfff) { out += "�"; this.p++; continue; }
      out += t[this.p];
      this.p++;
    }
  }
  // ONE RFC 8259 number, and whether it was integral in FORM
  number() {
    this.space();
    const t = this.t, start = this.p;
    const digit = (ch) => ch >= "0" && ch <= "9";
    let integral = true;
    if (t[this.p] === "-") { this.p++; }
    if (this.p >= t.length) { return null; }
    if (t[this.p] === "0") { this.p++; }
    else if (t[this.p] >= "1" && t[this.p] <= "9") { while (digit(t[this.p])) { this.p++; } }
    else { return null; }
    if (t[this.p] === ".") {
      this.p++;
      if (!digit(t[this.p])) { return null; }
      while (digit(t[this.p])) { this.p++; }
      integral = false;
    }
    if (t[this.p] === "e" || t[this.p] === "E") {
      this.p++;
      if (t[this.p] === "-" || t[this.p] === "+") { this.p++; }
      if (!digit(t[this.p])) { return null; }
      while (digit(t[this.p])) { this.p++; }
      integral = false;
    }
    const n = this.p - start;
    if (n <= 0 || n > 511) { return null; }
    return { tok: t.slice(start, this.p), integral };
  }
  // the label: a positive integer spelled as one, no sign, fraction, exponent
  // or leading zero (§16.7)
  label() {
    this.space();
    const t = this.t;
    if (this.p >= t.length || t[this.p] < "1" || t[this.p] > "9") { this.bad = true; return null; }
    let v = 0n;
    while (this.p < t.length && t[this.p] >= "0" && t[this.p] <= "9") {
      v = v * 10n + BigInt(t.charCodeAt(this.p) - 48);
      if (v > 0xffffffffffffffffn) { this.bad = true; return null; }
      this.p++;
    }
    return v;
  }
  skip(depth, graph) {
    switch (this.peek()) {
      case "{": return this.skipContainer("}", depth, graph);
      case "[": return this.skipContainer("]", depth, graph);
      case "\"": return this.str() !== null;
      case "t": return this.literal("true");
      case "f": return this.literal("false");
      case "n": return this.literal("null");
      case "": this.bad = true; return false;
    }
    if (this.number() === null) { this.bad = true; return false; }
    return true;
  }
  // a skipped OBJECT may still be a pointer's: an "&node" opening it names a
  // node the storage could not hold, and the label still exists (§16.7)
  skipContainer(close, depth, graph) {
    if (depth > 128) { this.bad = true; return false; }
    this.p++;
    let first = true;
    for (;;) {
      let ch = this.peek();
      if (ch === close) { this.p++; return true; }
      if (ch === "") { this.bad = true; return false; }
      if (close === "}") {
        const key = this.str();
        if (key === null) { return false; }
        if (this.peek() !== ":") { this.bad = true; return false; }
        this.p++;
        if (key.length > 0 && key[0] === "&") {
          if (!first || key !== "&node") { this.bad = true; return false; }
          const label = this.label();
          if (label === null) { return false; }
          if (!graph.has(label)) { graph.set(label, { node: null, c: null, open: false }); }
          first = false;
          ch = this.peek();
          if (ch === ",") { this.p++; continue; }
          if (ch === close) { this.p++; return true; }
          this.bad = true;
          return false;
        }
      }
      first = false;
      if (!this.skip(depth + 1, graph)) { return false; }
      ch = this.peek();
      if (ch === ",") { this.p++; continue; }
      if (ch === close) { this.p++; return true; }
      this.bad = true;
      return false;
    }
  }
}

// ONE TEXT: the root's object and nothing after it. The instance holds what
// was placed before any stop; a text that is not JSON is malformed.
function TableVariableFromJson(c, text, report) {
  const inp = new TableVariableTextIn(text);
  const v = c.make();
  const ok = TableVariableReadTable(c, inp, v, 0, new Map(), report);
  if (ok) {
    inp.space();
    if (inp.p !== text.length) { inp.bad = true; }
  }
  if (inp.bad || !ok) { report.malformed = true; }
  return v;
}

function TableVariableReadTable(c, inp, v, depth, graph, report) {
  if (depth > 128) { inp.bad = true; return false; }
  if (inp.peek() !== "{") { inp.bad = true; return false; }
  inp.p++;
  return TableVariableReadKeys(c, inp, v, depth, null, graph, report);
}

// keys are field keys: an unknown one is skipped and counted, a repeated one is
// last-wins and counted, null puts a pointer back at null, and a value of the
// wrong JSON shape is skipped and counted, never coerced (§16.2)
function TableVariableReadKeys(c, inp, v, depth, firstKey, graph, report) {
  const seen = new Set();
  for (;;) {
    let key;
    if (firstKey !== null) {
      key = firstKey;
      firstKey = null;
    } else {
      const ch = inp.peek();
      if (ch === "}") { inp.p++; return true; }
      if (ch === "") { inp.bad = true; return false; }
      key = inp.str();
      if (key === null) { return false; }
      if (inp.peek() !== ":") { inp.bad = true; return false; }
      inp.p++;
    }
    // THE AMPERSAND PREFIX IS RESERVED TO THE FORM (§16.7): "&node" opens a
    // pointer's object and nothing else
    if (key.length > 0 && key[0] === "&") { inp.bad = true; return false; }
    const fd = c.byKey.get(key);
    if (fd === undefined) {
      report.unknown++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else {
      if (seen.has(fd)) { report.duplicate++; }
      seen.add(fd);
      const shape = inp.shape();
      if (shape === "z" && (fd.s === "ptr" || fd.s === "sblob" || fd.s === "bblob")) {
        if (!inp.literal("null")) { return false; }
        v[fd.n] = null;
      } else if (shape !== fd.js) {
        report.kindMismatch++;
        if (!inp.skip(depth + 1, graph)) { return false; }
      } else if (!TableVariableTextField(fd, inp, v, depth, graph, report)) {
        return false;
      }
    }
    const ch = inp.peek();
    if (ch === ",") { inp.p++; continue; }
    if (ch === "}") { inp.p++; return true; }
    inp.bad = true;
    return false;
  }
}

// a base64 body six bits at a time (read.go readBase64): '=' anywhere is
// padding, a character outside the alphabet makes the body the wrong shape
// (one kind mismatch), and past the bound the excess is clamped
function TableVariableBase64Read(inp, bound, report) {
  if (inp.peek() !== "\"") { inp.bad = true; return null; }
  inp.p++;
  const t = inp.t;
  const out = [];
  let acc = 0, held = 0, clamped = false, bad = false;
  for (;;) {
    if (inp.p >= t.length) { inp.bad = true; return null; }
    const ch = t[inp.p++];
    if (ch === "\"") { break; }
    if (ch === "=" || bad) { continue; }
    const at = TableVariableBase64.indexOf(ch);
    if (at < 0) { bad = true; continue; }
    acc = ((acc << 6) | at) & 0xffffff;
    held += 6;
    if (held >= 8) {
      held -= 8;
      if (bound < 0 || out.length < bound) { out.push((acc >> held) & 0xff); } else { clamped = true; }
    }
  }
  if (bad) { report.kindMismatch++; return { bad: true, bytes: new Uint8Array(0) }; }
  if (clamped) { report.clamped++; }
  return { bad: false, bytes: new Uint8Array(out) };
}

function TableVariableTextField(fd, inp, v, depth, graph, report) {
  switch (fd.s) {
    case "sblob": {
      const s = inp.str();
      if (s === null) { return false; }
      v[fd.n] = s;
      return true;
    }
    case "bblob": {
      const b = TableVariableBase64Read(inp, -1, report);
      if (b === null) { return false; }
      // not base64: counted, the reference left null
      v[fd.n] = b.bad ? null : b.bytes;
      return true;
    }
    case "str": {
      const s = inp.str();
      if (s === null) { return false; }
      const b = TableVariableUtf8Encode(s);
      if (b.length > fd.bound) {
        // A CLAMP IS A PREFIX, cut between code points (§16.2)
        let cut = fd.bound;
        while (cut > 0 && (b[cut] & 0xc0) === 0x80) { cut--; }
        v[fd.n] = TableVariableUtf8Decode(b.slice(0, cut)).s;
        report.clamped++;
      } else {
        v[fd.n] = s;
      }
      return true;
    }
    case "bytes": {
      const b = TableVariableBase64Read(inp, fd.bound, report);
      if (b === null) { return false; }
      v[fd.n] = b.bytes;
      return true;
    }
    case "arr": return TableVariableTextArray(fd, inp, v, depth, graph, report);
  }
  return TableVariableTextScalar(fd, inp, v, fd.n, depth, graph, report);
}

function TableVariableZero(e) {
  switch (e.s) {
    case "tab": return e.c.make();
    case "bool": return false;
    case "int": return e.big ? 0n : 0;
    case "float": return 0;
  }
  return null;
}

// an array re-established before placing; an unbounded one reads every
// element, a bounded one keeps its prefix and counts the excess clamped; a
// null element of an array of pointers is a null slot (§16.2)
function TableVariableTextArray(fd, inp, v, depth, graph, report) {
  if (inp.peek() !== "[") { inp.bad = true; return false; }
  inp.p++;
  const out = [];
  v[fd.n] = out;
  const bound = fd.list ? 0x7fffffff : fd.bound;
  const e = fd.e;
  for (;;) {
    let ch = inp.peek();
    if (ch === "]") { inp.p++; return true; }
    if (ch === "") { inp.bad = true; return false; }
    const shape = inp.shape();
    if (out.length >= bound) {
      report.clamped++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else if (e.s === "ptr" && shape === "z") {
      if (!inp.literal("null")) { return false; }
      out.push(null);
    } else if (shape !== e.js) {
      report.kindMismatch++;
      if (!inp.skip(depth + 1, graph)) { return false; }
      out.push(TableVariableZero(e));
    } else {
      out.push(TableVariableZero(e));
      if (!TableVariableTextScalar(e, inp, out, out.length - 1, depth + 1, graph, report)) { return false; }
    }
    ch = inp.peek();
    if (ch === ",") { inp.p++; continue; }
    if (ch === "]") { inp.p++; return true; }
    inp.bad = true;
    return false;
  }
}

// a pointer's object (§16.7): its FIRST key decides what it is — "&node" naming
// a label not yet defined, with fields after it, DEFINES it; "&node" naming one
// already defined, alone, REFERS to it; any other key is a node named once.
// A reference inside its own definition is the cycle the wire refuses.
function TableVariableTextPointer(d, inp, holder, key, depth, graph, report) {
  if (depth + 1 > 128) { inp.bad = true; return false; }
  if (inp.peek() !== "{") { inp.bad = true; return false; }
  inp.p++;
  let ch = inp.peek();
  if (ch === "}") { inp.p++; holder[key] = d.c.make(); return true; }
  if (ch === "") { inp.bad = true; return false; }
  const k = inp.str();
  if (k === null) { return false; }
  if (inp.peek() !== ":") { inp.bad = true; return false; }
  inp.p++;
  if (k !== "&node") {
    const node = d.c.make();
    holder[key] = node;
    return TableVariableReadKeys(d.c, inp, node, depth + 1, k, graph, report);
  }
  const label = inp.label();
  if (label === null) { return false; }
  const entry = graph.get(label);
  ch = inp.peek();
  if (ch === ",") { inp.p++; ch = inp.peek(); }
  const bare = ch === "}";
  if (bare !== (entry !== undefined)) { inp.bad = true; return false; }
  if (bare) {
    inp.p++;
    if (entry.open) { inp.bad = true; return false; }
    holder[key] = null;
    if (entry.c === null) { return true; }
    if (entry.c !== d.c) { report.kindMismatch++; return true; }
    holder[key] = entry.node;
    return true;
  }
  const node = d.c.make();
  holder[key] = node;
  graph.set(label, { node: null, c: null, open: true });
  if (!TableVariableReadKeys(d.c, inp, node, depth + 1, null, graph, report)) { return false; }
  graph.set(label, { node, c: d.c, open: false });
  return true;
}

// one value at one cell: a nested object, a pointer, an enum name, a bool or a
// number
function TableVariableTextScalar(d, inp, holder, key, depth, graph, report) {
  switch (d.s) {
    case "ptr": return TableVariableTextPointer(d, inp, holder, key, depth, graph, report);
    case "tab": {
      const inst = d.c.make();
      holder[key] = inst;
      return TableVariableReadTable(d.c, inp, inst, depth + 1, graph, report);
    }
    case "enum": {
      const s = inp.str();
      if (s === null) { return false; }
      if (s === "None") { holder[key] = null; return true; }
      for (const [name] of d.vs) { if (name === s) { holder[key] = name; return true; } }
      // a name this build cannot name reads None and counts unknown (§4)
      holder[key] = null;
      report.unknown++;
      return true;
    }
    case "bool":
      if (inp.peek() === "t") {
        if (!inp.literal("true")) { return false; }
        holder[key] = true;
        return true;
      }
      if (!inp.literal("false")) { return false; }
      holder[key] = false;
      return true;
    case "float": {
      const n = inp.number();
      if (n === null) { inp.bad = true; return false; }
      let x = Number(n.tok);
      // a magnitude the format cannot hold is the wrong shape for the kind
      if (!Number.isFinite(x)) { report.kindMismatch++; return true; }
      if (d.f32) {
        x = Math.fround(x);
        if (!Number.isFinite(x)) { report.kindMismatch++; return true; }
      }
      holder[key] = x;
      return true;
    }
    case "int": return TableVariableTextInt(d, inp, holder, key, report);
  }
  inp.bad = true;
  return false;
}

// an integer field takes any token whose VALUE is integral (2, 2.0, 1e3); a
// fractional value is the wrong shape. The declared range clamps first, then
// the storage domain, the wire's order (§4, read.go placeInteger).
function TableVariableTextInt(d, inp, holder, key, report) {
  const n = inp.number();
  if (n === null) { inp.bad = true; return false; }
  let neg = false, mag = 0n;
  if (n.integral) {
    let t = n.tok;
    if (t[0] === "-") { neg = true; t = t.slice(1); }
    mag = BigInt(t);
    if (mag > 0xffffffffffffffffn) { mag = 0xffffffffffffffffn; report.clamped++; }
  } else {
    const x = Number(n.tok);
    if (!Number.isFinite(x)) { report.kindMismatch++; return true; }
    neg = x < 0 || Object.is(x, -0);
    const a = Math.abs(x);
    if (a >= 18446744073709551616) {
      mag = 0xffffffffffffffffn;
      report.clamped++;
    } else {
      if (Math.trunc(a) !== a) { report.kindMismatch++; return true; }
      mag = BigInt(a);
    }
  }
  let x = neg ? -mag : mag;
  if (d.tlo !== null) {
    if (x < d.tlo) { x = d.tlo; report.clamped++; }
    else if (x > d.thi) { x = d.thi; report.clamped++; }
  }
  if (x < d.dlo) { x = d.dlo; report.clamped++; }
  else if (x > d.dhi) { x = d.dhi; report.clamped++; }
  holder[key] = d.big ? x : Number(x);
  return true;
}
`
