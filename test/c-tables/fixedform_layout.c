/* THE LAYOUT VALIDATION, C leg (docs/SPEC-TABLES.md §3.4,
   docs/FIXED-FORM-ALGORITHM.md §1.1). The C++ reference is the
   layout_validation() function of test/tables/fixedform_main.cpp and this is
   its twin: the same twelve hostile layouts, refused under the same names.

   THE LAYOUT ARRIVES FROM AN UNTRUSTED PEER. It is the one structure a reader
   must parse before it knows anything at all, so every rule it is held to
   refuses under ITS OWN NAME, before a single record byte is touched. A
   VALIDATION NOBODY WATCHED FAIL IS A VALIDATION NOBODY HAS, so there is one
   case per named rule, each taking a layout this reader ACCEPTS and breaking
   EXACTLY ONE THING in it.

   THIS IS THE FX1 READER ON A FILE BUILT AROUND FX2'S LAYOUT, which is what
   the reference does too (its `good` is written by tblfx2 and read by tblfx1).
   C has no namespace, so the two generations cannot share a translation unit
   (fixedform.h says why): the FX2 bytes arrive as a parameter, exactly as they
   do for fixed_fx1_read_fx2.

   The file is the HEADER (docs/SPEC-TABLES.md §3: the form byte, seven
   reserved zero bytes, the layout hash at 8, the body at 16), then `u32 layout
   length, layout, records` — so the layout starts at byte 20, the entry count
   is the four bytes there, and entry k is the seventeen bytes at 20 + 4 + 17k:
   id (u64), kind (u8), size (u32), children (u32), every number
   little-endian.

   THE READER THESE CASES ASSERT IS RETIRED, AND THE FUNCTION STAYS ANYWAY
   (docs/FIXED-FORM-ALGORITHM.md §5.6, §5.9 #23). The words here used to say the
   header's hash is checked LAST, so a byte broken inside the layout refused by
   the layout's own rule rather than as a lying header — that was true of the
   run-time walk of a stranger's layout, and §5.6 retired that walk. Under §5.3
   the HASH IS STEP 5 and it is looked up FIRST: a hash no lineage entry holds
   comes back `layout_newer` before one layout byte is touched, and the seven
   §1.1 malformations under a KNOWN hash all come back as the one name
   `layout_malformed`. So every case below would now answer by the HEADER and
   never by its own rule, which is why fixedform_main.c SKIPS
   fixed_fx1_layout_validation BY NAME and says where the coverage is owed
   again: the LOCK's validation of what it records (internal/lockfile). The
   function is not deleted — a deleted test is a coverage claim nobody can
   audit — and it stays compiling, which is the whole of §5.9 #23's rule. */

#include <string.h>

#include "FX1Table.h"
#include "fixedform.h"

/* the plan storage the caller owns; no layout below ever gets as far as
   compiling, which is the point — a refusal costs no plan */
#define PlanCapacity 1024
static TableFixedEntry g_plan[PlanCapacity];

/* THIS LEG ALLOCATES NOTHING, HERE LEAST OF ALL. fixedform_main.c holds its
   own bytes in a static g_buffer and so does every translation unit beside
   this one, so the hostile files are static arrays too. The deep case is the
   reason there are two: 4097 entries is about 70KB, past main's 65536. */
#define BrokenBytes 4096
static uint8_t g_broken[BrokenBytes];

#define DeepEntries 4097u
#define DeepLayoutBytes ( 4u + DeepEntries * 17u )
#define DeepFileBytes ( (size_t) kTableFixedHeaderBytes + 4u + DeepLayoutBytes )
static uint8_t g_deep[DeepFileBytes];

#define LayoutAt ( (size_t) kTableFixedHeaderBytes + 4u )
#define Entry0At ( LayoutAt + 4u )

static uint8_t * entry_at( uint8_t * f, size_t k ) { return f + Entry0At + k * 17u; }

/* an entry written straight into a hand-built file, for the rules no single
   break of a real layout reaches */
static void put_entry( uint8_t * f, size_t k, uint64_t id, uint8_t kind, uint32_t size, uint32_t children )
{
    uint8_t * e = entry_at( f, k );
    table_fixed_put64( e, id );
    e[8] = kind;
    table_fixed_put32( e + 9, size );
    table_fixed_put32( e + 13, children );
}

