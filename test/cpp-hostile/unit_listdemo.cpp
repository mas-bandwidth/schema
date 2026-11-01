// the listdemo unit (tables/lists).
#include "HoldersTable.h"
#define HC_NS listdemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Army )

extern "C" const hc_binding_t * hc_bindings_listdemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "tables/lists", "Army", Army ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
