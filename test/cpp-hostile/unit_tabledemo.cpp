// the tabledemo unit (tables/examples).
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#define HC_NS tabledemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_FIXED( RootConfig )
HC_FIXED( PackConfig )
HC_FIXED( GlobalSettings )
HC_FIXED( ShipEntry )
HC_ANNOUNCE()

extern "C" const hc_binding_t * hc_bindings_tabledemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_FIXED_ROW( "tables/examples", "RootConfig", RootConfig ); rows.push_back( row ); }
        { HC_FIXED_ROW( "tables/examples", "PackConfig", PackConfig ); row.announce = hc_announce; rows.push_back( row ); }
        { HC_FIXED_ROW( "tables/examples", "GlobalSettings", GlobalSettings ); rows.push_back( row ); }
        { HC_FIXED_ROW( "tables/examples", "ShipEntry", ShipEntry ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
