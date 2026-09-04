// THE HAND-BUILT WIRE, SHARED BY THE BATTERIES (docs/SPEC-TABLES.md §3).
//
// A forgery is a body a conforming WRITER could not have produced, and the
// point of one is the single thing that is wrong. So the framing around it is
// built the way §3 states it rather than spelled in magic bytes: the body names
// ids by REFERENCE, the builder interns them in first-use order, and `finish`
// lays out the form byte, the body and the id table the body actually named.
#ifndef SCHEMA_TEST_TABLE_WIRE_BUILDER_H
#define SCHEMA_TEST_TABLE_WIRE_BUILDER_H

#include <stdint.h>
#include <string.h>

// an independent implementation of the table-wire id — fnv1a64 over the name,
// no fold and no rebound (docs/SPEC-TABLES.md §5) — pinning the compiler's hash
// against a second implementation written from the spec alone. ONE HASH SERVES
// EVERY VOCABULARY: a field's id, an enum variant's, a union arm's and a
// table's own name id are all this.
static uint64_t field_id( const char * name )
{
    uint64_t h = 0xCBF29CE484222325ull;
    for ( const char * p = name; *p; ++p )
    {
        h ^= (uint64_t) (uint8_t) *p;
        h *= 0x00000100000001B3ull;
    }
    return h;
}

// THE EMPTY WIRE is ten bytes: the form byte, the root body's zero reference,
// and the eight-byte entry count of an id table with no entries (§3).
static const int64_t empty_wire_bytes = 10;


static void le16( uint8_t * p, uint16_t v ) { p[0] = (uint8_t) v; p[1] = (uint8_t) ( v >> 8 ); }
static void le32( uint8_t * p, uint32_t v ) { p[0] = (uint8_t) v; p[1] = (uint8_t) ( v >> 8 ); p[2] = (uint8_t) ( v >> 16 ); p[3] = (uint8_t) ( v >> 24 ); }
static void le64( uint8_t * p, uint64_t v ) { le32( p, (uint32_t) v ); le32( p + 4, (uint32_t) ( v >> 32 ) ); }

// ---- A HAND-BUILT WIRE, WRITTEN FROM THE GRAMMAR (docs/SPEC-TABLES.md §3) --
//
// A forgery in this file is a body a conforming WRITER could not have produced,
// and the point of every one of them is the one thing that is wrong. So the
// framing around it is built the way §3 states it rather than spelled in magic
// bytes: the body names ids by REFERENCE, the builder interns them in
// first-use order, and `finish` lays out the form byte, the body and the id
// table the body actually named.
struct WireBuilder
{
    uint8_t body[1 << 16];
    int64_t n;
    uint64_t ids[128];
    int32_t count;

    WireBuilder() : n( 0 ), count( 0 ) {}

    // the reference an id takes, appended on first use — reference k is the
    // table's kth entry, counted from 1
    uint64_t ref( uint64_t id )
    {
        for ( int32_t i = 0; i < count; i++ ) { if ( ids[i] == id ) { return (uint64_t) i + 1; } }
        ids[count++] = id;
        return (uint64_t) count;
    }
    uint64_t ref( const char * name ) { return ref( field_id( name ) ); }

    void u8( uint8_t v ) { body[n++] = v; }
    void raw( const void * data, int64_t bytes ) { memcpy( body + n, data, (size_t) bytes ); n += bytes; }
    void u16( uint16_t v ) { le16( body + n, v ); n += 2; }
    void u32( uint32_t v ) { le32( body + n, v ); n += 4; }
    void u64( uint64_t v ) { le64( body + n, v ); n += 8; }

    // one canonical unsigned LEB128: every length, count, index and reference
    void leb( uint64_t v )
    {
        while ( v >= 0x80 ) { u8( (uint8_t) v | 0x80 ); v >>= 7; }
        u8( (uint8_t) v );
    }
    // and its NON-MINIMAL spelling, which is what a control writes
    void leb_padded( uint64_t v, int32_t extra )
    {
        while ( v >= 0x80 ) { u8( (uint8_t) v | 0x80 ); v >>= 7; }
        u8( (uint8_t) v | 0x80 );
        for ( int32_t i = 1; i < extra; i++ ) { u8( 0x80 ); }
        u8( 0 );
    }

    // A LENGTH-SHAPED PAYLOAD: a canonical LEB128 length cannot be patched in
    // place, so one placeholder byte rides, the payload fills, and the payload
    // moves up when the length needs more room. What comes out is the one legal
    // spelling.
    int64_t open_len() { const int64_t at = n; u8( 0 ); return at; }
    void close_len( int64_t at )
    {
        const int64_t payload = n - at - 1;
        int64_t width = 1;
        for ( uint64_t v = (uint64_t) payload; v >= 0x80; v >>= 7 ) { width++; }
        if ( width > 1 ) { memmove( body + at + width, body + at + 1, (size_t) payload ); n += width - 1; }
        int64_t w = at;
        uint64_t v = (uint64_t) payload;
        while ( v >= 0x80 ) { body[w++] = (uint8_t) v | 0x80; v >>= 7; }
        body[w] = (uint8_t) v;
    }

    // SEED FROM A SAVED WIRE: the writer's own body minus its terminator, and
    // the id table it named — so a second occurrence splices in under the same
    // references the writer used, which is what a repeat is (§3).
    void seed( const uint8_t * wire, int64_t bytes )
    {
        uint64_t entries = 0;
        for ( int i = 7; i >= 0; i-- ) { entries = ( entries << 8 ) | wire[bytes - 8 + i]; }
        const int64_t first = bytes - (int64_t) entries * 8 - 8;
        count = (int32_t) entries;
        for ( int32_t i = 0; i < count; i++ )
        {
            uint64_t id = 0;
            for ( int k = 7; k >= 0; k-- ) { id = ( id << 8 ) | wire[first + i * 8 + k]; }
            ids[i] = id;
        }
        n = first - 1 - 1; // the body, minus its own zero reference
        memcpy( body, wire + 1, (size_t) n );
    }

    // a field header: the id reference and the kind byte, which is the whole of it
    void field( const char * name, uint8_t kind ) { leb( ref( name ) ); u8( kind ); }
    void field( uint64_t id, uint8_t kind ) { leb( ref( id ) ); u8( kind ); }
    void end() { u8( 0 ); } // the body ENDS AT ITS OWN ZERO REFERENCE

    // the whole file: the form byte, the body, then the entries and the fixed
    // u64 count the reader finds it from
    int64_t finish( uint8_t * out ) const
    {
        int64_t at = 0;
        out[at++] = 1; // the FORM BYTE is the whole header
        memcpy( out + at, body, (size_t) n ); at += n;
        for ( int32_t i = 0; i < count; i++ ) { le64( out + at, ids[i] ); at += 8; }
        le64( out + at, (uint64_t) count ); at += 8;
        return at;
    }
};

#endif // SCHEMA_TEST_TABLE_WIRE_BUILDER_H
