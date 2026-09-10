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
