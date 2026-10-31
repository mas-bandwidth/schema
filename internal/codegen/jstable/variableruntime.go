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
// record, and one shared String object when two or more do — the tolerant
// read keeps the one node — and a save of that value refuses it, blob_shared:
// every writer refuses a blob two slots name (§2.5).
const jsVariableCodecRuntime = `
// ---------------------------------------------------------------------------
// THE CODEC WALK (docs/SPEC-TABLES.md §3, §3.1), driven by TableVariableCodecs
// ---------------------------------------------------------------------------

const TableVariableScratch = new DataView(new ArrayBuffer(8));

// A TABLE FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, the finite rule of
// 2026-10-27, the packet wire's rule): every writer refuses a NaN or an
// infinity, and every read refuses one on its RAW BITS, before the
// bits become a number, as non_finite_float naming <Table>.<field>. A finite
// value rides as its bit pattern, -0 and the subnormals exact, so the float32
// widens and narrows by the ordinary conversion.
//
// TableVariableFiniteFloat is the writers' test: a float32 field's value must
// stay finite at float32 too, so a double past the float32 range is refused
// rather than written as an infinity.
function TableVariableFiniteFloat(f32, v) {
  return f32 ? Number.isFinite(Math.fround(v)) : Number.isFinite(v);
}

// TableVariableSameFloat is ELISION'S compare (§3, the 10-15 rule): the bit
// patterns, so a -0 under a +0 default rides. For two finite values Object.is
// is exactly the bit compare, at float32 once both are rounded to it.
function TableVariableSameFloat(f32, a, b) {
  return f32 ? Object.is(Math.fround(a), Math.fround(b)) : Object.is(a, b);
}

// the write that meets a non-finite float refuses the save, and names the
// reason to a MeasureReason walk (TableVariableSaveWhy) when one is asking
let TableVariableSaveWhy = null;
function TableVariableRefuseNonFinite() {
  if (TableVariableSaveWhy !== null && TableVariableSaveWhy.reason === null) { TableVariableSaveWhy.reason = "non_finite_float"; }
  return false;
}

// a float's payload at its kind's width; false (refused) for a non-finite one
function TableVariableWriteFloat(w, f32, v) {
  if (!TableVariableFiniteFloat(f32, v)) { return TableVariableRefuseNonFinite(); }
  if (f32) { w.f32(v); } else { w.f64(v); }
  return true;
}

// the raw-bits test on an unknown entry a read is retaining (§3.3, §6.6): kind
// 10's four bytes and kind 11's eight, little-endian; true for any other kind
function TableVariableRetainedFinite(b, from, to, kind) {
  if (kind === 10 && to - from === 4) { return ((b[from + 3] << 24 | b[from + 2] << 16) & 0x7f800000) !== 0x7f800000; }
  if (kind === 11 && to - from === 8) { return ((b[from + 7] << 24 | b[from + 6] << 16) & 0x7ff00000) !== 0x7ff00000; }
  return true;
}

// a read's non-finite float, refused on its raw bits: the first name wins, and
// the load that met it refuses whole (TableVariableLoad)
function TableVariableNonFinite(report, d) {
  if (!report.nonFiniteFloat) { report.nonFiniteFloat = d.nf; }
}

// §4's WIDENING RUNGS: a narrower kind on its own signedness ladder, and f32
// into f64. The 128-bit rungs are kind 18 (I128) accepting 2..5 and kind 19
// (U128) accepting 6..9 — a cross-sign or float payload is a kind mismatch, not
// a widen, exactly as ir.TableKindWidens decides (docs/SPEC-TABLES.md §4).
function TableVariableWidens(kind, declared) {
  switch (declared) {
    case 3: case 4: case 5: return kind >= 2 && kind < declared;
    case 7: case 8: case 9: return kind >= 6 && kind < declared;
    case 18: return kind >= 2 && kind <= 5;
    case 19: return kind >= 6 && kind <= 9;
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
function TableVariableLink(codecs, unions) {
  TableVariableLinkUnions(codecs, unions);
  for (const name of Object.keys(codecs)) {
    const c = codecs[name];
    c.byId = new Map();
    c.byKey = new Map();
    for (const fd of c.fields) {
      c.byId.set(fd.id, fd);
      c.byKey.set(fd.key, fd);
      fd.q = TableVariableJsonString(fd.key);
      TableVariableLinkDesc(fd, codecs, unions);
    }
  }
  for (const name of Object.keys(codecs)) {
    const p = codecs[name].place;
    p.byType = new Map();
    for (const t of p.tables) { p.byType.set(codecs[t].typeId, codecs[t]); }
  }
}

// THE FREEZE (docs/PORTING.md M13: descriptors are constants): once the link
// has filled every descriptor, the whole graph is frozen, every object and
// array reachable from it, so nothing after module load can change how a
// table saves, loads or writes text. A Map the link built is frozen as an
// object and its values are frozen; its entries are written by the link alone.
// A function (a codec's make) and a typed array are left as they are.
function TableVariableFreeze(x) {
  if (x === null || typeof x !== "object" || ArrayBuffer.isView(x) || Object.isFrozen(x)) { return x; }
  Object.freeze(x);
  if (x instanceof Map) {
    for (const v of x.values()) { TableVariableFreeze(v); }
    return x;
  }
  for (const k of Object.keys(x)) { TableVariableFreeze(x[k]); }
  return x;
}

// the live count of an array: a bounded array never writes past its bound
function TableVariableCount(fd, val) { if (fd.fixed) { return fd.bound; } return fd.list ? val.length : Math.min(val.length, fd.bound); }

// a field guarded by bool fields rides only when every guard holds (§2.4)
function TableVariableGuardHolds(fd, v) {
  if (fd.g !== undefined) {
    for (const [k, want] of fd.g) { if (Boolean(v[k]) !== want) { return false; } }
  }
  return true;
}

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

// RETENTION'S NODE STEP (§6.6): a record retained inside a node record names
// which node it belongs to by the object's own identity, so the same record
// rides back into that node's body when the value is saved. The ordinal is
// reader-private: nothing on the wire reads it, and the root's path has none.
const TableVariableRetainNodeIds = new WeakMap();
let TableVariableRetainNextNodeId = 1;
// the ROOT's path is the one node step that names no record, so a discard in
// the root body cannot reach a record retained inside a node.
function TableVariableRetainRootPath() { return [{ o: 0xffffffff, i: 0 }]; }
function TableVariableRetainNodePath(v) {
  let id = TableVariableRetainNodeIds.get(v);
  if (id === undefined) { id = TableVariableRetainNextNodeId++; TableVariableRetainNodeIds.set(v, id); }
  return [{ o: 0xffffffff, i: id }];
}

// the pointer and blob edges of one node's by-value closure, in the walk's
// order: fields in declaration order, array elements in index order, a nested
// table or a table element descended in place
function TableVariableEdges(c, v, out) {
  for (const fd of c.fields) { TableVariableFieldEdges(fd, v, out); }
  return out;
}

// one field's edges, read at v[fd.n]: an absent optional has none (§2.3), a
// map's are its entries' in key order (§2.8), a union's its set arm's (§2.6)
function TableVariableFieldEdges(fd, v, out) {
  if (!TableVariableGuardHolds(fd, v)) { return; }
  const val = v[fd.n];
  if (fd.opt && val === null) { return; }
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
    case "map":
      for (const e of TableVariableMapEntries(fd, val)) { TableVariableEdges(fd.c, e, out); }
      break;
    case "union":
      TableVariableUnionEdges(fd.u, val, out);
      break;
    case "keyed":
      if (fd.e.s === "tab") { for (let i = 0; i < val.length; i++) { TableVariableEdges(fd.e.c, val[i], out); } }
      break;
    case "arr": {
      const n = TableVariableCount(fd, val);
      if (fd.e.s === "ptr") {
        for (let i = 0; i < n; i++) { if (val[i] !== null) { out.push({ blob: false, c: fd.e.c, val: val[i] }); } }
      } else if (fd.e.s === "tab") {
        for (let i = 0; i < n; i++) { TableVariableEdges(fd.e.c, val[i], out); }
      } else if (fd.e.s === "union") {
        for (let i = 0; i < n; i++) { TableVariableUnionEdges(fd.e.u, val[i], out); }
      }
      break;
    }
  }
}

// refuseShared is the WRITER'S rule (§2.5): a blob object a second slot
// names is blob_shared, and Save and Measure refuse it (null, -1) rather than
// write the edge or a copy per slot. A cook packs the graph as it finds it and
// passes false.
function TableVariableNumber(c, root, refuseShared, why) {
  const nodes = new TableVariableNodes();
  nodes.index.set(root, 1);
  const open = new Set([root]);
  const stack = [{ v: root, edges: TableVariableEdges(c, root, []), i: 0 }];
  while (stack.length > 0) {
    const top = stack[stack.length - 1];
    if (top.i >= top.edges.length) { open.delete(top.v); stack.pop(); continue; }
    const e = top.edges[top.i++];
    if (e.blob) {
      if (!TableVariableBlobNode(nodes, e.holder, e.fd, e.val) && refuseShared) {
        if (why) { why.reason = "blob_shared"; }
        return null;
      }
      continue;
    }
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
    return true;
  }
  // a blob OBJECT already numbered is one blob a second slot names: shared
  if (nodes.index.has(val)) { return false; }
  nodes.list.push({ typeId, body: fd.s === "sblob" ? TableVariableUtf8Encode(String(val)) : val });
  nodes.index.set(val, nodes.list.length + 1);
  return true;
}

// ---- THE SAVE WALK (docs/SPEC-TABLES.md §3; internal/tablewire/encode.go) ---

function TableVariableHeader(w, ids, fd, kind) { w.leb(ids.ref(fd.id)); w.u8(kind); }

function TableVariableWriteInt(w, width, v) {
  switch (width) {
    case 1: w.u8(v); break;
    case 2: w.u16(v); break;
    case 4: w.u32(v); break;
    case 16: w.u128(v); break;
    default: w.u64(BigInt.asUintN(64, v));
  }
}

// an enum value's reference: the variant name's id, 0 for None, and -1 for a
// value no variant names, which the save refuses (§5)
function TableVariableEnumRef(e, ids, val) {
  if (val === null || val === "None") { return 0; }
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
  if (!TableVariableRetainTail(w, ids, retain, path)) { return false; }
  w.u8(0);
  return true;
}

// ONE FIELD, elided at its declared default (§3): a pointer and a blob at
// null, an array when empty, a nested table when its body is its terminator
// alone — the field's id rolled back with it, so an elided field costs the id
// table nothing either.
function TableVariableWriteField(fd, w, ids, nodes, v, retain, path) {
  if (!TableVariableGuardHolds(fd, v)) { return true; }
  const val = v[fd.n];
  if (fd.opt) {
    // ABSENT IS NOT WRITTEN; PRESENT ALWAYS RIDES (§2.3)
    if (val === null) { return true; }
    return TableVariableWritePresent(fd, w, ids, nodes, v, retain, path);
  }
  switch (fd.s) {
    case "map": return TableVariableWriteMap(fd, w, ids, nodes, val, retain, path);
    case "keyed": return TableVariableWriteKeyed(fd, w, ids, nodes, val, retain, path);
    case "union":
      // None elides: the absence of the field is the None (§3)
      if (val === null) { return true; }
      w.leb(ids.ref(fd.id));
      w.u8(15);
      return TableVariableArmWrite(fd.u, w, ids, nodes, val, retain, path, fd.o);
    case "bool":
      if (val !== fd.def) { TableVariableHeader(w, ids, fd, 1); w.u8(val ? 1 : 0); }
      return true;
    case "int": case "flags":
      if (val !== fd.def) { TableVariableHeader(w, ids, fd, fd.k); TableVariableWriteInt(w, fd.kw, val); }
      return true;
    case "float":
      if (!TableVariableFiniteFloat(fd.f32, val)) { return TableVariableRefuseNonFinite(); }
      if (!TableVariableSameFloat(fd.f32, val, fd.def)) {
        TableVariableHeader(w, ids, fd, fd.k);
        TableVariableWriteFloat(w, fd.f32, val);
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
    case "wstr":
      // WIDE TEXT (docs/SPEC-TABLES.md §3; SPEC.md §4.12): it takes no
      // declared default, so the EMPTY text is what elides; L is a BYTE length,
      // twice the units, and a value past N units refuses the save
      if (val.length !== 0) {
        if (val.length > fd.bound) { return false; }
        TableVariableHeader(w, ids, fd, 33);
        w.leb(val.length * 2);
        TableVariableWideWrite(w, val, fd.bound);
      }
      return true;
    case "bytes":
      if (!TableVariableSameBytes(val, fd.def)) {
        // bytes ride as an array of u8 (§2.5): the body is the element kind,
        // the count and the bytes, and L is that body's length — the count's
        // LEB128 is two bytes from 128 on, so L is never a constant plus the
        // byte count (internal/tablewire encode.go)
        TableVariableHeader(w, ids, fd, 14);
        const body = new TableVariableWriter();
        body.u8(6);
        body.leb(val.length);
        body.raw(val);
        w.leb(body.len);
        w.raw(body.b);
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
      if (fd.fixed && fd.e.s !== "tab") {
        let allDef = true;
        for (let i = 0; i < n; i++) {
          if (!TableVariableElemIsDefault(fd.e, val[i])) { allDef = false; break; }
        }
        if (allDef) { return true; }
      }
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
    case "int": case "flags": TableVariableWriteInt(w, e.kw, val); return true;
    case "float": return TableVariableWriteFloat(w, e.f32, val);
    case "enum": {
      const vref = TableVariableEnumRef(e, ids, val);
      if (vref < 0) { return false; }
      w.leb(vref);
      return true;
    }
    case "ptr": w.leb(val === null ? 0 : nodes.index.get(val)); return true;
    case "union": {
      // a None element rides as the single zero byte: position is identity (§3)
      if (val === null) { w.leb(0); return true; }
      // THE ELEMENT'S OWN STEP (§6.6): repeated union elements retain under
      // different indices, as the reference's retainReadPath(i) does.
      if (path !== null) { path.push({ o: fd.o, i: i }); }
      const okU = TableVariableArmWrite(e.u, w, ids, nodes, val, retain, path, fd.o);
      if (path !== null) { path.pop(); }
      return okU;
    }
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
// or a value with no wire identity refuses the save (null), and so does one
// blob two slots name, blob_shared (§2.5): every writer refuses it.
function TableVariableSave(c, v, retain, why) {
  const outer = TableVariableEntryCache, outerWhy = TableVariableSaveWhy;
  TableVariableEntryCache = new Map();
  TableVariableSaveWhy = why === undefined ? null : why;
  try {
    return TableVariableSaveWalk(c, v, retain, why);
  } finally {
    TableVariableEntryCache = outer;
    TableVariableSaveWhy = outerWhy;
  }
}

// THE MEASURE'S REASON TWIN (Go's MeasureReason): [size, null] for a value
// the wire carries, [-1, reason] for one it refuses — blob_shared for a blob
// two slots name (§2.5), non_finite_float for a NaN or an infinity (§3.3),
// invalid_value for any other value outside the
// writer's contract (a data cycle, §3.1; an enum value no variant names, §5).
function TableVariableMeasureReason(c, v) {
  const why = { reason: null };
  const b = TableVariableSave(c, v, null, why);
  if (b !== null) { return [b.length, null]; }
  return [-1, why.reason === null ? "invalid_value" : why.reason];
}

function TableVariableSaveWalk(c, v, retain, why) {
  const nodes = TableVariableNumber(c, v, true, why);
  if (nodes === null) { return null; }
  const w = new TableVariableWriter();
  w.u8(TableVariableForm);
  const ids = new TableVariableIds(retain);
  const path = TableVariableRetainRootPath();
  if (!TableVariableWriteFields(c, w, ids, nodes, v, retain, path)) { return null; }
  if (!TableVariableRetainTail(w, ids, retain, path)) { return null; }
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
      if (!TableVariableWriteBody(n.codec, body, ids, nodes, n.v, retain, TableVariableRetainNodePath(n.v))) { return null; }
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
// (an early terminator is damage): null when the wire is refused or damaged.
// A REFUSED form byte names its direction in the report's reason
// (docs/SPEC-TABLES.md §3, §3.3, §6.5): message_form_as_file for a batch (form
// 2) handed to a file root, newer_form for any other byte — the fixed form 3
// is newer than this one, and a byte no form defines is too. Damage moves the
// malformed flag and names no reason, as the reference's Load does.
function TableVariableOpen(bytes, report) {
  if (bytes.length < 1) { report.malformed = true; return null; }
  if (bytes[0] !== TableVariableForm) {
    report.refused = 1;
    report.reason = bytes[0] === 2 ? "message_form_as_file" : "newer_form";
    return null;
  }
  if (bytes.length < 9) { report.malformed = true; return null; }
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const count = dv.getBigUint64(bytes.length - 8, true);
  if (count > TableVariableEntryBound || count > BigInt(Math.floor((bytes.length - 9) / 8))) { report.malformed = true; return null; }
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
  // a caller that passed no report still gets the refusal: the read keeps one
  if (report === undefined || report === null) { report = new TableVariableReport(); }
  // this read's own non-finite refusal (§3.3)
  report.nonFiniteFloat = "";
  // DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): this read judges
  // its own damage, so a report a caller hands in already marked malformed
  // stays marked and does not refuse a clean wire.
  const carried = report.malformed;
  report.malformed = false;
  const open = TableVariableOpen(bytes, report);
  // A REFUSED form byte hands a non-variable root back at its declared
  // defaults (§3); DAMAGE hands back nothing (§4).
  if (open === null) {
    const damaged = report.malformed;
    report.malformed = carried || damaged;
    return c.variable || damaged ? null : c.make();
  }
  const table = open.table;
  const v = c.make();
  const raw = c.variable ? TableVariableNodeTableScan(bytes, table, open.end, report) : null;
  const st = { good: raw !== null, nodes: [], root: v, rootCodec: c, pending: [], maps: [] };
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
      // RETENTION INSIDE A NODE RECORD (§6.6): the record's path starts with
      // the node's own identity, so its unknown fields ride back into the same
      // record's tail on the save.
      TableVariableBodyRead(n.codec, new TableVariableReader(n.body, 0, n.body.length), table, n.v, report, st, true, retain, TableVariableRetainNodePath(n.v));
    }
  }
  TableVariableBodyRead(c, new TableVariableReader(bytes, 1, open.end), table, v, report, st, false, retain, TableVariableRetainRootPath());
  // A NON-FINITE FLOAT REFUSES THE READ, the tolerant one too, and outranks
  // any damage the walk met after it (§3.3): nothing lands.
  if (report.nonFiniteFloat) {
    report.refused = 1;
    report.reason = "non_finite_float: " + report.nonFiniteFloat;
    return null;
  }
  TableVariableResolve(st, report);
  // DAMAGE DISCARDS THE WHOLE LOAD (§4): a read that met damage anywhere hands
  // back no root, and a retaining read keeps nothing it would save.
  if (report.malformed) {
    if (retain !== null) { retain.reset(); }
    return null;
  }
  report.malformed = carried;
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
  // a map's entries resolved in place: each Map is built again from them, so a
  // pointer or a blob an entry's value holds lands in the Map (§2.8, §3.1)
  if (st.maps !== undefined) {
    for (const { m, ents } of st.maps) {
      m.clear();
      for (const e of ents) { m.set(e.key, e.value); }
    }
  }
}

// ONE BODY (§3, §4): fields by id in any order, a field this table cannot name
// skipped by its kind and counted unknown (and retained when the caller opted
// in, §6.6), a kind that widens decoded at its own width and counted widened,
// any other kind a kind mismatch skipped whole. Damage stops the body.
function TableVariableBodyRead(c, r, table, v, report, st, nested, retain, path) {
  // A BODY READ REPLACES WHAT AN EARLIER PASS RETAINED HERE (§6.6): the
  // reference's LoadBodyRetain opens with TableRetain.discard(Path,false,0),
  // which is what keeps a duplicate map entry from leaving two records.
  if (retain !== null && path !== null) { TableVariableRetainDiscard(retain, path, false, 0); }
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
        // A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (§3.3, §6.6):
        // kind 10's or 11's raw bits, and a NaN or an infinity refuses the
        // read, named <Table>.0x<id>
        if (!TableVariableRetainedFinite(r.b, from, r.at, kind)) {
          if (!report.nonFiniteFloat) { report.nonFiniteFloat = c.name + ".0x" + id.toString(16).padStart(16, "0"); }
          continue;
        }
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
        // a map entry's KEY widening is the map's one count (mapField)
        if (!(c.entry && fd.n === "key")) { report.widened++; }
        continue;
      }
      // the escape kind 31 and the payload-free kind 32 at a named position take
      // this same rule and no other (§3)
      report.kindMismatch++;
      if (!TableVariableSkip(r, kind)) { report.malformed = true; return false; }
      continue;
    }
    if (!TableVariableReadField(fd, r, table, v, report, st, retain, path)) { return false; }
    // the field rode, so an optional is PRESENT; a None it holds is "None" (§2.3)
    if (fd.opt && fd.s === "enum" && v[fd.n] === null) { v[fd.n] = "None"; }
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
    case 1: {
      // A BOOL BYTE IS 0 OR 1 (§3): any other byte reads true, counts one
      // malformed and the body reads on (decode.go scalarAt)
      const b = r.u8();
      if (b > 1) { report.malformed = true; }
      return b !== 0;
    }
    case 10: {
      // A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (§3.3), before the
      // bits become a number and before the field is written; a compressed
      // float takes the test before its bounds. Every finite float32 widens
      // exactly, so the float rung is the ordinary conversion.
      const b = r.u32();
      if ((b & 0x7f800000) === 0x7f800000) { TableVariableNonFinite(report, d); return undefined; }
      const s = TableVariableScratch;
      s.setUint32(0, b, true);
      let v = s.getFloat32(0, true);
      if (d.f32 && d.lo !== null) {
        if (v < d.lo) { v = Math.fround(d.lo); report.clamped++; }
        else if (v > d.hi) { v = Math.fround(d.hi); report.clamped++; }
      }
      return v;
    }
    case 11: {
      // the exponent is the high word's bits 20..30
      const hi = (r.b[r.at + 4] | (r.b[r.at + 5] << 8) | (r.b[r.at + 6] << 16) | (r.b[r.at + 7] << 24)) >>> 0;
      if ((hi & 0x7ff00000) === 0x7ff00000) { r.at += 8; TableVariableNonFinite(report, d); return undefined; }
      return r.f64();
    }
    case 2: case 20: x = r.i8(); break;
    case 3: case 21: x = r.i16(); break;
    case 4: case 22: x = r.i32(); break;
    case 5: case 23: x = r.i64(); break;
    case 6: case 25: x = r.u8(); break;
    case 7: case 26: x = r.u16(); break;
    case 8: case 27: x = r.u32(); break;
    case 9: case 28: x = r.u64(); break;
    case 18: case 24: x = r.i128(); break;
    case 19: case 29: x = r.u128(); break;
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
    case "bool": case "int": case "flags": case "float": {
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
    case "wstr": {
      // WIDE TEXT (docs/SPEC-TABLES.md §3, §4; internal/tablewire decode.go):
      // an ODD L is framing damage on this field and ill-formed UTF-16 (an
      // unpaired surrogate, a zero unit) is damage too — each reads the
      // declared default, counts one malformed, and the parent reads on past
      // L; a payload past N units is CLAMPED, a pair never split
      const L = r.leb();
      if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
      const s = TableVariableWideDecode(r.b.subarray(r.at, r.at + L));
      r.at += L;
      if (s === null) { report.malformed = true; v[fd.n] = fd.def; return true; }
      if (s.length > fd.bound) { report.clamped++; v[fd.n] = TableVariableWideClamp(s, fd.bound); }
      else { v[fd.n] = s; }
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
    case "map":
      return TableVariableReadMap(fd, r, table, v, report, st, retain, path);
    case "keyed":
      return TableVariableReadKeyed(fd, r, table, v, report, st, retain, path);
    case "union": {
      const x = TableVariableUnionRead(fd.u, r, table, report, st, retain, path, fd.o);
      if (x === undefined) { return false; }
      v[fd.n] = x;
      return true;
    }
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
  TableVariableReadArrayBody(fd, r, end, table, v, report, st, retain, path);
  r.at = end;
  return true;
}

// AN ARRAY BODY between r.at and end — a FIELD's after its own L, an ARM's
// under the arm's L (decode.go arrayBody). It answers whether the body was
// SELECTED: false for a damaged count or a foreign element kind, where an arm
// holding it reads None. A present optional array that rode with its own
// element kind is present even when its body is inert (§2.3).
//
// THE HEADER IS READ AGAINST THE PARENT BUFFER and not against the body's own
// L, because a canonical LEB128 is up to ten bytes and an L of 2 is the
// shortest body that carries one at all (docs/SPEC-TABLES.md §3, §4; decode.go
// arrayBody). A count spelled wide enough therefore leaves the cursor past the
// end it was framed by, and a bounded array's excess is clamped and counts even
// as that body covers no elements; read is bounded by the body and a count the
// body cannot cover keeps the decoded prefix.
function TableVariableReadArrayBody(fd, r, end, table, v, report, st, retain, path) {
  const L = end - r.at;
  if (L >= 2) {
    const ek = r.u8();
    const count = r.leb();
    if (r.bad) {
      report.malformed = true;
      if (fd.opt && v[fd.n] === null) { v[fd.n] = TableVariableFieldDefault(fd); }
      return false;
    }
    if (ek !== fd.ek) {
      if (!TableVariableWidens(ek, fd.ek)) { report.kindMismatch++; return false; }
      report.widened++;
    }
    let keep = count;
    if (fd.list) {
      if (count > 0x7fffffff) { v[fd.n] = []; report.malformed = true; return false; }
    } else if (count > fd.bound) {
      keep = fd.bound;
      report.clamped++;
    }
    // the span from the cursor to the body's end, empty rather than negative
    // when the count's own spelling ran past it (decode.go subTo)
    const subStart = r.at;
    const subEnd = Math.max(r.at, end);
    const sub = new TableVariableReader(r.b, subStart, subEnd);
    if (retain !== null && path !== null) { TableVariableRetainDiscard(retain, path, true, fd.o); }
    if (fd.s === "bytes") {
      const out = [];
      for (let i = 0; i < keep; i++) {
        if (sub.at >= sub.end) { report.malformed = true; break; }
        out.push(sub.u8());
      }
      v[fd.n] = new Uint8Array(out);
    } else if (fd.fixed) {
      if (!Array.isArray(v[fd.n]) || v[fd.n].length !== fd.bound) {
        v[fd.n] = Array.from({ length: fd.bound }, () => TableVariableElemDefault(fd.e));
      }
      const out = v[fd.n];
      for (let i = 0; i < keep; i++) {
        if (!TableVariableReadElement(fd, sub, table, out, report, st, retain, path, ek, i)) { break; }
      }
    } else {
      const out = [];
      for (let i = 0; i < keep; i++) {
        if (!TableVariableReadElement(fd, sub, table, out, report, st, retain, path, ek, i)) { break; }
      }
      v[fd.n] = out;
    }
  }
  if (fd.opt && v[fd.n] === null) { v[fd.n] = TableVariableFieldDefault(fd); }
  return true;
}

function TableVariableReadElement(fd, r, table, out, report, st, retain, path, ek, i) {
  const e = fd.e;
  switch (e.s) {
    case "union": {
      // an ARM HEADER in its place (decode.go element): null for None
      if (r.at >= r.end) { report.malformed = true; return false; }
      if (fd.fixed) { out[i] = null; }
      // THE ELEMENT'S OWN STEP (§6.6), the twin of the writer's: repeated
      // union elements are different records, not one re-read.
      if (path !== null) { path.push({ o: fd.o, i: i }); }
      const x = TableVariableUnionRead(e.u, r, table, report, st, retain, path, fd.o);
      if (path !== null) { path.pop(); }
      if (x === undefined) { return false; }
      out[i] = x;
      return true;
    }
    case "ptr": {
      const index = r.leb();
      if (r.bad) { report.malformed = true; return false; }
      out[i] = null;
      st.pending.push([out, i, index, e]);
      return true;
    }
    case "enum": {
      const x = TableVariableEnumRead(e, r, table, report, retain);
      if (x === undefined) { return false; }
      out[i] = x;
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
      out[i] = inst;
      return true;
    }
  }
  const x = TableVariableScalar(r, e, ek, report);
  if (x === undefined) { return false; }
  out[i] = x;
  return true;
}

// ---- THE REGION MEASURE (docs/SPEC-TABLES.md §6.5, §20.3) ----------------
//
// TableVariableExtent walks ONE table body, mirroring
// internal/tablewire/extent.go's extentScan: it adds the STORAGE the framing
// commands — the element extent of each unbounded list (§2.9) and each map
// (§2.8), aligned as the element is laid — and descends into a table nested by
// value and into a union's selected arm. It never descends into a list or map
// ELEMENT: those records are sized by the node table, not by the parent.
// A count that cannot fit its framing answers -1, a refusal LoadMeasure reports.
function TableVariableExtent(c, r, ids, at) {
  for (;;) {
    if (r.at >= r.end) { return at; }
    const ref = r.leb();
    if (r.bad || ref === 0 || ref > ids.length) { return at; }
    if (r.at >= r.end) { return at; }
    const id = ids[ref - 1];
    const kind = r.u8();
    if (id === TableNodeTableId) { if (!TableVariableSkip(r, kind)) { return at; } continue; }
    const fd = c.byId.get(id);
    if (fd === undefined) { if (!TableVariableSkip(r, kind)) { return at; } continue; }
    if (kind !== fd.k) {
      if (!TableVariableWidens(kind, fd.k)) { if (!TableVariableSkip(r, kind)) { return at; } continue; }
    }
    if (fd.s === "union") {
      const rr = TableVariableUnionBodyExtent(fd.u, r, ids, at);
      if (rr < 0) { return -1; }
      at = rr;
      continue;
    }
    switch (fd.s) {
      case "tab": case "arr": case "bytes": case "map": case "keyed": {
        const rr = TableVariableContainerExtent(fd, r, ids, at);
        if (rr < 0) { return -1; }
        at = rr;
        continue;
      }
      default: {
        if (!TableVariableSkip(r, kind)) { return at; }
      }
    }
  }
}

// TableVariableContainerExtent reads one container field's L-bounded payload and
// adds the storage it commands (docs/SPEC-TABLES.md §6.5, §20.3): a table by
// value recurses, a list adds its element extent, a map adds its entry extent,
// and each element or entry record is then walked for the nested lists and maps
// it holds by value, as internal/tablewire's extentScan field() does. bytes(N),
// a fixed array and a keyed array add no element extent of their own, but their
// element records still add the storage they hold. A length the reader cannot
// hold ends the WHOLE walk of the enclosing body, as extentBody's r.off =
// len(r.buf) does: the bytes after an overrunning length are not fields, so
// nothing past it is counted.
function TableVariableContainerExtent(fd, r, ids, at) {
  const L = r.leb();
  if (r.bad || r.at + L > r.end) { r.at = r.end; return at; }
  const sub = new TableVariableReader(r.b, r.at, r.at + L);
  r.at += L;
  switch (fd.s) {
    case "tab": return TableVariableExtent(fd.c, sub, ids, at);
    case "arr": return TableVariableArrayExtent(fd, sub, ids, at);
    case "map": return TableVariableMapExtent(fd, sub, ids, at);
    case "keyed": return TableVariableKeyedExtent(fd, sub, ids, at);
    default: return at;
  }
}

// TableVariableElementsExtent adds the nested storage of n element records
// (docs/SPEC-TABLES.md §6.5, §20.3). A table element is one L-bounded body; a
// union element is an arm header in its place. Any other element takes no
// nested storage, so there is nothing to walk.
function TableVariableElementsExtent(e, sub, ids, at, n) {
  if (e.s === "tab") {
    for (let i = 0; i < n && sub.at < sub.end; i++) {
      const elen = sub.leb();
      if (sub.bad || sub.at + elen > sub.end) { return at; }
      const er = new TableVariableReader(sub.b, sub.at, sub.at + elen);
      sub.at += elen;
      const rr = TableVariableExtent(e.c, er, ids, at);
      if (rr < 0) { return -1; }
      at = rr;
    }
  } else if (e.s === "union") {
    for (let i = 0; i < n && sub.at < sub.end; i++) {
      const rr = TableVariableUnionBodyExtent(e.u, sub, ids, at);
      if (rr < 0) { return -1; }
      at = rr;
    }
  }
  return at;
}

// TableVariableArrayExtent adds an unbounded array's element extent, aligned as
// its element is laid, then walks every element record (docs/SPEC-TABLES.md
// §6.5, §20.3): the element storage of nested lists and maps is inside the
// holder's node extent and must be counted at every depth.
function TableVariableArrayExtent(fd, sub, ids, at) {
  if (sub.end - sub.at < 2) { return at; }
  const ek = sub.u8();
  const n = sub.leb();
  if (sub.bad) { return at; }
  if (ek !== fd.ek && !TableVariableWidens(ek, fd.ek)) { return at; }
  if (fd.list) {
    if (n > 0x7fffffff) { return -1; }
    const floor = ek === 13 ? 2 : ek === 17 ? 1 : Math.max(1, tableRetainWidth(ek));
    if (n > Math.floor((sub.end - sub.at) / floor)) { return -1; }
    at = TableVariableRound(at, fd.eal);
    at += n * fd.esz;
  }
  return TableVariableElementsExtent(fd.e, sub, ids, at, n);
}

// TableVariableMapExtent adds a map's entry extent, aligned as its entry is
// laid, then walks every entry record (docs/SPEC-TABLES.md §6.5, §20.3): the
// entries are by-value records inside the holder's node extent, and the lists
// and maps an entry's value holds by value are laid after it.
function TableVariableMapExtent(fd, sub, ids, at) {
  if (sub.end - sub.at < 2) { return at; }
  const ek = sub.u8();
  const n = sub.leb();
  if (sub.bad) { return at; }
  if (ek !== 13) { return at; }
  if (n > 0x7fffffff) { return -1; }
  if (n > Math.floor((sub.end - sub.at) / 2)) { return -1; }
  at = TableVariableRound(at, fd.eal);
  at += n * fd.esz;
  for (let i = 0; i < n && sub.at < sub.end; i++) {
    const elen = sub.leb();
    if (sub.bad || sub.at + elen > sub.end) { return at; }
    const er = new TableVariableReader(sub.b, sub.at, sub.at + elen);
    sub.at += elen;
    const rr = TableVariableExtent(fd.c, er, ids, at);
    if (rr < 0) { return -1; }
    at = rr;
  }
  return at;
}

// TableVariableKeyedExtent walks a keyed array's present slots (docs/
// SPEC-TABLES.md §6.5, §20.3): a slot is a key reference, then its element
// where an unbounded array holds one. The slot's record storage is inside the
// holder's node extent, so its nested lists and maps are counted.
function TableVariableKeyedExtent(fd, sub, ids, at) {
  if (sub.end - sub.at < 2) { return at; }
  const ek = sub.u8();
  const n = sub.leb();
  if (sub.bad) { return at; }
  if (ek !== fd.ek && !TableVariableWidens(ek, fd.ek)) { return at; }
  if (fd.e.s !== "tab" && fd.e.s !== "union") { return at; }
  for (let i = 0; i < n && sub.at < sub.end; i++) {
    sub.leb();
    if (sub.bad) { return at; }
    if (fd.e.s === "union") {
      const rr = TableVariableUnionBodyExtent(fd.e.u, sub, ids, at);
      if (rr < 0) { return -1; }
      at = rr;
      continue;
    }
    const elen = sub.leb();
    if (sub.bad || sub.at + elen > sub.end) { return at; }
    const er = new TableVariableReader(sub.b, sub.at, sub.at + elen);
    sub.at += elen;
    const rr = TableVariableExtent(fd.e.c, er, ids, at);
    if (rr < 0) { return -1; }
    at = rr;
  }
  return at;
}

// TableVariableUnionBodyExtent reads one union payload from a reader (a field's
// or an array element's place) and adds the storage its selected arm commands.
// The arm is chosen by its id alone, exactly as internal/tablewire/extent.go's
// extentScan.union does: the arm's kind byte is consumed and not compared, so a
// rewritten or damaged kind still measures the selected arm's payload, the way
// the oracle counts it. A header the reader cannot hold (a bad arm reference,
// an id past the trailer, a length past the end) ends the walk of the reader it
// was read from, as extentScan.union sets r.off = len(r.buf).
function TableVariableUnionBodyExtent(u, r, ids, at) {
  if (r.at >= r.end) { return at; }
  const armRef = r.leb();
  if (r.bad) { r.at = r.end; return at; }
  if (armRef === 0) { return at; }
  if (armRef > ids.length || r.at >= r.end) { r.at = r.end; return at; }
  const armId = ids[armRef - 1];
  r.u8();
  const n = r.leb();
  if (r.bad || r.at + n > r.end) { r.at = r.end; return at; }
  const sub = new TableVariableReader(r.b, r.at, r.at + n);
  r.at += n;
  const arm = u.byId.get(armId);
  if (arm === undefined) { return at; }
  if (arm.body) { return TableVariableExtent(arm.c, sub, ids, at); }
  if (!arm.void && arm.f !== undefined) { return TableVariableContainerExtent(arm.f, sub, ids, at); }
  return at;
}
`

