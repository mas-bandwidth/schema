// THE BLOCK WALK of the C++ conformance driver (test/conformance/README.md):
// the block opener, the block row dump (testdata/conformance/tables/FORMAT.md)
// and the block forgery emitter, over the manifest reader and the file helpers
// of manifest.h.
//
// It is a header so that TWO binaries compile the SAME walk:
//
//   - build/conformance-cpp (its block surfaces in driver_blocks.cpp), the
//     reference leg, which answers every surface and byte-compares the block
//     dumps against their pins; and
//   - build/conformance-cpp-block (block_pin.cpp), which answers only the
//     block-dump surface and emit-block-forgeries and compiles only the two
//     units a block names (blockdemo and cooktorture). `make
//     pins-conformance-block` writes the block dump pins with it, so that row
//     builds what its pins check and not the whole driver.
//
// A dump the two binaries wrote differently could only come from a different
// walk, and there is one: this file.
#pragma once

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <string>
#include <vector>

#include "RenderBlock.h"
#include "PaddedBlock.h"
#include "CookTortureBlock.h"

// the manifest reader and the file helpers, shared with every unit of the
// driver (manifest.h)
#include "manifest.h"

// ---------------------------------------------------------------------------
// blocks
// ---------------------------------------------------------------------------
//
// A block's base is 64-byte aligned by construction (§19.1), so the bytes are
// copied once into aligned storage — which is what a host engine's boundary
// looks like, and it keeps BlockOpen's alignment check a real one.

struct Aligned
{
    uint8_t * raw;
    uint8_t * base;
    int64_t bytes;

    // `extent` is the length the CALLER claims, which a forgery may set past
    // the bytes it carries: that is the fact row nine and row ten of the
    // battery are about, and a file alone cannot carry it. The allocation is
    // the claim, so a reader that walks past what it was given walks into a
    // sanitizer's redzone rather than into a neighbour.
    //
    // `lead` is the POINTER column: 0 an aligned base, 1..63 that many bytes
    // past one. An unaligned base is a pointer fact and not a file fact, so the
    // bytes are copied AT the base the column states and the base-alignment
    // clause is the one that must refuse them.
    bool create( const std::vector<uint8_t> & data, int64_t extent, int lead = 0 )
    {
        // the allocation IS the claim, and the claim may be shorter than the
        // file: a driver copies what fits and zeroes the rest, which is the one
        // rule both forgery batteries read the extent column by.
        bytes = extent < 0 ? (int64_t) data.size() : extent;
        raw = (uint8_t *) malloc( (size_t) ( bytes > 0 ? bytes : 1 ) + 64 + (size_t) lead );
        if ( raw == NULL ) return false;
        base = (uint8_t *) ( ( (uintptr_t) raw + 63 ) & ~(uintptr_t) 63 ) + lead;
        memset( base, 0, (size_t) bytes );
        const size_t copy = data.size() < (size_t) bytes ? data.size() : (size_t) bytes;
        memcpy( base, data.data(), copy );
        return true;
    }
    void destroy() { free( raw ); raw = NULL; }
};

// the block opened, and on a refusal the REASON BlockOpen names beside its
// false (docs/SPEC-TABLES.md §19.2): the caller's own initial value is what
// a match leaves standing
static inline bool open_block( const std::string & name, const std::vector<uint8_t> & data, int64_t extent,
                               blockdemo::TableRefuseReason * reason = NULL, int lead = 0 )
{
    Aligned storage = {};
    if ( !storage.create( data, extent, lead ) ) return false;
    bool opened = false;
    if ( name.rfind( "block_render", 0 ) == 0 )
    {
        blockdemo::RenderFrameBlock block;
        opened = blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes, reason );
    }
    else if ( name.rfind( "block_padded", 0 ) == 0 )
    {
        blockdemo::PaddedFrameBlock block;
        opened = blockdemo::PaddedFrameBlockOpen( block, storage.base, storage.bytes, reason );
    }
    else if ( name.rfind( "block_torture", 0 ) == 0 )
    {
        cooktorture::TortureFrameBlock block;
        cooktorture::TableRefuseReason r = {};
        opened = cooktorture::TortureFrameBlockOpen( block, storage.base, storage.bytes, reason ? &r : NULL );
        if ( reason ) *reason = (blockdemo::TableRefuseReason) r;
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name.c_str() );
        storage.destroy();
        exit( 1 );
    }
    storage.destroy();
    return opened;
}

