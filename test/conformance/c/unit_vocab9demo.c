#include "Vocab9Table.h"
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Wide00, wide00 )
SCHEMA_CONFORMANCE_CODEC( Wide19, wide19 )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( vocab9demo, Wide00 ),
    SCHEMA_CONFORMANCE_ROW( vocab9demo, Wide19 )
};
const ConformanceCodec * conformance_codecs_vocab9demo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
