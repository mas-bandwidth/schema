// THE FIRST TWO VARIABLE-EVOLUTION UNITS of the C++ conformance driver's codec
// rows (driver.h): tblvarevo1 and tblvarevo2, the retain pair among them.

#include "driver.h"

#include "VarEvo1Table.h"
#include "VarEvo2Table.h"

static const Codec codecs[] = {
    CODEC( "tblvarevo1", tblvarevo1, EvoMsg ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "tblvarevo1", tblvarevo1, EvoDoc ),
    VARCODEC( "tblvarevo2", tblvarevo2, EvoDoc ),
};

static const MsgCodec msg_codecs[] = {
    MSGCODEC( "tblvarevo1", tblvarevo1, EvoMsg ),
};

static const RetainCodec retain_codecs[] = {
    RETAINCODEC( "tblvarevo1", tblvarevo1, EvoDoc ),
    RETAINCODEC( "tblvarevo2", tblvarevo2, EvoDoc ),
};

static const RetainSequenceCodec retain_sequence_codecs[] = {
    RETAINSEQUENCE( "tblvarevo1", tblvarevo1, EvoDoc ),
};

const DriverTables driver_tables_varevo12 = {
    span_of( codecs ), span_of( var_codecs ), span_of( msg_codecs ), span_of( retain_codecs ), span_of( retain_sequence_codecs ), {}, {},
};
