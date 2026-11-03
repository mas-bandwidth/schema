#include "M1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblm1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLM1
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Msg, msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblm1, Msg )
};
const ConformanceCodec * conformance_codecs_tblm1( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( Msg, msg )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tblm1, Msg )
};

const ConformanceStrict * conformance_stricts_tblm1( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
