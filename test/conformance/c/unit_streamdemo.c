#include "StreamTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE streamdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER STREAMDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Feed,feed)
const ConformanceCodec * conformance_codecs_streamdemo(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(streamdemo,Feed)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_GRAPH_STRICT( Feed, feed )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( streamdemo, Feed )
};

const ConformanceStrict * conformance_stricts_streamdemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
