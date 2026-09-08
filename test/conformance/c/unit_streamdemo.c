#include "StreamTable.h"
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Feed,feed)
const ConformanceCodec * conformance_codecs_streamdemo(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(streamdemo,Feed)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
