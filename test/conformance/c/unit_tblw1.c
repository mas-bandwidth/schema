#include "W1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblw1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLW1
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Fleet,fleet)
const ConformanceCodec * conformance_codecs_tblw1(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblw1,Fleet)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
