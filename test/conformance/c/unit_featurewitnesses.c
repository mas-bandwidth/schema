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

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_GRAPH_STRICT( Root, root )
SCHEMA_CONFORMANCE_GRAPH_STRICT( ArrayRoot, array_root )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( featurewitnesses, Root ),
    SCHEMA_CONFORMANCE_STRICT_ROW( featurewitnesses, ArrayRoot )
};

const ConformanceStrict * conformance_stricts_featurewitnesses( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
