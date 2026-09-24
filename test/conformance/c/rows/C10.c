/* C10.c — THE SCHEMA MATRIX CELL c/C10 (docs/roadmap.sexp, matrix schema#876,
   audit schema#898): "enum ordinal past top → None".

   THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.4, the bounds-pass row at line 945):

     | the bounds pass | `clamped` | once per field: a ranged scalar off its
       end, a `bits(N)` past `2^N - 1`, a union tag past the arm count, AN ENUM
       ORDINAL PAST THE TOP VARIANT, and a FORGED ORDINAL REMAPPED TO `None` —
       one past the WRITER's own variant count ... on the COMPILED plan exactly
       as on the identity one |

   and §5.8 row 12 (line 1132) / §4.6, the bill's rule: the SAME forged bytes
   land the SAME `clamped == 1` on either plan; §5.4's row at line 946 adds
   that the `ordinal` op itself moves nothing on the identity read — the remap
   to None is the BOUNDS PASS's to count, and a port that counts in the op as
   well counts twice. `== 1`, never `>= 1`.

   THE DERIVATION OF THE VECTOR, from the generated code this test compiles
   against (build/tables-generated-c/k1/K1Table.h, from test/tables/K1.schema):

     fixed table Root { grade Grade; raw uint16; }   package tblk1
     enum Grade { Bronze, Silver, Gold }             GRADE_NONE 0, top 3

     The fixed form (§3.4): a 16-byte header — form byte 3 at 0, seven reserved
     zero bytes, the LAYOUT hash at 8 — then the u32 layout length, the layout
     bytes, then records to the end of the file. One record is its own 8-byte
     layout hash and then the body in declared order, every field at its
     declared storage width: grade's ordinal rides at width 1 (the top wire
     value 3 fits a byte), raw's uint16 at 2 bytes, low half first. So one
     record is ELEVEN bytes and the body is the file's tail.

     The bytes are NOT pinned by hand: the test writes the record with this
     build's own `root_fixed_save` — once at grade = Bronze (1), once at
     grade = Gold (3), raw = 7 both times — and LOCATES the ordinal byte by
     diffing the two files, which must differ in exactly that one byte. The
     law's forge is then exactly one byte: the ordinal, set one past the
     WRITER's own variant count — 4, with GRADE_COUNT 3. Writing the base
     through the build's own writer is what §5.9 #10's fixture is, and the
     diff-locator is the versioning probes' needle discipline (asserted to
     locate exactly one byte), carried in the test itself.

   THE ASSERTIONS, one printed line each, exit 0 green / exit 1 red:

     - the clean record reads back exactly, every counter at zero: the fixture
       is sound before anything is forged;
     - the forged record (ordinal 4) LANDS `GRADE_NONE`, is not refused, is
       not malformed, leaves `raw` at 7, counts `clamped == 1` EXACTLY — the
       `==` is what catches a port counting in the op as well — and moves no
       other counter;
     - THE BOUNDARY CONTROL: ordinal 3, the TOP variant, reads back Gold with
       `clamped == 0` — past top is past, at top is not.

   It reaches the generated C tables code the way the driver does
   (test/conformance/c/main.c, make/c.mk's C_CONFORMANCE_INCLUDES): one
   translation unit per unit of its own, here build/tables-generated-c/k1,
   linked with that unit's generated K1Table.c. It depends on no other rows/
   file and edits no shared file. */

#include <stdio.h>
#include <string.h>
#include <stdint.h>

#include "K1Table.h"

static int failed;

static void check( int ok, const char * what, int64_t got )
{
    if ( ok ) { printf( "ok: %s (%lld)\n", what, (long long) got ); }
    else { printf( "FAILED: %s (got %lld)\n", what, (long long) got ); failed = 1; }
}