/* THE FILE AROUND A HAND-BUILT LAYOUT: the form byte, the layout's length, the
   layout's own hash in the header, and NO RECORDS — every rule below refuses
   before a record is reached, so there is nothing for one to be. The C twin of
   the reference's file_of, except that the layout is built in place: a second
   buffer for the deep case would be another 70KB for nothing. */
static int64_t hand_built( uint8_t * f, uint32_t entries )
{
    const uint32_t layout_bytes = 4u + entries * 17u;
    f[0] = 3; /* the fixed form's byte */
    table_fixed_put32( f + LayoutAt, entries );
    table_fixed_put32( f + kTableFixedHeaderBytes, layout_bytes );
    table_fixed_put64( f + kTableFixedHashAt, table_fixed_hash_of( f + LayoutAt, (int64_t) layout_bytes ) );
    return (int64_t) kTableFixedHeaderBytes + 4 + (int64_t) layout_bytes;
}

static void refuses( const uint8_t * broken, int64_t bytes, int want, const char * what )
{
    FxRoot v;
    TableReport r;
    int64_t n;
    fx_root_reset( &v );
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &v, 1, broken, bytes, g_plan, PlanCapacity, NULL, &r );
    fixed_check( n < 0 && r.refused && r.reason == want, what );
    /* NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read a
       record would be the damage the refusal exists to prevent. */
    fixed_check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed,
                 "a layout refusal sets nothing and counts nothing" );
}

