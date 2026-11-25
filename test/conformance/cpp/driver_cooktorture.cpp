// THE COOKTORTURE FAMILY of the C++ conformance driver's codec rows (driver.h):
// the cook torture unit's fixed write-refusal root and its region root.

#include "driver.h"

#include "CookTortureTable.h"

static const Codec codecs[] = {
    CODEC( "cooktorture", cooktorture, TortureFixedWref ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "cooktorture", cooktorture, TortureCook ),
};

const DriverTables driver_tables_cooktorture = {
    span_of( codecs ), span_of( var_codecs ), {}, {}, {}, {}, {},
};
