package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VARIABLE WIRE (docs/SPEC-TABLES.md §3, §3.1), form byte 1, for the
// variable class this leg carries: table roots and tables nested by value or
// as array elements; pointers *T over the flat node table (§3.1); *string and
// *bytes blobs (§2.5); bool, every int/uint width, bits(N), float32 and
// float64, and enum fields with their defaults and ranges; string(N) and
// bytes(N); and unbounded []T and bounded [..N]T arrays of scalar, enum, table
// and pointer elements (§2.9). What it does not carry is refused by name
// (variablerefusal.go).
//
// This file emits the RUST runtime of that wire: the canonical LEB128 every
// length, count and reference on a FILE spells (§3), the form byte, the
// tolerant read report (§4, §5), the scalar ladder (§4), the id table, the
// numbering a save derives and the node table a load gathers (§3.1), and, per
// source file, the constant every body codec carries: each field's wire id
// (§5) beside its kind byte (§3).
//
// The modules are emitted STAND-ALONE. [cardVariableModules] carries the
// boundary refusal (§11) on the card's own path, while
// [scopedVariableModules] — GenerateLineage's entry point — leaves a variable
// table whose constructs this leg does not carry ABSENT rather than refusing
// the unit whole, so the fixed-form corpus (rusttable_test.go's `pointered`)
// still emits its fixed form beside a variable table it does not encode.
const VariableRuntimeModule = "variable_runtime"

// cardVariableModules returns the variable wire's modules for a unit whose
// variable tables this leg carries: the shared runtime and, for each source
// file that declares an emitted table, that file's constant block and codec.
// The refusal runs FIRST (§11), scoped here to the variable wire path alone.
func cardVariableModules(u *ir.Unit) (map[string][]byte, error) {
	if len(ir.VariableTables(u)) == 0 {
		return nil, nil
	}
	if err := refuseCardUnsupportedVariableConstructs(u); err != nil {
		return nil, err
	}
	return variableModules(u), nil
}

// scopedVariableModules is the GENERATE-path entry point: the same modules as
// [cardVariableModules], but WITHOUT the whole-unit refusal. A variable table
// whose constructs this leg does not carry is left ABSENT — its wire is not
// emitted and nothing is mis-encoded — so a unit that mixes a fixed table with
// a variable table carrying a union (rusttable_test.go's `pointered`) still
// emits the fixed form. The by-name refusal lives on the card's own path, in
// [cardVariableModules], where a unit that gains an excluded construct is
// refused naming table, field and construct (docs/SPEC-TABLES.md §11).
//
// A closure that holds only FIXED tables takes the modules too: a fixed table
// rides the variable form as well as its own (docs/SPEC-TABLES.md §2.2, §3),
// so the gate is the closure, not the variable class.
func scopedVariableModules(u *ir.Unit) map[string][]byte {
	if len(ir.TableClosure(u)) == 0 {
		return nil
	}
	return variableModules(u)
}

// variableModules emits the runtime, the descriptors and every file's constant
// block and codec for the carried set; nil when no variable table is carried.
func variableModules(u *ir.Unit) map[string][]byte {
	vc := newVariableClass(u)
	any := false
	for n := range ir.TableClosure(u) {
		if vc.emitted[n] {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	out := map[string][]byte{}
	out[VariableRuntimeModule+".rs"] = []byte(variableRuntimeModule(u))
	if d := variableDescriptorModule(u); d != nil {
		out[VariableDescriptorModule+".rs"] = d
	}
	if body := tableEnumsModule(u, vc); body != nil {
		out[TableEnumsModule+".rs"] = body
	}
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: ir.TableClosure(u), vc: vc}
		if body := g.variableModule(); body != nil {
			out[strings.ToLower(f.Base)+"_variable.rs"] = body
		}
		if body := g.variableCodecModule(); body != nil {
			out[strings.ToLower(f.Base)+"_table.rs"] = body
		}
	}
	return out
}

