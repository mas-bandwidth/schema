// THE MESSAGE FORM's C++ runtime (docs/SPEC-TABLES.md §3.3): the bit stream a
// bitpacked body rides on, the unit's announcement as a compile-time constant,
// the vocabulary a receiver reads it into, and the BATCH that is the form's
// primitive.
//
// A file carries its own id table and a message stream announces one and then
// carries none, and the body itself is BITPACKED: references at the width the
// vocabulary needs, values at the widths their declarations state, no kind
// byte, no length, and no alignment inside a body or between bodies.
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableMessageForm emits the shared, per-package half of the message form.
//
// THE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT and the C++
// reference emits it as one, which §3.3 licenses in so many words: every byte
// of it is settled by the compiler, so a walk would compute at run time what
// the emitter already knows. Announce is a copy and AnnounceMeasure is a
// constant.
func tableMessageForm(u *ir.Unit, forceInline string, anyVariable bool) string {
	announcement := ir.TableAnnouncement(u)
	entries := ir.TableVocabulary(u)

	var b strings.Builder
	fmt.Fprintf(&b, `// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): a batch of BITPACKED bodies
// under one announced vocabulary.
//
// A form 2 wire is THREE PARTS: the form byte, the body count, and the bodies
// as one continuous bit stream, zero-padded to the next byte at the end and
// nowhere else. A body is a sequence of fields, each a REFERENCE followed by a
// PAYLOAD and nothing else: no kind byte and no length, because the
// announcement carries the kind and the shape of every entry.
const uint8_t kTableWireMessageForm = 2;

// THE COUNT IS A RANGED INTEGER OVER [1, 256], eight bits carrying M - 1. 256
// is a WIRE CONSTANT of this form rather than a receiver's policy, because the
// count's WIDTH depends on it and two peers that disagreed on the width would
// not be reading the same wire. A batch of zero is not spellable.
static const int64_t kTableMessageBatchMax = 256;

// The RESERVED ids of the announcement's own two fields (§5, §11), beside the
// node table's. They are the announcement's transport, they never appear in a
// body, and they take no slot in the vocabulary.
static const uint64_t kTableBuildVersionFieldId = 0xFFFFFFFFFFFFFFFEull;
static const uint64_t kTableMessageVocabularyFieldId = 0xFFFFFFFFFFFFFFFDull;

// THIS UNIT'S OWN REFERENCE WIDTH: the bits a writer spends on every reference
// of every body it writes, which is a compile-time constant because the
// vocabulary is. A READER spends the width the SENDER's vocabulary settles.
static const int64_t kTableMessageRefBitsHere = %d;

`, ir.TableMessageRefBits(len(entries)))

	if anyVariable {
		// THE NODE TABLE's OWN SLOT, in a unit that HAS a node table and in no
		// other. The reserved node-table id rides in every unit's vocabulary,
		// because every reader owes §3.1's refusal of it inside a nested body;
		// the SLOT is the writer's half and only a pointered body ever names
		// it, so a value-only unit carries none of it.
		fmt.Fprintf(&b, `// The reserved NODE-TABLE id's own slot in this unit's vocabulary (§3.3). A
// pointered body names the node table through it, and the node table is the
// ROOT body's FIRST field because a pointer index's width is settled by the
// node count it carries.
static const uint64_t kTableNodeTableFieldSlot = %d;

`, slotOfName(entries, ir.TableNodeWireId))
	}

	b.WriteString(cppMessageRuntime)

	fmt.Fprintf(&b, `// THE UNIT'S ANNOUNCEMENT, byte for byte: %d entries and %d bytes. It is an
// ordinary form 1 FILE — the form byte, a body carrying the BUILD VERSION
// under the reserved id at kind 9 and the VOCABULARY under the reserved id at
// kind 14 over element kind 6, and a trailer of those two reserved ids.
//
// THE VOCABULARY IS A FIELD AND NOT THE TRAILER, and that buys three things:
// §3's writer rule that an id no body references is never written is restored
// unbroken, an entry can carry a KIND and a SHAPE which a trailer of bare ids
// cannot, and one NAME can appear at two shapes.
//
// The order is the COOK PROJECTION's (§20.2) — each record in the order the
// projection renders it and each record's fields in the order the projection
// renders them, then each enum's variants and each union's arms — followed by
// the tail the projection does not name: the reserved node-table id, the three
// blob type ids as bytes, string and wstring, and every table's own name id in
// the projection's sorted record order. The tail is UNCONDITIONAL, so an
// ordinary edit only ever grows it at its end and never moves a slot a
// generated field header carries as a literal.
static const int64_t kTableAnnounceBytes = %d;
static const uint8_t kTableAnnounce[ kTableAnnounceBytes ] = {
`, len(entries), len(announcement), len(announcement))
	for i, by := range announcement {
		if i%12 == 0 {
			b.WriteString("    ")
		}
		fmt.Fprintf(&b, "0x%02x,", by)
		if i%12 == 11 || i == len(announcement)-1 {
			b.WriteString("\n")
		} else {
			b.WriteString(" ")
		}
	}
	b.WriteString("};\n\n")
	b.WriteString(cppMessageAnnounce)
	_ = forceInline
	return b.String()
}

