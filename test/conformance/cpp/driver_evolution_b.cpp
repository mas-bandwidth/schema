// THE OTHER EVOLUTION PAIRS of the C++ conformance driver's codec rows
// (driver.h): tblk1/tblk2, tblr1/tblr2, tblw1/tblw2 and tblg1.

#include "driver.h"

#include "K1Table.h"
#include "K2Table.h"
#include "R1Table.h"
#include "R2Table.h"
#include "W1Table.h"
#include "W2Table.h"
#include "G1Table.h"

static const Codec codecs[] = {
    CODEC( "tblk1", tblk1, Root ),
    CODEC( "tblk2", tblk2, Root ),
    CODEC( "tblr1", tblr1, Cfg ),
    CODEC( "tblr2", tblr2, Cfg ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "tblg1", tblg1, Guarded ),
    VARCODEC( "tblw1", tblw1, Fleet ),
    VARCODEC( "tblw2", tblw2, Fleet ),
};

const DriverTables driver_tables_evolution_b = {
    span_of( codecs ), span_of( var_codecs ), {}, {}, {}, {}, {},
};
