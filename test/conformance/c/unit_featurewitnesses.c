#include "FeaturesTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE featurewitnesses
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER FEATUREWITNESSES
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Root,root)
SCHEMA_CONFORMANCE_GRAPH_CODEC(ArrayRoot,array_root)
const ConformanceCodec * conformance_codecs_featurewitnesses(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(featurewitnesses,Root),SCHEMA_CONFORMANCE_GRAPH_ROW(featurewitnesses,ArrayRoot)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
