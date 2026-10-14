/* F4 -- layout_malformed, truncated, the C leg of the schema matrix cell
   (docs/roadmap.sexp row group "file-envelope"; docs/FIXED-FORM-ALGORITHM.md
   §2 step 3, the load's third line):

       "`L := LE(4, b+16)`; if `20 + L > bytes`, `REFUSE layout_malformed`"

   A fixed-form file is the sixteen-byte header (form byte 3, seven reserved
   zero bytes, the layout hash at 8), then the u32 LAYOUT LENGTH L at 16,
   then L layout bytes, then the records -- so the layout ends at 20 + L. A
   file whose bytes stop inside the layout it announces is TRUNCATED, and the
   law's answer is the NAME layout_malformed (report->refused with
   report->reason = SCHEMA_TABLE_LAYOUT_MALFORMED) and never the `malformed`
   FLAG: the flag is F7, "under 20 bytes", the residue a name cannot be put
   on; this row is the NAME, and the two must not swap.

   THE CHECK IS STEP 3 AND THE HASH SELECT IS STEPS 5 AND 6, so the header's
   hash is irrelevant to it: a truncated layout is refused layout_malformed
   even for a hash in no lineage, where the hash select alone would have
   answered layout_newer. And the comparison is `>` and not `>=`: a file that
   ends EXACTLY at the layout's end (20 + L == bytes) is not truncated -- it
   holds zero records and reads.

   VECTOR DERIVATION. The positive file is what the generated writer itself
   lays down: `cell_fixed_save` writes the form byte, the seven reserved
   zeros, this build's own layout hash at 8, the TRUE L at 16
   (cell_fixed_layout_bytes), the layout, then one 24-byte record (8 hash +
   16 body). Every case below is that one file, or a minimal header built by
   hand, with EXACTLY ONE thing moved:

     - truncated: the caller's extent is cut one byte inside the layout,
       bytes = 20 + L - 1, so 20 + L > bytes and nothing else is touched;
     - minimal: twenty zeroed bytes, the form byte at 0, an unknown hash at
       8 (0xDEADBEEFCAFEF00D, in no lineage), L = 1 at 16 -- the smallest
       vector the law admits, claiming one more layout byte than the file
       holds, under a hash that would name layout_newer if the select ran
       first;
     - boundary: the extent stops exactly at the layout's end,
       bytes = 20 + L, the record dropped whole -- NOT truncated, n = 0;
     - under-20: bytes = 19, one byte short of the header's own twenty: the
       malformed FLAG, never the name (the F7 distinction, pinned from this
       side so the two rows cannot drift together).

   REFUSE IS TOTAL (§5.8) AND SO IS MALFORMED: on either refusal no counter
   may move and not one destination byte is written -- the destination is
   poisoned 0x5A first and checked byte for byte after.

   Build: cc -std=c11 -Wall <the -I and link flags make/c.mk gives
          build/conformance-c> test/conformance/c/rows/F4.c
          -o build/rows-c-F4 && ./build/rows-c-F4

   SELF-CONTAINED: reaches the generated fixed form through the SAME include
   path the conformance driver uses (build/tables-generated-c/v1), includes
   only V1Table.h, and depends on no other rows/ file and no shared fixture. */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

#include "V1Table.h"

/* totality, shared by both refusals: nothing counted, nothing written */
static int check_nothing_moved( const Cell * back, const TableReport * r )
{
    int64_t i;
    if ( r->clamped || r->unknown || r->kind_mismatch || r->widened || r->duplicate || r->retained || r->retain_lost )
    {
        printf( "F4 layout_malformed, truncated: a counter moved: clamped=%d unknown=%d kind_mismatch=%d widened=%d duplicate=%d retained=%d retain_lost=%d\n",
                r->clamped, r->unknown, r->kind_mismatch, r->widened, r->duplicate, r->retained, r->retain_lost );
        return 0;
    }
    for ( i = 0; i < (int64_t) sizeof( *back ); ++i )
    {
        if ( ( (const uint8_t *) back )[i] != 0x5A )
        {
            printf( "F4 layout_malformed, truncated: byte %lld is 0x%02x -- the refusal wrote a destination byte\n",
                    (long long) i, ( (const uint8_t *) back )[i] );
            return 0;
        }
    }
    return 1;
}

