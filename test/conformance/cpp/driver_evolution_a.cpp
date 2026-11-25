// THE FIRST EVOLUTION PAIRS of the C++ conformance driver's codec rows
// (driver.h): tblv1/tblv2, the pointered evolution units tblp1/tblp2/tblp3 and
// tbla1/tbla2.

#include "driver.h"

#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P2Table.h"
#include "P3Table.h"
#include "A1Table.h"
#include "A2Table.h"

static const Codec codecs[] = {
    CODEC( "tblv1", tblv1, Cfg ),
    CODEC( "tblv2", tblv2, Cfg ),
    CODEC( "tblp1", tblp1, Chain ),
    CODEC( "tblp3", tblp3, Chain ),
    CODEC( "tbla1", tbla1, Root ),
    CODEC( "tbla2", tbla2, Root ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "tblp2", tblp2, Chain ),
};

const DriverTables driver_tables_evolution_a = {
    span_of( codecs ), span_of( var_codecs ), {}, {}, {}, {}, {},
};
