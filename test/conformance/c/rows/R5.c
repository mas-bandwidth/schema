/* test/conformance/c/rows/R5.c — cell c/R5, standalone (docs/roadmap.sexp:1484).
 *
 * THE LAW (docs/FIXED-FORM-ALGORITHM.md:1130, §5.8 row 10, audit schema#898 /
 * matrix schema#876): the definitions digest the fixed-form hash folds in
 * carries EVERY RANGE (an 'R' row per ranged field, bounds as i64 LE), EVERY
 * RESOLUTION (tag 'Q' — none in this unit's corpus, see the note at the foot),
 * EVERY READER LIMIT (tag 'L' — reserved: no table spelling exists, so the
 * row stays empty), and A FLAGS TYPE DEDUPED BY NAME, ONCE, as a struct is
 * (§5.2: "Structs, flags and unions share one seen map keyed by bare name").
 *
 * THE PRODUCTION PATH THIS EXERCISES (not a helper): the schema compiler
 * computes the digest at the hash site from the schema — ir.TableFixedLayoutHash
 * (ir/fixedform.go:613) calls ir.TableFixedDefinitionsDigest (ir/fixedform.go:651)
 * with no digest argument a leg could omit — and the C leg's emitter folds that
 * hash into the generated constant at internal/codegen/ctable/fixedform.go:364,
 * emitted as `<T>_fixed_hash` (fixedform.go:373). The fixed-form writer puts
 * that constant into every file's header and every record
 * (fixedform.go:567, fixedform.go:572). So the observable the C leg owns is the
 * baked constant and the bytes its own writer saves — what this file pins.
 *
 * THE DERIVATION: the digest never rides the wire and the generated C carries
 * no digest bytes, so the expected digest is derived HERE, per row, from the
 * byte table the law states (§5.2), for the tables of the unit
 * tables/examples/Tables.schema (package tabledemo). The layout bytes are NOT
 * derived: they are read from the generated artifact itself
 * (`<t>_fixed_layout`, "a PRE-ORDER walk of the closure in the writer's
 * declared order. Every byte is settled by the schema compiler"), so the pin
 * has exactly one free variable — the digest — and one bound constant. The
 * identity checked is the law's own: HASH(layout_bytes, T) = fnv1a64 over the
 * layout bytes and then over DIGEST(T).
 *
 * The closure of RootConfig, walked as §1.1 walks it (declared field order,
 * depth first, each NAMED type emitted once — the seen map is keyed by bare
 * name), with every row the law's table derives:
 *
 *   RootConfig
 *     version_note string(16)          no range, no bits/fixed/flags: no row
 *     weapons [..8]WeaponConfig        recurse the element type, once:
 *       damage float32 = 21.0            a float WITHOUT a range: no row, and
 *                                      no 'Q' either — only a float RANGE
 *                                      carries the resolution row
 *       speed float32 = 500.0            no row
 *       penetration int32 | min=0, max=10
 *                                      'R' + i64 LE 0 + i64 LE 10
 *       channel bits(6)                  'B' + u32 LE 6
 *       homing bool                      no row
 *       effect Effect                    a union: recurse into each arm's
 *                                      payload, in declared order, no row of
 *                                      its own while unseen:
 *         buff Buff   -> multiplier float32   no row
 *         debuff Debuff -> amount int32 | min=0, max=100
 *                                      'R' + i64 LE 0 + i64 LE 100
 *     profiles [..4]ProfileConfig      recurse the element type, once:
 *       name string(32), icon bytes(16), experience..epoch, precision
 *       float64, ratings [4]float32, has_loadout bool
 *                                      no rows
 *       loadout LoadoutConfig          recurse:
 *         grade Grade, grades [..4]Grade, podium [3]Grade
 *                                      enums carry no digest row
 *         perks Perks                  a FLAGS type, FIRST NAMING FIELD:
 *                                      'F' + u32 LE 3 (three variants, no
 *                                      explicit width: one bit per flag), then
 *                                      per flag 'f' + fnv1a64(name) u64 LE —
 *                                      "Shielded", "Cloaked", "Turbo"
 *         primary WeaponConfig         SEEN (deduped by bare name): no rows —
 *                                      though the closure names this type
 *                                      through THREE naming fields (weapons,
 *                                      primary, backups), the rows above are
 *                                      carried once
 *         backups [2]WeaponConfig      SEEN: no rows
 *         attachments [..8]Attachment  recurse the element type:
 *           slot int32 | min=0, max=7  'R' + i64 LE 0 + i64 LE 7
 *           power float32              no row
 *
 *   WeaponConfig's own digest is its three rows above; LoadoutConfig's is the
 *   flags block FIRST (perks is declared before primary) then the three
 *   WeaponConfig rows then Attachment's; ProfileConfig's is LoadoutConfig's
 *   own, its one digest-bearing descendant. Every byte is in the law's table;
 *   nothing else is in the digest — in particular no 'L': 'L' is RESERVED
 *   until a table-declared reader-side limit exists (§5.2), and an exact pin
 *   refuses one the day somebody emits it.
 *
 * THE NEGATIVE CONTROLS (recorded in RESULT.md): flipping one byte of the
 * derived digest vector (the flags wire bit count 3 -> 2) turns every pin red;
 * so does flipping one byte of the generated `root_config_fixed_hash`
 * constant in build/tables-generated-c/examples/TablesTable.h — the test reads
 * the artifact, not its own arithmetic twice.
 *
 * Runs standalone: cc -std=c11 -Wall <the -I and link flags make/c.mk gives
 * build/conformance-c> test/conformance/c/rows/R5.c -o build/rows-c-R5 && ./build/rows-c-R5
 * Exit 0 green, 1 red; one printed line per assertion.
 *
 * ONE TRANSLATION UNIT: the generated unit's descriptors and text form are
 * defined in TablesTable.c (the header only declares them — the same split the
 * driver builds as two TUs), so this file includes the generated .c after the
 * header and stands alone; it links nothing else and depends on no other
 * rows/ file.
 */

