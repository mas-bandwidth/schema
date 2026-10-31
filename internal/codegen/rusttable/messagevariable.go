// THE MESSAGE FORM OVER A POINTERED ROOT ON THE RUST LEG (docs/SPEC-TABLES.md
// §3.1, §3.3): a VARIABLE root's batch, the three verbs over the variable
// form's own storage structs.
//
// THE BYTES ARE THE REFERENCE's (internal/codegen/cpptable/messagevariable.go)
// and the repository's codec's (internal/tablewire messageencode.go and
// messagedecode.go). A body's NODE TABLE is its FIRST FIELD, because a pointer
// index is `bits_required(0, node count + 1)` wide and the node count rides in
// the node table: the reserved id as a reference, the count at thirty-two raw
// bits, then the records back to back, each a type id reference and then a
// table body ending at its own zero reference, or a blob's thirty-two bit
// length, an align and its bytes. A root that reaches no node writes no node
// table at all. The numbering is the variable form's own (`<root>_number`),
// derived on every write, so measure and save agree without passing anything.
//
// THE STORAGE IS RUST's: a node is an Rc, its identity is the Rc, and a load
// gathers the numbering first, then decodes a record the first time a slot
// names it and shares it with every later slot, exactly as the variable form's
// file read does (TableNodeReader, variable.go). Every placeable record is
// decoded whether or not a slot names it, so its body's events count as the
// oracle counts them.
//
// THE CARRIED SET is a closure whose every field is a scalar of at most 64
// bits, an enum, a string(N), a bytes(N), a table by value, an optional table,
// enum or scalar, a pointer to a table, a *string or *bytes blob, or a fixed or
// counted array of scalars, enums, tables or pointers, with no `if` group. A
// variable root whose closure reaches anything else grows no message verb, and
// a caller meets a missing function, never a wrong bit.
package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// VariableMessageRuntimeModule is the unit's one home for the pointered
// batch's shared runtime: the index-aware skip, the node table read, the node
// build dispatch and the record write dispatch.
const VariableMessageRuntimeModule = "vmessage_runtime"

// vmsgFieldOK reports whether one field is in the carried set above.
func vmsgFieldOK(f *ir.Field) bool {
	switch {
	case f.IsMap(), f.KeyEnum != "", f.Type.Kind == ir.TWString, isUnionRef(f.Type), isFlags(f) != nil:
		return false
	case f.Type.TextUnbounded():
		return false
	case (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Width > 64:
		return false
	case f.Type.Blob():
		return f.Array == ir.ArrayNone && !f.Type.Optional
	case f.Type.Pointer:
		return !f.Type.Optional && (f.Array == ir.ArrayNone || f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted)
	case f.Type.Optional:
		return f.Array == ir.ArrayNone && (isStructRef(f.Type) || msgEnum(f) != nil || classOf(f) == vScalar)
	case f.Array != ir.ArrayNone:
		if f.Array != ir.ArrayFixed && f.Array != ir.ArrayCounted {
			return false
		}
		if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
			return false
		}
		return isStructRef(f.Type) || msgEnum(f) != nil || classOf(f) == vScalar
	}
	return true
}

// vmsgClosure is every struct one root reaches, by value, by element or by
// pointer, the root first; false when any of them is outside the carried set.
func vmsgClosure(u *ir.Unit, vc *variableClass, slots map[string]uint64, root *ir.Struct) ([]*ir.Struct, bool) {
	seen := map[string]bool{}
	var out []*ir.Struct
	ok := true
	var walk func(st *ir.Struct)
	walk = func(st *ir.Struct) {
		if seen[st.Name] || !ok {
			return
		}
		seen[st.Name] = true
		out = append(out, st)
		if !vc.emitted[st.Name] || !vc.carried(st.Name) || len(guardConds(st, "v")) > 0 {
			ok = false
			return
		}
		for _, f := range st.Fields {
			if !vmsgFieldOK(f) {
				ok = false
				return
			}
			if _, known := slots[ir.TableFieldEntry(f).Key()]; !known {
				ok = false
				return
			}
			if isStructRef(f.Type) {
				ref := vStruct(u, f.Type.Name)
				if ref == nil {
					ok = false
					return
				}
				walk(ref)
			}
		}
	}
	walk(root)
	return out, ok
}

// vmsgPlan is one unit's carried pointered roots, and every struct a walker
// is emitted for.
type vmsgPlan struct {
	roots   []*ir.Struct
	members map[string]bool
	targets []*ir.Struct // every table a node record can be, over all roots
}

// variableMessagePlan decides the carried set for a unit that carries both
// the variable form and the message form's announcement.
func variableMessagePlan(u *ir.Unit, vc *variableClass) *vmsgPlan {
	slots := ir.TableVocabularySlots(u)
	nodeSlot := vocabNameSlot(u, ir.TableNodeWireId)
	if nodeSlot == 0 || vocabNameSlot(u, ir.BytesWireTypeId) == 0 || vocabNameSlot(u, ir.StringWireTypeId) == 0 {
		return nil
	}
	p := &vmsgPlan{members: map[string]bool{}}
	targets := map[string]*ir.Struct{}
	names := make([]string, 0, len(u.Tables))
	for n := range u.Tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := u.Tables[n]
		if !st.IsTable || st.FixedDeclared || st.IsMapEntry() || !ir.TableClosure(u)[n] {
			continue
		}
		closure, ok := vmsgClosure(u, vc, slots, st)
		if !ok {
			continue
		}
		reach := ir.PointerReachable(st)
		good := true
		for _, t := range reach {
			if vocabNameSlot(u, ir.TableWireId(t.WireName())) == 0 {
				good = false
			}
		}
		if !good {
			continue
		}
		p.roots = append(p.roots, st)
		for _, c := range closure {
			p.members[c.Name] = true
		}
		for _, t := range reach {
			targets[t.Name] = t
		}
	}
	if len(p.roots) == 0 {
		return nil
	}
	tn := make([]string, 0, len(targets))
	for n := range targets {
		tn = append(tn, n)
	}
	sort.Strings(tn)
	for _, n := range tn {
		p.targets = append(p.targets, targets[n])
	}
	return p
}