// slotOfName is one kind-0 entry's slot, counted from 1.
func slotOfName(entries []ir.TableVocabularyEntry, id uint64) uint64 {
	want := ir.TableVocabularyEntry{Id: id}.Key()
	for i, e := range entries {
		if e.Key() == want {
			return uint64(i + 1)
		}
	}
	return 0
}

// cppMessageRuntime is the bit stream, the announced entry and the shape
// parse: everything the form needs that is not one unit's own constant.
const cppMessageRuntime = `// THE BIT STREAM the bodies ride on (§3.3). It is the packet wire's own
// layout, bit i of the stream in byte i/8 at bit position i%8 low bit first,
// so a value written here and a value written by a generated packet writer are
// the same bits in the same places.
struct TableBitWriter
{
    uint8_t * buffer;
    int64_t capacity; // bytes
    int64_t bits;
    bool overflow;

    TableBitWriter() : buffer( NULL ), capacity( 0 ), bits( 0 ), overflow( false ) {}
    TableBitWriter( uint8_t * to_buffer, int64_t to_capacity ) : buffer( to_buffer ), capacity( to_capacity ), bits( 0 ), overflow( false ) {}

    void put( uint64_t value, int64_t n )
    {
        for ( int64_t i = 0; i < n; i++ )
        {
            const int64_t at = bits + i;
            if ( at / 8 >= capacity ) { overflow = true; bits += n - i; return; }
            if ( ( at % 8 ) == 0 ) { buffer[ at / 8 ] = 0; }
            if ( ( value >> (uint64_t) i ) & 1 ) { buffer[ at / 8 ] = uint8_t( buffer[ at / 8 ] | ( 1 << ( at % 8 ) ) ); }
        }
        bits += n;
    }

    void putbytes( const uint8_t * data, int64_t n )
    {
        for ( int64_t i = 0; i < n; i++ ) { put( (uint64_t) data[i], 8 ); }
    }

    // a string's or a bytes' payload ALIGNS before its bytes, which buys a
    // memcpy on the largest payload on the wire, and a batch aligns once at
    // its end. Both are zero fill.
    void align() { while ( ( bits % 8 ) != 0 ) { put( 0, 1 ); } }
};

// TableAlignBits is what an align costs from a bit position, which a measure
// spends exactly where a save does.
inline int64_t TableAlignBits( int64_t bits ) { return ( 8 - ( bits % 8 ) ) % 8; }

struct TableBitReader
{
    const uint8_t * buffer;
    int64_t bits;   // the stream's extent, in bits
    int64_t offset; // bits consumed

    TableBitReader() : buffer( NULL ), bits( 0 ), offset( 0 ) {}
    TableBitReader( const uint8_t * from_buffer, int64_t from_bytes ) : buffer( from_buffer ), bits( from_bytes * 8 ), offset( 0 ) {}

    bool has( int64_t n ) const { return n >= 0 && offset + n <= bits; }

    bool get( uint64_t & value, int64_t n )
    {
        if ( !has( n ) ) { return false; }
        value = 0;
        for ( int64_t i = 0; i < n; i++ )
        {
            if ( ( buffer[ offset / 8 ] >> ( offset % 8 ) ) & 1 ) { value |= uint64_t(1) << (uint64_t) i; }
            offset++;
        }
        return true;
    }

    bool skip( int64_t n ) { if ( !has( n ) ) { return false; } offset += n; return true; }

    // the pad to the next byte boundary is VERIFIED ZERO, which is the packet
    // wire's rule for the same reason (SPEC.md §4.3)
    bool align()
    {
        while ( ( offset % 8 ) != 0 )
        {
            uint64_t bit = 0;
            if ( !get( bit, 1 ) || bit != 0 ) { return false; }
        }
        return true;
    }
};

// TableBitsRequired is bits_required( min, max ): the bit length of max - min,
// and zero where the two are equal, which is a value that spends no bit at all.
inline int64_t TableBitsRequired( int64_t min, int64_t max )
{
    if ( max <= min ) { return 0; }
    uint64_t span = (uint64_t) ( max - min );
    int64_t n = 0;
    while ( span > 0 ) { n++; span >>= 1; }
    return n;
}

// THE ANNOUNCED ENTRY (§3.3): an id, a kind, and a SHAPE — the width and range
// facts a reader needs to SKIP a field exactly and to DECODE one whose own
// declaration has moved. One name may take TWO entries, at two kinds or two
// shapes, and a body names the one it means.
//
// The ELEMENT's own facts ride beside the field's because an array's element
// is the one nesting this wire has: an array of arrays is not a table-wire
// construct, so one level is every level.
struct TableMessageEntry
{
    uint64_t id = 0;
    uint8_t kind = 0;
    uint8_t packing = 0;
    uint8_t elem_kind = 0;
    uint8_t elem_packing = 0;
    int64_t value_bits = 0; // a ranged or quantized width
    int64_t min = 0;        // an array's minimum count
    int64_t max = 0;        // an array's maximum count, a string's capacity, a keyed array's slots
    int64_t base_lo = 0;    // the ranged base, low half
    int64_t base_hi = 0;    // its high half, for a 128-bit kind
    float qmin = 0.0f;
    float qstep = 0.0f;
    int64_t elem_value_bits = 0;
    int64_t elem_max = 0;
    int64_t elem_base_lo = 0;
    int64_t elem_base_hi = 0;
    float elem_qmin = 0.0f;
    float elem_qstep = 0.0f;
};

inline bool TableMessageIntegerKind( uint8_t kind )
{
    return ( kind >= 2 && kind <= 9 ) || kind == 18 || kind == 19;
}

inline bool TableMessageFixedKind( uint8_t kind ) { return kind >= 20 && kind <= 29; }

inline bool TableMessageKnownKind( uint8_t kind )
{
    return kind == 0 || ( kind >= 1 && kind <= 17 ) || ( kind >= 18 && kind <= 29 ) || ( kind >= 30 && kind <= 32 );
}

// A CANONICAL LEB128, which is the announcement's own integer: the
// announcement is a form 1 FILE and takes §3's rule.
inline bool TableMessageLeb( const uint8_t * in, int64_t size, int64_t & at, uint64_t & value )
{
    value = 0;
    for ( int64_t shift = 0; at < size; shift += 7 )
    {
        if ( shift >= 64 ) { return false; }
        const uint8_t by = in[ at++ ];
        value |= uint64_t( by & 0x7F ) << shift;
        if ( ( by & 0x80 ) == 0 ) { return !( shift > 0 && by == 0 ); }
    }
    return false;
}

inline bool TableMessageShapeRead( const uint8_t * in, int64_t size, int64_t & at, uint8_t kind,
                                   uint8_t & packing, int64_t & value_bits, int64_t & base_lo, int64_t & base_hi,
                                   float & qmin, float & qstep, int64_t & min, int64_t & max, uint8_t & elem_kind );

// TableMessageEntryRead parses ONE entry, and answers false for a HOSTILE
// SHAPE: bits above 128, an array whose min exceeds its max, an element kind
// outside the closed set, or a shape running past the vocabulary's own bytes.
// It runs once, at AnnounceRead, and never again — so a width a reader
// accepted is a width bounded by the kind it came under.
inline bool TableMessageEntryRead( const uint8_t * in, int64_t size, int64_t & at, TableMessageEntry & entry )
{
    if ( at + 9 > size ) { return false; }
    entry = TableMessageEntry();
    for ( int i = 0; i < 8; i++ ) { entry.id |= uint64_t( in[ at + i ] ) << ( 8 * i ); }
    entry.kind = in[ at + 8 ];
    at += 9;
    if ( !TableMessageKnownKind( entry.kind ) ) { return false; }
    if ( !TableMessageShapeRead( in, size, at, entry.kind, entry.packing, entry.value_bits,
                                 entry.base_lo, entry.base_hi, entry.qmin, entry.qstep,
                                 entry.min, entry.max, entry.elem_kind ) ) { return false; }
    if ( entry.kind == 14 || entry.kind == 16 )
    {
        int64_t elem_min = 0;
        uint8_t inner_kind = 0;
        if ( !TableMessageShapeRead( in, size, at, entry.elem_kind, entry.elem_packing, entry.elem_value_bits,
                                     entry.elem_base_lo, entry.elem_base_hi, entry.elem_qmin, entry.elem_qstep,
                                     elem_min, entry.elem_max, inner_kind ) ) { return false; }
        (void) elem_min; (void) inner_kind;
    }
    return true;
}

// TableMessageShapeRead is one shape, by the kind that names it (§3.3's shape
// table). Every number in it is a canonical LEB128 except where the row says
// otherwise.
inline bool TableMessageShapeRead( const uint8_t * in, int64_t size, int64_t & at, uint8_t kind,
                                   uint8_t & packing, int64_t & value_bits, int64_t & base_lo, int64_t & base_hi,
                                   float & qmin, float & qstep, int64_t & min, int64_t & max, uint8_t & elem_kind )
{
    uint64_t v = 0;
    if ( TableMessageIntegerKind( kind ) || TableMessageFixedKind( kind ) || kind == 10 )
    {
        if ( at >= size ) { return false; }
        packing = in[ at++ ];
        if ( packing == 0 ) { return true; }
        if ( packing == 1 && kind != 10 )
        {
            if ( !TableMessageLeb( in, size, at, v ) || v > 128 ) { return false; }
            value_bits = (int64_t) v;
            if ( kind == 18 || kind == 19 || TableMessageFixedKind( kind ) )
            {
                if ( at + 16 > size ) { return false; }
                uint64_t lo = 0, hi = 0;
                for ( int i = 0; i < 8; i++ ) { lo |= uint64_t( in[ at + i ] ) << ( 8 * i ); }
                for ( int i = 0; i < 8; i++ ) { hi |= uint64_t( in[ at + 8 + i ] ) << ( 8 * i ); }
                base_lo = (int64_t) lo; base_hi = (int64_t) hi;
                at += 16;
                return true;
            }
            if ( !TableMessageLeb( in, size, at, v ) ) { return false; }
            base_lo = (int64_t) ( v >> 1 ) ^ -(int64_t) ( v & 1 ); // zigzag
            return true;
        }
        if ( packing == 2 && kind == 10 )
        {
            if ( !TableMessageLeb( in, size, at, v ) || v > 32 ) { return false; }
            value_bits = (int64_t) v;
            if ( at + 8 > size ) { return false; }
            uint32_t raw_min = 0, raw_step = 0;
            for ( int i = 0; i < 4; i++ ) { raw_min |= uint32_t( in[ at + i ] ) << ( 8 * i ); }
            for ( int i = 0; i < 4; i++ ) { raw_step |= uint32_t( in[ at + 4 + i ] ) << ( 8 * i ); }
            at += 8;
            memcpy( &qmin, &raw_min, 4 );
            memcpy( &qstep, &raw_step, 4 );
            return true;
        }
        return false; // a packing outside the closed set
    }
    if ( kind == 12 || kind == 33 )
    {
        if ( !TableMessageLeb( in, size, at, v ) ) { return false; }
        max = (int64_t) v;
        return true;
    }
    if ( kind == 14 || kind == 16 )
    {
        if ( kind == 14 )
        {
            if ( !TableMessageLeb( in, size, at, v ) ) { return false; }
            min = (int64_t) v;
        }
        if ( !TableMessageLeb( in, size, at, v ) || (int64_t) v < min ) { return false; }
        max = (int64_t) v;
        if ( at >= size ) { return false; }
        elem_kind = in[ at++ ];
        if ( !TableMessageKnownKind( elem_kind ) ) { return false; }
        return true;
    }
    return true;
}

// TableMessageValueBits is one value's width under a shape, and -1 where the
// kind's payload is not a fixed-width value at all.
inline int64_t TableMessageValueBits( uint8_t kind, uint8_t packing, int64_t value_bits )
{
    if ( kind == 1 ) { return 1; }
    if ( kind == 11 ) { return 64; }
    if ( kind == 10 ) { return packing == 2 ? value_bits : 32; }
    if ( TableMessageIntegerKind( kind ) || TableMessageFixedKind( kind ) )
    {
        if ( packing == 1 ) { return value_bits; }
        switch ( kind )
        {
            case 2: case 6: case 20: case 25: return 8;
            case 3: case 7: case 21: case 26: return 16;
            case 4: case 8: case 22: case 27: return 32;
            case 5: case 9: case 23: case 28: return 64;
            default: return 128;
        }
    }
    return -1;
}

`

