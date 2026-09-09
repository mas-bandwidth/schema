/* FX2's half of the fixed form's versioning conformance (docs/SPEC-TABLES.md
   §3.4). The only translation unit that names tblfx2's types. */

#include <string.h>

#include "FX2Table.h"
#include "fixedform.h"

#define PlanCapacity 8192
static TableFixedEntry g_plan[PlanCapacity];

int64_t fixed_fx2_bytes( void ) { return fx_root_fixed_measure( 1 ); }

int64_t fixed_fx2_write( uint8_t * buffer, int64_t capacity )
{
    FxRoot two;
    fx_root_reset( &two );
    two.keep = 5150u;
    two.narrow = 70000u;      /* past uint16: FX1 must NOT narrow it, it must skip */
    two.renamed_to = 808;
    two.added = 12;
    two.nested.a = 33;
    two.nested.b = 44;
    two.extra.x = 5;
    two.extra.y = 6;
    return fx_root_fixed_save( &two, 1, buffer, capacity );
}

/* CASE 2: AN OLDER WRITER — a WIDENED field decoded exactly and counted, a
   RENAMED field arriving under `was =`, a field this reader has that the
   record does not (the prefill's declared default stands), a whole nested type
   the writer never carried, and the one field the writer has that this reader
   cannot name. */
void fixed_fx2_read_fx1( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    int64_t n;
    memset( &r, 0, sizeof( r ) );
    n = fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
    fixed_check( n == 1, "older writer: one record" );
    fixed_check( back.keep == 4242u, "older writer: an unmoved field" );
    fixed_check( back.narrow == 40000u, "WIDENED: uint16 into uint32, exactly" );
    fixed_check( r.widened == 1, "WIDENED: one widened counts" );
    fixed_check( back.renamed_to == 321, "RENAMED: `was =` keeps the wire id" );
    fixed_check( back.added == 11, "MISSING: a field the writer does not carry takes its declared default" );
    fixed_check( back.extra.x == 0 && back.extra.y == 0, "MISSING: a whole nested type takes its defaults" );
    fixed_check( back.nested.a == 111 && back.nested.b == 222, "older writer: the nesting" );
    fixed_check( r.unknown == 1, "older writer: `gone` is the one field this reader cannot name" );
    fixed_check( r.kind_mismatch == 0 && !r.malformed && !r.refused, "older writer: nothing else fired" );
}

/* A READ THAT CLAIMS NOTHING ABOUT THE VALUES. A form-3 file with one bit
   flipped is a block whose child sizes may not sum to its parent's, a tree that
   may not close, a chain a thousand deep, or a record count that divides
   wrongly. Every one of those must come out as a refusal by name, a malformed
   read, or a read that lands values — and never as a step outside the buffer,
   which is what the sanitized build of this file is here to say. */
void fixed_fx2_probe( const uint8_t * data, int64_t bytes )
{
    FxRoot back;
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    fx_root_reset( &back );
    (void) fx_root_fixed_load( &back, 1, data, bytes, g_plan, PlanCapacity, &r );
}
