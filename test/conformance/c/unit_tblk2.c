#include "K2Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblk2
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLK2
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Root, root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblk2, Root )
};
const ConformanceCodec * conformance_codecs_tblk2( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( Root, root )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tblk2, Root )
};

const ConformanceStrict * conformance_stricts_tblk2( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