// The json-walk markers (docs/PORTING.md M9): everything between them is the
// one text walk, byte-identical in every unit's <Package>Variable.js. The
// package name never appears between them, so the gate normalises nothing.
const (
	jsJsonWalkBegin = "// ---- json walk: begin ----"
	jsJsonWalkEnd   = "// ---- json walk: end ----"
)

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
// trailing point removed, an exponent of at least two digits. The digits are
// the double's EXACT decimal expansion rounded to prec significant digits, a
// tie to EVEN, as C and Go round. The host's toExponential and toFixed round a
// tie AWAY from zero (ECMA-262 "pick the larger n"), which is not the engine's
// rule: the float32 3624545.25 (0x4a5d3985) at eight digits is 3624545.2 in
// the engine and was 3624545.3 here (I13, TestTextDifferentialJavaScript).
function TableVariableExactDigits(a) {
  // a finite, positive double as m * 2^e, then its exact decimal digits D with
  // the decimal exponent of D's first digit
  const view = new DataView(new ArrayBuffer(8));
  view.setFloat64(0, a);
  const bits = view.getBigUint64(0);
  const biased = Number((bits >> 52n) & 0x7FFn);
  let m = bits & 0xFFFFFFFFFFFFFn;
  let e = -1074;
  if (biased !== 0) { m |= 1n << 52n; e = biased - 1075; }
  if (e >= 0) {
    const d = (m << BigInt(e)).toString();
    return { d, exp: d.length - 1 };
  }
  const d = (m * 5n ** BigInt(-e)).toString();
  return { d, exp: d.length - 1 + e };
}