void fixed_fx1_layout_validation( const uint8_t * data, int64_t bytes )
{
    fixed_check( bytes > 0 && bytes <= (int64_t) BrokenBytes,
                 "C layout validation: the unbroken file fits this unit's buffer" );
    if ( bytes <= 0 || bytes > (int64_t) BrokenBytes ) { return; }

    /* THE NEGATIVE CONTROL, FIRST: the UNBROKEN file READS, so every refusal
       below is the ONE break and not the file. */
    {
        FxRoot v;
        TableReport r;
        fx_root_reset( &v );
        memset( &r, 0, sizeof( r ) );
        fixed_check( fx_root_fixed_load( &v, 1, data, bytes, g_plan, PlanCapacity, NULL, &r ) == 1 && !r.refused,
                     "C layout validation: the unbroken file reads, so the breaks below are the breaks" );
    }

    /* 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY */
    {
        uint32_t count;
        memcpy( g_broken, data, (size_t) bytes );
        count = table_fixed_get32( g_broken + LayoutAt );
        table_fixed_put32( g_broken + LayoutAt, count + 1u );
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_COUNT_MISMATCH,
                 "RULE: the entry count fits the layout length exactly" );
    }
    {
        /* a count of ZERO is not a layout either: there is no root to walk */
        memcpy( g_broken, data, (size_t) bytes );
        table_fixed_put32( g_broken + LayoutAt, 0u );
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_COUNT_MISMATCH,
                 "RULE: an entry count of zero is not a layout" );
    }

    /* 2. EVERY KIND IS IN THE CLOSED SET. A fixed form's kind set is CLOSED,
          so a kind outside it means a NEWER FORM BYTE — a different form — and
          not a newer layout of this one. It is refused, never stepped over. */
    {
        memcpy( g_broken, data, (size_t) bytes );
        entry_at( g_broken, 1 )[8] = 200; /* a kind no form byte this build carries defines */
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_KIND_UNKNOWN,
                 "RULE: a kind outside the closed set is REFUSED, not skipped" );
    }

    /* 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table */
    {
        memcpy( g_broken, data, (size_t) bytes );
        entry_at( g_broken, 0 )[8] = 14; /* an array as the root of a record */
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_KIND_INVALID,
                 "RULE: the root entry is a TABLE" );
    }

    /* 4. A CONSTANT SIZE MATCHES ITS KIND. ENTRY 1 IS THE FOUR-BYTE LEAF this
          case needs, and on this layout it is not a guess: FX2's FxRoot is the
          root table at entry 0 and `keep`, a uint32, is its first field — kind
          8, size 4, no children. */
    {
        memcpy( g_broken, data, (size_t) bytes );
        fixed_check( entry_at( g_broken, 1 )[8] == 8 && table_fixed_get32( entry_at( g_broken, 1 ) + 9 ) == 4u,
                     "C layout validation: entry 1 really is a four-byte uint32 leaf" );
        table_fixed_put32( entry_at( g_broken, 1 ) + 9, 5u ); /* a uint32 leaf in five bytes */
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH,
                 "RULE: a constant size its kind does not admit" );
    }
    {
        /* and a TABLE's size is the SUM of its children's, not a number of its own */
        uint32_t body;
        memcpy( g_broken, data, (size_t) bytes );
        body = table_fixed_get32( entry_at( g_broken, 0 ) + 9 );
        table_fixed_put32( entry_at( g_broken, 0 ) + 9, body + 4u );
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_SIZE_MISMATCH,
                 "RULE: a table's size is the sum of its fields'" );
    }

    /* 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES */
    {
        uint32_t kids;
        memcpy( g_broken, data, (size_t) bytes );
        kids = table_fixed_get32( entry_at( g_broken, 0 ) + 13 );
        table_fixed_put32( entry_at( g_broken, 0 ) + 13, kids + 1u );
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_TREE_UNCLOSED,
                 "RULE: the tree runs out of layout" );
    }
    {
        /* THE OTHER DIRECTION: a tree that closes EARLY leaves entries no walk
           reaches. It takes a hand-built layout to reach, and that is itself
           worth stating: dropping a child of a TABLE is caught one rule
           sooner, by the size that no longer sums, so the only subtree whose
           loss the size rule cannot see is one that contributes NO size — an
           enum's variants, at kind 32 and size 0. */
        int64_t total;
        memset( g_broken, 0, sizeof( g_broken ) );
        put_entry( g_broken, 0, 1u, 13u, 4u, 1u ); /* a table of one field */
        put_entry( g_broken, 1, 2u, 30u, 4u, 0u ); /* an enum, its TWO variants unreached */
        put_entry( g_broken, 2, 3u, 32u, 0u, 0u );
        put_entry( g_broken, 3, 4u, 32u, 0u, 0u );
        total = hand_built( g_broken, 4u );
        refuses( g_broken, total, SCHEMA_TABLE_LAYOUT_TREE_UNCLOSED,
                 "RULE: the layout outlasts the tree" );
    }

    /* 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW */
    {
        memcpy( g_broken, data, (size_t) bytes );
        table_fixed_put32( entry_at( g_broken, 0 ) + 9, 65537u );
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE,
                 "RULE: a record size past 65536" );
    }
    {
        /* A SIZE THAT WOULD WRAP. The children's sizes are summed in 64 bits
           precisely so a u32 that overflows is CAUGHT rather than wrapped into
           a small number that then agrees with a parent. */
        memcpy( g_broken, data, (size_t) bytes );
        table_fixed_put32( entry_at( g_broken, 1 ) + 9, 0xFFFFFFFFu );
        refuses( g_broken, bytes, SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE,
                 "RULE: a size that would overflow the sum" );
    }

    /* 7. NOTHING NESTED PAST THE READER'S WALK BOUND. A BOUND ON THE WALK AND
          NOT ON THE WIRE: the validation recurses, so a layout of a thousand
          entries each claiming one child would spend a reader's stack before
          any other rule could fire. Nothing in §3.4 fixes the number. */
    {
        const uint32_t depth = DeepEntries - 1u; /* far past any reader's own bound */
        uint32_t i;
        int64_t total;
        memset( g_deep, 0, sizeof( g_deep ) );
        for ( i = 0; i < depth; ++i )
        {
            /* a table, then optional wrappers all the way down */
            put_entry( g_deep, i, 1u, ( i == 0u ) ? 13u : 35u, depth - i, 1u );
        }
        put_entry( g_deep, depth, 2u, 1u, 1u, 0u ); /* a bool at the bottom */
        total = hand_built( g_deep, depth + 1u );
        refuses( g_deep, total, SCHEMA_TABLE_LAYOUT_TOO_DEEP,
                 "RULE: a nesting depth past the walk's own bound" );
    }

    /* AND THE RESIDUE: bytes that are not a layout at all, which is the one
       case the seven named rules never reach. A layout needs four bytes to
       carry an entry count and these are two. */
    {
        const uint32_t layout_bytes = 2u;
        memset( g_broken, 0, LayoutAt + layout_bytes );
        g_broken[0] = 3;
        table_fixed_put32( g_broken + kTableFixedHeaderBytes, layout_bytes );
        table_fixed_put64( g_broken + kTableFixedHashAt,
                           table_fixed_hash_of( g_broken + LayoutAt, (int64_t) layout_bytes ) );
        refuses( g_broken, (int64_t) LayoutAt + layout_bytes, SCHEMA_TABLE_LAYOUT_MALFORMED,
                 "RULE: fewer bytes than a header is layout_malformed" );
    }
}