// ---------------------------------------------------------------------------
// the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md)
// ---------------------------------------------------------------------------
//
// The `block` surface says only that an image OPENS, which a port passes with
// a reader that checks the prologue and stops. This one reads every row VALUE
// FOR VALUE and writes the walk out as text, so two implementations' reads are
// byte-compared. It is written against §8's descriptors and NOTHING ELSE — no
// generated struct, no field named in this file — because that is the claim
// §19.2 makes for them, and a walk that reached for a struct would be proving
// something else.
//
// The conventions are the cook's node dump's (§7.5), with one addition the
// cook's corpus never needed: a FLOAT is written as its IEEE-754 BIT PATTERN.
// A block row is a byte-identical projection, so its bits are the fact and a
// decimal spelling would be a rounding rule two languages have to agree on for
// no gain — the cook's dump refuses a float rather than inventing one, and
// this is the same refusal to invent, answered by not spelling it as a number.

typedef blockdemo::TableBlockInfo BlockInfo;
typedef blockdemo::TableBlockFieldInfo BlockField;

static inline void dump_scalar( std::string & out, const uint8_t * at, uint8_t kind, uint32_t width )
{
    char text[64];
    switch ( kind )
    {
        case 1: // bool
            snprintf( text, sizeof( text ), "%s", *at != 0 ? "true" : "false" );
            break;
        case 10: // float32: the bit pattern, in this build's own order
        {
            uint32_t bits = 0;
            memcpy( &bits, at, 4 );
            snprintf( text, sizeof( text ), "0x%08x", bits );
            break;
        }
        case 11: // float64
        {
            uint64_t bits = 0;
            memcpy( &bits, at, 8 );
            snprintf( text, sizeof( text ), "0x%016llx", (unsigned long long) bits );
            break;
        }
        case 2: case 3: case 4: case 5: // the SIGNED integers
        {
            int64_t v = 0;
            if ( width == 1 ) { int8_t t; memcpy( &t, at, 1 ); v = t; }
            else if ( width == 2 ) { int16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { int32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%lld", (long long) v );
            break;
        }
        default: // an enum's ordinal, a flags mask, and every unsigned integer
        {
            uint64_t v = 0;
            if ( width == 1 ) { v = *at; }
            else if ( width == 2 ) { uint16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { uint32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%llu", (unsigned long long) v );
            break;
        }
    }
    out += text;
}

// a string's or a `bytes`' USED bytes, quoted, with everything outside
// printable ASCII escaped — the cook dump's spelling, exactly
static inline void dump_text( std::string & out, const uint8_t * at, int32_t used )
{
    if ( used < 0 ) used = 0;
    out += '"';
    for ( int32_t i = 0; i < used; i++ )
    {
        const uint8_t c = at[i];
        if ( c >= 0x20 && c < 0x7f && c != '"' && c != '\\' )
        {
            out += (char) c;
        }
        else
        {
            char escape[8];
            snprintf( escape, sizeof( escape ), "\\x%02x", (unsigned) c );
            out += escape;
        }
    }
    out += '"';
    char tail[32];
    snprintf( tail, sizeof( tail ), " len=%d", (int) used );
    out += tail;
}

static inline std::string dump_join( const std::string & prefix, const char * name )
{
    return prefix.empty() ? std::string( name ) : prefix + "." + name;
}

static inline std::string dump_slot( const std::string & path, int64_t slot )
{
    char index[32];
    snprintf( index, sizeof( index ), "[%lld]", (long long) slot );
    return path + index;
}

// One record's leaves, at two spaces, in descriptor order. `storage` is the
// record's own base. Out-of-line arrays are the caller's business: they are a
// section of their own, not a leaf.
template <typename InfoT>
static inline bool dump_record( std::string & out, const uint8_t * storage, const InfoT * info,
                                const std::string & path )
{
    if ( info == NULL ) { fprintf( stderr, "driver: a descriptor names no record\n" ); return false; }
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const auto & f = info->fields[i];
        if ( f.out_of_line ) continue;
        const std::string name = dump_join( path, f.name );

        if ( f.counted )
        {
            // a string or a `bytes`: the used length lives at count_offset
            int32_t used = 0;
            memcpy( &used, storage + f.count_offset, sizeof( used ) );
            if ( used < 0 || used > f.array_bound )
            {
                fprintf( stderr, "driver: %s.%s carries a used length of %d, outside [ 0, %d ]\n",
                         info->name, f.name, used, f.array_bound );
                return false;
            }
            out += "  " + name + " = ";
            dump_text( out, storage + f.offset, used );
            out += "\n";
        }
        else
        {
            const int64_t slots = f.is_array ? (int64_t) f.array_bound : 1;
            for ( int64_t s = 0; s < slots; s++ )
            {
                const std::string at = f.is_array ? dump_slot( name, s ) : name;
                const uint8_t * value = storage + f.offset + s * (int64_t) f.elem_size;
                if ( f.element != NULL )
                {
                    if ( !dump_record( out, value, f.element(), at ) ) return false;
                }
                else
                {
                    out += "  " + at + " = ";
                    dump_scalar( out, value, f.kind, f.elem_size );
                    out += "\n";
                }
            }
        }

        if ( f.optional )
        {
            out += "  " + name + "#present = ";
            out += storage[f.present_offset] != 0 ? "true" : "false";
            out += "\n";
        }
    }
    return true;
}

// the whole dump of one opened block: the projection's own fields, then every
// out-of-line array in declaration order, row by row
template <typename InfoT>
static inline bool dump_block( std::string & out, const uint8_t * base, const InfoT * info )
{
    char header[256];
    snprintf( header, sizeof( header ), "projection %s @0\n", info->name );
    out += header;
    if ( !dump_record( out, base, info, "" ) ) return false;

    for ( int i = 0; i < info->num_fields; i++ )
    {
        const auto & f = info->fields[i];
        if ( !f.out_of_line ) continue;
        uint64_t offset_of = 0;
        uint32_t count = 0, stride = 0;
        memcpy( &offset_of, base + f.offset_of_offset, 8 );
        memcpy( &count, base + f.count_offset, 4 );
        memcpy( &stride, base + f.stride_offset, 4 );
        const auto * row = f.element();
        if ( row == NULL ) { fprintf( stderr, "driver: %s names no element\n", f.name ); return false; }
        snprintf( header, sizeof( header ), "array %s %s @%llu count=%u stride=%u\n",
                  f.name, row->name, (unsigned long long) offset_of, count, stride );
        out += header;
        for ( uint32_t r = 0; r < count; r++ )
        {
            const uint64_t at = offset_of + (uint64_t) r * stride;
            snprintf( header, sizeof( header ), "row %u @%llu\n", r, (unsigned long long) at );
            out += header;
            if ( !dump_record( out, base + at, row, "" ) ) return false;
        }
    }
    return true;
}

static inline bool block_dump( const std::string & name, const std::vector<uint8_t> & data, std::string & out )
{
    Aligned storage = {};
    if ( !storage.create( data, -1 ) ) return false;
    bool ok = false;
    if ( name.rfind( "block_render", 0 ) == 0 )
    {
        blockdemo::RenderFrameBlock block;
        ok = blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, blockdemo::RenderFrameBlock::Type() );
    }
    else if ( name.rfind( "block_padded", 0 ) == 0 )
    {
        blockdemo::PaddedFrameBlock block;
        ok = blockdemo::PaddedFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, blockdemo::PaddedFrameBlock::Type() );
    }
    else if ( name.rfind( "block_torture", 0 ) == 0 )
    {
        cooktorture::TortureFrameBlock block;
        ok = cooktorture::TortureFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, cooktorture::TortureFrameBlock::Type() );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name.c_str() );
    }
    storage.destroy();
    return ok;
}

