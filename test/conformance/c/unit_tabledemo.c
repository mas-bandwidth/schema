/* the tabledemo unit's codecs (tables/examples). */
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( RootConfig )
SCHEMA_CONFORMANCE_CODEC( ProfileConfig )
SCHEMA_CONFORMANCE_CODEC( LoadoutConfig )
SCHEMA_CONFORMANCE_CODEC( WideBlob )
SCHEMA_CONFORMANCE_CODEC( ArchiveConfig )
SCHEMA_CONFORMANCE_CODEC( KeyedConfig )
SCHEMA_CONFORMANCE_CODEC( PackConfig )

static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( tabledemo, RootConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, ProfileConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, LoadoutConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, WideBlob ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, ArchiveConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, KeyedConfig ),
    SCHEMA_CONFORMANCE_ROW( tabledemo, PackConfig )
};

const ConformanceCodec * conformance_codecs_tabledemo( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
