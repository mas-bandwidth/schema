#include "MessagesTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE messagedemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER MESSAGEDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( ToolMessage, tool_message )
SCHEMA_CONFORMANCE_CODEC( Edit, edit )
SCHEMA_CONFORMANCE_GRAPH_CODEC( ToolEnvelope, tool_envelope )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( messagedemo, ToolMessage ),
    SCHEMA_CONFORMANCE_ROW( messagedemo, Edit ),
    SCHEMA_CONFORMANCE_GRAPH_ROW( messagedemo, ToolEnvelope )
};
const ConformanceCodec * conformance_codecs_messagedemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
