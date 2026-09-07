#include "AssetsTable.h"
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Catalog,catalog)
const ConformanceCodec * conformance_codecs_blobdemo(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(blobdemo,Catalog)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
