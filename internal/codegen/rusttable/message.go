package rusttable

// The MESSAGE FORM (form byte 2, docs/SPEC-TABLES.md §3.3): a batch of
// bitpacked bodies under one announced vocabulary. It is a fixed table's wire
// and nobody else's, and this leg carries it beside the fixed form (form 3)
// the same way the C++ reference does: the announcement is a compile-time
// constant of the unit, the vocabulary a receiver reads one announcement into,
// and the batch is the primitive the three verbs move.
//
// The bit stream is the packet wire's own order (bit i in byte i/8, low bit
// first), so a value written here and a value written by a generated packet
// writer are the same bits in the same places.

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageRuntimeModule is the shared module a unit with a fixed root grows for
// the message form, beside the fixed runtime.
const MessageRuntimeModule = "message_runtime"

// messageRuntimeModule emits the shared, unit-independent half of the message
// form: the bit writer and reader, the announced entry and the shape parse,
// the announcement read, the generic skip, and the batch's own open and close.
func messageRuntimeModule(u *ir.Unit) []byte {
	return []byte(header(runtimeSourceName(u), u.Package,
		"THE MESSAGE FORM's shared runtime (docs/SPEC-TABLES.md §3.3)") + messageRuntimeBody)
}

// messageRuntimeBody is the whole runtime as a fixed string, byte-identical in
// every unit, because the message form's shared runtime is named by the package
// and not by what a unit declares (docs/SPEC-TABLES.md §19.2).
const messageRuntimeBody = `//
// The message form's shared runtime, emitted once per unit that carries a
// fixed root.

#![allow(clippy::needless_range_loop)]
#![allow(clippy::too_many_arguments)]

/// The form byte. Form 2 is the MESSAGE FORM (docs/SPEC-TABLES.md §3.3).
pub const TABLE_MESSAGE_FORM: u8 = 2;

/// THE COUNT IS A RANGED INTEGER OVER [1, 256], eight bits carrying M - 1. 256
/// is a WIRE CONSTANT of this form, not a receiver's policy, because the
/// count's WIDTH depends on it and two peers that disagreed on the width would
/// not be reading the same wire. A batch of zero is not spellable.
pub const TABLE_MESSAGE_BATCH_MAX: usize = 256;

/// The RESERVED ids of the announcement's own two fields (§5, §11), beside the
/// node table's. They are the announcement's transport and never appear in a
/// body.
pub const TABLE_BUILD_VERSION_FIELD_ID: u64 = 0xFFFF_FFFF_FFFF_FFFE;
pub const TABLE_MESSAGE_VOCABULARY_FIELD_ID: u64 = 0xFFFF_FFFF_FFFF_FFFD;
pub const TABLE_NODE_TABLE_FIELD_ID: u64 = 0xFFFF_FFFF_FFFF_FFFF;

/// THE WIDEST COUNT THIS FORM SPELLS, which is the count an UNBOUNDED array
/// announces (§2.9).
pub const TABLE_MESSAGE_LIST_MAX: u64 = 0xFFFF_FFFF;

/// The conforming default byte bound of an announced vocabulary (§3.3).
pub const TABLE_MESSAGE_DEFAULT_MAX_BYTES: usize = 64 * 1024;

// ---- the bit stream ---------------------------------------------------------

/// The bit writer. put is ONE WORD AT A TIME, never one bit: the value is
/// shifted into place once and the bytes it occupies are stored from that
/// register, low bit first (docs/SPEC-TABLES.md §3.3, "THE STREAM'S BIT ORDER").
pub struct TableMessageWriter<'a> {
    buffer: &'a mut [u8],
    bits: i64,
    measuring: bool,
    overflow: bool,
}

impl<'a> TableMessageWriter<'a> {
    pub fn measuring() -> Self {
        TableMessageWriter { buffer: &mut [], bits: 0, measuring: true, overflow: false }
    }
    pub fn writing(buffer: &'a mut [u8]) -> Self {
        TableMessageWriter { buffer, bits: 0, measuring: false, overflow: false }
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

/// The bit reader, the mirror of the writer: one word of the buffer at a time.
pub struct TableMessageReader<'a> {
    buffer: &'a [u8],
    offset: i64,
}

impl<'a> TableMessageReader<'a> {
    pub fn new(buffer: &'a [u8]) -> Self {
        TableMessageReader { buffer, offset: 0 }
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
        match self.get((-self.offset) & 7) {
            Some(0) => true,
            _ => false,
        }
    }
}

/// bits_required(min, max), and zero where the two are equal.
pub fn table_message_bits(n: u64) -> i64 {
    let mut count = 0i64;
    let mut v = n;
    while v != 0 {
        count += 1;
        v >>= 1;
    }
    count
}

// ---- the announced entry ----------------------------------------------------

/// The width and range facts a reader needs to SKIP a field exactly and to
/// DECODE one whose own declaration has moved (§3.3). Which members carry
/// meaning is decided by the entry's kind, and a member a kind does not name
/// is zero.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableMessageShape {
    pub kind: u8,
    pub packing: u8,
    pub bits: i64,
    pub base_lo: u64,
    pub base_hi: u64,
    pub min: i64,
    pub max: i64,
    pub qmin: f32,
    pub qdelta: f32,
    pub qcount: u32,
    pub elem_kind: u8,
}

/// ONE announced entry: an id, a kind, and a shape.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableMessageEntry {
    pub id: u64,
    pub shape: TableMessageShape,
    pub element: TableMessageShape,
}

/// ONE DIRECTION's announced vocabulary (§3.3), resolved once. The storage is
/// the caller's and this library never allocates.
#[derive(Clone, Copy, Debug)]
pub struct TableVocabulary<'a> {
    pub entries: &'a mut [TableMessageEntry],
    pub count: usize,
    pub ref_bits: i64,
    pub build_version: u64,
    pub announced: bool,
    pub refused: bool,
    pub max_bytes: usize,
}

impl<'a> TableVocabulary<'a> {
    pub fn init(storage: &'a mut [TableMessageEntry]) -> Self {
        TableVocabulary {
            entries: storage,
            count: 0,
            ref_bits: 0,
            build_version: 0,
            announced: false,
            refused: false,
            max_bytes: TABLE_MESSAGE_DEFAULT_MAX_BYTES,
        }
    }
    pub fn entry(&self, reference: u64) -> Option<&TableMessageEntry> {
        if reference == 0 || reference > self.count as u64 {
            return None;
        }
        self.entries.get(reference as usize - 1)
    }
    /// A reference used as a VALUE: an enum variant, a keyed slot key or a node
    /// type id, which must name a kind-0 entry (§3.3).
    pub fn name(&self, reference: u64) -> Option<u64> {
        let e = self.entry(reference)?;
        if e.shape.kind == 0 && e.id < TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            Some(e.id)
        } else {
            None
        }
    }
}

/// EVERY REFUSAL IS BY NAME (§3.3). A refusal is a verdict, not an event:
/// nothing is decoded, no counter moves and malformed does not fire.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum TableMessageReason {
    #[default]
    None,
    /// a body from a peer that never announced
    NoVocabulary,
    /// M above 256 on the write side, M above capacity on the read side
    BatchTooLarge,
    /// an announcement naming more entries or bytes than the caller declared
    VocabularyTooLarge,
    /// a second announcement, refused by name and terminal
    SecondAnnouncement,
    /// a form byte this reader does not carry
    NewerForm,
    /// a form-2 batch where a FILE was expected
    MessageFormAsFile,
}

impl TableMessageReason {
    pub fn name(&self) -> &'static str {
        match self {
            Self::None => "none",
            Self::NoVocabulary => "no_vocabulary",
            Self::BatchTooLarge => "batch_too_large",
            Self::VocabularyTooLarge => "vocabulary_too_large",
            Self::SecondAnnouncement => "second_announcement",
            Self::NewerForm => "newer_form",
            Self::MessageFormAsFile => "message_form_as_file",
        }
    }
}

/// The read report — §4's ledger and this form's refusals beside it.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableMessageReport {
    pub unknown: u32,
    pub kind_mismatch: u32,
    pub widened: u32,
    pub clamped: u32,
    pub malformed: bool,
    pub refused: bool,
    pub reason: TableMessageReason,
}

impl TableMessageReport {
    pub fn refuse<T>(&mut self, reason: TableMessageReason) -> Option<T> {
        self.unknown = 0;
        self.kind_mismatch = 0;
        self.widened = 0;
        self.clamped = 0;
        self.malformed = false;
        self.refused = true;
        self.reason = reason;
        None
    }
}

// ---- the announcement -------------------------------------------------------

/// The announcement read (§3.3): an ordinary form-1 file whose body carries
/// the build version and the vocabulary under the two reserved ids. The read
/// is tolerant except for exactly two strict checks, and the first
/// announcement is the only one a connection takes.
pub fn announce_read(v: &mut TableVocabulary, data: &[u8], report: &mut TableMessageReport) -> bool {
    if v.announced || v.refused {
        report.refused = true;
        report.reason = TableMessageReason::SecondAnnouncement;
        return false;
    }
    let ok = table_announce_read(v, data, report);
    if !ok {
        v.refused = true;
    }
    ok
}

// table_announce_read parses one announcement. It is self-contained: the
// announcement is a fixed structure, so it needs none of the variable form's
// machinery beyond a canonical LEB128.
fn table_announce_read(v: &mut TableVocabulary, data: &[u8], report: &mut TableMessageReport) -> bool {
    if data.is_empty() {
        report.malformed = true;
        return false;
    }
    if data[0] != 1 {
        report.refused = true;
        report.reason = if data[0] == TABLE_MESSAGE_FORM {
            TableMessageReason::MessageFormAsFile
        } else {
            TableMessageReason::NewerForm
        };
        return false;
    }
    // The trailer: two reserved ids then the entry count, a fixed u64.
    if data.len() < 25 {
        report.malformed = true;
        return false;
    }
    let trailer_count = u64::from_le_bytes(data[data.len() - 8..].try_into().expect("eight bytes"));
    if trailer_count != 2 {
        report.malformed = true;
        return false;
    }
    let end = data.len() - 8 - (trailer_count as usize) * 8;
    let ids = &data[end..end + 16];
    if ids[0..8] != TABLE_BUILD_VERSION_FIELD_ID.to_le_bytes()
        || ids[8..16] != TABLE_MESSAGE_VOCABULARY_FIELD_ID.to_le_bytes()
    {
        report.malformed = true;
        return false;
    }
    // The body: reference (LEB), kind, payload, ending at a zero reference.
    let mut at = 1usize;
    let mut versions = 0usize;
    let mut vocabularies = 0usize;
    let mut words: &[u8] = &[];
    loop {
        let Some(reference) = leb128(data, &mut at) else {
            report.malformed = true;
            return false;
        };
        if reference == 0 {
            break;
        }
        if reference > 2 || at >= end {
            report.malformed = true;
            return false;
        }
        let kind = data[at];
        at += 1;
        match reference {
            1 => {
                if kind != 9 || at + 8 > end {
                    report.malformed = true;
                    return false;
                }
                v.build_version = u64::from_le_bytes(data[at..at + 8].try_into().expect("eight bytes"));
                at += 8;
                versions += 1;
            }
            2 => {
                if kind != 14 {
                    report.malformed = true;
                    return false;
                }
                // An array body: element kind, then a length, then the bytes.
                let Some(framed) = leb128(data, &mut at) else {
                    report.malformed = true;
                    return false;
                };
                let begin = at;
                let fin = begin + framed as usize;
                if begin >= fin || fin > end {
                    report.malformed = true;
                    return false;
                }
                at = fin;
                if data[begin] != 6 {
                    report.malformed = true;
                    return false;
                }
                let mut p = begin + 1;
                let Some(length) = leb128(data, &mut p) else {
                    report.malformed = true;
                    return false;
                };
                if p + length as usize != fin {
                    report.malformed = true;
                    return false;
                }
                if length as usize > v.max_bytes {
                    report.refused = true;
                    report.reason = TableMessageReason::VocabularyTooLarge;
                    return false;
                }
                words = &data[p..p + length as usize];
                vocabularies += 1;
            }
            _ => {
                report.malformed = true;
                return false;
            }
        }
    }
    if versions != 1 || vocabularies != 1 {
        report.malformed = true;
        return false;
    }
    // The entries, resolved once into the caller's storage.
    let mut count = 0usize;
    let mut q = 0usize;
    while q < words.len() {
        if count >= v.entries.len() {
            report.refused = true;
            report.reason = TableMessageReason::VocabularyTooLarge;
            return false;
        }
        let Some(mut e) = table_message_entry_read(words, &mut q) else {
            report.malformed = true;
            return false;
        };
        if e.id == TABLE_BUILD_VERSION_FIELD_ID || e.id == TABLE_MESSAGE_VOCABULARY_FIELD_ID {
            report.malformed = true;
            return false;
        }
        let mut dup = false;
        for i in 0..count {
            if v.entries[i] == e {
                dup = true;
                break;
            }
        }
        if dup {
            report.malformed = true;
            return false;
        }
        v.entries[count] = e;
        count += 1;
        let _ = &mut e;
    }
    v.count = count;
    v.ref_bits = table_message_bits(count as u64);
    if v.ref_bits == 0 {
        v.ref_bits = 1;
    }
    v.announced = true;
    true
}

/// A canonical LEB128, which is the announcement's own integer.
fn leb128(data: &[u8], at: &mut usize) -> Option<u64> {
    let mut value = 0u64;
    let mut shift = 0u32;
    loop {
        if shift >= 64 || *at >= data.len() {
            return None;
        }
        let byte = data[*at];
        *at += 1;
        value |= u64::from(byte & 0x7f) << shift;
        if byte & 0x80 == 0 {
            return Some(value);
        }
        shift += 7;
    }
}

fn table_message_known_kind(kind: u8) -> bool {
    kind <= 33
}

fn table_message_kind_bits(kind: u8) -> i64 {
    match kind {
        1 => 1,
        2 | 6 | 20 | 25 => 8,
        3 | 7 | 21 | 26 => 16,
        4 | 8 | 10 | 22 | 27 => 32,
        5 | 9 | 11 | 23 | 28 => 64,
        18 | 19 | 24 | 29 => 128,
        _ => -1,
    }
}

/// One entry's shape, by the kind that names it (§3.3's shape table).
fn table_message_shape_read(data: &[u8], at: &mut usize, kind: u8) -> Option<TableMessageShape> {
    let mut s = TableMessageShape { kind, bits: table_message_kind_bits(kind), ..Default::default() };
    let integer = (2..=9).contains(&kind) || (18..=29).contains(&kind);
    if integer || kind == 10 {
        if *at >= data.len() {
            return None;
        }
        s.packing = data[*at];
        *at += 1;
        if s.packing == 0 {
            return Some(s);
        }
        if s.packing == 1 && kind != 10 {
            let bits = leb128(data, at)?;
            if bits > s.bits as u64 {
                return None;
            }
            s.bits = bits as i64;
            if kind >= 18 {
                if *at + 16 > data.len() {
                    return None;
                }
                s.base_lo = u64::from_le_bytes(data[*at..*at + 8].try_into().expect("eight bytes"));
                s.base_hi = u64::from_le_bytes(data[*at + 8..*at + 16].try_into().expect("eight bytes"));
                *at += 16;
            } else {
                let base = leb128(data, at)?;
                if (2..=5).contains(&kind) {
                    s.base_lo = (base >> 1) ^ (base & 1).wrapping_neg();
                } else {
                    s.base_lo = base;
                }
            }
            return Some(s);
        }
        if s.packing == 2 && kind == 10 {
            if *at + 12 > data.len() {
                return None;
            }
            let lo = f32::from_bits(u32::from_le_bytes(data[*at..*at + 4].try_into().expect("four bytes")));
            let hi = f32::from_bits(u32::from_le_bytes(data[*at + 4..*at + 8].try_into().expect("four bytes")));
            let res = f32::from_bits(u32::from_le_bytes(data[*at + 8..*at + 12].try_into().expect("four bytes")));
            *at += 12;
            if !(lo < hi) || !(res > 0.0) {
                return None;
            }
            let delta = hi - lo;
            let mut values = delta / res;
            if !(delta - delta == 0.0) || !(values - values == 0.0) {
                return None;
            }
            if values < 1.0 {
                values = 1.0;
            } else if values > 4294967040.0 {
                values = 4294967040.0;
            }
            s.qmin = lo;
            s.qdelta = delta;
            s.qcount = values.ceil() as u32;
            s.bits = table_message_bits(s.qcount as u64);
            return Some(s);
        }
        return None;
    }
    if kind == 12 || kind == 33 {
        let max = leb128(data, at)?;
        if max > i32::MAX as u64 {
            return None;
        }
        s.max = max as i64;
        return Some(s);
    }
    if kind == 14 || kind == 16 {
        if kind == 14 {
            let min = leb128(data, at)?;
            if min > u32::MAX as u64 {
                return None;
            }
            s.min = min as i64;
        }
        let max = leb128(data, at)?;
        if max > u32::MAX as u64 || (max as i64) < s.min {
            return None;
        }
        s.max = max as i64;
        if *at >= data.len() {
            return None;
        }
        s.elem_kind = data[*at];
        *at += 1;
        if !table_message_known_kind(s.elem_kind) || s.elem_kind == 12 || s.elem_kind == 33 {
            return None;
        }
    }
    Some(s)
}

/// One announced entry: the id, the kind, then the shape.
fn table_message_entry_read(data: &[u8], at: &mut usize) -> Option<TableMessageEntry> {
    if *at + 9 > data.len() {
        return None;
    }
    let id = u64::from_le_bytes(data[*at..*at + 8].try_into().expect("eight bytes"));
    let kind = data[*at + 8];
    *at += 9;
    if !table_message_known_kind(kind) {
        return None;
    }
    let shape = table_message_shape_read(data, at, kind)?;
    let mut e = TableMessageEntry { id, shape, element: TableMessageShape::default() };
    if kind == 14 || kind == 16 {
        let elem_kind = shape.elem_kind;
        e.element = table_message_shape_read(data, at, elem_kind)?;
    }
    Some(e)
}

// ---- the generic skip -------------------------------------------------------

fn table_message_count(r: &mut TableMessageReader, s: &TableMessageShape) -> Option<u64> {
    let raw = r.get(table_message_bits((s.max - s.min) as u64))?;
    Some(raw + s.min as u64)
}

fn table_message_skip(r: &mut TableMessageReader, v: &TableVocabulary, index_bits: i64, e: &TableMessageEntry, depth: usize) -> bool {
    if depth > 128 {
        return false;
    }
    let s = &e.shape;
    match s.kind {
        0 | 32 => true,
        30 => {
            let reference = r.get(v.ref_bits)?;
            reference == 0 || v.name(reference).is_some()
        }
        13 => table_message_skip_body(r, v, index_bits, depth + 1),
        17 => index_bits > 0 && r.skip(index_bits),
        15 => {
            let reference = r.get(v.ref_bits)?;
            if reference == 0 {
                return true;
            }
            let arm = v.entry(reference).copied();
            match arm {
                Some(a) if a.id < TABLE_MESSAGE_VOCABULARY_FIELD_ID && a.shape.kind != 0 => {
                    table_message_skip(r, v, index_bits, &a, depth + 1)
                }
                _ => false,
            }
        }
        12 | 33 => {
            let n = table_message_count(r, s)?;
            let width: i64 = if s.kind == 12 { 8 } else { 16 };
            if s.kind == 12 && !r.align() {
                return false;
            }
            r.skip(n as i64 * width)
        }
        31 => {
            if !r.align() {
                return false;
            }
            let n = r.get(32)?;
            r.skip(n as i64 * 8)
        }
        14 | 16 => {
            let n = table_message_count(r, s)?;
            if s.kind == 14 && e.element.kind == 6 && !r.align() {
                return false;
            }
            if e.element.bits >= 0 {
                let mut width = e.element.bits;
                if s.kind == 16 {
                    width += v.ref_bits;
                }
                return r.skip(n as i64 * width);
            }
            for _ in 0..n {
                if s.kind == 16 && !r.skip(v.ref_bits) {
                    return false;
                }
                let elem = TableMessageEntry { shape: e.element, ..Default::default() };
                if !table_message_skip(r, v, index_bits, &elem, depth + 1) {
                    return false;
                }
            }
            true
        }
        _ => s.bits >= 0 && r.skip(s.bits),
    }
}

fn table_message_skip_body(r: &mut TableMessageReader, v: &TableVocabulary, index_bits: i64, depth: usize) -> bool {
    if depth > 128 {
        return false;
    }
    loop {
        let reference = r.get(v.ref_bits)?;
        if reference == 0 {
            return true;
        }
        let entry = v.entry(reference).copied();
        match entry {
            Some(e) if table_message_skip(r, v, index_bits, &e, depth) => {}
            _ => return false,
        }
    }
}

// ---- the batch --------------------------------------------------------------

/// Opens a batch: the form byte, the count, and the body stream. A refusal by
/// name returns the reason on the report and a negative count; the wire's M is
/// the returned count on a read-side batch_too_large.
pub fn table_message_batch_open<'a>(
    v: &TableVocabulary,
    data: &'a [u8],
    report: &mut TableMessageReport,
) -> Result<(TableMessageReader<'a>, i64), i64> {
    if data.is_empty() {
        report.malformed = true;
        return Err(-1);
    }
    if data[0] != TABLE_MESSAGE_FORM {
        report.refused = true;
        report.reason = TableMessageReason::NewerForm;
        return Err(-1);
    }
    if !v.announced {
        report.refused = true;
        report.reason = TableMessageReason::NoVocabulary;
        return Err(-1);
    }
    if data.len() < 2 {
        report.malformed = true;
        return Err(-1);
    }
    let count = data[1] as i64 + 1;
    Ok((TableMessageReader::new(&data[2..]), count))
}

/// Closes a batch: the pad to the next byte boundary is verified zero, and no
/// byte may remain after it.
pub fn table_message_batch_close(r: &mut TableMessageReader, report: &mut TableMessageReport) -> bool {
    if !r.align() || r.offset != r.buffer.len() as i64 * 8 {
        report.malformed = true;
        return false;
    }
    true
}

/// The read side of one scalar: the value at the announced width, with the
/// ranged base added back. Returns the low and high halves of up to 128 bits.
pub fn table_message_read_scalar(r: &mut TableMessageReader, s: &TableMessageShape) -> Option<(u64, u64)> {
    let lo = r.get(s.bits.min(64))?;
    let mut hi = 0u64;
    if s.bits > 64 {
        hi = r.get(s.bits - 64)?;
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
        return Some((u32::from(f32::to_bits(scaled + s.qmin)) as u64, 0));
    }
    Some((lo, hi))
}

/// The write side of one scalar: the value minus the ranged base, at the
/// announced width.
pub fn table_message_write_scalar(w: &mut TableMessageWriter, s: &TableMessageShape, mut lo: u64, mut hi: u64) {
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
    w.put(lo, s.bits.min(64));
    if s.bits > 64 {
        w.put(hi, s.bits - 64);
    }
}
`

