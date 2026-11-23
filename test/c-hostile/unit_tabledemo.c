/* the tabledemo unit (tables/examples). */
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "GuardedTable.h"
#define HC_PACKAGE tabledemo
#define HC_PACKAGE_UPPER TABLEDEMO
#include "hostile_c.h"

HC_FIXED( root_config )
HC_FIXED( pack_config )
HC_FIXED( global_settings )
HC_FIXED( ship_entry )
HC_FIXED( counted_min )
HC_FIXED( float_bound )
HC_FIXED( moods )
HC_FIXED( mood_field )
HC_VAR( patrol )
HC_ANNOUNCE()

static const hc_binding_t rows[] = {
    { HC_FIXED_FIELDS( "tables/examples", "RootConfig", root_config ) },
    { HC_FIXED_FIELDS( "tables/examples", "PackConfig", pack_config ), .announce = hc_announce },
    { HC_FIXED_FIELDS( "tables/examples", "GlobalSettings", global_settings ) },
    { HC_FIXED_FIELDS( "tables/examples", "ShipEntry", ship_entry ) },
    { HC_FIXED_FIELDS( "tables/examples", "CountedMin", counted_min ) },
    { HC_FIXED_FIELDS( "tables/examples", "FloatBound", float_bound ) },
    { HC_FIXED_FIELDS( "tables/examples", "Moods", moods ) },
    { HC_FIXED_FIELDS( "tables/examples", "MoodField", mood_field ) },
    { HC_VAR_FIELDS( "tables/examples/Guarded.schema", "Patrol", patrol ) },
};

const hc_binding_t * hc_bindings_tabledemo( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
