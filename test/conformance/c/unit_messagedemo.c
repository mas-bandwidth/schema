#include "MessagesTable.h"
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