// ---- the unit's constants ---------------------------------------------------

// messageVocabulary emits the unit's own announcement and vocabulary as
// compile-time constants: the announcement is a constant of the unit (§3.3)
// and the entries are the writer's own reference table.
func (g *gen) messageVocabulary() {
	entries := ir.TableVocabulary(g.unit)
	announcement := ir.TableAnnouncement(g.unit)
	g.pf("/// THIS UNIT'S OWN REFERENCE WIDTH: the bits a writer spends on every\n")
	g.pf("/// reference of every body it writes (§3.3).\n")
	g.pf("pub const TABLE_MESSAGE_REF_BITS_HERE: i64 = %d;\n", ir.TableMessageRefBits(len(entries)))
	g.pf("/// THIS UNIT'S OWN ENTRY COUNT, the capacity a receiver declares when it\n")
	g.pf("/// talks only to peers of this schema (§3.3).\n")
	g.pf("pub const TABLE_MESSAGE_ENTRIES_HERE: usize = %d;\n\n", len(entries))
	g.pf("/// THE UNIT'S ANNOUNCEMENT, byte for byte: an ordinary form-1 file whose\n")
	g.pf("/// body carries the build version and the vocabulary (§3.3).\n")
	g.pf("pub const TABLE_ANNOUNCEMENT: [u8; %d] = [\n", len(announcement))
	for i, by := range announcement {
		if i%12 == 0 {
			g.pf("    ")
		}
		g.pf("0x%02x,", by)
		if i%12 == 11 || i == len(announcement)-1 {
			g.pf("\n")
		} else {
			g.pf(" ")
		}
	}
	g.pf("];\n\n")
	g.pf("/// THIS UNIT'S OWN VOCABULARY, resolved at compile time (§3.3): the\n")
	g.pf("/// entries a writer references and a reader resolves.\n")
	g.pf("pub const TABLE_ENTRIES: [TableMessageEntry; %d] = [\n", len(entries))
	for _, e := range entries {
		g.pf("    %s,\n", messageEntryLiteral(e))
	}
	g.pf("];\n\n")
}