function TableVariableFormatG(v, prec) {
  const neg = v < 0 || Object.is(v, -0);
  const a = Math.abs(v);
  let keep, exp;
  if (a === 0) {
    keep = "0".repeat(prec);
    exp = 0;
  } else {
    const x = TableVariableExactDigits(a);
    exp = x.exp;
    if (x.d.length <= prec) {
      keep = x.d + "0".repeat(prec - x.d.length);
    } else {
      keep = x.d.slice(0, prec);
      const rest = x.d.slice(prec);
      let up = rest[0] > "5" || (rest[0] === "5" && /[1-9]/.test(rest.slice(1)));
      if (rest[0] === "5" && !up && !/[1-9]/.test(rest.slice(1))) {
        up = (keep.charCodeAt(prec - 1) - 48) % 2 === 1;
      }
      if (up) {
        keep = (BigInt(keep) + 1n).toString();
        if (keep.length > prec) { keep = keep.slice(0, prec); exp++; }
      }
    }
  }
  let s;
  if (exp < -4 || exp >= prec) {
    let m = keep[0];
    if (prec > 1) { m = (m + "." + keep.slice(1)).replace(/0+$/, "").replace(/\.$/, ""); }
    const e = Math.abs(exp);
    s = m + "e" + (exp < 0 ? "-" : "+") + (e < 10 ? "0" + e : String(e));
  } else {
    if (exp >= 0) {
      s = keep.slice(0, exp + 1);
      if (keep.length > exp + 1) { s += "." + keep.slice(exp + 1); }
    } else {
      s = "0." + "0".repeat(-exp - 1) + keep;
    }
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
  const outer = TableVariableEntryCache;
  TableVariableEntryCache = new Map();
  try {
    return TableVariableToJsonWalk(c, v);
  } finally {
    TableVariableEntryCache = outer;
  }
}

function TableVariableToJsonWalk(c, v) {
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
    if (!TableVariableGuardHolds(fd, v)) { continue; }
    // an ABSENT optional writes no key: the absent key is the absence (§16.2)
    if (fd.opt && v[fd.n] === null) { continue; }
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
    // wide text's text is the same JSON string (docs/SPEC-TABLES.md §16.2;
    // tabletext writeWString), an unpaired surrogate written as U+FFFD
    case "wstr": out.push(TableVariableJsonString(val)); return true;
    case "bytes": out.push(TableVariableBase64Text(val)); return true;
    case "map": return TableVariableJsonMap(fd, val, depth, g, out);
    case "keyed": return TableVariableJsonKeyed(fd, val, depth, g, out);
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

// a fixed(W, F)'s exact decimal (internal/tabletext/wide.go FormatWide):
// val * 2^-F is an exact F-place decimal (rem * 5^F in F digits), with
// trailing fraction zeros past the first dropped
function TableVariableFormatFixed(val, frac) {
  let v = BigInt(val);
  let neg = false;
  if (v < 0n) { neg = true; v = -v; }
  const whole = v >> BigInt(frac);
  const rem = v & ((1n << BigInt(frac)) - 1n);
  let out = (neg ? "-" : "") + String(whole) + ".";
  if (rem === 0n) { return out + "0"; }
  const scaled = (rem * (5n ** BigInt(frac))).toString().padStart(frac, "0").replace(/0+$/, "");
  return out + scaled;
}

function TableVariableJsonScalar(d, val, depth, g, out) {
  switch (d.s) {
    case "ptr": return TableVariableJsonPointer(d.c, val, depth, g, out);
    case "tab": return TableVariableJsonValue(d.c, val, depth, g, out);
    case "union": return TableVariableJsonUnion(d.u, val, depth, g, out);
    case "enum":
      if (val === null || val === "None") { out.push("\"None\""); return true; }
      for (const [name] of d.vs) { if (name === val) { out.push(TableVariableJsonString(name)); return true; } }
      return false;
    case "flags": {
      const bits = BigInt(val);
      if (bits === 0n) { out.push("[]"); return true; }
      out.push("[");
      const ind = "\n" + "  ".repeat(depth + 1);
      let first = true;
      for (let bit = 0; (bits >> BigInt(bit)) > 0n; bit++) {
        if (((bits >> BigInt(bit)) & 1n) === 0n) { continue; }
        if (bit >= d.vs.length) { return false; }
        if (!first) { out.push(","); }
        first = false;
        out.push(ind + TableVariableJsonString(d.vs[bit]));
      }
      out.push("\n" + "  ".repeat(depth) + "]");
      return true;
    }
    case "bool": out.push(val ? "true" : "false"); return true;
    case "float": {
      const t = TableVariableFloatText(val, d.f32);
      if (t === null) { return false; }
      out.push(t);
      return true;
    }
    case "int":
      if (d.k >= 20 && d.k <= 29) { out.push(TableVariableFormatFixed(val, d.frac)); return true; }
      out.push(String(val));
      return true;
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
      if (c >= 0xd800 && c <= 0xdfff) { out += String.fromCharCode(0xfffd); this.p++; continue; }
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
  const carried = report.malformed;
  report.malformed = false;
  const inp = new TableVariableTextIn(text);
  const v = c.make();
  const ok = TableVariableReadTable(c, inp, v, 0, new Map(), report);
  if (ok) {
    inp.space();
    if (inp.p !== text.length) { inp.bad = true; }
  }
  // DAMAGE DISCARDS THE WHOLE LOAD (§4, §16.2): text that does not read hands
  // back no value
  if (inp.bad || !ok || report.malformed) { report.malformed = true; return null; }
  report.malformed = carried;
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
      if (shape === "z" && (fd.opt || fd.s === "ptr" || fd.s === "sblob" || fd.s === "bblob")) {
        // null is the ABSENCE: an optional reads absent, a pointer null (§16.2)
        if (!inp.literal("null")) { return false; }
        v[fd.n] = null;
      } else {
        // PRESENCE OF THE KEY IS PRESENCE (§16.2): an optional whose key is
        // here is present, at its default until a value is placed — a value of
        // the wrong shape included
        if (fd.opt && v[fd.n] === null) { v[fd.n] = TableVariableFieldDefault(fd); }
        if (shape !== fd.js) {
          report.kindMismatch++;
          if (!inp.skip(depth + 1, graph)) { return false; }
        } else if (!TableVariableTextField(fd, inp, v, depth, graph, report)) {
          return false;
        }
        if (fd.opt && fd.s === "enum" && v[fd.n] === null) { v[fd.n] = "None"; }
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
    case "wstr": {
      // the wstring row of §16.2 (tabletext scanWString): the same text,
      // TRANSCODED at the boundary — a JavaScript string already is UTF-16 —
      // and clamped at N CODE UNITS with a pair never split
      const s = inp.str();
      if (s === null) { return false; }
      if (s.length > fd.bound) { report.clamped++; v[fd.n] = TableVariableWideClamp(s, fd.bound); }
      else { v[fd.n] = s; }
      return true;
    }
    case "bytes": {
      const b = TableVariableBase64Read(inp, fd.bound, report);
      if (b === null) { return false; }
      v[fd.n] = b.bytes;
      return true;
    }
    case "arr": return TableVariableTextArray(fd, inp, v, depth, graph, report);
    case "map": return TableVariableTextMap(fd, inp, v, depth, graph, report);
    case "keyed": return TableVariableTextKeyed(fd, inp, v, depth, graph, report);
  }
  return TableVariableTextScalar(fd, inp, v, fd.n, depth, graph, report);
}

function TableVariableZero(e) {
  switch (e.s) {
    case "tab": return e.c.make();
    case "bool": return false;
    case "int": case "flags": return e.big ? 0n : 0;
    case "float": return 0;
  }
  return null;
}

// an array re-established before placing; an unbounded one reads every
// element, a bounded one keeps its prefix and counts the excess clamped; a
// fixed-length one stays at its declared length with unread tail slots at zero;
// a null element of an array of pointers is a null slot (§16.2)
function TableVariableTextArray(fd, inp, v, depth, graph, report) {
  if (inp.peek() !== "[") { inp.bad = true; return false; }
  inp.p++;
  const e = fd.e;
  const out = fd.fixed ? Array.from({ length: fd.bound }, () => TableVariableZero(e)) : [];
  v[fd.n] = out;
  const bound = fd.list ? 0x7fffffff : fd.bound;
  let placed = 0;
  for (;;) {
    let ch = inp.peek();
    if (ch === "]") { inp.p++; return true; }
    if (ch === "") { inp.bad = true; return false; }
    const shape = inp.shape();
    if (placed >= bound) {
      report.clamped++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else if (e.s === "ptr" && shape === "z") {
      if (!inp.literal("null")) { return false; }
      out[placed++] = null;
    } else if (shape !== e.js) {
      report.kindMismatch++;
      if (!inp.skip(depth + 1, graph)) { return false; }
      out[placed++] = TableVariableZero(e);
    } else {
      out[placed] = TableVariableZero(e);
      if (!TableVariableTextScalar(e, inp, out, placed, depth + 1, graph, report)) { return false; }
      placed++;
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

// one value at one cell: a nested object, a pointer, an enum name, a flags
// array, a bool or a number
function TableVariableTextScalar(d, inp, holder, key, depth, graph, report) {
  switch (d.s) {
    case "ptr": return TableVariableTextPointer(d, inp, holder, key, depth, graph, report);
    case "union": return TableVariableTextUnion(d, inp, holder, key, depth, graph, report);
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
    case "flags": {
      if (inp.peek() !== "[") { inp.bad = true; return false; }
      inp.p++;
      let mask = 0n;
      for (;;) {
        let ch = inp.peek();
        if (ch === "]") { inp.p++; holder[key] = d.big ? mask : Number(mask); return true; }
        if (ch === "") { inp.bad = true; return false; }
        if (inp.shape() !== "s") {
          report.kindMismatch++;
          if (!inp.skip(depth + 1, graph)) { return false; }
        } else {
          const s = inp.str();
          if (s === null) { return false; }
          const bit = d.vs.indexOf(s);
          if (bit >= 0) { mask |= 1n << BigInt(bit); } else { report.unknown++; }
        }
        ch = inp.peek();
        if (ch === ",") { inp.p++; continue; }
        if (ch === "]") { inp.p++; holder[key] = d.big ? mask : Number(mask); return true; }
        inp.bad = true;
        return false;
      }
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
      // the float32 a decimal token names, correctly rounded from the token
      // itself; x is the token's correctly-rounded double
      const f32OfToken = (tok, x) => {
        const f = Math.fround(x);
        if (f === x || Number.isNaN(x)) { return f; }
        const b = new DataView(new ArrayBuffer(8));
        const step = (v, up) => {
          if (v === Infinity) { return up ? Infinity : 3.4028234663852886e38; }
          if (v === -Infinity) { return up ? -3.4028234663852886e38 : -Infinity; }
          if (v === 0) { return up ? 1.401298464324817e-45 : -1.401298464324817e-45; }
          b.setFloat32(0, v, true);
          const u = b.getUint32(0, true);
          b.setUint32(0, (v > 0) === up ? u + 1 : u - 1, true);
          return b.getFloat32(0, true);
        };
        const lo = f < x ? f : step(f, false), hi = f < x ? step(f, true) : f;
        const at = (v) => (v === Infinity ? 2 ** 128 : v === -Infinity ? -(2 ** 128) : v);
        if (at(lo) + at(hi) !== 2 * x) { return f; } // not a tie: one rounding is the right one
        // the tie: the sign of (token - x), both exact rationals
        const m = /^(-?)(\d*)(?:\.(\d*))?(?:[eE]([+-]?\d+))?$/.exec(tok);
        if (m === null) { return f; }
        const e10 = Number(m[4] ?? "0") - (m[3] ?? "").length;
        if (!Number.isFinite(e10) || Math.abs(e10) > 100000) { return f; }
        let left = BigInt((m[2] || "0") + (m[3] ?? "")) * (m[1] === "-" ? -1n : 1n);
        b.setFloat64(0, x, true);
        const hiw = b.getUint32(4, true), low = b.getUint32(0, true);
        const ex = (hiw >>> 20) & 0x7ff;
        let mant = (BigInt(hiw & 0xfffff) << 32n) | BigInt(low);
        let e2 = ex === 0 ? -1074 : ex - 1075;
        if (ex !== 0) { mant |= 1n << 52n; }
        let right = x < 0 ? -mant : mant;
        if (e10 >= 0) { left *= 10n ** BigInt(e10); } else { right *= 10n ** BigInt(-e10); }
        if (e2 >= 0) { right <<= BigInt(e2); } else { left <<= BigInt(-e2); }
        if (left === right) { return f; } // exactly on the midpoint: ties-to-even, which fround is
        return left > right ? hi : lo;
      };
      const n = inp.number();
      if (n === null) { inp.bad = true; return false; }
      let x = Number(n.tok);
      // a magnitude the format cannot hold is the wrong shape for the kind
      if (!Number.isFinite(x)) { report.kindMismatch++; return true; }
      let ranged = false;
      if (d.lo !== null) {
        if (x < d.lo) { x = d.lo; ranged = true; report.clamped++; }
        else if (x > d.hi) { x = d.hi; ranged = true; report.clamped++; }
      }
      if (d.f32) {
        // ONE correctly-rounded conversion at the field's own width
        // (internal/tabletext read.go placeFloat, strconv.ParseFloat(tok, 32);
        // docs/SPEC-TABLES.md §16.2). Number() then Math.fround() rounds twice,
        // and the two roundings part company exactly where the double lands ON
        // a float32 rounding midpoint — the band between FLT_MAX and the
        // midpoint above it among them — so a tie is broken by the exact
        // decimal, compared with the double in integer arithmetic.
        x = ranged ? Math.fround(x) : f32OfToken(n.tok, x);
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

// ParseWide + placeWide + WideClamp (internal/tabletext/wide.go, read.go):
// exact decimal arithmetic for 128-bit integers (kinds 18, 19) and fixed(W, F)
// (kinds 20..29), docs/SPEC-TABLES.md §16.2. The token converts to the RAW
// integer, value × 2^F, with no rounding: a value Q I.F cannot spell is the
// wrong shape for the field, a kind mismatch, and nothing is placed (SPEC.md
// §4.6's rule for a fixed default). A magnitude past the 128-bit domain of the
// kind's signedness saturates and counts a clamp; the declared range clamps
// after it, as for every bounded scalar (§4).
function TableVariableTextWide(d, tok, holder, key, report) {
  let i = 0, neg = false;
  if (tok[i] === "-" || tok[i] === "+") { neg = tok[i] === "-"; i++; }
  const digit = () => i < tok.length && tok[i] >= "0" && tok[i] <= "9";
  let intDigits = "", fracDigits = "";
  while (digit()) { intDigits += tok[i++]; }
  if (tok[i] === ".") { i++; while (digit()) { fracDigits += tok[i++]; } }
  let exp = 0;
  if (tok[i] === "e" || tok[i] === "E") {
    i++;
    let expNeg = false;
    if (tok[i] === "-" || tok[i] === "+") { expNeg = tok[i] === "-"; i++; }
    while (digit()) { if (exp < 100000) { exp = exp * 10 + (tok.charCodeAt(i) - 48); } i++; }
    if (expNeg) { exp = -exp; }
  }
  // the digits with the decimal point after POINT of them, leading and trailing zeros stripped
  let digits = intDigits + fracDigits;
  let point = intDigits.length + exp;
  while (digits.length > 0 && digits[0] === "0") { digits = digits.slice(1); point--; }
  digits = digits.replace(/0+$/, "");
  const signed = d.dlo < 0n;
  const lo = signed ? -(1n << 127n) : 0n, hi = signed ? (1n << 127n) - 1n : (1n << 128n) - 1n;
  let raw = 0n, saturated = false;
  if (digits.length > 0) {
    // the decimal band (wideDecimalBand, 40): above it the value is past 2^128
    // whatever the digits say, below it finer than any F can spell
    if (point > 40) {
      raw = neg ? lo : hi;
      saturated = true;
    } else if (point < -40) {
      report.kindMismatch++;
      return true;
    } else {
      const scale = point - digits.length;
      raw = BigInt(digits) << BigInt(d.frac);
      if (scale >= 0) {
        raw *= 10n ** BigInt(scale);
      } else {
        const den = 10n ** BigInt(-scale);
        if (raw % den !== 0n) { report.kindMismatch++; return true; }
        raw /= den;
      }
      if (neg) { raw = -raw; }
      if (raw < lo) { raw = lo; saturated = true; } else if (raw > hi) { raw = hi; saturated = true; }
    }
  }
  if (saturated) { report.clamped++; }
  if (d.tlo !== null) {
    if (raw < d.tlo) { raw = d.tlo; report.clamped++; }
    else if (raw > d.thi) { raw = d.thi; report.clamped++; }
  }
  // the storage landing: every fixed field declares a range inside its width,
  // so this binds only a wide kind declaring none, whose domain is the 128 above
  if (raw < d.dlo) { raw = d.dlo; report.clamped++; }
  else if (raw > d.dhi) { raw = d.dhi; report.clamped++; }
  holder[key] = d.big ? raw : Number(raw);
  return true;
}

// an integer field takes any token whose VALUE is integral (2, 2.0, 1e3); a
// fractional value is the wrong shape. The declared range clamps first, then
// the storage domain, the wire's order (§4, read.go placeInteger).
function TableVariableTextInt(d, inp, holder, key, report) {
  const n = inp.number();
  if (n === null) { inp.bad = true; return false; }
  if (d.k >= 18 && d.k <= 29) { return TableVariableTextWide(d, n.tok, holder, key, report); }
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
