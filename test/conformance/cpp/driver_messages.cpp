// THE MESSAGES FAMILY of the C++ conformance driver's codec rows (driver.h):
// messagedemo, a union whose arms are tables (docs/SPEC-TABLES.md §2.6).

#include "driver.h"

#include "MessagesTable.h"

static const Codec codecs[] = {
    CODEC( "messagedemo", messagedemo, ToolMessage ),
    CODEC( "messagedemo", messagedemo, Edit ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "messagedemo", messagedemo, ToolEnvelope ),
};

static const MsgCodec msg_codecs[] = {
    MSGCODEC( "messagedemo", messagedemo, ToolMessage ),
};

static const AnnounceUnit announce_units[] = {
    ANNOUNCEUNIT( "messagedemo", messagedemo ),
};

const DriverTables driver_tables_messages = {
    span_of( codecs ), span_of( var_codecs ), span_of( msg_codecs ), {}, {}, span_of( announce_units ), {},
};