// vocabNameSlot is the own-vocabulary slot of a NAME entry (kind 0), 0 when
// the vocabulary carries none.
func vocabNameSlot(u *ir.Unit, id uint64) uint64 {
	for i, e := range ir.TableVocabulary(u) {
		if e.Id == id && e.Kind == 0 {
			return uint64(i + 1)
		}
	}
	return 0
}

// ---- the unit runtime ------------------------------------------------------

func variableMessageRuntimeModule(u *ir.Unit, p *vmsgPlan) []byte {
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package, "THE MESSAGE FORM over a pointered root: the node table and its dispatch (docs/SPEC-TABLES.md §3.1, §3.3)"))
	b.WriteString(`#![allow(unused_imports)]
#![allow(clippy::all)]

use crate::tables::*;
use std::any::Any;
use std::rc::Rc;

`)
	b.WriteString(vmsgRuntime)
	// the type id each node record names, as a reference into this unit's
	// own vocabulary (§3.3)
	fmt.Fprintf(&b, "/// One node record on the wire (§3.1, §3.3): the type id's reference, then a\n")
	fmt.Fprintf(&b, "/// table's body to its zero reference, or a blob's thirty-two bit length, an\n")
	fmt.Fprintf(&b, "/// align and its bytes.\n")
	fmt.Fprintf(&b, "pub fn table_message_record_save(w: &mut TableBitWriter, g: &TableNodeGraph, ib: u32, rec: &TableNodeRecord) -> bool {\n")
	fmt.Fprintf(&b, "    match rec {\n")
	fmt.Fprintf(&b, "        TableNodeRecord::Blob(tid, bytes) => {\n")
	fmt.Fprintf(&b, "            let slot = if *tid == BYTES_TYPE_ID { %d } else if *tid == STRING_TYPE_ID { %d } else { return false };\n",
		vocabNameSlot(u, ir.BytesWireTypeId), vocabNameSlot(u, ir.StringWireTypeId))
	fmt.Fprintf(&b, "            w.put(slot, TABLE_MESSAGE_REF_BITS);\n")
	fmt.Fprintf(&b, "            w.put(bytes.len() as u64, 32);\n")
	fmt.Fprintf(&b, "            w.align();\n")
	fmt.Fprintf(&b, "            w.raw(bytes);\n")
	fmt.Fprintf(&b, "            true\n")
	fmt.Fprintf(&b, "        }\n")
	fmt.Fprintf(&b, "        TableNodeRecord::Table(tid, node) => match *tid {\n")
	for _, t := range p.targets {
		fmt.Fprintf(&b, "            0x%016x => {\n", ir.TableWireId(t.WireName()))
		fmt.Fprintf(&b, "                w.put(%d, TABLE_MESSAGE_REF_BITS);\n", vocabNameSlot(u, ir.TableWireId(t.WireName())))
		fmt.Fprintf(&b, "                match node.downcast_ref::<%s>() { Some(v) => %s(w, g, ib, v), None => false }\n", t.Name, fn(t.Name, "message_vsave"))
		fmt.Fprintf(&b, "            }\n")
	}
	fmt.Fprintf(&b, "            _ => false,\n")
	fmt.Fprintf(&b, "        },\n")
	fmt.Fprintf(&b, "    }\n")
	fmt.Fprintf(&b, "}\n\n")

	fmt.Fprintf(&b, "/// One table record decoded into its own storage the first time a slot names\n")
	fmt.Fprintf(&b, "/// it, and shared by every later slot (§3.1). A record naming itself while\n")
	fmt.Fprintf(&b, "/// its own body decodes is a cycle this storage cannot hold: that slot reads\n")
	fmt.Fprintf(&b, "/// null, as the variable form's file read answers it.\n")
	fmt.Fprintf(&b, "pub fn table_message_node_build(voc: &TableVocabulary, nodes: &mut TableMessageNodes, k: usize, report: &mut TableReport) -> Option<Rc<dyn Any>> {\n")
	fmt.Fprintf(&b, "    match &nodes.reader.slots[k] {\n")
	fmt.Fprintf(&b, "        TableNodeSlot::Built(_, n) => return Some(n.clone()),\n")
	fmt.Fprintf(&b, "        TableNodeSlot::Table(_) => {}\n")
	fmt.Fprintf(&b, "        TableNodeSlot::Building => { report.cycle_on_save = true; return None; }\n")
	fmt.Fprintf(&b, "        _ => return None,\n")
	fmt.Fprintf(&b, "    }\n")
	fmt.Fprintf(&b, "    let (tid, start, _) = nodes.reader.recs[k];\n")
	fmt.Fprintf(&b, "    nodes.reader.slots[k] = TableNodeSlot::Building;\n")
	fmt.Fprintf(&b, "    let mut r = TableBitReader::new(nodes.buffer);\n")
	fmt.Fprintf(&b, "    r.offset = start as u64;\n")
	fmt.Fprintf(&b, "    // PASS ONE counted this body's events already: these land in the sink\n")
	fmt.Fprintf(&b, "    let mut sink = core::mem::take(&mut nodes.sink);\n")
	fmt.Fprintf(&b, "    let node: Option<Rc<dyn Any>> = match tid {\n")
	for _, t := range p.targets {
		fmt.Fprintf(&b, "        0x%016x => {\n", ir.TableWireId(t.WireName()))
		fmt.Fprintf(&b, "            let mut v = %s::default();\n", t.Name)
		fmt.Fprintf(&b, "            let _ = %s(&mut r, voc, &mut sink, nodes, &mut v);\n", fn(t.Name, "message_vload"))
		fmt.Fprintf(&b, "            Some(Rc::new(v))\n")
		fmt.Fprintf(&b, "        }\n")
	}
	fmt.Fprintf(&b, "        _ => None,\n")
	fmt.Fprintf(&b, "    };\n")
	fmt.Fprintf(&b, "    sink.cycle_on_save |= nodes.sink.cycle_on_save;\n")
	fmt.Fprintf(&b, "    nodes.sink = sink;\n")
	fmt.Fprintf(&b, "    nodes.reader.slots[k] = match &node { Some(n) => TableNodeSlot::Built(tid, n.clone()), None => TableNodeSlot::Null };\n")
	fmt.Fprintf(&b, "    node\n")
	fmt.Fprintf(&b, "}\n\n")

	fmt.Fprintf(&b, "/// PASS ONE over one table record (§3.1, §3.3): its body decoded into scratch\n")
	fmt.Fprintf(&b, "/// in wire order, its events counted, its pointers resolved and not built.\n")
	fmt.Fprintf(&b, "/// False is damage inside the record, terminal for the batch.\n")
	fmt.Fprintf(&b, "pub fn table_message_node_count(voc: &TableVocabulary, nodes: &mut TableMessageNodes, k: usize, report: &mut TableReport) -> bool {\n")
	fmt.Fprintf(&b, "    let (tid, start, _) = nodes.reader.recs[k];\n")
	fmt.Fprintf(&b, "    let mut r = TableBitReader::new(nodes.buffer);\n")
	fmt.Fprintf(&b, "    r.offset = start as u64;\n")
	fmt.Fprintf(&b, "    match tid {\n")
	for _, t := range p.targets {
		fmt.Fprintf(&b, "        0x%016x => { let mut v = %s::default(); %s(&mut r, voc, report, nodes, &mut v) }\n", ir.TableWireId(t.WireName()), t.Name, fn(t.Name, "message_vload"))
	}
	fmt.Fprintf(&b, "        _ => true,\n")
	fmt.Fprintf(&b, "    }\n")
	fmt.Fprintf(&b, "}\n\n")
	fmt.Fprintf(&b, "/// The node-table reference's own slot in this unit's vocabulary (§3.1, §3.3).\n")
	fmt.Fprintf(&b, "pub const TABLE_MESSAGE_NODE_TABLE_SLOT: u64 = %d;\n", vocabNameSlot(u, ir.TableNodeWireId))
	return []byte(b.String())
}