int main( void )
{
    static uint8_t bronze[512], gold[512], top[512];
    Root values[1];
    TableReport report;
    TableFixedEntry plan[4096];
    int64_t bronze_len, gold_len, top_len, n, i;
    int64_t ordinal_at = -1;

    /* --- the base records, written by this build's own fixed-form writer --- */

    root_reset( &values[0] );
    values[0].grade = GRADE_BRONZE;
    values[0].raw = 7;
    bronze_len = root_fixed_save( values, 1, bronze, (int64_t) sizeof( bronze ) );
    check( bronze_len > 0, "root_fixed_save wrote the Bronze record", bronze_len );

    root_reset( &values[0] );
    values[0].grade = GRADE_GOLD;
    values[0].raw = 7;
    gold_len = root_fixed_save( values, 1, gold, (int64_t) sizeof( gold ) );
    check( gold_len == bronze_len, "the two records are the same length", gold_len );

    /* --- the locator: exactly one byte moves, and it is the ordinal --- */

    for ( i = 0; i < bronze_len; i++ )
    {
        if ( bronze[i] != gold[i] )
        {
            if ( ordinal_at >= 0 ) { check( 0, "the locator found the ordinal byte more than once", i ); ordinal_at = -2; break; }
            ordinal_at = i;
        }
    }
    check( ordinal_at >= 0, "the locator found the ordinal byte", ordinal_at );
    if ( ordinal_at >= 0 )
    {
        check( bronze[ordinal_at] == GRADE_BRONZE && gold[ordinal_at] == GRADE_GOLD,
               "that byte holds the ordinal (1 in Bronze, 3 in Gold)", gold[ordinal_at] );
    }

    /* --- control: the clean record reads back exactly, no counter moved --- */

    memset( &report, 0, sizeof( report ) );
    root_reset( &values[0] );
    n = root_fixed_load( values, 1, gold, gold_len, plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ), NULL, &report );
    check( n == 1, "the clean file reads one record", n );
    check( values[0].grade == GRADE_GOLD, "the clean read lands Gold", values[0].grade );
    check( values[0].raw == 7, "the clean read lands raw = 7", values[0].raw );
    check( report.clamped == 0, "the clean read counts no clamp", report.clamped );
    check( report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0 && report.duplicate == 0,
           "the clean read moves no other counter", report.unknown + report.kind_mismatch + report.widened + report.duplicate );

    /* --- THE FORGE: the ordinal one past the WRITER's own variant count --- */

    if ( ordinal_at >= 0 ) { gold[ordinal_at] = (uint8_t) ( GRADE_COUNT + 1 ); } /* 4: one past the top variant */

    memset( &report, 0, sizeof( report ) );
    root_reset( &values[0] );
    n = root_fixed_load( values, 1, gold, gold_len, plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ), NULL, &report );
    check( n == 1, "the forged file reads one record", n );
    check( report.refused == 0 && report.reason == 0 && report.layout_hash == 0,
           "a forged ordinal is not a refusal (same-hash read)", report.reason );
    check( report.malformed == 0, "a forged ordinal is not malformed", report.malformed );
    check( values[0].grade == GRADE_NONE, "the forged ordinal lands None", values[0].grade );
    check( values[0].raw == 7, "the scalar beside the enum stands at 7", values[0].raw );
    check( report.clamped == 1, "the forged ordinal counts clamped exactly once", report.clamped );
    check( report.unknown == 0 && report.kind_mismatch == 0 && report.widened == 0 && report.duplicate == 0,
           "no other counter moved on the forged read", report.unknown + report.kind_mismatch + report.widened + report.duplicate );

    /* --- THE BOUNDARY CONTROL: the TOP variant is not past it --- */

    if ( ordinal_at >= 0 ) { gold[ordinal_at] = (uint8_t) GRADE_MAX; } /* 3: at the top, not past it */

    memset( &report, 0, sizeof( report ) );
    root_reset( &values[0] );
    top_len = gold_len;
    n = root_fixed_load( values, 1, gold, top_len, plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ), NULL, &report );
    (void) top;
    check( n == 1, "the top-variant file reads one record", n );
    check( values[0].grade == GRADE_GOLD, "the top variant reads back Gold", values[0].grade );
    check( report.clamped == 0, "the top variant counts no clamp", report.clamped );

    if ( failed ) { printf( "C10: RED\n" ); return 1; }
    printf( "C10: GREEN — an enum ordinal past the top variant lands None and counts clamped exactly once\n" );
    return 0;
}
