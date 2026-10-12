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
// (this card), then MeasureMessages, SaveMessages and LoadMessages (the next
// card). Rust spells the free functions snake_case (SPEC.md §6.1), so they
// land as `announce`, `announce_measure` and `announce_read`, exactly as the
// fixed form spells `<name>_load`.
//
// The form is "A FIXED TABLE'S, and that is a declaration, not a derivation"
// (§3.3), so the surface is emitted for a unit that carries a fixed root,
// beside the fixed form's runtime.
package rusttable

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageRuntimeModule is the message form's shared runtime, emitted once per
// unit that carries a fixed root.
const MessageRuntimeModule = "message_runtime"

// messageRuntimeModule emits message_runtime.rs: the announcement as a
// compile-time constant byte array, its length, the TableVocabulary a receiver
// reads it into, and AnnounceRead.
func messageRuntimeModule(u *ir.Unit) []byte {
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

	b.WriteString("/// The variable form's form byte (§3): the announcement IS an ordinary form-1\n")
	b.WriteString("/// FILE (§3.3), so it opens with this byte and not the message form's own.\n")
	b.WriteString("pub const TABLE_WIRE_FORM: u8 = 1;\n\n")
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

	b.WriteString(messageRuntimeBody)
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
}

// ---- the report -------------------------------------------------------------

/// THE MESSAGE FORM's refusals, each by name (§3.3).
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
}

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
        }
    }
}

/// The message form's read report. A refusal BY NAME: nothing is decoded, no
/// counter moves, and malformed does not fire (§3.3).
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableReport {
    pub unknown: u32,
    pub malformed: bool,
    pub refused: bool,
    pub reason: TableReason,
    pub build_version_peer: u64,
    pub build_version_own: u64,
}

impl TableReport {
    pub fn refuse(&mut self, reason: TableReason) -> bool {
        self.malformed = false;
        self.refused = true;
        self.reason = reason;
        false
    }
}

// ---- the read ---------------------------------------------------------------

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
        report.build_version_peer = vocabulary.build_version;
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
                    && &vocabulary_bytes[other.shape.min as usize..other.shape.max as usize]
                        == &vocabulary_bytes[began..at]
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
    kind == 0 || (kind >= 1 && kind <= 17) || (kind >= 18 && kind <= 29) || (kind >= 30 && kind <= 33)
}

fn integer_kind(kind: u8) -> bool {
    (kind >= 2 && kind <= 9) || kind == 18 || kind == 19
}