// vmsgRuntime is the part of the runtime that names no type.
const vmsgRuntime = `/// A pointered body's node numbering on the READ side (§3.1, §3.3): the
/// variable form's TableNodeReader over BIT offsets into the batch and the
/// index width the node count settles. A body is read in TWO passes, so its
/// counters are the oracle's in the oracle's order: PASS ONE decodes every
/// placeable record in wire order into scratch, resolving each pointer slot's
/// index without building (build false), and its events count; PASS TWO
/// decodes the root and builds each record the first time a slot names it,
/// the records' events landing in the sink, which is read for a cycle alone.
pub struct TableMessageNodes<'a> {
    pub reader: TableNodeReader,
    pub buffer: &'a [u8],
    pub index_bits: u32,
    pub build: bool,
    pub sink: TableReport,
}

impl<'a> TableMessageNodes<'a> {
    pub fn new(buffer: &'a [u8], root_type: u64) -> Self {
        TableMessageNodes {
            reader: TableNodeReader { good: true, root_type, retaining: false, retain_capacity: 0, retained: Vec::new(), recs: Vec::new(), slots: Vec::new(), blob_named: Vec::new() },
            buffer,
            // a root that reaches no node numbers none, and an index is then
            // bits_required(0, 1) wide (§3.3)
            index_bits: table_message_bits(1),
            build: false,
            sink: TableReport::default(),
        }
    }
}

/// table_message_skip with a node index width: a kind-17 value is an index
/// at the body's own width (§3.3).
pub fn table_message_vskip(r: &mut TableBitReader, v: &TableVocabulary, e: &TableMessageEntry, depth: u32, ib: u32) -> bool {
    if depth > 128 {
        return false;
    }
    let s = &e.shape;
    match e.kind {
        13 => table_message_vskip_body(r, v, depth + 1, ib),
        17 => ib > 0 && r.skip(ib as u64),
        15 => match r.get(v.ref_bits) {
            None => false,
            Some(0) => true,
            Some(reference) => match v.entry(reference).copied() {
                Some(arm) => arm.id < 0xffff_ffff_ffff_fffd && arm.kind != 0 && table_message_vskip(r, v, &arm, depth + 1, ib),
                None => false,
            },
        },
        14 | 16 => {
            let element = TableMessageEntry { id: 0, kind: s.elem, shape: e.element, element: TableMessageShape::default() };
            if s.elem != 13 && s.elem != 15 && s.elem != 17 {
                return table_message_skip(r, v, e, depth);
            }
            let Some(n) = table_message_count(r, s) else { return false };
            for _ in 0..n {
                if e.kind == 16 && !r.skip(v.ref_bits as u64) {
                    return false;
                }
                if !table_message_vskip(r, v, &element, depth + 1, ib) {
                    return false;
                }
            }
            true
        }
        _ => table_message_skip(r, v, e, depth),
    }
}

/// A nested body skipped whole, with a node index width (§3.3).
pub fn table_message_vskip_body(r: &mut TableBitReader, v: &TableVocabulary, depth: u32, ib: u32) -> bool {
    if depth > 128 {
        return false;
    }
    loop {
        match r.get(v.ref_bits) {
            None => return false,
            Some(0) => return true,
            Some(reference) => match v.entry(reference).copied() {
                Some(e) => {
                    if !table_message_vskip(r, v, &e, depth, ib) {
                        return false;
                    }
                }
                None => return false,
            },
        }
    }
}

/// The ROOT body's first field when it is the node table (§3.1, §3.3): PASS
/// ONE walks the records to learn what each node is and where its bits are, a
/// table record stepped over by its announced shapes and a blob by its length.
/// A record whose type id this root cannot place keeps its index, counts one
/// unknown, and every slot naming it reads null; a *string blob that is not
/// well-formed text is damage, terminal for the batch. False is damage, with
/// malformed set.
#[allow(clippy::too_many_arguments)]
pub fn table_message_node_table(r: &mut TableBitReader, voc: &TableVocabulary, report: &mut TableReport, nodes: &mut TableMessageNodes, placeable: &[u64], bytes_edge: bool, string_edge: bool, retaining: bool) -> bool {
    let at = r.offset;
    let Some(reference) = r.get(voc.ref_bits) else { report.malformed = true; return false; };
    match voc.entry(reference) {
        None => {
            if reference != 0 { report.malformed = true; return false; }
            r.offset = at;
            return true;
        }
        Some(e) if e.id != TABLE_NODE_TABLE_ID => {
            // a pointered root that reaches no node writes no node table
            r.offset = at;
            return true;
        }
        _ => {}
    }
    let Some(count) = r.get(32) else { report.malformed = true; return false; };
    nodes.index_bits = table_message_bits(count + 1);
    let mut recs: Vec<(u64, usize, usize)> = Vec::new();
    for _ in 0..count {
        let Some(tref) = r.get(voc.ref_bits) else { report.malformed = true; return false; };
        let Some(tid) = voc.name(tref) else { report.malformed = true; return false; };
        if tid == BYTES_TYPE_ID || tid == STRING_TYPE_ID {
            let Some(n) = r.get(32) else { report.malformed = true; return false; };
            if !r.align() { report.malformed = true; return false; }
            let start = r.offset;
            if !r.skip(n * 8) { report.malformed = true; return false; }
            recs.push((tid, start as usize, r.offset as usize));
            continue;
        }
        let start = r.offset;
        if !table_message_vskip_body(r, voc, 0, nodes.index_bits) { report.malformed = true; return false; }
        recs.push((tid, start as usize, r.offset as usize));
    }
    let mut slots = Vec::with_capacity(recs.len());
    for &(tid, start, end) in &recs {
        if tid == BYTES_TYPE_ID || tid == STRING_TYPE_ID {
            let placed = if tid == BYTES_TYPE_ID { bytes_edge } else { string_edge };
            if !placed {
                report.unknown += 1;
                if retaining { report.retain_lost += 1; }
                slots.push(TableNodeSlot::Null);
                continue;
            }
            let bytes = &nodes.buffer[start / 8..end / 8];
            if tid == STRING_TYPE_ID {
                if !text_valid(bytes) { report.malformed = true; return false; }
                slots.push(TableNodeSlot::Text(Rc::from(std::str::from_utf8(bytes).unwrap_or(""))));
            } else {
                slots.push(TableNodeSlot::Bytes(Rc::from(bytes)));
            }
            continue;
        }
        if placeable.contains(&tid) {
            slots.push(TableNodeSlot::Table(tid));
        } else {
            report.unknown += 1;
            if retaining { report.retain_lost += 1; }
            slots.push(TableNodeSlot::Null);
        }
    }
    nodes.reader.recs = recs;
    nodes.reader.slots = slots;
    nodes.reader.good = true;
    true
}

`