// ---------------------------------------------------------------------------
// the block-dump surface
// ---------------------------------------------------------------------------

static inline int surface_block_dump( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        std::string text;
        if ( !block_dump( f[1], data, text ) ) return 1;
        if ( !spill( out, f[1], text.data(), text.size() ) ) return 1;
    }
    return 0;
}

// ---------------------------------------------------------------------------
// pinning the block forgeries as DATA (test/conformance/README.md)
// ---------------------------------------------------------------------------
//
// The battery in test/tables/block_main.cpp names the words it damages through
// the projection's own field names, which no other language can read out of a
// manifest. This mode resolves each of them to a BYTE OFFSET inside the block
// image and prints the manifest lines, so the eleven words — and the
// base-alignment row that damages no word — become data every backend runs, the
// C++ one included, through the same path.

struct Patch
{
    const char * name;
    uint64_t offset;
    int width;
    uint64_t value;
    const char * verdict;
    const char * label;
    int64_t claim; // the extent the caller passes; -1 means "the file's own"
};

static inline uint64_t swap64( uint64_t v )
{
    uint64_t r = 0;
    for ( int i = 0; i < 8; i++ ) r |= ( ( v >> ( i * 8 ) ) & 0xffull ) << ( 56 - i * 8 );
    return r;
}