// cppMessageAnnounce is the vocabulary a receiver reads an announcement into,
// the read itself, the generic skip, and the batch's own surface.
const cppMessageAnnounce = `// TableVocabulary is ONE DIRECTION's announced vocabulary (§3.3): the entries
// an announcement carried, whole, under one numbering.
//
// It BORROWS the announcement's bytes rather than copying them, so the
// announcement has to outlive it. What it holds of its own is the OFFSET of
// each entry inside those bytes, which is what turns a slot into an index
// rather than a search. A peer holds TWO of these for a connection, the one it
// writes with and the one it reads with, and neither is the other's. A restart
// opens a fresh connection with an empty vocabulary and nothing is cached
// across connections.
struct TableVocabulary
{
    // THE CONFORMING DEFAULT BOUNDS (§3.3), and there are two because an entry
    // is not a fixed width: a receiver refuses an announcement above either by
    // name, and the byte bound is read off the vocabulary field's own length
    // before an entry is touched.
    static const int64_t kDefaultMaxEntries = 4096;
    static const int64_t kDefaultMaxBytes = 64 * 1024;

    const uint8_t * vocabulary = NULL; // the announcement's own bytes, borrowed
    int64_t vocabulary_bytes = 0;
    int32_t offsets[ kDefaultMaxEntries ] = {};
    int64_t count = 0;
    int64_t ref_bits = 0;
    uint64_t build_version = 0;
    bool announced = false;
    int64_t max_entries = kDefaultMaxEntries;
    int64_t max_bytes = kDefaultMaxBytes;
};

// TableVocabularyEntryAt is the entry a reference names, counted from 1. The
// offset is an index and the shape is re-read from the announcement's own
// bytes, which is a handful of byte reads and no allocation.
inline TableMessageEntry TableVocabularyEntryAt( const TableVocabulary & vocabulary, uint64_t slot )
{
    TableMessageEntry entry;
    int64_t at = vocabulary.offsets[ slot - 1 ];
    TableMessageEntryRead( vocabulary.vocabulary, vocabulary.vocabulary_bytes, at, entry );
    return entry;
}

// AnnounceRead reads an announcement into one direction's vocabulary (§3.3).
//
// The announcement IS a file, so every malformed rule of §3 already covers it.
// Over its body there are EXACTLY TWO STRICT CHECKS: the BUILD VERSION
// present, exactly once, under kind 9, eight bytes wide, and the VOCABULARY
// present, exactly once, under kind 14 over element kind 6. Everything else is
// ordinary and tolerant, so an unknown field is skipped and counted and the
// announcement can GAIN a field in a later minor without a lockstep redeploy.
//
// The FIRST announcement sets the vocabulary and it is the only one that can.
// A SECOND is refused by name: it does not replace it, does not amend it and
// changes nothing. A refused announcement sets NO VOCABULARY.
inline bool AnnounceRead( TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    TableReport ignored;
    TableReport * to = report != NULL ? report : &ignored;
    if ( vocabulary.announced )
    {
        to->refused = true;
        to->reason = second_announcement;
        return false;
    }
    if ( bytes < 1 ) { to->malformed = true; return false; }
    if ( buffer[0] != kTableWireForm )
    {
        to->refused = true;
        to->reason = buffer[0] == kTableWireMessageForm ? message_form_as_file : newer_form;
        return false;
    }
    if ( bytes < 9 ) { to->malformed = true; return false; }
    TableIdTable table;
    int64_t body_bytes = 0;
    const TableOpenVerdict verdict = TableOpen( buffer, bytes, table, body_bytes );
    if ( verdict != TableOpenOk )
    {
        if ( verdict == TableOpenDamaged ) { to->malformed = true; }
        else { to->refused = true; to->reason = newer_form; }
        return false;
    }
    if ( TableBodyEndsEarly( buffer + 1, body_bytes, table ) ) { to->malformed = true; return false; }
    TableReader r( buffer + 1, body_bytes, to, &table );
    uint64_t version = 0;
    const uint8_t * words = NULL;
    int64_t words_bytes = 0;
    int32_t seen_version = 0, seen_vocabulary = 0;
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.getleb( ref ) ) { to->malformed = true; return false; }
        if ( ref == 0 ) { break; }
        if ( ref > (uint64_t) table.count || !r.has( 1 ) ) { to->malformed = true; return false; }
        const uint64_t id = table.at( ref );
        const uint8_t kind = r.get8();
        if ( id == kTableBuildVersionFieldId )
        {
            if ( kind != 9 || !r.has( 8 ) ) { to->refused = true; to->reason = no_vocabulary; return false; }
            version = r.get64();
            seen_version++;
            continue;
        }
        if ( id == kTableMessageVocabularyFieldId )
        {
            // kind 14 over element kind 6, which is §3's spelling for an
            // opaque run of bytes
            uint64_t framed = 0;
            if ( kind != 14 || !r.getleb( framed ) || !r.has( (int64_t) framed ) ) { to->refused = true; to->reason = no_vocabulary; return false; }
            const int64_t begin = r.offset, end = r.offset + (int64_t) framed;
            r.offset = end;
            if ( begin >= end || r.buffer[ begin ] != 6 ) { to->refused = true; to->reason = no_vocabulary; return false; }
            int64_t at = begin + 1;
            uint64_t length = 0;
            if ( !TableMessageLeb( r.buffer, end, at, length ) || at + (int64_t) length != end ) { to->refused = true; to->reason = no_vocabulary; return false; }
            if ( (int64_t) length > vocabulary.max_bytes ) { to->refused = true; to->reason = vocabulary_too_large; return false; }
            words = r.buffer + at;
            words_bytes = (int64_t) length;
            seen_vocabulary++;
            continue;
        }
        to->unknown++;
        if ( !r.skip( kind ) ) { to->malformed = true; return false; }
    }
    if ( seen_version != 1 || seen_vocabulary != 1 ) { to->refused = true; to->reason = no_vocabulary; return false; }

    // THE ENTRIES, parsed once: every width is checked here and never again
    int64_t at = 0, count = 0;
    while ( at < words_bytes )
    {
        if ( count >= vocabulary.max_entries ) { to->refused = true; to->reason = vocabulary_too_large; return false; }
        const int64_t begin = at;
        TableMessageEntry parsed;
        if ( !TableMessageEntryRead( words, words_bytes, at, parsed ) ) { to->malformed = true; return false; }
        vocabulary.offsets[ count++ ] = (int32_t) begin;
    }
    vocabulary.vocabulary = words;
    vocabulary.vocabulary_bytes = words_bytes;
    vocabulary.count = count;
    vocabulary.ref_bits = TableBitsRequired( 0, count );
    vocabulary.build_version = version;
    vocabulary.announced = true;
    return true;
}

// TableMessageSkip steps over one field's payload without decoding it, using
// the announced ENTRY alone (§3.3). It is what makes an unknown entry
// skippable on a body with no kind byte, and it is ONE function over every
// table, because a shape says everything a skipper needs.
inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & vocabulary );

inline bool TableMessageSkip( TableBitReader & r, const TableVocabulary & vocabulary, const TableMessageEntry & entry )
{
    switch ( entry.kind )
    {
        case 0: case 32: return true;              // a name, and a payload-free arm
        case 30: return r.skip( vocabulary.ref_bits );
        case 13: return TableMessageSkipBody( r, vocabulary );
        case 17: return r.skip( vocabulary.ref_bits ); // a node index, whose width the node count settles
        case 15:
        {
            uint64_t arm = 0;
            if ( !r.get( arm, vocabulary.ref_bits ) ) { return false; }
            if ( arm == 0 ) { return true; }
            if ( arm > (uint64_t) vocabulary.count ) { return false; }
            return TableMessageSkip( r, vocabulary, TableVocabularyEntryAt( vocabulary, arm ) );
        }
        case 12:
        {
            uint64_t n = 0;
            if ( !r.get( n, TableBitsRequired( 0, entry.max ) ) || !r.align() ) { return false; }
            return r.skip( (int64_t) n * 8 );
        }
        case 14: case 16:
        {
            uint64_t n = (uint64_t) entry.min;
            const int64_t width = entry.kind == 16 ? TableBitsRequired( 0, entry.max ) : TableBitsRequired( entry.min, entry.max );
            if ( entry.kind == 16 ) { n = 0; }
            if ( width > 0 )
            {
                uint64_t raw = 0;
                if ( !r.get( raw, width ) ) { return false; }
                n = entry.kind == 16 ? raw : raw + (uint64_t) entry.min;
            }
            if ( entry.kind == 14 && entry.elem_kind == 6 && !r.align() ) { return false; }
            const int64_t elem = TableMessageValueBits( entry.elem_kind, entry.elem_packing, entry.elem_value_bits );
            for ( uint64_t i = 0; i < n; i++ )
            {
                if ( entry.kind == 16 && !r.skip( vocabulary.ref_bits ) ) { return false; }
                switch ( entry.elem_kind )
                {
                    case 13: if ( !TableMessageSkipBody( r, vocabulary ) ) { return false; } break;
                    case 30: case 17: if ( !r.skip( vocabulary.ref_bits ) ) { return false; } break;
                    case 15:
                    {
                        TableMessageEntry inner;
                        inner.kind = 15;
                        if ( !TableMessageSkip( r, vocabulary, inner ) ) { return false; }
                        break;
                    }
                    default: if ( elem < 0 || !r.skip( elem ) ) { return false; } break;
                }
            }
            return true;
        }
    }
    const int64_t width = TableMessageValueBits( entry.kind, entry.packing, entry.value_bits );
    return width >= 0 && r.skip( width );
}

inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & vocabulary )
{
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.get( ref, vocabulary.ref_bits ) ) { return false; }
        if ( ref == 0 ) { return true; }
        if ( ref > (uint64_t) vocabulary.count ) { return false; }
        if ( !TableMessageSkip( r, vocabulary, TableVocabularyEntryAt( vocabulary, ref ) ) ) { return false; }
    }
}

// AnnounceMeasure is the announcement's byte count, which is a constant of the
// unit and not a walk.
inline int64_t AnnounceMeasure() { return kTableAnnounceBytes; }

// Announce writes the announcement into the caller's buffer and answers the
// bytes written — exactly AnnounceMeasure's answer — or -1 when the buffer is
// too small. It allocates nothing and walks nothing.
inline int64_t Announce( uint8_t * buffer, int64_t capacity )
{
    if ( buffer == NULL || capacity < kTableAnnounceBytes ) { return -1; }
    memcpy( buffer, kTableAnnounce, (size_t) kTableAnnounceBytes );
    return kTableAnnounceBytes;
}

// THE PRIMITIVE IS A BATCH (§3.3): a number of bodies of ONE ROOT in one
// buffer, one count and one continuous bit stream with no alignment between
// them. A single message is the batch of one.
//
// The count rides ahead of the bodies, so a writer declares it at Begin and
// End refuses a batch that wrote a different number: a count the bodies do not
// match is not a wire this writer will hand anyone.
struct TableMessageBatch
{
    TableBitWriter w;
    int64_t declared = 0;
    int64_t written = 0;
};

inline bool TableMessageBatchBegin( TableMessageBatch & batch, uint8_t * buffer, int64_t capacity, int64_t bodies )
{
    if ( buffer == NULL || capacity < 1 || bodies < 1 || bodies > kTableMessageBatchMax ) { return false; }
    buffer[0] = kTableWireMessageForm; // the FORM BYTE is read first, always
    batch.w = TableBitWriter( buffer + 1, capacity - 1 );
    batch.declared = bodies;
    batch.written = 0;
    batch.w.put( (uint64_t) ( bodies - 1 ), 8 ); // a ranged integer over [1, 256]
    return true;
}

// TableMessageBatchEnd zero-fills to the next byte — the one alignment a batch
// spends at its end — and answers the whole batch's byte count, or -1.
inline int64_t TableMessageBatchEnd( TableMessageBatch & batch )
{
    if ( batch.written != batch.declared || batch.w.overflow ) { return -1; }
    batch.w.align();
    if ( batch.w.overflow ) { return -1; }
    return 1 + batch.w.bits / 8;
}

// TableMessageBatchBytes is a batch's byte count from its bodies' BIT count,
// which is what every <Root>MeasureMessages answers.
inline int64_t TableMessageBatchBytes( int64_t body_bits )
{
    if ( body_bits < 0 ) { return -1; }
    return 1 + ( 8 + body_bits + 7 ) / 8;
}

// The reading half. A batch is opened once and its bodies are then read in
// order into the storage the caller sized for them: which root a batch carries
// is the APPLICATION's and never this wire's.
struct TableMessageBatchReader
{
    TableBitReader r;
    const TableVocabulary * vocabulary = NULL;
    TableReport * report = NULL;
    int64_t remaining = 0;
};

// TableMessageBatchOpen answers the batch's body count, or -1 with the refusal
// on the report: a form byte this reader does not carry, or a body from a peer
// that never announced.
inline int64_t TableMessageBatchOpen( TableMessageBatchReader & br, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    static TableReport ignored;
    br.report = report != NULL ? report : &ignored;
    br.vocabulary = &vocabulary;
    if ( bytes < 1 ) { br.report->malformed = true; return -1; }
    if ( buffer[0] != kTableWireMessageForm ) { br.report->refused = true; br.report->reason = newer_form; return -1; }
    if ( !vocabulary.announced ) { br.report->refused = true; br.report->reason = no_vocabulary; return -1; }
    br.r = TableBitReader( buffer + 1, bytes - 1 );
    uint64_t count = 0;
    if ( !br.r.get( count, 8 ) ) { br.report->malformed = true; return -1; }
    br.remaining = (int64_t) count + 1;
    return br.remaining;
}

// TableMessageBatchClose verifies the trailing pad, which is the last of the
// three ways a batch is damaged (§3.3).
inline bool TableMessageBatchClose( TableMessageBatchReader & br )
{
    if ( br.remaining != 0 || !br.r.align() ) { br.report->malformed = true; return false; }
    return true;
}

`
