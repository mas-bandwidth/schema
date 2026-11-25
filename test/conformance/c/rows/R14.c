/* c/R14 — EVERY COMPILED ENTRY'S REACH IS IN THE WRITER'S COORDINATES: a
   union tag grown 1 -> 8 reads the writer's tag at the WRITER's width and
   compiles whole.

   THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15, settled
   2026-11-25): when a union's tag widens between the writer's layout and the
   reader's, the union's None `const` LANDS my_tag bytes in the reader's
   storage but reads the writer's raw tag at THE WRITER'S TAG WIDTH, their_tag
   bytes, never my_tag. Its source reach is src + their_tag, inside the
   writer's record. A reach at the reader's width reads the arm's payload into
   the tag and refuses a lawful file `layout_record_too_large`.

   THE PRODUCTION PATH this row drives, every stage generated into the unit's
   header (build/tables-generated-c/w1/W1Table.h, emitted from
   internal/codegen/ctable/fixedruntime.go):

     table_fixed_parse_layout   the layout's own rules (§1.1)
     table_fixed_compile        the plan compiler: case 15 lays the None const
                                (src their_at, size my_tag, sign their_tag) and
                                table_fixed_push bounds its reach at
                                src + sign against the writer's record
     table_fixed_dead_refuses   the check pass the load runs before the run
     table_fixed_run            the one read loop; the None const reads the
                                writer's raw tag at `sign` bytes and counts a
                                tag past the writer's arm set out_of_range

   The lineage lane (<name>_fixed_lineage_init, internal/codegen/ctable/
   fixedform.go) is this same table_fixed_compile call over a lock entry's
   bytes: `made >= 0` lays a ready plan, `-2` records
   SCHEMA_TBLW1_TABLE_LAYOUT_RECORD_TOO_LARGE on the entry. So the lane for MINE(8)
   is ready exactly when the compile below answers made >= 0.

   THE VECTOR (one for every leg). A layout block is a u32LE entry count then
   17-byte entries: id u64LE, kind u8, size u32LE, children u32LE. Kind 13 a
   table, 15 a union (size = tag + widest arm), 8 a u32.
     THEIRS, the writer: [1,13,5,1] [7,15,5,1] [9,8,4,0] — a record of 5
       bytes, one union whose tag is 1 byte over one u32 arm.
     MINE(tag), the reader: [1,13,tag+4,1] [7,15,tag+4,1] [9,8,4,0], its
       storage the union's tag at 0 (tag bytes) and the u32 arm at `tag`.
   The vector is forged here because no real schema grows a tag past 4 bytes
   (8 needs more than 2^32 arms), so no corpus file can carry it.

   Run (flags per make/conformance-rows.mk; tools/conformancerows builds it):
     cc -std=c11 -Wall -Wextra -Werror <the -I flags> test/conformance/c/rows/R14.c
   Exit 0 green, 1 red; one printed line per assertion. */

/* the unit's source reads locale_t, POSIX.1-2008, and asks for this define
   before the first #include of the translation unit that includes it */
#define _POSIX_C_SOURCE 200809L

#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

/* SELF-CONTAINED, as c/W8: the unit's implementation rides in this
   translation unit, so the header's statics link without the unit's other
   sources and the first link of the row is its only link. */
#include "W1Table.h"
#include "W1Table.c"

static int failures;

