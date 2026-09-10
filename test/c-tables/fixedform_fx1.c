/* FX1's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4). This translation unit is the ONLY one that names tblfx1's types; see
   fixedform.h for why there is more than one. */

#include <string.h>

#include "FX1Table.h"
#include "fixedform.h"

/* The plan storage the caller owns. The identity path never touches it; a
   plan compiled from another writer's layout lands in it, and a layout whose
   plan does not fit is a refusal by name (§3.4: this codec never allocates). */
#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

static void fill( FxRoot * value )
{
    fx_root_reset( value );
    value->keep = 4242u;
    value->narrow = 40000u;   /* a uint16 value FX2's widened read must reproduce */
    value->renamed = 321;
    value->gone = 654;
    value->nested.a = 111;
    value->nested.b = 222;
    value->label[0] = 'h';
    value->label[1] = 'i';
    value->label_length = 2;
    value->marks[0] = 7;
    value->marks_count = 1;
    /* A `bytes(N)` IS AN ARRAY OF u8 ON THIS WIRE (§3.4), so its destination
       row is an ARRAY's — the buffer, and the live length beside it — and not
       a text field's, which is the other way round. Only a COMPILED plan reads
       those columns, so only FX2's read of this record can tell. */
    value->blob[0] = 0xDE; value->blob[1] = 0xAD; value->blob[2] = 0xBE; value->blob[3] = 0xEF;
    value->blob_length = 4;
}

/* THE SLACK IS ZERO (docs/SPEC-TABLES.md §3.4), and this is the C twin of the
   reference's slack_case. A `string(N)` shorter than N and a `[..N]T` with
   unused slots are DECLARED bytes carrying no value: what rides in them is the
   TEMPLATE'S ZEROS, never whatever this writer's storage held past the used
   length or the live count.

   THE CONTROL IS THE STAIN: the storage past the used length and the live
   count is filled with a byte a clean record carries nowhere, the test proves
   the stain IS in the storage, then proves the WIRE carries none of it, then
   proves a whole-span copy of the same storage WOULD have carried it. */
void fixed_fx1_slack( void )
{
    static uint8_t file[8192];
    static TableFixedEntry plan[PlanCapacity];
    FxRoot v, back;
    TableReport r;
    const uint8_t * body;
    size_t body_bytes;
    uint8_t whole[sizeof( v.label )];
    int32_t marks[4];
    int k;
    int64_t need, n;

    fx_root_reset( &v );
    v.keep = 11u;
    v.narrow = 22u;
    v.renamed = 33;
    v.gone = 44;
    v.nested.a = 55;
    v.nested.b = 66;
    memset( v.label, 0xAA, sizeof( v.label ) );
    v.label[0] = 'h';
    v.label[1] = 'i';
    v.label_length = 2;
    for ( k = 0; k < 4; k++ ) { v.marks[k] = 0x5A5A5A5A; }
    v.marks[0] = 7;
    v.marks_count = 1;

    fixed_check( (uint8_t) v.label[2] == 0xAAu, "C CONTROL: the text slack really is stained in storage" );
    fixed_check( v.marks[1] == 0x5A5A5A5A, "C CONTROL: the array slack really is stained in storage" );

    need = fx_root_fixed_measure( 1 );
    fixed_check( need <= (int64_t) sizeof( file ), "C slack: the record fits the buffer" );
    fixed_check( fx_root_fixed_save( &v, 1, file, (int64_t) sizeof( file ) ) == need, "C slack: the record saves" );
    body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof( fx_root_fixed_layout ) + 8;
    body_bytes = (size_t) fx_root_fixed_body_bytes;
    fixed_check( memchr( body, 0xAA, body_bytes ) == NULL,
                 "C SLACK IS ZERO: not one stained TEXT byte reached the wire" );
    fixed_check( memchr( body, 0x5A, body_bytes ) == NULL,
                 "C SLACK IS ZERO: not one stained ARRAY byte reached the wire" );

    memcpy( whole, v.label, sizeof( v.label ) );
    fixed_check( memchr( whole, 0xAA, sizeof( whole ) ) != NULL,
                 "C NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain" );
    memcpy( marks, v.marks, sizeof( marks ) );
    fixed_check( marks[3] == 0x5A5A5A5A,
                 "C NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain" );

    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, file, need, plan, PlanCapacity, &r );
    fixed_check( n == 1, "C slack: the record reads" );
    fixed_check( back.label_length == 2 && back.label[0] == 'h' && back.label[1] == 'i' && back.label[2] == 0,
                 "C slack: the used length reads, and the buffer terminates at it" );
    fixed_check( back.marks_count == 1 && back.marks[0] == 7, "C slack: the live count reads" );
    fixed_check( back.marks[1] == 0 && back.marks[2] == 0 && back.marks[3] == 0,
                 "C slack: an unused slot lands as the wire's zero" );
    fixed_check( r.clamped == 0 && !r.malformed && !r.refused, "C slack: a clean read moves no counter" );
}

int64_t fixed_fx1_bytes( void ) { return fx_root_fixed_measure( 1 ); }

int64_t fixed_fx1_write( uint8_t * buffer, int64_t capacity )
{
    FxRoot one;
    fill( &one );
    return fx_root_fixed_save( &one, 1, buffer, capacity );
}

/* CASE 1: THE SAME SCHEMA — the identity plan, a static constant this build
   laid down, and the loop that runs it is the loop every other case runs. */
void fixed_fx1_read_own( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
    fixed_check( n == 1, "same schema: one record" );
    fixed_check( back.keep == 4242u && back.narrow == 40000u && back.renamed == 321 && back.gone == 654,
                 "same schema: the scalars" );
    fixed_check( back.nested.a == 111 && back.nested.b == 222, "same schema: the nesting" );
    fixed_check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
                 "same schema: a silent report" );
}

/* CASE 3: A NEWER WRITER — a field this reader cannot name, stepped over by
   the size its layout entry states; a whole nested TYPE it cannot name, stepped
   over by its layout size; a field the writer dropped, which keeps this
   reader's declared default; and a kind that MOVED, which is reported and
   never reinterpreted. */
void fixed_fx1_read_fx2( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
    fixed_check( n == 1, "newer writer: one record" );
    fixed_check( back.keep == 5150u, "newer writer: an unmoved field lands past the unknowns" );
    fixed_check( back.renamed == 808, "newer writer: `was =` reads the other way too" );
    fixed_check( back.gone == 9, "newer writer: a field the writer dropped takes its declared default" );
    fixed_check( back.nested.a == 33 && back.nested.b == 44, "newer writer: the nesting lands past the unknown type" );
    fixed_check( r.unknown == 2, "newer writer: two names this reader does not have" );
    fixed_check( r.kind_mismatch == 1, "newer writer: uint32 into uint16 is a kind that moved, not a widening" );
    fixed_check( back.narrow == 3, "newer writer: a narrowing leaves the declared default" );
    fixed_check( !r.malformed && !r.refused, "newer writer: no damage and no refusal" );
}
