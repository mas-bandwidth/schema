package jstable

// variablearms.go carries the VARIABLE FORM's maps, unions, optionals and
// enum-keyed arrays in JavaScript (docs/SPEC-TABLES.md §2.3, §2.4, §2.6, §2.8,
// §3, §3.2, §16.2), a port of the Go reference engine (internal/tablewire
// encode.go encodeMap, encodeKeyed, encodeArmHeader, encodeArm; decode.go
// mapField, mapKey, keyed, unionCell, arm; internal/tabletext write.go
// writeMap, writeKeyed, writeScalar; read.go readMap, readKeyed, readUnion),
// held to it byte for byte by TestMapsAndUnionArmsOnTheTableWireJavaScript.
//
// THE STORAGE each construct takes in a generated table class:
//
//	map[K]V     a JavaScript Map from key to value. A string(N) key is a
//	            string; an integer key is a number, or a BigInt at eight bytes.
//	            Entries ride in ASCENDING KEY ORDER whatever the Map's order
//	            (§2.8): byte order of the UTF-8 for a string key.
//	union U     null for None, and { arm: "<arm name>", value } for a set arm:
//	            value is null for a payload-free arm, the arm table's instance
//	            for a table arm, and the arm field line's own storage otherwise.
//	[]U, [..N]U an array of the same, null for a None element (§3).
//	?T          null when ABSENT, and T's own storage when present (§2.3). A
//	            present optional enum holding None is the string "None".
//	[E]T        an array with one slot per keyed variant, slot i keying the
//	            enum's variant i (§2.4); every slot is live.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// keyedSlots renders an enum-keyed array's slots as [variant name, wire id]
// pairs, slot i keying the enum value i + 1 (internal/tabletext
// KeyedSlotValue). A slot past the declared variants (a `| max = K` widening)
// has no name and no wire identity: it is null, and the save and the text
// writer refuse a value that reaches it (§3.2).
func keyedSlots(f *ir.Field) string {
	parts := make([]string, f.ArrayBound)
	for s := range parts {
		if s < len(f.KeyEnumRef.Variants) {
			parts[s] = fmt.Sprintf("[%s, %s]", jsStr(f.KeyEnumRef.Variants[s]), jsId(ir.TableWireId(f.KeyEnumRef.VariantWireName(s))))
		} else {
			parts[s] = "null"
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// keyedElemDesc is a keyed slot's element descriptor: the array element's,
// with the DECLARED DEFAULT the writer elides a slot against (encodeKeyed's
// cellIsDefault) and a reset slot holds.
func (g *variableGen) keyedElemDesc(f *ir.Field) string {
	d := g.elemDesc(f)
	if !f.Type.Pointer && tableOf(f) != nil {
		return d
	}
	return strings.TrimSuffix(d, " }") + ", def: " + g.elemDefaultExpr(f) + " }"
}

// armWireKind is the kind byte an arm header carries (internal/tablewire
// armWireKind): 32 for a payload-free arm (§2.6), 13 for a table body, 17 for
// a pointer, 14 for bytes(N), and otherwise the kind a field of the arm's type
// takes.
func armWireKind(arm ir.UnionVariant) int {
	switch {
	case arm.Void():
		return ir.TableKindNoPayload
	case arm.Body():
		return ir.TableKindTable
	case arm.F.Type.Pointer && arm.F.Array == ir.ArrayNone:
		return ir.TableKindPointer
	case arm.F.Type.Kind == ir.TBytes:
		return ir.TableKindArray
	}
	return ir.TableWireFieldKind(arm.F)
}

// emitUnionDescriptors emits TableVariableUnions: one descriptor per union the
// closure reaches — as a field, an array's element, or a nested arm — naming
// each arm's name, wire id (the arm NAME's hash, §5), header kind, and either
// its table (a body arm) or its field line's descriptor under the property
// "value" (a direct arm, §2.6). fw is the payload width a fixed-width arm's L
// must equal (ir.ArmWireFixedWidth, §3).
func (g *variableGen) emitUnionDescriptors(closure []*ir.Struct) {
	seen := map[string]bool{}
	var order []*ir.Union
	var visit func(f *ir.Field)
	visit = func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.IsMap() {
			for _, ef := range f.MapEntry.Fields {
				visit(ef)
			}
			return
		}
		un := unionOf(f)
		if un == nil || seen[un.Name] {
			return
		}
		seen[un.Name] = true
		for _, arm := range un.Variants {
			if !arm.Void() && !arm.Body() {
				visit(arm.F)
			}
		}
		order = append(order, un)
	}
	for _, st := range closure {
		for _, f := range st.Fields {
			visit(f)
		}
	}
	g.pf("const TableVariableUnions = {\n")
	for _, un := range order {
		g.pf("  %s: {\n", un.Name)
		g.pf("    name: %s,\n", jsStr(un.Name))
		g.pf("    arms: [\n")
		for _, arm := range un.Variants {
			id := jsId(ir.TableWireId(arm.WireName()))
			switch {
			case arm.Void():
				g.pf("      { name: %s, id: %s, k: %d, void: true, body: false, fw: 0 },\n", jsStr(arm.Name), id, armWireKind(arm))
			case arm.Body():
				g.pf("      { name: %s, id: %s, k: %d, void: false, body: true, fw: 0, t: %s },\n", jsStr(arm.Name), id, armWireKind(arm), jsStr(arm.Ref.Name))
			default:
				g.pf("      { name: %s, id: %s, k: %d, void: false, body: false, fw: %d, f: %s },\n",
					jsStr(arm.Name), id, armWireKind(arm), ir.ArmWireFixedWidth(arm.F), g.fieldDescNamed(arm.F, "value", 0, nil))
			}
		}
		g.pf("    ],\n")
		g.pf("  },\n")
	}
	g.pf("};\n\n")
}

// jsVariableArmsRuntime is the walk for the four constructs, called from the
// codec walk's own switches (variableruntime.go).
const jsVariableArmsRuntime = `
// ---------------------------------------------------------------------------
// MAPS, UNIONS, OPTIONALS AND ENUM-KEYED ARRAYS (docs/SPEC-TABLES.md §2.3,
// §2.4, §2.6, §2.8, §3.2)
// ---------------------------------------------------------------------------

// A SAVE'S MAP ENTRIES: one sorted entry object per map, made once per save
// or text write so the numbering, the body walk and a *string slot's identity
// all see the same entry (§3.1)
let TableVariableEntryCache = null;

function TableVariableUtf8Compare(a, b) {
  const x = TableVariableUtf8Encode(a), y = TableVariableUtf8Encode(b);
  const n = Math.min(x.length, y.length);
  for (let i = 0; i < n; i++) { if (x[i] !== y[i]) { return x[i] < y[i] ? -1 : 1; } }
  return x.length === y.length ? 0 : (x.length < y.length ? -1 : 1);
}

// THE KEY ORDER (internal/tabletext MapKeyOrder): byte-wise over the UTF-8 for
// a string key, numeric (signed where declared) for an integer one
function TableVariableKeyOrder(fd, a, b) {
  if (fd.ks) { return TableVariableUtf8Compare(a, b); }
  return a < b ? -1 : (a > b ? 1 : 0);
}

function TableVariableMapEntries(fd, m) {
  if (TableVariableEntryCache !== null) {
    const hit = TableVariableEntryCache.get(m);
    if (hit !== undefined) { return hit; }
  }
  const out = [];
  for (const [k, val] of m) {
    const e = fd.c.make();
    e.key = k;
    e.value = val;
    out.push(e);
  }
  out.sort((a, b) => TableVariableKeyOrder(fd, a.key, b.key));
  if (TableVariableEntryCache !== null) { TableVariableEntryCache.set(m, out); }
  return out;
}

// THE LINK of one descriptor: its table, its union, its element, its slots
function TableVariableLinkDesc(d, codecs, unions) {
  if (d.t !== undefined) { d.c = codecs[d.t]; }
  if (d.un !== undefined) { d.u = unions[d.un]; }
  if (d.e !== undefined) { TableVariableLinkDesc(d.e, codecs, unions); }
  if (d.slots !== undefined) {
    d.slotById = new Map();
    d.slotByName = new Map();
    d.slots.forEach((s, i) => { if (s !== null) { d.slotById.set(s[1], i); d.slotByName.set(s[0], i); } });
  }
}

function TableVariableLinkUnions(codecs, unions) {
  for (const name of Object.keys(unions)) {
    const u = unions[name];
    u.byId = new Map();
    u.byName = new Map();
    for (const arm of u.arms) {
      u.byId.set(arm.id, arm);
      u.byName.set(arm.name, arm);
      if (arm.t !== undefined) { arm.c = codecs[arm.t]; }
      if (arm.f !== undefined) { TableVariableLinkDesc(arm.f, codecs, unions); }
    }
  }
}

// a field's DEFAULT STORAGE, the value a present-but-unplaced optional and a
// fresh arm hold (tabletext FieldDefaultCell, NewArm)
function TableVariableFieldDefault(fd) {
  switch (fd.s) {
    case "bool": case "int": case "flags": case "float": case "str": case "wstr": case "enum": return fd.def;
    case "bytes": return Uint8Array.from(fd.def);
    case "arr": return fd.fixed ? Array.from({ length: fd.bound }, () => TableVariableElemDefault(fd.e)) : [];
    case "tab": return fd.c.make();
    case "map": return new Map();
    case "keyed": return fd.slots.map(() => TableVariableElemDefault(fd.e));
  }
  return null;
}

function TableVariableElemDefault(e) {
  if (e.s === "tab") { return e.c.make(); }
  if (e.s === "union" || e.s === "ptr") { return null; }
  return e.def;
}

function TableVariableElemIsDefault(e, x) {
  // a float compares its bit pattern (§3): a -0 under a +0 default rides
  if (e.s === "float") { return TableVariableSameFloat(e.f32, x, e.def); }
  return x === e.def;
}

// ---- edges (the numbering, §3.1) --------------------------------------------

function TableVariableUnionEdges(u, val, out) {
  if (val === null) { return; }
  const arm = u.byName.get(val.arm);
  if (arm === undefined || arm.void) { return; }
  if (arm.body) { TableVariableEdges(arm.c, val.value, out); return; }
  TableVariableFieldEdges(arm.f, val, out);
}

// ---- the save (encode.go) ---------------------------------------------------

// A PRESENT OPTIONAL ALWAYS RIDES (§2.3): no default elision, an empty array
// and an all-default table included
function TableVariableWritePresent(fd, w, ids, nodes, v, retain, path) {
  const val = v[fd.n];
  switch (fd.s) {
    case "bool": TableVariableHeader(w, ids, fd, 1); w.u8(val ? 1 : 0); return true;
    case "int": case "flags": TableVariableHeader(w, ids, fd, fd.k); TableVariableWriteInt(w, fd.kw, val); return true;
    case "float":
      if (!TableVariableFiniteFloat(fd.f32, val)) { return TableVariableRefuseNonFinite(); }
      TableVariableHeader(w, ids, fd, fd.k);
      return TableVariableWriteFloat(w, fd.f32, val);
    case "str": {
      const b = TableVariableUtf8Encode(val);
      TableVariableHeader(w, ids, fd, 12);
      w.leb(b.length);
      w.raw(b);
      return true;
    }
    case "bytes": {
      TableVariableHeader(w, ids, fd, 14);
      const body = new TableVariableWriter();
      body.u8(6);
      body.leb(val.length);
      body.raw(val);
      w.leb(body.len);
      w.raw(body.b);
      return true;
    }
    case "enum": {
      const ref = ids.ref(fd.id);
      const vref = TableVariableEnumRef(fd, ids, val);
      if (vref < 0) { return false; }
      w.leb(ref); w.u8(30); w.leb(vref);
      return true;
    }
    case "tab": {
      const ref = ids.ref(fd.id);
      const body = new TableVariableWriter();
      path.push({ o: fd.o, i: 0 });
      const ok = TableVariableWriteBody(fd.c, body, ids, nodes, val, retain, path);
      path.pop();
      if (!ok) { return false; }
      w.leb(ref); w.u8(13); w.leb(body.len); w.raw(body.b);
      return true;
    }
    case "arr": {
      const n = TableVariableCount(fd, val);
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

// A MAP (encodeMap): kind 14 over element kind 13, its entries in ascending key
// order, each entry ALWAYS riding (identity is the key); an empty map elides
function TableVariableWriteMap(fd, w, ids, nodes, val, retain, path) {
  if (val.size === 0) { return true; }
  const ents = TableVariableMapEntries(fd, val);
  w.leb(ids.ref(fd.id));
  w.u8(14);
  const body = new TableVariableWriter();
  body.u8(13);
  body.leb(ents.length);
  for (let i = 0; i < ents.length; i++) {
    const eb = new TableVariableWriter();
    path.push({ o: fd.o, i: i });
    const ok = TableVariableWriteBody(fd.c, eb, ids, nodes, ents[i], retain, path);
    path.pop();
    if (!ok) { return false; }
    body.leb(eb.len);
    body.raw(eb.b);
  }
  w.leb(body.len);
  w.raw(body.b);
  return true;
}

// AN ENUM-KEYED ARRAY (encodeKeyed): kind 16, then (key reference, L, element)
// per slot that does not hold its default, ascending by variant; none present
// elides the field and its id
function TableVariableWriteKeyed(fd, w, ids, nodes, val, retain, path) {
  const mark = ids.mark();
  const ref = ids.ref(fd.id);
  const body = new TableVariableWriter();
  let pairs = 0;
  for (let s = 0; s < fd.slots.length; s++) {
    const slot = fd.slots[s];
    if (slot === null) { return false; }
    const sm = ids.mark();
    const kref = ids.ref(slot[1]);
    const x = val[s];
    const eb = new TableVariableWriter();
    if (fd.e.s === "tab") {
      path.push({ o: fd.o, i: s });
      const ok = TableVariableWriteBody(fd.e.c, eb, ids, nodes, x, retain, path);
      path.pop();
      if (!ok) { return false; }
      if (eb.len <= 1) { ids.rollback(sm); continue; }
    } else {
      if (TableVariableElemIsDefault(fd.e, x)) { ids.rollback(sm); continue; }
      if (!TableVariableWriteElement(fd, eb, ids, nodes, x, retain, path, s)) { return false; }
    }
    body.leb(kref);
    body.leb(eb.len);
    body.raw(eb.b);
    pairs++;
  }
  if (pairs === 0) { ids.rollback(mark); return true; }
  w.leb(ref);
  w.u8(16);
  const inner = new TableVariableWriter();
  inner.u8(fd.ek);
  inner.leb(pairs);
  inner.raw(body.b);
  w.leb(inner.len);
  w.raw(inner.b);
  return true;
}

// AN ARM HEADER IS A FIELD HEADER (encodeArmHeader): the arm id reference, the
// arm's kind byte, L, then the arm's payload; an arm no union names refuses
function TableVariableArmWrite(u, w, ids, nodes, val, retain, path, o) {
  const arm = u.byName.get(val.arm);
  if (arm === undefined) { return false; }
  w.leb(ids.ref(arm.id));
  w.u8(arm.k);
  const body = new TableVariableWriter();
  if (!TableVariableArmPayload(arm, body, ids, nodes, val, retain, path, o)) { return false; }
  w.leb(body.len);
  w.raw(body.b);
  return true;
}

// ONE ARM'S PAYLOAD (encodeArm): what a FIELD of the arm's type puts after its
// own framing prefix, never elided
function TableVariableArmPayload(arm, w, ids, nodes, val, retain, path, o) {
  if (arm.void) { return true; }
  if (arm.body) {
    path.push({ o: o, i: 0 });
    const ok = TableVariableWriteBody(arm.c, w, ids, nodes, val.value, retain, path);
    path.pop();
    return ok;
  }
  const f = arm.f, x = val.value;
  switch (f.s) {
    case "bool": w.u8(x ? 1 : 0); return true;
    case "int": case "flags": TableVariableWriteInt(w, f.kw, x); return true;
    case "float": return TableVariableWriteFloat(w, f.f32, x);
    case "str": w.raw(TableVariableUtf8Encode(x)); return true;
    // AN ARM'S L IS THE WSTRING'S BYTE LENGTH (§3's arm payload table): the
    // units ride under it with no length of their own; past N units refuses
    case "wstr": return TableVariableWideWrite(w, x, f.bound);
    case "bytes": w.u8(6); w.leb(x.length); w.raw(x); return true;
    case "enum": {
      const vref = TableVariableEnumRef(f, ids, x);
      if (vref < 0) { return false; }
      w.leb(vref);
      return true;
    }
    case "ptr": w.leb(x === null ? 0 : nodes.index.get(x)); return true;
    case "sblob": case "bblob":
      if (x === null) { w.leb(0); return true; }
      w.leb(f.s === "sblob" && typeof x === "string" ? nodes.slot(val, "value") : nodes.index.get(x));
      return true;
    case "arr": {
      const n = TableVariableCount(f, x);
      w.u8(f.ek);
      w.leb(n);
      for (let i = 0; i < n; i++) {
        if (!TableVariableWriteElement(f, w, ids, nodes, x[i], retain, path, i)) { return false; }
      }
      return true;
    }
    case "union":
      // a nested union's None is L = 1 and that one zero byte (§3)
      if (x === null) { w.leb(0); return true; }
      return TableVariableArmWrite(f.u, w, ids, nodes, x, retain, path, o);
  }
  return false;
}

// ---- the load (decode.go) ---------------------------------------------------

// THE KEY SCAN (mapKey): before an entry's body decodes, its headers are
// scanned for the key's id and the LAST occurrence read; a key under a kind
// the declaration does not take (and does not widen to) is kindBad, and a
// body whose terminator does not consume its L is not ok
function TableVariableMapKeyScan(fd, bytes, at, end, table) {
  const kd = fd.c.fields[0];
  const r = new TableVariableReader(bytes, at, end);
  const rep = new TableVariableReport();
  let key = kd.def, kindBad = false, widened = false;
  for (;;) {
    const ref = r.leb();
    if (r.bad) { return { ok: false, key, kindBad, widened }; }
    if (ref === 0) { return { ok: r.at === r.end, key, kindBad, widened }; }
    if (ref > table.length || r.at >= r.end) { return { ok: false, key, kindBad, widened }; }
    const id = table[ref - 1];
    const kind = r.u8();
    if (id === kd.id) {
      if (kind !== kd.k && TableVariableWidens(kind, kd.k)) {
        widened = true;
        const x = TableVariableScalar(r, kd, kind, rep);
        if (x === undefined) { return { ok: false, key, kindBad, widened }; }
        key = x;
        continue;
      }
      kindBad = kind !== kd.k;
      if (kindBad) {
        if (!TableVariableSkip(r, kind)) { return { ok: false, key, kindBad, widened }; }
        continue;
      }
      if (kd.s === "str") {
        const L = r.leb();
        if (r.bad || r.at + L > r.end) { return { ok: false, key, kindBad, widened }; }
        const dec = TableVariableUtf8Decode(bytes.subarray(r.at, r.at + L));
        if (!dec.ok) { return { ok: false, key, kindBad, widened }; }
        key = dec.s;
        r.at += L;
      } else {
        const x = TableVariableScalar(r, kd, kind, rep);
        if (x === undefined) { return { ok: false, key, kindBad, widened }; }
        key = x;
      }
      continue;
    }
    if (!TableVariableSkip(r, kind)) { return { ok: false, key, kindBad, widened }; }
  }
}

// ONE MAP (mapField): ascending against the last entry that LANDED, a repeated
// key last-wins and counted duplicate, a descending key stops the map with what
// it has and flags malformed, a key past its bound drops its entry and counts
// clamped, and a key kind the reader does not declare empties the map for one
// kind_mismatch. A pointer inside an entry resolves after the whole load, so
// the Map is built again then (TableVariableResolve).
function TableVariableReadMap(fd, r, table, v, report, st, retain, path) {
  const L = r.leb();
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  const end = r.at + L;
  if (L >= 2) {
    // THE HEADER IS READ AGAINST THE PARENT BUFFER, as the oracle reads it
    // (decode.go mapField): a canonical LEB128 is up to ten bytes, so a count
    // spelled wide enough leaves the cursor past the body's own L. The span
    // from the cursor to the body's end is empty rather than negative then
    // (decode.go subTo, docs/SPEC-TABLES.md §3).
    const ek = r.u8();
    const count = r.leb();
    if (r.bad) { report.malformed = true; r.at = end; return true; }
    if (ek !== 13) { report.kindMismatch++; r.at = end; return true; }
    if (retain !== null && path !== null) { TableVariableRetainDiscard(retain, path, true, fd.o); }
    const m = new Map();
    v[fd.n] = m;
    if (count > 0x7fffffff) { report.malformed = true; r.at = end; return true; }
    const sub = new TableVariableReader(r.b, r.at, Math.max(r.at, end));
    const ents = [];
    let last = null, landed = false, widened = false;
    for (let i = 0; i < count; i++) {
      const elen = sub.leb();
      if (sub.bad || sub.at + elen > sub.end) { report.malformed = true; break; }
      const eat = sub.at;
      sub.at += elen;
      const scan = TableVariableMapKeyScan(fd, r.b, eat, eat + elen, table);
      if (scan.widened && !widened) { widened = true; report.widened++; }
      if (scan.kindBad) { report.kindMismatch++; ents.length = 0; break; }
      if (!scan.ok) { report.malformed = true; break; }
      if (fd.ks && TableVariableUtf8Encode(scan.key).length > fd.kb) { report.clamped++; continue; }
      let order = -1;
      if (landed) { order = TableVariableKeyOrder(fd, last, scan.key); }
      if (order > 0) { report.malformed = true; break; }
      const entry = fd.c.make();
      if (path !== null) { path.push({ o: fd.o, i: order === 0 ? ents.length - 1 : ents.length }); }
      TableVariableBodyRead(fd.c, new TableVariableReader(r.b, eat, eat + elen), table, entry, report, st, true, retain, path);
      if (path !== null) { path.pop(); }
      entry.key = scan.key;
      if (order === 0) { ents[ents.length - 1] = entry; report.duplicate++; continue; }
      ents.push(entry);
      last = scan.key;
      landed = true;
    }
    for (const e of ents) { m.set(e.key, e.value); }
    if (st !== null && st.maps !== undefined) { st.maps.push({ m, ents }); }
  }
  r.at = end;
  return true;
}

// ONE KEYED ARRAY (keyed): triples placed BY KEY REFERENCE; a key this reader
// cannot name is skipped by its length and counted unknown, a key reference of
// 0 is damage, and a slot the writer never sent keeps its value
function TableVariableReadKeyed(fd, r, table, v, report, st, retain, path) {
  const L = r.leb();
  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }
  const end = r.at + L;
  if (L < 2) { r.at = end; return true; }
  // the same parent-buffer header as a map's and an array's (decode.go keyed,
  // subTo; docs/SPEC-TABLES.md §3): a count spelled past the body leaves the
  // entry span empty rather than negative
  const ek = r.u8();
  const count = r.leb();
  if (r.bad) { report.malformed = true; r.at = end; return true; }
  const sub = new TableVariableReader(r.b, r.at, Math.max(r.at, end));
  if (ek !== fd.ek) {
    if (!TableVariableWidens(ek, fd.ek)) { report.kindMismatch++; r.at = end; return true; }
    report.widened++;
  }
  const arr = v[fd.n];
  for (let i = 0; i < count; i++) {
    const key = sub.leb();
    if (sub.bad || key === 0 || key > table.length) { report.malformed = true; break; }
    const elen = sub.leb();
    if (sub.bad || sub.at + elen > sub.end) { report.malformed = true; break; }
    const slot = fd.slotById.get(table[key - 1]);
    if (slot === undefined) {
      report.unknown++;
      if (retain !== null) { report.retainLost++; }
      sub.at += elen;
      continue;
    }
    const er = new TableVariableReader(r.b, sub.at, sub.at + elen);
    switch (fd.e.s) {
      case "enum": {
        const x = TableVariableEnumRead(fd.e, er, table, report, retain);
        if (x !== undefined) { arr[slot] = x; }
        break;
      }
      case "tab": {
        let inst = fd.e.c.make();
        if (path !== null) { path.push({ o: fd.o, i: slot }); }
        TableVariableBodyRead(fd.e.c, er, table, inst, report, st, true, retain, path);
        if (path !== null) { path.pop(); }
        if (er.at !== er.end) { report.malformed = true; inst = fd.e.c.make(); }
        arr[slot] = inst;
        break;
      }
      default: {
        const x = TableVariableScalar(er, fd.e, ek, report);
        if (x !== undefined) { arr[slot] = x; }
      }
    }
    sub.at += elen;
  }
  r.at = end;
  return true;
}

// ONE UNION (unionCell): null for None and for an arm this reader does not
// select — an unknown arm (counted unknown), a retyped one (kind_mismatch), an
// arm damaged inside its L (malformed) — and undefined for damage that stops
// the enclosing body
function TableVariableUnionRead(u, r, table, report, st, retain, path, o) {
  // A re-selected arm replaces the records an earlier selection retained here
  // under this field's ordinal (the reference's retainDiscard before the arm).
  if (retain !== null && path !== null) { TableVariableRetainDiscard(retain, path, true, o); }
  const ref = r.leb();
  if (r.bad) { report.malformed = true; return undefined; }
  if (ref === 0) { return null; }
  if (ref > table.length || r.at >= r.end) { report.malformed = true; return undefined; }
  const armId = table[ref - 1];
  const kind = r.u8();
  const n = r.leb();
  if (r.bad || r.at + n > r.end) { report.malformed = true; return undefined; }
  const end = r.at + n;
  const sub = new TableVariableReader(r.b, r.at, end);
  const arm = u.byId.get(armId);
  let out = null;
  if (arm === undefined) {
    report.unknown++;
    if (retain !== null) { report.retainLost++; }
  } else if (kind !== arm.k) {
    if (!arm.void && !arm.body && TableVariableWidens(kind, arm.k)) {
      // WIDENED AT AN ARM: decoded at its own width, and an L that is not that
      // width is the arm's own framing damage
      if (n !== tableRetainWidth(kind)) {
        report.malformed = true;
      } else {
        const x = TableVariableScalar(sub, arm.f, kind, report);
        if (x !== undefined) { out = { arm: arm.name, value: x }; report.widened++; }
      }
    } else {
      report.kindMismatch++;
    }
  } else if (arm.body) {
    const inst = arm.c.make();
    if (path !== null) { path.push({ o: o, i: 0 }); }
    TableVariableBodyRead(arm.c, sub, table, inst, report, st, true, retain, path);
    if (path !== null) { path.pop(); }
    if (sub.at !== end) { report.malformed = true; } else { out = { arm: arm.name, value: inst }; }
  } else {
    out = TableVariableArmRead(arm, sub, table, report, st, retain, path, o);
  }
  r.at = end;
  return out;
}

// ONE DIRECT ARM's payload out of a reader bounded to its L (decode.go arm):
// null where the arm is not selected
function TableVariableArmRead(arm, r, table, report, st, retain, path, o) {
  const n = r.end - r.at;
  if (arm.void) {
    if (n !== 0) { report.malformed = true; return null; }
    return { arm: arm.name, value: null };
  }
  if (arm.fw > 0 && n !== arm.fw) { report.malformed = true; return null; }
  const f = arm.f;
  const holder = { arm: arm.name, value: TableVariableFieldDefault(f) };
  switch (f.s) {
    case "ptr": case "sblob": case "bblob": {
      const index = r.leb();
      if (r.bad || r.at !== r.end) { report.malformed = true; return null; }
      holder.value = null;
      st.pending.push([holder, "value", index, f]);
      return holder;
    }
    case "str": {
      const bytes = r.b.subarray(r.at, r.end);
      const dec = TableVariableUtf8Decode(bytes);
      if (!dec.ok) { report.malformed = true; return null; }
      if (bytes.length > f.bound) {
        report.clamped++;
        let cut = f.bound;
        while (cut > 0 && (bytes[cut] & 0xc0) === 0x80) { cut--; }
        holder.value = TableVariableUtf8Decode(bytes.subarray(0, cut)).s;
      } else {
        holder.value = dec.s;
      }
      return holder;
    }
    case "wstr": {
      // an ODD L or ill-formed UTF-16 at an arm is that arm's own damage: the
      // union reads None, one malformed counts, the parent reads on past L
      // (§3; decode.go arm); past N units clamps, a pair never split
      const s = TableVariableWideDecode(r.b.subarray(r.at, r.end));
      if (s === null) { report.malformed = true; return null; }
      if (s.length > f.bound) { report.clamped++; holder.value = TableVariableWideClamp(s, f.bound); }
      else { holder.value = s; }
      return holder;
    }
    case "bytes": case "arr": {
      const res = TableVariableReadArrayBody(f, r, r.end, table, holder, report, st, retain, path);
      return res ? holder : null;
    }
    case "union": {
      const x = TableVariableUnionRead(f.u, r, table, report, st, retain, path, o);
      if (x === undefined) { return null; }
      holder.value = x;
      return holder;
    }
    case "enum": {
      const x = TableVariableEnumRead(f, r, table, report, retain);
      if (x === undefined) { return null; }
      if (r.at !== r.end) { report.malformed = true; return null; }
      holder.value = x;
      return holder;
    }
  }
  const x = TableVariableScalar(r, f, f.k, report);
  if (x === undefined) { return null; }
  holder.value = x;
  return holder;
}
`

// jsVariableArmsTextRuntime is the text half of the four constructs' walk,
// called from the text walk's own switches (jsVariableTextRuntime). It is
// emitted inside the json-walk markers with that walk (docs/PORTING.md M9).
const jsVariableArmsTextRuntime = `
// ---- the text (write.go, read.go) -------------------------------------------

function TableVariableJsonMap(fd, val, depth, g, out) {
  if (val.size === 0) { out.push("{}"); return true; }
  const ents = TableVariableMapEntries(fd, val);
  const vd = fd.c.fields[1];
  const ind = "\n" + "  ".repeat(depth + 1);
  out.push("{");
  for (let i = 0; i < ents.length; i++) {
    if (i > 0) { out.push(","); }
    out.push(ind + TableVariableJsonString(String(ents[i].key)) + ": ");
    if (!TableVariableJsonField(vd, ents[i].value, depth + 1, g, out)) { return false; }
  }
  out.push("\n" + "  ".repeat(depth) + "}");
  return true;
}

function TableVariableJsonKeyed(fd, val, depth, g, out) {
  if (fd.slots.length === 0) { out.push("{}"); return true; }
  const ind = "\n" + "  ".repeat(depth + 1);
  out.push("{");
  for (let s = 0; s < fd.slots.length; s++) {
    if (fd.slots[s] === null) { return false; }
    if (s > 0) { out.push(","); }
    out.push(ind + TableVariableJsonString(fd.slots[s][0]) + ": ");
    if (!TableVariableJsonScalar(fd.e, val[s], depth + 1, g, out)) { return false; }
  }
  out.push("\n" + "  ".repeat(depth) + "}");
  return true;
}

// a union is an object with ONE key, the arm's name; None is {} (§16.2)
function TableVariableJsonUnion(u, val, depth, g, out) {
  if (val === null) { out.push("{}"); return true; }
  const arm = u.byName.get(val.arm);
  if (arm === undefined) { return false; }
  out.push("{\n" + "  ".repeat(depth + 1) + TableVariableJsonString(arm.name) + ": ");
  if (arm.void) {
    out.push("null");
  } else if (arm.body) {
    if (!TableVariableJsonValue(arm.c, val.value, depth + 1, g, out)) { return false; }
  } else if (!TableVariableJsonField(arm.f, val.value, depth + 1, g, out)) {
    return false;
  }
  out.push("\n" + "  ".repeat(depth) + "}");
  return true;
}

// a map key's text (placeMapKey): a string key is the key; an integer key is
// ONE JSON number, integral in VALUE (2.0 and 1e3 are integers) and inside the
// key kind's range — anything else of that shape is a kind mismatch, and a
// token that is not one JSON number is malformed
function TableVariableMapKeyNumber(kd, s) {
  const m = /^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$/.exec(s);
  if (m === null) { return { status: "malformed" }; }
  const frac = m[3] === undefined ? "" : m[3];
  let digits = BigInt(m[2] + frac);
  let exp = (m[4] === undefined ? 0 : parseInt(m[4], 10)) - frac.length;
  if (digits !== 0n) {
    if (exp > 40) { return { status: "mismatch" }; }
    if (exp >= 0) { digits *= 10n ** BigInt(exp); }
    else {
      if (-exp > 400) { return { status: "mismatch" }; }
      const p = 10n ** BigInt(-exp);
      if (digits % p !== 0n) { return { status: "mismatch" }; }
      digits /= p;
    }
  }
  const x = m[1] === "-" ? -digits : digits;
  if (x < kd.dlo || x > kd.dhi) { return { status: "mismatch" }; }
  return { status: "ok", key: kd.big ? x : Number(x) };
}

// one field read at a holder WITHOUT the key walk's shape gate: what a map's
// value takes (readMap calls readField directly), null placing a pointer's null
function TableVariableTextValue(fd, inp, holder, depth, graph, report) {
  if (inp.shape() === "z" && (fd.s === "ptr" || fd.s === "sblob" || fd.s === "bblob")) {
    if (!inp.literal("null")) { return false; }
    holder[fd.n] = null;
    return true;
  }
  if (inp.shape() !== fd.js) { inp.bad = true; return false; }
  return TableVariableTextField(fd, inp, holder, depth, graph, report);
}

function TableVariableTextMap(fd, inp, v, depth, graph, report) {
  if (inp.peek() !== "{") { inp.bad = true; return false; }
  inp.p++;
  const kd = fd.c.fields[0], vd = fd.c.fields[1];
  const ents = [];
  v[fd.n] = new Map();
  for (;;) {
    let ch = inp.peek();
    if (ch === "}") { inp.p++; break; }
    if (ch === "") { inp.bad = true; return false; }
    const ks = inp.str();
    if (ks === null) { return false; }
    if (inp.peek() !== ":") { inp.bad = true; return false; }
    inp.p++;
    let key = ks, status = "ok";
    if (!fd.ks) {
      const res = TableVariableMapKeyNumber(kd, ks);
      status = res.status;
      key = res.key;
    }
    if (status === "malformed") { report.malformed = true; inp.bad = true; return false; }
    if (status === "mismatch") {
      report.kindMismatch++;
      if (!inp.skip(depth, graph)) { return false; }
    } else if (fd.ks && TableVariableUtf8Encode(key).length > fd.kb) {
      // KEYS NEVER CLAMP: the entry drops whole (§2.8)
      report.clamped++;
      if (!inp.skip(depth, graph)) { return false; }
    } else {
      const entry = fd.c.make();
      entry.key = key;
      if (!TableVariableTextValue(vd, inp, entry, depth, graph, report)) { return false; }
      const at = ents.findIndex((e) => TableVariableKeyOrder(fd, e.key, key) === 0);
      if (at >= 0) { report.duplicate++; ents[at] = entry; } else { ents.push(entry); }
    }
    ch = inp.peek();
    if (ch === ",") { inp.p++; continue; }
    if (ch === "}") { inp.p++; break; }
    inp.bad = true;
    return false;
  }
  ents.sort((a, b) => TableVariableKeyOrder(fd, a.key, b.key));
  for (const e of ents) { v[fd.n].set(e.key, e.value); }
  return true;
}

function TableVariableTextKeyed(fd, inp, v, depth, graph, report) {
  if (inp.peek() !== "{") { inp.bad = true; return false; }
  inp.p++;
  const arr = fd.slots.map(() => TableVariableElemDefault(fd.e));
  v[fd.n] = arr;
  const seen = new Set();
  for (;;) {
    let ch = inp.peek();
    if (ch === "}") { inp.p++; return true; }
    if (ch === "") { inp.bad = true; return false; }
    const key = inp.str();
    if (key === null) { return false; }
    if (inp.peek() !== ":") { inp.bad = true; return false; }
    inp.p++;
    const slot = fd.slotByName.get(key);
    if (slot !== undefined) {
      if (seen.has(slot)) { report.duplicate++; }
      seen.add(slot);
    }
    if (slot === undefined) {
      report.unknown++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else if (inp.shape() !== fd.e.js) {
      report.kindMismatch++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else if (!TableVariableTextScalar(fd.e, inp, arr, slot, depth + 1, graph, report)) {
      return false;
    }
    ch = inp.peek();
    if (ch === ",") { inp.p++; continue; }
    if (ch === "}") { inp.p++; return true; }
    inp.bad = true;
    return false;
  }
}

// a union's text (readUnion): {} is None, one key names the arm, and a second
// key is a text this walk will not guess at
function TableVariableTextUnion(d, inp, holder, key, depth, graph, report) {
  if (inp.peek() !== "{") { inp.bad = true; return false; }
  inp.p++;
  holder[key] = null;
  if (inp.peek() === "}") { inp.p++; return true; }
  const k = inp.str();
  if (k === null) { return false; }
  if (inp.peek() !== ":") { inp.bad = true; return false; }
  inp.p++;
  const arm = d.u.byName.get(k);
  if (arm === undefined) {
    report.unknown++;
    if (!inp.skip(depth + 1, graph)) { return false; }
  } else if (arm.void) {
    if (inp.shape() !== "z") {
      report.kindMismatch++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else {
      if (!inp.literal("null")) { return false; }
      holder[key] = { arm: arm.name, value: null };
    }
  } else if (arm.body) {
    if (inp.shape() !== "o") {
      report.kindMismatch++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else {
      const inst = arm.c.make();
      if (!TableVariableReadTable(arm.c, inp, inst, depth + 1, graph, report)) { return false; }
      holder[key] = { arm: arm.name, value: inst };
    }
  } else {
    const h = { arm: arm.name, value: TableVariableFieldDefault(arm.f) };
    const shape = inp.shape();
    if (shape === "z" && (arm.f.s === "ptr" || arm.f.s === "sblob" || arm.f.s === "bblob")) {
      if (!inp.literal("null")) { return false; }
      h.value = null;
      holder[key] = h;
    } else if (shape !== arm.f.js) {
      report.kindMismatch++;
      if (!inp.skip(depth + 1, graph)) { return false; }
    } else {
      if (!TableVariableTextField(arm.f, inp, h, depth + 1, graph, report)) { return false; }
      holder[key] = h;
    }
  }
  let c = inp.peek();
  if (c === ",") { inp.p++; c = inp.peek(); }
  if (c === "}") { inp.p++; return true; }
  inp.bad = true;
  return false;
}
`
