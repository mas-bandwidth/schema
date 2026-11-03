#include "MessagesTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE messagedemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER MESSAGEDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( ToolMessage, tool_message )
SCHEMA_CONFORMANCE_CODEC( Edit, edit )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( messagedemo, ToolMessage ),
    SCHEMA_CONFORMANCE_ROW( messagedemo, Edit )
};
const ConformanceCodec * conformance_codecs_messagedemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}

/* the strict read of each root (docs/SPEC-TABLES.md §6.5) */
SCHEMA_CONFORMANCE_STRICT( ToolMessage, tool_message )
SCHEMA_CONFORMANCE_STRICT( Edit, edit )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( messagedemo, ToolMessage ),
    SCHEMA_CONFORMANCE_STRICT_ROW( messagedemo, Edit )
};

const ConformanceStrict * conformance_stricts_messagedemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
