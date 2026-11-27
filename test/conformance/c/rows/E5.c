/* test/conformance/c/rows/E5.c — schema matrix cell c/E5: "?T vs plain
 * nesting" (docs/roadmap.sexp:2313, matrix schema#876).
 *
 * THE LAW (docs/SPEC-TABLES.md:7194): "A field moved between `?T` and a plain
 * nesting is not an evolution event at all — the bytes do not move." P1 nests
 * Link BY VALUE (test/tables/P1.schema); P3 marks the same field `?Link`
 * (test/tables/P3.schema). In the fixed form the two are ONE BYTE APART
 * (docs/FIXED-FORM-ALGORITHM.md:182, :186-188: `?T` costs `1 + C(T)`, the
 * present flag THEN the payload, which rides WHOLE), and §7 proof item 1
 * (:1682) holds each side to "read a file and save it back; the bytes must be
 * identical". The Go leg's row (test/conformance/go/rows/E5_test.go) is the
 * reference this row follows.
 *
 * WHAT MOVED, and where each old assertion went:
 *   - P1 and P3's Chain are FIXED roots, so their file read is form 3 and a
 *     form-1 file is refused previous_form (docs/rulings.json
 *     fixed-root-refuses-form1; docs/plans/strict-only.md). The old
 *     "p1_as_p3 / p3_as_p1 / p1_empty_as_p3 / p3_empty_as_p1 read SILENT"
 *     over the form-1 files in testdata/wire/tables/ maps to: the historical
 *     P1 and P3 form-1 files are byte-identical (the form-1 bytes do not
 *     move), and each fixed reader refuses them previous_form, landing nothing.
 *   - "silent" itself lost four of its counters to strict-only-read and
 *     refusal-verdict-only: kind_mismatch, clamped (now the out_of_range
 *     refusal), duplicate and malformed (the damage reason) each refuse the
 *     whole load, so silent is "returned the record, refused 0, reason and
 *     damage OK, unknown and widened 0".
 *   - The old "re-save is byte-identical" maps to the form-3 fixtures in
 *     testdata/conformance/tables/wire/ (and to files this row saves): each
 *     side reads its own file silently and saves it back byte-identical, and
 *     the P3 record is the P1 record with the present flag 1 inserted before
 *     the payload. The old "the empty end elides in both spellings" maps to
 *     the empty fixtures: P1's reads all-default, P3's reads its present
 *     ?Link at a zero payload, both saved back byte-identical.
 *
 * Standalone: depends on no other rows/ file, reaches the generated C tables
 * of the two P units (their own package prefixes, one translation unit), edits
 * no shared file. */

#include "P1Table.h"
#include "P3Table.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failed;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failed++; }
}

