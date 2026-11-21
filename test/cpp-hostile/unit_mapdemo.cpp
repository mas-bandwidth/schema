// the mapdemo unit (tables/maps).
#include "TextTable.h"
#include "DepthTable.h"
#include "RowsTable.h"
#define HC_NS mapdemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Text )
HC_VAR( Depth )
HC_VAR( EdgeRow )

extern "C" const hc_binding_t * hc_bindings_mapdemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "tables/maps", "Text", Text ); rows.push_back( row ); }
        { HC_VAR_ROW( "tables/maps", "Depth", Depth ); rows.push_back( row ); }
        { HC_VAR_ROW( "tables/maps", "EdgeRow", EdgeRow ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
