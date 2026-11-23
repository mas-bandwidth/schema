// the shapes unit (test/tables/shapes).
#include "ShapesTable.h"
#define HC_NS shapes
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Bounds )
HC_FIXED( FixedBounds )

extern "C" const hc_binding_t * hc_bindings_shapes( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "test/tables/shapes", "Bounds", Bounds ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/shapes", "FixedBounds", FixedBounds ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