// messageShapeLiteral spells one shape for the emitted entry constant, mirroring
// the Go leg's messageShapeLiteral: the id, kind, packing and the resolved
// width and base a writer and reader need.
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
	return fmt.Sprintf("TableMessageShape{kind:%d,packing:%d,bits:%d,base_lo:0x%x,base_hi:0x%x,min:%d,max:%d,qmin:f32::from_bits(0x%x),qdelta:f32::from_bits(0x%x),qcount:%d,elem_kind:%d}",
		kind, s.Packing, ir.TableMessageValueBits(kind, s), lo, hi, s.Min, s.Max,
		math.Float32bits(s.QMin), math.Float32bits(delta), count, s.Elem)
}

func messageEntryLiteral(e ir.TableVocabularyEntry) string {
	inner := "TableMessageShape::default()"
	if e.Shape.Inner != nil {
		inner = messageShapeLiteral(e.Shape.Elem, *e.Shape.Inner)
	}
	return fmt.Sprintf("TableMessageEntry{id:0x%016x,shape:%s,element:%s}", e.Id, messageShapeLiteral(e.Kind, e.Shape), inner)
}

// ---- the per-root surface ---------------------------------------------------

// messageModule emits <base>_message.rs: the unit's announcement and entries,
// then for every fixed root of this file the batch verbs and their body
// walkers. The message form is a fixed table's and nobody else's (§3.3).
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

