// the featurewitnesses unit (testdata/conformance/feature-witnesses/full).
#include "FeaturesTable.h"
#define HC_NS featurewitnesses
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Root )

extern "C" const hc_binding_t * hc_bindings_featurewitnesses( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "testdata/conformance/feature-witnesses/full/Features.schema", "Root", Root ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
