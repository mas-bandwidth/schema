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
SCHEMA_CONFORMANCE_ANNOUNCE_1122
static const ConformanceUnit1122 unit1122 = { "messagedemo", schema_conformance_announce_measure_, schema_conformance_announce_write_,
                                              schema_conformance_announce_read_, NULL, NULL };
const ConformanceUnit1122 * conformance_unit1122_messagedemo( void ) { return &unit1122; }
const ConformanceCodec * conformance_codecs_messagedemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
