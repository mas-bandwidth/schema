// the blobdemo unit (tables/blobs).
#include "AssetsTable.h"
#define HC_NS blobdemo
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Catalog )

extern "C" const hc_binding_t * hc_bindings_blobdemo( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "tables/blobs", "Catalog", Catalog ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
