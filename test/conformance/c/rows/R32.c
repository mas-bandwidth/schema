/* C cell R32 — retire for real: a retired version is refused by name, once,
   idempotently.

   LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 §5.3):
     "if i < R.floor: RETIRED. The hash stays KNOWN, so a file carrying it is
      named layout_unsupported and never layout_newer; its PLAN IS NEVER BUILT"

   R32 tests this path: given a file whose header hash matches a KNOWN lineage
   entry that sits BELOW the floor (is RETIRED), the reader must return
   `layout_unsupported`, report the file's hash, and decode no record. A second
   identical read returns the same refusal — the retirement of one hash changes
   nothing on another read of the same hash (§5.7 "idempotent").

   THE PRODUCTION READER, NOT A MODEL OF IT. This row first carried its own
   copy of the two-line decision (select by hash, compare with the floor) over
   a hand-written lineage, and a WIRE_BLOB it never read: it checked the copy,
   not the C leg. It now reads a real file through the generated C reader of a
   real lineage with a retired entry: the FixedEvo unit's EvoRoot
   (build/tables-generated-c/fixedevo, test/tables FixedEvo), whose lineage
   holds four layouts and whose floor is 1, so entry 0 is retired. The file is
   built from the lineage's own bytes (entry 0's hash, layout and record size),
   never hand-pinned. Each old assertion maps to the same check on the
   production read:
     - test 1: -1, refused, reason layout_unsupported, layout_hash = the
       file's hash, no counter moved — and nothing decoded (the destination
       comes back at its reset image).
     - test 2: two reads of the same retired file give the same refusal.
     - test 3, the contrast: a hash in no lineage entry. The row expected
       `layout_newer`; §5.3 step 5 now answers `layout_unknown` in every leg
       and `layout_newer` is RETIRED (docs/FIXED-FORM-ALGORITHM.md "STEP 5's
       ANSWER IS layout_unknown, IN EVERY LEG", decided 2026-11-03; SPEC-TABLES
       §21.3). The row holds the reader to it by name: refused, reason
       SCHEMA_TABLE_LAYOUT_UNKNOWN, layout_hash = the file's hash, landing
       nothing — and so NEITHER the retired layout_newer NOR
       layout_unsupported (the two operator answers stay distinct). */

#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "FixedEvoTable.h"

static int failed;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failed = 1; }
}

/* THE FILE: form byte, seven reserved zeros, the hash, the layout behind its
   u32 length, then ONE record (its hash, a zero body) of the entry's size. */
static uint8_t wire_blob[4096];

static int64_t make_file( int32_t entry, uint64_t hash )
{
    const tblfixedevo_table_fixed_known_layout_t * k = &tblfixedevo_evo_root_fixed_known[entry];
    const int64_t at = TBLFIXEDEVO_K_TABLE_FIXED_HEADER_BYTES + 4 + k->layout_bytes;
    if ( at + k->record_bytes > (int64_t) sizeof( wire_blob ) ) { return 0; }
    memset( wire_blob, 0, sizeof( wire_blob ) );
    wire_blob[0] = TBLFIXEDEVO_K_TABLE_FIXED_FORM;
    tblfixedevo_table_fixed_put64( wire_blob + TBLFIXEDEVO_K_TABLE_FIXED_HASH_AT, hash );
    tblfixedevo_table_fixed_put32( wire_blob + TBLFIXEDEVO_K_TABLE_FIXED_HEADER_BYTES, (uint32_t) k->layout_bytes );
    memcpy( wire_blob + TBLFIXEDEVO_K_TABLE_FIXED_HEADER_BYTES + 4, k->layout, (size_t) k->layout_bytes );
    tblfixedevo_table_fixed_put64( wire_blob + at, hash );
    return at + k->record_bytes;
}

static tblfixedevo_table_fixed_entry_t plan[4096];

static int64_t read_file( int64_t bytes, tblfixedevo_evo_root_t * value, tblfixedevo_table_report_t * report )
{
    memset( value, 0xA5, sizeof( *value ) ); /* poison: a refusal must land nothing */
    memset( report, 0, sizeof( *report ) );
    return tblfixedevo_evo_root_fixed_load( value, 1, wire_blob, bytes, plan,
                                            (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ), NULL, report );
}

