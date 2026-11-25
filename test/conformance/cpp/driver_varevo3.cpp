// THE THIRD VARIABLE-EVOLUTION UNIT, THE FIXED-EVOLUTION UNIT AND THE PRIOR
// WRITER of the C++ conformance driver's codec rows (driver.h).

#include "driver.h"

#include "VarEvo3Table.h"
#include "FixedEvoTable.h"
#include "PriorNarrowTable.h"

static const Codec codecs[] = {
    CODEC( "tblvarevo3", tblvarevo3, EvoMsg ),
    CODEC( "tblfixedevo", tblfixedevo, EvoRoot ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "tblvarevo3", tblvarevo3, EvoDoc ),
};

static const MsgCodec msg_codecs[] = {
    MSGCODEC( "tblvarevo3", tblvarevo3, EvoMsg ),
    // the prior-writer rows (FORMAT.md `message-prior`): one field per shape a
    // ranged value takes, read against an earlier release's announcement
    MSGCODEC( "tblprior", tblprior, Prior ),
};

static const ReadIntoCodec read_into_codecs[] = {
    READINTO_FIXED( "tblfixedevo", tblfixedevo, EvoRoot ),
};

const DriverTables driver_tables_varevo3 = {
    span_of( codecs ), span_of( var_codecs ), span_of( msg_codecs ), {}, {}, {}, span_of( read_into_codecs ),
};