// variableRuntimeModule emits the shared framing the variable wire's bodies
// ride on: the canonical LEB128 reader and writer (docs/SPEC-TABLES.md §3 —
// every length, count and reference on a FILE is this one spelling) and the
// tolerant report the reader fills (section 4, §5).
func variableRuntimeModule(u *ir.Unit) string {
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package, "the variable wire's framing (docs/SPEC-TABLES.md §3, §4, §5)"))
	b.WriteString(`
use std::any::Any;
use std::collections::HashMap;
use std::rc::Rc;
/// The VARIABLE FORM's form byte (docs/SPEC-TABLES.md §3): the one arm whose
/// header is the form byte alone. Nothing is aligned and nothing is padded.
pub const TABLE_WIRE_FORM: u8 = 1;

`)
	b.WriteString(tableReportRust)
	b.WriteString(`
/// Read one canonical LEB128 u64 from the cursor; None when it overruns, runs
/// past ten bytes, or is NON-MINIMAL — a redundant continuation byte, or a tenth
/// byte above the 64th value bit, is malformed (docs/SPEC-TABLES.md §3).
pub fn leb_read(buf: &[u8], off: &mut usize) -> Option<u64> {
    let mut v: u64 = 0;
    let mut shift = 0u32;
    let mut p = *off;
    for i in 0..10 {
        if p >= buf.len() {
            return None;
        }
        let b = buf[p];
        p += 1;
        if i == 9 && b > 1 {
            return None;
        }
        v |= ((b & 0x7f) as u64) << shift;
        if b & 0x80 == 0 {
            if i > 0 && b == 0 {
                return None;
            }
            *off = p;
            return Some(v);
        }
        shift += 7;
    }
    None
}

/// Write one canonical LEB128 u64.
pub fn leb_write(out: &mut Vec<u8>, v: u64) {
    let mut x = v;
    loop {
        let b = (x & 0x7f) as u8;
        x >>= 7;
        if x == 0 {
            out.push(b);
            break;
        }
        out.push(b | 0x80);
    }
}

/// The RESERVED node-table id and the *string blob's type id
/// (docs/SPEC-TABLES.md §3.1, §5): constants of the rule, so a save needs no
/// lookup and a reader can name the numbering.
pub const NODE_TABLE_ID: u64 = 0xffffffffffffffff;
pub const STRING_TYPE_ID: u64 = STRING_TYPE_ID_VALUE;
pub const BYTES_TYPE_ID: u64 = BYTES_TYPE_ID_VALUE;
pub const BUILD_VERSION_ID: u64 = 0xfffffffffffffffe;
pub const VOCABULARY_ID: u64 = 0xfffffffffffffffd;

/// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
/// well-formed UTF-8 with no zero byte, checked as it ARRIVES and before the
/// reader's own bound.
pub fn text_valid(b: &[u8]) -> bool {
    std::str::from_utf8(b).is_ok() && !b.contains(&0)
}

/// WIDE TEXT ON THE WIRE (docs/SPEC-TABLES.md §3, kind 33; SPEC.md §4.12): a
/// payload of L BYTES is L / 2 UTF-16 code units, little-endian. None is
/// damage, the oracle's (internal/tablewire decode.go): an odd L is not a
/// wstring at any length, and the content rule is paired surrogates with no
/// zero unit among the used units. The bound is the reader's, applied after.
pub fn table_wide_units(b: &[u8]) -> Option<Vec<u16>> {
    if b.len() & 1 != 0 { return None; }
    let units: Vec<u16> = b.as_chunks::<2>().0.iter().map(|c| u16::from_le_bytes(*c)).collect();
    let mut i = 0;
    while i < units.len() {
        let u = units[i];
        if u == 0 || (0xDC00..=0xDFFF).contains(&u) { return None; }
        if (0xD800..=0xDBFF).contains(&u) {
            if i + 1 >= units.len() || !(0xDC00..=0xDFFF).contains(&units[i + 1]) { return None; }
            i += 2;
            continue;
        }
        i += 1;
    }
    Some(units)
}

/// A CLAMP NEVER SPLITS A PAIR (docs/SPEC-TABLES.md §3): the longest prefix
/// of at most the bound's units that does not end on a pair's high half.
pub fn table_wide_boundary(units: &[u16], bound: usize) -> usize {
    if units.len() <= bound { return units.len(); }
    let mut keep = bound;
    if keep > 0 && (0xD800..=0xDBFF).contains(&units[keep - 1]) { keep -= 1; }
    keep
}

/// THE WRITER'S BOUND (docs/SPEC-TABLES.md §3, SPEC.md §4.12): a value over
/// the bound's code units is the save's refusal, recorded BY NAME (at is
/// <owner>.<field>) for <root>_save_checked; the units that ride are the
/// longest prefix that fits without splitting a pair.
pub fn table_wide_fit(ids: &mut IdTable, units: &[u16], bound: usize, at: &str) -> usize {
    if units.len() <= bound { return units.len(); }
    if ids.refused.is_none() {
        ids.refused = Some(format!("{at}: wstring({bound}) holds {} code units, over its bound (docs/SPEC-TABLES.md §3)", units.len()));
    }
    table_wide_boundary(units, bound)
}

/// The units, little-endian, with no length of their own: the caller's L
/// frames them (docs/SPEC-TABLES.md §3, kind 33 and an arm's payload).
pub fn table_wide_put(dst: &mut Vec<u8>, units: &[u16]) {
    for u in units { dst.extend_from_slice(&u.to_le_bytes()); }
}

/// The id table a save builds in FIRST-USE order (docs/SPEC-TABLES.md §3): one
/// entry per distinct id, referenced by position from 1. id_ref returns the
/// position, appending the id when it has not been named yet.
#[derive(Default)]
pub struct IdTable {
    pub entries: Vec<u64>,
    /// the save's refusal BY NAME, when a value is over its bound
    /// (docs/SPEC-TABLES.md §3): <root>_save_checked answers it
    pub refused: Option<String>,
}

impl IdTable {
    pub fn new() -> Self {
        IdTable { entries: Vec::new(), refused: None }
    }

    /// A mark to roll back to: an ELIDED field costs nothing in the id table
    /// either (§3), so a nested body that turns out to be its terminator alone
    /// undoes the ids its walk interned.
    pub fn mark(&self) -> usize {
        self.entries.len()
    }

    pub fn rollback(&mut self, mark: usize) {
        self.entries.truncate(mark);
    }

    pub fn id_ref(&mut self, id: u64) -> u64 {
        for (i, &e) in self.entries.iter().enumerate() {
            if e == id {
                return i as u64 + 1;
            }
        }
        self.entries.push(id);
        self.entries.len() as u64
    }
}

/// Resolve a reference to its id (docs/SPEC-TABLES.md §3): reference k is the
/// table's kth entry; reference 0 names no id; a reference past the table is
/// None and the caller counts malformed.
pub fn ref_id(ids: &[u64], r: u64) -> Option<u64> {
    if r == 0 {
        return None;
    }
    ids.get(r as usize - 1).copied()
}

/// THE SAVE'S NUMBERING (docs/SPEC-TABLES.md §3.1): every node a root reaches,
/// in the FIRST-VISIT order of a depth-first pre-order walk over pointer edges
/// only — fields in declaration order, elements in index order, descending
/// through every by-value edge. The root is index 1 and record k (1-based) is
/// index k + 1. A node's identity is its Rc — a table's Rc<T>, a *string
/// blob's Rc<str>, a *bytes blob's Rc<[u8]> — so two slots holding one Rc are
/// one node, written once, and two slots holding EQUAL bytes in two Rcs are two
/// nodes, exactly as the oracle numbers by object (internal/tablewire/nodes.go).
/// It is derived from the graph before the body is written and never carried.
pub enum TableNodeRecord {
    Table(u64, Rc<dyn Any>),
    Blob(u64, Vec<u8>),
}

#[derive(Default)]
pub struct TableNodeGraph {
    pub records: Vec<TableNodeRecord>,
    tables: HashMap<usize, u64>,
    blobs: HashMap<usize, u64>,
    /// A blob two slots name (docs/SPEC-TABLES.md §2.5): the walk met its Rc
    /// a second time, so every writer refuses the graph by name, blob_shared.
    pub blob_shared: bool,
}

impl TableNodeGraph {
    pub fn new() -> Self {
        TableNodeGraph::default()
    }

    /// Number one table node at its FIRST visit; true when it is new, which is
    /// when the walk descends into it (§3.1: a node takes its index the first
    /// time it is reached and never again).
    pub fn visit_table(&mut self, type_id: u64, node: Rc<dyn Any>) -> bool {
        let key = Rc::as_ptr(&node) as *const () as usize;
        if self.tables.contains_key(&key) {
            return false;
        }
        self.records.push(TableNodeRecord::Table(type_id, node));
        self.tables.insert(key, self.records.len() as u64 + 1);
        true
    }

    /// Number one blob node at its first visit (§2.5): a blob reaches nothing.
    /// key is the blob's identity, the address its Rc owns.
    pub fn visit_blob(&mut self, type_id: u64, key: usize, bytes: &[u8]) {
        if self.blobs.contains_key(&key) {
            // A BLOB IS OWNED BY ONE SLOT (§2.5): the second naming is the
            // writer's refusal, never a second record or a copy per slot
            self.blob_shared = true;
            return;
        }
        self.records.push(TableNodeRecord::Blob(type_id, bytes.to_vec()));
        self.blobs.insert(key, self.records.len() as u64 + 1);
    }

    /// The index a pointer slot writes (§3.1).
    pub fn node_index<T>(&self, node: &Rc<T>) -> u64 {
        *self.tables.get(&(Rc::as_ptr(node) as *const () as usize)).unwrap_or(&0)
    }

    /// The index a blob slot writes (§2.5, §3.1), by the blob's identity.
    pub fn blob_index(&self, key: usize) -> u64 {
        *self.blobs.get(&key).unwrap_or(&0)
    }
}

/// One record's state in a LOAD's numbering (docs/SPEC-TABLES.md §3.1).
pub enum TableNodeSlot {
    /// a record this root cannot place (counted unknown once, at the record) or
    /// a text blob refused as malformed: every slot naming it reads null
    Null,
    /// a *bytes blob record (§2.5), materialized ONCE and shared by every
    /// slot that names it
    Bytes(Rc<[u8]>),
    /// a *string blob record (§2.5), checked well-formed, materialized once
    Text(Rc<str>),
    /// a table record this root can place, not decoded yet
    Table(u64),
    /// a table record whose body is being decoded right now
    Building,
    /// a decoded table node, materialized ONCE and shared by every slot
    /// that names it — the identity §3.1 preserves
    Built(u64, Rc<dyn Any>),
}

/// THE NODE TABLE A LOAD GATHERS (docs/SPEC-TABLES.md §3.1), before any body is
/// decoded: the node table is written LAST, so a reader that met "head = 2"
/// must know the numbering before it can resolve it. good is false when the
/// table could not be read whole, and then every pointer in the save reads
/// null. A pointer's payload is a NUMBER, bounds-checked here; the node it
/// names is decoded once, the first time a slot resolves it, and shared.
pub struct TableNodeReader {
    pub good: bool,
    pub root_type: u64,
    pub retaining: bool,
    pub retain_capacity: usize,
    pub retained: Vec<u8>,
    pub recs: Vec<(u64, usize, usize)>,
    pub slots: Vec<TableNodeSlot>,
    /// which blob records a slot has already named (§2.5): each later naming
    /// of the same record counts blob_shared on the tolerant read
    pub blob_named: Vec<bool>,
}

impl TableNodeReader {
    /// Gather the numbering out of a ROOT body ending at end (§3.1). Framing
    /// damage on the root body ends the gathering scan with what it found and
    /// is counted by the body read, not here. A node table under a kind other than 12, a record
    /// that runs past its field, bytes left over, or a node_count the scan
    /// does not match make the TABLE malformed: good is false, one malformed
    /// counts, and nothing in it is counted unknown, because the numbering it
    /// belonged to does not exist. A record whose type id this root cannot
    /// place keeps its index and counts unknown once; a *string blob that is
    /// not well-formed text is malformed and its slots read null.
    #[allow(clippy::too_many_arguments)]
    pub fn scan(wire: &[u8], end: usize, ids: &[u64], root_type: u64, placeable: &[u64], bytes_edge: bool, string_edge: bool, retaining: bool, retain_capacity: usize, report: &mut TableReport) -> Option<TableNodeReader> {
        let mut reader = TableNodeReader { good: true, root_type, retaining, retain_capacity, retained: Vec::new(), recs: Vec::new(), slots: Vec::new(), blob_named: Vec::new() };
        let mut off = 1usize;
        while off < end {
            let Some(r) = leb_read(&wire[..end], &mut off) else { break; };
            if r == 0 { break; }
            // Damage in a field the gathering scan cannot pass ends the scan
            // and refuses nothing: the body read meets the same damage and
            // counts it, after decoding the fields before it (the oracle's
            // nodeTableBytes).
            if off >= end { break; }
            let kind = wire[off]; off += 1;
            let Some(id) = ref_id(ids, r) else { break; };
            if id != NODE_TABLE_ID {
                if !leb_skip(wire, &mut off, end, kind) { break; }
                continue;
            }
            if kind != 12 {
                report.malformed = true;
                reader.good = false;
                reader.recs.clear();
                break;
            }
            // A truncated node-table length cannot borrow trailer bytes. The
            // root body still decodes its preceding fields with null nodes.
            let Some(l) = leb_read(&wire[..end], &mut off) else { report.malformed = true; reader.good = false; reader.recs.clear(); break; };
            if off > end || l > (end - off) as u64 { report.malformed = true; reader.good = false; reader.recs.clear(); break; }
            let sub = off + l as usize;
            let mut recs = Vec::new();
            let mut scan_ok = true;
            match leb_read(&wire[..sub], &mut off) {
                None => scan_ok = false,
                Some(declared) => {
                    while off < sub {
                        let Some(tref) = leb_read(&wire[..sub], &mut off) else { scan_ok = false; break; };
                        if tref == 0 || tref as usize > ids.len() { scan_ok = false; break; }
                        let Some(bl) = leb_read(&wire[..sub], &mut off) else { scan_ok = false; break; };
                        if off > sub || bl > (sub - off) as u64 { scan_ok = false; break; }
                        recs.push((ids[tref as usize - 1], off, off + bl as usize));
                        off += bl as usize;
                    }
                    if off != sub || declared != recs.len() as u64 { scan_ok = false; }
                }
            }
            if !scan_ok {
                reader.good = false;
                reader.recs.clear();
            } else {
                reader.good = true;
                reader.recs = recs;
            }
            off = sub;
        }
        if reader.good {
            for &(tid, start, stop) in &reader.recs {
                let bytes = &wire[start..stop];
                if tid == BYTES_TYPE_ID && bytes_edge {
                    reader.slots.push(TableNodeSlot::Bytes(Rc::from(bytes)));
                } else if tid == STRING_TYPE_ID && string_edge {
                    if text_valid(bytes) {
                        reader.slots.push(TableNodeSlot::Text(Rc::from(std::str::from_utf8(bytes).unwrap())));
                    } else {
                        report.malformed = true;
                        reader.slots.push(TableNodeSlot::Null);
                    }
                } else if placeable.contains(&tid) {
                    reader.slots.push(TableNodeSlot::Table(tid));
                } else {
                    // skipped by its length, and it keeps its index (§3.1); a
                    // node record is an excluded class for retention (§6.6)
                    report.unknown += 1;
                    if retaining { report.retain_lost += 1; }
                    reader.slots.push(TableNodeSlot::Null);
                }
            }
        } else {
            report.malformed = true;
            reader.recs.clear();
        }
        Some(reader)
    }

    /// Resolve a table slot's index against the numbering (§3.1): Some(record)
    /// when the record is a placeable node of the slot's declared table, None
    /// otherwise with the event counted — an index above node_count + 1 is
    /// malformed, a record of another type (or a blob) is a kind mismatch, and
    /// an index of 1 names the ROOT, which a node slot whose target is not the
    /// reader's own root table takes as a kind mismatch. (A root pointing at
    /// itself is a cycle no writer produces, §3.1, and this storage cannot hold
    /// one: it reads null.)
    pub fn node_slot(&self, index: u64, want: u64, report: &mut TableReport) -> Option<usize> {
        if !self.good || index == 0 {
            return None;
        }
        if index == 1 {
            if want != self.root_type { report.kind_mismatch += 1; }
            return None;
        }
        let k = (index - 2) as usize;
        if index - 2 >= self.slots.len() as u64 {
            report.malformed = true;
            return None;
        }
        match &self.slots[k] {
            TableNodeSlot::Null => None,
            TableNodeSlot::Bytes(_) | TableNodeSlot::Text(_) => { report.kind_mismatch += 1; None }
            TableNodeSlot::Table(t) | TableNodeSlot::Built(t, _) => {
                if *t == want { Some(k) } else { report.kind_mismatch += 1; None }
            }
            TableNodeSlot::Building => {
                if self.recs[k].0 == want { Some(k) } else { report.kind_mismatch += 1; None }
            }
        }
    }

    /// The blob slot a node index names (§2.5, §3.1), or None with the event
    /// counted: index 1 (the root) and a table record are kind mismatches, an
    /// index past the table is malformed.
    fn blob_record(&self, index: u64, report: &mut TableReport) -> Option<&TableNodeSlot> {
        if !self.good || index == 0 {
            return None;
        }
        if index == 1 {
            report.kind_mismatch += 1;
            return None;
        }
        if index - 2 >= self.slots.len() as u64 {
            report.malformed = true;
            return None;
        }
        Some(&self.slots[(index - 2) as usize])
    }

    /// Count a blob record's naming (§2.5): the tolerant read keeps the ONE
    /// node every slot names and counts each naming after the first in the
    /// report's blob_shared counter.
    fn name_blob(&mut self, index: u64, report: &mut TableReport) {
        let k = (index - 2) as usize;
        if self.blob_named.len() < self.slots.len() {
            self.blob_named.resize(self.slots.len(), false);
        }
        if self.blob_named[k] {
            report.blob_shared += 1;
        } else {
            self.blob_named[k] = true;
        }
    }

    /// Resolve a *bytes slot (§2.5, §3.1): the ONE blob every slot naming the
    /// record shares; a *string record or a table record is a kind mismatch.
    pub fn bytes_slot(&mut self, index: u64, report: &mut TableReport) -> Option<Rc<[u8]>> {
        let b = match self.blob_record(index, report)? {
            TableNodeSlot::Null => return None,
            TableNodeSlot::Bytes(b) => b.clone(),
            _ => { report.kind_mismatch += 1; return None; }
        };
        self.name_blob(index, report);
        Some(b)
    }

    /// Resolve a *string slot (§2.5, §3.1), the same way.
    pub fn text_slot(&mut self, index: u64, report: &mut TableReport) -> Option<Rc<str>> {
        let s = match self.blob_record(index, report)? {
            TableNodeSlot::Null => return None,
            TableNodeSlot::Text(s) => s.clone(),
            _ => { report.kind_mismatch += 1; return None; }
        };
        self.name_blob(index, report);
        Some(s)
    }
}

/// THE SCALAR LADDER (docs/SPEC-TABLES.md §3, §4): a fixed-width kind's payload
/// size, 0 for every framed or reference-shaped kind.
pub fn table_kind_width(kind: u8) -> usize {
    match kind {
        1 | 2 | 6 | 20 | 25 => 1,
        3 | 7 | 21 | 26 => 2,
        4 | 8 | 10 | 22 | 27 => 4,
        5 | 9 | 11 | 23 | 28 | 36 => 8,
        18 | 19 | 24 | 29 => 16,
        _ => 0,
    }
}

/// Whether a payload under kind decodes EXACTLY into a field declared at
/// declared (§4): an integer into a WIDER integer of the same signedness, and
/// f32 into f64. Every other pair is a kind mismatch, the fixed-point kinds
/// 20–29 every way: a raw value read at another F is another number (§4).
pub fn table_kind_widens(kind: u8, declared: u8) -> bool {
    match declared {
        3..=5 => (2..declared).contains(&kind),
        7..=9 => (6..declared).contains(&kind),
        11 => kind == 10,
        18 => matches!(kind, 2..=5),
        19 => matches!(kind, 6..=9),
        _ => false,
    }
}

/// Read one scalar at kind's own width (§3): the raw little-endian bits, a
/// signed kind sign-extended into the u64. None when the payload runs past
/// end.
pub fn table_scalar_read(buf: &[u8], off: &mut usize, end: usize, kind: u8) -> Option<u64> {
    let w = table_kind_width(kind);
    if w == 0 || w > 8 || *off + w > end {
        return None;
    }
    let mut raw = 0u64;
    for i in 0..w {
        raw |= (buf[*off + i] as u64) << (8 * i);
    }
    *off += w;
    if matches!(kind, 2..=5 | 20..=23) && w < 8 {
        let s = 64 - 8 * w as u32;
        raw = (((raw << s) as i64) >> s) as u64;
    }
    Some(raw)
}

/// Read one 128-bit scalar (or a narrower integer widened into it) at kind's
/// own width (§3, §4): little-endian, sign-extended into u128 for signed kinds.
/// A fixed128 or ufixed128 (kinds 24, 29) is its sixteen raw bytes.
pub fn scalar_read_128(buf: &[u8], off: &mut usize, end: usize, kind: u8) -> Option<u128> {
    let w = table_kind_width(kind);
    if w == 0 || *off + w > end {
        return None;
    }
    let mut raw = 0u128;
    for i in 0..w {
        raw |= (buf[*off + i] as u128) << (8 * i);
    }
    *off += w;
    if matches!(kind, 2..=5) && w < 16 {
        let s = 128 - 8 * w as u32;
        raw = (((raw << s) as i128) >> s) as u128;
    }
    Some(raw)
}

/// THE FINITE RULE ON A RETAINED UNKNOWN (docs/SPEC-TABLES.md §3.3, §6.6): a
/// payload of a float kind is tested on its raw bits like a known field's, and
/// a NaN or an infinity refuses the read. Every other kind, and a payload that
/// is not its kind's width, is the walk's to judge.
pub fn table_retained_float_finite(kind: u8, payload: &[u8]) -> bool {
    match (kind, payload.len()) {
        (10, 4) => table_finite_f32(u32::from_le_bytes([payload[0], payload[1], payload[2], payload[3]])),
        (11, 8) => {
            let mut b = [0u8; 8];
            b.copy_from_slice(payload);
            table_finite_f64(u64::from_le_bytes(b))
        }
        _ => true,
    }
}

/// THE WRITER'S FINITE RULE (docs/SPEC-TABLES.md §3.3, the same rule as the
/// packet wire's, SPEC.md §4.3): a NaN or an infinity is
/// not a value of the type, so the save refuses it BY NAME (at is
/// <owner>.<field>) for <root>_save_checked, as for a value over its bound.
pub fn table_float_refuse(ids: &mut IdTable, at: &str) {
    if ids.refused.is_none() {
        ids.refused = Some(format!("non_finite_float: {at}"));
    }
}

/// Walk past a payload the reader does not store, by kind (§4), bounded by the
/// enclosing body's end. The fixed-width kinds skip their width; the framed
/// kinds read L and skip L bytes; a reference-shaped kind (pointer, enum) reads
/// one LEB128; a union reads its arm header and L. A kind the reader does not
/// know at all is framing damage and returns false (§3).
pub fn leb_skip(buf: &[u8], off: &mut usize, end: usize, kind: u8) -> bool {
    // A nested body has its own byte boundary (§3, §4). Parse every LEB from
    // that slice so a truncated header cannot consume the parent's next byte.
    if end > buf.len() { return false; }
    let bounded = &buf[..end];
    let fixed = |o: &mut usize, w: usize| -> bool {
        if *o > end || w > end - *o { return false; }
        *o += w;
        true
    };
    match kind {
        1 | 2 | 6 | 20 | 25 => fixed(off, 1),
        3 | 7 | 21 | 26 => fixed(off, 2),
        4 | 8 | 10 | 22 | 27 => fixed(off, 4),
        // kind 36, a timestamp, is one eight-byte payload (§2.11, §3)
        5 | 9 | 11 | 23 | 28 | 36 => fixed(off, 8),
        18 | 19 | 24 | 29 => fixed(off, 16),
        17 | 30 => {
            leb_read(bounded, off).is_some() && *off <= end
        }
        12 | 13 | 14 | 16 | 31 | 32 | 33 => {
            let Some(n) = leb_read(bounded, off) else { return false; };
            let n = n as usize;
            if *off > end || n > end - *off { return false; }
            *off += n;
            true
        }
        15 => {
            let Some(arm) = leb_read(bounded, off) else { return false; };
            if arm == 0 { return true; }
            if *off >= end { return false; }
            *off += 1;
            let Some(n) = leb_read(bounded, off) else { return false; };
            let n = n as usize;
            if *off > end || n > end - *off { return false; }
            *off += n;
            true
        }
        _ => false,
    }
}

/// THE RETAIN-UNKNOWN STORE (docs/SPEC-TABLES.md §6.6): the caller's two
/// bounded stores threaded through the retaining load and save. 'bytes' holds
/// the retained records, 'ids' the retained ids' own list; both are the
/// caller's, neither grows on demand, and a whole record that does not fit is
/// dropped and counted once in the report's 'retain_lost'.
#[derive(Default)]
pub struct TableRetain {
    pub bytes: Vec<u8>,
    pub ids: Vec<u64>,
}

/// THE WALK'S NESTING CAP (docs/SPEC-TABLES.md §6.6): a small stated constant,
/// the one depth on this path a file can drive.
pub const TABLE_RETAIN_DEPTH_CAP: usize = 64;

/// THE RETAIN PATH (docs/SPEC-TABLES.md §6.6): the region's address for one
/// body — depth steps, each the pair (field ordinal, element index). A step is
/// computed locally as the walk descends, so one address is available on both
/// sides of the round trip, and a body whose element count is data-driven is
/// addressed by the same step a nested table takes.
#[derive(Clone, Copy)]
pub struct TableRetainPath {
    pub depth: u32,
    pub steps: [(u32, u32); TABLE_RETAIN_DEPTH_CAP],
}

impl Default for TableRetainPath {
    fn default() -> Self {
        TableRetainPath {
            depth: 0,
            steps: [(0, 0); TABLE_RETAIN_DEPTH_CAP],
        }
    }
}

impl TableRetainPath {
    pub fn step(&self, ordinal: u32, index: u32) -> TableRetainPath {
        let mut p = *self;
        if (p.depth as usize) < TABLE_RETAIN_DEPTH_CAP {
            p.steps[p.depth as usize] = (ordinal, index);
            p.depth += 1;
        }
        p
    }
}

/// A retained node body's address follows the Rc through save numbering.
/// Input node indices may change when the save visits the reachable graph;
/// the pointer identity is stable for this load/save pair and fits in the
/// path's existing two words without growing either caller-owned store.
pub fn retain_node_path(node: &Rc<dyn Any>) -> TableRetainPath {
    let key = Rc::as_ptr(node) as *const () as usize as u64;
    TableRetainPath::default().step(key as u32, (key >> 32) as u32)
}

pub fn retain_bind_node(retain: &mut TableRetain, node: &Rc<dyn Any>) {
    let path = retain_node_path(node);
    let mut at = 0usize;
    while at + 17 <= retain.bytes.len() {
        let len = retain_record_len(&retain.bytes[at..]);
        if len < 17 || len > retain.bytes.len() - at { break; }
        let depth = retain_record_depth(&retain.bytes[at..]);
        if depth > 0 && 17 + 8 * depth <= len {
            retain.bytes[at + 17..at + 21].copy_from_slice(&path.steps[0].0.to_le_bytes());
            retain.bytes[at + 21..at + 25].copy_from_slice(&path.steps[0].1.to_le_bytes());
        }
        at += len;
    }
}

/// ONE RETAINED RECORD is
/// [u32 len][u32 depth][u64 field id][u8 kind][depth × (u32 ordinal, u32 index)][resolved payload],
/// len = 17 + 8×depth + payload.len(). The payload is READER-PRIVATE (§6.6):
/// every reference is the eight-byte id it names and every framed length is
/// eight bytes, so a save re-interns references against a different id table.
pub fn retain_record_len(rec: &[u8]) -> usize {
    (u32::from_le_bytes(rec[..4].try_into().unwrap()) & 0x7fff_ffff) as usize
}

/// The high bit of the private record length is a per-save placement mark
/// (§6.6). Each save clears it, then counts records no body visited.
pub fn retain_clear_placed(retain: &mut TableRetain) {
    let mut at = 0usize;
    while at + 4 <= retain.bytes.len() {
        let len = retain_record_len(&retain.bytes[at..]);
        if len < 17 || len > retain.bytes.len() - at { break; }
        retain.bytes[at + 3] &= 0x7f;
        at += len;
    }
}

pub fn retain_count_unplaced(retain: &TableRetain, report: &mut TableReport) {
    let mut at = 0usize;
    while at + 4 <= retain.bytes.len() {
        let len = retain_record_len(&retain.bytes[at..]);
        if len < 17 || len > retain.bytes.len() - at { break; }
        if retain.bytes[at + 3] & 0x80 == 0 { report.retain_lost += 1; }
        at += len;
    }
}

/// The record's path depth (docs/SPEC-TABLES.md §6.6), read from the four bytes
/// after the record length.
fn retain_record_depth(rec: &[u8]) -> usize {
    u32::from_le_bytes(rec[4..8].try_into().unwrap()) as usize
}

/// Does the record's path equal the save's current path? (docs/SPEC-TABLES.md
/// §6.6): a retained record goes back at the end of the body it came from, so a
/// save emits only the records whose steps name the body it is writing.
fn retain_path_eq(rec: &[u8], depth: usize, path: &TableRetainPath) -> bool {
    if depth != path.depth as usize {
        return false;
    }
    for i in 0..depth {
        let base = 17 + 8 * i;
        let o = u32::from_le_bytes(rec[base..base + 4].try_into().unwrap());
        let x = u32::from_le_bytes(rec[base + 4..base + 8].try_into().unwrap());
        if o != path.steps[i].0 || x != path.steps[i].1 {
            return false;
        }
    }
    true
}

/// Does the record live UNDER the save's current path (docs/SPEC-TABLES.md
/// §6.6), and, when 'field' is set, under the step whose ordinal is 'ordinal'?
/// A known ancestor body reset by a later occurrence discards every record
/// under it, so the discarded record dies with the body that carried it.
fn retain_under(rec: &[u8], depth: usize, path: &TableRetainPath, field: bool, ordinal: u32) -> bool {
    if depth <= path.depth as usize {
        return false;
    }
    for i in 0..path.depth as usize {
        let base = 17 + 8 * i;
        let o = u32::from_le_bytes(rec[base..base + 4].try_into().unwrap());
        let x = u32::from_le_bytes(rec[base + 4..base + 8].try_into().unwrap());
        if o != path.steps[i].0 || x != path.steps[i].1 {
            return false;
        }
    }
    if !field {
        return true;
    }
    let base = 17 + 8 * path.depth as usize;
    let o = u32::from_le_bytes(rec[base..base + 4].try_into().unwrap());
    o == ordinal
}

struct RetainCur<'a> {
    buf: &'a [u8],
    off: usize,
}

impl<'a> RetainCur<'a> {
    fn leb(&mut self) -> Option<u64> {
        let mut off = self.off;
        let v = leb_read(self.buf, &mut off)?;
        self.off = off;
        Some(v)
    }
}

/// THE WALK IS AN INTERPRETATION (docs/SPEC-TABLES.md §6.6): a reference above
/// the entry count, at an entry of zero, or at one of the three reserved ids
/// drops the record. It reads kind bytes, lengths and references and nothing
/// else, so no value is decoded and no bound is checked.
fn retain_id(ids: &[u64], r: u64, bad: &mut bool) -> u64 {
    if r == 0 || r as usize > ids.len() {
        *bad = true;
        return 0;
    }
    let id = ids[r as usize - 1];
    if id == 0 || id == NODE_TABLE_ID || id == BUILD_VERSION_ID || id == VOCABULARY_ID {
        *bad = true;
        return 0;
    }
    id
}

/// Step one payload of 'kind' by §3's framing rules alone, over a retained
/// cursor. Kinds 13, 14, 15, 16, 17 and 30 never come back this way: the walk
/// resolves each, and a kind outside the closed set is framing damage (§3).
fn retain_skip(c: &mut RetainCur, kind: u8) -> bool {
    let w = |c: &mut RetainCur, n: usize| -> bool {
        if c.off + n > c.buf.len() {
            return false;
        }
        c.off += n;
        true
    };
    match kind {
        1 | 2 | 6 | 20 | 25 => w(c, 1),
        3 | 7 | 21 | 26 => w(c, 2),
        4 | 8 | 10 | 22 | 27 => w(c, 4),
        5 | 9 | 11 | 23 | 28 | 36 => w(c, 8),
        18 | 19 | 24 | 29 => w(c, 16),
        12 | 31 | 32 | 33 => {
            let Some(n) = c.leb() else { return false; };
            let n = n as usize;
            if c.off + n > c.buf.len() {
                return false;
            }
            c.off += n;
            true
        }
        _ => false,
    }
}

/// retain_capture resolves one unknown field's payload. None is the DROP — a
/// kind 17 at any depth, a reference the walk cannot resolve, a length that
/// runs past, or a depth past the cap — and the caller counts one retain_lost
/// (docs/SPEC-TABLES.md §6.6).
pub fn retain_capture(ids: &[u64], kind: u8, payload: &[u8]) -> Option<Vec<u8>> {
    let mut c = RetainCur { buf: payload, off: 0 };
    let mut out = Vec::new();
    let mut bad = false;
    retain_resolve(&mut c, ids, kind, 0, &mut out, &mut bad);
    if bad || c.off != payload.len() {
        return None;
    }
    Some(out)
}

fn retain_resolve(c: &mut RetainCur, ids: &[u64], kind: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    if *bad || depth > TABLE_RETAIN_DEPTH_CAP {
        *bad = true;
        return;
    }
    match kind {
        17 => {
            // THE NODE-INDEX CLASS met at the outer kind (§6.6)
            *bad = true;
        }
        30 => {
            // an enum variant reference; zero is None and names no id
            let Some(r) = c.leb() else { *bad = true; return; };
            if r == 0 {
                out.extend_from_slice(&0u64.to_le_bytes());
                return;
            }
            let id = retain_id(ids, r, bad);
            out.extend_from_slice(&id.to_le_bytes());
        }
        15 => {
            let Some(r) = c.leb() else { *bad = true; return; };
            if r == 0 {
                out.extend_from_slice(&0u64.to_le_bytes());
                return;
            }
            let id = retain_id(ids, r, bad);
            out.extend_from_slice(&id.to_le_bytes());
            if c.off >= c.buf.len() {
                *bad = true;
                return;
            }
            let arm_kind = c.buf[c.off];
            c.off += 1;
            out.push(arm_kind);
            retain_framed(c, ids, arm_kind, depth, out, bad);
        }
        13 | 14 | 16 => retain_framed(c, ids, kind, depth, out, bad),
        _ => {
            let from = c.off;
            if !retain_skip(c, kind) {
                *bad = true;
                return;
            }
            out.extend_from_slice(&c.buf[from..c.off]);
        }
    }
}

fn retain_framed(c: &mut RetainCur, ids: &[u64], kind: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    let Some(n) = c.leb() else { *bad = true; return; };
    let n = n as usize;
    if c.off + n > c.buf.len() {
        *bad = true;
        return;
    }
    let mut sub = RetainCur { buf: &c.buf[c.off..c.off + n], off: 0 };
    c.off += n;
    let mark = out.len();
    out.extend_from_slice(&[0u8; 8]);
    retain_content(&mut sub, ids, kind, depth + 1, out, bad);
    if *bad {
        return;
    }
    let len = (out.len() - mark - 8) as u64;
    out[mark..mark + 8].copy_from_slice(&len.to_le_bytes());
}

fn retain_content(c: &mut RetainCur, ids: &[u64], kind: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    if *bad || depth > TABLE_RETAIN_DEPTH_CAP {
        *bad = true;
        return;
    }
    match kind {
        17 => {
            *bad = true;
            return;
        }
        13 => retain_body(c, ids, depth, out, bad),
        14 | 16 => {
            if c.off >= c.buf.len() {
                *bad = true;
                return;
            }
            let ek = c.buf[c.off];
            c.off += 1;
            if ek == 17 {
                // an array whose element kind is 17 is the excluded class
                *bad = true;
                return;
            }
            out.push(ek);
            let Some(count) = c.leb() else { *bad = true; return; };
            out.extend_from_slice(&count.to_le_bytes());
            for _ in 0..count {
                if kind == 16 {
                    retain_keyed(c, ids, ek, depth, out, bad);
                } else {
                    retain_resolve(c, ids, ek, depth, out, bad);
                }
                if *bad {
                    return;
                }
            }
        }
        15 | 30 => retain_resolve(c, ids, kind, depth, out, bad),
        _ => {
            out.extend_from_slice(&c.buf[c.off..]);
            c.off = c.buf.len();
        }
    }
    if !*bad && c.off != c.buf.len() {
        *bad = true;
    }
}

fn retain_keyed(c: &mut RetainCur, ids: &[u64], ek: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    let Some(r) = c.leb() else { *bad = true; return; };
    let id = retain_id(ids, r, bad);
    out.extend_from_slice(&id.to_le_bytes());
    retain_framed(c, ids, ek, depth, out, bad);
}

fn retain_body(c: &mut RetainCur, ids: &[u64], depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    loop {
        let Some(r) = c.leb() else { *bad = true; return; };
        if r == 0 {
            out.extend_from_slice(&0u64.to_le_bytes());
            return;
        }
        if c.off >= c.buf.len() {
            *bad = true;
            return;
        }
        let kind = c.buf[c.off];
        c.off += 1;
        let id = retain_id(ids, r, bad);
        out.extend_from_slice(&id.to_le_bytes());
        out.push(kind);
        retain_resolve(c, ids, kind, depth, out, bad);
        if *bad {
            return;
        }
    }
}

/// THE FILE STILL CARRIES ONE ID TABLE (docs/SPEC-TABLES.md §6.6): an id this
/// build can name takes its entry from the generated table (already interned by
/// the plain save walk), a retained id takes its entry from the caller's list,
/// and both are numbered into one trailer in first-use order. A retained id
/// past the caller's capacity answers 0 and the caller drops the record.
pub fn retain_ref(ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], id: u64) -> u64 {
    for (i, &e) in ids.entries.iter().enumerate() {
        if e == id {
            return i as u64 + 1;
        }
    }
    if known.binary_search(&id).is_ok() {
        ids.entries.push(id);
        return ids.entries.len() as u64;
    }
    if retain.ids.len() >= retain.ids.capacity() {
        return 0;
    }
    retain.ids.push(id);
    ids.entries.push(id);
    ids.entries.len() as u64
}

/// retain_emit_tail re-emits the retained records whose path names the body the
/// save is writing right now — in the order retained (docs/SPEC-TABLES.md §6.6),
/// interning every id the payload names through [retain_ref]. A record whose id
/// cannot be interned is dropped and counted one retain_lost, and the save is
/// never refused.
pub fn retain_emit_tail(out: &mut Vec<u8>, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], path: TableRetainPath, report: &mut TableReport) {
    let mut at = 0usize;
    while at + 4 <= retain.bytes.len() {
        let len = retain_record_len(&retain.bytes[at..]);
        if len < 17 || at + len > retain.bytes.len() {
            break;
        }
        let rec = &retain.bytes[at..at + len];
        let depth = retain_record_depth(rec);
        if depth > TABLE_RETAIN_DEPTH_CAP || 17 + 8 * depth > len {
            break;
        }
        at += len;
        if !retain_path_eq(rec, depth, &path) {
            continue;
        }
        let field_id = u64::from_le_bytes(rec[8..16].try_into().unwrap());
        let kind = rec[16];
        let payload = rec[17 + 8 * depth..len].to_vec();
        retain.bytes[at - len + 3] |= 0x80;
        let mut bad = false;
        let slot = retain_ref(ids, retain, known, field_id);
        if slot == 0 {
            report.retain_lost += 1;
            continue;
        }
        let mut c = RetainCur { buf: &payload, off: 0 };
        let mut body = Vec::new();
        retain_emit_resolve(&mut c, ids, retain, known, kind, 0, &mut body, &mut bad);
        if bad || c.off != payload.len() {
            report.retain_lost += 1;
            continue;
        }
        leb_write(out, slot);
        out.push(kind);
        out.extend_from_slice(&body);
    }
}

/// retain_discard removes every retained record under the field at 'ordinal' in
/// the save's current path's body (docs/SPEC-TABLES.md §6.6): a known ancestor
/// body reset by a later legal occurrence dies with its records, and only the
/// winning occurrence's records survive to the save. The discard moves neither
/// retained nor retain_lost — both stay monotonic.
pub fn retain_discard(retain: &mut TableRetain, path: TableRetainPath, ordinal: u32) {
    let mut read = 0usize;
    let mut write = 0usize;
    while read + 4 <= retain.bytes.len() {
        let len = retain_record_len(&retain.bytes[read..]);
        if len < 17 || read + len > retain.bytes.len() {
            break;
        }
        let rec = &retain.bytes[read..read + len];
        let depth = retain_record_depth(rec);
        let drop = depth <= TABLE_RETAIN_DEPTH_CAP
            && 17 + 8 * depth <= len
            && retain_under(rec, depth, &path, true, ordinal);
        if !drop {
            if write != read {
                retain.bytes.copy_within(read..read + len, write);
            }
            write += len;
        }
        read += len;
    }
    retain.bytes.truncate(write);
}

/// A repeated map key replaces one entry occurrence (§6.6), including the
/// unknown fields retained directly in that entry and its nested bodies.
pub fn retain_discard_body(retain: &mut TableRetain, path: TableRetainPath) {
    let mut read = 0usize;
    let mut write = 0usize;
    while read + 4 <= retain.bytes.len() {
        let len = retain_record_len(&retain.bytes[read..]);
        if len < 17 || len > retain.bytes.len() - read { break; }
        let rec = &retain.bytes[read..read + len];
        let depth = retain_record_depth(rec);
        let drop = depth <= TABLE_RETAIN_DEPTH_CAP && 17 + 8 * depth <= len
            && (retain_path_eq(rec, depth, &path) || retain_under(rec, depth, &path, false, 0));
        if !drop {
            if write != read { retain.bytes.copy_within(read..read + len, write); }
            write += len;
        }
        read += len;
    }
    retain.bytes.truncate(write);
}

fn retain_emit_ref(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], bad: &mut bool) -> u64 {
    if c.off + 8 > c.buf.len() {
        *bad = true;
        return 0;
    }
    let id = u64::from_le_bytes(c.buf[c.off..c.off + 8].try_into().unwrap());
    c.off += 8;
    // The zero reference is a union/enum None, not an id to intern (§3, §6.6).
    if id == 0 { return 0; }
    retain_ref(ids, retain, known, id)
}

#[allow(clippy::too_many_arguments)] // one recursive walk's shared state, threaded explicitly
fn retain_emit_resolve(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], kind: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    if *bad || depth > TABLE_RETAIN_DEPTH_CAP {
        *bad = true;
        return;
    }
    match kind {
        17 => {
            *bad = true;
        }
        30 => {
            let slot = retain_emit_ref(c, ids, retain, known, bad);
            leb_write(out, slot);
        }
        15 => {
            let slot = retain_emit_ref(c, ids, retain, known, bad);
            leb_write(out, slot);
            if slot == 0 {
                return;
            }
            if c.off >= c.buf.len() {
                *bad = true;
                return;
            }
            let arm_kind = c.buf[c.off];
            c.off += 1;
            out.push(arm_kind);
            retain_emit_framed(c, ids, retain, known, arm_kind, depth, out, bad);
        }
        13 | 14 | 16 => retain_emit_framed(c, ids, retain, known, kind, depth, out, bad),
        _ => {
            let from = c.off;
            if !retain_skip(c, kind) {
                *bad = true;
                return;
            }
            out.extend_from_slice(&c.buf[from..c.off]);
        }
    }
}

#[allow(clippy::too_many_arguments)] // one recursive walk's shared state, threaded explicitly
fn retain_emit_framed(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], kind: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    if c.off + 8 > c.buf.len() {
        *bad = true;
        return;
    }
    let len = u64::from_le_bytes(c.buf[c.off..c.off + 8].try_into().unwrap()) as usize;
    c.off += 8;
    if c.off + len > c.buf.len() {
        *bad = true;
        return;
    }
    let mut sub = RetainCur { buf: &c.buf[c.off..c.off + len], off: 0 };
    c.off += len;
    let mut content = Vec::new();
    retain_emit_content(&mut sub, ids, retain, known, kind, depth + 1, &mut content, bad);
    if *bad {
        return;
    }
    leb_write(out, content.len() as u64);
    out.extend_from_slice(&content);
}

#[allow(clippy::too_many_arguments)] // one recursive walk's shared state, threaded explicitly
fn retain_emit_content(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], kind: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    if *bad || depth > TABLE_RETAIN_DEPTH_CAP {
        *bad = true;
        return;
    }
    match kind {
        17 => {
            *bad = true;
            return;
        }
        13 => retain_emit_body(c, ids, retain, known, depth, out, bad),
        14 | 16 => {
            if c.off >= c.buf.len() {
                *bad = true;
                return;
            }
            let ek = c.buf[c.off];
            c.off += 1;
            out.push(ek);
            if c.off + 8 > c.buf.len() {
                *bad = true;
                return;
            }
            let count = u64::from_le_bytes(c.buf[c.off..c.off + 8].try_into().unwrap());
            c.off += 8;
            leb_write(out, count);
            for _ in 0..count {
                if kind == 16 {
                    retain_emit_keyed(c, ids, retain, known, ek, depth, out, bad);
                } else {
                    retain_emit_resolve(c, ids, retain, known, ek, depth, out, bad);
                }
                if *bad {
                    return;
                }
            }
        }
        15 | 30 => retain_emit_resolve(c, ids, retain, known, kind, depth, out, bad),
        _ => {
            out.extend_from_slice(&c.buf[c.off..]);
            c.off = c.buf.len();
        }
    }
    if !*bad && c.off != c.buf.len() {
        *bad = true;
    }
}

#[allow(clippy::too_many_arguments)] // one recursive walk's shared state, threaded explicitly
fn retain_emit_keyed(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], ek: u8, depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    let slot = retain_emit_ref(c, ids, retain, known, bad);
    leb_write(out, slot);
    retain_emit_framed(c, ids, retain, known, ek, depth, out, bad);
}

fn retain_emit_body(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], depth: usize, out: &mut Vec<u8>, bad: &mut bool) {
    loop {
        if c.off + 8 > c.buf.len() {
            *bad = true;
            return;
        }
        let id = u64::from_le_bytes(c.buf[c.off..c.off + 8].try_into().unwrap());
        c.off += 8;
        if id == 0 {
            leb_write(out, 0);
            return;
        }
        if c.off >= c.buf.len() {
            *bad = true;
            return;
        }
        let kind = c.buf[c.off];
        c.off += 1;
        let slot = retain_ref(ids, retain, known, id);
        if slot == 0 {
            *bad = true;
            return;
        }
        leb_write(out, slot);
        out.push(kind);
        retain_emit_resolve(c, ids, retain, known, kind, depth, out, bad);
        if *bad {
            return;
        }
    }
}

/// Every id this unit's closure can spell, ascending (docs/SPEC-TABLES.md
/// §6.6): the set a save consults to decide which store an id inside a
/// retained record takes — an id it can name takes the generated table's
/// entry, any other takes one from the caller's list.
pub static KNOWN_IDS: &[u64] = &[
`)
	for _, id := range ir.TableWireIds(u) {
		fmt.Fprintf(&b, "    0x%016x,\n", id)
	}
	b.WriteString("];\n")
	b.WriteString(textHelpers())
	b.WriteString(cookRegionWriterRust)
	text := b.String()
	text = strings.Replace(text, "STRING_TYPE_ID_VALUE", fmt.Sprintf("0x%016x", ir.StringWireTypeId), 1)
	text = strings.Replace(text, "BYTES_TYPE_ID_VALUE", fmt.Sprintf("0x%016x", ir.BytesWireTypeId), 1)
	return text
}

