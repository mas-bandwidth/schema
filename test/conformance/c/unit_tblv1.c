/* the tblv1 unit's codecs (test/tables/V1.schema). */
#include "V1Table.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Cfg )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblv1, Cfg )
};

const ConformanceCodec * conformance_codecs_tblv1( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
