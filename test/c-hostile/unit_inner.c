/* the inner unit (test/tables/inner): a bound held one level down. */
#include "InnerTable.h"
#define HC_PACKAGE inner
#define HC_PACKAGE_UPPER INNER
#include "hostile_c.h"

HC_FIXED( fixed_loose_count )
HC_FIXED( fixed_tight_count )
HC_FIXED( fixed_loose_range )
HC_FIXED( fixed_tight_range )
HC_FIXED( fixed_loose_text )
HC_FIXED( fixed_tight_text )
HC_FIXED( fixed_loose_bytes )
HC_FIXED( fixed_tight_bytes )
HC_VAR( var_loose_count )
HC_VAR( var_tight_count )
HC_VAR( var_loose_range )
HC_VAR( var_tight_range )
HC_VAR( var_loose_text )
HC_VAR( var_tight_text )
HC_VAR( var_loose_bytes )
HC_VAR( var_tight_bytes )

static const hc_binding_t rows[] = {
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedLooseCount", fixed_loose_count ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedTightCount", fixed_tight_count ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedLooseRange", fixed_loose_range ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedTightRange", fixed_tight_range ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedLooseText", fixed_loose_text ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedTightText", fixed_tight_text ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedLooseBytes", fixed_loose_bytes ) },
    { HC_FIXED_FIELDS( "test/tables/inner", "FixedTightBytes", fixed_tight_bytes ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarLooseCount", var_loose_count ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarTightCount", var_tight_count ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarLooseRange", var_loose_range ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarTightRange", var_tight_range ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarLooseText", var_loose_text ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarTightText", var_tight_text ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarLooseBytes", var_loose_bytes ) },
    { HC_VAR_FIELDS( "test/tables/inner", "VarTightBytes", var_tight_bytes ) },
};

const hc_binding_t * hc_bindings_inner( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