// ---- the per-file module ---------------------------------------------------

// variableMessageModule is <base>_vmessage.rs: the walkers of every struct
// this file declares that a carried pointered root reaches, and the batch
// verbs of every carried root it declares.
func (g *gen) variableMessageModule(p *vmsgPlan) []byte {
	var members []*ir.Struct
	for _, st := range g.cookRoots() {
		if p.members[st.Name] {
			members = append(members, st)
		}
	}
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok && p.members[st.Name] && !st.IsTable {
			dup := false
			for _, m := range members {
				if m == st {
					dup = true
				}
			}
			if !dup {
				members = append(members, st)
			}
		}
	}
	if len(members) == 0 {
		return nil
	}
	m := &msgGen{u: g.unit, slots: ir.TableVocabularySlots(g.unit)}
	m.pf("%s", header(g.file.Base, g.unit.Package, "THE MESSAGE FORM over a pointered root: the bodies and the batch (docs/SPEC-TABLES.md §3.1, §3.3)"))
	m.pf(`#![allow(unused_imports)]
#![allow(unused_variables)]
#![allow(unused_mut)]
#![allow(unused_assignments)]
#![allow(unused_labels)]
#![allow(unused_comparisons)]
#![allow(clippy::all)]

use crate::tables::*;
use std::rc::Rc;

`)
	isRoot := map[string]bool{}
	for _, r := range p.roots {
		isRoot[r.Name] = true
	}
	for _, st := range members {
		m.emitVSave(st)
		m.emitVLoad(st)
		if isRoot[st.Name] {
			m.emitVBatch(st)
		}
	}
	return []byte(m.b.String())
}

// ---- the write -------------------------------------------------------------

func (m *msgGen) emitVSave(st *ir.Struct) {
	m.pf("/// One %s body on the message wire (§3.3): each field off its declared\n/// default, a pointer as its node index at the body's width `ib`, then the\n/// zero reference.\n", st.Name)
	m.pf("pub fn %s(w: &mut TableBitWriter, g: &TableNodeGraph, ib: u32, v: &%s) -> bool {\n", fn(st.Name, "message_vsave"), st.Name)
	for _, f := range st.Fields {
		m.pf("    {\n")
		m.vsaveField(f, "v."+ir.RustSnake(f.Name), "        ")
		m.pf("    }\n")
	}
	m.pf("    w.put(0, TABLE_MESSAGE_REF_BITS);\n    !w.overflow\n}\n\n")
}

func vmsgElemDefault(f *ir.Field) string {
	switch {
	case f.Type.Pointer:
		return "None"
	case isStructRef(f.Type):
		return f.Type.Name + "::default()"
	case msgEnum(f) != nil:
		return enumDefault(f)
	}
	return scalarDefault(f)
}

