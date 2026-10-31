// THE MESSAGE FORM'S ANNOUNCEMENT ON THE RUST LEG (docs/SPEC-TABLES.md §3.3).
//
// The message form is a BATCH of bitpacked bodies under one announced
// vocabulary. "The ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT, every
// byte of it settled by the compiler, so a backend emits Announce and
// AnnounceMeasure as a constant byte array and its length rather than as a
// walk" (§3.3). The bytes are the C++ reference's, pinned by
// testdata/conformance/tables/MANIFEST.txt's `connection` rows and rebuilt
// here by ir.TableAnnouncement.
//
// The surface and its names follow the C# leg (internal/codegen/cstable/
// message.go): TableVocabulary, Announce, AnnounceMeasure and AnnounceRead
// here, and MeasureMessages, SaveMessages and LoadMessages, the batch itself,
// in messagebatch.go (the bit stream's runtime rides in this module, the
// per-root verbs in each file's <base>_message.rs). Rust spells the free
// functions snake_case (SPEC.md §6.1), so they land as `announce`,
// `announce_measure`, `announce_read` and `<root>_load_messages`, exactly as
// the fixed form spells `<name>_load`.
//
// The form is "A FIXED TABLE'S, and that is a declaration, not a derivation"
// (§3.3), so the surface is emitted for a unit that carries a fixed root,
// beside the fixed form's runtime.
package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageRuntimeModule is the message form's shared runtime, emitted once per
// unit that carries a fixed root.
const MessageRuntimeModule = "message_runtime"