// cardVariableTableNames returns the names of a file's tables the variable
// codec emits — the carried variable tables and every table one of them
// reaches (docs/SPEC-TABLES.md §2, §3.1). A variable table this leg does not
// carry stays ABSENT: the boundary is scoped to the wire, so a unit mixing a
// fixed table with a variable table holding a union still emits the fixed
// form and never mis-encodes the union.
func cardVariableTableNames(g *gen) []string {
	vc := g.vc
	if vc == nil {
		vc = newVariableClass(g.unit)
		g.vc = vc
	}
	return fileVariableTableNames(g.unit, g.file, vc)
}

func fileVariableTableNames(u *ir.Unit, file *ir.File, vc *variableClass) []string {
	if vc == nil {
		vc = newVariableClass(u)
	}
	names := make([]string, 0)
	for _, st := range file.Tables {
		if vc.emitted[st.Name] {
			names = append(names, st.Name)
		}
	}
	// a `type` the carried set reaches by value is a body of this file's
	// codec too (docs/SPEC-TABLES.md §2, §3)
	for _, d := range file.Decls {
		if st, ok := d.(*ir.Struct); ok && vc.emitted[st.Name] {
			names = append(names, st.Name)
		}
	}
	sort.Strings(names)
	return names
}

// isVariableRoot reports whether an emitted body is a ROOT of the variable
// form, which takes the whole-file save, load and text roots: every table of
// the closure, a FIXED table included, because a fixed table's own form-1 file
// is a variable-form file like any other (docs/SPEC-TABLES.md §2.2, §3). A map
// entry and a `type` are bodies only, never a file's root.
func (g *gen) isVariableRoot(st *ir.Struct) bool {
	return st.IsTable && ir.TableClosure(g.unit)[st.Name] && !st.IsMapEntry()
}

