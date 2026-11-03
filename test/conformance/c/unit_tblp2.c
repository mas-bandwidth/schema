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

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_GRAPH_STRICT( Chain, chain )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tblp2, Chain )
};

const ConformanceStrict * conformance_stricts_tblp2( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
