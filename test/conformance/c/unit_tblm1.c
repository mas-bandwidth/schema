#include "M1Table.h"
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Msg, msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblm1, Msg )
};
const ConformanceCodec * conformance_codecs_tblm1( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
