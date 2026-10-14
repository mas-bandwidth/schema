/* R22.c — cell c/R22: THE CLOSURE RULE, asserted at the C leg's tip
 * (docs/FIXED-FORM-ALGORITHM.md:961, §5.5; docs/roadmap.sexp c/R22).
 *
 * The law:
 *
 *     closure(T) is T, every `table` or `type` it reaches by value, every
 *     enum, union, flags and constant any of them names, recursively. Every
 *     table or type in it is declared `fixed`; a pointer, map or unbounded
 *     array in it is a compile refusal; T is never in its own closure.
 *
 * The compiler side of the law — REFUSING a schema whose fixed closure
 * carries a pointer, a map or an unbounded array — is the compiler leg's
 * own cell. What this leg asserts is the law's whole face in the EMITTED
 * STORAGE, clause by clause, on the corpus's richest fixed closure, tblv1's
 * Cfg (test/tables/V1.schema):
 *
 *   every table or type reached by value is itself fixed
 *       Cfg embeds the fixed table Cell, the types Inner (twice: inner,
 *       extra) and Boost and Ward (through the union Effect) as plain
 *       by-value members: every one of them lies inside the Cfg object
 *       (R22.3) and inside its size (R22.4).
 *   a pointer, map or unbounded array in it is a compile refusal
 *       nothing unbounded is emitted: every array member of the storage
 *       carries the schema's own declared bound — items 8, slots MaxSlots
 *       6, tally 3, ledger 3, grades 4, podium 3, the three [Slot] arrays
 *       Slot's four variants, Cell's string(8) (R22.2) — and an array or a
 *       struct member placed behind a pointer would sit outside the object,
 *       which R22.3 catches at run time.
 *   every enum, union, flags and constant any of them names
 *       Mode, Grade and Slot and the constants MaxSlots, TallySlots,
 *       LedgerSlots are emitted with the schema's declared extents (R22.1),
 *       and both arms of the union round-trip (R22.7, R22.8). This closure
 *       names no flags type; that clause rides the enum clause — the same
 *       vocabulary shape, the same refusal.
 *   T is never in its own closure
 *       a struct that reached itself by value would have no finite size and
 *       no definition at all, so the file-scope typedef probes below compile
 *       only because every closure size is a compile-time constant — and
 *       the whole closure round-trips by value through the table's own
 *       codec (R22.5 through R22.8).
 *
 * The vector is the corpus's own committed instance of this very closure,
 * testdata/conformance/tables/MANIFEST.txt:88 — instance v1_cfg, tblv1 Cfg,
 * testdata/wire/tables/v1_cfg.bin, 271 bytes — the same wire the driver's
 * `wire` surface loads; no byte is derived here. The fill round trips
 * hand-fill every closure member non-default, both arms of the union, every
 * bounded array at its exact declared bound.
 *
 * One unit per translation unit is the C leg's own load-bearing rule
 * (make/c.mk:103-108: two units' Table headers cannot meet in one C
 * translation unit), so this file includes V1Table.h alone and calls the
 * generated entry points the driver reaches through unit_tblv1.c's codec
 * table (test/conformance/c/main.c surface_wire -> find_codec ->
 * conformance_codecs_tblv1): cfg_load, cfg_save, cfg_measure, generated at
 * build/tables-generated-c/v1/V1Table.h.
 */

#include "V1Table.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* T is never in its own closure: the layout terminates in sizes the compiler
   knows at compile time. A file-scope array bound must be an integer
   constant expression, so these probes compile only if every closure size is
   one — which a self-reaching by-value closure could never give (a struct
   cannot contain itself by value and be defined at all). */
typedef char r22_cfg_size_is_constant[ sizeof( Cfg ) ];
typedef char r22_cell_size_is_constant[ sizeof( Cell ) ];
typedef char r22_inner_size_is_constant[ sizeof( Inner ) ];
typedef char r22_effect_size_is_constant[ sizeof( Effect ) ];

static int r22_failures = 0;

/* one printed line per assertion; exit 1 at the end if any failed */
static void r22_check( int ok, const char * what )
{
    printf( "R22 %s: %s\n", what, ok ? "ok" : "FAIL" );
    if ( !ok ) { r22_failures++; }
}

/* every closure member's bytes lie inside the Cfg object: that is what
   "reached by value" means in storage — no member behind a pointer */
static int r22_inside( const void * object, size_t object_bytes,
                       const void * member, size_t member_bytes )
{
    const char * o = (const char *) object;
    const char * m = (const char *) member;
    return m >= o && m + member_bytes <= o + object_bytes;
}

static uint8_t * r22_slurp( const char * path, size_t * bytes )
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
        fclose( file );
        free( out );
        return NULL;
    }
    fclose( file );
    *bytes = (size_t) size;
    return out;
}

/* every closure member non-default, every bounded array at its exact
   declared bound; `arm` selects which arm of the union rides */
