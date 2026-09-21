// wire.nut — THE SQUIRREL LEG'S TABLE-WIRE FOUNDATION (STAGE 0)
//
// One module, no dependencies. Every later row card of the squirrel port
// imports this file and adds its own row; nothing else in squirrel exists
// yet, and nothing else may be built until this lands.
//
// The form this foundation reads and writes is the FIXED FORM, form byte 3
// (docs/SPEC-TABLES.md §3.4), on the header §3's "THE FIRST BYTE" states for
// every form: the form byte at 0, seven reserved zero bytes, the form's
// eight-byte hash at 8, the body at 16. The §3.4 file is that header, then a
// u32 layout length, then the layout, then the records to the end:
//
//     offset  0        form byte 3
//     offsets 1 .. 7   reserved, written zero
//     offsets 8 .. 15  the LAYOUT HASH, fnv1a64 over the layout's bytes
//     offset  16       layout length (u32 LE), then the layout
//                      records, back to back, to the end of the file
//
// THE LAYOUT (§3.4) is the writer's whole type, flattened — the only
// self-description this form has, sent once:
//
//     layout := entry count (u32 LE), entry count ENTRIES
//     entry  := id (u64 LE), kind (u8), constant size (u32 LE), child count (u32 LE)
//
// An entry is SEVENTEEN BYTES, no LEB128 anywhere in a layout, every number
// little-endian. A RECORD is the eight-byte hash, then the values in declared
// order, every field at its bound: no field reference, no kind byte, no
// length, no terminator, no trailer.
//
// THREE SPELLING RULINGS of this port, stated once, here:
//
// - A FLOAT IS ITS IEEE-754 BIT PATTERN (§3): a float of either width rides
//   as the INTEGER pattern, carried bit for bit and never through a
//   conversion. Squirrel holds one float width and no honest cast between
//   the two, and the hardware conversion between them "sets the quiet bit on
//   a signalling NaN and drops a payload the narrower cell would have kept"
//   — so f32At answers the four bytes and f64At the eight, as integers, and
//   the writer stores the same integers back.
//
// - 64-BIT VALUES LIVE IN SQUIRREL'S SIGNED 64-BIT INTEGER. The bits are the
//   fact, equality is bit equality, and format("%x") prints a value whole.
//   A hash or pattern with its top bit set reads back negative and compares
//   true against the literal that spells it.
//
// - REFUSALS ARE NAMED VALUES, NEVER EXCEPTIONS (§3.4): a layout is the one
//   structure a reader must parse before it knows anything at all, and it
//   arrives from an untrusted peer, so every rule it fails refuses under its
//   own name, sets nothing, and moves none of §4's counters. The primitives
//   below trust the caller's offset the way the reference's Get32 does; the
//   reader bounds every offset before it touches one.

local API = {};

// ---------------------------------------------------------------------------
// §3, "THE FIRST BYTE" and §3.4: THE FRAMING, AS CONSTANTS
//
// "A form's header is the form byte, then its eight-byte hash, padded to the
// body's alignment"; "the fixed form does not need the padding and pays it
// anyway". The registry (§3.4) is ordered: 1 the variable form, 2 the message
// form, 3 the fixed form, 4 and 5 reserved by name for the cook and the block.
// ---------------------------------------------------------------------------

API.FORM_VARIABLE <- 1;         // §3: form byte, root body, id table
API.FORM_MESSAGE <- 2;          // §3.3: a batch of bitpacked bodies, announced
API.FORM_FIXED <- 3;            // §3.4: this module's form
API.HEADER_BYTES <- 16;         // the form byte, seven reserved zeros, the hash
API.HASH_AT <- 8;               // the form's eight-byte hash, little-endian
API.LAYOUT_LENGTH_AT <- 16;     // the u32 layout length
API.LAYOUT_AT <- 20;            // the layout begins here
API.ENTRY_COUNT_AT <- 20;       // the layout's own first four bytes
API.ENTRY0_AT <- 24;            // entry k is the seventeen bytes at 24 + 17k
API.ENTRY_BYTES <- 17;          // id, kind, constant size, child count
API.LAYOUT_HEADER_BYTES <- 4;   // the u32 entry count, and nothing else
API.RECORD_HASH_BYTES <- 8;     // every record names its layout again
API.RECORD_MAX_BYTES <- 65536;  // §3.4's bound: past it no reader decodes
API.MAX_DEPTH <- 64;            // a bound on the WALK, not on the wire
API.MIN_FILE_BYTES <- 20;       // the header plus the layout's length word

