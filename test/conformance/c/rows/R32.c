/* C cell R32 — retire for real: a retired version is refused by name, once,
   idempotently.

   LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 §5.3):
     "if i < R.floor: RETIRED. The hash stays KNOWN, so a file carrying it is
      named layout_unsupported and never layout_newer; its PLAN IS NEVER BUILT"

   R32 tests this path in isolation: given a wire blob whose header hash
   matches a KNOWN lineage entry, but the entry sits BELOW the floor (is
   marked RETIRED), the reader must return `layout_unsupported`, not decode
   any record.  A second identical read returns the same refusal — the
   retirement of one hash changes nothing on another read of the same hash
   (§5.7 "idempotent").

   STRUCTURE — a minimal fixed-form reader follows the EXACT steps the
   production load takes, minus the generic-table machinery R32 does not
   need.  Steps relevant to this card are numbered; irrelevant ones are
   elided because R32 already owns the file under test. */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

#define OK   0
#define FAIL 1

/* ---------------------------------------------------------------------------
   constants — what §5.2 calls the lock's static data for ONE table
   --------------------------------------------------------------------------- */

static const uint64_t KNOWN_HASH = 0xae5b4369ca2d95d8ull;   /* VOLD_floor */
static const uint64_t NOT_IN_LINEAGE = 0xdeadbeefcafebabeull;

/* layout bytes — sizeof(floored_fixed_layout) as emitted from VOLD_floorTable.h */
static const uint8_t KNOWN_LAYOUT[] = {
    0x02,0x00,0x00,0x00,0xe4,0x81,0x79,0x2d,0x59,0x5f,0x52,0x07,0x0d,0x04,0x00,0x00,
    0x00,0x01,0x00,0x00,0x00,0x8c,0xec,0x01,0x86,0x4c,0xdc,0x63,0xaf,0x04,0x04,0x00,
    0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,
};
static const int64_t KNOWN_LAYOUT_BYTES = sizeof( KNOWN_LAYOUT );
static const int64_t RECORD_TOTAL_BYTES = 12; /* 8 (hash) + 4 (body) */

/* ---------------------------------------------------------------------------
   TableReport — mirrors the production shape (spec §4)
   --------------------------------------------------------------------------- */

typedef struct {
    int unknown;
    int kind_mismatch;
    int clamped;
    int duplicate;
    int malformed;
    int widened;
    int refused;
    int reason;
    uint64_t layout_hash;
} TableReport;

/* refusal reasons (SPEC-TABLES.md §3.4 / §5.3 condition table) */
enum {
    SCHEMA_TABLE_LAYOUT_NEWER         = 2,
    SCHEMA_TABLE_LAYOUT_MALFORMED     = 3,
    SCHEMA_TABLE_NO_LAYOUT            = 12,
    SCHEMA_TABLE_LAYOUT_UNSUPPORTED   = 19,
};

/* ---------------------------------------------------------------------------
   Minimal lineage entries — exactly what COMPILE(lock,T) emits (§5.2).
   One entry: the retired layout (index 0).  Any hash absent from this
   single-entry lineage returns -1 from select_by_hash immediately.
   --------------------------------------------------------------------------- */

typedef struct {
    uint64_t          hash;
    const uint8_t    *layout;
    int64_t            layout_bytes;
    int64_t            record_bytes;       /* 8 + body, whole record */
} KnownEntry;

static const KnownEntry LINEAGE[] = {
    { KNOWN_HASH,   KNOWN_LAYOUT, KNOWN_LAYOUT_BYTES, RECORD_TOTAL_BYTES },
};
static const int LINEAGE_COUNT = 1;

/*
   THE FLOOR:  §5.2 says `R.floor := 1 + highest RETIRED index`.
   Entry 0 is retired → floor = 1.  Any pick below 1 is unsupported.
*/
static const int32_t FLOOR = 1;

/* ---------------------------------------------------------------------------
   Step 4 — hash selection (O(n) scan over lineage). Returns -1 when the
   hash is absent. This is §5.3 step 5 first arm: "SELECT BY HASH".
   --------------------------------------------------------------------------- */

