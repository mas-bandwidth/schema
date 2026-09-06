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

// THE WIDEST COUNT THIS FORM SPELLS, which is the count an UNBOUNDED array
// announces (§2.9): an unbounded array states no bound, so the announcement
// states the widest one a batch could carry. It is the ceiling an array's or a
// keyed entry's announced min and max are checked against.
static const uint64_t kTableMessageListMax = 0xFFFFFFFFull;

// THIS UNIT'S OWN REFERENCE WIDTH: the bits a writer spends on every reference
// of every body it writes, which is a compile-time constant because the
// vocabulary is. A READER spends the width the SENDER's vocabulary settles.
static const int64_t kTableMessageRefBitsHere = %d;

// THIS UNIT'S OWN ENTRY COUNT, which is the CAPACITY a receiver declares for
// its resolved vocabulary when it talks only to peers of this schema (§3.3).
// The vocabulary is a pure function of the build version, so a peer at this
// build announces exactly this many entries; a receiver that means to meet
// OTHER builds declares more, and an announcement above whatever it declared
// is refused as vocabulary_too_large.
static const int64_t kTableMessageEntriesHere = %d;

`, ir.TableMessageRefBits(len(entries)), len(entries))

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

// EIGHT BYTES OF THE STREAM AS ONE WORD, and the word is LITTLE-END-FIRST
// whatever order this host is in, because the stream's own definition puts
// bit i in byte i/8: byte 0 of the run holds the word's low eight bits. That
// is what lets one value of any width move in one unaligned load or store
// instead of one touch a byte, and the BITS ON THE WIRE do not move.
inline uint64_t table_message_byteswap64( uint64_t v )
{
    return ( v >> 56 ) | ( ( v >> 40 ) & 0xff00ull ) | ( ( v >> 24 ) & 0xff0000ull ) | ( ( v >> 8 ) & 0xff000000ull )
         | ( ( v << 8 ) & 0xff00000000ull ) | ( ( v << 24 ) & 0xff0000000000ull ) | ( ( v << 40 ) & 0xff000000000000ull )
         | ( v << 56 );
}

inline uint64_t table_message_load64( const uint8_t * p )
{
    uint64_t v = 0;
    memcpy( &v, p, 8 );
#if defined( __BYTE_ORDER__ ) && defined( __ORDER_BIG_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
    v = table_message_byteswap64( v );
#endif
    return v;
}

inline void table_message_store64( uint8_t * p, uint64_t v )
{
#if defined( __BYTE_ORDER__ ) && defined( __ORDER_BIG_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
    v = table_message_byteswap64( v );
#endif
    memcpy( p, &v, 8 );
}

struct TableBitWriter
{
    uint8_t * buffer;
    int64_t capacity; // bytes
    int64_t bits;
    bool overflow;

    TableBitWriter() : buffer( NULL ), capacity( 0 ), bits( 0 ), overflow( false ) {}
    TableBitWriter( uint8_t * to_buffer, int64_t to_capacity ) : buffer( to_buffer ), capacity( to_capacity ), bits( 0 ), overflow( false ) {}

    // ONE WORD AT A TIME, never one bit and never one byte of arithmetic: the
    // value is shifted into place in a REGISTER once, and the bytes it
    // occupies are stored from that register with no read back. A sixty-four
    // bit field costs one shift rather than nine masked read-modify-writes.
    // The word is assembled little-end-first, so the BITS ON THE WIRE are the
    // same bits in the same places — bit i in byte i/8 at position i%8, low
    // bit first — which is what the pinned goldens hold. IT WRITES EXACTLY
    // THE BYTES THE VALUE OCCUPIES and never one past them, so a caller's
    // buffer beyond the batch is its own.
    void put( uint64_t value, int64_t n )
    {
        if ( n <= 0 ) { return; }
        if ( ( bits + n + 7 ) / 8 > capacity ) { overflow = true; bits += n; return; }
        if ( n < 64 ) { value &= ( uint64_t( 1 ) << n ) - 1; } // a caller's high bits never leak
        const int64_t index = bits >> 3;
        const int64_t bit = bits & 7;
        // the byte the write STARTS in keeps the bits already written to it,
        // and every byte after it is this value's own
        const uint64_t head = bit != 0 ? ( uint64_t( buffer[index] ) & ( ( uint64_t( 1 ) << bit ) - 1 ) ) : 0;
        const uint64_t word = head | ( value << bit );
        const int64_t need = ( bit + n + 7 ) >> 3; // 1 to 9 bytes
        if ( need >= 8 )
        {
            table_message_store64( buffer + index, word );
            if ( need > 8 ) { buffer[index + 8] = uint8_t( value >> ( 64 - bit ) ); }
        }
        else
        {
            for ( int64_t i = 0; i < need; i++ ) { buffer[index + i] = uint8_t( word >> ( 8 * i ) ); }
        }
        bits += n;
    }

    // THE ALIGN IS WHAT BUYS THIS (docs/SPEC-TABLES.md §3.3): a string(N), a
    // bytes(N) and a blob record align before their bytes precisely so the
    // largest payload on the wire moves as ONE memcpy. Off a boundary there is
    // nothing to memcpy and the bytes go through put.
    void putbytes( const uint8_t * data, int64_t n )
    {
        if ( n <= 0 ) { return; }
        if ( ( bits & 7 ) == 0 )
        {
            if ( ( bits >> 3 ) + n > capacity ) { overflow = true; bits += n * 8; return; }
            memcpy( buffer + ( bits >> 3 ), data, (size_t) n );
            bits += n * 8;
            return;
        }
        for ( int64_t i = 0; i < n; i++ ) { put( (uint64_t) data[i], 8 ); }
    }

    // a string's or a bytes' payload ALIGNS before its bytes, and a batch
    // aligns once at its end. Both are zero fill, spent in one call.
    void align() { put( 0, ( 8 - ( bits & 7 ) ) & 7 ); }
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

    // the primitive is sixty-four bits, and a width above it is refused
    // here as well as at the announcement: no field on any body can ask this
    // reader to move more bits than it holds
    // ONE WORD OF THE BUFFER AT A TIME, the mirror of the writer's put: the
    // eight bytes the value starts in load as one little-end-first word and a
    // ninth byte carries the spill a value that straddles the word needs.
    // Within nine bytes of the stream's end there is no room for a word load
    // and the bytes come one at a time, by the same arithmetic.
    bool get( uint64_t & value, int64_t n )
    {
        if ( n > 64 || !has( n ) ) { return false; }
        value = 0;
        if ( n == 0 ) { return true; }
        const int64_t index = offset >> 3;
        const int64_t bit = offset & 7;
        const int64_t bytes = ( bits + 7 ) >> 3;
        if ( index + 9 <= bytes )
        {
            uint64_t v = table_message_load64( buffer + index ) >> bit;
            if ( bit != 0 && bit + n > 64 ) { v |= uint64_t( buffer[index + 8] ) << ( 64 - bit ); }
            value = n == 64 ? v : ( v & ( ( uint64_t( 1 ) << n ) - 1 ) );
            offset += n;
            return true;
        }
        int64_t got = 0;
        while ( got < n )
        {
            const int64_t byte = offset >> 3;
            const int64_t off = offset & 7;
            const int64_t room = 8 - off;
            const int64_t take = ( n - got ) < room ? ( n - got ) : room;
            const uint64_t chunk = ( uint64_t( buffer[byte] ) >> off ) & ( ( uint64_t( 1 ) << take ) - 1 );
            value |= chunk << got;
            offset += take;
            got += take;
        }
        return true;
    }

    // the bytes of an ALIGNED payload, which is the read side of the memcpy
    // the align buys (docs/SPEC-TABLES.md §3.3)
    bool getbytes( uint8_t * out, int64_t n )
    {
        if ( n < 0 || !has( n * 8 ) ) { return false; }
        if ( ( offset & 7 ) == 0 )
        {
            memcpy( out, buffer + ( offset >> 3 ), (size_t) n );
            offset += n * 8;
            return true;
        }
        for ( int64_t i = 0; i < n; i++ )
        {
            uint64_t by = 0;
            if ( !get( by, 8 ) ) { return false; }
            out[i] = (uint8_t) by;
        }
        return true;
    }

    bool skip( int64_t n ) { if ( !has( n ) ) { return false; } offset += n; return true; }

    // the pad to the next byte boundary is VERIFIED ZERO, which is the packet
    // wire's rule for the same reason (SPEC.md §4.3)
    bool align()
    {
        const int64_t pad = ( 8 - ( offset & 7 ) ) & 7;
        if ( pad == 0 ) { return true; }
        uint64_t bits_read = 0;
        return get( bits_read, pad ) && bits_read == 0;
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
//
// IT IS THE RESOLVED ENTRY AND THE CALLER SIZES AN ARRAY OF THEM, so it
// carries what a DECODE takes and nothing a decode does not: qmin, qdelta and
// qcount are what SPEC.md §4.3's rule leaves behind, and the qmax and qres
// that rule CONSUMES are locals of the parse. The widths are int16 because a
// width is bounded by the kind it came under and no kind holds more than 128
// bits.
struct TableMessageEntry
{
    uint64_t id = 0;
    int64_t min = 0;        // an array's minimum count
    int64_t max = 0;        // an array's maximum count, a string's capacity, a keyed array's slots
    int64_t base_lo = 0;    // the ranged base, low half: a signed kind's sign-extends, an unsigned kind's is whole
    int64_t base_hi = 0;    // its high half, for a 128-bit kind
    int64_t elem_max = 0;
    int64_t elem_base_lo = 0;
    int64_t elem_base_hi = 0;
    // what SPEC.md §4.3's derivation leaves: the base, the step and the count
    float qmin = 0.0f;
    float qdelta = 0.0f;
    uint32_t qcount = 0;
    float elem_qmin = 0.0f;
    float elem_qdelta = 0.0f;
    uint32_t elem_qcount = 0;
    // THE PAYLOAD'S WIDTH, RESOLVED: what the kind, the packing and the
    // announced bits together say, computed once at AnnounceRead, and -1
    // where the payload is not a fixed-width value at all
    int16_t value_bits = -1;
    int16_t elem_value_bits = -1;
    uint8_t kind = 0;
    uint8_t packing = 0;
    uint8_t elem_kind = 0;
    uint8_t elem_packing = 0;
};

// TableMessageEntrySame reports whether two RESOLVED entries carry the same
// shape, which is every fact of the entry but its id and its kind. It is what
// the announcement's duplicate rule is asked in: two entries that agree on all
// three parts are malformed (§3.3).
inline bool TableMessageEntrySame( const TableMessageEntry & a, const TableMessageEntry & b )
{
    return a.min == b.min && a.max == b.max && a.base_lo == b.base_lo && a.base_hi == b.base_hi
        && a.elem_max == b.elem_max && a.elem_base_lo == b.elem_base_lo && a.elem_base_hi == b.elem_base_hi
        && a.qmin == b.qmin && a.qdelta == b.qdelta && a.qcount == b.qcount
        && a.elem_qmin == b.elem_qmin && a.elem_qdelta == b.elem_qdelta && a.elem_qcount == b.elem_qcount
        && a.value_bits == b.value_bits && a.elem_value_bits == b.elem_value_bits
        && a.packing == b.packing && a.elem_kind == b.elem_kind && a.elem_packing == b.elem_packing;
}

// TableMessageKindBits is the widest RANGED value a kind can carry, its own
// storage width: a width above it is a hostile width on the announcement.
inline int64_t TableMessageKindBits( uint8_t kind )
{
    switch ( kind )
    {
        case 2: case 6: case 20: case 25: return 8;
        case 3: case 7: case 21: case 26: return 16;
        case 4: case 8: case 22: case 27: return 32;
        case 5: case 9: case 23: case 28: return 64;
        default: return 128;
    }
}

// TableMessageQuantization is SPEC.md §4.3's derivation over an announced
// triple, in float32 and by nothing else: delta, the step count and the
// width. False is a triple SPEC.md calls non-conforming, which on the
// announcement is a hostile width like any other (§3.3).
inline bool TableMessageQuantization( float qmin, float qmax, float qres, float & delta, uint32_t & count, int64_t & bits )
{
    if ( !( qmin < qmax ) || !( qres > 0.0f ) ) { return false; }
    delta = qmax - qmin;
    float values = delta / qres;
    if ( !( delta - delta == 0.0f ) || !( values - values == 0.0f ) ) { return false; } // Inf - Inf is NaN
    if ( !( values >= 1.0f ) ) { values = 1.0f; }
    else if ( values > 4294967040.0f ) { values = 4294967040.0f; } // the largest float below 2^32
    count = (uint32_t) values;
    if ( (float) count < values ) { count++; } // ceil, on a value the cast holds exactly
    bits = TableBitsRequired( 0, (int64_t) count );
    return true;
}

// The two roundings on each side of the rule (SPEC.md §7.2): the product
// rounds to float32 BEFORE the add, which a compiler permitted to contract
// would otherwise fuse into one rounding and move the wire.
#if ( defined( __GNUC__ ) || defined( __clang__ ) ) && ( defined( __aarch64__ ) || defined( _M_ARM64 ) )
#define TABLE_FLOAT_FORCE_ROUND( x ) __asm__ ( "" : "+w" ( x ) )
#elif ( defined( __GNUC__ ) || defined( __clang__ ) ) && ( defined( __x86_64__ ) || defined( __i386__ ) )
#define TABLE_FLOAT_FORCE_ROUND( x ) __asm__ ( "" : "+x" ( x ) )
#else
#define TABLE_FLOAT_FORCE_ROUND( x ) do { volatile float table_float_force_round_slot = ( x ); ( x ) = table_float_force_round_slot; } while ( 0 )
#endif

// TableMessageQuantize is the writer's half: the index a value takes.
inline uint32_t TableMessageQuantize( float value, float qmin, float delta, uint32_t count )
{
    float normalized = ( value - qmin ) / delta;
    if ( !( normalized >= 0.0f ) ) { normalized = 0.0f; }
    else if ( !( normalized <= 1.0f ) ) { normalized = 1.0f; }
    float scaled = normalized * (float) count;
    TABLE_FLOAT_FORCE_ROUND( scaled );
    uint32_t index = (uint32_t) ( scaled + 0.5f ); // floor of a non-negative value
    if ( index > count ) { index = count; }
    return index;
}

// TableMessageDequantize is the reader's half: the float an index names.
inline float TableMessageDequantize( uint32_t index, float qmin, float delta, uint32_t count )
{
    if ( index > count ) { index = count; }
    const float normalized = index / (float) count;
    float scaled = normalized * delta;
    TABLE_FLOAT_FORCE_ROUND( scaled );
    return scaled + qmin;
}

inline bool TableMessageIntegerKind( uint8_t kind )
{
    return ( kind >= 2 && kind <= 9 ) || kind == 18 || kind == 19;
}

inline bool TableMessageFixedKind( uint8_t kind ) { return kind >= 20 && kind <= 29; }

inline bool TableMessageKnownKind( uint8_t kind )
{
    return kind == 0 || ( kind >= 1 && kind <= 17 ) || ( kind >= 18 && kind <= 29 ) || ( kind >= 30 && kind <= 33 );
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

// TableMessageShapeFacts is where one shape's facts land: the field's own,
// or its element's, which is the one nesting this wire has.
struct TableMessageShapeFacts
{
    uint8_t & packing; int64_t & value_bits; int64_t & base_lo; int64_t & base_hi;
    float & qmin; float & qmax; float & qres; float & qdelta; uint32_t & qcount;
    int64_t & min; int64_t & max; uint8_t & elem_kind;
};

inline bool TableMessageShapeRead( const uint8_t * in, int64_t size, int64_t & at, uint8_t kind, TableMessageShapeFacts f );
inline int64_t TableMessageValueBits( uint8_t kind, uint8_t packing, int64_t value_bits );

// TableMessageEntryRead parses ONE entry, and answers false for a HOSTILE
// SHAPE: bits above the kind's own domain, an array whose min exceeds its
// max, an element kind outside the closed set, a quantized triple SPEC.md
// calls non-conforming, or a shape running past the vocabulary's own bytes.
inline bool TableMessageEntryRead( const uint8_t * in, int64_t size, int64_t & at, TableMessageEntry & entry )
{
    if ( at + 9 > size ) { return false; }
    entry = TableMessageEntry();
    for ( int i = 0; i < 8; i++ ) { entry.id |= uint64_t( in[ at + i ] ) << ( 8 * i ); }
    entry.kind = in[ at + 8 ];
    at += 9;
    if ( !TableMessageKnownKind( entry.kind ) ) { return false; }
    // The parse lands in LOCALS and the entry keeps what a decode reads: the
    // quantized max and res are the derivation's inputs and never a field's.
    uint8_t packing = 0, elem_kind = 0;
    int64_t bits = 0, base_lo = 0, base_hi = 0, min = 0, max = 0;
    float qmin = 0.0f, qmax = 0.0f, qres = 0.0f, qdelta = 0.0f;
    uint32_t qcount = 0;
    TableMessageShapeFacts own = { packing, bits, base_lo, base_hi,
                                   qmin, qmax, qres, qdelta, qcount,
                                   min, max, elem_kind };
    if ( !TableMessageShapeRead( in, size, at, entry.kind, own ) ) { return false; }
    entry.packing = packing;
    entry.value_bits = (int16_t) TableMessageValueBits( entry.kind, packing, bits );
    entry.base_lo = base_lo;
    entry.base_hi = base_hi;
    entry.qmin = qmin;
    entry.qdelta = qdelta;
    entry.qcount = qcount;
    entry.min = min;
    entry.max = max;
    entry.elem_kind = elem_kind;
    if ( entry.kind == 14 || entry.kind == 16 )
    {
        uint8_t elem_packing = 0, inner_kind = 0;
        int64_t elem_bits = 0, elem_base_lo = 0, elem_base_hi = 0, elem_min = 0, elem_max = 0;
        float elem_qmin = 0.0f, elem_qmax = 0.0f, elem_qres = 0.0f, elem_qdelta = 0.0f;
        uint32_t elem_qcount = 0;
        TableMessageShapeFacts elem = { elem_packing, elem_bits, elem_base_lo, elem_base_hi,
                                        elem_qmin, elem_qmax, elem_qres, elem_qdelta, elem_qcount,
                                        elem_min, elem_max, inner_kind };
        if ( !TableMessageShapeRead( in, size, at, entry.elem_kind, elem ) ) { return false; }
        entry.elem_packing = elem_packing;
        entry.elem_value_bits = (int16_t) TableMessageValueBits( entry.elem_kind, elem_packing, elem_bits );
        entry.elem_base_lo = elem_base_lo;
        entry.elem_base_hi = elem_base_hi;
        entry.elem_qmin = elem_qmin;
        entry.elem_qdelta = elem_qdelta;
        entry.elem_qcount = elem_qcount;
        entry.elem_max = elem_max;
    }
    return true;
}

// TableMessageShapeRead is one shape, by the kind that names it (§3.3's shape
// table). Every number in it is a canonical LEB128 except where the row says
// otherwise: a RANGED BASE IS ENCODED BY ITS KIND'S SIGNEDNESS, zigzag for the
// signed kinds, unsigned for the unsigned kinds and sixteen bytes for the
// 128-bit and fixed-point kinds, and a QUANTIZED f32 carries min, max and res
// as float32, from which the step count and the width derive by SPEC.md
// §4.3's rule and by nothing else.
inline bool TableMessageShapeRead( const uint8_t * in, int64_t size, int64_t & at, uint8_t kind, TableMessageShapeFacts f )
{
    uint64_t v = 0;
    if ( TableMessageIntegerKind( kind ) || TableMessageFixedKind( kind ) || kind == 10 )
    {
        if ( at >= size ) { return false; }
        f.packing = in[ at++ ];
        if ( f.packing == 0 ) { return true; }
        if ( f.packing == 1 && kind != 10 )
        {
            if ( !TableMessageLeb( in, size, at, v ) || (int64_t) v > TableMessageKindBits( kind ) ) { return false; }
            f.value_bits = (int64_t) v;
            if ( kind == 18 || kind == 19 || TableMessageFixedKind( kind ) )
            {
                if ( at + 16 > size ) { return false; }
                uint64_t lo = 0, hi = 0;
                for ( int i = 0; i < 8; i++ ) { lo |= uint64_t( in[ at + i ] ) << ( 8 * i ); }
                for ( int i = 0; i < 8; i++ ) { hi |= uint64_t( in[ at + 8 + i ] ) << ( 8 * i ); }
                f.base_lo = (int64_t) lo; f.base_hi = (int64_t) hi;
                at += 16;
                return true;
            }
            if ( !TableMessageLeb( in, size, at, v ) ) { return false; }
            if ( kind >= 2 && kind <= 5 ) { f.base_lo = (int64_t) ( v >> 1 ) ^ -(int64_t) ( v & 1 ); } // zigzag
            else { f.base_lo = (int64_t) v; }                                                       // the unsigned domain, whole
            return true;
        }
        if ( f.packing == 2 && kind == 10 )
        {
            if ( at + 12 > size ) { return false; }
            uint32_t raw[3] = { 0, 0, 0 };
            for ( int k = 0; k < 3; k++ ) { for ( int i = 0; i < 4; i++ ) { raw[k] |= uint32_t( in[ at + 4 * k + i ] ) << ( 8 * i ); } }
            at += 12;
            memcpy( &f.qmin, &raw[0], 4 );
            memcpy( &f.qmax, &raw[1], 4 );
            memcpy( &f.qres, &raw[2], 4 );
            return TableMessageQuantization( f.qmin, f.qmax, f.qres, f.qdelta, f.qcount, f.value_bits );
        }
        return false; // a packing outside the closed set
    }
    // A MAX ABOVE WHAT THE KIND CAN HOLD IS A HOSTILE WIDTH (§3.3). A string
    // and a wide string are bounded by the int32 storage cap the checker
    // applies to every N (SPEC §4.3, §6.1), and an array and a keyed entry by
    // the 32-bit count an unbounded array announces (§2.9), which is the
    // widest count this form spells. A larger bound is a shape no conforming
    // declaration can produce, and a reader that carried it would do its
    // length arithmetic in a range that overflows.
    if ( kind == 12 || kind == 33 )
    {
        if ( !TableMessageLeb( in, size, at, v ) || v > (uint64_t) INT32_MAX ) { return false; }
        f.max = (int64_t) v;
        return true;
    }
    if ( kind == 14 || kind == 16 )
    {
        if ( kind == 14 )
        {
            if ( !TableMessageLeb( in, size, at, v ) || v > kTableMessageListMax ) { return false; }
            f.min = (int64_t) v;
        }
        if ( !TableMessageLeb( in, size, at, v ) || v > kTableMessageListMax ) { return false; }
        if ( (int64_t) v < f.min ) { return false; }
        f.max = (int64_t) v;
        if ( at >= size ) { return false; }
        f.elem_kind = in[ at++ ];
        if ( !TableMessageKnownKind( f.elem_kind ) ) { return false; }
        // AND AN ELEMENT KIND OF 12 OR 33 IS REFUSED HERE, at the
        // announcement, rather than at the skip that would meet it (§3.3): no
        // declaration this language accepts is an array of string(N) or of
        // wstring(N), so a shape announcing one is one rule's business and not
        // two.
        if ( f.elem_kind == 12 || f.elem_kind == 33 ) { return false; }
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
// an announcement carried, RESOLVED ONCE, under one numbering.
//
// THE RECEIVER RESOLVES ONCE (§3.3), so this holds the entries themselves and
// not the announcement's bytes: every entry is parsed at AnnounceRead, and
// every body after it dispatches through ONE ARRAY INDEX with nothing to
// re-read and nothing to decide. The announcement is free the moment
// AnnounceRead returns.
//
// THE STORAGE IS THE CALLER'S and this library never allocates. The caller
// declares an array of entries wherever it wants it — static, on a heap, in
// an arena, beside its connection — and hands it here with its CAPACITY. The
// announcement holds for the life of the connection (§3.3), so the array does
// too, and a peer holds TWO for a connection, the one it writes with and the
// one it reads with. A restart opens a fresh connection with an empty
// vocabulary and nothing is cached across connections.
//
// kTableMessageEntriesHere is the capacity a receiver that talks only to peers
// of THIS schema declares, and a receiver meeting other builds declares more.
struct TableVocabulary
{
    // THE CONFORMING DEFAULT BYTE BOUND (§3.3). The ENTRY bound has no default
    // because it IS the caller's capacity: an announcement naming more entries
    // than the caller made room for is refused as vocabulary_too_large before
    // an entry is touched, and the byte bound is read off the vocabulary
    // field's own length before that.
    static const int64_t kDefaultMaxBytes = 64 * 1024;

    TableVocabulary( TableMessageEntry * storage, int64_t capacity )
        : entries( storage ), max_entries( capacity ) {}

    TableMessageEntry * entries; // THE CALLER'S, capacity max_entries
    int64_t max_entries;
    int64_t count = 0;
    int64_t ref_bits = 0;
    uint64_t build_version = 0;
    bool announced = false;
    // REFUSAL IS TERMINAL (§3.3): a connection whose first announcement was
    // refused, for any reason, carries no vocabulary for its life, and every
    // announcement after it is refused as second_announcement
    bool refused = false;
    int64_t max_bytes = kDefaultMaxBytes;
};

// TableVocabularyEntryAt is the entry a reference names, counted from 1: ONE
// ARRAY INDEX into the caller's resolved storage, no parse and no branch.
inline const TableMessageEntry & TableVocabularyEntryAt( const TableVocabulary & vocabulary, uint64_t slot )
{
    return vocabulary.entries[ slot - 1 ];
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
// changes nothing. A refused announcement sets NO VOCABULARY, and the refusal
// is TERMINAL: every announcement after it, whether or not the first set
// anything, is second_announcement, so a peer holds no retry on the
// connection and cannot buy a second resolve by having its first refused.
inline bool AnnounceReadOnce( TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * to );

inline bool AnnounceRead( TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    TableReport ignored;
    TableReport * to = report != NULL ? report : &ignored;
    if ( vocabulary.announced || vocabulary.refused )
    {
        to->refused = true;
        to->reason = second_announcement;
        return false;
    }
    const bool set = AnnounceReadOnce( vocabulary, buffer, bytes, to );
    if ( !set ) { vocabulary.refused = true; }
    return set;
}

inline bool AnnounceReadOnce( TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * to )
{
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
            if ( kind != 9 || !r.has( 8 ) ) { to->malformed = true; return false; }
            version = r.get64();
            // THE BUILD VERSION IS KEPT THE MOMENT IT IS READ, refusal or not, so
            // that a refusal on this connection NAMES IT (§3.3). It is not the
            // vocabulary, and a refused announcement still sets none.
            vocabulary.build_version = version;
            seen_version++;
            continue;
        }
        if ( id == kTableMessageVocabularyFieldId )
        {
            // kind 14 over element kind 6, which is §3's spelling for an
            // opaque run of bytes
            uint64_t framed = 0;
            if ( kind != 14 || !r.getleb( framed ) || !r.has( (int64_t) framed ) ) { to->malformed = true; return false; }
            const int64_t begin = r.offset, end = r.offset + (int64_t) framed;
            r.offset = end;
            if ( begin >= end || r.buffer[ begin ] != 6 ) { to->malformed = true; return false; }
            int64_t at = begin + 1;
            uint64_t length = 0;
            if ( !TableMessageLeb( r.buffer, end, at, length ) || at + (int64_t) length != end ) { to->malformed = true; return false; }
            if ( (int64_t) length > vocabulary.max_bytes ) { to->refused = true; to->reason = vocabulary_too_large; return false; }
            words = r.buffer + at;
            words_bytes = (int64_t) length;
            seen_vocabulary++;
            continue;
        }
        to->unknown++;
        if ( !r.skip( kind ) ) { to->malformed = true; return false; }
    }
    if ( seen_version != 1 || seen_vocabulary != 1 ) { to->malformed = true; return false; }

    // THE ENTRIES, RESOLVED ONCE into the caller's storage (§3.3): every width
    // is checked here and never again, and no body after this parses a byte of
    // an announcement. An entry count above the caller's CAPACITY is refused
    // by name before the entry is touched.
    int64_t at = 0, count = 0, node_table_slots = 0;
    while ( at < words_bytes )
    {
        if ( count >= vocabulary.max_entries ) { to->refused = true; to->reason = vocabulary_too_large; return false; }
        TableMessageEntry & parsed = vocabulary.entries[ count ];
        if ( !TableMessageEntryRead( words, words_bytes, at, parsed ) ) { to->malformed = true; return false; }
        // THE RESERVED IDS WHERE THEY DO NOT BELONG (§3.3): the announcement's
        // own two never take a slot, and the node-table id takes exactly one,
        // so a vocabulary carrying either of the first or a SECOND node-table
        // id is malformed whole and sets nothing
        if ( parsed.id == kTableBuildVersionFieldId || parsed.id == kTableMessageVocabularyFieldId ) { to->malformed = true; return false; }
        if ( parsed.id == kTableNodeTableFieldId ) { if ( node_table_slots++ > 0 ) { to->malformed = true; return false; } }
        // A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE, so two entries that
        // agree on the id, the kind and every fact of the shape are malformed
        // (§3.3): no writer this wire has produces one, and a reader that took
        // it would carry two slots naming one thing. The scan is quadratic in
        // the entry count, and the entry count is bounded above at 4096, so it
        // is at most eight million compares on a path that runs ONCE a
        // connection and never again.
        for ( int64_t seen = 0; seen < count; seen++ )
        {
            const TableMessageEntry & other = vocabulary.entries[ seen ];
            if ( other.id == parsed.id && other.kind == parsed.kind && TableMessageEntrySame( other, parsed ) ) { to->malformed = true; return false; }
        }
        count++;
    }
    vocabulary.count = count;
    vocabulary.ref_bits = TableBitsRequired( 0, count );
    vocabulary.build_version = version;
    vocabulary.announced = true;
    return true;
}

// TableMessageReserved is one of the three ids the language holds back (§3.1,
// §3.3, §5): each is malformed anywhere but its own transport, and the rule
// OUTRANKS the wrong-sort rule below.
inline bool TableMessageReserved( uint64_t id )
{
    // THE THREE ARE THE TOP THREE VALUES a uint64 holds, so the test is ONE
    // comparison: 0xFFFFFFFFFFFFFFFD, FE and FF and nothing else is at or
    // above the vocabulary's own id, and a declaration hashing to any of them
    // is refused by name (§11)
    return id >= kTableMessageVocabularyFieldId;
}

// TableMessageNameEntry resolves a reference used as a VALUE — an enum's
// variant, a keyed array's slot key, a node record's type id — which must
// name a kind-0 entry (§3.3). A reference of 0 where an entry is required, one
// above E, one naming a reserved id and one naming an entry that carries a
// payload are each damage: the reader RESOLVED the entry and it contradicts
// the position it was used in, so the next bit's meaning is what is in doubt.
inline bool TableMessageNameEntry( const TableVocabulary & vocabulary, uint64_t ref, TableMessageEntry & entry )
{
    if ( ref == 0 || ref > (uint64_t) vocabulary.count ) { return false; }
    entry = TableVocabularyEntryAt( vocabulary, ref );
    return !TableMessageReserved( entry.id ) && entry.kind == 0;
}

// TableMessageArmEntry resolves a UNION's arm reference, which must name an
// entry carrying the arm's own kind and shape: a kind-0 entry frames nothing,
// and a reserved id belongs to no arm (§3.3).
inline bool TableMessageArmEntry( const TableVocabulary & vocabulary, uint64_t ref, TableMessageEntry & entry )
{
    if ( ref == 0 || ref > (uint64_t) vocabulary.count ) { return false; }
    entry = TableVocabularyEntryAt( vocabulary, ref );
    return !TableMessageReserved( entry.id ) && entry.kind != 0;
}

// TableMessageSkipVariant steps over an ENUM's variant reference on a SKIP
// path and RESOLVES it while it is there: 0 is None and the whole payload, and
// every other reference must name a kind-0 entry, because every reference
// above E is damage and one naming an entry that carries a payload
// contradicts the position it was used in, whether or not this reader was
// going to keep the value (§3.3).
inline bool TableMessageSkipVariant( TableBitReader & r, const TableVocabulary & vocabulary )
{
    uint64_t ref = 0;
    if ( !r.get( ref, vocabulary.ref_bits ) ) { return false; }
    if ( ref == 0 ) { return true; }
    TableMessageEntry named;
    return TableMessageNameEntry( vocabulary, ref, named );
}
// TableMessageSkip steps over one field's payload without decoding it, using
// the announced ENTRY alone (§3.3). It is what makes an unknown entry
// skippable on a body with no kind byte, and it is ONE function over every
// table, because a shape says everything a skipper needs.
inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits );
inline bool TableMessageSkip( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, const TableMessageEntry & entry );

// TableMessageSkipElement steps over ONE element of an array or keyed entry
// by the element's own announced shape: a nested body to its zero reference,
// a variant or a node index at its reference width, a union arm by its own
// entry, and a fixed-width value at its bits.
inline bool TableMessageSkipElement( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, const TableMessageEntry & entry )
{
    switch ( entry.elem_kind )
    {
        case 13: return TableMessageSkipBody( r, vocabulary, index_bits );
        case 30: return TableMessageSkipVariant( r, vocabulary );
        case 17: return index_bits > 0 && r.skip( index_bits );
        case 15:
        {
            TableMessageEntry inner;
            inner.kind = 15;
            return TableMessageSkip( r, vocabulary, index_bits, inner );
        }
        default:
        {
            const int64_t elem = entry.elem_value_bits;
            return elem >= 0 && r.skip( elem );
        }
    }
}

// TableMessageElementRunBits is the bits ONE element of an array or a keyed
// entry occupies on the SKIP path, where nothing is resolved and a run of them
// is one multiplication, and -1 where the element's width is its own
// content's. A ZERO is a real answer, and it is why this exists: a ranged
// element whose min equals its max rides no bits at all (§3.3).
inline int64_t TableMessageElementRunBits( const TableVocabulary & vocabulary, const TableMessageEntry & entry )
{
    int64_t elem = 0;
    switch ( entry.elem_kind )
    {
        // a nested body, a union arm, an enum's variant and a node index each
        // RESOLVE something, and a resolve that contradicts its position is
        // damage this reader must still find, so they are walked
        case 13: case 15: case 30: case 17: return -1;
        default: elem = entry.elem_value_bits; break;
    }
    if ( elem < 0 ) { return -1; }
    if ( entry.kind == 16 ) { elem += vocabulary.ref_bits; } // a keyed slot's own key reference
    return elem;
}

// TableMessageSkipRun steps over n elements of one fixed width in a single
// arithmetic step. A FIXED-WIDTH ELEMENT IS ARITHMETIC (§3.3), and a loop here
// would be the one superlinear thing in this form: a zero-width element under
// a count of 2^31 is six bytes of wire.
inline bool TableMessageSkipRun( TableBitReader & r, uint64_t n, int64_t width )
{
    if ( width < 0 ) { return false; }
    if ( width == 0 ) { return true; }
    if ( n > (uint64_t) ( INT64_MAX / width ) ) { return false; }
    return r.skip( (int64_t) ( n * (uint64_t) width ) );
}

inline bool TableMessageSkip( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, const TableMessageEntry & entry )
{
    switch ( entry.kind )
    {
        case 0: case 32: return true;              // a name, and a payload-free arm
        case 30: return TableMessageSkipVariant( r, vocabulary );
        case 13: return TableMessageSkipBody( r, vocabulary, index_bits );
        case 17: return index_bits > 0 && r.skip( index_bits ); // a node index, at the width the body's node count settled
        case 15:
        {
            uint64_t arm = 0;
            if ( !r.get( arm, vocabulary.ref_bits ) ) { return false; }
            if ( arm == 0 ) { return true; }
            TableMessageEntry arm_entry;
            if ( !TableMessageArmEntry( vocabulary, arm, arm_entry ) ) { return false; }
            return TableMessageSkip( r, vocabulary, index_bits, arm_entry );
        }
        case 12:
        {
            uint64_t n = 0;
            if ( !r.get( n, TableBitsRequired( 0, entry.max ) ) || !r.align() ) { return false; }
            return r.skip( (int64_t) n * 8 );
        }
        case 33:
        {
            // the length, NO align, then SIXTEEN bits a code unit (§3.3)
            uint64_t n = 0;
            if ( !r.get( n, TableBitsRequired( 0, entry.max ) ) ) { return false; }
            return r.skip( (int64_t) n * 16 );
        }
        case 31:
        {
            // THE ESCAPE: align, a thirty-two bit L, then L bytes, opaque — the one
            // path a later-major writer has on this form (§3.3)
            uint64_t n = 0;
            if ( !r.align() || !r.get( n, 32 ) ) { return false; }
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
            // A RUN OF FIXED-WIDTH ELEMENTS IS ONE MULTIPLICATION (§3.3), and
            // only an element whose width is its own content's is walked
            const int64_t run = TableMessageElementRunBits( vocabulary, entry );
            if ( run >= 0 ) { return TableMessageSkipRun( r, n, run ); }
            for ( uint64_t i = 0; i < n; i++ )
            {
                if ( entry.kind == 16 && !r.skip( vocabulary.ref_bits ) ) { return false; }
                if ( !TableMessageSkipElement( r, vocabulary, index_bits, entry ) ) { return false; }
            }
            return true;
        }
    }
    const int64_t width = entry.value_bits;
    return width >= 0 && r.skip( width );
}

inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits )
{
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.get( ref, vocabulary.ref_bits ) ) { return false; }
        if ( ref == 0 ) { return true; }
        if ( ref > (uint64_t) vocabulary.count ) { return false; }
        if ( !TableMessageSkip( r, vocabulary, index_bits, TableVocabularyEntryAt( vocabulary, ref ) ) ) { return false; }
    }
}

// TableMessageNodeTableOpen reads the node table's opening when a body has
// one: the reserved id's reference and the count at thirty-two raw bits. A
// body whose first reference is anything else has no node table, and the
// reader is left where it was. False is damage: a reference past E, or bits
// that run out.
inline bool TableMessageNodeTableOpen( TableBitReader & r, const TableVocabulary & vocabulary, int64_t & count )
{
    count = 0;
    const int64_t at = r.offset;
    uint64_t ref = 0;
    if ( !r.get( ref, vocabulary.ref_bits ) ) { return false; }
    if ( ref == 0 ) { r.offset = at; return true; }
    if ( ref > (uint64_t) vocabulary.count ) { return false; }
    if ( TableVocabularyEntryAt( vocabulary, ref ).id != kTableNodeTableFieldId ) { r.offset = at; return true; }
    uint64_t n = 0;
    if ( !r.get( n, 32 ) ) { return false; }
    count = (int64_t) n;
    return true;
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
    // THE SINK A CALLER THAT PASSED NO REPORT WRITES INTO IS THE READER'S OWN,
    // not a static: a static is shared mutable state, and two threads reading
    // two batches without reports would be writing one object. LoadMessages
    // already keeps its sink locally, for the same reason.
    TableReport ignored;
};

// TableMessageBatchOpen answers the batch's body count, or -1 with the refusal
// on the report: a form byte this reader does not carry, or a body from a peer
// that never announced.
inline int64_t TableMessageBatchOpen( TableMessageBatchReader & br, const TableVocabulary & vocabulary, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    br.report = report != NULL ? report : &br.ignored;
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

// TableMessageRefuseBatch is the batch's own refusal (§3.3): M above 256 on the
// write side, or above the caller's capacity on the read side. Nothing is
// written or decoded, no counter moves, and the reason names it.
inline void TableMessageRefuseBatch( TableReport * report )
{
    if ( report == NULL ) { return; }
    report->refused = true;
    report->reason = batch_too_large;
}

// TableMessageBatchClose verifies the trailing pad, and that NOTHING FOLLOWS
// IT: the batch ends at the pad to the byte boundary, and a buffer with bytes
// left over describes no batch this reader can name (§3.3).
inline bool TableMessageBatchClose( TableMessageBatchReader & br )
{
    if ( br.remaining != 0 || !br.r.align() || br.r.offset != br.r.bits ) { br.report->malformed = true; return false; }
    return true;
}

`

