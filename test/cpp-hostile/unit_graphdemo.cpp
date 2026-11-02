// the graphdemo unit (tables/pointers).
#include "GraphTable.h"
#define HC_NS graphdemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Scene )

extern "C" const hc_binding_t * hc_bindings_graphdemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "tables/pointers", "Scene", Scene ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
