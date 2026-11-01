/* the tabledemo unit (tables/examples). */
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#define HC_PACKAGE tabledemo
#define HC_PACKAGE_UPPER TABLEDEMO
#include "hostile_c.h"

HC_FIXED( root_config )
HC_FIXED( pack_config )
HC_FIXED( global_settings )
HC_FIXED( ship_entry )
HC_ANNOUNCE()

static const hc_binding_t rows[] = {
    { HC_FIXED_FIELDS( "tables/examples", "RootConfig", root_config ) },
    { HC_FIXED_FIELDS( "tables/examples", "PackConfig", pack_config ), .announce = hc_announce },
    { HC_FIXED_FIELDS( "tables/examples", "GlobalSettings", global_settings ) },
    { HC_FIXED_FIELDS( "tables/examples", "ShipEntry", ship_entry ) },
};

const hc_binding_t * hc_bindings_tabledemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
