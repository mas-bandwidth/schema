// THE MESSAGE FORM'S ANNOUNCEMENT ON THE RUST LEG (docs/SPEC-TABLES.md §3.3).
//
// The message form is a BATCH of bitpacked bodies under one announced
// vocabulary. "The ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT, every
// byte of it settled by the compiler, so a backend emits Announce and
// AnnounceMeasure as a constant byte array and its length rather than as a
// walk" (§3.3). The bytes are the C++ reference's, pinned by
// testdata/conformance/tables/MANIFEST.txt's `connection` rows and rebuilt
// here by tablewire.Announce.
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

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
	announcement := tablewire.Announce(u)
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

	b.WriteString("/// THE ANNOUNCEMENT, byte for byte: the unit's message vocabulary as a\n")
	b.WriteString("/// compile-time constant (§3.3). It is an ordinary form-1 FILE whose body\n")
	b.WriteString("/// carries the build version at kind 9 and the vocabulary at kind 14 over\n")
	b.WriteString("/// element kind 6, and whose trailer is those two reserved ids.\n")
	b.WriteString("#[allow(clippy::large_const_arrays)]\n")
	fmt.Fprintf(&b, "pub const TABLE_ANNOUNCEMENT: [u8; %d] = [", len(announcement))
	for i, v := range announcement {
		if i%12 == 0 {
			b.WriteString("\n    ")
		}
		fmt.Fprintf(&b, "0x%02x,", v)
	}
	b.WriteString("\n];\n\n")
	b.WriteString("/// R8: the writer refuses a vocabulary past the readers' default entry maximum.\n")
	fmt.Fprintf(&b, "pub const TABLE_ANNOUNCEMENT_TOO_LARGE: bool = %t;\n\n", len(tablewire.MessageVocabulary(u)) > ir.TableVocabularyMaxEntries)

	// THE WRITER's reference width is its OWN vocabulary's (§3.3): every slot
	// of the announcement it sent, and the zero reference beside them.
	b.WriteString("/// The width of a reference on a body this build WRITES: bits_required(0, n)\n")
	b.WriteString("/// over its own announced vocabulary of n entries (§3.3).\n")
	fmt.Fprintf(&b, "pub const TABLE_MESSAGE_REF_BITS: u32 = %d;\n\n", ir.TableMessageRefBits(len(tablewire.MessageVocabulary(u))))

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
    /// THE ID'S ORDINAL among the vocabulary's distinct ids, counted from 1 in
    /// announcement order and set once when the announcement is accepted: two
    /// slots announcing one id at two shapes share it, so a body's seen set is
    /// keyed by id (§3.3). Zero for an entry a caller built by hand, which
    /// stands for its own slot.
    pub first: u32,
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
    pub fn new(schema_entries: &'a mut [TableMessageEntry]) -> Self {
        TableVocabulary {
            entries: schema_entries,
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
    pub fn entry(&self, schema_reference: u64) -> Option<&TableMessageEntry> {
        if schema_reference == 0 || schema_reference > self.count as u64 {
            return None;
        }
        Some(&self.entries[schema_reference as usize - 1])
    }

    /// THE SEEN KEY of a reference (§3.3): the entry's id ordinal, set once at
    /// announce. An entry built by hand carries no ordinal and stands for the
    /// first slot announcing its id, found by a scan of the slots before it,
    /// so two slots naming one id share a key either way.
    pub fn seen_key(&self, schema_reference: u64) -> u64 {
        let Some(schema_e) = self.entry(schema_reference) else {
            return schema_reference;
        };
        if schema_e.first != 0 {
            return schema_e.first as u64;
        }
        let schema_before = schema_reference as usize - 1;
        match self.entries[..schema_before].iter().position(|schema_x| schema_x.id == schema_e.id) {
            Some(schema_i) => schema_i as u64 + 1,
            None => schema_reference,
        }
    }

    /// THE ID a NAME reference spells (§3.3): an enum's variant or a keyed
    /// slot rides as a reference to a kind-0 entry, never a reserved id.
    pub fn name(&self, schema_reference: u64) -> Option<u64> {
        let schema_e = self.entry(schema_reference)?;
        if schema_e.kind == 0 && schema_e.id < 0xffff_ffff_ffff_fffd {
            Some(schema_e.id)
        } else {
            None
        }
    }
}

`

// messageRuntimeRead is the runtime after the report: the announcement's
// measure, copy and read (§3.3).
const messageRuntimeRead = `// ---- the read ---------------------------------------------------------------

/// The reader's id-table distinctness bound and open-addressing probe capacity
/// (docs/SPEC-TABLES.md §3).
const ID_TABLE_PROBE_BOUND: usize = 4096;
const ID_TABLE_PROBE_SLOTS: usize = 8192;

/// The announcement's byte count and the copy that hands it out (§3.3): both
/// are the constant, never a walk. Nothing here allocates.
pub fn announce_measure() -> i64 {
    if TABLE_ANNOUNCEMENT_TOO_LARGE {
        return -1;
    }
    TABLE_ANNOUNCEMENT.len() as i64
}

/// Copy the announcement into the caller's buffer and answer its byte count, or
/// -1 when the buffer is short (§3.3).
pub fn announce(buffer: &mut [u8]) -> i64 {
    if TABLE_ANNOUNCEMENT_TOO_LARGE || buffer.len() < TABLE_ANNOUNCEMENT.len() {
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
    schema_vocabulary: &mut TableVocabulary,
    data: &[u8],
    schema_report: &mut TableReport,
) -> bool {
    if schema_vocabulary.announced || schema_vocabulary.refused {
        return schema_report.refuse(TableReason::SecondAnnouncement);
    }
    let ok = announce_read_once(schema_vocabulary, data, schema_report);
    if !ok {
        schema_vocabulary.refused = true;
    }
    ok
}

fn announce_read_once(
    schema_vocabulary: &mut TableVocabulary,
    data: &[u8],
    schema_report: &mut TableReport,
) -> bool {
    if data.is_empty() {
        schema_report.damaged = true;
        return false;
    }
    // "the FORM BYTE is a whole byte and is read FIRST" (§3.3): a form this
    // reader does not carry is a refusal by name and never damage.
    if data[0] != TABLE_WIRE_FORM {
        return schema_report.refuse(if data[0] == TABLE_WIRE_MESSAGE_FORM {
            TableReason::MessageFormAsFile
        } else {
            TableReason::NewerForm
        });
    }
    if data.len() < 9 {
        schema_report.damaged = true;
        return false;
    }
    // the trailer: a run of u64 ids and a u64 count at the very end (§3.3)
    let schema_trailer_count = read_u64(data, data.len() - 8);
    if schema_trailer_count > ((data.len() - 9) / 8) as u64 {
        schema_report.damaged = true;
        return false;
    }
    // "THE DISTINCTNESS CHECK IS BOUNDED WORK, and the bound is a format
    // fact" (§3): the announcement is an ordinary form-1 FILE, so a reader
    // refuses an id table over 4096 whatever the buffer would hold, before any
    // distinctness work.
    if schema_trailer_count > ID_TABLE_PROBE_BOUND as u64 {
        schema_report.damaged = true;
        return false;
    }
    let end = data.len() - 8 - (schema_trailer_count * 8) as usize;
    // "a table carrying one id twice" is the whole wire malformed (§3.3),
    // proved with the open-addressing probe over a fixed 8192-slot stack table
    // rather than a pairwise scan (§3).
    let mut schema_probe_slots = [0u64; ID_TABLE_PROBE_SLOTS];
    let mut schema_probe_used = [0u64; ID_TABLE_PROBE_SLOTS / 64];
    for schema_i in 0..schema_trailer_count as usize {
        let id = read_u64(data, end + schema_i * 8);
        let mut schema_h = ((id.wrapping_mul(0x9e3779b97f4a7c15) >> 51) as usize) & (ID_TABLE_PROBE_SLOTS - 1);
        loop {
            let schema_bit = 1u64 << (schema_h & 63);
            if (schema_probe_used[schema_h >> 6] & schema_bit) == 0 {
                schema_probe_used[schema_h >> 6] |= schema_bit;
                schema_probe_slots[schema_h] = id;
                break;
            }
            if schema_probe_slots[schema_h] == id {
                schema_report.damaged = true;
                return false;
            }
            schema_h = (schema_h + 1) & (ID_TABLE_PROBE_SLOTS - 1);
        }
    }
    let schema_body = &data[1..end];
    // THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS (§3.3):
    // the build version present, exactly once, kind 9, eight bytes wide, and
    // the vocabulary present, exactly once, kind 14 over element kind 6. A
    // FAILED STRICT CHECK IS MALFORMED, not a refusal.
    let mut schema_at = 0usize;
    let mut schema_versions = 0u32;
    let mut schema_vocabularies = 0u32;
    let mut schema_terminated = false;
    let mut schema_vocabulary_bytes: &[u8] = &[];
    let mut schema_seen_refs = [0u64; ID_TABLE_PROBE_BOUND / 64];
    let mut schema_next_ref = 1u64;
    let mut schema_non_finite = false;
    while schema_at < schema_body.len() {
        let (schema_reference, ok) = read_leb(schema_body, &mut schema_at);
        if !ok {
            schema_report.damaged = true;
            return false;
        }
        if schema_reference == 0 {
            schema_terminated = true;
            // "Any byte between the root's terminator and the table's first
            // entry is malformed, because no field claims it and the two ends
            // of the file have met" (§3). The terminator ends the body, so the
            // cursor must be at the body's end.
            if schema_at != schema_body.len() {
                schema_report.damaged = true;
                return false;
            }
            break;
        }
        if schema_reference > schema_trailer_count || schema_reference > schema_next_ref || schema_at >= schema_body.len() {
            schema_report.damaged = true;
            return false;
        }
        if schema_reference == schema_next_ref {
            schema_next_ref += 1;
        }
        let schema_ridx = (schema_reference - 1) as usize;
        let schema_rbit = 1u64 << (schema_ridx & 63);
        if (schema_seen_refs[schema_ridx >> 6] & schema_rbit) != 0 {
            schema_report.damaged = true;
            return false;
        }
        schema_seen_refs[schema_ridx >> 6] |= schema_rbit;
        let id = read_u64(data, end + schema_ridx * 8);
        let schema_kind = schema_body[schema_at];
        schema_at += 1;
        if id == TABLE_BUILD_VERSION_FIELD_ID {
            if schema_kind != 9 || schema_at + 8 > schema_body.len() {
                schema_report.damaged = true;
                return false;
            }
            // "THE BUILD VERSION IS KEPT THE MOMENT IT IS READ, refusal or not"
            // (§3.3): it is not the vocabulary, and a refused announcement
            // still sets none.
            schema_vocabulary.build_version = read_u64(schema_body, schema_at);
            schema_at += 8;
            schema_versions += 1;
        } else if id == TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            if schema_kind != 14 {
                schema_report.damaged = true;
                return false;
            }
            let (schema_framed, ok) = read_leb(schema_body, &mut schema_at);
            if !ok || schema_at > schema_body.len() || schema_framed > (schema_body.len() - schema_at) as u64 {
                schema_report.damaged = true;
                return false;
            }
            let begin = schema_at;
            let schema_end_at = schema_at + schema_framed as usize;
            schema_at = schema_end_at;
            if begin >= schema_end_at || schema_body[begin] != 6 {
                schema_report.damaged = true;
                return false;
            }
            let mut schema_vat = begin + 1;
            let (schema_length, ok) = read_leb(schema_body, &mut schema_vat);
            if !ok || schema_vat > schema_end_at || schema_length != (schema_end_at - schema_vat) as u64 {
                schema_report.damaged = true;
                return false;
            }
            // "A VOCABULARY PAST A BOUND IS REFUSED BEFORE ANYTHING IS
            // ALLOCATED" (§3.3): the byte bound is checked from the field's own
            // length before an entry is touched.
            // A caller bound of 0 refuses every non-empty vocabulary: it never
            // means the default (rulings 2026-11-22, R9).
            if schema_length > schema_vocabulary.max_bytes as u64 {
                return schema_report.refuse(TableReason::VocabularyTooLarge);
            }
            schema_vocabulary_bytes = &schema_body[schema_vat..schema_vat + schema_length as usize];
            schema_vocabularies += 1;
        } else {
            if id == TABLE_NODE_TABLE_ID {
                schema_report.damaged = true;
                return false;
            }
            // "Every other field of its body is ordinary and strict, so an
            // unknown one is skipped and counted" (§3.3).
            schema_report.unknown += 1;
            if !skip_field(schema_body, &mut schema_at, schema_kind, schema_trailer_count, &mut schema_next_ref, &mut schema_non_finite, 0) {
                schema_report.damaged = true;
                return false;
            }
        }
    }
    if !schema_terminated || schema_next_ref - 1 != schema_trailer_count || schema_versions != 1 || schema_vocabularies != 1 {
        schema_report.damaged = true;
        return false;
    }
    if schema_non_finite {
        return schema_report.refuse(TableReason::NonFiniteFloat);
    }
    // "STRICT MODE IS OPT-IN, and the default is unchanged" (§3.3).
    if schema_vocabulary.strict && schema_vocabulary.build_version != BUILD_VERSION {
        schema_report.build_version_remote = schema_vocabulary.build_version;
        schema_report.build_version_own = BUILD_VERSION;
        return schema_report.refuse(TableReason::BuildVersionMismatch);
    }
    // THE ENTRIES, resolved once into the caller's storage (§3.3): every width
    // is checked here and never again. An entry count above the caller's
    // capacity is refused by name before the entry is touched.
    let mut schema_at = 0usize;
    let mut schema_count = 0usize;
    let mut schema_node_seen = false;
    while schema_at < schema_vocabulary_bytes.len() {
        if schema_count >= schema_vocabulary.entries.len() {
            return schema_report.refuse(TableReason::VocabularyTooLarge);
        }
        let schema_began = schema_at;
        let (schema_entry, ok) = read_entry(schema_vocabulary_bytes, &mut schema_at);
        if !ok {
            schema_report.damaged = true;
            return false;
        }
        // THE TWO RESERVED IDS OF THE ANNOUNCEMENT ITSELF ARE NOT IN THE
        // VOCABULARY (§3.3), and the node-table id takes exactly one slot.
        if schema_entry.id == TABLE_BUILD_VERSION_FIELD_ID || schema_entry.id == TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            schema_report.damaged = true;
            return false;
        }
        if schema_entry.id == TABLE_NODE_TABLE_ID {
            if schema_node_seen {
                schema_report.damaged = true;
                return false;
            }
            schema_node_seen = true;
        }
        // "A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE" (§3.3): byte equality
        // is declared-fact equality, and a repeated is malformed.
        {
            let (schema_filled, schema_rest) = schema_vocabulary.entries.split_at_mut(schema_count);
            for schema_other in schema_filled.iter() {
                let schema_other_len = (schema_other.shape.max - schema_other.shape.min) as usize;
                if schema_other_len == schema_at - schema_began
                    && schema_vocabulary_bytes[schema_other.shape.min as usize..schema_other.shape.max as usize]
                        == schema_vocabulary_bytes[schema_began..schema_at]
                {
                    schema_report.damaged = true;
                    return false;
                }
            }
            let schema_slot = &mut schema_rest[0];
            *schema_slot = schema_entry;
            // the byte extent parks in min/max until the second pass resolves it
            schema_slot.shape.min = schema_began as u64;
            schema_slot.shape.max = schema_at as u64;
        }
        schema_count += 1;
    }
    // the second pass: resolve every entry in place over bytes already accepted
    let mut schema_at = 0usize;
    for schema_slot in &mut schema_vocabulary.entries[..schema_count] {
        let (schema_entry, ok) = read_entry(schema_vocabulary_bytes, &mut schema_at);
        if !ok {
            schema_report.damaged = true;
            return false;
        }
        *schema_slot = schema_entry;
    }
    // EACH ID's ORDINAL, once per connection (§3.3): a body's repeat test
    // is then one bit per id, whichever slot names it. The scan is quadratic
    // in the entry count, once per announcement, as the triple test above is.
    let mut schema_ordinals = 0u32;
    for schema_i in 0..schema_count {
        let (schema_before, schema_rest) = schema_vocabulary.entries.split_at_mut(schema_i);
        let id = schema_rest[0].id;
        schema_rest[0].first = match schema_before.iter().find(|schema_e| schema_e.id == id) {
            Some(schema_e) => schema_e.first,
            None => {
                schema_ordinals += 1;
                schema_ordinals
            }
        };
    }
    schema_vocabulary.count = schema_count;
    schema_vocabulary.ref_bits = bits_required(schema_count);
    schema_vocabulary.announced = true;
    true
}

