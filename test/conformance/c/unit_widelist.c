#include "WidesTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE widelistdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER WIDELISTDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC( Wide, wide )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( widelist, Wide )
};
const ConformanceCodec * conformance_codecs_widelist( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