func (m *msgGen) vsaveField(f *ir.Field, x, ind string) {
	entry := ir.TableFieldEntry(f)
	slot := m.slot(entry)
	ref := fmt.Sprintf("w.put(%d, TABLE_MESSAGE_REF_BITS);", slot)
	switch {
	case f.Type.Blob():
		m.pf("%sif let Some(n) = &%s {\n%s    %s\n%s    w.put(g.blob_index(Rc::as_ptr(n) as *const u8 as usize), ib);\n%s}\n", ind, x, ind, ref, ind, ind)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		m.pf("%sif let Some(n) = &%s {\n%s    %s\n%s    w.put(g.node_index(n), ib);\n%s}\n", ind, x, ind, ref, ind, ind)
	case f.Type.Optional:
		e := msgElement(f)
		m.pf("%sif let Some(o) = &%s {\n%s    %s\n", ind, x, ind, ref)
		m.vsaveValue(e, entry, "(*o)", "o", ind+"    ")
		m.pf("%s}\n", ind)
	case f.Type.Kind == ir.TString:
		m.pf("%sif !%s.is_empty() {\n", ind, x)
		m.pf("%s    if %s.len() > %d { return false; }\n", ind, x, f.Type.Size)
		m.pf("%s    %s\n%s    w.put(%s.len() as u64, %d);\n%s    w.align();\n%s    w.raw(%s.as_bytes());\n%s}\n",
			ind, ref, ind, x, ir.TableMessageBitsRequired(0, entry.Shape.Max), ind, ind, x, ind)
	case f.Type.Kind == ir.TBytes:
		m.pf("%sif !%s.is_empty() {\n", ind, x)
		m.pf("%s    if %s.len() > %d { return false; }\n", ind, x, f.Type.Size)
		m.pf("%s    %s\n%s    w.put(%s.len() as u64, %d);\n%s    w.align();\n%s    w.raw(&%s);\n%s}\n",
			ind, ref, ind, x, ir.TableMessageCountBits(entry.Shape), ind, ind, x, ind)
	case f.Array == ir.ArrayCounted:
		m.pf("%sif !%s.is_empty() {\n", ind, x)
		m.pf("%s    %s\n", ind, ref)
		m.pf("%s    let count = %s.len().min(%d);\n", ind, x, f.ArrayBound)
		m.vsaveArrayBody(f, entry, x, "count", ind+"    ")
		m.pf("%s}\n", ind)
	case f.Array == ir.ArrayFixed:
		e := msgElement(f)
		m.pf("%slet dflt: %s = %s;\n", ind, elemRust(f), vmsgElemDefault(e))
		if f.Type.Pointer || !isStructRef(f.Type) {
			cond := "*e != dflt"
			if f.Type.Pointer {
				cond = "e.is_some()"
			}
			m.pf("%sif (0..%d).any(|i| { let e = %s.get(i).unwrap_or(&dflt); %s }) {\n", ind, f.ArrayBound, x, cond)
		} else {
			m.pf("%s{\n", ind)
		}
		m.pf("%s    %s\n", ind, ref)
		m.vsaveArrayBody(f, entry, x, fmt.Sprint(f.ArrayBound), ind+"    ")
		m.pf("%s}\n", ind)
	case isStructRef(f.Type):
		m.pf("%slet mut probe = TableBitWriter::measure();\n", ind)
		m.pf("%sif !%s(&mut probe, g, ib, &%s) { return false; }\n", ind, fn(f.Type.Name, "message_vsave"), x)
		m.pf("%sif probe.bits > TABLE_MESSAGE_REF_BITS as u64 {\n%s    %s\n%s    if !%s(w, g, ib, &%s) { return false; }\n%s}\n",
			ind, ind, ref, ind, fn(f.Type.Name, "message_vsave"), x, ind)
	case msgEnum(f) != nil:
		m.pf("%sif %s != %s {\n%s    %s\n", ind, x, enumDefault(f), ind, ref)
		m.vsaveEnum(f, x, ind+"    ")
		m.pf("%s}\n", ind)
	default:
		rides := m.rides(f, x, scalarDefault(f), ind)
		m.pf("%sif %s {\n%s    %s\n", ind, rides, ind, ref)
		m.writeValue(f, entry, x, "", ind+"    ")
		m.pf("%s}\n", ind)
	}
}

// vsaveArrayBody is an array's count (none for a fixed array) and its
// elements, `count` of them, a fixed array's missing tail at its defaults.
func (m *msgGen) vsaveArrayBody(f *ir.Field, entry ir.TableVocabularyEntry, x, count, ind string) {
	if bits := ir.TableMessageCountBits(entry.Shape); bits > 0 {
		m.pf("%sw.put((%s as u64).wrapping_sub(%d), %d);\n", ind, count, entry.Shape.Min, bits)
	}
	if ir.TableMessageAligns(entry.Kind, entry.Shape) {
		m.pf("%sw.align();\n", ind)
	}
	e := msgElement(f)
	if f.Array == ir.ArrayFixed {
		m.pf("%sfor i in 0..%s {\n%s    let e = %s.get(i).unwrap_or(&dflt);\n", ind, count, ind, x)
	} else {
		m.pf("%sfor e in %s.iter().take(%s) {\n", ind, x, count)
	}
	inner := ind + "    "
	switch {
	case f.Type.Pointer:
		m.pf("%sw.put(match e { Some(n) => g.node_index(n), None => 0 }, ib);\n", inner)
	case isStructRef(f.Type):
		m.pf("%sif !%s(w, g, ib, e) { return false; }\n", inner, fn(f.Type.Name, "message_vsave"))
	case msgEnum(f) != nil:
		m.vsaveEnum(e, "(*e)", inner)
	default:
		m.writeValue(e, msgElementEntry(entry), "(*e)", "", inner)
	}
	m.pf("%s}\n", ind)
}

// vsaveValue is an optional's present value: a body, an enum or a scalar.
func (m *msgGen) vsaveValue(e *ir.Field, entry ir.TableVocabularyEntry, x, ref, ind string) {
	switch {
	case isStructRef(e.Type):
		m.pf("%sif !%s(w, g, ib, %s) { return false; }\n", ind, fn(e.Type.Name, "message_vsave"), ref)
	case msgEnum(e) != nil:
		m.vsaveEnum(e, x, ind)
	default:
		m.writeValue(e, entry, x, "", ind)
	}
}

// vsaveEnum is a variant name's reference, the zero reference for None.
func (m *msgGen) vsaveEnum(f *ir.Field, x, ind string) {
	en := msgEnum(f)
	m.pf("%smatch %s {\n%s    %s::None => w.put(0, TABLE_MESSAGE_REF_BITS),\n", ind, x, ind, en.Name)
	for i, v := range en.Variants {
		s := m.nameSlot(ir.TableWireId(en.VariantWireName(i)))
		if s == 0 {
			m.pf("%s    %s::%s => return false,\n", ind, en.Name, rustVariant(v))
			continue
		}
		m.pf("%s    %s::%s => w.put(%d, TABLE_MESSAGE_REF_BITS),\n", ind, en.Name, rustVariant(v), s)
	}
	m.pf("%s}\n", ind)
}

