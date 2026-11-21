/* the tblvaluedefaults unit (test/tables/ValueDefaults.schema). */
#include "ValueDefaultsTable.h"
#define HC_PACKAGE tblvaluedefaults
#define HC_PACKAGE_UPPER TBLVALUEDEFAULTS
#include "hostile_c.h"

HC_FIXED( fixed_defaults )
HC_VAR( var_defaults )

static const hc_binding_t rows[] = {
    { HC_FIXED_FIELDS( "test/tables/ValueDefaults.schema", "FixedDefaults", fixed_defaults ) },
    { HC_VAR_FIELDS( "test/tables/ValueDefaults.schema", "VarDefaults", var_defaults ) },
};

const hc_binding_t * hc_bindings_tblvaluedefaults( int * count )
{
    *count = (int) ( sizeof( rows ) / sizeof( rows[0] ) );
    return rows;
}