// ---- the wire reader --------------------------------------------------------

fn read_u64(bytes: &[u8], schema_at: usize) -> u64 {
    let mut schema_v = 0u64;
    for schema_i in 0..8 {
        schema_v |= (bytes[schema_at + schema_i] as u64) << (8 * schema_i);
    }
    schema_v
}

fn read_u32(bytes: &[u8], schema_at: usize) -> u32 {
    let mut schema_v = 0u32;
    for schema_i in 0..4 {
        schema_v |= (bytes[schema_at + schema_i] as u32) << (8 * schema_i);
    }
    schema_v
}

/// A canonical LEB128, the announcement's own integer: the announcement is a
/// form-1 FILE and takes §3's rule (§3.3). "An encoding past ten bytes, or a
/// tenth byte with a bit above the 64th value bit, is malformed on the same
/// rule" (docs/SPEC-TABLES.md:3707), so the tenth byte may carry only the
/// 64th value bit: shift 63 with a byte past 0x01 is a bit past it and is
/// refused.
fn read_leb(bytes: &[u8], schema_at: &mut usize) -> (u64, bool) {
    let mut schema_value = 0u64;
    let mut schema_shift = 0u32;
    while *schema_at < bytes.len() {
        let schema_b = bytes[*schema_at];
        *schema_at += 1;
        if schema_shift >= 64 {
            return (0, false);
        }
        if schema_shift == 63 && schema_b > 1 {
            return (0, false);
        }
        schema_value |= ((schema_b & 0x7f) as u64) << schema_shift;
        if schema_b & 0x80 == 0 {
            return if schema_shift > 0 && schema_b == 0 {
                (0, false)
            } else {
                (schema_value, true)
            };
        }
        schema_shift += 7;
    }
    (0, false)
}

