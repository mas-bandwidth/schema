/* the tblp1 unit's codecs (test/tables/P1.schema). */
#include "P1Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblp1
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLP1
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( Chain, chain )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblp1, Chain )
};

const ConformanceCodec * conformance_codecs_tblp1( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( Chain, chain )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tblp1, Chain )
};

const ConformanceStrict * conformance_stricts_tblp1( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
