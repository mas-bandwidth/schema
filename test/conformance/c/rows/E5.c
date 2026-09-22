/* test/conformance/c/rows/E5.c — schema matrix cell c/E5: "?T vs plain
 * nesting" (docs/roadmap.sexp:2313, matrix schema#876).
 *
 * THE LAW (docs/SPEC-TABLES.md:7194): "A field moved between `?T` and a plain
 * nesting is not an evolution event at all — the bytes do not move." P1 nests
 * Link BY VALUE (test/tables/P1.schema); P3 marks the same field `?Link`
 * (test/tables/P3.schema); on the tolerant table wire the two spellings are
 * byte-identical for content that is not entirely default
 * (test/tables/main.cpp, "the three spellings over NON-DEFAULT content"), so
 * every direction of the seam reads SILENT — no counter moved, no refusal,
 * the payload rides whole. The asymmetry at the empty end is pinned too: a
 * by-value nesting at its defaults writes nothing while a PRESENT optional
 * writes its body anyway, so the round-trip byte identity is asserted on the
 * non-default directions only.
 *
 * The conformance corpus pins the read report of every direction in
 * testdata/conformance/tables/reports.txt — p1_as_p3, p3_as_p1,
 * p1_empty_as_p3, p3_empty_as_p1 all answer 0,0,0,0,0,false,read — and the
 * wire bytes live in testdata/wire/tables/. This test walks the SAME path the
 * C driver does (test/conformance/c/main.c): the erased ConformanceCodec each
 * unit's translation unit exports, over the generated C tables code. It adds
 * the byte-level half the report surface does not look at — read a file
 * written under one schema's layout and save it back under the other's; the
 * bytes must be identical (docs/FIXED-FORM-ALGORITHM.md:1683, proof item 1).
 *
 * Standalone: depends on no other rows/ file, links only the two P units'
 * translation units and their generated tables, edits no shared file. */

#include "driver.h"

#include <stdio.h>

static int failed;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failed++; }
}

static const ConformanceCodec * find_codec( const ConformanceCodec * codecs, int count,
                                            const char * unit, const char * root )
{
    int i;
    for ( i = 0; i < count; i++ )
    {
        if ( strcmp( codecs[i].unit, unit ) == 0 && strcmp( codecs[i].root, root ) == 0 )
        {
            return &codecs[i];
        }
    }
    return NULL;
}

static uint8_t * read_file( const char * path, size_t * bytes )
{
    FILE * file = fopen( path, "rb" );
    long size;
    uint8_t * out;
    if ( file == NULL ) { return NULL; }
    fseek( file, 0, SEEK_END );
    size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out = (uint8_t *) malloc( (size_t) ( size > 0 ? size : 1 ) );
    if ( out == NULL ) { fclose( file ); return NULL; }
    if ( size > 0 && fread( out, 1, (size_t) size, file ) != (size_t) size )
    {
        free( out );
        fclose( file );
        return NULL;
    }
    fclose( file );
    *bytes = (size_t) size;
    return out;
}

static int silent( const ConformanceReport * r )
{
    return r->unknown == 0 && r->kind_mismatch == 0 && r->widened == 0
        && r->clamped == 0 && r->duplicate == 0 && !r->malformed && !r->refused;
}

/* Read <wire_path>, written under the OTHER spelling of the seam, with this
 * codec's schema and re-save under it. Silent is the law's "not an evolution
 * event"; round-trip byte identity is the law's "the bytes do not move". */
static void cross_read( const ConformanceCodec * codec, const char * wire_path,
                        const char * what, int require_identical )
{
    uint8_t * wire = NULL;
    size_t wire_bytes = 0;
    void * value;
    ConformanceReport report;
    uint8_t * out = NULL;
    int64_t size;
    char line[512];

    wire = read_file( wire_path, &wire_bytes );
    if ( wire == NULL )
    {
        snprintf( line, sizeof( line ), "%s: cannot read %s", what, wire_path );
        check( 0, line );
        return;
    }
    value = codec->storage();
    memset( &report, 0, sizeof( report ) );
    if ( !codec->load( value, wire, (int64_t) wire_bytes, &report ) )
    {
        snprintf( line, sizeof( line ), "%s: the read failed outright", what );
        check( 0, line );
        free( wire );
        return;
    }
    snprintf( line, sizeof( line ), "%s: silent cross-read (unknown %d kind_mismatch %d widened %d clamped %d duplicate %d malformed %d refused %d)",
              what, report.unknown, report.kind_mismatch, report.widened,
              report.clamped, report.duplicate, report.malformed, report.refused );
    check( silent( &report ), line );

    if ( require_identical )
    {
        int ok;
        size = codec->measure( value );
        if ( size < 0 ) { check( 0, "cross_read: measure failed" ); }
        else
        {
            out = (uint8_t *) malloc( (size_t) size );
            if ( out == NULL ) { check( 0, "cross_read: out of memory" ); }
            else
            {
                ok = codec->save( value, out, size ) == size
                    && (uint64_t) size == wire_bytes
                    && memcmp( out, wire, (size_t) size ) == 0;
                snprintf( line, sizeof( line ), "%s: re-save is byte-identical (%lld/%zu bytes)",
                          what, (long long) size, wire_bytes );
                check( ok, line );
                free( out );
            }
        }
    }
    free( wire );
}

int main( void )
{
    int count;
    const ConformanceCodec * p1 = NULL;
    const ConformanceCodec * p3 = NULL;

    p1 = find_codec( conformance_codecs_tblp1( &count ), count, "tblp1", "Chain" );
    p3 = find_codec( conformance_codecs_tblp3( &count ), count, "tblp3", "Chain" );
    check( p1 != NULL && p3 != NULL, "both P codecs (tblp1/Chain, tblp3/Chain) are on the driver's table" );

    if ( p1 != NULL && p3 != NULL )
    {
        /* P1's plain-nested file, read under P3's ?Link schema: silent, and
           the bytes come back unchanged. P3 declares the field optional and
           its save writes the identical file (test/tables/main.cpp pins the
           two spellings byte-identical). */
        cross_read( p3, "testdata/wire/tables/chain_value.bin", "p1_as_p3", 1 );

        /* P3's optional-present file, read under P1's plain schema: silent,
           and the bytes come back unchanged. */
        cross_read( p1, "testdata/wire/tables/chain_optional.bin", "p3_as_p1", 1 );

        /* P1's all-default file, read under P3: silent; the empty end elides
           in both spellings, so the round trip is still byte-identical. */
        cross_read( p3, "testdata/wire/tables/chain_value_empty.bin", "p1_empty_as_p3", 1 );

        /* P3's present-and-all-default file, read under P1: silent (the body
           rides), but re-saving under P1 elides — the documented asymmetry —
           so no byte identity is demanded here. */
        cross_read( p1, "testdata/wire/tables/chain_optional_empty.bin", "p3_empty_as_p1", 0 );
    }

    printf( "E5 %s\n", failed ? "RED" : "GREEN" );
    return failed ? 1 : 0;
}