/* the mapdemo unit (tables/maps). */
#include "TextTable.h"
#define HC_PACKAGE mapdemo
#define HC_PACKAGE_UPPER MAPDEMO
#include "hostile_c.h"

HC_VAR( text )

static const hc_binding_t rows[] = {
    { HC_VAR_FIELDS( "tables/maps", "Text", text ) },
};

const hc_binding_t * hc_bindings_mapdemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
