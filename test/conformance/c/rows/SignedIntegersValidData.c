/* test/conformance/c/rows/SignedIntegersValidData.c — schema roadmap task
 * signed-integers/c/valid-data: "Signed integers: 8, 16, 32 and 64 bits:
 * valid-data write/read acceptance" (docs/roadmap.sexp, row signed-integers;
 * audit schema#898, matrix schema#876).
 *
 * THE LAW (docs/FIXED-FORM-ALGORITHM.md:156, §3 "The record and the writer"):
 *   "No per-field reference, no kind byte, no length, no terminator, no
 *    trailer. Every field rides as its declared storage image, little-endian,
 *    at its declared storage width, nothing padded between fields."
 * and the kind row the contract names (docs/FIXED-FORM-ALGORITHM.md:171,
 * contract docs/SPEC-TABLES.md:6404, §3.4 record, int8..int64):
 *   "int8..uint64, and a RANGED integer | the declared storage width | the
 *    bounds do not ride"
 * so a valid record of the four signed widths must write each value as its
 * little-endian image at 1, 2, 4 and 8 bytes respectively — min and max of the
 * STORAGE TYPE included, never clamped, never sign-extended to a neighbour's
 * byte — and read every one of them back exactly, silently.
 *
 * THE GAP THIS CLOSES: the roadmap's cited C-leg evidence
 * (internal/codegen/ctable/fixedversioning_test.go:137) drives one width's
 * int widen in the versioning corpus; the row's own words say "one tested
 * width is insufficient for this grouped row". RangedSigned
 * (tables/examples/Ranges.schema) is the corpus's one table that is ALL
 * signed integers — four fields per width, the span fields declared AT the
 * storage limits [-128,127] [-32768,32767] [INT32_MIN,INT32_MAX]
 * [INT64_MIN,INT64_MAX] — and no conformance surface reaches it: the
 * tabledemo codec table (test/conformance/c/unit_tabledemo.c) lists no
 * Ranges root, and no wire golden for it exists (the MANIFEST's tabledemo
 * instances stop at keyed_default).
 *
 * THE PRODUCTION PATH THIS TEST DRIVES is the same fixed-form path the
 * driver's other roots reach through their codec tables: the caller's entry
 * points ranged_signed_fixed_save() (build/tables-generated-c/examples/
 * RangesTable.h:8770) and ranged_signed_fixed_load() (RangesTable.h:8812),
 * the emitted store line schema_tabledemo_ranged_signed_fixed_write_body_
 * putting every width at its declared offset, the identity plan
 * ranged_signed_fixed_plan's copies landing them again, and the read-side
 * bounds pass schema_tabledemo_ranged_signed_fixed_clamp_, which must fire
 * on nothing here. The unit is the leg's own build target's: the
 * build/tables-generated-c/.stamp rule generates it and
 * make/c.mk:188 links build/tables-generated-c/examples/RangesTable.c into
 * build/conformance-c.
 *
 * THE VECTOR is constructed from the law, not read from a golden, because
 * the tree carries none for this root. Per §3's kind table, the record body
 * is the fields in declared order, every field at its declared storage
 * width, little-endian, nothing between:
 *
 *   off  size  field
 *    0     1  i8_span .. i8_inside (int8, one byte each, offsets 0..3)
 *    4     2  i16_span .. i16_inside (int16 LE, offsets 4,6,8,10)
 *   12     4  i32_span .. i32_inside (int32 LE, offsets 12,16,20,24)
 *   28     8  i64_span .. i64_inside (int64 LE, offsets 28,36,44,52)
 *   60     4  edges_count (i32 LE — [..4]int16 rides "the count THEN Max
 *            elements", docs/FIXED-FORM-ALGORITHM.md:179)
 *   64     8  edges[0..3] (int16 LE × 4)
 *   = 72 bytes, and NOT ONE MORE: the bounds do not ride, no kind byte, no
 *   terminator. A record is the 8-byte layout hash then this body, 80 bytes;
 *   the file is form byte 3, seven zero bytes, the hash at 8, the layout
 *   length at 16, the layout, then the records.
 *
 * Three records: MIN (every width at its storage minimum, the low/high/
 * inside fields at their declared minimum — a value the sign bit alone
 * makes), MAX (every width at its storage maximum), and CROSS (interior
 * sign-crossing values — -1, 0, 1 — with the int16 element lane carrying
 * both edges and the sign boundary: -32768, -1, 0, 32767).
 *
 * Standalone: depends on no other rows/ file, links only the examples unit's
 * generated RangesTable.c, edits no shared file.
 * Run (flags per make/c.mk's build/conformance-c, C_CONFORMANCE_INCLUDES):
 *   cc -std=c11 -Wall <the -I flags> test/conformance/c/rows/SignedIntegersValidData.c \
 *      build/tables-generated-c/examples/RangesTable.c -o build/rows-c-SignedIntegersValidData -lm \
 *   && ./build/rows-c-SignedIntegersValidData
 */

