package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VARIABLE WIRE (docs/SPEC-TABLES.md §3, §3.1), form byte 1, for exactly
// the card record's constructs (§2.5, §2.9): a plain `table` root and tables
// as list elements; bool, int32, int64 and enum fields with their defaults and
// ranges; string(N); *string; and unbounded []T lists of tables.
//
// This file emits the RUST half of that wire. It is the FIRST STAGE of the
// port: the framing runtime — the canonical LEB128 every length, count and
// reference on a FILE spells (§3), the form byte, and the tolerant read report
// (§4, §5) — and, per source file, the constant every generated body codec
// will carry: each field's wire id (§5) beside its kind byte (§3). Those two
// facts are the whole field header of §3; what follows them — the id table's
// reference numbering, the trailer that carries it, and the node table of
// §3.1 that carries the *string blobs and the []T list elements — is the next
// stage and is not emitted here.
//
// The modules are emitted STAND-ALONE. [cardVariableModules] carries the
// card-boundary refusal (§11) on the card's own path, while
// [scopedVariableModules] — GenerateLineage's entry point — leaves a variable
// table whose fields the boundary does not carry ABSENT rather than refusing
// the unit whole, so the fixed-form corpus (rusttable_test.go's `pointered`)
// still emits its fixed form beside a variable table it does not encode.
const VariableRuntimeModule = "variable_runtime"

// cardVariableModules returns the variable wire's modules for a unit whose
// variable tables the card boundary accepts: the shared runtime and, for each
// source file that declares a variable table, that file's constant block. The
// refusal runs FIRST (§11), scoped here to the variable wire path alone.
func cardVariableModules(u *ir.Unit) (map[string][]byte, error) {
	if len(ir.VariableTables(u)) == 0 {
		return nil, nil
	}
	if err := refuseCardUnsupportedVariableConstructs(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	out[VariableRuntimeModule+".rs"] = []byte(variableRuntimeModule(u))
	out[VariableDescriptorModule+".rs"] = variableDescriptorModule(u)
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: ir.TableClosure(u)}
		if body := g.variableModule(); body != nil {
			out[strings.ToLower(f.Base)+"_variable.rs"] = body
		}
		if body := g.variableCodecModule(); body != nil {
			out[strings.ToLower(f.Base)+"_table.rs"] = body
		}
	}
	return out, nil
}

// scopedVariableModules is the GENERATE-path entry point: the same modules as
// [cardVariableModules], but WITHOUT the whole-unit refusal. A variable table
// whose fields the card boundary does not carry is left ABSENT — its wire is
// not emitted and nothing is mis-encoded — so a unit that mixes a fixed table
// with a pointered variable table (rusttable_test.go's `pointered`) still
// emits the fixed form. The by-name refusal lives on the card's own path, in
// [cardVariableModules], where a card unit that gains an excluded construct is
// refused naming table, field and construct (docs/SPEC-TABLES.md §11).
func scopedVariableModules(u *ir.Unit) map[string][]byte {
	if len(ir.VariableTables(u)) == 0 {
		return nil
	}
	hasCard := false
	for _, f := range u.Files {
		if len(cardVariableTableNames(&gen{unit: u, file: f})) > 0 {
			hasCard = true
			break
		}
	}
	if !hasCard {
		return nil
	}
	out := map[string][]byte{}
	out[VariableRuntimeModule+".rs"] = []byte(variableRuntimeModule(u))
	out[VariableDescriptorModule+".rs"] = variableDescriptorModule(u)
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: ir.TableClosure(u)}
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
/// The VARIABLE FORM's form byte (docs/SPEC-TABLES.md §3): the one arm whose
/// header is the form byte alone. Nothing is aligned and nothing is padded.
pub const TABLE_WIRE_FORM: u8 = 1;

/// The tolerant read report (docs/SPEC-TABLES.md §4, §5): a field this build
/// cannot name is skipped and counted, a field it can name but that arrives at
/// another kind is a kind mismatch, and an absent field reads its default.
/// widened counts a kind that GREW since the writer (§4), duplicate a repeated
/// id (zero on the card's wire: no maps), and refused is the form byte this
/// reader does not carry (§3) — the verdict, not one of §4's events.
#[derive(Default, Clone, Copy)]
pub struct TableReport {
    pub unknown: u64,
    pub kind_mismatch: u64,
    pub widened: u64,
    pub clamped: u64,
    pub duplicate: u64,
    pub malformed: bool,
    pub refused: bool,
    /// RETAIN-UNKNOWN'S TWO COUNTERS (docs/SPEC-TABLES.md §6.6): retained
    /// counts the unknown fields whose bytes were kept, retain_lost counts
    /// every unknown this load or save could not keep (capacity, an excluded
    /// class, or a damaged resolving walk). Both are zero in every read that
    /// did not opt in, and both ride the one report type for the reason
    /// duplicate does (§4).
    pub retained: u64,
    pub retain_lost: u64,
}

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
pub const STRING_TYPE_ID: u64 = 0x704be0d8faaffc58;
pub const BYTES_TYPE_ID: u64 = 0x2f2ec0474f1c4fe4;
pub const BUILD_VERSION_ID: u64 = 0xfffffffffffffffe;
pub const VOCABULARY_ID: u64 = 0xfffffffffffffffd;

