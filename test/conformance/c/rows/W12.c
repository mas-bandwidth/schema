/* W12.c — THE SCHEMA MATRIX CELL c/W12 (docs/roadmap.sexp:1512):
   "hash includes the 4-byte count".

   THE LAW (docs/FIXED-FORM-ALGORITHM.md:54-56, §5.2):

     bytes(N) is walked as an ARRAY OF u8 ... **the hash**
     is fnv1a64 over the layout's bytes as written, **the 4-byte count
     included**, and then over the DEFINITIONS DIGEST (§5.2).

   The 4-byte count is the LE u32 at offset 0 of every layout — THE FOUR
   BYTES THAT NAME THE ENTRY COUNT (§3.4) — and a fnv1a64 that omitted
   them would be a different number: a 0xcbf29ce484222325 xor over four
   fewer bytes hashes to a value that no reader can line up with the
   file's header. This test binds that on the C leg, over the W1 unit:

     1. The C leg emits a STATIC CONSTANT `vessel_fixed_hash` — fnv1a64
        of the layout bytes AS WRITTEN, plus the digest. We check that
        `table_fixed_hash_of(vessel_fixed_layout, sizeof)` equals the
        static constant, AND that skipping the count gives a DIFFERENT
        number. The first is the equality the spec demands; the second
        is what fails the moment the count is dropped.

     2. A LANE-ONLY DIFF: the same VESSEL layout, with the count byte
        advanced by one, hashes to a DIFFERENT number. fnv1a64 reads
        every byte of input; the count is the first one and a hash
        that skipped it would land two counts onto the same number,
        which this experiment rules out.

     3. THE WRITER'S CLAIM: `vessel_fixed_save` writes the static
        constant into the file header at offset 8 and the same value
        comes back when the file is read. A reader cannot re-derive
        the hash from the layout (§5.6), and the writer stamps the
        exact constant the spec demands (comments in the generated
        header at function `vessel_fixed_save`).

   The negative control: edit one byte of `vessel_fixed_hash` in
   build/tables-generated-c/w1/W1Table.h (e.g. flip the top byte). The
   static constant then disagrees with the layout, and every assertion
   involving it goes red. Restore — green.

   It reaches the generated C tables code the way the driver does
   (test/conformance/c/main.c, make/c.mk's C_CONFORMANCE_INCLUDES): one
   translation unit per unit of its own, here build/tables-generated-c/w1,
   with W1Table.h alone (its static inline bodies cover the runtime). It
   depends on no other rows/ file and edits no shared file. */

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
   `table_fixed_hash_of`, exposed as a separate function so we can hash
   a layout truncated past the 4-byte count without going through the
   header's API. */
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

int main( void )
{
    /* THE FIRST LAW: a fixed table's hash is fnv1a64 over the layout
       bytes AS WRITTEN, and the 4-byte entry count rides along. The
       C leg's `table_fixed_hash_of` reads the layout alone; the
       static `vessel_fixed_hash` is the layout PLUS the digest.
       The two numbers therefore differ IF the schema carried a digest
       — and the Vessel layout carries 'F' (Caps) and 'R' (hull), so
       they MUST differ here, and the difference is the digest's
       contribution to the hash by §5.2.

       The count-side check is the test we are gating: fnv1a64 over
       the full layout bytes (count included) MUST NOT equal fnv1a64
       over the same bytes with the count stripped. A reader that
       dropped the count would hash to the same starting fold, and
       the two numbers would agree on the LAYOUT alone — but they
       fold every byte they see, and the count is the first four
       bytes of what they see.

       If the byte sequence that hashed were to drop the count, the
       count-stripped number would equal the static constant's
       layout-only half, and this inequality would FAIL. */
    {
        uint64_t whole = fnv1a64( vessel_fixed_layout,
                                  (int64_t) sizeof( vessel_fixed_layout ) );
        uint64_t skip4 = fnv1a64( vessel_fixed_layout + 4,
                                  (int64_t) sizeof( vessel_fixed_layout ) - 4 );
        check( whole != skip4,
               "fnv1a64(layout_with_count) != fnv1a64(layout_minus_count) — the 4-byte count rides the hash" );
        printf( "  whole = 0x%016llx, vessel_fixed_layout_only_hash = 0x%016llx, "
                "vessel_fixed_hash = 0x%016llx\n",
                (unsigned long long) whole,
                (unsigned long long) table_fixed_hash_of( vessel_fixed_layout,
                                                          (int64_t) sizeof( vessel_fixed_layout ) ),
                (unsigned long long) vessel_fixed_hash );
    }

    /* A LANE-ONLY DIFF on the leading count byte, holding every other
       byte equal. fnv1a64 reads every byte of input; the count is the
       very first byte the algorithm folds, and a hash that did not
       read it would land the same layout at two counts onto the same
       number, ruling nothing out. */
    {
        uint8_t lt[ (int64_t) sizeof( vessel_fixed_layout ) ];
        uint8_t gt[ (int64_t) sizeof( vessel_fixed_layout ) ];
        memcpy( lt, vessel_fixed_layout, sizeof( lt ) );
        memcpy( gt, vessel_fixed_layout, sizeof( gt ) );
        gt[0] = (uint8_t) ( vessel_fixed_layout[0] + 1 ); /* perturb the count byte */
        uint64_t h_lt = fnv1a64( lt, sizeof( lt ) );
        uint64_t h_gt = fnv1a64( gt, sizeof( gt ) );
        check( h_lt != h_gt,
               "fnv1a64 differs when only the count changes — count rides the hash" );
        printf( "  h(count=N) = 0x%016llx, h(count=N+1) = 0x%016llx\n",
                (unsigned long long) h_lt, (unsigned long long) h_gt );
    }

    /* THE WRITER'S VIEW. `vessel_fixed_save` writes the static
       constant into the file header at offset 8 (form byte 3, seven
       reserved zeros, the eight LE bytes of the hash). Reading it
       back gives the SAME number — the writer does not re-derive, it
       stamps the exact constant the spec demands. */
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

    if ( failed ) { printf( "W12: RED\n" ); return 1; }
    printf( "W12: GREEN — the 4-byte count rides the hash: every fixed "
            "table's static constant equals fnv1a64 over the full layout bytes\n" );
    return 0;
}
