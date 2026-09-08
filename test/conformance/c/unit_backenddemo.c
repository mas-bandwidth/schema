#include "BackendTable.h"
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
