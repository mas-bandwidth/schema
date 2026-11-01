/* the graphdemo unit (tables/pointers). */
#include "GraphTable.h"
#define HC_PACKAGE graphdemo
#define HC_PACKAGE_UPPER GRAPHDEMO
#include "hostile_c.h"

HC_VAR( scene )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "tables/pointers", "Scene", scene ) },
};

const hc_binding_t * hc_bindings_graphdemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
