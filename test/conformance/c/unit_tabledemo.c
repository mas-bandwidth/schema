/* the tabledemo unit's codecs (tables/examples). */
#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tabledemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TABLEDEMO
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

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( RootConfig, root_config )
SCHEMA_CONFORMANCE_STRICT( ProfileConfig, profile_config )
SCHEMA_CONFORMANCE_STRICT( LoadoutConfig, loadout_config )
SCHEMA_CONFORMANCE_STRICT( WideBlob, wide_blob )
SCHEMA_CONFORMANCE_STRICT( ArchiveConfig, archive_config )
SCHEMA_CONFORMANCE_STRICT( KeyedConfig, keyed_config )
SCHEMA_CONFORMANCE_STRICT( PackConfig, pack_config )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, RootConfig ),
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, ProfileConfig ),
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, LoadoutConfig ),
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, WideBlob ),
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, ArchiveConfig ),
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, KeyedConfig ),
    SCHEMA_CONFORMANCE_STRICT_ROW( tabledemo, PackConfig )
};

const ConformanceStrict * conformance_stricts_tabledemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
