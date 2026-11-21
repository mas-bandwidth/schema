/* the mapdemo unit (tables/maps). */
#include "TextTable.h"
#include "DepthTable.h"
#include "RowsTable.h"
#define HC_PACKAGE mapdemo
#define HC_PACKAGE_UPPER MAPDEMO
#include "hostile_c.h"

HC_VAR( text )
HC_VAR( depth )
HC_VAR( edge_row )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "tables/maps", "Text", text ) },
    { HC_VAR_FIELDS( "tables/maps", "Depth", depth ) },
    { HC_VAR_FIELDS( "tables/maps", "EdgeRow", edge_row ) },
};

const hc_binding_t * hc_bindings_mapdemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
