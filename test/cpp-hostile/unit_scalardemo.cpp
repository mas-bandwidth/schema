// the scalardemo unit (tables/scalars).
#include "ScalarsTable.h"
#define HC_NS scalardemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_FIXED( SimState )

extern "C" const hc_binding_t * hc_bindings_scalardemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_FIXED_ROW( "tables/scalars", "SimState", SimState ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
