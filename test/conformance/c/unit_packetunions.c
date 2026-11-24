#include "PacketUnionsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE tblpacketunions
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER TBLPACKETUNIONS
#include "unit.h"

/* an ARRAY OF PACKET UNIONS (docs/SPEC-TABLES.md §2.6): ShapeDoc is the
 * variable class, ShapeMsg the fixed class and the message form */
SCHEMA_CONFORMANCE_GRAPH_CODEC( ShapeDoc, shape_doc )
SCHEMA_CONFORMANCE_CODEC( ShapeMsg, shape_msg )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_GRAPH_ROW( packetunions, ShapeDoc ),
    SCHEMA_CONFORMANCE_ROW( packetunions, ShapeMsg )
};
const ConformanceCodec * conformance_codecs_packetunions( int * count )
{
    *count = (int) ( sizeof( codecs ) / sizeof( codecs[0] ) );
    return codecs;
}