// variableModule emits one source file's variable-table constant block. Nil
// for a file that declares no card-boundary variable table (a table-free
// file's output is byte-identical with this module in or out).
func (g *gen) variableModule() []byte {
	names := cardVariableTableNames(g)
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(header(g.file.Base, g.unit.Package, "the variable wire's field header constants (docs/SPEC-TABLES.md §3, §5)"))
	for _, n := range names {
		g.emitVariableConstants(&b, vStruct(g.unit, n))
	}
	return []byte(b.String())
}

// emitVariableConstants emits one variable table's field header constants: the
// two facts §3's field header is made of — the field's wire id (§5, the
// fnv1a64 of its `was` alias when one is declared and of its own name
// otherwise) and its kind byte (§3). The save and load bodies that frame these
// constants — the id table's reference numbering, the trailer, and the node
// table of §3.1 — are the next stage.
func (g *gen) emitVariableConstants(b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "\n// %s — the card slice of the variable wire (§2.5, §2.9).\n", st.Name)
	fmt.Fprintf(b, "pub mod %s_variable {\n", ir.RustSnake(st.Name))
	for _, f := range st.Fields {
		id := ir.TableFieldWireId(f)
		kind := ir.TableWireFieldKind(f)
		fmt.Fprintf(b, "    /// %s: id %016x, kind %d\n", f.Name, id, kind)
		fmt.Fprintf(b, "    pub const %s: (u64, u8) = (0x%016x, %d);\n", strings.ToUpper(ir.RustSnake(f.Name)), id, kind)
	}
	b.WriteString("}\n")
}

