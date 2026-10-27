#include "P2Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblp2
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLP2
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Chain,chain)
const ConformanceCodec * conformance_codecs_tblp2(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(tblp2,Chain)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