static void r22_fill( Cfg * value, int arm )
{
    int i;
    memset( value, 0, sizeof( *value ) ); /* zeroes padding, so the round-trip memcmp is honest */
    cfg_reset( value );
    value->a = 1000; /* | min = 0, max = 1000 */
    value->b = -0.5f;
    value->mode = MODE_ALPHA;
    memcpy( value->name, "closure", 7 );
    value->name_length = 7;
    value->inner.factor = 9.75f;
    for ( i = 0; i < 8; i++ ) { value->items[i] = 100 + i; } /* | min = 0, max = 255 */
    value->items_count = 8;
    value->grade = GRADE_GOLD;
    for ( i = 0; i < 4; i++ ) { value->grades[i] = ( i % 2 ) ? GRADE_GOLD : GRADE_BRONZE; }
    value->grades_count = 4;
    value->podium[0] = GRADE_BRONZE;
    value->podium[1] = GRADE_NONE;
    value->podium[2] = GRADE_GOLD;
    for ( i = 0; i < MAX_SLOTS; i++ ) { value->slots[i] = 10 * ( i + 1 ); }
    value->slots_count = MAX_SLOTS;
    for ( i = 0; i < TALLY_SLOTS; i++ ) { value->tally[i] = 7 + i; }
    for ( i = 0; i < LEDGER_SLOTS; i++ ) { value->ledger[i] = 70 + i; }
    value->effect.type = arm ? EFFECT_TYPE_WARD : EFFECT_TYPE_BOOST;
    if ( arm ) { value->effect.as.ward.charge = 3.5f; }
    else       { value->effect.as.boost.power = 77; }
    for ( i = 0; i < SLOT_MAX; i++ )
    {
        value->bank[i].power = 100 + i; /* | min = 0, max = 1000 */
        value->bank[i].label[0] = (char) ( 'A' + i );
        value->bank[i].label[1] = 0;
        value->bank[i].label_length = 1;
    }
    for ( i = 0; i < SLOT_MAX; i++ ) { value->tokens[i] = 40 + i; }
    for ( i = 0; i < SLOT_MAX; i++ ) { value->ranks[i] = ( i % 2 ) ? GRADE_GOLD : GRADE_BRONZE; }
    value->extra.factor = -1.25f;
    value->extra_present = 1;
    value->tier = -5;
    value->tier_present = 1;
    value->mark = GRADE_BRONZE;
    value->mark_present = 1;
}

/* the full round trip through the table's own codec: measure, save, load
   into a zeroed twin, and the two closures must agree byte for byte */
static int r22_roundtrip( const Cfg * from, Cfg * into )
{
    int64_t need = cfg_measure( from );
    uint8_t * buffer;
    TableReport report;
    int64_t wrote;
    int ok;
    if ( need <= 0 ) { return 0; }
    buffer = (uint8_t *) malloc( (size_t) need );
    if ( buffer == NULL ) { return 0; }
    wrote = cfg_save( from, buffer, need );
    if ( wrote != need ) { free( buffer ); return 0; }
    memset( into, 0, sizeof( *into ) );
    cfg_reset( into );
    memset( &report, 0, sizeof( report ) );
    ok = cfg_load( into, buffer, wrote, &report )
        && report.unknown == 0 && report.kind_mismatch == 0
        && report.clamped == 0 && report.duplicate == 0
        && report.widened == 0 && !report.malformed && !report.refused;
    free( buffer );
    if ( !ok ) { return 0; }
    return memcmp( from, into, sizeof( *from ) ) == 0;
}

