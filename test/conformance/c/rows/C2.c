/* c/C2 — count clamp v>Max (audit schema#898, matrix schema#876, roadmap c/C2).
 *
 * THE LAW (docs/FIXED-FORM-ALGORITHM.md §4.5, the scatter, the `count` op):
 *   "v := SLE(4, record+src); if v < 0 then v := 0, COUNT clamped, else if
 *    v > size then v := size, COUNT clamped; PUT(4, out+dst, v). The bound is
 *    the READER's own Max, in ELEMENTS."
 * A count word greater than Max lands Max — never the forged value, never a
 * byte-wide reading of the bound — and counts `clamped` exactly ONCE for the
 * field (§5.4, once per entry per record). count == Max itself is lawful and
 * counts nothing: the clamp is `v > Max`, never `v >= Max`.
 *
 * THE PRODUCTION PATH THIS TEST DRIVES (the same path the conformance driver's
 * `report` surface reaches through its codec table): the caller's entry point
 * cfg_fixed_load() (build/tables-generated-c/v1/V1Table.h), the file's own hash
 * selecting the static identity plan cfg_fixed_plan, table_fixed_run() walking
 * it, and table_fixed_apply()'s case kTableFixedCount doing the clamp. The
 * generated bounds pass that runs after the loop (schema_tblv1_cfg_fixed_clamp_)
 * holds ranged scalars only — it never touches a count — so `clamped` here is
 * the count op's own, and asserting it pins the op and nothing else.
 *
 * THE VECTOR is this build's own Cfg record — the emitter's own bytes, with ONE
 * field forged: the count word of `items [..8]int32` (Max = 8) is put at 1000,
 * a value the WRITER could not have written (cfg_fixed_save refuses no count,
 * but a count of 1000 past Max 8 is precisely the hostile word §4.5 exists
 * for). The plan entry carrying the count op is found by its destination
 * (offsetof(Cfg, items_count)), so the forge lands on the word the law bounds
 * and not on a neighbouring one.
 *
 * Standalone: depends on no other rows/ file, edits no shared file.
 * Run (flags per make/c.mk's build/conformance-c, C_CONFORMANCE_INCLUDES):
 *   cc -std=c11 -Wall <the -I flags> test/conformance/c/rows/C2.c \
 *      build/tables-generated-c/v1/V1Table.c -o build/rows-c-C2 -lm \
 *   && ./build/rows-c-C2
 */

#include <stddef.h>
#include <stdio.h>

#include "V1Table.h"

static int failures;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures = 1; }
}

int main( void )
{
    static uint8_t file[16384];
    const TableFixedEntry * count = NULL;
    Cfg v, back;
    TableReport r;
    uint8_t * body;
    int64_t n;
    int i;

    /* The identity plan carries the count op for items, bound Max = 8. */
    for ( i = 0; i < cfg_fixed_plan_count; ++i )
    {
        if ( cfg_fixed_plan[i].op == kTableFixedCount && cfg_fixed_plan[i].dst == (uint32_t) offsetof( Cfg, items_count ) )
        {
            count = &cfg_fixed_plan[i];
        }
    }
    check( count != NULL, "c/C2: the identity plan carries a count op for items" );
    if ( count == NULL ) { return 1; }
    check( count->size == 8, "c/C2: the count op's bound is Max, in ELEMENTS (8, not the field's 32 bytes)" );

    /* THE BOUNDARY: count == Max is lawful and counts nothing. */
    cfg_reset( &v );
    v.items_count = 8;
    for ( i = 0; i < 8; ++i ) { v.items[i] = 100 + i; }
    n = cfg_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    check( n == cfg_fixed_measure( 1 ), "c/C2: the boundary record (items_count == Max) saves" );
    memset( &r, 0, sizeof( r ) );
    check( cfg_fixed_load( &back, 1, file, n, NULL, 0, NULL, &r ) == 1, "c/C2 BOUNDARY: count == Max reads" );
    check( back.items_count == 8 && back.items[0] == 100 && back.items[7] == 107,
           "c/C2 BOUNDARY: count == Max lands whole" );
    check( r.clamped == 0, "c/C2 BOUNDARY: count == Max counts nothing — the clamp is v > Max, never v >= Max" );

    /* THE FORGE: one field — the count word — to 1000 > Max. */
    body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( cfg_fixed_layout ) + 8;
    table_fixed_put32( body + count->src, 1000u );

    memset( &back, 0, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    check( cfg_fixed_load( &back, 1, file, n, NULL, 0, NULL, &r ) == 1,
           "c/C2: a count forged past Max still reads — clamped, never refused, never malformed" );
    check( back.items_count == 8,
           "c/C2 CLAMP: the count lands Max (8), never the forged 1000, never 0" );
    check( back.items[0] == 100 && back.items[1] == 101 && back.items[2] == 102 && back.items[3] == 103 &&
           back.items[4] == 104 && back.items[5] == 105 && back.items[6] == 106 && back.items[7] == 107,
           "c/C2: every live element behind the clamped count lands exactly" );
    check( r.clamped == 1, "c/C2 COUNTED: clamped == 1 exactly — once per entry per record (§5.4), never 0, never 2" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.duplicate == 0,
           "c/C2: and no other counter moves" );
    check( !r.malformed && !r.refused, "c/C2: the read is a read — malformed and refused both stay false" );

    printf( "%s\n", failures ? "c/C2: RED" : "c/C2: GREEN" );
    return failures;
}
