/* the scalardemo unit (tables/scalars). */
#include "ScalarsTable.h"
#define HC_PACKAGE scalardemo
#define HC_PACKAGE_UPPER SCALARDEMO
#include "hostile_c.h"

HC_FIXED( sim_state )

static const hc_binding_t rows[] = {
    { HC_FIXED_FIELDS( "tables/scalars", "SimState", sim_state ) },
};

const hc_binding_t * hc_bindings_scalardemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
