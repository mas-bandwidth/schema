/* the tblv1 unit's codecs (test/tables/V1.schema). */
#include "V1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblv1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLV1
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Cfg, cfg )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblv1, Cfg )
};

const ConformanceCodec * conformance_codecs_tblv1( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
