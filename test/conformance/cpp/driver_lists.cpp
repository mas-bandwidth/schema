// THE LISTS FAMILY of the C++ conformance driver's codec rows (driver.h):
// listdemo (test/tables/lists), widelist and the value-defaults unit.

#include "driver.h"

#include "SaveTable.h"
#include "HoldersTable.h"
#include "SharedTable.h"
#include "MigrateTable.h"
#include "ReportTable.h"
#include "WidesTable.h"
#include "ValueDefaultsTable.h"

static const Codec codecs[] = {
    CODEC( "listdemo", listdemo, Bounded ),
    CODEC( "tblvaluedefaults", tblvaluedefaults, FixedDefaults ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "listdemo", listdemo, Save ),
    VARCODEC( "listdemo", listdemo, Mixed ),
    VARCODEC( "listdemo", listdemo, Album ),
    VARCODEC( "listdemo", listdemo, Sheet ),
    VARCODEC( "listdemo", listdemo, Army ),
    VARCODEC( "listdemo", listdemo, Unbounded ),
    VARCODEC( "listdemo", listdemo, Ints ),
    VARCODEC( "listdemo", listdemo, Squad ),
    VARCODEC( "widelist", widelistdemo, Wide ),
    VARCODEC( "tblvaluedefaults", tblvaluedefaults, VarDefaults ),
};

const DriverTables driver_tables_lists = {
    span_of( codecs ), span_of( var_codecs ), {}, {}, {}, {}, {},
};