#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "TablesTable.h"
#include "TablesTable.c"

static int g_failures;

static void check(int ok, const char * what)
{
    printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { g_failures++; }
}

/* fnv1a64, the hash the law states (§5.2): offset 0xcbf29ce484222325,
   prime 0x100000001b3, no fold, no rebound. */
static uint64_t r5_fnv1a64( const uint8_t * b, int64_t n )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < n; ++i ) { h ^= (uint64_t) b[i]; h *= 0x100000001b3ull; }
    return h;
}

/* THE DIGEST VECTOR, built one law-row at a time. 512 bytes is four times the
   largest derivation below (RootConfig's, 88 bytes) and then some. */
typedef struct { uint8_t b[512]; int n; } r5_digest;

static void r5_put_u32( r5_digest * d, uint32_t v )
{
    int i;
    for ( i = 0; i < 4; ++i ) { d->b[d->n++] = (uint8_t)( v >> ( 8 * i ) ); }
}

static void r5_put_u64( r5_digest * d, uint64_t v )
{
    int i;
    for ( i = 0; i < 8; ++i ) { d->b[d->n++] = (uint8_t)( v >> ( 8 * i ) ); }
}

/* an INTEGER range: 'R', then min and max, each as an i64 LE. */
static void r5_row_range( r5_digest * d, int64_t min, int64_t max )
{
    d->b[d->n++] = (uint8_t) 'R';
    r5_put_u64( d, (uint64_t) min );
    r5_put_u64( d, (uint64_t) max );
}

/* bits(N): 'B', then N as u32 LE. */
static void r5_row_bits( r5_digest * d, uint32_t width )
{
    d->b[d->n++] = (uint8_t) 'B';
    r5_put_u32( d, width );
}

/* a flags type: 'F', its wire bit count as u32 LE, then per flag 'f' and
   fnv1a64(name) as u64 LE. A flags type is a NAMED type: the second naming
   field finds it in the seen map and emits nothing. */
static void r5_row_flags( r5_digest * d, uint32_t wirebits, int nflags, const char * const * names )
{
    int i;
    d->b[d->n++] = (uint8_t) 'F';
    r5_put_u32( d, wirebits );
    for ( i = 0; i < nflags; ++i )
    {
        d->b[d->n++] = (uint8_t) 'f';
        r5_put_u64( d, r5_fnv1a64( (const uint8_t *) names[i], (int64_t) strlen( names[i] ) ) );
    }
}