fn fixed_kind(kind: u8) -> bool {
    kind >= 20 && kind <= 29
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
    if !(qmin < qmax) || !(qres > 0.0) {
        return (0, 0.0, 0, false);
    }
    let delta = qmax - qmin;
    let mut values = delta / qres;
    if (delta - delta) != 0.0 || (values - values) != 0.0 {
        return (0, 0.0, 0, false);
    }
    if !(values >= 1.0) {
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
            if kind >= 2 && kind <= 5 {
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

// messageBatchRuntime is the message form's BATCH half (form byte 2,
// docs/SPEC-TABLES.md §3.3): the bit stream the bodies ride, the batch's own
// open and close, the scalar read and write, and the generic skip a reader
// uses for a field it cannot name. It is emitted once per unit, byte-identical
// in every unit, into the same module as the announcement runtime.
const messageBatchRuntime = `// ---- the batch (form byte 2) ------------------------------------------------

/// THE COUNT IS A RANGED INTEGER OVER [1, 256] (§3.3): eight bits carrying
/// M - 1, and 256 is a WIRE CONSTANT of this form, not a receiver's policy,
/// because the count's width depends on it and two peers that disagreed on
/// the width would not be reading the same wire. A batch of zero is not
/// spellable.
pub const TABLE_MESSAGE_BATCH_MAX: usize = 256;

// ---- the bit stream ---------------------------------------------------------

/// The bit writer: the packet wire's own order, bit i in byte i / 8, low bit
/// first (§3.3). put is ONE WORD AT A TIME, never one bit.
pub struct TableBitWriter<'a> {
    buffer: &'a mut [u8],
    pub bits: i64,
    measuring: bool,
    pub overflow: bool,
}

impl<'a> TableBitWriter<'a> {
    pub fn measuring() -> Self {
        TableBitWriter { buffer: &mut [], bits: 0, measuring: true, overflow: false }
    }
    pub fn writing(buffer: &'a mut [u8]) -> Self {
        TableBitWriter { buffer, bits: 0, measuring: false, overflow: false }
    }
    pub fn put(&mut self, value: u64, n: i64) {
        if n < 0 || n > 64 {
            self.overflow = true;
            return;
        }
        if n == 0 {
            return;
        }
        let at = self.bits;
        self.bits += n;
        if self.bits < at {
            self.overflow = true;
            return;
        }
        if self.measuring {
            return;
        }
        if (self.bits + 7) / 8 > self.buffer.len() as i64 {
            self.overflow = true;
            return;
        }
        let value = if n < 64 { value & ((1u64 << n) - 1) } else { value };
        let index = (at >> 3) as usize;
        let shift = (at & 7) as u32;
        let head: u64 = if shift != 0 { u64::from(self.buffer[index]) & ((1u64 << shift) - 1) } else { 0 };
        let word = head | (value << shift);
        let need = (shift as i64 + n + 7) / 8;
        if need >= 8 {
            self.buffer[index..index + 8].copy_from_slice(&word.to_le_bytes());
            if need > 8 {
                self.buffer[index + 8] = (value >> (64 - shift)) as u8;
            }
        } else {
            for j in 0..need {
                self.buffer[index + j as usize] = (word >> (j * 8)) as u8;
            }
        }
    }
    /// The pad to the next byte boundary, written zero (§3.3).
    pub fn align(&mut self) {
        self.put(0, (-self.bits) & 7);
    }
    pub fn put_bytes(&mut self, data: &[u8]) {
        self.align();
        if self.measuring {
            self.bits += data.len() as i64 * 8;
            return;
        }
        let at = (self.bits / 8) as usize;
        self.bits += data.len() as i64 * 8;
        if at > self.buffer.len() - data.len() {
            self.overflow = true;
            return;
        }
        self.buffer[at..at + data.len()].copy_from_slice(data);
    }
}

/// The bit reader, the writer's mirror (§3.3).
pub struct TableBitReader<'a> {
    buffer: &'a [u8],
    offset: i64,
}

impl<'a> TableBitReader<'a> {
    pub fn new(buffer: &'a [u8]) -> Self {
        TableBitReader { buffer, offset: 0 }
    }
    pub fn get(&mut self, n: i64) -> Option<u64> {
        if n < 0 || n > 64 || self.offset < 0 || n > self.buffer.len() as i64 * 8 - self.offset {
            return None;
        }
        if n == 0 {
            return Some(0);
        }
        let at = (self.offset >> 3) as usize;
        let shift = (self.offset & 7) as u32;
        let need = (shift as i64 + n + 7) / 8;
        let mut value: u64 = if need >= 8 {
            let word = u64::from_le_bytes(self.buffer[at..at + 8].try_into().expect("eight bytes"));
            let mut v = word >> shift;
            if need > 8 {
                v |= u64::from(self.buffer[at + 8]) << (64 - shift);
            }
            v
        } else {
            let mut v = 0u64;
            for j in 0..need {
                v |= u64::from(self.buffer[at + j as usize]) << (8 * j);
            }
            v >> shift
        };
        if n < 64 {
            value &= (1u64 << n) - 1;
        }
        self.offset += n;
        Some(value)
    }
    pub fn skip(&mut self, n: i64) -> bool {
        if n < 0 || n > self.buffer.len() as i64 * 8 - self.offset {
            return false;
        }
        self.offset += n;
        true
    }
    /// The pad to the next byte boundary is VERIFIED ZERO (§3.3).
    pub fn align(&mut self) -> bool {
        self.get((-self.offset) & 7) == Some(0)
    }
}

/// bits_required(0, n): the width of a reference into an n-entry vocabulary,
/// one bit even for none (§3.3).
pub fn table_message_bits(n: u64) -> i64 {
    let mut count = 0i64;
    let mut v = n;
    while v != 0 {
        count += 1;
        v >>= 1;
    }
    count
}

/// A reference used as a VALUE — an enum variant, a keyed slot key, an arm's
/// name or a node type id — must name a kind-0 entry (§3.3).
impl<'a> TableVocabulary<'a> {
    pub fn name(&self, reference: u64) -> Option<u64> {
        let e = self.entry(reference)?;
        if e.kind == 0 && e.id < TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            Some(e.id)
        } else {
            None
        }
    }
}

// ---- the batch's open and close ---------------------------------------------

/// Opens a batch: the form byte, the count, and the body stream (§3.3). A
/// refusal by name returns the reason on the report and a negative count; the
/// wire's M is the returned count on a read-side batch_too_large.
pub fn table_message_batch_open<'a>(
    v: &TableVocabulary,
    data: &'a [u8],
    report: &mut TableReport,
) -> Result<(TableBitReader<'a>, i64), i64> {
    if data.is_empty() {
        report.malformed = true;
        return Err(-1);
    }
    if data[0] != TABLE_WIRE_MESSAGE_FORM {
        report.refuse(TableReason::NewerForm);
        return Err(-1);
    }
    if !v.announced {
        report.refuse(TableReason::NoVocabulary);
        return Err(-1);
    }
    if data.len() < 2 {
        report.malformed = true;
        return Err(-1);
    }
    let count = data[1] as i64 + 1;
    Ok((TableBitReader::new(&data[2..]), count))
}

/// Closes a batch: the pad to the next byte boundary is verified zero, and no
/// byte may remain after it (§3.3).
pub fn table_message_batch_close(r: &mut TableBitReader, report: &mut TableReport) -> bool {
    if !r.align() || r.offset != r.buffer.len() as i64 * 8 {
        report.malformed = true;
        return false;
    }
    true
}

/// The read side of one scalar: the value at the announced width, with the
/// ranged base added back and a quantized index de-quantized (§3.3). Answers
/// the low and high halves of up to 128 bits.
pub fn table_message_read_scalar(r: &mut TableBitReader, s: &TableMessageShape) -> Option<(u64, u64)> {
    let lo = r.get(s.bits.min(64) as i64)?;
    let mut hi = 0u64;
    if s.bits > 64 {
        hi = r.get((s.bits - 64) as i64)?;
    }
    if s.packing == 1 {
        let sum = lo.wrapping_add(s.base_lo);
        let carry = u64::from(sum < lo);
        let lo = sum;
        let hi = hi.wrapping_add(s.base_hi).wrapping_add(carry);
        return Some((lo, hi));
    }
    if s.packing == 2 {
        let index = (lo as u32).min(s.qcount);
        let ratio = index as f32 / s.qcount as f32;
        let scaled = ratio * s.qdelta;
        return Some((f32::to_bits(scaled + s.qmin) as u64, 0));
    }
    Some((lo, hi))
}

/// The write side of one scalar: the value minus the ranged base, or the
/// quantized index of a compressed float, at the announced width (§3.3).
pub fn table_message_write_scalar(w: &mut TableBitWriter, s: &TableMessageShape, mut lo: u64, mut hi: u64) {
    if s.packing == 1 {
        let borrow = u64::from(lo < s.base_lo);
        lo = lo.wrapping_sub(s.base_lo);
        hi = hi.wrapping_sub(s.base_hi).wrapping_sub(borrow);
    }
    if s.packing == 2 {
        let value = f32::from_bits(lo as u32);
        let ratio = (value - s.qmin) / s.qdelta;
        let ratio = if ratio < 0.0 {
            0.0
        } else if ratio > 1.0 {
            1.0
        } else {
            ratio
        };
        let scaled = ratio * s.qcount as f32;
        lo = ((scaled + 0.5).floor() as u32).min(s.qcount) as u64;
    }
    w.put(lo, s.bits.min(64) as i64);
    if s.bits > 64 {
        w.put(hi, (s.bits - 64) as i64);
    }
}

/// The count of an array or a keyed array, and a string's length: the ranged
/// integer its declared shape spells (§3.3).
pub fn table_message_count(r: &mut TableBitReader, s: &TableMessageShape) -> Option<u64> {
    let raw = r.get(table_message_bits(s.max - s.min))?;
    Some(raw + s.min)
}

// ---- the generic skip -------------------------------------------------------

/// A tolerant skip of a field the reader cannot name, by its announced shape
/// (§3.3): the packet wire's own skip rules, one function over the closed kind
/// set. It is the reader's counterpart to the writer's stores and moves the
/// cursor over a payload exactly.
pub fn table_message_skip(r: &mut TableBitReader, v: &TableVocabulary, e: &TableMessageEntry, depth: usize) -> bool {
    if depth > 128 {
        return false;
    }
    let s = &e.shape;
    match e.kind {
        0 | 32 => true,
        30 => {
            let reference = match r.get(v.ref_bits as i64) {
                Some(x) => x,
                None => return false,
            };
            reference == 0 || v.name(reference).is_some()
        }
        13 => table_message_skip_body(r, v, depth + 1),
        17 => false, // a node index, whose width the node count settles (§3.1)
        15 => {
            let reference = match r.get(v.ref_bits as i64) {
                Some(x) => x,
                None => return false,
            };
            if reference == 0 {
                return true;
            }
            match v.entry(reference).copied() {
                Some(a) if a.id < TABLE_MESSAGE_VOCABULARY_FIELD_ID && a.kind != 0 => {
                    table_message_skip(r, v, &a, depth + 1)
                }
                _ => false,
            }
        }
        12 | 33 => {
            let n = match table_message_count(r, s) {
                Some(x) => x,
                None => return false,
            };
            let width: i64 = if e.kind == 12 { 8 } else { 16 };
            if e.kind == 12 && !r.align() {
                return false;
            }
            r.skip(n as i64 * width)
        }
        31 => {
            if !r.align() {
                return false;
            }
            let n = match r.get(32) {
                Some(x) => x,
                None => return false,
            };
            r.skip(n as i64 * 8)
        }
        14 | 16 => {
            let n = match table_message_count(r, s) {
                Some(x) => x,
                None => return false,
            };
            if e.kind == 14 && e.shape.elem == 6 && !r.align() {
                return false;
            }
            if e.element.bits > 0 {
                let mut width = e.element.bits as i64;
                if e.kind == 16 {
                    width += v.ref_bits as i64;
                }
                return r.skip(n as i64 * width);
            }
            for _ in 0..n {
                if e.kind == 16 && !r.skip(v.ref_bits as i64) {
                    return false;
                }
                let elem = TableMessageEntry { shape: e.element, ..Default::default() };
                if !table_message_skip(r, v, &elem, depth + 1) {
                    return false;
                }
            }
            true
        }
        _ => r.skip(e.shape.bits as i64),
    }
}

fn table_message_skip_body(r: &mut TableBitReader, v: &TableVocabulary, depth: usize) -> bool {
    if depth > 128 {
        return false;
    }
    loop {
        let reference = match r.get(v.ref_bits as i64) {
            Some(x) => x,
            None => return false,
        };
        if reference == 0 {
            return true;
        }
        let entry = v.entry(reference).copied();
        match entry {
            Some(e) if table_message_skip(r, v, &e, depth) => {}
            _ => return false,
        }
    }
}
`

// ---- the per-root surface ---------------------------------------------------

// messageModule emits <base>_message.rs: this unit's reference width, then for
// every fixed root of this file the batch verbs and their body walkers. The
// message form is a fixed table's and nobody else's (§3.3).
func (g *gen) messageModule() []byte {
	roots := g.fixedRoots()
	if len(roots) == 0 {
		return nil
	}
	reach, _ := unitFixedClosure(g.unit, g.closure)
	var members []*ir.Struct
	for _, st := range g.cookRoots() {
		if reach[st.Name] {
			members = append(members, st)
		}
	}
	g.body.Reset()
	g.pf("%s", header(g.file.Base, g.unit.Package, "THE MESSAGE FORM, form byte 2 (docs/SPEC-TABLES.md §3.3)"))
	g.pf(`//
// A form-2 wire is THREE PARTS: the form byte, the body count, and the bodies
// as one continuous bit stream, zero-padded to the next byte at the end. A body
// is a sequence of fields, each a REFERENCE followed by a PAYLOAD and nothing
// else, ending at its own zero reference.
//
// The VALUE is the unit's blittable ` + "`<Name>Row`" + `, the same plain data the fixed
// form carries (§7.2).

#![allow(unused_imports)]
#![allow(clippy::needless_range_loop)]
#![allow(clippy::too_many_arguments)]

use crate::*;

`)
	g.messageVocabulary()
	for _, st := range members {
		g.emitMessageWriteBody(st)
		g.emitMessageReadBody(st)
	}
	for _, st := range roots {
		g.emitMessageSurface(st)
	}
	return []byte(g.body.String())
}

// messageVocabulary emits the unit's own reference width as a compile-time
// constant: the bits a writer spends on every reference of every body it
// writes (§3.3).
func (g *gen) messageVocabulary() {
	entries := ir.TableVocabulary(g.unit)
	g.pf("/// THIS UNIT'S OWN REFERENCE WIDTH (§3.3): the bits a writer spends on\n")
	g.pf("/// every reference of every body it writes.\n")
	g.pf("pub const TABLE_MESSAGE_REF_BITS_HERE: i64 = %d;\n\n", ir.TableMessageRefBits(len(entries)))
}

// messageShapeLiteral spells one resolved shape for an emitted literal: the
// packing, the resolved width, the ranged base, the array bounds and element
// kind, and the quantized triple's own derivation, exactly as AnnounceRead
// resolves them at run time.
func messageShapeLiteral(kind uint8, s ir.TableMessageShape) string {
	lo, hi := uint64(0), uint64(0)
	if s.Base != nil {
		v := new(big.Int).Set(s.Base)
		v.Mod(v, new(big.Int).Lsh(big.NewInt(1), 128))
		lo = v.Uint64()
		hi = new(big.Int).Rsh(v, 64).Uint64()
	}
	if kind < 18 {
		hi = 0
	}
	count, delta, _ := ir.TableMessageQuantization(s)
	bits := ir.TableMessageValueBits(kind, s)
	if bits < 0 {
		bits = 0
	}
	return fmt.Sprintf("TableMessageShape{packing:%d,bits:%d,base_lo:0x%x,base_hi:0x%x,min:%d,max:%d,elem:%d,qmin:f32::from_bits(0x%x),qdelta:f32::from_bits(0x%x),qcount:%d}",
		s.Packing, bits, lo, hi, s.Min, s.Max, s.Elem,
		math.Float32bits(s.QMin), math.Float32bits(delta), count)
}

// messageSlot is one entry's slot in this unit's vocabulary, counted from 1.
func messageSlot(u *ir.Unit, e ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(u) {
		if e.Key() == v.Key() {
			return i + 1
		}
	}
	return 0
}

// nameSlot is a kind-0 entry's slot by its id (a variant name, an arm name, or
// a reserved name), counted from 1.
func nameSlot(u *ir.Unit, id uint64) int {
	for i, e := range ir.TableVocabulary(u) {
		if e.Id == id && e.Kind == 0 {
			return i + 1
		}
	}
	return 0
}

// isStructRef / isUnionRef report a named struct or union value field.
func isStructRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

func isUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

func i128Name(f *ir.Field) string {
	if f.Type.Signed {
		return "I128"
	}
	return "U128"
}

// messageRides is the Rust expression that says whether a field is written: the
// message form elides a field at its declared default (§3.3).
func (g *gen) messageRides(f *ir.Field, expr string) string {
	switch {
	case f.Type.Optional:
		return expr + "_present"
	case f.KeyEnum != "":
		return "true" // a keyed array rides; its own walk counts the present slots
	case f.Array == ir.ArrayCounted:
		return expr + "_count > 0"
	case f.Array != ir.ArrayNone:
		return "true" // a fixed or unbounded array always rides
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		return expr + "_length > 0"
	case isUnionRef(f.Type):
		return expr + ".tag != 0"
	case enumRef(f) != nil:
		return expr + " != 0"
	case f.Type.Kind == ir.TBool:
		return expr
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return expr + " != " + rustDefaultLiteral(f)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits || f.Type.Kind == ir.TFixed:
		return expr + " != " + rustDefaultLiteral(f)
	case f.Type.Kind == ir.TNamed:
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			return expr + " != " + rustDefaultLiteral(f)
		}
	case isStructRef(f.Type):
		return "true" // a nested type always rides; its own walk elides its fields
	}
	return "true"
}

// rustDefaultLiteral is the Rust literal a field's default compares against on
// the write side.
func rustDefaultLiteral(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return floatLit(f.DefFloat, 32)
		}
		return "0.0f32"
	case ir.TFloat64:
		if f.HasDefault {
			return floatLit(f.DefFloat, 64)
		}
		return "0.0f64"
	case ir.TInt, ir.TBits, ir.TFixed:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case ir.TNamed:
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String()
			}
			return "0"
		}
	}
	return "0"
}

