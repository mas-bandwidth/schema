// the inner unit (test/tables/inner): a bound held one level down.
#include "InnerTable.h"
#define HC_NS inner
#include "hostile.hpp"

HC_CPP_REASONS()
HC_FIXED( FixedLooseCount )
HC_FIXED( FixedTightCount )
HC_FIXED( FixedLooseRange )
HC_FIXED( FixedTightRange )
HC_FIXED( FixedLooseText )
HC_FIXED( FixedTightText )
HC_FIXED( FixedLooseBytes )
HC_FIXED( FixedTightBytes )
HC_VAR( VarLooseCount )
HC_VAR( VarTightCount )
HC_VAR( VarLooseRange )
HC_VAR( VarTightRange )
HC_VAR( VarLooseText )
HC_VAR( VarTightText )
HC_VAR( VarLooseBytes )
HC_VAR( VarTightBytes )

extern "C" const hc_binding_t * hc_bindings_inner( int * count )
{
    static std::vector<hc_binding_t> rows;
    if ( rows.empty() )
    {
        { HC_FIXED_ROW( "test/tables/inner", "FixedLooseCount", FixedLooseCount ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedTightCount", FixedTightCount ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedLooseRange", FixedLooseRange ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedTightRange", FixedTightRange ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedLooseText", FixedLooseText ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedTightText", FixedTightText ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedLooseBytes", FixedLooseBytes ); rows.push_back( row ); }
        { HC_FIXED_ROW( "test/tables/inner", "FixedTightBytes", FixedTightBytes ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarLooseCount", VarLooseCount ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarTightCount", VarTightCount ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarLooseRange", VarLooseRange ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarTightRange", VarTightRange ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarLooseText", VarLooseText ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarTightText", VarTightText ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarLooseBytes", VarLooseBytes ); rows.push_back( row ); }
        { HC_VAR_ROW( "test/tables/inner", "VarTightBytes", VarTightBytes ); rows.push_back( row ); }
    }
    *count = (int) rows.size();
    return rows.data();
}