// §3.4's named refusals, collected so a port can spell them in one place.
API.REASONS <- [
    "previous_form", "message_form_as_file", "newer_form", "no_layout",
    "layout_malformed", "layout_count_mismatch", "layout_kind_unknown",
    "layout_kind_invalid", "layout_size_mismatch", "layout_tree_unclosed",
    "layout_record_too_large", "layout_too_deep",
];

// ---------------------------------------------------------------------------
// §3: THE LITTLE-ENDIAN MOVES
//
// "The wire is neutral ... Little-endian, byte-oriented throughout", and a
// record's every field "rides as its declared storage image, little-endian,
// at its declared storage width" (§3.4). These are the moves every row card
// builds on. `bytes` is a blob or a string; the caller bounds the offset.
// ---------------------------------------------------------------------------

API.u8At <- function(bytes, at) {
    return bytes[at];
};

API.u16At <- function(bytes, at) {
    return bytes[at] | (bytes[at + 1] << 8);
};

API.u32At <- function(bytes, at) {
    return bytes[at] | (bytes[at + 1] << 8) | (bytes[at + 2] << 16) | (bytes[at + 3] << 24);
};

API.u64At <- function(bytes, at) {
    local v = 0;
    for (local i = 0; i < 8; i++) { v = v | (bytes[at + i] << (8 * i)); }
    return v;
};

// §3: "a float rides as its IEEE-754 bit pattern, with NO canonicalisation"
// (kinds 10 and 11) — the pattern read back as an integer, never a conversion.
API.f32At <- function(bytes, at) {
    return API.u32At(bytes, at);
};

API.f64At <- function(bytes, at) {
    return API.u64At(bytes, at);
};

// The write side of the same moves. A pattern in, a pattern out: putF32 and
// putF64 take the IEEE-754 bits as an integer, because that is what f32At and
// f64At answered and §3 forbids anything standing between the two.
API.putU8 <- function(out, at, v) {
    out[at] = v & 0xff;
};

API.putU16 <- function(out, at, v) {
    out[at] = v & 0xff;
    out[at + 1] = (v >> 8) & 0xff;
};

API.putU32 <- function(out, at, v) {
    for (local i = 0; i < 4; i++) { out[at + i] = (v >> (8 * i)) & 0xff; }
};

API.putU64 <- function(out, at, v) {
    for (local i = 0; i < 8; i++) { out[at + i] = (v >> (8 * i)) & 0xff; }
};

API.putF32 <- function(out, at, pattern) {
    API.putU32(out, at, pattern);
};

API.putF64 <- function(out, at, pattern) {
    API.putU64(out, at, pattern);
};

// putU writes `size` bytes (1, 2, 4 or 8) little-endian — the one move the
// writer's value stores use, the size being the layout entry's own.
API.putU <- function(out, at, size, v) {
    for (local i = 0; i < size; i++) { out[at + i] = (v >> (8 * i)) & 0xff; }
};

// ---------------------------------------------------------------------------
// §5: THE NAME ID, AND §3.4: THE LAYOUT HASH
//
// "An entry is fnv1a64(name) and nothing else" — one hash serves every
// vocabulary the wire has — and "the hash is fnv1a64 over the layout's bytes,
// exactly as written", which is the eight bytes every record carries and the
// header's word at 8. The multiply wraps mod 2^64, as fnv1a64 does.
// ---------------------------------------------------------------------------