/* HASH(layout_bytes, T): fnv1a64 over the layout's bytes exactly as written,
   then over DIGEST(T) — the digest computed at the hash site, never an
   argument a caller supplies. The layout side is the generated artifact's own
   array; nothing about it is derived here. */
static uint64_t r5_hash_of( const uint8_t * layout, int64_t layout_n, const r5_digest * d )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < layout_n; ++i ) { h ^= (uint64_t) layout[i]; h *= 0x100000001b3ull; }
    for ( i = 0; i < d->n; ++i ) { h ^= (uint64_t) d->b[i]; h *= 0x100000001b3ull; }
    return h;
}

static uint64_t r5_le64( const uint8_t * b )
{
    uint64_t v = 0;
    int i;
    for ( i = 7; i >= 0; --i ) { v = ( v << 8 ) | (uint64_t) b[i]; }
    return v;
}

int main( void )
{
    /* Perks, from tables/examples/Tables.schema line 22:
       flags Perks { Shielded, Cloaked, Turbo } — three variants, no explicit
       width, so the wire bit count is one bit per flag: 3. */
    static const char * const perks[3] = { "Shielded", "Cloaked", "Turbo" };

    /* WeaponConfig: 'R' 0..10 (penetration), 'B' 6 (channel bits(6)),
       'R' 0..100 (effect -> debuff Debuff.amount; the buff arm's multiplier
       float32 carries no range and so no row and no 'Q'). */
    r5_digest weapon;
    memset( &weapon, 0, sizeof( weapon ) );
    r5_row_range( &weapon, 0, 10 );
    r5_row_bits( &weapon, 6 );
    r5_row_range( &weapon, 0, 100 );

    /* LoadoutConfig: the flags block FIRST (perks is declared before primary),
       then primary WeaponConfig's three rows, then Attachment's — backups
       [2]WeaponConfig is deduped by name and emits nothing. */
    r5_digest loadout;
    memset( &loadout, 0, sizeof( loadout ) );
    r5_row_flags( &loadout, 3, 3, perks );
    r5_row_range( &loadout, 0, 10 );
    r5_row_bits( &loadout, 6 );
    r5_row_range( &loadout, 0, 100 );
    r5_row_range( &loadout, 0, 7 );

    /* ProfileConfig: its one digest-bearing descendant is loadout. */
    r5_digest profile = loadout;

    /* RootConfig: weapons' WeaponConfig rows (once — the closure names the
       type through weapons, primary AND backups), then profiles' branch with
       the Perks flags block and Attachment's slot row. */
    r5_digest root;
    memset( &root, 0, sizeof( root ) );
    r5_row_range( &root, 0, 10 );
    r5_row_bits( &root, 6 );
    r5_row_range( &root, 0, 100 );
    r5_row_flags( &root, 3, 3, perks );
    r5_row_range( &root, 0, 7 );

    printf( "R5: derived digests: weapon=%d loadout=%d profile=%d root=%d bytes\n",
            weapon.n, loadout.n, profile.n, root.n );

    /* THE PINS. <T>_fixed_hash is the constant the emitter baked from
       ir.TableFixedLayoutHash at internal/codegen/ctable/fixedform.go:364 —
       the hash every saved file's header and record carry. If the digest this
       leg folds in drops a range row, drops the bits row, narrows a bound,
       re-emits a named type per naming field, emits a 'Q' for an uncompression
       float, or emits the reserved 'L', the number below moves off the
       constant and the pin names it. */
    {
        uint64_t got = r5_hash_of( weapon_config_fixed_layout, weapon_config_fixed_layout_bytes, &weapon );
        printf( "R5: WeaponConfig derived hash = 0x%016llx, baked = 0x%016llx\n",
                (unsigned long long) got, (unsigned long long) weapon_config_fixed_hash );
        check( got == weapon_config_fixed_hash,
               "R5 weapon_config: digest carries every range + the bits row, once per named type" );
    }
    {
        uint64_t got = r5_hash_of( loadout_config_fixed_layout, loadout_config_fixed_layout_bytes, &loadout );
        printf( "R5: LoadoutConfig derived hash = 0x%016llx, baked = 0x%016llx\n",
                (unsigned long long) got, (unsigned long long) loadout_config_fixed_hash );
        check( got == loadout_config_fixed_hash,
               "R5 loadout_config: the flags row rides at its naming field, first by declared order" );
    }
    {
        uint64_t got = r5_hash_of( profile_config_fixed_layout, profile_config_fixed_layout_bytes, &profile );
        printf( "R5: ProfileConfig derived hash = 0x%016llx, baked = 0x%016llx\n",
                (unsigned long long) got, (unsigned long long) profile_config_fixed_hash );
        check( got == profile_config_fixed_hash,
               "R5 profile_config: the walk recurses the nested table and dedupes by bare name" );
    }
    {
        uint64_t got = r5_hash_of( root_config_fixed_layout, root_config_fixed_layout_bytes, &root );
        printf( "R5: RootConfig derived hash = 0x%016llx, baked = 0x%016llx\n",
                (unsigned long long) got, (unsigned long long) root_config_fixed_hash );
        check( got == root_config_fixed_hash,
               "R5 root_config: a type named by THREE fields emits once; no 'Q' without a float range; no reserved 'L'" );
    }

    /* THE PRODUCTION WRITER: the fixed-form save the generated surface ships.
       The hash it writes into the file's header (offset kTableFixedHashAt) and
       into the record is the constant above — the digest's only ride to disk. */
    {
        static uint8_t file[8192];
        RootConfig value;
        int64_t saved;
        root_config_reset( &value );
        saved = root_config_fixed_save( &value, 1, file, (int64_t) sizeof( file ) );
        if ( saved < root_config_fixed_measure( 1 ) )
        {
            check( 0, "R5 root_config_fixed_save wrote a whole record" );
        }
        else
        {
            printf( "R5: saved %lld bytes; header hash = 0x%016llx, record hash = 0x%016llx\n",
                    (long long) saved,
                    (unsigned long long) r5_le64( file + kTableFixedHashAt ),
                    (unsigned long long) r5_le64( file + kTableFixedHeaderBytes + 4
                                                  + root_config_fixed_layout_bytes ) );
            check( r5_le64( file + kTableFixedHashAt ) == root_config_fixed_hash,
                   "R5 writer: the file header's hash is the digest-bound constant" );
            check( r5_le64( file + kTableFixedHeaderBytes + 4 + root_config_fixed_layout_bytes )
                       == root_config_fixed_hash,
                   "R5 writer: the record's own hash is the digest-bound constant" );
        }
    }

    /* NOT ASSERTED HERE, and why (the cell's other two conjuncts):
       - tag 'Q', a float range's resolution: no fixed table this unit
         generates carries a float RANGE (tables/examples' floats are
         uncompressed or rangeless), so there is no generated C artifact whose
         hash a 'Q' row moves. The row is pinned where the digest is computed,
         at the shared hash site: TestTableFixedDefinitionsDigestResolutionMovesTheHash
         (ir/fixedform_hash_test.go:96) and TestTableFixedDefinitionsDigestFloatBoundIsItsFloat64Bits
         (ir/fixedform_hash_test.go:267) hold ir.TableFixedDefinitionsDigest —
         the very call the C emitter's hash makes — to the 'Q' row and the
         float64-bits bound encoding.
       - a flags type reached through TWO naming fields: no emitted fixed table
         in this corpus names one flags type twice, so the once-per-name rule
         is pinned here through the struct flavour (WeaponConfig, three naming
         fields) and at the shared site (TestTableFixedDefinitionsDigestFlagsOnceByName,
         ir/fixedform_hash_test.go:116).
       The exact pins above still refuse both defects for every row this
       corpus can carry: an extra 'Q' or a second 'F' block would move the
       baked hash off the derivation. */

    printf( "R5: %s\n", g_failures == 0 ? "ALL PASS" : "FAILED" );
    return g_failures == 0 ? 0 : 1;
}
