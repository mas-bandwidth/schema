#include "ValueDefaultsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblvaluedefaults
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLVALUEDEFAULTS
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_CODEC( VarDefaults, var_defaults )
SCHEMA_CONFORMANCE_CODEC( FixedDefaults, fixed_defaults )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( tblvaluedefaults, VarDefaults ),
    SCHEMA_CONFORMANCE_ROW( tblvaluedefaults, FixedDefaults )
};
const ConformanceCodec * conformance_codecs_tblvaluedefaults( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
