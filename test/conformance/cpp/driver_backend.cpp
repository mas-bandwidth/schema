// THE MESSAGE FORM'S FAMILY of the C++ conformance driver's codec rows
// (driver.h; docs/SPEC-TABLES.md §3.3): the backend unit, the two
// wide-vocabulary units, the message evolution pair and the two
// announcement-cap units.

#include "driver.h"

#include "BackendTable.h"
#include "VocabTable.h"
#include "Vocab9Table.h"
#include "M1Table.h"
#include "M2Table.h"
#include "announce4096/AnnounceTable.h"
#include "announce4097/AnnounceTable.h"

static const Codec codecs[] = {
    CODEC( "tblm1", tblm1, Msg ),
    CODEC( "tblm2", tblm2, Msg ),
    // their FILE-form vectors are ordinary instances and ride every surface an
    // instance rides
    CODEC( "backenddemo", backenddemo, LoginRequest ),
    CODEC( "backenddemo", backenddemo, MatchResult ),
    CODEC( "backenddemo", backenddemo, StorePurchase ),
    CODEC( "vocabdemo", vocabdemo, Wide00 ),
    CODEC( "vocabdemo", vocabdemo, Wide09 ),
    CODEC( "vocab9demo", vocab9demo, Wide00 ),
    CODEC( "vocab9demo", vocab9demo, Wide19 ),
};

static const MsgCodec msg_codecs[] = {
    MSGCODEC( "backenddemo", backenddemo, LoginRequest ),
    MSGCODEC( "backenddemo", backenddemo, MatchResult ),
    MSGCODEC( "backenddemo", backenddemo, StorePurchase ),
    MSGCODEC( "vocabdemo", vocabdemo, Wide00 ),
    MSGCODEC( "vocabdemo", vocabdemo, Wide09 ),
    MSGCODEC( "vocab9demo", vocab9demo, Wide00 ),
    MSGCODEC( "vocab9demo", vocab9demo, Wide19 ),
};

static const AnnounceUnit announce_units[] = {
    ANNOUNCEUNIT( "tblann4096", announce4096 ),
    ANNOUNCEUNIT( "tblann4097", announce4097 ),
    ANNOUNCEUNIT( "backenddemo", backenddemo ),
};

static const ReadIntoCodec read_into_codecs[] = {
    READINTO_FIXED( "backenddemo", backenddemo, StorePurchase ),
};

const DriverTables driver_tables_backend = {
    span_of( codecs ), {}, span_of( msg_codecs ), {}, {}, span_of( announce_units ), span_of( read_into_codecs ),
};