API.fnv1a64 <- function(name) {
    local h = 0xcbf29ce484222325;
    foreach (c in name) {
        h = h ^ c;
        h = h * 0x100000001b3;
    }
    return h;
};

API.hashOf <- function(bytes, from, to) {
    local h = 0xcbf29ce484222325;
    for (local i = from; i < to; i++) {
        h = h ^ bytes[i];
        h = h * 0x100000001b3;
    }
    return h;
};

// ---------------------------------------------------------------------------
// §4: THE READ REPORT
//
// Six counters, the refusal verdict that is "not one of §4's events and moves
// no counter" (§3), and the reason, read only when refused. Five zero counters
// and a false flag are what a clean read prints too — the verdict is what
// tells them apart.
// ---------------------------------------------------------------------------

API.newReport <- function() {
    return {
        unknown = 0, kind_mismatch = 0, widened = 0, clamped = 0, duplicate = 0,
        malformed = false, refused = false, reason = null,
    };
};

// The reports.txt row shape (testdata/conformance/tables/FORMAT.md):
// "<unknown>,<kind_mismatch>,<widened>,<clamped>,<duplicate>,<malformed>,<verdict>".
API.reportRow <- function(report) {
    return ::format("%d,%d,%d,%d,%d,%s,%s",
        report.unknown, report.kind_mismatch, report.widened, report.clamped,
        report.duplicate, report.malformed ? "true" : "false",
        report.refused ? "refused" : "read");
};

// ---------------------------------------------------------------------------
// §3.4: THE LAYOUT, WRITTEN
//
// "The entries are a pre-order walk of the root's closure, in the writer's
// declared order. Entry 0 is the root, at kind 13, carrying the root's
// constant size and its field count." layoutBytes lays the count and the
// entries down exactly as the spec states them.
// ---------------------------------------------------------------------------

API.layoutBytes <- function(entries) {
    local out = ::blob(API.LAYOUT_HEADER_BYTES + API.ENTRY_BYTES * entries.len());
    API.putU32(out, 0, entries.len());
    for (local k = 0; k < entries.len(); k++) {
        local e = entries[k];
        local at = API.LAYOUT_HEADER_BYTES + k * API.ENTRY_BYTES;
        API.putU64(out, at, e.id);
        out[at + 8] = e.kind & 0xff;
        API.putU32(out, at + 9, e.size);
        API.putU32(out, at + 13, e.children);
    }
    return out;
};

// ---------------------------------------------------------------------------
// §3.4: THE LAYOUT, PARSED — WHAT A READER HOLDS AN UNTRUSTED PEER'S LAYOUT TO
//
// "A layout is the one structure a reader must parse before it knows anything
// at all, and on a stream it came from a stranger. Every rule below runs
// BEFORE A SINGLE RECORD BYTE IS TOUCHED, each is a refusal by its own name
// rather than one word for all of them, and a layout that fails any of them
// sets nothing." There is no cycle rule and there cannot be a cycle: an
// entry's children are the entries that follow it, so a child's index is
// always strictly greater than its parent's.
//
// parseLayout(bytes, at, len) reads the layout whose count sits at `at`; it
// answers { ok, reason } on a refusal, and on success the count, the entries
// — each with the byte offset it occupies inside its parent — the root's
// body size, and the layout's hash. Offsets are a TABLE's running sums, an
// OPTIONAL's present byte plus one, a UNION's tag then its widest arm, an
// ARRAY's element (bare at 0, or 4 past a count — the validation admits
// either reading and the entry carries its facts for the row cards to settle),
// and null for the entries that NAME no bytes: a variant, a keyed array's key
// enum, because "no key rides".
// ---------------------------------------------------------------------------