func floatLit(v float64, bits int) string {
	s := fmt.Sprintf("%g", v)
	if bits == 32 {
		return s + "f32"
	}
	return s + "f64"
}

// emitMessageWriteBody is one type's bitpacked stores: a straight line of
// references and payloads, by field, ending at the zero reference.
func (g *gen) emitMessageWriteBody(st *ir.Struct) {
	g.pf("/// %s's stores, each a REFERENCE then its PAYLOAD (§3.3).\n", st.Name)
	g.pf("pub fn %s(w: &mut TableBitWriter, value: &%sRow) -> bool {\n", fn(st.Name, "save_message_body"), st.Name)
	for _, f := range st.Fields {
		entry := ir.TableFieldEntry(f)
		g.pf("    {\n")
		g.pf("        if %s {\n", g.messageRides(f, "value."+f.Name))
		g.pf("            w.put(%d, TABLE_MESSAGE_REF_BITS_HERE);\n", messageSlot(g.unit, entry))
		g.emitMessageWriteValue(f, entry, "value."+f.Name, "            ")
		g.pf("        }\n")
		g.pf("    }\n")
	}
	g.pf("    w.put(0, TABLE_MESSAGE_REF_BITS_HERE);\n    !w.overflow\n}\n\n")
}

func (g *gen) emitMessageWriteValue(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("%sif %s { w.put(1, 1); } else { w.put(0, 1); }\n", ind, expr)
	case f.Type.Kind == ir.TString:
		g.pf("%sw.put(%s_length as u64, %d);\n", ind, expr, ir.TableMessageCountBits(entry.Shape))
		g.pf("%sw.put_bytes(&%s[..%s_length as usize]);\n", ind, expr, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sw.put(%s_length as u64, %d);\n", ind, expr, ir.TableMessageCountBits(entry.Shape))
		g.pf("%sw.put_bytes(&%s[..%s_length as usize]);\n", ind, expr, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("%sw.put(%s_length as u64, %d);\n", ind, expr, ir.TableMessageCountBits(entry.Shape))
		g.pf("%sfor i in 0..%s_length as usize { w.put(%s[i] as u64, 16); }\n", ind, expr, expr)
	case f.KeyEnum != "":
		g.emitMessageWriteKeyed(f, entry, expr, ind)
	case f.Array == ir.ArrayCounted:
		g.pf("%sw.put(%s_count as u64, %d);\n", ind, expr, ir.TableMessageCountBits(entry.Shape))
		g.emitMessageWriteArray(f, entry, expr, ind, true)
	case f.Array == ir.ArrayFixed:
		g.emitMessageWriteArray(f, entry, expr, ind, false)
	case isStructRef(f.Type):
		g.pf("%sif !%s(w, &%s) { return false; }\n", ind, fn(f.Type.Name, "save_message_body"), expr)
	case isUnionRef(f.Type):
		g.emitMessageWriteUnion(f, expr, ind)
	case enumRef(f) != nil:
		g.emitMessageWriteEnum(f, expr, ind)
	default:
		g.emitMessageWriteScalar(f, entry, expr, ind)
	}
}

func (g *gen) emitMessageWriteScalar(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	lo, hi := "0", "0"
	switch {
	case f.Type.Kind == ir.TFloat32:
		lo = fmt.Sprintf("(f32::to_bits(%s) as u64)", expr)
	case f.Type.Kind == ir.TFloat64:
		lo = fmt.Sprintf("f64::to_bits(%s)", expr)
	case f.Type.Width > 64:
		lo = fmt.Sprintf("(%s.0 as u64)", expr)
		hi = fmt.Sprintf("((%s.0 as u128 >> 64) as u64)", expr)
	case f.Type.Signed && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed):
		lo = fmt.Sprintf("(%s as i64 as u64)", expr)
	default:
		lo = fmt.Sprintf("(%s as u64)", expr)
	}
	g.pf("%stable_message_write_scalar(w, &%s, %s, %s);\n", ind, messageShapeLiteral(entry.Kind, entry.Shape), lo, hi)
}

