package ctable

// The C reads hold the reference's refusal rules (docs/SPEC-TABLES.md §4, §6.5,
// §16.2; docs/FIXED-FORM-ALGORITHM.md §5.3 step 11):
//
//   - <t>_from_json answers 0 on every refusal: a wrong type, a bare number in
//     a 64-bit field, a repeated key, an out-of-range integer;
//   - a refusal leaves the caller's value at its declared defaults, never
//     partly filled;
//   - each read zeroes the caller's report first, so refused, refuse_reason and
//     unknown never carry over from an earlier read;
//   - a fixed-form batch checks every record's hash before landing any, so a
//     mismatch at record k leaves values[0..k-1] untouched.

import "testing"

const cRefusalResetSchema = `package probe

fixed table Rec
{
    a int32
    big uint64
    small uint8
}
`

const cRefusalResetProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>

static int failed = 0;
#define EXPECT( cond, what ) do { if ( !( cond ) ) { fprintf( stderr, "FAIL: %s\n", what ); failed = 1; } } while ( 0 )

static void refuses( const char * text, const char * what )
{
    Rec v;
    TableReport r;
    int ok;
    memset( &r, 0, sizeof( r ) );
    rec_reset( &v );
    v.a = 77;
    ok = rec_from_json( &v, text, (int64_t) strlen( text ), &r );
    if ( ok != 0 ) { fprintf( stderr, "FAIL: %s: returned %d, not a refusal\n", what, ok ); failed = 1; }
    if ( !r.refused ) { fprintf( stderr, "FAIL: %s: report not refused\n", what ); failed = 1; }
    if ( v.a != 0 || v.big != 0 || v.small != 0 ) { fprintf( stderr, "FAIL: %s: value left partly filled (a=%d small=%d)\n", what, (int) v.a, (int) v.small ); failed = 1; }
}

static uint8_t file[1 << 16];
static TableFixedEntry plan[4096];

int main( void )
{
    Rec v, in[3], out[3];
    TableReport r;
    const char * ok_text = "{\"a\":1}";
    const char * unknown_text = "{\"zz\":1}";
    int64_t bytes, i, n;
    int hits = 0;

    refuses( "{\"a\":5,\"small\":300}", "out of range integer" );
    refuses( "{\"a\":5,\"big\":5}", "bare number in a 64-bit field" );
    refuses( "{\"a\":5,\"a\":6}", "repeated key" );
    refuses( "{\"a\":5,\"small\":\"x\"}", "wrong type" );

    /* the report is this read's own */
    memset( &r, 0, sizeof( r ) );
    rec_reset( &v );
    EXPECT( rec_from_json( &v, unknown_text, (int64_t) strlen( unknown_text ), &r ) == 1, "unknown key reads" );
    EXPECT( r.unknown == 1, "one unknown" );
    EXPECT( rec_from_json( &v, unknown_text, (int64_t) strlen( unknown_text ), &r ) == 1, "unknown key reads again" );
    EXPECT( r.unknown == 1, "unknown does not accumulate across reads" );
    {
        const char * bad = "{\"a\":5,\"a\":6}";
        EXPECT( rec_from_json( &v, bad, (int64_t) strlen( bad ), &r ) == 0, "repeated key refuses" );
        EXPECT( r.refused, "refused set" );
    }
    EXPECT( rec_from_json( &v, ok_text, (int64_t) strlen( ok_text ), &r ) == 1, "clean read after a refusal" );
    EXPECT( !r.refused && r.refuse_reason == 0 && r.unknown == 0, "a clean read leaves no refusal or unknown in the report" );
    EXPECT( v.a == 1, "clean read lands" );

    /* fixed batch: a hash mismatch at the last record lands nothing */
    for ( i = 0; i < 3; ++i ) { rec_reset( &in[i] ); in[i].a = (int32_t) ( 10 + i ); }
    bytes = rec_fixed_save( in, 3, file, (int64_t) sizeof( file ) );
    EXPECT( bytes > 0, "fixed save" );
    for ( i = 0; i + 8 <= bytes; ++i )
    {
        if ( table_fixed_get64( file + i ) == rec_fixed_hash ) { hits++; if ( hits == 3 ) { file[i] ^= 0xff; break; } }
    }
    EXPECT( hits == 3, "found three record hashes" );
    for ( i = 0; i < 3; ++i ) { rec_reset( &out[i] ); out[i].a = 99; }
    memset( &r, 0, sizeof( r ) );
    n = rec_fixed_load( out, 3, file, bytes, plan, 4096, NULL, &r );
    EXPECT( n == -1 && r.refused, "hash mismatch refuses" );
    EXPECT( out[0].a == 99 && out[1].a == 99 && out[2].a == 99, "a hash mismatch at record 2 landed no record" );

    return failed;
}
`

func TestCRefusalResetsAndReportsPerRead(t *testing.T) {
	t.Parallel()
	runCHostileResidual(t, cRefusalResetSchema, cRefusalResetProbe)
}