static inline int emit_block_forgeries()
{
    std::vector<uint8_t> data;
    if ( !slurp( "testdata/wire/tables/block_render.bin", data ) )
    {
        fprintf( stderr, "driver: cannot read the block golden\n" );
        return 1;
    }
    Aligned storage = {};
    if ( !storage.create( data, -1 ) ) return 1;
    blockdemo::RenderFrameBlock block;
    if ( !blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes ) )
    {
        fprintf( stderr, "driver: the block golden does not open\n" );
        return 1;
    }

    const uint8_t * base = storage.base;
    const uint64_t at_magic = (uint64_t) ( (const uint8_t *) &block.projection->magic - base );
    const uint64_t at_build = (uint64_t) ( (const uint8_t *) &block.projection->build_version - base );
    const uint64_t at_order = (uint64_t) ( (const uint8_t *) &block.projection->byte_order - base );
    const uint64_t at_ships_offset = (uint64_t) ( (const uint8_t *) &block.projection->ships.offset_of - base );
    const uint64_t at_ships_stride = (uint64_t) ( (const uint8_t *) &block.projection->ships.stride - base );
    const uint64_t at_ships_count = (uint64_t) ( (const uint8_t *) &block.projection->ships.count - base );
    const uint64_t at_cameras_offset = (uint64_t) ( (const uint8_t *) &block.projection->cameras.offset_of - base );

    const uint64_t magic = block.projection->magic;
    const uint64_t build = block.projection->build_version;
    const uint64_t order = block.projection->byte_order;
    const uint64_t ships_offset = block.projection->ships.offset_of;
    const uint32_t ships_stride = block.projection->ships.stride;

    const Patch patches[] = {
        { "block_magic_foreign", at_magic, 8, 0, "refuse", "a foreign magic", -1 },
        { "block_magic_swapped", at_magic, 8, swap64( magic ), "refuse", "a block of the other byte order", -1 },
        { "block_build_version", at_build, 8, build ^ 1ull, "refuse", "a build this one does not match", -1 },
        { "block_byte_order", at_order, 8, (uint64_t) ( order == 1 ? 2 : 1 ), "refuse", "the other byte order in the prologue's own word", -1 },
        { "block_array_unaligned", at_ships_offset, 8, ships_offset + 1, "refuse", "an array start not aligned for its element", -1 },
        { "block_array_in_prologue", at_ships_offset, 8, 0, "refuse", "an array start inside the projection", -1 },
        { "block_array_escapes", at_ships_offset, 8, (uint64_t) blockdemo::RenderFrameBlockMaxBytes, "refuse", "an array that leaves the block", -1 },
        { "block_pitch", at_ships_stride, 4, (uint64_t) ( ships_stride + 8 ), "refuse", "a pitch that is not this build's own", -1 },
        { "block_count_past_extent", at_ships_count, 4, 4000, "refuse", "a count whose rows leave the extent the caller passed", 200000 },
        { "block_count_past_maximum", at_ships_count, 4,
          (uint64_t) ( blockdemo::RenderFrameBlock::ShipsMax + 904 ), "refuse",
          "a count past the declared maximum, inside a roomy extent", (int64_t) blockdemo::RenderFrameBlockMaxBytes },
        { "block_offset_overflow", at_cameras_offset, 8, 0x7fffffffffffffc0ull, "refuse",
          "an offset 64-aligned and just under 2^63 — the forgery fuzzer's find", -1 },
    };

    printf( "# THE BLOCK FORGERY BATTERY as data (docs/SPEC-TABLES.md §19.2), pinned from\n" );
    printf( "# test/tables/block_main.cpp's eleven by test/conformance/cpp: each row is one\n" );
    printf( "# word of an otherwise valid block image, resolved to a byte offset, plus the\n" );
    printf( "# base-alignment row the cook battery has always carried and the block's had not.\n" );
    printf( "#\n" );
    printf( "#   forgery <name> block <subject> <base> <pointer> <offset> <width> <value> <extent> <verdict> <label>\n" );
    printf( "#\n" );
    printf( "# <base> names the block line the image comes from; <extent> is the length the\n# CALLER claims (-1: the image's own); <pointer> is the BUFFER the caller holds —\n# 0 an aligned base, 1..63 that many bytes past one, `null` no buffer at all —\n# because an unaligned base is a pointer fact and not a file fact.\n" );
    printf( "# The harness applies the patch and hands a driver a path.\n" );
    printf( "# Repin with: make conformance-pin.\n" );
    for ( size_t i = 0; i < sizeof( patches ) / sizeof( patches[0] ); i++ )
    {
        const Patch & p = patches[i];
        printf( "forgery %-26s block block_render block_render 0 0x%04llx %d 0x%-16llx %8lld %s %s\n",
                p.name, (unsigned long long) p.offset, p.width,
                (unsigned long long) p.value, (long long) p.claim, p.verdict, p.label );
    }
    // and the base-alignment row, which damages no word: the image is whole and
    // the CALLER's buffer sits one byte past an aligned base, so only the last
    // clause of the read can refuse it (docs/SPEC-TABLES.md §19.2).
    printf( "forgery %-26s block block_render block_render 1 -1 0 0 -1 refuse a base one byte past an aligned one\n",
            "block_base_unaligned" );
    storage.destroy();
    return 0;
}