API.parseLayout <- function(bytes, at, len) {
    // THE RESIDUE: "a layout this reader cannot parse at all — fewer bytes
    // than a header — is layout_malformed".
    if (len < API.LAYOUT_HEADER_BYTES) { return { ok = false, reason = "layout_malformed" }; }

    // 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY, and is not zero.
    local count = API.u32At(bytes, at);
    if (count == 0 || API.LAYOUT_HEADER_BYTES + API.ENTRY_BYTES * count != len) {
        return { ok = false, reason = "layout_count_mismatch" };
    }

    local entries = [];
    for (local k = 0; k < count; k++) {
        local e = at + API.LAYOUT_HEADER_BYTES + k * API.ENTRY_BYTES;
        entries.append({
            id = API.u64At(bytes, e), kind = bytes[e + 8],
            size = API.u32At(bytes, e + 9), children = API.u32At(bytes, e + 13),
            offset = null, stride = null,
        });
    }

    // 2. THE ROOT IS A TABLE, and its size is the record's body.
    local root = entries[0];
    if (root.kind != 13) { return { ok = false, reason = "layout_kind_invalid" }; }
    if (root.size == 0 || root.size > API.RECORD_MAX_BYTES) {
        return { ok = false, reason = "layout_record_too_large" };
    }

    // THE CLOSED KIND SET (§3 plus the one kind this layout adds, 35): "a kind
    // outside the set is not a newer layout of form 3, it is a layout of a
    // form this reader never saw, and it is refused by name rather than
    // stepped over." 31 is §3's body framing escape and 34 is reserved for
    // float16: neither is a kind a declaration spells.
    local function knownKind(kind) {
        return (kind >= 1 && kind <= 30) || kind == 32 || kind == 33 || kind == 35;
    }

    // A LEAF KIND'S ADMITTED SIZES, §3.4's constant-size table. The one
    // exception is the reference's own: a bits(N) field rides at its declared
    // storage width — four bytes for N <= 32, eight above — under the unsigned
    // kind its bit count picks, so kinds 6 and 7 admit four as well as their
    // own width.
    local function isLeafKind(kind) {
        return (kind >= 1 && kind <= 11) || kind == 17 || (kind >= 18 && kind <= 29) || kind == 32;
    }
    local function leafSizeOk(kind, size) {
        if (kind == 1 || kind == 2 || kind == 20 || kind == 25) { return size == 1; }
        if (kind == 3 || kind == 21 || kind == 26) { return size == 2; }
        if (kind == 4 || kind == 8 || kind == 10 || kind == 22 || kind == 27) { return size == 4; }
        if (kind == 5 || kind == 9 || kind == 11 || kind == 23 || kind == 28) { return size == 8; }
        if (kind == 6) { return size == 1 || size == 4; }
        if (kind == 7) { return size == 2 || size == 4; }
        if (kind == 17) { return size == 4; }
        if (kind == 18 || kind == 19 || kind == 24 || kind == 29) { return size == 16; }
        return size == 0; // kind 32: a variant, and an arm that holds nothing
    }

    local why = null;

    // 3..7, the pre-order walk: every kind in the closed set and used as its
    // definition allows, every size the one its kind or its children account
    // for, the record inside 65536, nothing nested past the walk's bound —
    // and the offsets as the walk goes. (The slot is declared before the
    // closure because a squirrel closure cannot see a local being declared by
    // the very statement that creates it — the body's `walk` captures the
    // slot, not the value.)
    local walk = null;
    walk = function(i, depth) {
        if (i < 0 || i >= count) { why = "layout_tree_unclosed"; return -1; }
        if (depth > API.MAX_DEPTH) { why = "layout_too_deep"; return -1; }
        local e = entries[i];
        if (!knownKind(e.kind)) { why = "layout_kind_unknown"; return -1; }
        if (e.size > API.RECORD_MAX_BYTES) { why = "layout_record_too_large"; return -1; }

        // THE CHILDREN FIRST, in the pre-order the layout is written in. The
        // sums are kept in 64 bits "precisely so a u32 that overflows is
        // caught rather than wrapped into a small number that then agrees
        // with a parent".
        local at = i + 1;
        local sum = 0;
        local widest = 0;
        local first = null;
        local second = null;
        for (local k = 0; k < e.children; k++) {
            local used = walk(at, depth + 1);
            if (used < 0) { return -1; }
            local c = entries[at];
            if (k == 0) { first = c; }
            if (k == 1) { second = c; }
            sum = sum + c.size;
            if (c.size > widest) { widest = c.size; }
            if (sum > API.RECORD_MAX_BYTES) { why = "layout_record_too_large"; return -1; }
            at = at + used;
        }

        if (isLeafKind(e.kind)) {
            if (!leafSizeOk(e.kind, e.size)) { why = "layout_size_mismatch"; return -1; }
            if (e.children != 0) { why = "layout_kind_invalid"; return -1; }
            return at - i;
        }

        if (e.kind == 13) { // a TABLE: its size is the sum of its fields', laid at running offsets
            if (sum != e.size) { why = "layout_size_mismatch"; return -1; }
            local run = 0;
            for (local k = 1; k <= e.children; k++) {
                entries[i + k].offset = run;
                run = run + entries[i + k].size;
            }
        } else if (e.kind == 35) { // the OPTIONAL WRAPPER: one present byte in front of a payload that rides whole
            if (e.children != 1) { why = "layout_kind_invalid"; return -1; }
            if (sum + 1 != e.size) { why = "layout_size_mismatch"; return -1; }
            entries[i + 1].offset = 1;
        } else if (e.kind == 14) { // an ARRAY: a whole number of elements, behind a count or not
            if (e.children != 1) { why = "layout_kind_invalid"; return -1; }
            if (first.size == 0) { why = "layout_size_mismatch"; return -1; }
            local bare = (e.size % first.size) == 0;
            local counted = e.size >= 4 && ((e.size - 4) % first.size) == 0;
            if (!bare && !counted) { why = "layout_size_mismatch"; return -1; }
            entries[i + 1].offset = bare ? 0 : 4;
            entries[i + 1].stride = first.size;
        } else if (e.kind == 16) { // an ENUM-KEYED array: the KEY ENUM then the ELEMENT, every slot written, no key riding
            if (e.children != 2) { why = "layout_kind_invalid"; return -1; }
            if (first.kind != 30) { why = "layout_kind_invalid"; return -1; }
            if (second.size == 0 || (e.size % second.size) != 0) { why = "layout_size_mismatch"; return -1; }
            if (e.size / second.size < first.children) { why = "layout_size_mismatch"; return -1; }
            entries[i + 1].offset = null; // the key enum names no bytes
            entries[i + 2].offset = 0;
            entries[i + 2].stride = second.size;
        } else if (e.kind == 15) { // a UNION: the tag at its own width, then the WIDEST arm, every arm overlaying
            if (e.children == 0) { why = "layout_kind_invalid"; return -1; }
            if (e.size <= widest) { why = "layout_size_mismatch"; return -1; }
            local tag = e.size - widest;
            if (tag != 1 && tag != 2 && tag != 4 && tag != 8) { why = "layout_size_mismatch"; return -1; }
            for (local k = 1; k <= e.children; k++) { entries[i + k].offset = tag; }
        } else if (e.kind == 30) { // an ENUM: the ordinal's storage width, its children VARIANTS that name no bytes
            if (e.size != 1 && e.size != 2 && e.size != 4 && e.size != 8) { why = "layout_size_mismatch"; return -1; }
            for (local k = 1; k <= e.children; k++) {
                if (entries[i + k].kind != 32) { why = "layout_kind_invalid"; return -1; }
                entries[i + k].offset = null;
            }
        } else if (e.kind == 12) { // string(N): the length in bytes, then N bytes
            if (e.children != 0) { why = "layout_kind_invalid"; return -1; }
            if (e.size < 4) { why = "layout_size_mismatch"; return -1; }
        } else if (e.kind == 33) { // wstring(N): the length in code units, then 2N bytes
            if (e.children != 0) { why = "layout_kind_invalid"; return -1; }
            if (e.size < 4 || ((e.size - 4) % 2) != 0) { why = "layout_size_mismatch"; return -1; }
        }
        return at - i;
    }

    local used = walk(0, 0);
    if (why != null) { return { ok = false, reason = why }; }

    // 4. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES: the tree
    // closes and the layout has nothing left over.
    if (used != count) { return { ok = false, reason = "layout_tree_unclosed" }; }

    root.offset = 0;
    return {
        ok = true, count = count, entries = entries, rootBody = root.size,
        hash = API.hashOf(bytes, at, at + len),
    };
};