static uint8_t * read_file( const char * path, size_t * bytes )
{
    FILE * file = fopen( path, "rb" );
    long size;
    uint8_t * out;
    *bytes = 0;
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

static int silent1( int64_t n, const tblp1_table_report_t * r )
{
    return n == 1 && !r->refused && r->refuse_reason == SCHEMA_TBLP1_TABLE_REFUSE_OK
        && r->damage_reason == SCHEMA_TBLP1_TABLE_REFUSE_OK && r->unknown == 0 && r->widened == 0;
}

static int silent3( int64_t n, const tblp3_table_report_t * r )
{
    return n == 1 && !r->refused && r->refuse_reason == SCHEMA_TBLP3_TABLE_REFUSE_OK
        && r->damage_reason == SCHEMA_TBLP3_TABLE_REFUSE_OK && r->unknown == 0 && r->widened == 0;
}

static tblp1_table_fixed_entry_t plan1[4096];
static tblp3_table_fixed_entry_t plan3[4096];

static int64_t load1( tblp1_chain_t * v, const uint8_t * data, size_t bytes, tblp1_table_report_t * r )
{
    memset( r, 0, sizeof( *r ) );
    return tblp1_chain_fixed_load( v, 1, data, (int64_t) bytes, plan1, 4096, NULL, r );
}

static int64_t load3( tblp3_chain_t * v, const uint8_t * data, size_t bytes, tblp3_table_report_t * r )
{
    memset( r, 0, sizeof( *r ) );
    return tblp3_chain_fixed_load( v, 1, data, (int64_t) bytes, plan3, 4096, NULL, r );
}

/* save one record back and compare it with the file it was read from */
static int resave1( const tblp1_chain_t * v, const uint8_t * file, size_t bytes )
{
    uint8_t out[512];
    int64_t n = tblp1_chain_fixed_save( v, 1, out, (int64_t) sizeof( out ) );
    return n > 0 && (size_t) n == bytes && memcmp( out, file, bytes ) == 0;
}

static int resave3( const tblp3_chain_t * v, const uint8_t * file, size_t bytes )
{
    uint8_t out[512];
    int64_t n = tblp3_chain_fixed_save( v, 1, out, (int64_t) sizeof( out ) );
    return n > 0 && (size_t) n == bytes && memcmp( out, file, bytes ) == 0;
}

#define BODY1( file ) ( ( file ) + TBLP1_K_TABLE_FIXED_HEADER_BYTES + 4 + tblp1_chain_fixed_layout_bytes + 8 )
#define BODY3( file ) ( ( file ) + TBLP3_K_TABLE_FIXED_HEADER_BYTES + 4 + tblp3_chain_fixed_layout_bytes + 8 )

int main( void )
{
    size_t ov = 0, oo = 0, ove = 0, ooe = 0, fv_n = 0, fo_n = 0, fve_n = 0, foe_n = 0;
    uint8_t * old_value = read_file( "testdata/wire/tables/chain_value.bin", &ov );
    uint8_t * old_optional = read_file( "testdata/wire/tables/chain_optional.bin", &oo );
    uint8_t * old_value_empty = read_file( "testdata/wire/tables/chain_value_empty.bin", &ove );
    uint8_t * old_optional_empty = read_file( "testdata/wire/tables/chain_optional_empty.bin", &ooe );
    uint8_t * fv = read_file( "testdata/conformance/tables/wire/chain_value.bin", &fv_n );
    uint8_t * fo = read_file( "testdata/conformance/tables/wire/chain_optional.bin", &fo_n );
    uint8_t * fve = read_file( "testdata/conformance/tables/wire/chain_value_empty.bin", &fve_n );
    uint8_t * foe = read_file( "testdata/conformance/tables/wire/chain_optional_empty.bin", &foe_n );
    tblp1_chain_t v1;
    tblp3_chain_t v3;
    tblp1_table_report_t r1;
    tblp3_table_report_t r3;
    int64_t n;

    /* ---- FORM 1 IS THE HISTORICAL FORM: the bytes did not move, and a fixed
       root refuses it previous_form (fixed-root-refuses-form1) ---- */
    check( old_value && old_optional && old_value_empty && old_optional_empty,
           "the four historical form-1 chain fixtures under testdata/wire/tables/ are present" );
    check( old_value && old_optional && ov == oo && memcmp( old_value, old_optional, ov ) == 0,
           "p1_as_p3 / p3_as_p1: the historical P1 and P3 form-1 files are byte-identical (the bytes do not move)" );
    if ( old_value && old_optional && old_value_empty && old_optional_empty )
    {
        n = load3( &v3, old_value, ov, &r3 );
        check( n == -1 && r3.refused && r3.reason == SCHEMA_TBLP3_TABLE_PREVIOUS_FORM && v3.name_length == 0,
               "p1_as_p3: P3's fixed reader refuses P1's form-1 file previous_form, landing nothing" );
        n = load1( &v1, old_optional, oo, &r1 );
        check( n == -1 && r1.refused && r1.reason == SCHEMA_TBLP1_TABLE_PREVIOUS_FORM && v1.name_length == 0,
               "p3_as_p1: P1's fixed reader refuses P3's form-1 file previous_form, landing nothing" );
        n = load3( &v3, old_value_empty, ove, &r3 );
        check( n == -1 && r3.refused && r3.reason == SCHEMA_TBLP3_TABLE_PREVIOUS_FORM,
               "p1_empty_as_p3: P3's fixed reader refuses P1's empty form-1 file previous_form" );
        n = load1( &v1, old_optional_empty, ooe, &r1 );
        check( n == -1 && r1.refused && r1.reason == SCHEMA_TBLP1_TABLE_PREVIOUS_FORM,
               "p3_empty_as_p1: P1's fixed reader refuses P3's empty form-1 file previous_form" );
    }

    /* ---- FORM 3: each side reads its own fixture silently and saves it back
       byte-identical; the two records are one flag byte apart ---- */
    check( fv && fo && fve && foe,
           "the four form-3 chain fixtures under testdata/conformance/tables/wire/ are present" );
    if ( fv && fo && fve && foe )
    {
        tblp1_chain_t e1;
        tblp3_chain_t e3;
        const uint8_t * b1;
        const uint8_t * b3;

        n = load1( &v1, fv, fv_n, &r1 );
        check( silent1( n, &r1 ) && v1.name_length == 5 && memcmp( v1.name, "chain", 5 ) == 0 &&
               v1.link.value == 7 && v1.link.tag_length == 3 && memcmp( v1.link.tag, "tip", 3 ) == 0,
               "P1 reads its form-3 chain_value.bin silently: 7/\"tip\"/\"chain\"" );
        check( resave1( &v1, fv, fv_n ), "P1's form-3 file: read and saved back, the bytes are identical" );

        n = load3( &v3, fo, fo_n, &r3 );
        check( silent3( n, &r3 ) && v3.link_present && v3.name_length == 5 && memcmp( v3.name, "chain", 5 ) == 0 &&
               v3.link.value == 7 && v3.link.tag_length == 3 && memcmp( v3.link.tag, "tip", 3 ) == 0,
               "P3 reads its form-3 chain_optional.bin silently with the ?Link present" );
        check( resave3( &v3, fo, fo_n ), "P3's form-3 file: read and saved back, the bytes are identical" );

        n = load1( &e1, fve, fve_n, &r1 );
        check( silent1( n, &r1 ) && e1.name_length == 0 && e1.link.value == 0 && e1.link.tag_length == 0,
               "P1's form-3 empty file reads all-default, silently" );
        check( resave1( &e1, fve, fve_n ), "P1's form-3 empty file: read and saved back, the bytes are identical" );

        n = load3( &e3, foe, foe_n, &r3 );
        check( silent3( n, &r3 ) && e3.link_present && e3.link.value == 0 && e3.link.tag_length == 0,
               "P3's form-3 empty file reads its present ?Link at a zero payload, silently" );
        check( resave3( &e3, foe, foe_n ), "P3's form-3 empty file: read and saved back, the bytes are identical" );

        b1 = BODY1( fv );
        b3 = BODY3( fo );
        check( (int64_t) fv_n == tblp1_chain_fixed_measure( 1 ) && (int64_t) fo_n == tblp3_chain_fixed_measure( 1 ) &&
               memcmp( b3, b1, 20 ) == 0 && b3[20] == 0x01 && memcmp( b3 + 21, b1 + 20, 16 ) == 0,
               "the form-3 fixtures: P3's present record is P1's with the flag 1 inserted before the payload" );
    }

    /* ---- THE SEAM IN THE LAYOUT: C(?T) = 1 + C(T) ---- */
    check( tblp1_chain_fixed_body_bytes == 36 && tblp3_chain_fixed_body_bytes == 37 &&
           tblp3_chain_fixed_record_bytes == tblp1_chain_fixed_record_bytes + 1 &&
           tblp3_chain_fixed_measure( 1 ) - tblp1_chain_fixed_measure( 1 ) == 1 + 17 &&
           tblp1_chain_fixed_layout[0] == 5 && tblp3_chain_fixed_layout[0] == 6,
           "C(?T) = 1 + C(T): body and record one byte apart (36 -> 37), the file by that byte and the flag's own 17-byte layout entry" );

    /* ---- THE WRITERS: a present ?T record is the plain one with the flag;
       an absent one is flag 0 and a zero payload slot; every file reads back
       and saves back byte-identical (§7 proof item 1) ---- */
    {
        static uint8_t p1_file[512], p3_file[512], a_file[512];
        tblp1_chain_t one, back1;
        tblp3_chain_t three, absent, back3, back_a;
        static const uint8_t zeros[16];
        int64_t n1, n3, na;

        tblp1_chain_reset( &one );
        memcpy( one.name, "hello", 5 ); one.name_length = 5;
        one.link.value = 42;
        memcpy( one.link.tag, "tag", 3 ); one.link.tag_length = 3;
        tblp3_chain_reset( &three );
        memcpy( three.name, "hello", 5 ); three.name_length = 5;
        three.link.value = 42;
        memcpy( three.link.tag, "tag", 3 ); three.link.tag_length = 3;
        three.link_present = 1;
        absent = three;
        absent.link_present = 0;

        n1 = tblp1_chain_fixed_save( &one, 1, p1_file, (int64_t) sizeof( p1_file ) );
        n3 = tblp3_chain_fixed_save( &three, 1, p3_file, (int64_t) sizeof( p3_file ) );
        na = tblp3_chain_fixed_save( &absent, 1, a_file, (int64_t) sizeof( a_file ) );
        check( n1 == tblp1_chain_fixed_measure( 1 ) && n3 == tblp3_chain_fixed_measure( 1 ) && na == n3,
               "P1 saves the present-content record, P3 its present and absent ?T twins" );
        if ( n1 > 0 && n3 > 0 && na > 0 )
        {
            const uint8_t * p1_body = BODY1( p1_file );
            const uint8_t * p3_body = BODY3( p3_file );
            const uint8_t * a_body = BODY3( a_file );
            check( memcmp( p3_body, p1_body, 20 ) == 0 && p3_body[20] == 0x01 && memcmp( p3_body + 21, p1_body + 20, 16 ) == 0,
                   "the present ?T record is the plain record with the flag 1 inserted BEFORE the payload — the payload rides WHOLE" );
            check( a_body[20] == 0 && memcmp( a_body + 21, zeros, 16 ) == 0 && memcmp( a_body, p1_body, 20 ) == 0,
                   "the absent record: flag 0, payload slot zero, one byte apart still" );

            n = load3( &back3, p3_file, (size_t) n3, &r3 );
            check( silent3( n, &r3 ) && resave3( &back3, p3_file, (size_t) n3 ),
                   "P3's present file: read and saved back, the bytes are identical" );
            n = load1( &back1, p1_file, (size_t) n1, &r1 );
            check( silent1( n, &r1 ) && resave1( &back1, p1_file, (size_t) n1 ),
                   "P1's plain-nesting file: read and saved back, the bytes are identical" );
            n = load3( &back_a, a_file, (size_t) na, &r3 );
            check( silent3( n, &r3 ) && resave3( &back_a, a_file, (size_t) na ),
                   "P3's absent file: read and saved back, the bytes are identical" );
            check( back3.link_present && back3.link.value == 42 && back3.link.tag_length == 3 &&
                   !back_a.link_present && back_a.link.value == 0,
                   "form 3 reads: present lands 42/\"tag\", absent reads flag false payload zero" );
        }
    }

    free( old_value ); free( old_optional ); free( old_value_empty ); free( old_optional_empty );
    free( fv ); free( fo ); free( fve ); free( foe );
    printf( "E5 %s\n", failed ? "RED" : "GREEN" );
    return failed ? 1 : 0;
}
