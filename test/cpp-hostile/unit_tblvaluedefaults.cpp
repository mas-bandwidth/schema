// the tblvaluedefaults unit (test/tables/ValueDefaults.schema).
#include "ValueDefaultsTable.h"
#define HC_NS tblvaluedefaults
#include "hostile.hpp"

HC_CPP_REASONS()
HC_FIXED( FixedDefaults )
HC_VAR( VarDefaults )

extern "C" const hc_binding_t * hc_bindings_tblvaluedefaults( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_FIXED_ROW( "test/tables/ValueDefaults.schema", "FixedDefaults", FixedDefaults ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/ValueDefaults.schema", "VarDefaults", VarDefaults ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
