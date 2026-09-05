// THE MESSAGE FORM's C++ runtime (docs/SPEC-TABLES.md §3.3): the unit's
// announcement as a compile-time constant, the connection table a receiver
// reads it into, and the tolerant read with its one strict check.
//
// A file carries its own id table and a MESSAGE STREAM announces one and then
// carries none, so everything here is about WHERE the table lives. The body's
// framing, its elision rules and every malformed rule of §3 are untouched.
package cpptable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableMessageForm emits the shared, per-package half of the message form: the
// form byte, the reserved build-version id, the ANNOUNCEMENT as a constant
// byte array and its length, the connection table, and the three unit-scope
// entry points Announce, AnnounceMeasure and AnnounceRead.
//
// THE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT and the C++
// reference emits it as one, which §3.3 licenses in so many words: every byte
// of it is settled by the compiler, so a walk would compute at run time what
// the emitter already knows. Announce is a copy and AnnounceMeasure is a
// constant.
func tableMessageForm(u *ir.Unit, forceInline string, anyVariable bool) string {
	announcement := ir.TableAnnouncement(u)
	vocabulary := ir.TableVocabulary(u)

	var b strings.Builder
	b.WriteString(`// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): a FILE carries its own id
// table and a MESSAGE STREAM announces one and then carries none.
//
// A form 2 wire is TWO PARTS, the form byte and the root body: the body ends
// at its own zero reference as it does in a file, there is no trailer, and the
// message's last byte is the body's terminator. Its references resolve against
// the CONNECTION's table, which is the unit's whole vocabulary in the order
// the compiler settled.
const uint8_t kTableWireMessageForm = 2;

// The RESERVED build-version id, the second id the language holds back (§5,
// §11), beside the node table's. It is the announcement's one required field,
// and a reserved id in any body but the one whose transport it is, is
// malformed (§3.1).
static const uint64_t kTableBuildVersionFieldId = 0xFFFFFFFFFFFFFFFEull;

// The RESERVED records id, the third the language holds back (§5, §11). Its
// payload is the per-entry RECORDS: one fixed-width record a slot, saying what
// a field header under that id spells, which is what lets a reader skip an id
// it cannot name on a body that has no kind byte.
static const uint64_t kTableMessageRecordsFieldId = 0xFFFFFFFFFFFFFFFDull;

`)
	if anyVariable {
		// THE NODE TABLE's OWN SLOT, in a unit that HAS a node table and in no
		// other. The reserved node-table ID rides in every unit, because every
		// reader owes §3.1's refusal of it inside a nested body; the SLOT is
		// the writer's half and only a pointered message ever names it, so a
		// value-only unit carries none of it and the zero-cost gate holds
		// (docs/SPEC-TABLES.md §2, §3.1, §3.3).
		fmt.Fprintf(&b, `// The reserved NODE-TABLE id's own slot in this unit's vocabulary (§3.3). A
// pointered message names the node table through it, exactly as every other
// field header names its id through a slot.
static const uint64_t kTableNodeTableFieldSlot = %d;

`, slotOfIn(vocabulary, ir.TableNodeWireId))
	}

	fmt.Fprintf(&b, `// The per-entry RECORD's stride, and this unit's own REFERENCE WIDTH — the
// bits a writer spends on every reference of every body it writes, which is a
// compile-time constant because the vocabulary is (§3.3). A READER spends the
// width the SENDER's table settles, which is the announcement's own count.
static const int64_t kTableMessageRecordBytes = %d;
static const int64_t kTableMessageRefBitsHere = %d;

`, ir.TableMessageRecordBytes, ir.TableMessageRefBits(len(vocabulary)))

	fmt.Fprintf(&b, `// THE UNIT'S ANNOUNCEMENT, byte for byte: %d entries and %d bytes. It is an
// ordinary form 1 FILE — the form byte, a body carrying the BUILD VERSION
// under the reserved id at kind 9, and the trailer that IS the connection's
// table, slot 1 the reserved id and slots 2 and up the vocabulary under one
// numbering.
//
// The vocabulary is the unit's whole closure in the COOK PROJECTION's order
// (§20.2) — each record in the order the projection renders it and each
// record's fields in the order the projection renders them, then each enum's
// variants and each union's arms — followed by the tail the projection does
// not name: the reserved node-table id, the three blob type ids as bytes,
// string and wstring, and every table's own name id in the projection's sorted
// record order. The tail is UNCONDITIONAL, so an ordinary edit only ever grows
// it at its end and never moves a slot a generated field header carries as a
// literal.
static const int64_t kTableAnnounceBytes = %d;
static const uint8_t kTableAnnounce[ kTableAnnounceBytes ] = {
`, len(vocabulary), len(announcement), len(announcement))
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
	b.WriteString(`};

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

// THE BIT STREAM the message form's bodies ride on (§3.3). It is the packet
// wire's own layout — bit i of the stream lives in byte i/8 at bit position
// i%8, low bit first — so a value written here and a value written by a
// generated packet writer are the same bits in the same places.
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

    // one unbounded number as a BIT LEB128: seven-bit groups, low group
    // first, each preceded by its continuation bit, in one canonical spelling
    void putleb( uint64_t value )
    {
        for ( ;; )
        {
            const uint64_t group = value & 0x7F;
            value >>= 7;
            put( value != 0 ? 1 : 0, 1 );
            put( group, 7 );
            if ( value == 0 ) { return; }
        }
    }

    void putbytes( const uint8_t * data, int64_t n )
    {
        for ( int64_t i = 0; i < n; i++ ) { put( (uint64_t) data[i], 8 ); }
    }

    // the ONE alignment this form spends, at a batch's end and nowhere else
    void align() { while ( ( bits % 8 ) != 0 ) { put( 0, 1 ); } }
};

// TableLebBits is one number's BIT LEB128 width, which a measure spends
// without writing anything.
inline int64_t TableLebBits( uint64_t value )
{
    int64_t n = 8;
    while ( value >= 0x80 ) { value >>= 7; n += 8; }
    return n;
}

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

    // A NON-CANONICAL SPELLING IS FRAMING DAMAGE, which is what §3 already
    // says of every length, count and index on the file form.
    bool getleb( uint64_t & value )
    {
        value = 0;
        for ( int64_t shift = 0; ; shift += 7 )
        {
            uint64_t more = 0, group = 0;
            if ( !get( more, 1 ) || !get( group, 7 ) ) { return false; }
            if ( shift >= 64 || ( shift == 63 && group > 1 ) ) { return false; }
            value |= group << (uint64_t) shift;
            if ( more == 0 ) { return !( shift > 0 && group == 0 ); }
        }
    }

    bool skip( int64_t n ) { if ( !has( n ) ) { return false; } offset += n; return true; }
};

// THE PER-ENTRY RECORD (§3.3): what a field header under one id spells, in
// enough detail that a reader which cannot NAME the id skips it at bit
// granularity. The record is the WIRE CONTRACT for its id and both halves
// write to it, so one name declared at two bounds in two records rides at the
// widest of them rather than at either.
struct TableMessageRecord
{
    uint8_t kind;        // the §3 kind a field header under this id carries, 0 for a name that is never a field
    uint8_t elem_kind;   // an array's or a keyed array's element kind
    uint8_t length_bits; // the width of the length or count this kind frames itself with
    uint8_t value_bits;  // the width of one value at its canonical range
    uint8_t flags;
    int64_t min;         // the range base: the wire carries value - min
};

// the record FLAGS (§3.3)
static const uint8_t kTableMessageAmbiguous = 1; // one id, two kinds: no shape spells both
static const uint8_t kTableMessageLebLength = 2; // a length whose bound the declaration does not state
static const uint8_t kTableMessageBitLength = 4; // a length that counts BITS, which only the node table's does

// the records are a FIXED-STRIDE array, so record k is an index and never a
// search
inline TableMessageRecord TableMessageRecordAt( const uint8_t * records, uint64_t slot )
{
    const uint8_t * at = records + ( slot - 1 ) * kTableMessageRecordBytes;
    TableMessageRecord rec;
    rec.kind = at[0]; rec.elem_kind = at[1]; rec.length_bits = at[2]; rec.value_bits = at[3]; rec.flags = at[4];
    uint64_t lo = uint64_t( at[5] ) | uint64_t( at[6] ) << 8 | uint64_t( at[7] ) << 16 | uint64_t( at[8] ) << 24;
    uint64_t hi = uint64_t( at[9] ) | uint64_t( at[10] ) << 8 | uint64_t( at[11] ) << 16 | uint64_t( at[12] ) << 24;
    rec.min = (int64_t) ( lo | ( hi << 32 ) );
    return rec;
}

// TableMessageRefBits is the width of a REFERENCE against a table of E
// entries: bits_required( 0, E ), the same function the packet wire uses for a
// ranged integer.
inline int64_t TableMessageRefBits( int64_t entries )
{
    if ( entries <= 0 ) { return 1; }
    int64_t n = 0;
    while ( entries > 0 ) { n++; entries >>= 1; }
    return n;
}

// TableMessageSigned is one signed value back from its raw bits: raw + min
// where the record carries a range base, and the two's complement of a value
// at the record's own width where it does not.
inline int64_t TableMessageSigned( uint64_t raw, const TableMessageRecord & rec )
{
    if ( rec.min != 0 ) { return (int64_t) raw + rec.min; }
    if ( rec.value_bits > 0 && rec.value_bits < 64 )
    {
        const uint64_t sign = uint64_t(1) << ( rec.value_bits - 1 );
        if ( ( raw & sign ) != 0 ) { return (int64_t) ( raw | ~( ( uint64_t(1) << rec.value_bits ) - 1 ) ); }
    }
    return (int64_t) raw;
}

// one length or count at the width the SENDER declared
inline bool TableMessageLength( TableBitReader & r, const TableMessageRecord & rec, uint64_t & n )
{
    if ( ( rec.flags & kTableMessageLebLength ) != 0 ) { return r.getleb( n ); }
    return r.get( n, rec.length_bits );
}

// TableVocabulary is ONE DIRECTION of ONE CONNECTION's id table (§3.3): the
// entries an announcement carried, whole, under one numbering with slot 1 the
// reserved build-version id.
//
// A peer holds TWO of these for a connection, the one it writes with and the
// one it reads with, and neither is the other's. A restart opens a fresh
// connection with empty tables and nothing is cached across connections, so
// its whole life is one connection's. It BORROWS the announcement's bytes rather than
// copying them, so a receiver holds one table a direction and its memory is
// the bound below and nothing else.
struct TableVocabulary
{
    // THE CONFORMING DEFAULT BOUND (§3.3): 32 KiB a direction, eight times the
    // 500-id unit that is already a large one. A connection's table is bounded
    // by nothing the wire carries, so the receiver declares the maximum and an
    // announcement above it is refused by name before an entry is touched.
    static const int64_t kDefaultMaxEntries = 4096;

    TableIdTable table;
    // THE RECORDS the announcement carried, borrowed like the entries: one
    // fixed-width record a slot, and the reference width the entry count
    // settles. A body has no kind byte, so these are what a reader spends and
    // what it skips an id it cannot name by.
    const uint8_t * records = NULL;
    int64_t ref_bits = 0;
    uint64_t build_version = 0;
    bool announced = false;
    int64_t max_entries = kDefaultMaxEntries;
};

// AnnounceRead reads an announcement into one direction's table (§3.3).
//
// THE BOUND IS CHECKED BEFORE ANYTHING IS ALLOCATED: the entry count is a
// fixed little-endian u64 at the end, so a receiver reads it, compares it and
// refuses without touching an entry. After that it is §3's ordinary FILE read,
// because the announcement IS a file, with EXACTLY ONE STRICT CHECK over its
// body: the reserved build-version field present, exactly once, under kind 9,
// eight bytes wide. Everything else is an ordinary field under §4's tolerance,
// so an unknown one is skipped and counted and the announcement can GAIN a
// field in a later minor without a lockstep redeploy.
//
// The FIRST announcement sets the table and it is the only one that can. A
// SECOND is refused by name: it does not replace the table, it does not amend
// it and it changes nothing. A refused announcement sets NO TABLE.
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
    const uint8_t * tail = buffer + bytes - 8;
    uint64_t lo = uint64_t( tail[0] ) | uint64_t( tail[1] ) << 8 | uint64_t( tail[2] ) << 16 | uint64_t( tail[3] ) << 24;
    uint64_t hi = uint64_t( tail[4] ) | uint64_t( tail[5] ) << 8 | uint64_t( tail[6] ) << 16 | uint64_t( tail[7] ) << 24;
    if ( ( lo | ( hi << 32 ) ) > (uint64_t) vocabulary.max_entries )
    {
        to->refused = true;
        to->reason = vocabulary_too_large;
        return false;
    }
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
    // the body, under §4's tolerance and this form's one strict check
    TableReader r( buffer + 1, body_bytes, to, &table );
    uint64_t version = 0;
    const uint8_t * records = NULL;
    int32_t seen = 0, seen_records = 0;
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
            seen++;
            continue;
        }
        if ( id == kTableMessageRecordsFieldId )
        {
            // THE SECOND STRICT CHECK: the records present, exactly once, at
            // kind 12, carrying exactly one fixed-width record an entry. A
            // bitpacked body has no kind byte, so a table with no records is a
            // table nothing can be read against.
            uint64_t records_bytes = 0;
            if ( kind != 12 || !r.getleb( records_bytes ) || records_bytes != (uint64_t) ( kTableMessageRecordBytes * table.count ) || !r.has( (int64_t) records_bytes ) )
            {
                to->refused = true; to->reason = no_vocabulary; return false;
            }
            records = buffer + 1 + r.offset;
            r.offset += (int64_t) records_bytes;
            seen_records++;
            continue;
        }
        to->unknown++;
        if ( !r.skip( kind ) ) { to->malformed = true; return false; }
    }
    if ( seen != 1 || seen_records != 1 ) { to->refused = true; to->reason = no_vocabulary; return false; }
    vocabulary.table = table;
    vocabulary.records = records;
    vocabulary.ref_bits = TableMessageRefBits( table.count );
    vocabulary.build_version = version;
    vocabulary.announced = true;
    return true;
}

// TableMessageSkip steps over one field's payload without decoding it, using
// the announced RECORD alone (§3.3). It is what makes an unknown id skippable
// on a body that carries no kind byte, and it is ONE function over every
// table, because a record says everything a skipper needs.
//
// An AMBIGUOUS entry cannot be skipped and neither can one that names no field
// shape: the body stops there, one malformed counts, and the fields decoded
// before it stand, which is §3's framing damage at the level it occurs.
inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & v );

inline bool TableMessageSkip( TableBitReader & r, const TableVocabulary & v, const TableMessageRecord & rec )
{
    if ( ( rec.flags & kTableMessageAmbiguous ) != 0 || rec.kind == 0 ) { return false; }
    switch ( rec.kind )
    {
        case 32: return true;                          // a payload-free arm carries nothing at all
        case 30: return r.skip( v.ref_bits );          // an enum is one reference
        case 13: return TableMessageSkipBody( r, v );  // a nested body is self-delimiting
        case 15:                                       // a union: the arm reference, then the arm's own shape
        {
            uint64_t arm = 0;
            if ( !r.get( arm, v.ref_bits ) ) { return false; }
            if ( arm == 0 ) { return true; }
            if ( arm > (uint64_t) v.table.count ) { return false; }
            return TableMessageSkip( r, v, TableMessageRecordAt( v.records, arm ) );
        }
        case 12:                                       // an opaque payload: bytes, or the node table's bits
        {
            uint64_t n = 0;
            if ( !TableMessageLength( r, rec, n ) ) { return false; }
            if ( ( rec.flags & kTableMessageBitLength ) != 0 ) { return r.skip( (int64_t) n ); }
            return r.skip( (int64_t) n * rec.value_bits );
        }
        case 14: case 16:                              // an array, and a keyed array's pairs
        {
            uint64_t n = 0;
            if ( !TableMessageLength( r, rec, n ) ) { return false; }
            for ( uint64_t i = 0; i < n; i++ )
            {
                if ( rec.kind == 16 && !r.skip( v.ref_bits ) ) { return false; }
                switch ( rec.elem_kind )
                {
                    case 13: if ( !TableMessageSkipBody( r, v ) ) { return false; } break;
                    case 30: if ( !r.skip( v.ref_bits ) ) { return false; } break;
                    case 17: if ( !r.skip( 32 ) ) { return false; } break;
                    default: if ( !r.skip( rec.value_bits ) ) { return false; } break;
                }
            }
            return true;
        }
    }
    return r.skip( rec.value_bits );
}

inline bool TableMessageSkipBody( TableBitReader & r, const TableVocabulary & v )
{
    for ( ;; )
    {
        uint64_t ref = 0;
        if ( !r.get( ref, v.ref_bits ) ) { return false; }
        if ( ref == 0 ) { return true; }
        if ( ref > (uint64_t) v.table.count ) { return false; }
        if ( !TableMessageSkip( r, v, TableMessageRecordAt( v.records, ref ) ) ) { return false; }
    }
}

// THE PRIMITIVE IS A BATCH (docs/SPEC-TABLES.md §3.3): a number of messages in
// ONE buffer, one count and one continuous bit stream with no alignment
// between the bodies. A single message is the batch of one.
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

inline bool TableMessageBatchBegin( TableMessageBatch & batch, uint8_t * buffer, int64_t capacity, int64_t messages )
{
    if ( buffer == NULL || capacity < 1 || messages < 0 ) { return false; }
    buffer[0] = kTableWireMessageForm; // the FORM BYTE is the whole header (§3)
    batch.w = TableBitWriter( buffer + 1, capacity - 1 );
    batch.declared = messages;
    batch.written = 0;
    batch.w.putleb( (uint64_t) messages );
    return true;
}

// TableMessageBatchEnd pads to the next byte — the ONE alignment this form
// spends — and answers the whole batch's byte count, or -1.
inline int64_t TableMessageBatchEnd( TableMessageBatch & batch )
{
    if ( batch.written != batch.declared || batch.w.overflow ) { return -1; }
    batch.w.align();
    if ( batch.w.overflow ) { return -1; }
    return 1 + batch.w.bits / 8;
}

// TableMessageBatchBytes is a batch's byte count from its bodies' BIT count,
// which is what every <Root>MeasureMessages answers.
inline int64_t TableMessageBatchBytes( int64_t messages, int64_t body_bits )
{
    if ( body_bits < 0 ) { return -1; }
    int64_t bits = TableLebBits( (uint64_t) messages ) + body_bits;
    return 1 + ( bits + 7 ) / 8;
}

// The reading half. A batch is opened once and its bodies are then read in
// order, each into the storage the caller sized for it: which root a message
// is, is the APPLICATION's and never this wire's.
struct TableMessageBatchReader
{
    TableBitReader r;
    const TableVocabulary * vocabulary = NULL;
    TableReport * report = NULL;
    int64_t remaining = 0;
};

// TableMessageBatchOpen answers the batch's message count, or -1 with the
// refusal on the report: a form byte this reader does not carry, or a body
// from a peer that never announced.
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
    if ( !br.r.getleb( count ) ) { br.report->malformed = true; return -1; }
    br.remaining = (int64_t) count;
    return br.remaining;
}

`)
	_ = forceInline
	return b.String()
}

// slotOfIn is one id's slot in a vocabulary, counted from 1.
func slotOfIn(vocabulary []uint64, id uint64) uint64 {
	for i, have := range vocabulary {
		if have == id {
			return uint64(i + 1)
		}
	}
	return 0
}
