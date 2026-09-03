/* the tblv2 unit's codecs (test/tables/V2.schema). */
#include "V2Table.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Cfg, cfg )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblv2, Cfg )
};

const ConformanceCodec * conformance_codecs_tblv2( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
