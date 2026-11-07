// the streamdemo unit (tables/stream).
#include "StreamTable.h"
#define HC_NS streamdemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Feed )

extern "C" const hc_binding_t * hc_bindings_streamdemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "tables/stream", "Feed", Feed ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