#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "RangesTable.h"

static int failures;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures = 1; }
}

/* THE EXPECTED VECTOR'S OWN PACKERS, spelled independently of the generated
 * code on purpose: little-endian means the LEAST significant byte first
 * (§3), and if a generated put ever wrote big-endian or a padded width, this
 * side would not follow it — the memcmp below would go red, not green. */
static void pack8( uint8_t * b, int8_t v ) { b[0] = (uint8_t) v; }
static void pack16( uint8_t * b, int16_t v )
{
    uint16_t u = (uint16_t) v;
    b[0] = (uint8_t) ( u );
    b[1] = (uint8_t) ( u >> 8 );
}
static void pack32( uint8_t * b, int32_t v )
{
    uint32_t u = (uint32_t) v;
    int i;
    for ( i = 0; i < 4; ++i ) { b[i] = (uint8_t) ( u >> ( 8 * i ) ); }
}
static void pack64( uint8_t * b, int64_t v )
{
    uint64_t u = (uint64_t) v;
    int i;
    for ( i = 0; i < 8; ++i ) { b[i] = (uint8_t) ( u >> ( 8 * i ) ); }
}

/* The 72-byte record body the law derives from the record above, built from
 * the SAME values the save was handed, so the memcmp pins every byte: each
 * width at its declared size, little-endian, and the bounds nowhere. */
static void pack_body( uint8_t * b, const RangedSigned * v )
{
    pack8( b + 0, v->i8_span );
    pack8( b + 1, v->i8_low );
    pack8( b + 2, v->i8_high );
    pack8( b + 3, v->i8_inside );
    pack16( b + 4, v->i16_span );
    pack16( b + 6, v->i16_low );
    pack16( b + 8, v->i16_high );
    pack16( b + 10, v->i16_inside );
    pack32( b + 12, v->i32_span );
    pack32( b + 16, v->i32_low );
    pack32( b + 20, v->i32_high );
    pack32( b + 24, v->i32_inside );
    pack64( b + 28, v->i64_span );
    pack64( b + 36, v->i64_low );
    pack64( b + 44, v->i64_high );
    pack64( b + 52, v->i64_inside );
    pack32( b + 60, v->edges_count );
    pack16( b + 64, v->edges[0] );
    pack16( b + 66, v->edges[1] );
    pack16( b + 68, v->edges[2] );
    pack16( b + 70, v->edges[3] );
}

static int same_record( const RangedSigned * a, const RangedSigned * b )
{
    return a->i8_span == b->i8_span && a->i8_low == b->i8_low
        && a->i8_high == b->i8_high && a->i8_inside == b->i8_inside
        && a->i16_span == b->i16_span && a->i16_low == b->i16_low
        && a->i16_high == b->i16_high && a->i16_inside == b->i16_inside
        && a->i32_span == b->i32_span && a->i32_low == b->i32_low
        && a->i32_high == b->i32_high && a->i32_inside == b->i32_inside
        && a->i64_span == b->i64_span && a->i64_low == b->i64_low
        && a->i64_high == b->i64_high && a->i64_inside == b->i64_inside
        && a->edges_count == b->edges_count && a->edges[0] == b->edges[0]
        && a->edges[1] == b->edges[1] && a->edges[2] == b->edges[2]
        && a->edges[3] == b->edges[3];
}

static int silent( const TableReport * r )
{
    return r->unknown == 0 && r->kind_mismatch == 0 && r->widened == 0
        && r->clamped == 0 && r->duplicate == 0 && !r->malformed && !r->refused;
}

