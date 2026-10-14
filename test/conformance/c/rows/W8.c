/* c/W8 — bytes(N) takes the array row (audit schema#898, matrix schema#876,
   roadmap c/W8; the C leg, fixed form, form byte 3).

   THE LAW (docs/FIXED-FORM-ALGORITHM.md):

     :262, §4.1, fix 10 — "A `bytes(N)` takes the ARRAY's row, not a text
       field's — the count to `aux` and the elements to `dst`", where a text
       field's row is the other way round. Backwards, a plan compiled from a
       stranger's layout gets a count destination that is the BUFFER'S FIRST
       FOUR BYTES and an element destination that is the LENGTH FIELD, and no
       record this build wrote can see it.

     :201, §3.1 — the slack rule is one rule: write the live extent onto the
       zeroed template and stop. "Arrays write `count` elements and text and
       bytes `length` units, never all Max" — an unused slot's STORAGE is not
       on the wire.

   THE PRODUCTION PATH this test drives, every stage generated
   (build/tables-generated-c/w1/W1Table.h, from test/tables/W1.schema; the
   emitter is internal/codegen/ctable/fixedform.go, fixedDstRow):

     vessel_fixed_dst            the DESTINATION rows the plan compiler reads —
                                 the row this law is about
     table_fixed_compile         the plan compiler, case 14 (an array): the
                                 count to the row's aux, the elements to its dst
     table_fixed_run             the one read loop over the compiled plan
     vessel_fixed_save           the write: the template, then the stores —
                                 schema_tblw1_vessel_fixed_write_body_ puts the
                                 length word at b+36 and `length` units at b+40
     vessel_fixed_load           the identity read this build's own hash selects

   THE VECTOR is built HERE from the law, because testdata/conformance/tables
   carries no fixed-form vector for this cell (its fixtures are the block,
   cook and json surfaces): one Vessel record whose bytes(4) field `tag`
   carries TWO live bytes, 0xDE 0xAD, with the two storage bytes past the
   live extent set 0xBE 0xEF on purpose — a writer that wrote all Max would
   leak them, and the law says the wire slack stays the template's zeros.
   The offsets come from the declaration: `name string(32)` takes 4 + 32 =
   36 bytes of body, so tag's LENGTH word rides at body+36 and its BUFFER at
   body+40, Max 4 — the same offsets the generated write-body stores at and
   the dst row carries (dst = offsetof(Vessel, tag) = 40, aux =
   offsetof(Vessel, tag_length) = 44).

   THE CONTROL is the old row, the twin of the C++ reference's bytes_row_case
   (test/tables/fixedform_main.cpp:794): the two columns swapped back to the
   TEXT convention on a copy of this build's own rows, the same plan compiled
   from the same layout, and the same record comes out WRONG — the count
   written into the buffer and the bytes written over the length. The row is
   found BY ITS SHAPE and never by its index — stride one and a live count is
   a `bytes(N)` and nothing else — so the control does not quietly stop
   pointing at it the day a field moves. (The same control in
   test/c-tables/fixedform_fx2.c is SKIPped there, retired with the runtime
   compile path by §5.6; this file is where the C leg's assertion stands.)

   SELF-CONTAINED: the unit's implementation rides in this translation unit
   (`#include "W1Table.c"`) because the run line names this file alone and at
   -O0 the header's unused statics would otherwise drag the unit's externs.
   No other rows/ file, no shared fixture, no file outside this one edited.

   Run (flags per make/c.mk's build/conformance-c, C_CONFORMANCE_INCLUDES):
     cc -std=c11 -Wall <the -I and link flags> test/conformance/c/rows/W8.c \
        -o build/rows-c-W8 && ./build/rows-c-W8
   Exit 0 green, 1 red; one printed line per assertion. */

#include <stddef.h>
#include <stdio.h>
#include <string.h>

#include "W1Table.h"
#include "W1Table.c"

static int failures;

