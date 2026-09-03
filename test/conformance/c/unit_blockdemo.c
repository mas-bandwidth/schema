/* THE BLOCK UNIT (tables/block): open, and the ROW DUMP.
 *
 * The `block` surface says only that an image OPENS, which a reader passes by
 * checking the prologue and stopping. `block-dump` reads every row VALUE FOR
 * VALUE, so two implementations' reads of the same bytes are byte-compared. It
 * is written against §8's DESCRIPTORS and NOTHING ELSE — no generated row
 * struct, no field named in this file — because that is the claim §19.2 makes
 * for them.
 *
 * The conventions are the cook node dump's (§7.5), with one addition the cook's
 * corpus never needed: a FLOAT is written as its IEEE-754 BIT PATTERN. A block
 * row is a byte-identical projection, so its bits are the fact and a decimal
 * spelling would be a rounding rule two languages have to agree on for no gain.
 */

#include "RenderBlock.h"
#include "PaddedBlock.h"
#include "unit.h"

/* ---- the shared leaf spellings ---- */

static void dump_scalar( ConformanceText * out, const uint8_t * at, uint8_t kind, uint32_t width )
{
    char text[64];
    switch ( kind )
    {
        case 1: /* bool */
            snprintf( text, sizeof( text ), "%s", *at != 0 ? "true" : "false" );
            break;
        case 10: /* float32: the bit pattern, in this build's own order */
        {
            uint32_t bits = 0;
            memcpy( &bits, at, 4 );
            snprintf( text, sizeof( text ), "0x%08x", bits );
            break;
        }
        case 11: /* float64 */
        {
            uint64_t bits = 0;
            memcpy( &bits, at, 8 );
            snprintf( text, sizeof( text ), "0x%016llx", (unsigned long long) bits );
            break;
        }
        case 2: case 3: case 4: case 5: /* the SIGNED integers */
        {
            int64_t v = 0;
            if ( width == 1 ) { int8_t t; memcpy( &t, at, 1 ); v = t; }
            else if ( width == 2 ) { int16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { int32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%lld", (long long) v );
            break;
        }
        default: /* an enum's ordinal, a flags mask, and every unsigned integer */
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
    conformance_text_add( out, text );
}

/* a string's or a `bytes`' USED bytes, quoted, with everything outside
   printable ASCII escaped — the cook dump's spelling, exactly */
static void dump_text( ConformanceText * out, const uint8_t * at, int32_t used )
{
    int32_t i;
    char escape[16];
    if ( used < 0 ) { used = 0; }
    conformance_text_add( out, "\"" );
    for ( i = 0; i < used; i++ )
    {
        const uint8_t c = at[i];
        if ( c >= 0x20 && c < 0x7f && c != '"' && c != '\\' )
        {
            char one[2];
            one[0] = (char) c;
            one[1] = 0;
            conformance_text_add( out, one );
        }
        else
        {
            snprintf( escape, sizeof( escape ), "\\x%02x", (unsigned) c );
            conformance_text_add( out, escape );
        }
    }
    conformance_text_add( out, "\"" );
    snprintf( escape, sizeof( escape ), " len=%d", (int) used );
    conformance_text_add( out, escape );
}

static void dump_join( char * out, size_t size, const char * prefix, const char * name )
{
    if ( prefix[0] == 0 ) { snprintf( out, size, "%s", name ); }
    else { snprintf( out, size, "%s.%s", prefix, name ); }
}

/* One record's leaves, at two spaces, in descriptor order. `storage` is the
   record's own base. Out-of-line arrays are the caller's business: they are a
   section of their own, not a leaf. */
static int dump_record( ConformanceText * out, const uint8_t * storage, const TableBlockInfo * info, const char * path )
{
    int i;
    if ( info == NULL ) { fprintf( stderr, "driver: a descriptor names no record\n" ); return 0; }
    for ( i = 0; i < info->num_fields; i++ )
    {
        const TableBlockFieldInfo * f = &info->fields[i];
        char name[512];
        char at[576];
        if ( f->out_of_line ) { continue; }
        dump_join( name, sizeof( name ), path, f->name );

        if ( f->counted )
        {
            /* a string or a `bytes`: the used length lives at count_offset */
            int32_t used = 0;
            memcpy( &used, storage + f->count_offset, sizeof( used ) );
            if ( used < 0 || used > f->array_bound )
            {
                if ( !conformance_quiet )
                {
                    fprintf( stderr, "driver: %s.%s carries a used length of %d, outside [ 0, %d ]\n",
                             info->name, f->name, used, f->array_bound );
                }
                return 0;
            }
            conformance_text_add( out, "  " );
            conformance_text_add( out, name );
            conformance_text_add( out, " = " );
            dump_text( out, storage + f->offset, used );
            conformance_text_add( out, "\n" );
        }
        else
        {
            const int64_t slots = f->is_array ? (int64_t) f->array_bound : 1;
            int64_t s;
            for ( s = 0; s < slots; s++ )
            {
                const uint8_t * value = storage + f->offset + s * (int64_t) f->elem_size;
                if ( f->is_array ) { snprintf( at, sizeof( at ), "%s[%lld]", name, (long long) s ); }
                else { snprintf( at, sizeof( at ), "%s", name ); }
                if ( f->element != NULL )
                {
                    if ( !dump_record( out, value, f->element, at ) ) { return 0; }
                }
                else
                {
                    conformance_text_add( out, "  " );
                    conformance_text_add( out, at );
                    conformance_text_add( out, " = " );
                    dump_scalar( out, value, f->kind, f->elem_size );
                    conformance_text_add( out, "\n" );
                }
            }
        }

        if ( f->optional )
        {
            conformance_text_add( out, "  " );
            conformance_text_add( out, name );
            conformance_text_add( out, "#present = " );
            conformance_text_add( out, storage[f->present_offset] != 0 ? "true" : "false" );
            conformance_text_add( out, "\n" );
        }
    }
    return 1;
}

/* the whole dump of one opened block: the projection's own fields, then every
   out-of-line array in declaration order, row by row */
static int dump_block( ConformanceText * out, const uint8_t * base, const TableBlockInfo * info )
{
    char header[256];
    int i;
    snprintf( header, sizeof( header ), "projection %s @0\n", info->name );
    conformance_text_add( out, header );
    if ( !dump_record( out, base, info, "" ) ) { return 0; }

    for ( i = 0; i < info->num_fields; i++ )
    {
        const TableBlockFieldInfo * f = &info->fields[i];
        uint64_t offset_of = 0;
        uint32_t count = 0, stride = 0;
        const TableBlockInfo * row;
        uint32_t r;
        if ( !f->out_of_line ) { continue; }
        memcpy( &offset_of, base + f->offset_of_offset, 8 );
        memcpy( &count, base + f->count_offset, 4 );
        memcpy( &stride, base + f->stride_offset, 4 );
        row = f->element;
        if ( row == NULL ) { fprintf( stderr, "driver: %s names no element\n", f->name ); return 0; }
        snprintf( header, sizeof( header ), "array %s %s @%llu count=%u stride=%u\n",
                  f->name, row->name, (unsigned long long) offset_of, count, stride );
        conformance_text_add( out, header );
        for ( r = 0; r < count; r++ )
        {
            const uint64_t row_at = offset_of + (uint64_t) r * stride;
            snprintf( header, sizeof( header ), "row %u @%llu\n", r, (unsigned long long) row_at );
            conformance_text_add( out, header );
            if ( !dump_record( out, base + row_at, row, "" ) ) { return 0; }
        }
    }
    return 1;
}

/* ---- the two entry points main.c names ---- */

int conformance_block_open( const char * name, const uint8_t * data, size_t bytes, int64_t extent, int pointer )
{
    ConformanceBuffer buffer;
    int opened = 0;
    if ( !conformance_buffer_create( &buffer, data, bytes, extent, pointer ) ) { return 0; }
    if ( strncmp( name, "block_render", 12 ) == 0 )
    {
        RenderFrameBlock block;
        opened = RenderFrameBlockOpen( &block, buffer.base, buffer.bytes );
    }
    else if ( strncmp( name, "block_padded", 12 ) == 0 )
    {
        PaddedFrameBlock block;
        opened = PaddedFrameBlockOpen( &block, buffer.base, buffer.bytes );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name );
        conformance_buffer_destroy( &buffer );
        exit( 1 );
    }
    conformance_buffer_destroy( &buffer );
    return opened;
}

int conformance_block_dump( const char * name, const uint8_t * data, size_t bytes, ConformanceText * out )
{
    ConformanceBuffer buffer;
    int ok = 0;
    if ( !conformance_buffer_create( &buffer, data, bytes, -1, 0 ) ) { return 0; }
    if ( strncmp( name, "block_render", 12 ) == 0 )
    {
        RenderFrameBlock block;
        ok = RenderFrameBlockOpen( &block, buffer.base, buffer.bytes ) &&
             dump_block( out, block.base, RenderFrameBlockType() );
    }
    else if ( strncmp( name, "block_padded", 12 ) == 0 )
    {
        PaddedFrameBlock block;
        ok = PaddedFrameBlockOpen( &block, buffer.base, buffer.bytes ) &&
             dump_block( out, block.base, PaddedFrameBlockType() );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name );
    }
    conformance_buffer_destroy( &buffer );
    return ok;
}
