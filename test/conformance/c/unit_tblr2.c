#include "R2Table.h"
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Cfg, cfg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblr2, Cfg )
};
const ConformanceCodec * conformance_codecs_tblr2( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