// TableEnumsModule holds every enum the unit's variable tables reach, with its
// JSON name maps, ONCE per crate: each file's table module imports it rather
// than declaring its own copy, so one schema enum is one Rust type across the
// table layer (docs/SPEC-TABLES.md, the Rust backend's notes).
const TableEnumsModule = "table_enums"

// tableEnumsModule emits TableEnumsModule; nil when no carried table reaches an
// enum. The order is the files' declaration order, then first reach.
func tableEnumsModule(u *ir.Unit, vc *variableClass) []byte {
	seen := map[string]bool{}
	var enums []*ir.Enum
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: ir.TableClosure(u), vc: vc}
		names := cardVariableTableNames(g)
		if len(names) == 0 {
			continue
		}
		for _, e := range g.collectVariableEnums(names) {
			if !seen[e.Name] {
				seen[e.Name] = true
				enums = append(enums, e)
			}
		}
	}
	if len(enums) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package, "the table layer's enums and their JSON names, once per crate (docs/SPEC-TABLES.md §3, §16.2)"))
	b.WriteString("\n")
	emitVariableEnums(&b, enums)
	emitKeyedAccessors(&b, enums)
	b.WriteString("// ---- the JSON text form's names (docs/SPEC-TABLES.md §16.2) ----\n\n")
	for _, e := range enums {
		emitEnumText(&b, e)
	}
	return []byte(b.String())
}

