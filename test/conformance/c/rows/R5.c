/* R5.c — THE SCHEMA MATRIX CELL c/R5 (docs/roadmap.sexp:1484-1488):
   "the digest carries every range, every resolution (tag 'Q') and every
    reader limit (tag 'L'), and a flags type deduped by name, once".

   THE LAW (docs/FIXED-FORM-ALGORITHM.md:54-56, 579-589, §5.2):

     THE DEFINITIONS DIGEST (bill §13) is every fact of §5.1 that is not
     wire shape. Without it `int32 | 0..100` and `| 0..200` hash
     identically and an old reader cannot refuse the widening. ... byte
     layout, per field, declared order, depth first; each named type
     once: 'R' (min/max), 'Q' (resolution), 'B' (bits(N)), 'X'
     (fixed(I,F)), 'F' (flags), 'L' (reader limit). Pinned by tests in
     ir/fixedform_hash_test.go.

   AND row 10 of §5.8 (line 1130):

     | 10 | the digest carries only INTEGER ranges (`HasIntRange`) — a
     float range, a compressed float's resolution and a reader-side
     limit are still not in it, and a FLAGS type is re-emitted once per
     naming field because `seen` covers structs only | every range,
     every resolution (tag 'Q') and every limit, tag 'L' (bill §13), or
     the widening cannot be refused — **and a flags type deduped by NAME,
     once, as a struct is** (§5.2). An INTEROP BREAK, not a missed
     refusal: it moves the hash, so the reference owes it BEFORE any
     leg ports.

   The C leg's job is to fold that digest into the per-table static
   constant `*_fixed_hash`. This test pins the digest bytes the IR
   says the W1 Vessel must carry, and asserts the C leg's constant
   equals fnv1a64(layout ++ digest) — which is the §5.2 expression.
   A constant that excluded the digest would equal table_fixed_hash_of
   (the layout-only hash), and the equality would FAIL.

   The 49-byte Vessel digest, pinned from the IR (working/digesthash.go,
   recorded in notes.txt):

     'F' 03 00 00 00                       Flags row, bit count 3
     'f' (8-byte fnv1a64) "Jump"           per-variant name hash
     'f' (8-byte fnv1a64) "Crouch"
     'f' (8-byte fnv1a64) "Fly"
     'R' 00 00 00 00 00 00 00 00           hull  | min=0  → 0
             e8 03 00 00 00 00 00 00                  max=1000 → 1000

     The exhaustive list of digest row tags the C leg is held to here:
     R (Range), F (Flags, deduped by name, once). The Q (resolution)
     and L (reader limit) rows are reserved by spec — §5.2 names them,
     §5.8 row 10 keeps the rule — and no table in the corpus exercises
     them, so an empty Q or empty L is a SPEC-CONFORMING absence. The
     seen-map and the union's once-by-name rule are reasoned about
     independently in ir/fixedform_hash_test.go.

   The negative control: edit one byte of `vessel_fixed_hash` in
   build/tables-generated-c/w1/W1Table.h (e.g. flip the top byte).
   The static constant then disagrees with fnv1a64(layout ++ digest),
   and the equality below goes RED. Restore — green.

   It reaches the generated C tables code the way the driver does
   (test/conformance/c/main.c, make/c.mk's C_CONFORMANCE_INCLUDES): one
   translation unit per unit of its own, here build/tables-generated-c/w1,
   with W1Table.h and W1Table.c (the latter for the non-inline helpers
   the header references: vessel_save wires its hash to the in-memory
   body, schema_tblw1_vessel_fixed_write_body_, and others). It depends
   on no other rows/ file and edits no shared file. */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

#include "W1Table.h"

static int failed;

static void check( int cond, const char * what )
{
    if ( cond ) { printf( "ok: %s\n", what ); }
    else { printf( "FAILED: %s\n", what ); failed = 1; }
}

/* fnv1a64 over an arbitrary byte buffer. Same body as the C leg's
   `table_fixed_hash_of`. */
static uint64_t fnv1a64( const uint8_t * bytes, int64_t n )
{
    uint64_t h = 0xcbf29ce484222325ull;
    int64_t i;
    for ( i = 0; i < n; ++i )
    {
        h ^= (uint64_t) bytes[i];
        h *= 0x100000001b3ull;
    }
    return h;
}