// messageRuntimeModule emits message_runtime.rs: the announcement as a
// compile-time constant byte array, its length, the TableVocabulary a receiver
// reads it into, and AnnounceRead.
//
// ownsReport is false when the unit also carries the variable runtime, which
// then owns TableReport, TableReason and TABLE_WIRE_FORM (see tableReportRust):
// this module reaches them through crate::*.
func messageRuntimeModule(u *ir.Unit, ownsReport bool) []byte {
	announcement := ir.TableAnnouncement(u)
	var b strings.Builder
	b.WriteString(header(runtimeSourceName(u), u.Package,
		"THE MESSAGE FORM's shared runtime (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("//\n")
	b.WriteString("// The message form is a BATCH of bitpacked bodies under ONE announced\n")
	b.WriteString("// vocabulary. The announcement is delivered once, reliably, before the first\n")
	b.WriteString("// body, and never again for the life of the connection; it is an ordinary\n")
	b.WriteString("// form-1 FILE whose body carries the BUILD VERSION and the VOCABULARY under\n")
	b.WriteString("// the two reserved ids (§3.3).\n")
	b.WriteString("//\n")
	b.WriteString("// The BUILD VERSION the strict check compares against lives in build_version.rs;\n")
	b.WriteString("// the crate root re-exports it, so this module reaches it through crate::*.\n")
	b.WriteString("\nuse crate::*;\n\n")

	if ownsReport {
		b.WriteString("/// The variable form's form byte (§3): the announcement IS an ordinary form-1\n")
		b.WriteString("/// FILE (§3.3), so it opens with this byte and not the message form's own.\n")
		b.WriteString("pub const TABLE_WIRE_FORM: u8 = 1;\n\n")
	}
	b.WriteString("/// The message form's form byte (§3.3). A form-2 wire is a batch, never a\n")
	b.WriteString("/// file's own form; handed to AnnounceRead it is refused by name.\n")
	b.WriteString("pub const TABLE_WIRE_MESSAGE_FORM: u8 = 2;\n\n")
	b.WriteString("/// The reserved ids of the announcement's own two fields (§3.3, §5, §11). They\n")
	b.WriteString("/// are the announcement's transport and never take a vocabulary slot.\n")
	b.WriteString("pub const TABLE_BUILD_VERSION_FIELD_ID: u64 = 0xffff_ffff_ffff_fffe;\n")
	b.WriteString("pub const TABLE_MESSAGE_VOCABULARY_FIELD_ID: u64 = 0xffff_ffff_ffff_fffd;\n")
	b.WriteString("/// The reserved node-table id, which takes exactly one slot (§3.3).\n")
	b.WriteString("pub const TABLE_NODE_TABLE_ID: u64 = 0xffff_ffff_ffff_ffff;\n\n")
	b.WriteString("/// THE CONFORMING DEFAULT BYTE BOUND (§3.3): 64 KiB, checked from the\n")
	b.WriteString("/// vocabulary field's own length before an entry is touched.\n")
	b.WriteString("pub const TABLE_MESSAGE_MAX_BYTES: usize = 64 * 1024;\n\n")

	b.WriteString("/// THE ANNOUNCEMENT, byte for byte: the unit's whole vocabulary as a\n")
	b.WriteString("/// compile-time constant (§3.3). It is an ordinary form-1 FILE whose body\n")
	b.WriteString("/// carries the build version at kind 9 and the vocabulary at kind 14 over\n")
	b.WriteString("/// element kind 6, and whose trailer is those two reserved ids.\n")
	fmt.Fprintf(&b, "pub const TABLE_ANNOUNCEMENT: [u8; %d] = [", len(announcement))
	for i, v := range announcement {
		if i%12 == 0 {
			b.WriteString("\n    ")
		}
		fmt.Fprintf(&b, "0x%02x,", v)
	}
	b.WriteString("\n];\n\n")

	// THE WRITER's reference width is its OWN vocabulary's (§3.3): every slot
	// of the announcement it sent, and the zero reference beside them.
	b.WriteString("/// The width of a reference on a body this build WRITES: bits_required(0, n)\n")
	b.WriteString("/// over its own announced vocabulary of n entries (§3.3).\n")
	fmt.Fprintf(&b, "pub const TABLE_MESSAGE_REF_BITS: u32 = %d;\n\n", ir.TableMessageRefBits(len(ir.TableVocabulary(u))))

	b.WriteString(messageRuntimeBody)
	if ownsReport {
		b.WriteString(tableReportRust)
	}
	b.WriteString(messageRuntimeRead)
	b.WriteString(messageBatchRuntime)
	return []byte(b.String())
}

// messageRuntimeBody is the shared Rust runtime for the announcement surface:
// the entry triple, the resolved vocabulary, the report and the read.
const messageRuntimeBody = `// ---- the entry triple -------------------------------------------------------

/// ONE RESOLVED SHAPE: the width and range facts a bitpacked body needs to SKIP
/// a field it cannot name and to DECODE one whose declaration has moved (§3.3).
/// Which members carry meaning is decided by the entry's kind; a member a kind
/// does not name stays zero.
#[derive(Clone, Copy, Default, PartialEq, Debug)]
pub struct TableMessageShape {
    pub packing: u8,
    pub bits: u64,
    pub base_lo: u64,
    pub base_hi: u64,
    pub min: u64,
    pub max: u64,
    /// an array's or a keyed entry's element kind; zero for a scalar
    pub elem: u8,
    pub qmin: f32,
    pub qdelta: f32,
    pub qcount: u32,
}

/// ONE ANNOUNCED ENTRY: an id, a kind, and a shape (§3.3). The ELEMENT is the
/// one nesting this wire has — an array's or a keyed entry's element shape —
/// and is zero for a scalar.
#[derive(Clone, Copy, Default, PartialEq, Debug)]
pub struct TableMessageEntry {
    pub id: u64,
    pub kind: u8,
    pub shape: TableMessageShape,
    pub element: TableMessageShape,
}

// ---- the resolved vocabulary ------------------------------------------------

/// ONE DIRECTION's announced vocabulary (§3.3): the entries an announcement
/// carried, resolved once. THE STORAGE IS THE CALLER'S — an array of entries
/// declared by capacity — and the codec never allocates.
pub struct TableVocabulary<'a> {
    pub entries: &'a mut [TableMessageEntry],
    pub count: usize,
    pub ref_bits: u32,
    pub build_version: u64,
    pub announced: bool,
    /// REFUSAL IS TERMINAL (§3.3): a connection whose first announcement was
    /// refused carries no vocabulary for its life.
    pub refused: bool,
    pub max_bytes: usize,
    /// STRICT MODE IS OPT-IN (§3.3).
    pub strict: bool,
}

impl<'a> TableVocabulary<'a> {
    pub fn new(entries: &'a mut [TableMessageEntry]) -> Self {
        TableVocabulary {
            entries,
            count: 0,
            ref_bits: 1,
            build_version: 0,
            announced: false,
            refused: false,
            max_bytes: TABLE_MESSAGE_MAX_BYTES,
            strict: false,
        }
    }

    /// THE ENTRY a reference names, counted from 1 (§3.3): one array index, no
    /// parse and no branch. Reference 0 names no entry.
    pub fn entry(&self, reference: u64) -> Option<&TableMessageEntry> {
        if reference == 0 || reference as usize > self.count {
            return None;
        }
        Some(&self.entries[reference as usize - 1])
    }

    /// THE ID a NAME reference spells (§3.3): an enum's variant or a keyed
    /// slot rides as a reference to a kind-0 entry, never a reserved id.
    pub fn name(&self, reference: u64) -> Option<u64> {
        let e = self.entry(reference)?;
        if e.kind == 0 && e.id < 0xffff_ffff_ffff_fffd {
            Some(e.id)
        } else {
            None
        }
    }
}

`

// messageRuntimeRead is the runtime after the report: the announcement's
// measure, copy and read (§3.3).
const messageRuntimeRead = `// ---- the read ---------------------------------------------------------------

/// The announcement's byte count and the copy that hands it out (§3.3): both
/// are the constant, never a walk. Nothing here allocates.
pub fn announce_measure() -> usize {
    TABLE_ANNOUNCEMENT.len()
}

/// Copy the announcement into the caller's buffer and answer its byte count, or
/// -1 when the buffer is short (§3.3).
pub fn announce(buffer: &mut [u8]) -> i64 {
    if buffer.len() < TABLE_ANNOUNCEMENT.len() {
        return -1;
    }
    buffer[..TABLE_ANNOUNCEMENT.len()].copy_from_slice(&TABLE_ANNOUNCEMENT);
    TABLE_ANNOUNCEMENT.len() as i64
}

/// AnnounceRead reads one announcement into a direction's vocabulary (§3.3).
///
/// "NO RE-ANNOUNCEMENT, EVER" (§3.3): the first announcement sets the
/// vocabulary and it is the only one that can. A second is refused by name and
/// does not replace or amend anything. REFUSAL IS TERMINAL: a connection whose
/// first announcement was refused carries no vocabulary for its life, and every
/// announcement after it is refused as second_announcement.
pub fn announce_read(
    vocabulary: &mut TableVocabulary,
    data: &[u8],
    report: &mut TableReport,
) -> bool {
    if vocabulary.announced || vocabulary.refused {
        return report.refuse(TableReason::SecondAnnouncement);
    }
    let ok = announce_read_once(vocabulary, data, report);
    if !ok {
        vocabulary.refused = true;
    }
    ok
}

fn announce_read_once(
    vocabulary: &mut TableVocabulary,
    data: &[u8],
    report: &mut TableReport,
) -> bool {
    if data.is_empty() {
        report.malformed = true;
        return false;
    }
    // "the FORM BYTE is a whole byte and is read FIRST" (§3.3): a form this
    // reader does not carry is a refusal by name and never damage.
    if data[0] != TABLE_WIRE_FORM {
        return report.refuse(if data[0] == TABLE_WIRE_MESSAGE_FORM {
            TableReason::MessageFormAsFile
        } else {
            TableReason::NewerForm
        });
    }
    if data.len() < 9 {
        report.malformed = true;
        return false;
    }
    // the trailer: a run of u64 ids and a u64 count at the very end (§3.3)
    let trailer_count = read_u64(data, data.len() - 8);
    if trailer_count > ((data.len() - 9) / 8) as u64 {
        report.malformed = true;
        return false;
    }
    let end = data.len() - 8 - (trailer_count as usize) * 8;
    // "a table carrying one id twice" is the whole wire malformed (§3.3).
    for i in 0..trailer_count {
        for j in 0..i {
            if read_u64(data, end + (i as usize) * 8) == read_u64(data, end + (j as usize) * 8) {
                report.malformed = true;
                return false;
            }
        }
    }
    let body = &data[1..end];
    // THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS (§3.3):
    // the build version present, exactly once, kind 9, eight bytes wide, and
    // the vocabulary present, exactly once, kind 14 over element kind 6. A
    // FAILED STRICT CHECK IS MALFORMED, not a refusal.
    let mut at = 0usize;
    let mut versions = 0u32;
    let mut vocabularies = 0u32;
    let mut terminated = false;
    let mut vocabulary_bytes: &[u8] = &[];
    while at < body.len() {
        let (reference, ok) = read_leb(body, &mut at);
        if !ok {
            report.malformed = true;
            return false;
        }
        if reference == 0 {
            terminated = true;
            // "Any byte between the root's terminator and the table's first
            // entry is malformed, because no field claims it and the two ends
            // of the file have met" (§3). The terminator ends the body, so the
            // cursor must be at the body's end.
            if at != body.len() {
                report.malformed = true;
                return false;
            }
            break;
        }
        if reference > trailer_count || at >= body.len() {
            report.malformed = true;
            return false;
        }
        let id = read_u64(data, end + ((reference - 1) as usize) * 8);
        let kind = body[at];
        at += 1;
        if id == TABLE_BUILD_VERSION_FIELD_ID {
            if kind != 9 || at + 8 > body.len() {
                report.malformed = true;
                return false;
            }
            // "THE BUILD VERSION IS KEPT THE MOMENT IT IS READ, refusal or not"
            // (§3.3): it is not the vocabulary, and a refused announcement
            // still sets none.
            vocabulary.build_version = read_u64(body, at);
            at += 8;
            versions += 1;
        } else if id == TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            if kind != 14 {
                report.malformed = true;
                return false;
            }
            let (framed, ok) = read_leb(body, &mut at);
            if !ok || at + (framed as usize) > body.len() {
                report.malformed = true;
                return false;
            }
            let begin = at;
            let end_at = at + framed as usize;
            at = end_at;
            if begin >= end_at || body[begin] != 6 {
                report.malformed = true;
                return false;
            }
            let mut vat = begin + 1;
            let (length, ok) = read_leb(body, &mut vat);
            if !ok || vat + (length as usize) != end_at {
                report.malformed = true;
                return false;
            }
            // "A VOCABULARY PAST A BOUND IS REFUSED BEFORE ANYTHING IS
            // ALLOCATED" (§3.3): the byte bound is checked from the field's own
            // length before an entry is touched.
            let limit = if vocabulary.max_bytes == 0 {
                TABLE_MESSAGE_MAX_BYTES
            } else {
                vocabulary.max_bytes
            };
            if length > limit as u64 {
                return report.refuse(TableReason::VocabularyTooLarge);
            }
            vocabulary_bytes = &body[vat..vat + length as usize];
            vocabularies += 1;
        } else {
            // "Every other field of its body is ordinary and tolerant, so an
            // unknown one is skipped and counted" (§3.3).
            report.unknown += 1;
            if !skip_field(body, &mut at, kind) {
                report.malformed = true;
                return false;
            }
        }
    }
    if !terminated || versions != 1 || vocabularies != 1 {
        report.malformed = true;
        return false;
    }
    // "STRICT MODE IS OPT-IN, and the default is unchanged" (§3.3).
    if vocabulary.strict && vocabulary.build_version != BUILD_VERSION {
        report.build_version_remote = vocabulary.build_version;
        report.build_version_own = BUILD_VERSION;
        return report.refuse(TableReason::BuildVersionMismatch);
    }
    // THE ENTRIES, resolved once into the caller's storage (§3.3): every width
    // is checked here and never again. An entry count above the caller's
    // capacity is refused by name before the entry is touched.
    let mut at = 0usize;
    let mut count = 0usize;
    let mut node_seen = false;
    while at < vocabulary_bytes.len() {
        if count >= vocabulary.entries.len() {
            return report.refuse(TableReason::VocabularyTooLarge);
        }
        let began = at;
        let (entry, ok) = read_entry(vocabulary_bytes, &mut at);
        if !ok {
            report.malformed = true;
            return false;
        }
        // THE TWO RESERVED IDS OF THE ANNOUNCEMENT ITSELF ARE NOT IN THE
        // VOCABULARY (§3.3), and the node-table id takes exactly one slot.
        if entry.id == TABLE_BUILD_VERSION_FIELD_ID || entry.id == TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            report.malformed = true;
            return false;
        }
        if entry.id == TABLE_NODE_TABLE_ID {
            if node_seen {
                report.malformed = true;
                return false;
            }
            node_seen = true;
        }
        // "A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE" (§3.3): byte equality
        // is declared-fact equality, and a duplicate is malformed.
        {
            let (filled, rest) = vocabulary.entries.split_at_mut(count);
            for other in filled.iter() {
                let other_len = (other.shape.max - other.shape.min) as usize;
                if other_len == at - began
                    && vocabulary_bytes[other.shape.min as usize..other.shape.max as usize]
                        == vocabulary_bytes[began..at]
                {
                    report.malformed = true;
                    return false;
                }
            }
            let slot = &mut rest[0];
            *slot = entry;
            // the byte extent parks in min/max until the second pass resolves it
            slot.shape.min = began as u64;
            slot.shape.max = at as u64;
        }
        count += 1;
    }
    // the second pass: resolve every entry in place over bytes already accepted
    let mut at = 0usize;
    for slot in 0..count {
        let (entry, ok) = read_entry(vocabulary_bytes, &mut at);
        if !ok {
            report.malformed = true;
            return false;
        }
        vocabulary.entries[slot] = entry;
    }
    vocabulary.count = count;
    vocabulary.ref_bits = bits_required(count);
    vocabulary.announced = true;
    true
}

// ---- the wire reader --------------------------------------------------------

fn read_u64(bytes: &[u8], at: usize) -> u64 {
    let mut v = 0u64;
    for i in 0..8 {
        v |= (bytes[at + i] as u64) << (8 * i);
    }
    v
}

fn read_u32(bytes: &[u8], at: usize) -> u32 {
    let mut v = 0u32;
    for i in 0..4 {
        v |= (bytes[at + i] as u32) << (8 * i);
    }
    v
}

/// A canonical LEB128, the announcement's own integer: the announcement is a
/// form-1 FILE and takes §3's rule (§3.3). "An encoding past ten bytes, or a
/// tenth byte with a bit above the 64th value bit, is malformed on the same
/// rule" (docs/SPEC-TABLES.md:3707), so the tenth byte may carry only the
/// 64th value bit: shift 63 with a byte past 0x01 is a bit past it and is
/// refused.
fn read_leb(bytes: &[u8], at: &mut usize) -> (u64, bool) {
    let mut value = 0u64;
    let mut shift = 0u32;
    while *at < bytes.len() {
        let b = bytes[*at];
        *at += 1;
        if shift >= 64 {
            return (0, false);
        }
        if shift == 63 && b > 1 {
            return (0, false);
        }
        value |= ((b & 0x7f) as u64) << shift;
        if b & 0x80 == 0 {
            return if shift > 0 && b == 0 {
                (0, false)
            } else {
                (value, true)
            };
        }
        shift += 7;
    }
    (0, false)
}

/// bits_required(0, count), the width of a reference into a count-entry
/// vocabulary (§3.3): one bit even for a vocabulary of no entries.
fn bits_required(count: usize) -> u32 {
    if count == 0 {
        return 1;
    }
    64 - (count as u64).leading_zeros()
}

fn known_kind(kind: u8) -> bool {
    kind == 0 || (1..=17).contains(&kind) || (18..=29).contains(&kind) || (30..=33).contains(&kind)
}

fn integer_kind(kind: u8) -> bool {
    (2..=9).contains(&kind) || kind == 18 || kind == 19
}

fn fixed_kind(kind: u8) -> bool {
    (20..=29).contains(&kind)
}

/// The widest ranged value a kind can carry, its own storage width (§3.3): a
/// width above it is a hostile width on the announcement.
fn kind_bits(kind: u8) -> u64 {
    match kind {
        2 | 6 | 20 | 25 => 8,
        3 | 7 | 21 | 26 => 16,
        4 | 8 | 22 | 27 => 32,
        5 | 9 | 23 | 28 => 64,
        _ => 128,
    }
}

/// SPEC.md §4.3's compressed-float derivation, in float32 and by nothing else
/// (§3.3): the step count, the delta and the width. False is a non-conforming
/// triple, which on the announcement is a hostile width.
fn quantization(qmin: f32, qmax: f32, qres: f32) -> (u32, f32, u64, bool) {
    if qmin.is_nan() || qmax.is_nan() || qmin >= qmax || qres.is_nan() || qres <= 0.0 {
        return (0, 0.0, 0, false);
    }
    let delta = qmax - qmin;
    let mut values = delta / qres;
    if delta.is_nan() || values.is_nan() {
        return (0, 0.0, 0, false);
    }
    if values.is_nan() || values < 1.0 {
        values = 1.0;
    } else if values > 4294967040.0 {
        values = 4294967040.0;
    }
    let count = values as u32;
    let count = if (count as f32) < values { count + 1 } else { count };
    let bits = if count == 0 {
        0u64
    } else {
        64u64 - (count as u64).leading_zeros() as u64
    };
    (count, delta, bits, true)
}

/// ONE entry: an id, a kind and the shape the kind names (§3.3). False is a
/// HOSTILE SHAPE — bits above the kind's own domain, an array whose min
/// exceeds its max, an element kind outside the closed set, a non-conforming
/// quantized triple, or a shape running past the vocabulary's own bytes.
fn read_entry(bytes: &[u8], at: &mut usize) -> (TableMessageEntry, bool) {
    let mut e = TableMessageEntry::default();
    if *at + 9 > bytes.len() {
        return (e, false);
    }
    e.id = read_u64(bytes, *at);
    e.kind = bytes[*at + 8];
    *at += 9;
    if !known_kind(e.kind) {
        return (e, false);
    }
    let (shape, ok) = read_shape(bytes, at, e.kind);
    if !ok {
        return (e, false);
    }
    e.shape = shape;
    // "The ELEMENT's own facts ride beside the field's" (§3.3): an array's
    // element is the one nesting this wire has.
    if e.kind == 14 || e.kind == 16 {
        let (element, ok) = read_shape(bytes, at, e.shape.elem);
        if !ok {
            return (e, false);
        }
        e.element = element;
    }
    (e, true)
}

/// ONE shape, by the kind that names it (§3.3's shape table). Every number in
/// it is a canonical LEB128 except where the row says otherwise: a RANGED BASE
/// is encoded by its kind's signedness — zigzag for the signed kinds, unsigned
/// for the unsigned kinds, sixteen bytes for the 128-bit and fixed-point kinds —
/// and a QUANTIZED f32 carries min, max and res as float32.
fn read_shape(bytes: &[u8], at: &mut usize, kind: u8) -> (TableMessageShape, bool) {
    let mut s = TableMessageShape::default();
    if integer_kind(kind) || fixed_kind(kind) || kind == 10 {
        if *at >= bytes.len() {
            return (s, false);
        }
        s.packing = bytes[*at];
        *at += 1;
        if s.packing == 0 {
            return (s, true);
        }
        if s.packing == 1 && kind != 10 {
            let (v, ok) = read_leb(bytes, at);
            if !ok || v > kind_bits(kind) {
                return (s, false);
            }
            s.bits = v;
            if kind == 18 || kind == 19 || fixed_kind(kind) {
                if *at + 16 > bytes.len() {
                    return (s, false);
                }
                s.base_lo = read_u64(bytes, *at);
                s.base_hi = read_u64(bytes, *at + 8);
                *at += 16;
                return (s, true);
            }
            let (v, ok) = read_leb(bytes, at);
            if !ok {
                return (s, false);
            }
            if (2..=5).contains(&kind) {
                s.base_lo = (v >> 1) ^ (0u64.wrapping_sub(v & 1));
            } else {
                s.base_lo = v;
            }
            return (s, true);
        }
        if s.packing == 2 && kind == 10 {
            if *at + 12 > bytes.len() {
                return (s, false);
            }
            let qmin = f32::from_bits(read_u32(bytes, *at));
            let qmax = f32::from_bits(read_u32(bytes, *at + 4));
            let qres = f32::from_bits(read_u32(bytes, *at + 8));
            *at += 12;
            let (qcount, qdelta, bits, ok) = quantization(qmin, qmax, qres);
            if !ok {
                return (s, false);
            }
            s.qmin = qmin;
            s.qdelta = qdelta;
            s.qcount = qcount;
            s.bits = bits;
            return (s, true);
        }
        return (s, false); // a packing outside the closed set
    }
    if kind == 12 || kind == 33 {
        let (v, ok) = read_leb(bytes, at);
        if !ok || v > i32::MAX as u64 {
            return (s, false);
        }
        s.max = v;
        return (s, true);
    }
    if kind == 14 || kind == 16 {
        if kind == 14 {
            let (v, ok) = read_leb(bytes, at);
            if !ok || v > 0xffff_ffff {
                return (s, false);
            }
            s.min = v;
        }
        let (v, ok) = read_leb(bytes, at);
        if !ok || v > 0xffff_ffff || v < s.min {
            return (s, false);
        }
        s.max = v;
        if *at >= bytes.len() {
            return (s, false);
        }
        s.elem = bytes[*at];
        *at += 1;
        if !known_kind(s.elem) {
            return (s, false);
        }
        // an element kind of 12 or 33 is refused here, at the announcement,
        // rather than at the skip that would meet it (§3.3)
        if s.elem == 12 || s.elem == 33 {
            return (s, false);
        }
        return (s, true);
    }
    (s, true)
}

/// A tolerant skip of an UNKNOWN field by its kind (§3.3): the ordinary
/// form-1 framed-field skip rules, the C++ reference's (cpptable) and the Go
/// leg's, one function over the closed kind set. This reader knows only the
/// announcement's own two fields, so a field of any other id is stepped over
/// by its kind: fixed-width kinds by their storage width, a node index and an
/// enum's variant by one LEB128, the framed kinds by one LEB128 length and
/// that many bytes, and a union by its arm reference, its arm's kind byte, one
/// length and that many bytes. A kind outside the set is not skippable, which
/// is why the set is closed and why kind 31 exists (§3).
fn skip_field(bytes: &[u8], at: &mut usize, kind: u8) -> bool {
    match kind {
        // the fixed-width kinds, each by its storage width (§3)
        1 | 2 | 6 | 20 | 25 => skip_width(bytes, at, 1),
        3 | 7 | 21 | 26 => skip_width(bytes, at, 2),
        4 | 8 | 10 | 22 | 27 => skip_width(bytes, at, 4),
        5 | 9 | 11 | 23 | 28 => skip_width(bytes, at, 8),
        18 | 19 | 24 | 29 => skip_width(bytes, at, 16),
        // a NODE INDEX (§3.1) and an ENUM's variant reference: one LEB128 and
        // stop
        17 | 30 => {
            let (_, ok) = read_leb(bytes, at);
            ok
        }
        // THE FRAMED KINDS: one LEB128 length and that many bytes. A string,
        // a nested body, an array, a keyed array, an escape, a payload-free
        // field and a wide string all frame the same way on the file, and a
        // wide string's length is its BYTE count (§3).
        12 | 13 | 14 | 16 | 31 | 32 | 33 => {
            let (n, ok) = read_leb(bytes, at);
            if !ok || *at + (n as usize) > bytes.len() {
                return false;
            }
            *at += n as usize;
            true
        }
        // a UNION: its arm reference, then the arm's kind byte, one length and
        // that many bytes. Reference 0 is the empty union and stops there.
        15 => {
            let (arm, ok) = read_leb(bytes, at);
            if !ok {
                return false;
            }
            if arm == 0 {
                return true;
            }
            if *at >= bytes.len() {
                return false;
            }
            *at += 1;
            let (n, ok) = read_leb(bytes, at);
            if !ok || *at + (n as usize) > bytes.len() {
                return false;
            }
            *at += n as usize;
            true
        }
        // a kind outside the closed set is not skippable (§3)
        _ => false,
    }
}

/// Skip a fixed-width payload of WIDTH bytes, or refuse past the buffer.
fn skip_width(bytes: &[u8], at: &mut usize, width: usize) -> bool {
    if *at + width > bytes.len() {
        return false;
    }
    *at += width;
    true
}
`

// tableReportRust is THE ONE READ REPORT of the Rust table leg
// (docs/SPEC-TABLES.md §4, §5, §3.3): TableReason and TableReport, emitted in
// variable_runtime.rs whenever the unit carries the variable form, and in
// message_runtime.rs only when it does not, so a crate root that glob-exports
// both runtimes names one TableReport and never two (an ambiguous glob a
// consumer cannot name).
const tableReportRust = `// ---- the report -------------------------------------------------------------

/// Refusals of the variable and message forms, each by name (§3.3, §5).
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum TableReason {
    #[default]
    None,
    NewerForm,
    MessageFormAsFile,
    NoVocabulary,
    SecondAnnouncement,
    BuildVersionMismatch,
    VocabularyTooLarge,
    BatchTooLarge,
    Ok,
    NotACook,
    ForeignOrder,
    WrongBuildVersion,
    ReservedNotZero,
    BadAlignment,
    Truncated,
    UnalignedBase,
    BadLayout,
    UnknownForm,
    CountOverLength,
    CountOverExtentCap,
    BlobOverSizeCap,
    BlobShared,
    DataCycle,
    InvalidValue,
    AllocationFailed,
    WireDamaged,
    /// A FLOAT WHOSE BITS ARE A NaN OR AN INFINITY (docs/SPEC-TABLES.md §3.3,
    /// the same rule as the packet wire's, SPEC.md §4.3): every read refuses one, the tolerant read included, and every writer
    /// refuses to emit one. The report names the field in non_finite_float.
    NonFiniteFloat,
}

/// The variable reader uses the Go reference's refusal vocabulary (§5).
pub type TableRefuseReason = TableReason;

impl TableReason {
    pub fn name(&self) -> &'static str {
        match self {
            TableReason::None => "none",
            TableReason::NewerForm => "newer_form",
            TableReason::MessageFormAsFile => "message_form_as_file",
            TableReason::NoVocabulary => "no_vocabulary",
            TableReason::SecondAnnouncement => "second_announcement",
            TableReason::BuildVersionMismatch => "build_version_mismatch",
            TableReason::VocabularyTooLarge => "vocabulary_too_large",
            TableReason::BatchTooLarge => "batch_too_large",
            TableReason::Ok => "ok",
            TableReason::NotACook => "not_a_cook",
            TableReason::ForeignOrder => "foreign_order",
            TableReason::WrongBuildVersion => "wrong_build_version",
            TableReason::ReservedNotZero => "reserved_not_zero",
            TableReason::BadAlignment => "bad_alignment",
            TableReason::Truncated => "truncated",
            TableReason::UnalignedBase => "unaligned_base",
            TableReason::BadLayout => "bad_layout",
            TableReason::UnknownForm => "unknown_form",
            TableReason::CountOverLength => "count_over_length",
            TableReason::CountOverExtentCap => "count_over_extent_cap",
            TableReason::BlobOverSizeCap => "blob_over_size_cap",
            TableReason::BlobShared => "blob_shared",
            TableReason::DataCycle => "data_cycle",
            TableReason::InvalidValue => "invalid_value",
            TableReason::AllocationFailed => "allocation_failed",
            TableReason::WireDamaged => "wire_damaged",
            TableReason::NonFiniteFloat => "non_finite_float",
        }
    }
}

/// THE ONE READ REPORT (docs/SPEC-TABLES.md §4, §5, §3.3): the variable form's
/// tolerant read and the message form's batch fill the same type, so a crate
/// carrying both forms exports one TableReport and never two.
///
/// A field this build cannot name is skipped and counted, a field it can name
/// but that arrives at another kind is a kind mismatch, and an absent field
/// reads its default. widened counts a kind that GREW since the writer (§4),
/// clamped a value clamped into a narrower declaration, duplicate a repeated
/// id (zero on the card's wire: no maps), and refused is a verdict, not one of
/// §4's events: the form byte this reader does not carry (§3), or one of the
/// message form's refusals BY NAME, whose reason the message form records
/// (nothing is decoded, no counter moves, and malformed does not fire, §3.3).
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableReport {
    pub unknown: u64,
    pub kind_mismatch: u64,
    pub widened: u64,
    pub clamped: u64,
    pub duplicate: u64,
    pub malformed: bool,
    pub refused: bool,
    /// Pointer cycles remain readable but cannot be serialized by the builder (§3.1).
    pub cycle_on_save: bool,
    /// A BLOB NAMED FROM TWO SLOTS (docs/SPEC-TABLES.md §2.5): the tolerant
    /// read keeps the one node and counts each extra naming here; every writer
    /// refuses that graph by name, blob_shared.
    pub blob_shared: u64,
    /// RETAIN-UNKNOWN'S TWO COUNTERS (docs/SPEC-TABLES.md §6.6): retained
    /// counts the unknown fields whose bytes were kept, retain_lost counts
    /// every unknown this load or save could not keep (capacity, an excluded
    /// class, or a damaged resolving walk). Both are zero in every read that
    /// did not opt in, and both ride the one report type for the reason
    /// duplicate does (§4).
    pub retained: u64,
    pub retain_lost: u64,
    /// The message form's refusal by name and the two build versions its
    /// strict check compared (§3.3); None and zero on the variable form.
    pub reason: TableReason,
    pub build_version_remote: u64,
    pub build_version_own: u64,
    /// THE FIRST NON-FINITE FLOAT A READ MET (docs/SPEC-TABLES.md §3.3): its
    /// <Table>.<field>, and "" when there was none. A read that names one
    /// refuses as non_finite_float and lands nothing, the tolerant read
    /// included. For a RETAINED UNKNOWN float (§6.6) it is the body's table
    /// and non_finite_float_id is the id the reader cannot name.
    pub non_finite_float: &'static str,
    pub non_finite_float_id: Option<u64>,
}

impl TableReport {
    pub fn refuse(&mut self, reason: TableReason) -> bool {
        self.malformed = false;
        self.refused = true;
        self.reason = reason;
        false
    }

    /// Names a float whose raw bits are a NaN or an infinity (§3.3). The first
    /// name a read records wins.
    pub fn non_finite(&mut self, field: &'static str) {
        if self.non_finite_float.is_empty() {
            self.non_finite_float = field;
        }
    }

    /// Names a RETAINED UNKNOWN float of a float kind whose bits are a NaN or
    /// an infinity (§3.3, §6.6): tested like a known field, refused alike.
    pub fn non_finite_unknown(&mut self, owner: &'static str, id: u64) {
        if self.non_finite_float.is_empty() {
            self.non_finite_float = owner;
            self.non_finite_float_id = Some(id);
        }
    }

    /// The refusal's reason as the Go reference spells it,
    /// "non_finite_float: <Table>.<field>" ("<Table>.0x<id>" for a retained
    /// unknown), or None when no non-finite float was named.
    pub fn non_finite_float_reason(&self) -> Option<String> {
        if self.non_finite_float.is_empty() {
            return None;
        }
        Some(match self.non_finite_float_id {
            Some(id) => format!("non_finite_float: {}.0x{:016x}", self.non_finite_float, id),
            None => format!("non_finite_float: {}", self.non_finite_float),
        })
    }
}

/// THE RAW-BITS TEST EVERY READ RUNS (docs/SPEC-TABLES.md §3.3): an exponent
/// of all ones is a NaN (any sign, any payload) or an infinity, and nothing
/// else is. -0 and the subnormals are values.
pub const fn table_finite_f32(bits: u32) -> bool {
    bits & 0x7F80_0000 != 0x7F80_0000
}

/// table_finite_f32 at sixty-four bits.
pub const fn table_finite_f64(bits: u64) -> bool {
    bits & 0x7FF0_0000_0000_0000 != 0x7FF0_0000_0000_0000
}

`
