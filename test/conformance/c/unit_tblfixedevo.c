/* the tblfixedevo unit's codecs (test/tables/fixedevo). */
#include "FixedEvoTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblfixedevo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLFIXEDEVO
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( EvoRoot, evo_root )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tblfixedevo, EvoRoot )
};
const ConformanceCodec * conformance_codecs_tblfixedevo( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