/// bits_required(0, count), the width of a reference into a count-entry
/// vocabulary (§3.3): one bit even for a vocabulary of no entries.
fn bits_required(schema_count: usize) -> u32 {
    if schema_count == 0 {
        return 1;
    }
    64 - (schema_count as u64).leading_zeros()
}

/// 31 is held by name and never written (R17): an announcement naming it is
/// malformed, as one naming 33 or 34 is.
fn known_kind(schema_kind: u8) -> bool {
    schema_kind == 0 || (1..=17).contains(&schema_kind) || (18..=29).contains(&schema_kind) || schema_kind == 30 || schema_kind == 32 || schema_kind == 36
}

fn integer_kind(schema_kind: u8) -> bool {
    (2..=9).contains(&schema_kind) || schema_kind == 18 || schema_kind == 19
}

fn fixed_kind(schema_kind: u8) -> bool {
    (20..=29).contains(&schema_kind)
}

/// The widest ranged value a kind can carry, its own storage width (§3.3): a
/// width above it is a hostile width on the announcement.
fn kind_bits(schema_kind: u8) -> u64 {
    match schema_kind {
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
fn quantization(schema_qmin: f32, schema_qmax: f32, schema_qres: f32) -> (u32, f32, u64, bool) {
    if !schema_qmin.is_finite() || !schema_qmax.is_finite() || !schema_qres.is_finite() || schema_qmin >= schema_qmax || schema_qres <= 0.0 {
        return (0, 0.0, 0, false);
    }
    let schema_delta = schema_qmax - schema_qmin;
    let mut schema_values = schema_delta / schema_qres;
    if !schema_delta.is_finite() || !schema_values.is_finite() {
        return (0, 0.0, 0, false);
    }
    if schema_values.is_nan() || schema_values < 1.0 {
        schema_values = 1.0;
    } else if schema_values > 4294967040.0 {
        schema_values = 4294967040.0;
    }
    let schema_count = schema_values as u32;
    let schema_count = if (schema_count as f32) < schema_values { schema_count + 1 } else { schema_count };
    let bits = if schema_count == 0 {
        0u64
    } else {
        64u64 - (schema_count as u64).leading_zeros() as u64
    };
    (schema_count, schema_delta, bits, true)
}

/// ONE entry: an id, a kind and the shape the kind names (§3.3). False is a
/// HOSTILE SHAPE — bits above the kind's own domain, an array whose min
/// exceeds its max, an element kind outside the closed set, a non-conforming
/// quantized triple, or a shape running past the vocabulary's own bytes.
fn read_entry(bytes: &[u8], schema_at: &mut usize) -> (TableMessageEntry, bool) {
    let mut schema_e = TableMessageEntry::default();
    if *schema_at + 9 > bytes.len() {
        return (schema_e, false);
    }
    schema_e.id = read_u64(bytes, *schema_at);
    schema_e.kind = bytes[*schema_at + 8];
    *schema_at += 9;
    if !known_kind(schema_e.kind) {
        return (schema_e, false);
    }
    let (schema_shape, ok) = read_shape(bytes, schema_at, schema_e.kind);
    if !ok {
        return (schema_e, false);
    }
    schema_e.shape = schema_shape;
    // "The ELEMENT's own facts ride beside the field's" (§3.3): an array's
    // element is the one nesting this wire has.
    if schema_e.kind == 14 || schema_e.kind == 16 {
        let (schema_element, ok) = read_shape(bytes, schema_at, schema_e.shape.elem);
        if !ok {
            return (schema_e, false);
        }
        schema_e.element = schema_element;
    }
    (schema_e, true)
}

/// ONE shape, by the kind that names it (§3.3's shape table). Every number in
/// it is a canonical LEB128 except where the row says otherwise: a RANGED BASE
/// is encoded by its kind's signedness — zigzag for the signed kinds, unsigned
/// for the unsigned kinds, sixteen bytes for the 128-bit and fixed-point kinds —
/// and a QUANTIZED f32 carries min, max and res as float32.
fn read_shape(bytes: &[u8], schema_at: &mut usize, schema_kind: u8) -> (TableMessageShape, bool) {
    let mut schema_s = TableMessageShape::default();
    if integer_kind(schema_kind) || fixed_kind(schema_kind) || schema_kind == 10 {
        if *schema_at >= bytes.len() {
            return (schema_s, false);
        }
        schema_s.packing = bytes[*schema_at];
        *schema_at += 1;
        if schema_s.packing == 0 {
            return (schema_s, true);
        }
        if schema_s.packing == 1 && schema_kind != 10 {
            let (schema_v, ok) = read_leb(bytes, schema_at);
            if !ok || schema_v > kind_bits(schema_kind) {
                return (schema_s, false);
            }
            schema_s.bits = schema_v;
            if schema_kind == 18 || schema_kind == 19 || fixed_kind(schema_kind) {
                if *schema_at + 16 > bytes.len() {
                    return (schema_s, false);
                }
                schema_s.base_lo = read_u64(bytes, *schema_at);
                schema_s.base_hi = read_u64(bytes, *schema_at + 8);
                *schema_at += 16;
                if (20..=23).contains(&schema_kind) {
                    let schema_sblo = schema_s.base_lo as i64;
                    let schema_whi = if schema_sblo < 0 { u64::MAX } else { 0 };
                    if schema_s.base_hi != schema_whi {
                        return (schema_s, false);
                    }
                    if schema_kind < 23 {
                        let schema_dom = kind_bits(schema_kind);
                        let schema_mn = -(1i64 << (schema_dom - 1));
                        let schema_mx = (1i64 << (schema_dom - 1)) - 1;
                        if schema_sblo < schema_mn || schema_sblo > schema_mx {
                            return (schema_s, false);
                        }
                    }
                } else if (25..=28).contains(&schema_kind) {
                    if schema_s.base_hi != 0 {
                        return (schema_s, false);
                    }
                    if schema_kind < 28 {
                        let schema_dom = kind_bits(schema_kind);
                        if schema_s.base_lo > (1u64 << schema_dom) - 1 {
                            return (schema_s, false);
                        }
                    }
                }
                return (schema_s, true);
            }
            let (schema_v, ok) = read_leb(bytes, schema_at);
            if !ok {
                return (schema_s, false);
            }
            if (2..=5).contains(&schema_kind) {
                schema_s.base_lo = (schema_v >> 1) ^ (0u64.wrapping_sub(schema_v & 1));
                if schema_kind < 5 {
                    let schema_dom = kind_bits(schema_kind);
                    let schema_mn = -(1i64 << (schema_dom - 1));
                    let schema_mx = (1i64 << (schema_dom - 1)) - 1;
                    let schema_sblo = schema_s.base_lo as i64;
                    if schema_sblo < schema_mn || schema_sblo > schema_mx {
                        return (schema_s, false);
                    }
                }
            } else {
                schema_s.base_lo = schema_v;
                if schema_kind < 9 {
                    let schema_dom = kind_bits(schema_kind);
                    if schema_s.base_lo > (1u64 << schema_dom) - 1 {
                        return (schema_s, false);
                    }
                }
            }
            return (schema_s, true);
        }
        if schema_s.packing == 2 && schema_kind == 10 {
            if *schema_at + 12 > bytes.len() {
                return (schema_s, false);
            }
            let schema_qmin = f32::from_bits(read_u32(bytes, *schema_at));
            let schema_qmax = f32::from_bits(read_u32(bytes, *schema_at + 4));
            let schema_qres = f32::from_bits(read_u32(bytes, *schema_at + 8));
            *schema_at += 12;
            let (schema_qcount, schema_qdelta, bits, ok) = quantization(schema_qmin, schema_qmax, schema_qres);
            if !ok {
                return (schema_s, false);
            }
            schema_s.qmin = schema_qmin;
            schema_s.qdelta = schema_qdelta;
            schema_s.qcount = schema_qcount;
            schema_s.bits = bits;
            return (schema_s, true);
        }
        return (schema_s, false); // a packing outside the closed set
    }
    if schema_kind == 12 {
        let (schema_v, ok) = read_leb(bytes, schema_at);
        if !ok || schema_v > i32::MAX as u64 {
            return (schema_s, false);
        }
        schema_s.max = schema_v;
        return (schema_s, true);
    }
    if schema_kind == 14 || schema_kind == 16 {
        if schema_kind == 14 {
            let (schema_v, ok) = read_leb(bytes, schema_at);
            if !ok || schema_v > 0xffff_ffff {
                return (schema_s, false);
            }
            schema_s.min = schema_v;
        }
        let (schema_v, ok) = read_leb(bytes, schema_at);
        if !ok || schema_v > 0xffff_ffff || schema_v < schema_s.min {
            return (schema_s, false);
        }
        schema_s.max = schema_v;
        if *schema_at >= bytes.len() {
            return (schema_s, false);
        }
        schema_s.elem = bytes[*schema_at];
        *schema_at += 1;
        // THE ELEMENT KINDS ARE CLOSED, one list per body kind (§3.3): an
        // array of strings rides element kind 12 and is walked, while no
        // declaration produces an array or a keyed body as an element, so
        // those are damage here at the announcement.
        let schema_listed = known_kind(schema_s.elem) && schema_s.elem != 14 && schema_s.elem != 16;
        if !schema_listed {
            return (schema_s, false);
        }
        return (schema_s, true);
    }
    (schema_s, true)
}

/// A strict skip of an UNKNOWN field by its kind (§3.3): the ordinary
/// form-1 framed-field skip rules, the C++ reference's (cpptable) and the Go
/// leg's, one function over the closed kind set. This reader knows only the
/// announcement's own two fields, so a field of any other id is stepped over
/// by its kind: fixed-width kinds by their storage width, a node index and an
/// enum's variant by one LEB128, the framed kinds by one LEB128 length and
/// that many bytes, and a union by its arm reference, its arm's kind byte, one
/// length and that many bytes. A kind outside the set is not skippable, which
/// is why the set is closed; kind 31 is held by name and never written
/// (R17), so it too is damage here (§3).
fn skip_field(
    bytes: &[u8],
    schema_at: &mut usize,
    schema_kind: u8,
    schema_id_count: u64,
    schema_next_ref: &mut u64,
    schema_non_finite: &mut bool,
    schema_depth: u32,
) -> bool {
    if schema_depth > 128 {
        return false;
    }
    match schema_kind {
        1 => {
            if *schema_at >= bytes.len() || bytes[*schema_at] > 1 {
                return false;
            }
            *schema_at += 1;
            true
        }
        2 | 6 | 20 | 25 => skip_width(bytes, schema_at, 1),
        3 | 7 | 21 | 26 => skip_width(bytes, schema_at, 2),
        4 | 8 | 22 | 27 => skip_width(bytes, schema_at, 4),
        10 => {
            if *schema_at + 4 > bytes.len() {
                return false;
            }
            let schema_raw = u32::from_le_bytes([bytes[*schema_at], bytes[*schema_at + 1], bytes[*schema_at + 2], bytes[*schema_at + 3]]);
            *schema_at += 4;
            if (schema_raw & 0x7f80_0000) == 0x7f80_0000 {
                *schema_non_finite = true;
            }
            true
        }
        5 | 9 | 23 | 28 | 36 => skip_width(bytes, schema_at, 8),
        11 => {
            if *schema_at + 8 > bytes.len() {
                return false;
            }
            let schema_raw = read_u64(bytes, *schema_at);
            *schema_at += 8;
            if (schema_raw & 0x7ff0_0000_0000_0000) == 0x7ff0_0000_0000_0000 {
                *schema_non_finite = true;
            }
            true
        }
        18 | 19 | 24 | 29 => skip_width(bytes, schema_at, 16),
        17 => {
            let (schema_idx, ok) = read_leb(bytes, schema_at);
            ok && schema_idx == 0
        }
        30 => {
            let (schema_r, ok) = read_leb(bytes, schema_at);
            if !ok || schema_r > schema_id_count {
                return false;
            }
            if schema_r != 0 {
                if schema_r > *schema_next_ref {
                    return false;
                }
                if schema_r == *schema_next_ref {
                    *schema_next_ref += 1;
                }
            }
            true
        }
        12 => {
            let (schema_n, ok) = read_leb(bytes, schema_at);
            if !ok || schema_n > (bytes.len() - *schema_at) as u64 {
                return false;
            }
            let schema_s = &bytes[*schema_at..*schema_at + schema_n as usize];
            *schema_at += schema_n as usize;
            !schema_s.contains(&0) && core::str::from_utf8(schema_s).is_ok()
        }
        32 => {
            let (schema_n, ok) = read_leb(bytes, schema_at);
            ok && schema_n == 0
        }
        13 => {
            if schema_depth >= 128 {
                return false;
            }
            let (schema_n, ok) = read_leb(bytes, schema_at);
            if !ok || schema_n > (bytes.len() - *schema_at) as u64 {
                return false;
            }
            let schema_sub = &bytes[*schema_at..*schema_at + schema_n as usize];
            *schema_at += schema_n as usize;
            let mut schema_sat = 0usize;
            let mut schema_seen = [0u64; ID_TABLE_PROBE_BOUND / 64];
            loop {
                let (schema_r, schema_rok) = read_leb(schema_sub, &mut schema_sat);
                if !schema_rok {
                    return false;
                }
                if schema_r == 0 {
                    return schema_sat == schema_sub.len();
                }
                if schema_r > schema_id_count || schema_r > *schema_next_ref || schema_sat >= schema_sub.len() {
                    return false;
                }
                if schema_r == *schema_next_ref {
                    *schema_next_ref += 1;
                }
                let schema_ridx = (schema_r - 1) as usize;
                let schema_rbit = 1u64 << (schema_ridx & 63);
                if (schema_seen[schema_ridx >> 6] & schema_rbit) != 0 {
                    return false;
                }
                schema_seen[schema_ridx >> 6] |= schema_rbit;
                let schema_k = schema_sub[schema_sat];
                schema_sat += 1;
                if !skip_field(schema_sub, &mut schema_sat, schema_k, schema_id_count, schema_next_ref, schema_non_finite, schema_depth + 1) {
                    return false;
                }
            }
        }
        14 | 16 => {
            if schema_depth >= 128 {
                return false;
            }
            let (schema_n, ok) = read_leb(bytes, schema_at);
            if !ok || schema_n > (bytes.len() - *schema_at) as u64 {
                return false;
            }
            let schema_sub = &bytes[*schema_at..*schema_at + schema_n as usize];
            *schema_at += schema_n as usize;
            skip_collection_body(schema_sub, schema_kind, schema_id_count, schema_next_ref, schema_non_finite, schema_depth + 1)
        }
        15 => {
            let (schema_arm, ok) = read_leb(bytes, schema_at);
            if !ok {
                return false;
            }
            if schema_arm == 0 {
                return true;
            }
            if schema_arm > schema_id_count || schema_arm > *schema_next_ref || *schema_at >= bytes.len() {
                return false;
            }
            if schema_arm == *schema_next_ref {
                *schema_next_ref += 1;
            }
            let schema_ak = bytes[*schema_at];
            *schema_at += 1;
            let (schema_n, ok) = read_leb(bytes, schema_at);
            if !ok || schema_n > (bytes.len() - *schema_at) as u64 {
                return false;
            }
            let schema_sub = &bytes[*schema_at..*schema_at + schema_n as usize];
            *schema_at += schema_n as usize;
            if schema_ak == 12 {
                !schema_sub.contains(&0) && core::str::from_utf8(schema_sub).is_ok()
            } else if schema_ak == 32 {
                schema_n == 0
            } else if schema_ak == 13 {
                if schema_depth >= 128 {
                    return false;
                }
                let mut schema_sat = 0usize;
                let mut schema_seen = [0u64; ID_TABLE_PROBE_BOUND / 64];
                loop {
                    let (schema_r, schema_rok) = read_leb(schema_sub, &mut schema_sat);
                    if !schema_rok {
                        return false;
                    }
                    if schema_r == 0 {
                        return schema_sat == schema_sub.len();
                    }
                    if schema_r > schema_id_count || schema_r > *schema_next_ref || schema_sat >= schema_sub.len() {
                        return false;
                    }
                    if schema_r == *schema_next_ref {
                        *schema_next_ref += 1;
                    }
                    let schema_ridx = (schema_r - 1) as usize;
                    let schema_rbit = 1u64 << (schema_ridx & 63);
                    if (schema_seen[schema_ridx >> 6] & schema_rbit) != 0 {
                        return false;
                    }
                    schema_seen[schema_ridx >> 6] |= schema_rbit;
                    let schema_k = schema_sub[schema_sat];
                    schema_sat += 1;
                    if !skip_field(schema_sub, &mut schema_sat, schema_k, schema_id_count, schema_next_ref, schema_non_finite, schema_depth + 1) {
                        return false;
                    }
                }
            } else if schema_ak == 14 || schema_ak == 16 {
                skip_collection_body(schema_sub, schema_ak, schema_id_count, schema_next_ref, schema_non_finite, schema_depth + 1)
            } else {
                let mut schema_sat = 0usize;
                skip_field(schema_sub, &mut schema_sat, schema_ak, schema_id_count, schema_next_ref, schema_non_finite, schema_depth) && schema_sat == schema_sub.len()
            }
        }
        // a kind outside the closed set is not skippable (§3)
        _ => false,
    }
}

fn skip_collection_body(
    schema_sub: &[u8],
    schema_kind: u8,
    schema_id_count: u64,
    schema_next_ref: &mut u64,
    schema_non_finite: &mut bool,
    schema_depth: u32,
) -> bool {
    if schema_depth > 128 || schema_sub.len() < 2 {
        return false;
    }
    let schema_ek = schema_sub[0];
    let mut schema_sat = 1usize;
    let (schema_cnt, schema_cok) = read_leb(schema_sub, &mut schema_sat);
    if !schema_cok || schema_cnt > 0x7fff_ffff {
        return false;
    }
    let schema_valid_ek = if schema_kind == 14 {
        (1..=13).contains(&schema_ek) || schema_ek == 15 || schema_ek == 17 || (18..=29).contains(&schema_ek) || schema_ek == 30 || schema_ek == 36
    } else {
        (1..=11).contains(&schema_ek) || schema_ek == 13 || (18..=29).contains(&schema_ek) || schema_ek == 30 || schema_ek == 36
    };
    if !schema_valid_ek {
        return false;
    }
    let mut schema_seen_keys = [0u64; ID_TABLE_PROBE_BOUND / 64];
    for _ in 0..schema_cnt {
        if schema_kind == 16 {
            let (schema_kr, schema_kok) = read_leb(schema_sub, &mut schema_sat);
            if !schema_kok || schema_kr == 0 || schema_kr > schema_id_count || schema_kr > *schema_next_ref {
                return false;
            }
            if schema_kr == *schema_next_ref {
                *schema_next_ref += 1;
            }
            let schema_kidx = (schema_kr - 1) as usize;
            let schema_kbit = 1u64 << (schema_kidx & 63);
            if (schema_seen_keys[schema_kidx >> 6] & schema_kbit) != 0 {
                return false;
            }
            schema_seen_keys[schema_kidx >> 6] |= schema_kbit;
            let (schema_el, schema_lok) = read_leb(schema_sub, &mut schema_sat);
            if !schema_lok || schema_el > (schema_sub.len() - schema_sat) as u64 {
                return false;
            }
            let schema_esub = &schema_sub[schema_sat..schema_sat + schema_el as usize];
            schema_sat += schema_el as usize;
            let mut schema_eat = 0usize;
            if schema_ek == 13 {
                if schema_depth >= 128 {
                    return false;
                }
                let mut schema_bseen = [0u64; ID_TABLE_PROBE_BOUND / 64];
                loop {
                    let (schema_r, schema_rok) = read_leb(schema_esub, &mut schema_eat);
                    if !schema_rok {
                        return false;
                    }
                    if schema_r == 0 {
                        if schema_eat != schema_esub.len() {
                            return false;
                        }
                        break;
                    }
                    if schema_r > schema_id_count || schema_r > *schema_next_ref || schema_eat >= schema_esub.len() {
                        return false;
                    }
                    if schema_r == *schema_next_ref {
                        *schema_next_ref += 1;
                    }
                    let schema_ridx = (schema_r - 1) as usize;
                    let schema_rbit = 1u64 << (schema_ridx & 63);
                    if (schema_bseen[schema_ridx >> 6] & schema_rbit) != 0 {
                        return false;
                    }
                    schema_bseen[schema_ridx >> 6] |= schema_rbit;
                    let schema_k = schema_esub[schema_eat];
                    schema_eat += 1;
                    if !skip_field(schema_esub, &mut schema_eat, schema_k, schema_id_count, schema_next_ref, schema_non_finite, schema_depth + 1) {
                        return false;
                    }
                }
            } else if !skip_field(schema_esub, &mut schema_eat, schema_ek, schema_id_count, schema_next_ref, schema_non_finite, schema_depth) || schema_eat != schema_esub.len() {
                return false;
            }
        } else if !skip_field(schema_sub, &mut schema_sat, schema_ek, schema_id_count, schema_next_ref, schema_non_finite, schema_depth) {
            return false;
        }
    }
    schema_sat == schema_sub.len()
}

/// Skip a fixed-width payload of WIDTH bytes, or refuse past the buffer.
fn skip_width(bytes: &[u8], schema_at: &mut usize, schema_width: usize) -> bool {
    if *schema_at + schema_width > bytes.len() {
        return false;
    }
    *schema_at += schema_width;
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
    /// the same rule as the packet wire's, SPEC.md §4.3): every read refuses one, the strict read included, and every writer
    /// refuses to emit one. The report names the field in non_finite_float.
    NonFiniteFloat,
    JsonRootNotArray,
    TimestampNotRfc3339,
    IntegrityMismatch,
    MissingRequired,
    OutOfRange,
    KindMismatch,
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
            TableReason::JsonRootNotArray => "json_root_not_array",
            TableReason::TimestampNotRfc3339 => "timestamp_not_rfc3339",
            TableReason::IntegrityMismatch => "integrity_mismatch",
            TableReason::MissingRequired => "missing_required",
            TableReason::OutOfRange => "out_of_range",
            TableReason::KindMismatch => "kind_mismatch",
        }
    }
}