int main( void )
{
    Cell source;
    Cell back;
    TableReport r;
    uint8_t wire[256];
    uint8_t tiny[20];
    int64_t bytes, n;

    cell_reset( &source );
    source.power = 7;
    memcpy( source.label, "hello", 6 );
    source.label_length = 5;

    bytes = cell_fixed_measure( 1 );
    if ( bytes < 0 || bytes > (int64_t) sizeof( wire ) )
    {
        printf( "F4 layout_malformed, truncated: measure out of range: %lld\n", (long long) bytes );
        return 1;
    }
    if ( cell_fixed_save( &source, 1, wire, bytes ) != bytes )
    {
        printf( "F4 layout_malformed, truncated: the clean save failed\n" );
        return 1;
    }

    /* THE NEGATIVE CONTROL, FIRST: the uncut file reads, and reads back the
       value it saved, so every refusal below is the ONE break and not the
       file. */
    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = cell_fixed_load( &back, 1, wire, bytes, NULL, 0, NULL, &r );
    if ( n != 1 || r.refused || r.malformed )
    {
        printf( "F4 layout_malformed, truncated: the uncut file did not read clean: n=%lld refused=%d malformed=%d\n",
                (long long) n, r.refused, r.malformed );
        return 1;
    }
    if ( back.power != 7 || back.label_length != 5 || memcmp( back.label, "hello", 6 ) != 0 )
    {
        printf( "F4 layout_malformed, truncated: the uncut read lost the value: power=%d label_length=%d\n",
                (int) back.power, (int) back.label_length );
        return 1;
    }

    /* THE TRUNCATED LAYOUT: the file ends one byte inside the layout, so
       20 + L > bytes, the record gone with the cut. The NAME, not the flag. */
    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = cell_fixed_load( &back, 1, wire, kTableFixedHeaderBytes + 4 + cell_fixed_layout_bytes - 1, NULL, 0, NULL, &r );
    if ( n != -1 )
    {
        printf( "F4 layout_malformed, truncated: a layout cut to 20+L-1 bytes did not refuse: n=%lld\n", (long long) n );
        return 1;
    }
    if ( !r.refused || r.reason != SCHEMA_TABLE_LAYOUT_MALFORMED )
    {
        printf( "F4 layout_malformed, truncated: refused=%d reason=%d, not layout_malformed\n", r.refused, r.reason );
        return 1;
    }
    if ( r.malformed )
    {
        printf( "F4 layout_malformed, truncated: malformed set -- a refusal by name is not the residue\n" );
        return 1;
    }
    if ( !check_nothing_moved( &back, &r ) ) { return 1; }

    /* THE MINIMAL VECTOR FROM THE LAW, AND THE ORDER: twenty bytes that are
       all header, claiming L = 1, under a hash in no lineage. 20 + 1 > 20,
       and the length is read (step 3) before the hash select (steps 5 and
       6), so the answer is layout_malformed and not layout_newer. */
    memset( tiny, 0, sizeof( tiny ) );
    tiny[0] = kTableFixedForm;
    table_fixed_put64( tiny + kTableFixedHashAt, 0xDEADBEEFCAFEF00Dull );
    table_fixed_put32( tiny + kTableFixedHeaderBytes, 1u );
    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = cell_fixed_load( &back, 1, tiny, (int64_t) sizeof( tiny ), NULL, 0, NULL, &r );
    if ( n != -1 )
    {
        printf( "F4 layout_malformed, truncated: a 20-byte header claiming L=1 did not refuse: n=%lld\n", (long long) n );
        return 1;
    }
    if ( !r.refused || r.reason != SCHEMA_TABLE_LAYOUT_MALFORMED )
    {
        printf( "F4 layout_malformed, truncated: refused=%d reason=%d, not layout_malformed before the hash select\n",
                r.refused, r.reason );
        return 1;
    }
    if ( r.malformed )
    {
        printf( "F4 layout_malformed, truncated: malformed set -- a refusal by name is not the residue\n" );
        return 1;
    }
    if ( !check_nothing_moved( &back, &r ) ) { return 1; }

    /* THE BOUNDARY: 20 + L == bytes is NOT truncated -- the comparison is
       `>`, so a file that ends exactly at the layout's end reads zero
       records, no refusal, no flag. */
    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = cell_fixed_load( &back, 1, wire, kTableFixedHeaderBytes + 4 + cell_fixed_layout_bytes, NULL, 0, NULL, &r );
    if ( n != 0 || r.refused || r.malformed )
    {
        printf( "F4 layout_malformed, truncated: exactly 20+L bytes is not truncated: n=%lld refused=%d malformed=%d\n",
                (long long) n, r.refused, r.malformed );
        return 1;
    }

    /* THE DISTINCTION FROM F7: a file one byte UNDER the twenty-byte header
       is the malformed FLAG, never the layout_malformed NAME -- a file that
       short cannot even carry the length field that would lie. */
    memset( &back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = cell_fixed_load( &back, 1, wire, kTableFixedHeaderBytes + 4 - 1, NULL, 0, NULL, &r );
    if ( n != -1 )
    {
        printf( "F4 layout_malformed, truncated: a 19-byte file did not refuse: n=%lld\n", (long long) n );
        return 1;
    }
    if ( !r.malformed || r.refused )
    {
        printf( "F4 layout_malformed, truncated: 19 bytes is the malformed flag, not the name: malformed=%d refused=%d\n",
                r.malformed, r.refused );
        return 1;
    }
    if ( !check_nothing_moved( &back, &r ) ) { return 1; }

    printf( "F4 layout_malformed, truncated: a file cut inside its layout refuses by the name; 20+L exactly reads; under 20 is the flag\n" );
    return 0;
}