// ---- the read --------------------------------------------------------------

func (m *msgGen) emitVLoad(st *ir.Struct) {
	m.pf("/// One %s body against the ENDPOINT's vocabulary (§3.3), its pointers\n/// resolved against the body's numbering (§3.1).\n", st.Name)
	m.pf("pub fn %s(r: &mut TableBitReader, v: &TableVocabulary, report: &mut TableReport, nodes: &mut TableMessageNodes, value: &mut %s) -> bool {\n", fn(st.Name, "message_vload"), st.Name)
	m.pf("    *value = %s::default();\n", st.Name)
	m.pf("    loop {\n")
	m.pf("        let Some(reference) = r.get(v.ref_bits) else %s;\n", msgBad)
	m.pf("        if reference == 0 { return true; }\n")
	m.pf("        let Some(entry) = v.entry(reference).copied() else %s;\n", msgBad)
	m.pf("        if entry.id >= 0xffff_ffff_ffff_fffd %s\n", msgBad)
	m.pf("        match entry.id {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		ind := "                "
		x := "value." + ir.RustSnake(f.Name)
		m.pf("            0x%016x => {\n", mine.Id)
		label := m.tmp("'f")
		m.pf("%slet mut widened = false;\n%s%s: {\n", ind, ind, label)
		m.vcompat(f, mine, "entry", label, ind+"    ")
		m.vloadField(f, x, "entry", ind+"    ")
		m.pf("%s    if widened { report.widened += 1; }\n%s}\n", ind, ind)
		m.pf("            }\n")
	}
	m.pf("            _ => {\n                report.unknown += 1;\n                if !table_message_vskip(r, v, &entry, 0, nodes.index_bits) %s\n            }\n", msgBad)
	m.pf("        }\n    }\n}\n\n")
}

// vcompat is compat with the index-aware skip.
func (m *msgGen) vcompat(f *ir.Field, mine ir.TableVocabularyEntry, entry, label, ind string) {
	compat := ""
	if wireWidenable(int(mine.Kind)) {
		compat = fmt.Sprintf("%s.shape.elem == %d && table_message_widens(%s.kind, %d)", entry, mine.Shape.Elem, entry, mine.Kind)
	}
	if mine.Kind == 14 && wireWidenable(int(mine.Shape.Elem)) {
		compat = fmt.Sprintf("%s.kind == 14 && table_message_widens(%s.shape.elem, %d)", entry, entry, mine.Shape.Elem)
	}
	m.pf("%sif %s.kind != %d || %s.shape.elem != %d {\n", ind, entry, mine.Kind, entry, mine.Shape.Elem)
	inner := ind + "    "
	if compat != "" {
		m.pf("%sif %s {\n%s    widened = true;\n%s} else {\n", inner, compat, inner, inner)
		inner += "    "
	}
	m.pf("%sreport.kind_mismatch += 1;\n%sif !table_message_vskip(r, v, &%s, 0, nodes.index_bits) %s\n%sbreak %s;\n", inner, inner, entry, msgBad, inner, label)
	if compat != "" {
		m.pf("%s    }\n", ind)
	}
	m.pf("%s}\n", ind)
}

// vresolve is one node index read at the body's width and resolved against
// the numbering into `dst`.
func (m *msgGen) vresolve(f *ir.Field, dst, ind string) {
	m.pf("%slet Some(index) = r.get(nodes.index_bits) else %s;\n", ind, msgBad)
	if f.Type.Blob() {
		if f.Type.Kind == ir.TString {
			m.pf("%s%s = nodes.reader.text_slot(index, report);\n", ind, dst)
		} else {
			m.pf("%s%s = nodes.reader.bytes_slot(index, report);\n", ind, dst)
		}
		return
	}
	tid := ir.TableWireId(f.Type.Ref.(*ir.Struct).WireName())
	m.pf("%s%s = match nodes.reader.node_slot(index, 0x%016x, report) {\n", ind, dst, tid)
	m.pf("%s    Some(k) if nodes.build => table_message_node_build(v, nodes, k, report).and_then(|n| n.downcast::<%s>().ok()),\n", ind, f.Type.Name)
	m.pf("%s    _ => None,\n%s};\n", ind, ind)
}

func (m *msgGen) vloadField(f *ir.Field, x, entry, ind string) {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		m.vresolve(f, x, ind)
	case f.Type.Optional:
		e := msgElement(f)
		m.pf("%slet mut item: %s = Default::default();\n", ind, elemRust(e))
		m.vloadValue(e, "item", entry, ind)
		m.pf("%s%s = Some(item);\n", ind, x)
	case f.Type.Kind == ir.TString:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if !r.align() %s\n", ind, msgBad)
		m.pf("%s    let buf = r.buffer;\n%s    let start = (r.offset / 8) as usize;\n", ind, ind)
		m.pf("%s    if !r.skip(n * 8) %s\n", ind, msgBad)
		m.pf("%s    let text = &buf[start..start + n as usize];\n", ind)
		m.pf("%s    if !table_message_utf8(text) %s\n", ind, msgBad)
		m.pf("%s    let kept = table_message_utf8_clamp(text, core::cmp::min(n as usize, %d));\n", ind, f.Type.Size)
		m.pf("%s    if kept < n as usize { report.clamped += 1; }\n", ind)
		m.pf("%s    %s = String::from(core::str::from_utf8(&text[..kept]).unwrap_or(\"\"));\n%s}\n", ind, x, ind)
	case f.Type.Kind == ir.TBytes:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
		m.pf("%s    if !r.align() %s\n", ind, msgBad)
		m.pf("%s    let buf = r.buffer;\n%s    let start = (r.offset / 8) as usize;\n", ind, ind)
		m.pf("%s    if !r.skip(n * 8) %s\n", ind, msgBad)
		m.pf("%s    let kept = core::cmp::min(n as usize, %d);\n", ind, f.Type.Size)
		m.pf("%s    if kept < n as usize { report.clamped += 1; }\n", ind)
		m.pf("%s    %s = buf[start..start + kept].to_vec();\n%s}\n", ind, x, ind)
	case f.Array != ir.ArrayNone:
		m.vloadArray(f, x, entry, ind)
	default:
		m.vloadValue(f, x, entry, ind)
	}
}