// ---------------------------------------------------------------------------
// §3.4: FIELDS BY NAME AND OFFSET
//
// "Entry 0 carries the root type's name id, and every other entry carries the
// name id of the field, variant or arm it describes" — and the name's id is
// §5's fnv1a64, so a caller finds its field BY NAME and reads it at the byte
// offset the layout's walk settled.
// ---------------------------------------------------------------------------

API.fieldById <- function(layout, id) {
    foreach (e in layout.entries) {
        if (e.id == id) { return e; }
    }
    return null;
};

API.fieldByName <- function(layout, name) {
    return API.fieldById(layout, API.fnv1a64(name));
};

// ---------------------------------------------------------------------------
// §3.4: THE READER — OPEN BYTES
//
// The form byte is READ FIRST (§3), "before any body and before any trailer,
// so a file that is both an unknown form and damaged is a refusal" — and a
// fixed reader names the DIRECTION: form 1 is the VARIABLE form, which is
// OLDER, so it answers previous_form and not newer_form; form 2 is a batch
// where a file was expected; a byte no form defines or reserves is the only
// byte newer_form is honest for.
//
// Then the layout's own rules, each under its own name. Then the header's
// hash, CHECKED LAST of the three, "so that a broken layout is never reported
// as a lying header". Then the records: "the records fill the rest of the file
// and there is no count ... bytes left over are malformed", and every record
// carries its own eight-byte hash, a record whose hash names no layout this
// reader holds refused by name as no_layout.
//
// A refusal answers { refused = true, reason = <name>, report } with every
// counter unmoved; damage answers report.malformed and no refusal. Nothing
// throws and nothing is swallowed.
// ---------------------------------------------------------------------------