// messageSlot is one entry's slot in this unit's vocabulary, counted from 1.
func messageSlot(u *ir.Unit, e ir.TableVocabularyEntry) int {
	for i, v := range ir.TableVocabulary(u) {
		if e.Key() == v.Key() {
			return i + 1
		}
	}
	return 0
}

// nameSlot is a kind-0 entry's slot by its id (a variant name, an arm's table
// name, or a reserved name), counted from 1.
func nameSlot(u *ir.Unit, id uint64) int {
	for i, e := range ir.TableVocabulary(u) {
		if e.Id == id && e.Kind == 0 {
			return i + 1
		}
	}
	return 0
}

// messageRides is the Rust expression that says whether a field is written:
// the message form elides a field at its declared default (§3.3).
func (g *gen) messageRides(f *ir.Field, expr string) string {
	switch {
	case f.Type.Optional:
		return expr + "_present"
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		return expr + "_length > 0"
	case f.Array == ir.ArrayCounted:
		return expr + "_count > 0"
	case isUnionRef(f.Type):
		return expr + ".tag != 0"
	case enumRef(f) != nil:
		return expr + " != 0"
	case f.Type.Kind == ir.TBool:
		if f.HasDefault && f.DefBool {
			return expr
		}
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
// the write side, matching the reader's prefill.
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

// emitMessageWriteBody is one type's bitpacked stores: a straight line of
// references and payloads, by field, ending at the zero reference.
func (g *gen) emitMessageWriteBody(st *ir.Struct) {
	g.pf("/// %s's stores, each a REFERENCE then its PAYLOAD (§3.3).\n", st.Name)
	g.pf("pub fn %s(w: &mut TableMessageWriter, value: &%sRow) -> bool {\n", fn(st.Name, "save_message_body"), st.Name)
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
		hi = fmt.Sprintf("((%s.0 >> 64) as u64)", expr)
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
	g.pf("pub fn %s(r: &mut TableMessageReader, v: &TableVocabulary, value: &mut %sRow, report: &mut TableMessageReport) -> bool {\n", fn(st.Name, "load_message_body"), st.Name)
	g.pf("    loop {\n")
	g.pf("        let reference = match r.get(v.ref_bits) { Some(x) => x, None => { report.malformed = true; return false; } };\n")
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
	g.pf("                if !table_message_skip(r, v, 0, &entry, 0) { report.malformed = true; return false; }\n")
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
		g.pf("%s    %s = Table%s(((hi as u128) << 64) | (lo as u128));\n", ind, expr, i128Name(f))
	case f.Type.Signed && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed):
		g.pf("%s    %s = lo as i64 as %s;\n", ind, expr, cookElemType(f))
	default:
		g.pf("%s    %s = lo as %s;\n", ind, expr, cookElemType(f))
	}
	g.pf("%s}\n", ind)
}