static void check( int ok, const char * what )
{
    printf( "%s: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures = 1; }
}

int main( void )
{
    static uint8_t file[1024];
    static TableFixedEntry plan[512];
    size_t rows, i, found;
    int64_t measure, saved, body;
    Vessel v, back;
    TableReport r;

    /* ---- THE WRITE: one record whose `tag` carries TWO live bytes --------
       The writer writes the length word at body+36, the two live bytes at
       body+40..42, and the two bytes past them (body+42..44) stay the
       template's zeros — `length` units, never all Max. The storage past the
       live extent is 0xBE 0xEF on purpose: the value nobody wrote. */
    vessel_reset( &v );
    v.tag[0] = 0xDE; v.tag[1] = 0xAD; v.tag[2] = 0xBE; v.tag[3] = 0xEF;
    v.tag_length = 2;

    memset( file, 0xAB, sizeof( file ) );
    measure = vessel_fixed_measure( 1 );
    saved = vessel_fixed_save( &v, 1, file, (int64_t) sizeof( file ) );
    check( saved == measure && saved > 0,
           "W8: the production writer saves the record (returns measure bytes)" );

    /* body: header 16, layout length 4, the layout, the record's hash 8. */
    body = kTableFixedHeaderBytes + 4 + vessel_fixed_layout_bytes + 8;

    check( table_fixed_get32( file + body + 36 ) == 2,
           "W8: the length word at body+36 is 2 (the live extent, not Max)" );
    check( file[body + 40] == 0xDE && file[body + 41] == 0xAD,
           "W8: the two live bytes ride at body+40..42" );
    check( file[body + 42] == 0 && file[body + 43] == 0,
           "W8: the slack past the live bytes is the template's zeros (body+42..44) — never all Max" );
    check( file[saved] == 0xAB,
           "W8: the writer stopped at the live extent (the byte past the file is untouched)" );

    /* ---- THE ROW: bytes(N) takes the ARRAY's row --------------------------
       The destination row the plan compiler reads for `tag`: the count to
       `aux`, the elements to `dst` — the array's row, not a text field's. */
    rows = sizeof( vessel_fixed_dst ) / sizeof( vessel_fixed_dst[0] );
    found = rows;
    for ( i = 0; i < rows; ++i )
    {
        if ( vessel_fixed_dst[i].stride == 1u && vessel_fixed_dst[i].counted != 0u ) { found = i; break; }
    }
    check( found != rows,
           "W8: the bytes(N) row is the one with stride one and a live count" );
    if ( found != rows )
    {
        check( vessel_fixed_dst[found].dst == (uint32_t) offsetof( Vessel, tag ),
               "W8: the elements go to the row's dst (the buffer's storage)" );
        check( vessel_fixed_dst[found].aux == (uint32_t) offsetof( Vessel, tag_length ),
               "W8: the count goes to the row's aux (the length's storage)" );
        check( vessel_fixed_dst[found].meta == kTableFixedTextBytes,
               "W8: the row's flavour is bytes (meta 3)" );
    }

    /* ---- THE COMPILED PLAN over the row, and THE CONTROL ------------------
       Compile the same plan from the same layout twice: once over this
       build's own rows, once over the copy with the two columns swapped back
       to the text convention. The destination is OVERSIZED on purpose: the
       wrong rows put an element destination where the length field is, and
       four bytes of elements past a four-byte field is a step outside the
       storage — the control gives it room to be wrong, and what reports the
       bug is the value that comes back rather than the sanitizer. */
    {
        TableFixedLayoutView theirs;
        static TableFixedDst swapped[sizeof( vessel_fixed_dst ) / sizeof( vessel_fixed_dst[0] )];
        static uint64_t storage[sizeof( Vessel ) / 8 + 16];
        const uint8_t * rec = file + body;
        int why = 0;
        int pass;

        check( table_fixed_parse_layout( file + kTableFixedHeaderBytes + 4,
                                         (int64_t) table_fixed_get32( file + kTableFixedHeaderBytes ),
                                         &theirs, &why ),
               "W8: the writer's layout parses" );
        for ( i = 0; i < rows; ++i ) { swapped[i] = vessel_fixed_dst[i]; }
        if ( found != rows )
        {
            uint32_t d = swapped[found].dst;
            swapped[found].dst = swapped[found].aux; /* the TEXT convention, as it was */
            swapped[found].aux = d;
        }

        for ( pass = 0; pass < 2; ++pass )
        {
            const TableFixedDst * rowset = pass == 0 ? vessel_fixed_dst : swapped;
            Vessel * out = (Vessel *) (void *) storage;
            int32_t guarded = 0, made;
            uint32_t fill_at = 0;
            int32_t fill_count = 0;
            int right;

            made = table_fixed_compile( &theirs, vessel_fixed_layout, (int32_t) vessel_fixed_layout_bytes,
                                        rowset, vessel_fixed_cover, vessel_fixed_cover_count,
                                        plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ),
                                        &guarded, &fill_at, &fill_count, NULL );
            check( made > 0,
                   "W8: the plan compiles either way — the rows are not what refuses" );
            memset( storage, 0, sizeof( storage ) );
            vessel_reset( out );
            memset( &r, 0, sizeof( r ) );
            table_fixed_run( plan, made, guarded, rec, (uint8_t *) out, &r );
            right = out->tag_length == 2 && out->tag[0] == 0xDE && out->tag[1] == 0xAD;
            if ( pass == 0 )
            {
                check( right,
                       "W8: the array row lands the buffer in the buffer and the length in the length" );
            }
            else
            {
                check( !right,
                       "W8 NEGATIVE CONTROL: the text row really does write the count into the buffer — live bytes lost" );
            }
        }
    }

    /* ---- THE READ: the identity plan (this build's own hash) reads it back */
    memset( &r, 0, sizeof( r ) );
    check( vessel_fixed_load( &back, 1, file, saved, plan,
                              (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ), NULL, &r ) == 1,
           "W8: the reader reads one record" );
    check( back.tag_length == 2, "W8: the reader reads the length word as 2" );
    check( back.tag[0] == 0xDE && back.tag[1] == 0xAD,
           "W8: the reader reads the two live bytes back" );
    check( r.clamped == 0 && !r.malformed && !r.refused,
           "W8: a clean read moves no counter" );

    if ( failures != 0 )
    {
        printf( "W8: FAILED\n" );
        return 1;
    }
    printf( "W8: bytes(N) takes the array row — green\n" );
    return 0;
}
