#include "G1Table.h"
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Guarded,guarded)
const ConformanceCodec * conformance_codecs_tblg1(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblg1,Guarded)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