static void check( int ok, const char * what )
{
    printf( "%s: R14: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures = 1; }
}

static void put_entry( uint8_t * b, int32_t * at, uint64_t id, uint8_t kind, uint32_t size, uint32_t children )
{
    tblw1_table_fixed_put64( b + *at, id );
    b[*at + 8] = kind;
    tblw1_table_fixed_put32( b + *at + 9, size );
    tblw1_table_fixed_put32( b + *at + 13, children );
    *at += 17;
}

/* THEIRS when tag == 0 (the writer's 1-byte tag), MINE(tag) otherwise. */
static int32_t layout( uint8_t * b, uint32_t tag )
{
    const uint32_t t = tag == 0u ? 1u : tag;
    int32_t at = 4;
    tblw1_table_fixed_put32( b, 3u );
    put_entry( b, &at, 1u, 13u, t + 4u, 1u );
    put_entry( b, &at, 7u, 15u, t + 4u, 1u );
    put_entry( b, &at, 9u, 8u, 4u, 0u );
    return at;
}

/* MY storage rows, by my layout's entry index: the table at 0, the union's
   arms at its own offset 0 with the tag at aux 0, the u32 arm past the tag. */
static void dst_rows( tblw1_table_fixed_dst_t * d, uint32_t tag )
{
    memset( d, 0, 3 * sizeof( *d ) );
    d[0].dst = 0u;
    d[1].dst = 0u;
    d[1].aux = 0u;
    d[2].dst = tag;
}

typedef struct Compiled
{
    tblw1_table_fixed_entry_t plan[512];
    int32_t made;
    int32_t guarded;
    tblw1_table_report_t report;
} Compiled;

static Compiled compiled;

static void compile( uint32_t tag )
{
    static uint8_t theirs_bytes[64], mine_bytes[64];
    tblw1_table_fixed_layout_view_t theirs = tblw1_table_fixed_layout_view_zero();
    tblw1_table_fixed_dst_t d[3];
    const int32_t tn = layout( theirs_bytes, 0u );
    const int32_t mn = layout( mine_bytes, tag );
    uint32_t fill_at = 0;
    int32_t fill_count = 0;
    int why = 0;
    memset( &compiled, 0, sizeof( compiled ) );
    dst_rows( d, tag );
    check( tblw1_table_fixed_parse_layout( theirs_bytes, tn, &theirs, &why ),
           "the writer's layout parses (all of §1.1)" );
    check( tblw1_table_fixed_entry_at( &theirs, 0 ).size == 5u, "the writer's declared record is 5 bytes" );
    compiled.made = tblw1_table_fixed_compile( &theirs, mine_bytes, mn, d, NULL, 0,
                                         compiled.plan, (int32_t) ( sizeof( compiled.plan ) / sizeof( compiled.plan[0] ) ),
                                         &compiled.guarded, &fill_at, &fill_count, &compiled.report );
}

/* THE LOAD'S ORDER over one writer record: the check pass, then the run. */
static void run( const uint8_t * record5, uint8_t * image, size_t bytes, tblw1_table_report_t * r )
{
    /* the record sits in a zeroed buffer wider than 5 bytes, so a reach past
       the writer's record reads zeros and never past the allocation */
    uint8_t src[16];
    memset( src, 0, sizeof( src ) );
    memcpy( src, record5, 5 );
    memset( image, 0xA5, bytes );
    memset( r, 0, sizeof( *r ) );
    if ( tblw1_table_fixed_dead_refuses( compiled.plan, compiled.made, compiled.guarded, src, NULL ) )
    {
        r->refused = 1;
        r->refuse_reason = SCHEMA_TBLW1_TABLE_REFUSE_WIRE_DAMAGED;
        return;
    }
    tblw1_table_fixed_run( compiled.plan, compiled.made, compiled.guarded, src, image, r, NULL, NULL );
}

int main( void )
{
    /* ---- 1. THE CELL: THEIRS against MINE(8) compiles WHOLE --------------- */
    compile( 8u );
    check( compiled.made == 3, "the grown tag (1 -> 8) compiles whole: None const, the arm's tag const, the u32's copy" );
    check( compiled.made != -2 && compiled.report.reason != SCHEMA_TBLW1_TABLE_LAYOUT_RECORD_TOO_LARGE,
           "not layout_record_too_large: no entry reaches past the writer's record of 5" );
    check( !compiled.report.refused, "nothing refused on the compile's report" );
    {
        const tblw1_table_fixed_entry_t * none = NULL;
        const tblw1_table_fixed_entry_t * copy = NULL;
        int32_t i;
        for ( i = 0; i < compiled.made; ++i )
        {
            const tblw1_table_fixed_entry_t * e = &compiled.plan[i];
            if ( e->op == TBLW1_K_TABLE_FIXED_CONST && e->aux == 0u && e->dstsize != 0u ) { none = e; }
            if ( e->op == TBLW1_K_TABLE_FIXED_COPY ) { copy = e; }
        }
        /* ---- 2. THE None CONST: src 0, lands 8, reads the writer's 1 ----- */
        check( none != NULL, "the union's None const is in the plan" );
        if ( none != NULL )
        {
            check( none->src == 0u, "the None const reads the writer's tag at its offset 0" );
            check( none->size == 8u, "the None const LANDS my 8-byte tag (size 8)" );
            check( none->sign == 1u, "the None const reads the WRITER's 1-byte tag (sign 1), never 8" );
            check( none->dstsize == 1u, "it carries the writer's arm count, 1" );
            check( none->guard == SCHEMA_TBLW1_TABLE_FIXED_NO_GUARD, "the None const is unguarded" );
            check( (uint64_t) none->src + (uint64_t) none->sign <= 5u,
                   "its reach, 0 + 1, is inside the writer's record of 5" );
        }
        check( copy != NULL && copy->src == 1u && copy->size == 4u && copy->dst == 8u,
               "the u32 arm copies 4 bytes from src 1 (past the WRITER's 1-byte tag) to my image 8" );
    }

    /* ---- 3. RUN the plan the grown tag lays -------------------------------- */
    {
        static const uint8_t arm1[5] = { 1u, 0x44u, 0x33u, 0x22u, 0x11u };
        static const uint8_t none0[5] = { 0u, 0u, 0u, 0u, 0u };
        static const uint8_t forged[5] = { 2u, 0u, 0u, 0u, 0u };
        uint8_t image[12];
        uint64_t tag;
        uint32_t u;
        tblw1_table_report_t r;

        run( arm1, image, sizeof( image ), &r );
        memcpy( &tag, image, 8 );
        memcpy( &u, image + 8, 4 );
        check( !r.refused, "arm 1 reads with no refusal" );
        check( tag == 1u, "my 8-byte tag lands exactly 1 (high bytes zero; the payload is never read as the tag)" );
        check( u == 0x11223344u, "the u32 lands whole past my tag: 0x11223344" );

        run( none0, image, sizeof( image ), &r );
        memcpy( &tag, image, 8 );
        check( !r.refused && tag == 0u, "tag 0 reads as None in my 8-byte tag" );

        run( forged, image, sizeof( image ), &r );
        check( r.refused && r.refuse_reason == SCHEMA_TBLW1_TABLE_REFUSE_OUT_OF_RANGE,
               "a forged writer tag 2 (past one arm) refuses out_of_range (§4.6)" );
    }

    /* ---- 4. THE TWINS: MINE(1) and MINE(2) compile whole too --------------- */
    compile( 1u );
    check( compiled.made > 0 && !compiled.report.refused, "THEIRS against MINE(1) (the same layout) compiles whole" );
    compile( 2u );
    check( compiled.made > 0 && !compiled.report.refused, "THEIRS against MINE(2) (the tag grown 1 -> 2) compiles whole" );

    /* ---- 5. THE LINEAGE LANE: the same compile the lineage init runs ------- */
    compile( 8u );
    check( compiled.made >= 0 && compiled.report.reason != SCHEMA_TBLW1_TABLE_LAYOUT_RECORD_TOO_LARGE,
           "the lineage lane for MINE(8) lays a ready plan (made >= 0, no reason recorded)" );

    if ( failures != 0 )
    {
        printf( "R14: FAILED\n" );
        return 1;
    }
    printf( "R14: a grown union tag reads the writer's tag width and compiles whole — green\n" );
    return 0;
}