/// THE ONE READ REPORT (docs/SPEC-TABLES.md §4, §5, §3.3): the variable form's
/// strict read and the message form's batch fill the same type, so a crate
/// carrying both forms exports one TableReport and never two.
///
/// STRICT-ONLY (docs/plans/strict-only.md): a read returns the whole value or
/// refuses the whole load. A field this build cannot name is skipped and
/// counted unknown, a kind that GREW since the writer decodes exactly and
/// counts widened (§4), and an absent field reads its default. A kind
/// mismatch, a value out of range, a count or length past its bound, a
/// repeated key or id and a shared blob each refuse the load as damaged.
/// refused is a verdict by name: the form byte this reader does not carry
/// (§3), or one of the message form's refusals, whose reason it records.
#[derive(Clone, PartialEq, Eq, Debug, Default)]
pub struct TableReport {
    pub unknown: u64,
    pub widened: u64,
    /// The first field a read met out of its declared range or bound, a
    /// diagnostic beside the refusal, never compared (§6.5).
    pub oor_field: &'static str,
    pub damaged: bool,
    pub refused: bool,
    /// The read met a pointer naming an OPEN node, a cycle no writer produces
    /// (§3.1): the load refuses whole, and a builder save refuses it as data_cycle.
    pub cycle_on_save: bool,
    /// RETAIN-UNKNOWN'S TWO COUNTERS (docs/SPEC-TABLES.md §6.6): retained
    /// counts the unknown fields whose bytes were kept, retain_lost counts
    /// every unknown this load or save could not keep (capacity, an excluded
    /// class, or a damaged resolving walk). Both are zero in every read that
    /// did not opt in, and both ride the one report type for the reason
    /// repeated does (§4).
    pub retained: u64,
    pub retain_lost: u64,
    /// The message form's refusal by name and the two build versions its
    /// strict check compared (§3.3); None and zero on the variable form.
    pub reason: TableReason,
    pub build_version_remote: u64,
    pub build_version_own: u64,
    /// THE FIRST NON-FINITE FLOAT A READ MET (docs/SPEC-TABLES.md §3.3): its
    /// <Table>.<field>, and "" when there was none. A read that names one
    /// refuses as non_finite_float and lands nothing, the strict read
    /// included. For a RETAINED UNKNOWN float (§6.6) it is the body's table
    /// and non_finite_float_id is the id the reader cannot name.
    pub non_finite_float: &'static str,
    pub non_finite_float_id: Option<u64>,
    /// A named text-form diagnostic, empty after a clean read.
    pub text_error: &'static str,
    /// Bodies refused because an active required field was absent (§4).
    pub missing_required: u64,
    /// The current refusal's names, in field declaration order.
    pub missing_required_fields: String,
}