// emitKeyedAccessors emits the keyed array's accessor (docs/PORTING.md M5,
// docs/SPEC-TABLES.md §2.3): key 1 lives in slot 0 and E.Max in the last, and
// None (key 0) and any key past E.Max are REFUSED in every build by one
// unsigned compare covering both ends, then a panic that names the KEY. The
// slice's own bounds check is not the technique: it names an index, and the
// shift would be the caller's to spell. Every keyed array is a slice of its
// slots, a `Vec` on the variable struct and an array on a fixed row, so the
// runtime is generic over the slice; each enum gets the typed spelling, and
// iteration yields the key beside the element.
func emitKeyedAccessors(b *strings.Builder, enums []*ir.Enum) {
	b.WriteString(`// ---- keyed arrays index by KEY, refused at both ends (docs/SPEC-TABLES.md §2.3) ----

/// The slot a key owns in a keyed array of that many slots: key 1 in slot 0, E.Max
/// in the last. None (key 0) and every key past E.Max are refused in every
/// build: one unsigned compare covers both ends.
#[inline(always)]
pub fn table_key_slot(key: usize, slots: usize) -> usize {
    let slot = key.wrapping_sub(1);
    if slot >= slots {
        table_key_refused(key, slots);
    }
    slot
}

/// The refusal: a panic naming the key, never an index.
#[cold]
#[inline(never)]
#[track_caller]
pub fn table_key_refused(key: usize, slots: usize) -> ! {
    panic!("keyed array: key {key} names no slot (keys are 1 to {slots}; None is 0) (docs/SPEC-TABLES.md §2.3)")
}

/// The slot the key owns.
#[inline(always)]
#[track_caller]
pub fn table_keyed<T>(slots: &[T], key: usize) -> &T {
    &slots[table_key_slot(key, slots.len())]
}

/// The slot the key owns, to write.
#[inline(always)]
#[track_caller]
pub fn table_keyed_mut<T>(slots: &mut [T], key: usize) -> &mut T {
    let slot = table_key_slot(key, slots.len());
    &mut slots[slot]
}

/// Every slot beside the key that owns it, in key order.
pub fn table_keyed_iter<T>(slots: &[T]) -> impl Iterator<Item = (usize, &T)> {
    slots.iter().enumerate().map(|(i, x)| (i + 1, x))
}

`)
	for _, e := range enums {
		fmt.Fprintf(b, "impl %s {\n", e.Name)
		fmt.Fprintf(b, "    /// The slot of a `[%s]T` keyed array this key owns; None is refused.\n", e.Name)
		b.WriteString("    #[inline(always)]\n    #[track_caller]\n")
		b.WriteString("    pub fn keyed<T>(self, slots: &[T]) -> &T {\n        table_keyed(slots, self as usize)\n    }\n\n")
		fmt.Fprintf(b, "    /// The slot of a `[%s]T` keyed array this key owns, to write; None is refused.\n", e.Name)
		b.WriteString("    #[inline(always)]\n    #[track_caller]\n")
		b.WriteString("    pub fn keyed_mut<T>(self, slots: &mut [T]) -> &mut T {\n        table_keyed_mut(slots, self as usize)\n    }\n\n")
		fmt.Fprintf(b, "    /// Every slot of a `[%s]T` keyed array beside the key that owns it.\n", e.Name)
		fmt.Fprintf(b, "    pub fn keyed_iter<T>(slots: &[T]) -> impl Iterator<Item = (%s, &T)> {\n", e.Name)
		fmt.Fprintf(b, "        const KEYS: [%s; %d] = [", e.Name, len(e.Variants))
		for i, v := range e.Variants {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s::%s", e.Name, rustVariant(v))
		}
		b.WriteString("];\n        KEYS.iter().copied().zip(slots.iter())\n    }\n}\n\n")
	}
}
