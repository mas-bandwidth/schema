#include "CaptionTable.h"
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Caption, caption )
SCHEMA_CONFORMANCE_CODEC( Stamp, stamp )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( widedemo, Caption ),
    SCHEMA_CONFORMANCE_ROW( widedemo, Stamp )
};
const ConformanceCodec * conformance_codecs_widedemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