// vloadValue decodes one body, enum or scalar into `dst`.
func (m *msgGen) vloadValue(f *ir.Field, dst, entry, ind string) {
	switch {
	case isStructRef(f.Type):
		m.pf("%sif !%s(r, v, report, nodes, &mut %s) { return false; }\n", ind, fn(f.Type.Name, "message_vload"), dst)
	case msgEnum(f) != nil:
		e := msgEnum(f)
		m.pf("%s{\n", ind)
		m.pf("%s    let Some(reference) = r.get(v.ref_bits) else %s;\n", ind, msgBad)
		m.pf("%s    if reference == 0 {\n%s        %s = %s::None;\n%s    } else {\n", ind, ind, dst, e.Name, ind)
		m.pf("%s        let Some(id) = v.name(reference) else %s;\n", ind, msgBad)
		m.pf("%s        %s = match id {\n", ind, dst)
		for i, vn := range e.Variants {
			m.pf("%s            0x%016x => %s::%s,\n", ind, ir.TableWireId(e.VariantWireName(i)), e.Name, rustVariant(vn))
		}
		m.pf("%s            _ => { report.unknown += 1; %s::None }\n%s        };\n%s    }\n%s}\n", ind, e.Name, ind, ind, ind)
	default:
		m.pf("%s{\n", ind)
		m.pf("%s    let Some((lo, hi)) = table_message_read_scalar(r, %s.kind, &%s.shape) else %s;\n", ind, entry, entry, msgBad)
		m.convertScalar(f, dst, entry, ind+"    ")
		m.pf("%s}\n", ind)
	}
}

// vloadArray is a count, then the elements: a fixed array keeps its bound in
// place, a counted one keeps what fits, and either counts clamped for a
// surplus, which is stepped over by arithmetic where its width is fixed.
func (m *msgGen) vloadArray(f *ir.Field, x, entry, ind string) {
	el := m.tmp("element")
	e := msgElement(f)
	m.pf("%s{\n", ind)
	m.pf("%s    let Some(n) = table_message_count(r, &%s.shape) else %s;\n", ind, entry, msgBad)
	m.pf("%s    if %s.shape.elem == 6 && !r.align() %s\n", ind, entry, msgBad)
	m.pf("%s    let %s = TableMessageEntry { id: 0, kind: %s.shape.elem, shape: %s.element, element: TableMessageShape::default() };\n", ind, el, entry, entry)
	m.pf("%s    let keep = core::cmp::min(n, %d);\n", ind, f.ArrayBound)
	m.pf("%s    if keep < n { report.clamped += 1; }\n", ind)
	m.pf("%s    let width = table_message_value_bits(%s.kind, &%s.shape);\n", ind, el, el)
	m.pf("%s    let walk = if width >= 0 { keep } else { n };\n", ind)
	if f.Array == ir.ArrayCounted {
		m.pf("%s    %s.clear();\n", ind, x)
	}
	m.pf("%s    for i in 0..walk {\n", ind)
	m.pf("%s        let mut item: %s = Default::default();\n", ind, elemRust(f))
	inner := ind + "        "
	if f.Type.Pointer {
		m.vresolve(e, "item", inner)
	} else {
		m.vloadValue(e, "item", el, inner)
	}
	if f.Array == ir.ArrayCounted {
		m.pf("%s        if i < keep { %s.push(item); }\n", ind, x)
	} else {
		m.pf("%s        if i < keep { %s[i as usize] = item; }\n", ind, x)
	}
	m.pf("%s    }\n", ind)
	m.pf("%s    if walk < n && !r.skip((n - walk) * width as u64) %s\n", ind, msgBad)
	m.pf("%s}\n", ind)
}

// ---- the batch -------------------------------------------------------------