int main( void )
{
    Cfg cfg;
    Cfg twin;
    TableReport report;
    uint8_t * golden;
    size_t golden_bytes = 0;
    int64_t need;
    int64_t wrote;
    uint8_t * buffer;
    int loaded;

    /* R22.1 — every enum and constant the closure names, emitted with the
       schema's declared extents: const MaxSlots = 6, TallySlots = 3,
       LedgerSlots = 3; enum Mode { Alpha, Beta }, Grade { Bronze, Gold },
       Slot { Alpha, Beta, Gamma, Delta }. */
    r22_check( MAX_SLOTS == 6 && TALLY_SLOTS == 3 && LEDGER_SLOTS == 3
        && MODE_COUNT == 2 && MODE_MAX == 2
        && GRADE_COUNT == 2 && GRADE_MAX == 2
        && SLOT_COUNT == 4 && SLOT_MAX == 4,
        "1: the constants and enum extents the closure names are the schema's "
        "own (6, 3, 3; Mode 2, Grade 2, Slot 4)" );

    memset( &cfg, 0, sizeof( cfg ) );
    cfg_reset( &cfg );

    /* R22.2 — the compile-refusal clause's face in the emitted storage: no
       unbounded array exists in the closure, and every array member carries
       the schema's own declared bound. */
    r22_check( sizeof( cfg.items ) == 8 * sizeof( int32_t )
        && sizeof( cfg.slots ) == (size_t) MAX_SLOTS * sizeof( int32_t )
        && sizeof( cfg.tally ) == (size_t) TALLY_SLOTS * sizeof( int32_t )
        && sizeof( cfg.ledger ) == (size_t) LEDGER_SLOTS * sizeof( int32_t )
        && sizeof( cfg.grades ) == 4 * sizeof( Grade )
        && sizeof( cfg.podium ) == 3 * sizeof( Grade )
        && sizeof( cfg.bank ) == (size_t) SLOT_MAX * sizeof( Cell )
        && sizeof( cfg.tokens ) == (size_t) SLOT_MAX * sizeof( int32_t )
        && sizeof( cfg.ranks ) == (size_t) SLOT_MAX * sizeof( Grade )
        && sizeof( cfg.bank[0].label ) == 8 + 1,
        "2: every array in the closure is bounded by its schema-declared "
        "bound (8, 6, 3, 3, 4, 3, Slot's 4, string(8))" );

    /* R22.3 — "reached by value" in storage: the whole of every closure
       member lies inside the Cfg object. A member behind a pointer — the
       pointer a fixed closure must refuse — would place its bytes outside. */
    r22_check( r22_inside( &cfg, sizeof( cfg ), &cfg.inner, sizeof( cfg.inner ) )
        && r22_inside( &cfg, sizeof( cfg ), &cfg.extra, sizeof( cfg.extra ) )
        && r22_inside( &cfg, sizeof( cfg ), &cfg.effect, sizeof( cfg.effect ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.bank, sizeof( cfg.bank ) )
        && r22_inside( &cfg, sizeof( cfg ), &cfg.bank[2], sizeof( cfg.bank[2] ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.items, sizeof( cfg.items ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.slots, sizeof( cfg.slots ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.tally, sizeof( cfg.tally ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.ledger, sizeof( cfg.ledger ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.grades, sizeof( cfg.grades ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.podium, sizeof( cfg.podium ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.tokens, sizeof( cfg.tokens ) )
        && r22_inside( &cfg, sizeof( cfg ), cfg.ranks, sizeof( cfg.ranks ) ),
        "3: every table, type and array the closure reaches by value is "
        "embedded inside the Cfg object (Cell bank, Inner twice, Effect, "
        "every array)" );

    /* R22.4 — the closure is physically part of T's own size: the embedded
       members' sum fits inside sizeof(Cfg). A pointer in any of their
       places would shrink the struct below the sum. */
    r22_check( sizeof( Cfg ) >= (size_t) SLOT_MAX * sizeof( Cell )
        + 2 * sizeof( Inner ) + sizeof( Effect )
        + sizeof( cfg.items ) + sizeof( cfg.slots ) + sizeof( cfg.tally )
        + sizeof( cfg.ledger ) + sizeof( cfg.grades ) + sizeof( cfg.podium )
        + sizeof( cfg.tokens ) + sizeof( cfg.ranks ),
        "4: Cfg's size carries its whole by-value closure inside it" );

    /* R22.5 and R22.6 — the corpus instance of this closure, loaded through
       the same generated reader the driver's wire surface uses and re-saved
       to the golden's own bytes. */
    golden = r22_slurp( "testdata/wire/tables/v1_cfg.bin", &golden_bytes );
    if ( golden == NULL )
    {
        r22_check( 0, "5: the corpus instance of the whole closure loads clean "
            "through cfg_load (cannot read testdata/wire/tables/v1_cfg.bin)" );
        r22_check( 0, "6: the loaded closure re-saves to the golden's own bytes "
            "(golden unreadable)" );
    }
    else
    {
        memset( &cfg, 0, sizeof( cfg ) );
        cfg_reset( &cfg );
        memset( &report, 0, sizeof( report ) );
        loaded = cfg_load( &cfg, golden, (int64_t) golden_bytes, &report );
        r22_check( loaded && report.unknown == 0 && report.kind_mismatch == 0
            && report.clamped == 0 && report.duplicate == 0
            && report.widened == 0 && !report.malformed && !report.refused,
            "5: the corpus instance of the whole closure loads clean through "
            "cfg_load (no unknown, mismatch, clamp, duplicate, widen, refusal)" );

        need = cfg_measure( &cfg );
        buffer = (uint8_t *) malloc( need > 0 ? (size_t) need : 1 );
        wrote = need > 0 ? cfg_save( &cfg, buffer, need ) : -1;
        r22_check( need > 0 && wrote == need && (size_t) wrote == golden_bytes
            && memcmp( buffer, golden, golden_bytes ) == 0,
            "6: the loaded closure re-saves to the golden's own 271 bytes, "
            "measure and save agreeing" );
        free( buffer );
        free( golden );
    }

    /* R22.7 and R22.8 — the closure by value, end to end, through the
       table's own codec: every member hand-filled non-default, every
       bounded array at its exact bound, both arms of the union. */
    r22_fill( &cfg, 0 );
    r22_check( r22_roundtrip( &cfg, &twin ),
        "7: a hand-filled closure round-trips by value through cfg_save and "
        "cfg_load, byte for byte (Effect = Boost arm)" );

    r22_fill( &cfg, 1 );
    r22_check( r22_roundtrip( &cfg, &twin ),
        "8: the same with the union's other arm (Effect = Ward)" );

    if ( r22_failures != 0 )
    {
        printf( "R22: %d assertion(s) failed\n", r22_failures );
        return 1;
    }
    printf( "R22: the closure rule holds at the C leg's tip (8 assertions)\n" );
    return 0;
}