int main( void )
{
    /* MIN: the signedness edge the sign bit alone makes, at every width.
     * The low/high/inside fields sit one inside a storage limit, so their
     * declared minimum is the value they carry — valid data, never a clamp. */
    RangedSigned mins;
    /* MAX: the other edge of the same storage types. */
    RangedSigned maxs;
    /* CROSS: -1, 0, 1 across the sign boundary at every width, and the
     * int16 ELEMENT lane carrying both storage edges and the boundary. */
    RangedSigned cross;
    RangedSigned back[3];
    TableReport report;
    static uint8_t file[8192];
    static uint8_t body[72];
    const int64_t layout_bytes = (int64_t) sizeof( ranged_signed_fixed_layout );
    const int64_t records_at = kTableFixedHeaderBytes + 4 + layout_bytes;
    const int64_t one = ranged_signed_fixed_measure( 1 );
    int64_t n;

    memset( &mins, 0, sizeof( mins ) );
    mins.i8_span = -128; mins.i8_low = -128; mins.i8_high = -127; mins.i8_inside = -127;
    mins.i16_span = -32768; mins.i16_low = -32768; mins.i16_high = -32767; mins.i16_inside = -32767;
    mins.i32_span = INT32_MIN; mins.i32_low = INT32_MIN; mins.i32_high = INT32_MIN + 1; mins.i32_inside = INT32_MIN + 1;
    mins.i64_span = INT64_MIN; mins.i64_low = INT64_MIN; mins.i64_high = INT64_MIN + 1; mins.i64_inside = INT64_MIN + 1;
    mins.edges_count = 4;
    mins.edges[0] = -32768; mins.edges[1] = -32768; mins.edges[2] = -32768; mins.edges[3] = -32768;

    memset( &maxs, 0, sizeof( maxs ) );
    maxs.i8_span = 127; maxs.i8_low = 126; maxs.i8_high = 127; maxs.i8_inside = 126;
    maxs.i16_span = 32767; maxs.i16_low = 32766; maxs.i16_high = 32767; maxs.i16_inside = 32766;
    maxs.i32_span = INT32_MAX; maxs.i32_low = INT32_MAX - 1; maxs.i32_high = INT32_MAX; maxs.i32_inside = INT32_MAX - 1;
    maxs.i64_span = INT64_MAX; maxs.i64_low = INT64_MAX - 1; maxs.i64_high = INT64_MAX; maxs.i64_inside = INT64_MAX - 1;
    maxs.edges_count = 4;
    maxs.edges[0] = 32767; maxs.edges[1] = 32767; maxs.edges[2] = 32767; maxs.edges[3] = 32767;

    memset( &cross, 0, sizeof( cross ) );
    cross.i8_span = -1; cross.i8_low = 0; cross.i8_high = 1; cross.i8_inside = -1;
    cross.i16_span = -1; cross.i16_low = 1; cross.i16_high = -1; cross.i16_inside = 0;
    cross.i32_span = 0; cross.i32_low = 1; cross.i32_high = -1; cross.i32_inside = 0;
    cross.i64_span = -1; cross.i64_low = 1; cross.i64_high = 0; cross.i64_inside = -1;
    cross.edges_count = 4;
    cross.edges[0] = -32768; cross.edges[1] = -1; cross.edges[2] = 0; cross.edges[3] = 32767;

    /* ---- the framing §3.4 states, and the record the law derives ------- */

    check( one - ( kTableFixedHeaderBytes + 4 + layout_bytes ) == 8 + 72,
           "signed-integers/c/valid-data: a record is the hash plus a 72-byte body — 4*1 + 4*2 + 4*4 + 4*8 + 4 + 4*2, and the bounds do not ride" );
    check( ranged_signed_fixed_known[0].record_bytes == 8 + 72,
           "signed-integers/c/valid-data: the lock's record_bytes is the same 80, never a width-a-past bound" );

    /* ---- WRITE: three valid records, the whole edge range -------------- */

    {
        RangedSigned all[3];
        all[0] = mins; all[1] = maxs; all[2] = cross;
        n = ranged_signed_fixed_save( all, 3, file, (int64_t) sizeof( file ) );
    }
    check( n == ranged_signed_fixed_measure( 3 ), "signed-integers/c/valid-data: the edge records save, whole" );
    check( file[0] == kTableFixedForm && table_fixed_get64( file + kTableFixedHashAt ) == ranged_signed_fixed_hash,
           "signed-integers/c/valid-data: form byte 3 and the layout's own hash head the file" );

    /* ---- the vector byte for byte, every width at its declared size ----- */

    memset( body, 0, sizeof( body ) );
    pack_body( body, &mins );
    check( memcmp( file + records_at + 8, body, 72 ) == 0,
           "signed-integers/c/valid-data MIN: int8 -128, int16 -32768, int32 0x80000000, int64 0x8000000000000000 ride as their LE images at 1, 2, 4 and 8 bytes" );
    memset( body, 0, sizeof( body ) );
    pack_body( body, &maxs );
    check( memcmp( file + records_at + 80 + 8, body, 72 ) == 0,
           "signed-integers/c/valid-data MAX: int8 127, int16 32767, int32 0x7fffffff, int64 0x7fffffffffffffff ride the same way" );
    memset( body, 0, sizeof( body ) );
    pack_body( body, &cross );
    check( memcmp( file + records_at + 160 + 8, body, 72 ) == 0,
           "signed-integers/c/valid-data CROSS: -1, 0, 1 and the element lane's -32768..32767 ride the same way, count first" );

    /* ---- READ: acceptance is silent, and the values land exactly -------- */

    memset( back, 0x5A, sizeof( back ) ); /* nothing may survive the read but a landed value */
    memset( &report, 0, sizeof( report ) );
    check( ranged_signed_fixed_load( back, 3, file, n, NULL, 0, NULL, &report ) == 3,
           "signed-integers/c/valid-data: all three records read" );
    check( silent( &report ),
           "signed-integers/c/valid-data: valid edges clamp NOTHING and refuse NOTHING — no counter moves" );

    check( same_record( &mins, &back[0] ),
           "signed-integers/c/valid-data: int8 -128, int16 -32768, int32 INT32_MIN and int64 INT64_MIN round-trip EXACTLY" );
    check( same_record( &maxs, &back[1] ),
           "signed-integers/c/valid-data: int8 127, int16 32767, int32 INT32_MAX and int64 INT64_MAX round-trip EXACTLY" );
    check( same_record( &cross, &back[2] ),
           "signed-integers/c/valid-data: -1, 0 and 1 round-trip exactly at every width, elements included" );

    /* per width, named, so a red names the width: int8 */
    check( back[0].i8_span == -128 && back[1].i8_span == 127 && back[2].i8_span == -1,
           "signed-integers/c/valid-data: the 8-bit width lands every edge exactly" );
    /* int16 */
    check( back[0].i16_span == -32768 && back[1].i16_span == 32767 && back[2].i16_span == -1,
           "signed-integers/c/valid-data: the 16-bit width lands every edge exactly" );
    /* int32 */
    check( back[0].i32_span == INT32_MIN && back[1].i32_span == INT32_MAX && back[2].i32_span == 0,
           "signed-integers/c/valid-data: the 32-bit width lands every edge exactly" );
    /* int64 */
    check( back[0].i64_span == INT64_MIN && back[1].i64_span == INT64_MAX && back[2].i64_span == -1,
           "signed-integers/c/valid-data: the 64-bit width lands every edge exactly" );
    /* the element lane */
    check( back[2].edges_count == 4 && back[2].edges[0] == -32768 && back[2].edges[1] == -1
           && back[2].edges[2] == 0 && back[2].edges[3] == 32767,
           "signed-integers/c/valid-data: a [..4]int16 carries the 16-bit edges as elements, exactly" );

    /* ---- and a re-save of the read is the same bytes: acceptance twice -- */

    {
        static uint8_t reread[8192];
        int64_t m = ranged_signed_fixed_measure( 3 );
        int64_t short_capacity = ranged_signed_fixed_save( back, 3, reread, m - 1 );
        int64_t w;
        check( short_capacity == -1, "signed-integers/c/valid-data: a capacity one short of the measure refuses rather than truncating" );
        w = ranged_signed_fixed_save( back, 3, reread, (int64_t) sizeof( reread ) );
        check( w == m && memcmp( reread, file, (size_t) m ) == 0,
               "signed-integers/c/valid-data: read then write is byte-identical — the values never moved" );
    }

    printf( "%s\n", failures ? "signed-integers/c/valid-data: RED" : "signed-integers/c/valid-data: GREEN" );
    return failures;
}
