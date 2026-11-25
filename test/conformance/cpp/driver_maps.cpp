// THE MAPS FAMILY of the C++ conformance driver's codec rows (driver.h):
// mapdemo (test/tables/maps), every root a region root.

#include "driver.h"

#include "CellsTable.h"
#include "ChunksTable.h"
#include "CrewsTable.h"
#include "DepthTable.h"
#include "DocsTable.h"
#include "FleetTable.h"
#include "PairsTable.h"
#include "RowsTable.h"
#include "RunsTable.h"
#include "SlotsTable.h"
#include "SpansTable.h"
#include "TextTable.h"
#include "TrailsTable.h"

static const VarCodec var_codecs[] = {
    VARCODEC( "mapdemo", mapdemo, Cells ),
    VARCODEC( "mapdemo", mapdemo, Chunks ),
    VARCODEC( "mapdemo", mapdemo, Crews ),
    VARCODEC( "mapdemo", mapdemo, Depth ),
    VARCODEC( "mapdemo", mapdemo, Docs ),
    VARCODEC( "mapdemo", mapdemo, Fleet ),
    VARCODEC( "mapdemo", mapdemo, Pairs ),
    VARCODEC( "mapdemo", mapdemo, Slots ),
    VARCODEC( "mapdemo", mapdemo, Spans ),
    VARCODEC( "mapdemo", mapdemo, Text ),
    VARCODEC( "mapdemo", mapdemo, Trails ),
    VARCODEC( "mapdemo", mapdemo, Runs ),
    VARCODEC( "mapdemo", mapdemo, Row ),
    VARCODEC( "mapdemo", mapdemo, WideRow ),
    VARCODEC( "mapdemo", mapdemo, EdgeRow ),
};

const DriverTables driver_tables_maps = {
    {}, span_of( var_codecs ), {}, {}, {}, {}, {},
};