/// ILL-FORMED TEXT IS DAMAGE (docs/SPEC-TABLES.md §3, §4): a kind 12 payload is
/// well-formed UTF-8 with no zero byte, checked as it ARRIVES and before the
/// reader's own bound.
pub fn text_valid(b: &[u8]) -> bool {
    std::str::from_utf8(b).is_ok() && !b.contains(&0)
}

/// The id table a save builds in FIRST-USE order (docs/SPEC-TABLES.md §3): one
/// entry per distinct id, referenced by position from 1. id_ref returns the
/// position, appending the id when it has not been named yet.
#[derive(Default)]
pub struct IdTable {
    pub entries: Vec<u64>,
}

impl IdTable {
    pub fn new() -> Self {
        IdTable { entries: Vec::new() }
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

/// The node index of one blob's bytes on the SAVE numbering (docs/SPEC-TABLES.md
/// §3.1): a shared blob is numbered once, so two slots naming the same bytes
/// write one node record and both reference its index. The root is 1 and record
/// k (1-based) is index k + 1, so a new node at the end is len + 1.
pub fn node_blob_index(nodes: &mut Vec<(u64, Vec<u8>)>, type_id: u64, bytes: &[u8]) -> u64 {
    for (i, (tid, b)) in nodes.iter().enumerate() {
        if *tid == type_id && b.as_slice() == bytes {
            return i as u64 + 2;
        }
    }
    nodes.push((type_id, bytes.to_vec()));
    nodes.len() as u64 + 1
}

/// Walk past a payload the reader does not store, by kind (§4), bounded by the
/// enclosing body's end. The fixed-width kinds skip their width; the framed
/// kinds read L and skip L bytes; a reference-shaped kind (pointer, enum) reads
/// one LEB128; a union reads its arm header and L. A kind the reader does not
/// know at all is framing damage and returns false (§3).
pub fn leb_skip(buf: &[u8], off: &mut usize, end: usize, kind: u8) -> bool {
    let fixed = |o: &mut usize, w: usize| -> bool {
        if *o + w > end { return false; }
        *o += w;
        true
    };
    match kind {
        1 | 2 | 6 | 20 | 25 => fixed(off, 1),
        3 | 7 | 21 | 26 => fixed(off, 2),
        4 | 8 | 10 | 22 | 27 => fixed(off, 4),
        5 | 9 | 11 | 23 | 28 => fixed(off, 8),
        18 | 19 | 24 | 29 => fixed(off, 16),
        17 | 30 => {
            if let Some(_) = leb_read(buf, off) { *off <= end } else { false }
        }
        12 | 13 | 14 | 16 | 31 | 32 | 33 => {
            let Some(n) = leb_read(buf, off) else { return false; };
            let n = n as usize;
            if *off + n > end { return false; }
            *off += n;
            true
        }
        15 => {
            let Some(arm) = leb_read(buf, off) else { return false; };
            if arm == 0 { return true; }
            if *off + 1 > end { return false; }
            *off += 1;
            let Some(n) = leb_read(buf, off) else { return false; };
            let n = n as usize;
            if *off + n > end { return false; }
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

/// ONE RETAINED RECORD is
/// [u32 len][u32 depth][u64 field id][u8 kind][depth × (u32 ordinal, u32 index)][resolved payload],
/// len = 17 + 8×depth + payload.len(). The payload is READER-PRIVATE (§6.6):
/// every reference is the eight-byte id it names and every framed length is
/// eight bytes, so a save re-interns references against a different id table.
pub fn retain_record_len(rec: &[u8]) -> usize {
    u32::from_le_bytes(rec[..4].try_into().unwrap()) as usize
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
        5 | 9 | 11 | 23 | 28 => w(c, 8),
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
        let len = retain_record_len(&retain.bytes[at..]) as usize;
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
        let len = retain_record_len(&retain.bytes[read..]) as usize;
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

fn retain_emit_ref(c: &mut RetainCur, ids: &mut IdTable, retain: &mut TableRetain, known: &[u64], bad: &mut bool) -> u64 {
    if c.off + 8 > c.buf.len() {
        *bad = true;
        return 0;
    }
    let id = u64::from_le_bytes(c.buf[c.off..c.off + 8].try_into().unwrap());
    c.off += 8;
    retain_ref(ids, retain, known, id)
}

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
	return b.String()
}

// cardVariableTableNames returns the names of a file's variable tables that
// pass the card boundary — the tables this wire emits (docs/SPEC-TABLES.md
// §2.5, §2.9). A variable table with any other construct stays ABSENT: the
// boundary is scoped to the wire, so a unit mixing a fixed table with a
// pointered variable table (rusttable_test.go's `pointered`) still emits the
// fixed form and never mis-encodes the pointer.
func cardVariableTableNames(g *gen) []string {
	variable := ir.VariableTables(g.unit)
	names := make([]string, 0)
	for _, st := range g.file.Tables {
		if !variable[st.Name] {
			continue
		}
		if refuseCardUnsupportedFields(st) != nil {
			continue
		}
		names = append(names, st.Name)
	}
	sort.Strings(names)
	return names
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
		g.emitVariableConstants(&b, g.unit.Tables[n])
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