impl TableReport {
    pub fn missing_required_reason(&self) -> Option<String> {
        if self.missing_required_fields.is_empty() { None }
        else { Some(format!("missing_required: {}", self.missing_required_fields)) }
    }

    pub fn err(&self) -> Option<TableRefuseReason> {
        match self.reason {
            TableReason::NewerForm | TableReason::MessageFormAsFile | TableReason::UnknownForm => {
                return Some(TableReason::UnknownForm);
            }
            TableReason::IntegrityMismatch => return Some(TableReason::IntegrityMismatch),
            _ => {}
        }
        if !self.non_finite_float.is_empty() {
            return Some(TableReason::NonFiniteFloat);
        }
        if self.damaged || self.cycle_on_save {
            return Some(TableReason::WireDamaged);
        }
        if self.missing_required > 0 {
            return Some(TableReason::MissingRequired);
        }
        if self.refused {
            return Some(match self.reason {
                TableReason::None | TableReason::Ok => TableReason::UnknownForm,
                named => named,
            });
        }
        None
    }

    pub fn refuse(&mut self, reason: TableReason) -> bool {
        self.damaged = false;
        self.refused = true;
        self.reason = reason;
        false
    }

    /// Names a float whose raw bits are a NaN or an infinity (§3.3). The first
    /// name a read records wins.
    pub fn non_finite(&mut self, schema_field: &'static str) {
        if self.non_finite_float.is_empty() {
            self.non_finite_float = schema_field;
        }
    }

    /// Names a value read outside its declared range, a bits(N) value past
    /// its width (§3.3). The read stops as on damage, and the message batch
    /// refuses BY NAME as out_of_range. THE FIRST REFUSAL WINS, as in the Go
    /// reference: a range miss after damage this read already met stays
    /// damage, and the first name recorded is the one kept.
    pub fn out_of_range(&mut self, schema_field: &'static str) {
        if self.oor_field.is_empty() && !self.damaged {
            self.oor_field = schema_field;
        }
        self.damaged = true;
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