static int at_reset( const tblfixedevo_evo_root_t * value )
{
    /* the reset image over the same poison, so padding bytes compare equal
       and only a field the read wrote (or failed to reset) differs */
    static tblfixedevo_evo_root_t reset;
    memset( &reset, 0xA5, sizeof( reset ) );
    tblfixedevo_evo_root_reset( &reset );
    return memcmp( value, &reset, sizeof( reset ) ) == 0;
}

int main( void )
{
    static tblfixedevo_evo_root_t value;
    tblfixedevo_table_report_t r1, r2;
    const uint64_t retired = tblfixedevo_evo_root_fixed_known[0].hash;
    int64_t bytes, n1, n2;
    int32_t i;
    uint64_t stranger;
    int held;

    check( tblfixedevo_evo_root_fixed_floor == 1 && tblfixedevo_evo_root_fixed_known_count > 1,
           "R32: the EvoRoot lineage holds a retired entry (floor 1: entry 0 is below it)" );
    check( retired != tblfixedevo_evo_root_fixed_hash,
           "R32: the retired entry is not this build's own layout" );

    /* TEST 1 — the retired hash below the floor → layout_unsupported */
    bytes = make_file( 0, retired );
    check( bytes > 0, "R32: the retired entry's file is built from the lineage's own bytes" );
    n1 = read_file( bytes, &value, &r1 );
    check( n1 == -1 && r1.refused && r1.reason == SCHEMA_TBLFIXEDEVO_TABLE_LAYOUT_UNSUPPORTED,
           "R32: a retired hash below the floor is refused layout_unsupported" );
    check( r1.layout_hash == retired,
           "R32: the refusal reports THE FILE'S hash" );
    check( r1.unknown == 0 && r1.widened == 0,
           "R32: no counter moved on the refusal" );
    check( at_reset( &value ),
           "R32: nothing decoded — the destination is at its reset image" );

    /* TEST 2 — IDEMPOTENT: the same retired file read again → the same refusal */
    n2 = read_file( bytes, &value, &r2 );
    check( n2 == n1 && r2.refused == r1.refused && r2.reason == r1.reason &&
           r2.refuse_reason == r1.refuse_reason && r2.layout_hash == r1.layout_hash,
           "R32: a second read of the retired hash gives the same refusal (idempotent)" );

    /* TEST 3 — THE CONTRAST: a hash no lineage entry holds, over the current
       layout's bytes, is layout_unknown (§5.3 step 5), never the retired
       layout_newer and never layout_unsupported. */
    stranger = 0xdeadbeefcafebabeull;
    held = 0;
    for ( i = 0; i < tblfixedevo_evo_root_fixed_known_count; ++i )
    {
        if ( tblfixedevo_evo_root_fixed_known[i].hash == stranger ) { held = 1; }
    }
    check( !held, "R32: the stranger hash is in no lineage entry" );
    bytes = make_file( tblfixedevo_evo_root_fixed_known_count - 1, stranger );
    n1 = read_file( bytes, &value, &r1 );
    check( n1 == -1 && r1.refused && r1.layout_hash == stranger && at_reset( &value ),
           "R32 CONTRAST: a hash outside the lineage is refused, reporting the file's hash, landing nothing" );
    printf( "  stranger hash: reason %d (layout_unknown %d, layout_newer %d, layout_unsupported %d)\n", r1.reason,
            (int) SCHEMA_TBLFIXEDEVO_TABLE_LAYOUT_UNKNOWN, (int) SCHEMA_TBLFIXEDEVO_TABLE_LAYOUT_NEWER,
            (int) SCHEMA_TBLFIXEDEVO_TABLE_LAYOUT_UNSUPPORTED );
    check( r1.reason == SCHEMA_TBLFIXEDEVO_TABLE_LAYOUT_UNKNOWN,
           "R32 CONTRAST: the refusal is layout_unknown (§5.3 step 5), never the retired layout_newer" );

    printf( "%s\n", failed ? "R32: RED" : "R32: GREEN" );
    return failed;
}