func i128Name(f *ir.Field) string {
	if f.Type.Signed {
		return "I128"
	}
	return "U128"
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
	g.pf("%s    let reference = match r.get(v.ref_bits) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
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
	g.pf("%s    let reference = match r.get(v.ref_bits) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s    if reference == 0 { %s.tag = 0; } else {\n", ind, expr)
	g.pf("%s        let arm = match v.entry(reference) { Some(a) => *a, None => { report.malformed = true; return false; } };\n", ind)
	g.pf("%s        if arm.id >= TABLE_MESSAGE_VOCABULARY_FIELD_ID || arm.shape.kind == 0 { report.malformed = true; return false; }\n", ind)
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
	g.pf("%s            _ => { report.unknown += 1; %s.tag = 0; if !table_message_skip(r, v, 0, &arm, 0) { report.malformed = true; return false; } }\n", ind, expr)
	g.pf("%s        }\n", ind)
	g.pf("%s    }\n", ind)
	g.pf("%s}\n", ind)
}

func (g *gen) emitMessageReadKeyed(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string) {
	g.pf("%s{\n", ind)
	g.pf("%s    let n = match table_message_count(r, &%s) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind, messageShapeLiteral(entry.Kind, entry.Shape))
	g.pf("%s    for _ in 0..n {\n", ind)
	g.pf("%s        let key = match r.get(v.ref_bits) { Some(x) => x, None => { report.malformed = true; return false; } };\n", ind)
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
	g.pf("pub fn %s(values: &[&%s], report: &mut TableMessageReport) -> i64 {\n", fn(n, "measure_messages"), typ)
	g.pf("    if values.len() < 1 || values.len() > TABLE_MESSAGE_BATCH_MAX { report.refuse(TableMessageReason::BatchTooLarge); return -1; }\n")
	g.pf("    let mut w = TableMessageWriter::measuring();\n")
	g.pf("    for value in values { if !%s(&mut w, value) { return -1; } }\n", fn(n, "save_message_body"))
	g.pf("    w.align();\n")
	g.pf("    if w.overflow { return -1; }\n")
	g.pf("    2 + w.bits / 8\n")
	g.pf("}\n\n")
	g.pf("/// %s's batch save: the form byte, the count, then the bodies (§3.3).\n", st.Name)
	g.pf("pub fn %s(values: &[&%s], buffer: &mut [u8], report: &mut TableMessageReport) -> i64 {\n", fn(n, "save_messages"), typ)
	g.pf("    if values.len() < 1 || values.len() > TABLE_MESSAGE_BATCH_MAX { report.refuse(TableMessageReason::BatchTooLarge); return -1; }\n")
	g.pf("    if buffer.len() < 2 { return -1; }\n")
	g.pf("    buffer[0] = TABLE_MESSAGE_FORM;\n")
	g.pf("    buffer[1] = (values.len() - 1) as u8;\n")
	g.pf("    let mut w = TableMessageWriter::writing(&mut buffer[2..]);\n")
	g.pf("    for value in values { if !%s(&mut w, value) { return -1; } }\n", fn(n, "save_message_body"))
	g.pf("    w.align();\n")
	g.pf("    if w.overflow { return -1; }\n")
	g.pf("    2 + w.bits / 8\n")
	g.pf("}\n\n")
	g.pf("/// %s's batch load: reads one buffer into the caller's storage (§3.3).\n", st.Name)
	g.pf("pub fn %s(values: &mut [%s], v: &TableVocabulary, data: &[u8], report: &mut TableMessageReport) -> Option<i64> {\n", fn(n, "load_messages"), typ)
	g.pf("    let mut r = match table_message_batch_open(v, data, report) {\n")
	g.pf("        Ok((r, count)) => {\n")
	g.pf("            if count > values.len() as i64 { report.refuse(TableMessageReason::BatchTooLarge); return Some(count); }\n")
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
