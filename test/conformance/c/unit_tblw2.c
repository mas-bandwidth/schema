#include "W2Table.h"
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Fleet,fleet)
const ConformanceCodec * conformance_codecs_tblw2(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblw2,Fleet)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
