#include "AssetsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE blobdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER BLOBDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Catalog,catalog)
const ConformanceCodec * conformance_codecs_blobdemo(int * count)
{
 static const ConformanceCodec codecs[]={SCHEMA_CONFORMANCE_GRAPH_ROW(blobdemo,Catalog)};
 *count=(int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_GRAPH_STRICT( Catalog, catalog )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( blobdemo, Catalog )
};

const ConformanceStrict * conformance_stricts_blobdemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