// cppMessageNodeRuntime is the node table on the MESSAGE wire, over the
// numbering the arena runtime declares: emitted after it, into a unit that has
// pointers at all.
const cppMessageNodeRuntime = `// ---- the NODE TABLE on the message wire (docs/SPEC-TABLES.md §3.1, §3.3) ----
//
// THE NODE TABLE, WHEN A BODY HAS ONE, IS THE FIRST FIELD OF THE ROOT BODY: the
// reserved id as a reference, the node count at THIRTY-TWO RAW BITS, then the
// records back to back, each a type id reference and a body — a table's fields
// ending at its own zero reference, a blob's length, align and bytes. A root
// that reaches no node elides the field, like every other empty thing.
//
// Measure derives the numbering from the graph and save derives the same one,
// and the two thunks stored at numbering time are what let one loop write a
// table of mixed types.
template <typename Ctx>
inline int64_t TableMessageNodeTableMeasure( const Ctx & ctx, const TableNumbering & n, int64_t index_bits, int64_t at )
{
    if ( n.count == 0 ) { return 0; } // a root that reaches no nodes writes none of them
    int64_t bits = kTableMessageRefBitsHere + 32;
    for ( int64_t k = 0; k < n.count; k++ )
    {
        bits += kTableMessageRefBitsHere;
        const int64_t body = n.entries[k].message_measure( (const void *) &ctx, n, index_bits, at + bits, n.entries[k].node );
        if ( body < 0 ) { return -1; }
        bits += body;
    }
    return bits;
}

template <typename Ctx>
inline bool TableMessageNodeTableSave( const Ctx & ctx, const TableNumbering & n, int64_t index_bits, TableBitWriter & w )
{
    if ( n.count == 0 ) { return true; }
    w.put( kTableNodeTableFieldSlot, kTableMessageRefBitsHere );
    w.put( (uint64_t) n.count, 32 );
    for ( int64_t k = 0; k < n.count; k++ )
    {
        w.put( n.entries[k].type_slot, kTableMessageRefBitsHere );
        if ( !n.entries[k].message_save( (const void *) &ctx, n, index_bits, w, n.entries[k].node ) ) { return false; }
    }
    return !w.overflow;
}

`
