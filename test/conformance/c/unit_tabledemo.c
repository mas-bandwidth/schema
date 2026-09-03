/* the tabledemo unit's codecs (tables/examples). */
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "unit.h"

SCHEMA_CONFORMANCE_CODEC( RootConfig, root_config )
SCHEMA_CONFORMANCE_CODEC( ProfileConfig, profile_config )
SCHEMA_CONFORMANCE_CODEC( LoadoutConfig, loadout_config )
SCHEMA_CONFORMANCE_CODEC( WideBlob, wide_blob )
SCHEMA_CONFORMANCE_CODEC( ArchiveConfig, archive_config )
SCHEMA_CONFORMANCE_CODEC( KeyedConfig, keyed_config )
SCHEMA_CONFORMANCE_CODEC( PackConfig, pack_config )

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