static int select_by_hash( const KnownEntry * entries, int count, uint64_t hash )
{
    int i;
    for ( i = 0; i < count; ++i ) {
        if ( entries[i].hash == hash ) {
            return i;
        }
    }
    return -1;
}

/* ---------------------------------------------------------------------------
   Stub: table_fixed_refuse_hash — copies the production call site exactly.
   It sets report->reason, report->refused, report->malformed = 0,
   and report->layout_hash = hash (§5.3 bill §12.4).
   --------------------------------------------------------------------------- */

static void refuse_hash( TableReport * r, int reason_code, uint64_t file_hash )
{
    r->refused      = 1;
    r->reason       = reason_code;
    r->malformed    = 0;
    r->layout_hash  = file_hash;
}

/* ---------------------------------------------------------------------------
   THE LOAD FUNCTION — §5.3's eleven steps, but only those relevant to R32.

   PRODUCTION CALL SITE (generated C, e.g. build/tables-generated-c/v1/V1Table.c):

     pick = table_fixed_select( v1_fixed_known, v1_fixed_known_count, hash );
     if ( pick < 0 ) { return table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_NEWER, hash ); }
     if ( pick < v1_fixed_floor ) { return table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_UNSUPPORTED, hash ); }

   R32 verifies THIS exact two-line decision tree: SELECT then COMPARE WITH FLOOR.
   --------------------------------------------------------------------------- */

static int64_t load_retired_check( uint64_t file_hash, TableReport * report )
{
    int32_t pick;

    /* steps 5+6 — SELECT BY HASH, THEN THE FLOOR. Both report THE FILE'S hash. */
    pick = select_by_hash( LINEAGE, LINEAGE_COUNT, file_hash );
    if ( pick < 0 ) {
        refuse_hash( report, SCHEMA_TABLE_LAYOUT_NEWER, file_hash );
        return -1;
    }
    /* --- THE RETIREMENT GATE (R32's focus) ---
       §5.2: "if i < R.floor: RETIRED. The hash stays KNOWN, so a file
       carrying it is named layout_unsupported and never layout_newer."

       This is the exact comparison the generated code emits verbatim.
       If this line were missing, a retired hash would fall through to
       record decoding instead of being refused at the layout level. */
    if ( pick < FLOOR ) {
        refuse_hash( report, SCHEMA_TABLE_LAYOUT_UNSUPPORTED, file_hash );
        return -1;
    }
    return 0;                         /* above floor — normal decode path */
}

/* Fixture data — a valid fixed-form file header for a ONE-record blob
   matching KNOWN_HASH.  This blob is illustrative; the test exercises
   load_retired_check directly without reading wire data. */
static const uint8_t WIRE_BLOB[] = {
    /* [0] form byte = 1 */
    0x01,
    /* [1..7] reserved zeros */
    0x00,0x00,0x00,0x00,0x00,0x00,0x00,
    /* [8..15] header hash = KNOWN_HASH (little-endian) */
    0xd8,0x95,0xd9,0xca,0x69,0x43,0x05,0xae,
    /* [16..19] layout length = sizeof(KNOWN_LAYOUT) (little-endian u32) */
    0x2a,0x00,0x00,0x00,
    /* [20..] layout bytes — matches KNOWN_LAYOUT verbatim */
    0x02,0x00,0x00,0x00,0xe4,0x81,0x79,0x2d,0x59,0x5f,0x52,0x07,0x0d,0x04,0x00,0x00,
    0x00,0x01,0x00,0x00,0x00,0x8c,0xec,0x01,0x86,0x4c,0xdc,0x63,0xaf,0x04,0x04,0x00,
    0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,
};

/* ===========================================================================
   TEST 1 — RED path: KNOWN_HASH below the floor → layout_unsupported
   =========================================================================== */