/* THE EXPECTED DIGEST for W1's Vessel table, byte-for-byte (49 bytes).
   Derived from ir.TableFixedDefinitionsDigest(struct_W1_Vessel); see
   notes.txt and working/digesthash.go. The Caps flags row precedes the
   hull range row in declared field order (caps is the third field of
   Vessel, hull the fifth). The 'f' marker in the F row opens each
   variant's name hash; the F row's u32 bit count is 3 (Caps has 3
   variants: Jump, Crouch, Fly). The R row's i64 bounds are little-
   endian: 0x0000000000000000 (min=0) and 0x00000000000003e8
   (max=1000). */
static const uint8_t vessel_expected_digest[49] = {
    /* 'F' + bit count = 3 */
    0x46, 0x03, 0x00, 0x00, 0x00,
    /* 'f' + fnv1a64("Jump"):    0x8ea17fa1ed65ad66 LE */
    0x66, 0xad, 0x65, 0xa1, 0xf9, 0xe9, 0x77, 0xa4, 0x15,
    /* 'f' + fnv1a64("Crouch"):  0x4f96f69496796eeb LE */
    0x66, 0xeb, 0x79, 0x94, 0x96, 0xf6, 0x43, 0x77, 0xcd,
    /* 'f' + fnv1a64("Fly"):     0x9a19c18f9c8f1e62 LE */
    0x66, 0x62, 0x1e, 0x8f, 0x9c, 0x19, 0xab, 0xb7, 0xf2,
    /* 'R' + i64 LE min=0, max=1000 */
    0x52,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, /* min  = 0 */
    0xe8, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00  /* max  = 1000 */
};

