#include "CaptionTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE wide
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER WIDE
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Caption, caption )
SCHEMA_CONFORMANCE_CODEC( Stamp, stamp )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( widedemo, Caption ),
    SCHEMA_CONFORMANCE_ROW( widedemo, Stamp )
};
const ConformanceCodec * conformance_codecs_widedemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( Caption, caption )
SCHEMA_CONFORMANCE_STRICT( Stamp, stamp )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( widedemo, Caption ),
    SCHEMA_CONFORMANCE_STRICT_ROW( widedemo, Stamp )
};

const ConformanceStrict * conformance_stricts_widedemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
