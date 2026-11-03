#include "BackendTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE backenddemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER BACKENDDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( LoginRequest, login_request )
SCHEMA_CONFORMANCE_CODEC( MatchResult, match_result )
SCHEMA_CONFORMANCE_CODEC( StorePurchase, store_purchase )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( backenddemo, LoginRequest ),
    SCHEMA_CONFORMANCE_ROW( backenddemo, MatchResult ),
    SCHEMA_CONFORMANCE_ROW( backenddemo, StorePurchase )
};
const ConformanceCodec * conformance_codecs_backenddemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( LoginRequest, login_request )
SCHEMA_CONFORMANCE_STRICT( MatchResult, match_result )
SCHEMA_CONFORMANCE_STRICT( StorePurchase, store_purchase )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( backenddemo, LoginRequest ),
    SCHEMA_CONFORMANCE_STRICT_ROW( backenddemo, MatchResult ),
    SCHEMA_CONFORMANCE_STRICT_ROW( backenddemo, StorePurchase )
};

const ConformanceStrict * conformance_stricts_backenddemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
