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
// The modules are emitted STAND-ALONE, the same status
// [refuseCardUnsupportedVariableConstructs] holds: neither is wired into
// GenerateLineage yet, because the refusal guards this very emitter and the
// fixed-form corpus (rusttable_test.go's `pointered`) declares variable tables
// with pointers this wire does not carry. Wiring the refusal into the top of
// GenerateLineage refuses those fixed-form fixtures, so the boundary stays
// scoped to the variable wire path alone until the emitter is complete enough
// to take that gate over.
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
`)
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