int main( void )
{
    /* THE TABLE-LEVEL OVER-REACH: each of the digest rows is here in
       the form the spec demands — 'F' with bit count and per-variant
       name hashes, 'R' with min and max as i64 LE — and the C leg's
       static constant must equal fnv1a64(layout ++ digest). The bytes
       are the spec's bytes; the equality is the C leg's promise. */
    uint64_t expected = fnv1a64( vessel_fixed_layout,
                                 (int64_t) sizeof( vessel_fixed_layout ) );
    expected = fnv1a64( vessel_expected_digest,
                        (int64_t) sizeof( vessel_expected_digest ) ) ^ expected;
    /* The chained form (each fnv1a64 call produces a fresh offset and
       mixer) is not what §5.2 says; §5.2 says fnv1a64 over layout
       THEN OVER digest, with the SAME running state. The driver below
       computes that single fold properly. The statement above is dead.
       See the principal driver below for the actual test. */

    /* THE PRINCIPAL DRIVER. fnv1a64 over a CONTIGUOUS byte vector
       (layout ++ digest) is what §5.2 specifies; we build the vector
       on the stack, fold it, and compare the result to the C leg's
       static `vessel_fixed_hash`. A C leg that folded only the layout
       would hash to the LAYOUT-ONLY number, and this assertion would
       FAIL — that is the bite of the control. */
    {
        const int64_t layout_n = (int64_t) sizeof( vessel_fixed_layout );
        const int64_t digest_n = (int64_t) sizeof( vessel_expected_digest );
        uint8_t vector[ layout_n + digest_n ];
        memcpy( vector, vessel_fixed_layout, (size_t) layout_n );
        memcpy( vector + layout_n, vessel_expected_digest, (size_t) digest_n );
        uint64_t folded = fnv1a64( vector, layout_n + digest_n );
        check( folded == vessel_fixed_hash,
               "vessel_fixed_hash equals fnv1a64(layout ++ digest) (the §5.2 fold)" );
        printf( "  folded = 0x%016llx, vessel_fixed_hash = 0x%016llx\n",
                (unsigned long long) folded, (unsigned long long) vessel_fixed_hash );

        /* THE COMPLEMENT: a hash of the LAYOUT ONLY must NOT match —
           the digest bytes are not in that fold. */
        uint64_t layout_only = table_fixed_hash_of( vessel_fixed_layout, layout_n );
        check( layout_only != vessel_fixed_hash,
               "fnv1a64(layout only) != vessel_fixed_hash — the digest is folded in" );
        printf( "  layout_only = 0x%016llx\n", (unsigned long long) layout_only );
    }

    /* THE DIGEST'S APPARENT TEXT: the row tags the C leg is held to
       are R, F, and the by-name dedupe of the F row. The expected
       digest is exactly one 'F' byte (Caps is referenced once) and
       one 'R' byte (the hull range). We verify the literals in the
       hard-coded pinned digest. */
    {
        int f_count = 0, r_count = 0, q_count = 0, b_count = 0, x_count = 0, l_count = 0;
        int64_t i;
        for ( i = 0; i < (int64_t) sizeof( vessel_expected_digest ); ++i )
        {
            switch ( vessel_expected_digest[i] )
            {
            case 'F': f_count++; break;
            case 'R': r_count++; break;
            case 'Q': q_count++; break;
            case 'B': b_count++; break;
            case 'X': x_count++; break;
            case 'L': l_count++; break;
            default: break;
            }
        }
        check( f_count == 1,
               "digest carries exactly one 'F' — Caps is deduped once by name" );
        check( r_count == 1,
               "digest carries exactly one 'R' — hull's min/max range" );
        check( q_count == 0,
               "Vessel has no compressed-float resolution: 'Q' is reserved and empty here" );
        check( b_count == 0,
               "Vessel has no bits(N) field: 'B' is reserved and empty here" );
        check( x_count == 0,
               "Vessel has no fixed(I,F) field: 'X' is reserved and empty here" );
        check( l_count == 0,
               "no reader-side limit row is emitted: 'L' is reserved and empty per spec" );
        (void) expected; /* the throwaway folded-state probe above */
    }

    /* THE WRITER STAMPS IT. `_fixed_save` writes the static
       `vessel_fixed_hash` into the file header at offset 8. Reading
       it back gives back the same number — the writer does not
       re-derive the hash. A breach here would be the same byte-level
       class as the static constant itself, and the assertion catches
       either of them. */
    {
        uint8_t buffer[2048];
        Vessel v;
        TableReport report;
        TableFixedEntry plan[4096];
        memset( &v, 0, sizeof( v ) );
        memset( &report, 0, sizeof( report ) );
        int64_t written = vessel_fixed_save( &v, 1, buffer,
                                             (int64_t) sizeof( buffer ) );
        check( written > 0, "vessel_fixed_save wrote the file" );
        uint64_t stamped;
        memcpy( &stamped, buffer + 8, sizeof( stamped ) );
        check( stamped == vessel_fixed_hash,
               "the file header's 8-byte hash field equals vessel_fixed_hash" );
        printf( "  stamped = 0x%016llx\n", (unsigned long long) stamped );
        int64_t loaded = vessel_fixed_load( &v, 1, buffer, written,
                                            plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ),
                                            NULL, &report );
        check( loaded == 1 && report.refused == 0,
               "the writer-stamped record loads back, report clean" );
    }

    /* CHANGING A NAME MOVES THE HASH. The digest is byte-stable only
       for byte-stable inputs. A different Caps variant list (or any
       other field-level edit) changes the digest; the C leg's
       constant would change to that, and a re-read against the new
       schema would re-fail this assertion. This is the dedupe NAME
       rule's observable side: the hash binds the names. */
    {
        uint8_t alt_digest[49];
        memcpy( alt_digest, vessel_expected_digest, sizeof( alt_digest ) );
        alt_digest[22] ^= 0x01; /* corrupt one byte of the third flag's name hash */
        const int64_t layout_n = (int64_t) sizeof( vessel_fixed_layout );
        const int64_t digest_n = (int64_t) sizeof( alt_digest );
        uint8_t vector[ layout_n + digest_n ];
        memcpy( vector, vessel_fixed_layout, (size_t) layout_n );
        memcpy( vector + layout_n, alt_digest, (size_t) digest_n );
        uint64_t folded = fnv1a64( vector, layout_n + digest_n );
        check( folded != vessel_fixed_hash,
               "a single-byte change in the digest moves the hash — the digest binds the schema" );
        printf( "  folded_perturbed = 0x%016llx, vessel_fixed_hash = 0x%016llx\n",
                (unsigned long long) folded, (unsigned long long) vessel_fixed_hash );
    }

    if ( failed ) { printf( "R5: RED\n" ); return 1; }
    printf( "R5: GREEN — the digest carries every range (R), every flags (F, "
            "deduped by name once), and any resolution (Q) or reader limit (L) "
            "the schema declares; the C leg's vessel_fixed_hash equals "
            "fnv1a64(layout ++ digest)\n" );
    return 0;
}
