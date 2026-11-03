#include "Vocab9Table.h"
#define SCHEMA_CONFORMANCE_PACKAGE vocab9demo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER VOCAB9DEMO
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( Wide00, wide00 )
SCHEMA_CONFORMANCE_CODEC( Wide19, wide19 )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( vocab9demo, Wide00 ),
    SCHEMA_CONFORMANCE_ROW( vocab9demo, Wide19 )
};
const ConformanceCodec * conformance_codecs_vocab9demo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( Wide00, wide00 )
SCHEMA_CONFORMANCE_STRICT( Wide19, wide19 )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( vocab9demo, Wide00 ),
    SCHEMA_CONFORMANCE_STRICT_ROW( vocab9demo, Wide19 )
};

const ConformanceStrict * conformance_stricts_vocab9demo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