API.open <- function(bytes) {
    local n = bytes.len();
    local report = API.newReport();

    local function refuse(reason) {
        report.refused = true;
        report.reason = reason;
        return { refused = true, reason = reason, report = report, hash = null, layout = null, records = null };
    }

    if (n < 1) { return refuse("layout_malformed"); } // no bytes at all: fewer than a header
    local form = bytes[0];
    if (form != API.FORM_FIXED) {
        if (form == API.FORM_VARIABLE) { return refuse("previous_form"); }
        if (form == API.FORM_MESSAGE) { return refuse("message_form_as_file"); }
        return refuse("newer_form");
    }
    if (n < API.MIN_FILE_BYTES) { return refuse("layout_malformed"); } // truncated: no room for the layout's length word

    local layoutLen = API.u32At(bytes, API.LAYOUT_LENGTH_AT);
    if (API.LAYOUT_AT + layoutLen > n) { return refuse("layout_malformed"); } // the layout runs past the file

    local layout = API.parseLayout(bytes, API.LAYOUT_AT, layoutLen);
    if (!layout.ok) { return refuse(layout.reason); }

    local hash = layout.hash;
    // CHECKED LAST of the three (§3.4): the eight bytes at 8 are the hash of
    // the layout behind them, and this is what proves they are READ and not
    // merely written.
    if (API.u64At(bytes, API.HASH_AT) != hash) { return refuse("layout_malformed"); }

    local recordBytes = API.RECORD_HASH_BYTES + layout.rootBody;
    local rest = n - API.LAYOUT_AT - layoutLen;
    if ((rest % recordBytes) != 0) {
        report.malformed = true; // bytes left over: the two ends of the file have met
        return { refused = false, reason = null, report = report, hash = hash, layout = layout, records = null };
    }

    local records = [];
    local at = API.LAYOUT_AT + layoutLen;
    for (local r = 0; r < rest / recordBytes; r++) {
        if (API.u64At(bytes, at) != hash) { return refuse("no_layout"); }
        records.append({ at = at, body = at + API.RECORD_HASH_BYTES });
        at = at + recordBytes;
    }
    return { refused = false, reason = null, report = report, hash = hash, layout = layout, records = records, recordBytes = recordBytes };
};

