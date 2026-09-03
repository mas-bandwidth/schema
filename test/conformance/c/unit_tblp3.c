/* the tblp3 unit's codecs (test/tables/P3.schema). */
#include "P3Table.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Chain )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblp3, Chain )
};

const ConformanceCodec * conformance_codecs_tblp3( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
