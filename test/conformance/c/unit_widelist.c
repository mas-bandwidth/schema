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

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_GRAPH_STRICT( Wide, wide )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( widelist, Wide )
};

const ConformanceStrict * conformance_stricts_widelist( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