// ---------------------------------------------------------------------------
// §3.4: THE WRITER — LAYOUT + FIELDS, AS BYTES
//
// "The writer is the type's constant bytes memcpy'd, and then value stores at
// constant offsets. The template is a compile-time constant of the type: the
// hash in the first eight bytes and zero everywhere a value lands, which is
// also what zero-fills every byte of declared slack without the writer
// touching it."
//
// writeRecord lays one record: the layout's hash, then the template's zeros,
// then each named field's value stored at its byte offset at the entry's own
// width. A float arrives as its IEEE-754 pattern and is stored as the pattern
// (§3). The write side makes no checks a release build must keep: a name the
// layout cannot place leaves the template's zero, which is the elision side
// of the same byte.
// ---------------------------------------------------------------------------

API.writeRecord <- function(layout, fields) {
    local out = ::blob(API.RECORD_HASH_BYTES + layout.rootBody); // zero-filled: the template
    API.putU64(out, 0, layout.hash);
    foreach (name, value in fields) {
        local e = API.fieldByName(layout, name);
        if (e != null && e.children == 0 && e.offset != null
            && (e.size == 1 || e.size == 2 || e.size == 4 || e.size == 8)) {
            API.putU(out, API.RECORD_HASH_BYTES + e.offset, e.size, value);
        }
    }
    return out;
};

// saveFile writes the whole §3.4 file: the form byte, seven reserved zeros,
// the layout's fnv1a64 at 8, the u32 layout length at 16, the layout, then
// the records back to back to the end. An unsavable layout answers null —
// the reference's Save answers -1 — and no target panics and none throws.
API.saveFile <- function(layoutBytes, fieldSets) {
    local layout = API.parseLayout(layoutBytes, 0, layoutBytes.len());
    if (!layout.ok) { return null; }
    local out = ::blob(API.LAYOUT_AT + layoutBytes.len() + fieldSets.len() * (API.RECORD_HASH_BYTES + layout.rootBody));
    out[0] = API.FORM_FIXED; // the reserved seven bytes are the template's zeros already
    API.putU64(out, API.HASH_AT, layout.hash);
    API.putU32(out, API.LAYOUT_LENGTH_AT, layoutBytes.len());
    for (local i = 0; i < layoutBytes.len(); i++) { out[API.LAYOUT_AT + i] = layoutBytes[i]; }
    local at = API.LAYOUT_AT + layoutBytes.len();
    foreach (fields in fieldSets) {
        local record = API.writeRecord(layout, fields);
        for (local i = 0; i < record.len(); i++) { out[at + i] = record[i]; }
        at = at + record.len();
    }
    return out;
};

// ---------------------------------------------------------------------------
// THE DRIVER CONTRACT'S CURRENCY (test/conformance/README.md): every path in
// the manifest is repo-relative and the working directory is the repository
// root, so a driver — and this test — reads a fixture in one move.
// ---------------------------------------------------------------------------

API.readFile <- function(path) {
    local f = ::file(path, "rb");
    local n = f.len();
    local b = f.readblob(n);
    f.close();
    return b;
};

return API;
