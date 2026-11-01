// the probe unit (schemas/probe.schema).
#include "probeTable.h"
#define HC_NS probe
#include "hostile.hpp"

HC_CPP_REASONS()
HC_VAR( Root )

extern "C" const hc_binding_t * hc_bindings_probe( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_VAR_ROW( "schemas/probe.schema", "Root", Root ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