func (g *gen) emitMessageWriteEnum(f *ir.Field, expr, ind string) {
	e := enumRef(f)
	g.pf("%sif %s == 0 { w.put(0, TABLE_MESSAGE_REF_BITS_HERE); } else {\n", ind, expr)
	g.pf("%s    let slot = match %s {\n", ind, expr)
	for i := range e.Variants {
		g.pf("%s        %d => %d,\n", ind, i+1, nameSlot(g.unit, ir.TableWireId(e.VariantWireName(i))))
	}
	g.pf("%s        _ => 0,\n", ind)
	g.pf("%s    };\n", ind)
	g.pf("%s    if slot == 0 { return false; }\n", ind)
	g.pf("%s    w.put(slot as u64, TABLE_MESSAGE_REF_BITS_HERE);\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageWriteUnion(f *ir.Field, expr, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%smatch %s.tag {\n", ind, expr)
	g.pf("%s    0 => w.put(0, TABLE_MESSAGE_REF_BITS_HERE),\n", ind)
	for i, v := range un.Variants {
		g.pf("%s    %d => {\n", ind, i+1)
		g.pf("%s        w.put(%d, TABLE_MESSAGE_REF_BITS_HERE);\n", ind, messageSlot(g.unit, ir.TableArmEntry(v)))
		if v.F != nil && !v.Void() {
			g.pf("%s        let arm = unsafe { %s.arms.%s };\n", ind, expr, v.Name)
			entry := ir.TableArmEntry(v)
			g.emitMessageWriteValue(v.F, entry, "arm", ind+"        ")
		}
		g.pf("%s    }\n", ind)
	}
	g.pf("%s    _ => return false,\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageWriteArray(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string, counted bool) {
	elem := ir.TableVocabularyEntry{Kind: entry.Shape.Elem, Shape: *entry.Shape.Inner}
	n := fmt.Sprintf("%d", f.ArrayBound)
	if counted {
		n = expr + "_count as usize"
	}
	g.pf("%sfor i in 0..%s {\n", ind, n)
	g.emitMessageWriteValue(messageElement(f), elem, expr+"[i]", ind+"    ")
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageWriteKeyed(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	elem := ir.TableVocabularyEntry{Kind: entry.Shape.Elem, Shape: *entry.Shape.Inner}
	element := messageElement(f)
	g.pf("%slet mut pairs = 0u64;\n", ind)
	g.pf("%sfor i in 0..%d { if %s { pairs += 1; } }\n", ind, f.ArrayBound, g.messageRides(element, expr+"[i]"))
	g.pf("%sw.put(pairs, %d);\n", ind, ir.TableMessageCountBits(entry.Shape))
	g.pf("%sfor i in 0..%d {\n", ind, f.ArrayBound)
	g.pf("%s    if !(%s) { continue; }\n", ind, g.messageRides(element, expr+"[i]"))
	g.pf("%s    let slot = match i {\n", ind)
	for i := range f.KeyEnumRef.Variants {
		g.pf("%s        %d => %d,\n", ind, i, nameSlot(g.unit, ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))
	}
	g.pf("%s        _ => 0,\n", ind)
	g.pf("%s    };\n", ind)
	g.pf("%s    if slot == 0 { return false; }\n", ind)
	g.pf("%s    w.put(slot as u64, TABLE_MESSAGE_REF_BITS_HERE);\n", ind)
	g.emitMessageWriteValue(element, elem, expr+"[i]", ind+"    ")
	g.pf("%s}\n", ind)
}

// messageElement returns the field shaped as one array element.
func messageElement(f *ir.Field) *ir.Field {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.Type.Optional = false
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		e.Type = ir.FieldType{Kind: ir.TInt, Width: 8}
	}
	return &e
}

// emitMessageReadBody is one type's bitpacked reads, resolving each reference
// against the vocabulary and dispatching by id.
func (g *gen) emitMessageReadBody(st *ir.Struct) {
	g.pf("/// %s's reads: every reference resolved and every payload decoded (§3.3).\n", st.Name)
	g.pf("pub fn %s(r: &mut TableBitReader, v: &TableVocabulary, value: &mut %sRow, report: &mut TableReport) -> bool {\n", fn(st.Name, "load_message_body"), st.Name)
	g.pf("    loop {\n")
	g.pf("        let reference = match r.get(v.ref_bits as i64) { Some(x) => x, None => { report.malformed = true; return false; } };\n")
	g.pf("        if reference == 0 { return true; }\n")
	g.pf("        let entry = match v.entry(reference) { Some(e) => *e, None => { report.malformed = true; return false; } };\n")
	g.pf("        if entry.id >= TABLE_MESSAGE_VOCABULARY_FIELD_ID { report.malformed = true; return false; }\n")
	g.pf("        match entry.id {\n")
	for _, f := range st.Fields {
		mine := ir.TableFieldEntry(f)
		g.pf("            0x%016x => {\n", mine.Id)
		g.emitMessageReadValue(f, mine, "value."+f.Name, "                ")
		g.pf("            }\n")
	}
	g.pf("            _ => {\n")
	g.pf("                report.unknown += 1;\n")
	g.pf("                if !table_message_skip(r, v, &entry, 0) { report.malformed = true; return false; }\n")
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("    }\n")
	g.pf("}\n\n")
}

func (g *gen) emitMessageReadValue(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	switch {
	case f.Type.Kind == ir.TBool:
		g.pf("%s%s = match r.get(1) { Some(0) => false, Some(_) => true, None => { report.malformed = true; return false; } };\n", ind, expr)
	case f.Type.Kind == ir.TString:
		g.emitMessageReadText(f, expr, ind)
	case f.Type.Kind == ir.TBytes:
		g.emitMessageReadText(f, expr, ind)
	case f.Type.Kind == ir.TWString:
		g.emitMessageReadWText(f, expr, ind)
	case f.KeyEnum != "":
		g.emitMessageReadKeyed(f, entry, expr, ind)
	case f.Array == ir.ArrayCounted || f.Array == ir.ArrayFixed:
		g.emitMessageReadArray(f, entry, expr, ind)
	case isStructRef(f.Type):
		g.pf("%sif !%s(r, v, &mut %s, report) { return false; }\n", ind, fn(f.Type.Name, "load_message_body"), expr)
	case isUnionRef(f.Type):
		g.emitMessageReadUnion(f, expr, ind)
	case enumRef(f) != nil:
		g.emitMessageReadEnum(f, expr, ind)
	default:
		g.emitMessageReadScalar(f, entry, expr, ind)
	}
	if f.Type.Optional {
		g.pf("%s%s_present = true;\n", ind, expr)
	}
}

func (g *gen) emitMessageReadScalar(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s    let (lo, hi) = match table_message_read_scalar(r, &%s) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind, messageShapeLiteral(entry.Kind, entry.Shape))
	switch {
	case f.Type.Kind == ir.TFloat32:
		g.pf("%s    %s = f32::from_bits(lo as u32);\n", ind, expr)
	case f.Type.Kind == ir.TFloat64:
		g.pf("%s    %s = f64::from_bits(lo);\n", ind, expr)
	case f.Type.Width > 64:
		if f.Type.Signed {
			g.pf("%s    %s = TableI128((((hi as u128) << 64) | (lo as u128)) as i128);\n", ind, expr)
		} else {
			g.pf("%s    %s = TableU128(((hi as u128) << 64) | (lo as u128));\n", ind, expr)
		}
	case f.Type.Signed && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed):
		g.pf("%s    %s = lo as i64 as %s;\n", ind, expr, cookElemType(f))
	default:
		g.pf("%s    %s = lo as %s;\n", ind, expr, cookElemType(f))
	}
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadText(f *ir.Field, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s    let n = match table_message_count(r, &%s) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind, messageShapeLiteral(ir.TableKindString, ir.TableMessageShape{Max: int64(f.Type.Size)}))
	g.pf("%s    if !r.align() { report.malformed = true; return false; }\n", ind)
	g.pf("%s    let n = (n as usize).min(%d);\n", ind, f.Type.Size)
	g.pf("%s    %s = [0u8; %d];\n", ind, expr, f.Type.Size+1)
	g.pf("%s    for i in 0..n { %s[i] = match r.get(8) { Some(b) => b as u8, None => { report.malformed = true; return false; } } }\n", ind, expr)
	g.pf("%s    %s_length = n as i32;\n", ind, expr)
	g.pf("%s    %s[n] = 0;\n", ind, expr)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadWText(f *ir.Field, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s    let n = match table_message_count(r, &%s) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind, messageShapeLiteral(ir.TableKindWstring, ir.TableMessageShape{Max: int64(f.Type.Size)}))
	g.pf("%s    let n = (n as usize).min(%d);\n", ind, f.Type.Size)
	g.pf("%s    %s = [0u16; %d];\n", ind, expr, f.Type.Size+1)
	g.pf("%s    for i in 0..n { %s[i] = match r.get(16) { Some(u) => u as u16, None => { report.malformed = true; return false; } } }\n", ind, expr)
	g.pf("%s    %s_length = n as i32;\n", ind, expr)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadArray(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	counted := f.Array == ir.ArrayCounted
	bound := f.ArrayBound
	g.pf("%s{\n", ind)
	var n string
	if counted {
		g.pf("%s    let raw = match table_message_count(r, &%s) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind, messageShapeLiteral(entry.Kind, entry.Shape))
		g.pf("%s    let n = (raw as usize).min(%d);\n", ind, bound)
		n = "n"
	} else {
		n = fmt.Sprintf("%d", bound)
	}
	elem := ir.TableVocabularyEntry{Kind: entry.Shape.Elem, Shape: *entry.Shape.Inner}
	g.pf("%s    for i in 0..%s {\n", ind, n)
	g.emitMessageReadValue(messageElement(f), elem, expr+"[i]", ind+"        ")
	g.pf("%s    }\n", ind)
	if counted {
		g.pf("%s    %s_count = %s as i32;\n", ind, expr, n)
	}
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadEnum(f *ir.Field, expr, ind string) {
	e := enumRef(f)
	g.pf("%s{\n", ind)
	g.pf("%s    let reference = match r.get(v.ref_bits as i64) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s    if reference == 0 { %s = 0; } else {\n", ind, expr)
	g.pf("%s        let id = match v.name(reference) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s        %s = match id {\n", ind, expr)
	for i := range e.Variants {
		g.pf("%s            0x%016x => %d,\n", ind, ir.TableWireId(e.VariantWireName(i)), i+1)
	}
	g.pf("%s            _ => { report.unknown += 1; 0 },\n", ind)
	g.pf("%s        };\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadUnion(f *ir.Field, expr, ind string) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%s{\n", ind)
	g.pf("%s    let reference = match r.get(v.ref_bits as i64) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s    if reference == 0 { %s.tag = 0; } else {\n", ind, expr)
	g.pf("%s        let arm = match v.entry(reference) { Some(a) => *a, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s        if arm.id >= TABLE_MESSAGE_VOCABULARY_FIELD_ID || arm.kind == 0 { report.malformed = true; return false; }\n", ind)
	g.pf("%s        match arm.id {\n", ind)
	for i, v := range un.Variants {
		mine := ir.TableArmEntry(v)
		g.pf("%s            0x%016x => {\n", ind, mine.Id)
		g.pf("%s                %s.tag = %d;\n", ind, expr, i+1)
		if v.F != nil && !v.Void() {
			g.pf("%s                let mut payload = %s::default();\n", ind, cookElemType(v.F))
			g.emitMessageReadValue(v.F, mine, "payload", ind+"                ")
			g.pf("%s                %s.arms.%s = payload;\n", ind, expr, v.Name)
		}
		g.pf("%s            }\n", ind)
	}
	g.pf("%s            _ => { report.unknown += 1; %s.tag = 0; if !table_message_skip(r, v, &arm, 0) { report.malformed = true; return false; } }\n", ind, expr)
	g.pf("%s        }\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadKeyed(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s    let n = match table_message_count(r, &%s) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind, messageShapeLiteral(entry.Kind, entry.Shape))
	g.pf("%s    for _ in 0..n {\n", ind)
	g.pf("%s        let key = match r.get(v.ref_bits as i64) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s        let id = match v.name(key) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s        let idx = match id {\n", ind)
	for i := range f.KeyEnumRef.Variants {
		g.pf("%s            0x%016x => %d,\n", ind, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), i)
	}
	g.pf("%s            _ => { report.unknown += 1; usize::MAX },\n", ind)
	g.pf("%s        };\n", ind)
	elem := ir.TableVocabularyEntry{Kind: entry.Shape.Elem, Shape: *entry.Shape.Inner}
	g.pf("%s        if idx < %d {\n", ind, f.ArrayBound)
	g.emitMessageReadValue(messageElement(f), elem, expr+"[idx]", ind+"            ")
	g.pf("%s        }\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
}

// emitMessageSurface is one root's three verbs (§3.3).
func (g *gen) emitMessageSurface(st *ir.Struct) {
	n := st.Name
	typ := st.Name + "Row"
	g.pf("/// %s's batch measure: the buffer size a save will write (§3.3).\n", st.Name)
	g.pf("pub fn %s(values: &[&%s], report: &mut TableReport) -> i64 {\n", fn(n, "measure_messages"), typ)
	g.pf("    if values.len() < 1 || values.len() > TABLE_MESSAGE_BATCH_MAX { report.refuse(TableReason::BatchTooLarge); return -1; }\n")
	g.pf("    let mut w = TableBitWriter::measuring();\n")
	g.pf("    for value in values { if !%s(&mut w, value) { return -1; } }\n", fn(n, "save_message_body"))
	g.pf("    w.align();\n")
	g.pf("    if w.overflow { return -1; }\n")
	g.pf("    2 + w.bits / 8\n")
	g.pf("}\n\n")
	g.pf("/// %s's batch save: the form byte, the count, then the bodies (§3.3).\n", st.Name)
	g.pf("pub fn %s(values: &[&%s], buffer: &mut [u8], report: &mut TableReport) -> i64 {\n", fn(n, "save_messages"), typ)
	g.pf("    if values.len() < 1 || values.len() > TABLE_MESSAGE_BATCH_MAX { report.refuse(TableReason::BatchTooLarge); return -1; }\n")
	g.pf("    if buffer.len() < 2 { return -1; }\n")
	g.pf("    buffer[0] = TABLE_WIRE_MESSAGE_FORM;\n")
	g.pf("    buffer[1] = (values.len() - 1) as u8;\n")
	g.pf("    let mut w = TableBitWriter::writing(&mut buffer[2..]);\n")
	g.pf("    for value in values { if !%s(&mut w, value) { return -1; } }\n", fn(n, "save_message_body"))
	g.pf("    w.align();\n")
	g.pf("    if w.overflow { return -1; }\n")
	g.pf("    2 + w.bits / 8\n")
	g.pf("}\n\n")
	g.pf("/// %s's batch load: reads one buffer into the caller's storage (§3.3).\n", st.Name)
	g.pf("pub fn %s(values: &mut [%s], v: &TableVocabulary, data: &[u8], report: &mut TableReport) -> Option<i64> {\n", fn(n, "load_messages"), typ)
	g.pf("    let mut r = match table_message_batch_open(v, data, report) {\n")
	g.pf("        Ok((r, count)) => {\n")
	g.pf("            if count > values.len() as i64 { report.refuse(TableReason::BatchTooLarge); return Some(count); }\n")
	g.pf("            r\n")
	g.pf("        }\n")
	g.pf("        Err(_) => return None,\n")
	g.pf("    };\n")
	g.pf("    let count = data[1] as i64 + 1;\n")
	g.pf("    for i in 0..count as usize {\n")
	g.pf("        if !%s(&mut r, v, &mut values[i], report) { return Some(i as i64); }\n", fn(n, "load_message_body"))
	g.pf("    }\n")
	g.pf("    if table_message_batch_close(&mut r, report) { Some(count) } else { None }\n")
	g.pf("}\n\n")
}