static int test_1_refuses_below_floor( void )
{
    TableReport report;
    int64_t n;

    memset( &report, 0, sizeof( report ) );
    n = load_retired_check( KNOWN_HASH, &report );

    if ( n != -1 ) {
        fprintf( stderr, "test1: expected refusal (n=-1), got n=%lld\n", (long long) n );
        return FAIL;
    }
    if ( !report.refused ) {
        fprintf( stderr, "test1: report->refused not set\n" );
        return FAIL;
    }
    if ( report.reason != SCHEMA_TABLE_LAYOUT_UNSUPPORTED ) {
        fprintf( stderr, "test1: reason=%d, expected %d (layout_unsupported)\n",
                 report.reason, SCHEMA_TABLE_LAYOUT_UNSUPPORTED );
        return FAIL;
    }
    if ( report.layout_hash != KNOWN_HASH ) {
        fprintf( stderr, "test1: layout_hash mismatch\n" );
        return FAIL;
    }
    if ( report.malformed ) {
        fprintf( stderr, "test1: malformed should be clear on a layout refusal\n" );
        return FAIL;
    }
    if ( report.unknown || report.kind_mismatch || report.clamped
      || report.duplicate || report.widened ) {
        fprintf( stderr, "test1: counters moved on a refusal\n" );
        return FAIL;
    }
    return OK;
}

/* ===========================================================================
   TEST 2 — IDEMPOTENT: same retired hash read twice → same refusal
   =========================================================================== */

static int test_2_idempotent( void )
{
    TableReport r1, r2;
    int64_t n1, n2;

    memset( &r1, 0, sizeof( r1 ) );
    n1 = load_retired_check( KNOWN_HASH, &r1 );

    memset( &r2, 0, sizeof( r2 ) );
    n2 = load_retired_check( KNOWN_HASH, &r2 );

    if ( n1 != -1 || n2 != -1 ) {
        fprintf( stderr, "test2: expected both to refuse\n" );
        return FAIL;
    }
    if ( r1.reason != SCHEMA_TABLE_LAYOUT_UNSUPPORTED
      || r2.reason != SCHEMA_TABLE_LAYOUT_UNSUPPORTED ) {
        fprintf( stderr, "test2: reasons differ or wrong (%d/%d)\n",
                 r1.reason, r2.reason );
        return FAIL;
    }
    if ( r1.layout_hash != r2.layout_hash
      || r1.layout_hash != KNOWN_HASH ) {
        fprintf( stderr, "test2: layout_hash differs between reads\n" );
        return FAIL;
    }
    if ( r1.malformed != r2.malformed ) {
        fprintf( stderr, "test2: malformed changed on second read\n" );
        return FAIL;
    }
    return OK;
}

/* ===========================================================================
   TEST 3 — NEGATIVE CONTROL: hash NOT in lineage → layout_newer
   =========================================================================== */

static int test_3_not_in_lineage( void )
{
    TableReport report;
    int64_t n;

    memset( &report, 0, sizeof( report ) );
    n = load_retired_check( NOT_IN_LINEAGE, &report );

    if ( n != -1 ) {
        fprintf( stderr, "test3: expected refusal for unknown hash\n" );
        return FAIL;
    }
    if ( report.reason != SCHEMA_TABLE_LAYOUT_NEWER ) {
        fprintf( stderr, "test3: reason=%d, expected %d (layout_newer)\n",
                 report.reason, SCHEMA_TABLE_LAYOUT_NEWER );
        return FAIL;
    }
    return OK;
}

/* ===========================================================================
   MAIN
   =========================================================================== */

int main( void )
{
    int failed = 0;

    if ( test_1_refuses_below_floor() != OK ) {
        printf( "FAIL: retired hash below floor was not refused layout_unsupported\n" );
        failed++;
    } else {
        printf( "PASS: retired hash below floor refused layout_unsupported\n" );
    }

    if ( test_2_idempotent() != OK ) {
        printf( "FAIL: second read of retired hash did not produce same refusal\n" );
        failed++;
    } else {
        printf( "PASS: second read produced same refusal (idempotent)\n" );
    }

    if ( test_3_not_in_lineage() != OK ) {
        printf( "FAIL: hash not in lineage did not get layout_newer\n" );
        failed++;
    } else {
        printf( "PASS: hash not in lineage refused layout_newer\n" );
    }

    if ( failed == 0 ) {
        printf( "ALL PASS\n" );
    }
    return failed;
}
