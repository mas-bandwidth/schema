/* the shapes unit (test/tables/shapes). */
#include "ShapesTable.h"
#define HC_PACKAGE shapes
#define HC_PACKAGE_UPPER SHAPES
#include "hostile_c.h"

HC_VAR( bounds )
HC_FIXED( fixed_bounds )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "test/tables/shapes", "Bounds", bounds ) },
    { HC_FIXED_FIELDS( "test/tables/shapes", "FixedBounds", fixed_bounds ) },
};

const hc_binding_t * hc_bindings_shapes( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