func (m *msgGen) emitVBatch(st *ir.Struct) {
	snake := ir.RustSnake(st.Name)
	var placeable []string
	for _, t := range ir.PointerReachable(st) {
		placeable = append(placeable, fmt.Sprintf("0x%016x", ir.TableWireId(t.WireName())))
	}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(st)
	// the C ABI storage the reference's reader lays out (§6.5), each rounded to
	// the unit's region alignment
	align := ir.TableRegionAlign(m.u)
	round := func(n int64) int64 { return (n + align - 1) & -align }
	var storage strings.Builder
	seen := map[uint64]bool{}
	for _, t := range ir.PointerReachable(st) {
		id := ir.TableWireId(t.WireName())
		if seen[id] {
			continue
		}
		seen[id] = true
		fmt.Fprintf(&storage, "                    0x%016x => %d,\n", id, round(ir.RecordLayout(m.u, t).Size))
	}
	m.pf(`/// One %[2]s ROOT body (§3.1, §3.3): the numbering derived from the graph,
/// the node table FIRST when the root reaches any node, then the fields.
pub fn %[1]s_message_vsave_root(w: &mut TableBitWriter, value: &%[2]s, report: &mut TableReport) -> bool {
    let mut g = TableNodeGraph::new();
    %[1]s_number(value, &mut g);
    if g.blob_shared {
        // a blob two slots name (§2.5, §16.7): the writer names it
        return report.refuse(TableReason::BlobShared);
    }
    let mut ib = table_message_bits(1);
    if !g.records.is_empty() {
        ib = table_message_bits(g.records.len() as u64 + 1);
        w.put(TABLE_MESSAGE_NODE_TABLE_SLOT, TABLE_MESSAGE_REF_BITS);
        w.put(g.records.len() as u64, 32);
        for rec in &g.records {
            if !table_message_record_save(w, &g, ib, rec) {
                return false;
            }
        }
    }
    %[1]s_message_vsave(w, &g, ib, value)
}

/// One %[2]s ROOT body read (§3.1, §3.3): the node table first, then every
/// placeable record in wire order (pass one, its events counted), then the
/// root's own fields, each node built the first time a slot names it.
pub fn %[1]s_message_vload_root(r: &mut TableBitReader, v: &TableVocabulary, report: &mut TableReport, value: &mut %[2]s) -> bool {
    let placeable: &[u64] = &[%[3]s];
    let mut nodes = TableMessageNodes::new(r.buffer, %[4]s);
    if !table_message_node_table(r, v, report, &mut nodes, placeable, %[5]v, %[6]v, false) {
        return false;
    }
    for k in 0..nodes.reader.slots.len() {
        if matches!(nodes.reader.slots[k], TableNodeSlot::Table(_)) && !table_message_node_count(v, &mut nodes, k, report) {
            return false;
        }
    }
    nodes.build = true;
    if !%[1]s_message_vload(r, v, report, &mut nodes, value) {
        return false;
    }
    // a cycle the root reaches is the save's refusal (§3.1)
    report.cycle_on_save |= nodes.sink.cycle_on_save;
    true
}

/// LoadMeasure over a batch (§3.3, §6.5): the region bytes the reference's
/// batch read lays out — per body one node directory, the root's storage and
/// each record's, a blob's header and bytes with a string's terminator — read
/// off the framing alone, or -1 where the numbering cannot be sized. A body
/// whose own fields give out sizes the batch through that body.
pub fn %[1]s_load_measure_messages(v: &TableVocabulary, data: &[u8]) -> i64 {
    if data.len() < 2 || data[0] != TABLE_WIRE_MESSAGE_FORM || !v.announced {
        return -1;
    }
    let round = |n: i64| (n + %[7]d - 1) & -%[7]d;
    let mut r = TableBitReader::new(&data[1..]);
    let Some(raw) = r.get(8) else { return -1 };
    let mut total = 0i64;
    for _ in 0..raw + 1 {
        let mut ib = table_message_bits(1);
        total += %[8]d + 16;
        let at = r.offset;
        let Some(reference) = r.get(v.ref_bits) else { return -1 };
        if v.entry(reference).is_some_and(|e| e.id == TABLE_NODE_TABLE_ID) {
            let Some(n) = r.get(32) else { return -1 };
            ib = table_message_bits(n + 1);
            for _ in 0..n {
                let Some(tref) = r.get(v.ref_bits) else { return -1 };
                let Some(tid) = v.name(tref) else { return -1 };
                total += 16;
                if tid == BYTES_TYPE_ID || tid == STRING_TYPE_ID {
                    let Some(len) = r.get(32) else { return -1 };
                    if !r.align() || !r.skip(len * 8) {
                        return -1;
                    }
                    total += round(8 + len as i64 + if tid == STRING_TYPE_ID { 1 } else { 0 });
                    continue;
                }
                if !table_message_vskip_body(&mut r, v, 0, ib) {
                    return -1;
                }
                total += match tid {
%[9]s                    _ => 0,
                };
            }
        } else {
            r.offset = at;
        }
        if !table_message_vskip_body(&mut r, v, 0, ib) {
            return total;
        }
    }
    total
}

/// MeasureMessages (§3.3): the byte count SaveMessages writes for this batch,
/// or -1. "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME".
pub fn %[1]s_measure_messages(values: &[%[2]s], report: &mut TableReport) -> i64 {
    if values.is_empty() || values.len() > TABLE_MESSAGE_BATCH_MAX {
        report.refuse(TableReason::BatchTooLarge);
        return -1;
    }
    let mut w = TableBitWriter::measure();
    for value in values {
        if !%[1]s_message_vsave_root(&mut w, value, report) {
            return -1;
        }
    }
    w.align();
    if w.overflow {
        return -1;
    }
    2 + (w.bits / 8) as i64
}

/// SaveMessages (§3.3): the form byte, M - 1, then every body as one continuous
/// bit stream padded to a byte once at its end, each body carrying its own
/// numbering. The byte count, or -1 for a refusal or a buffer too short.
pub fn %[1]s_save_messages(values: &[%[2]s], buffer: &mut [u8], report: &mut TableReport) -> i64 {
    if values.is_empty() || values.len() > TABLE_MESSAGE_BATCH_MAX {
        report.refuse(TableReason::BatchTooLarge);
        return -1;
    }
    if buffer.len() < 2 {
        return -1;
    }
    buffer[0] = TABLE_WIRE_MESSAGE_FORM;
    buffer[1] = (values.len() - 1) as u8;
    let mut w = TableBitWriter::new(&mut buffer[2..]);
    for value in values {
        if !%[1]s_message_vsave_root(&mut w, value, report) {
            return -1;
        }
    }
    w.align();
    if w.overflow {
        return -1;
    }
    2 + (w.bits / 8) as i64
}

/// LoadMessages (§3.3): one batch into the caller's storage against the
/// ENDPOINT's announced vocabulary, each body's nodes its own. The count of
/// bodies delivered and whether the batch read whole; damage inside body k
/// delivers bodies 1 to k - 1.
pub fn %[1]s_load_messages(values: &mut [%[2]s], vocabulary: &TableVocabulary, data: &[u8], report: &mut TableReport) -> (usize, bool) {
    let Some(count) = table_message_batch_open(vocabulary, data, report) else {
        return (0, false);
    };
    if count > values.len() {
        return (count, report.refuse(TableReason::BatchTooLarge));
    }
    let mut r = TableBitReader::new(&data[2..]);
    for (i, value) in values.iter_mut().take(count).enumerate() {
        if !%[1]s_message_vload_root(&mut r, vocabulary, report, value) {
            return (i, false);
        }
    }
    (count, table_message_batch_close(&mut r, report))
}

`, snake, st.Name, strings.Join(placeable, ", "), fmt.Sprintf("0x%016x", ir.TableWireId(st.WireName())), bytesEdge, stringEdge, align, round(ir.RecordLayout(m.u, st).Size), storage.String())
}
