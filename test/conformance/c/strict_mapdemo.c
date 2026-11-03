/* the mapdemo unit's strict reads (tables/maps). Its codec rows live in
   test/c-tables beside the collections differential; the strict rows live
   here, one translation unit per package as unit.h requires. */
#include "maps/CellsTable.h"
#include "maps/ChunksTable.h"
#include "maps/CrewsTable.h"
#include "maps/DepthTable.h"
#include "maps/DocsTable.h"
#include "maps/FleetTable.h"
#include "maps/PairsTable.h"
#include "maps/RowsTable.h"
#include "maps/RunsTable.h"
#include "maps/SlotsTable.h"
#include "maps/SpansTable.h"
#include "maps/TextTable.h"
#include "maps/TrailsTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE mapdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER MAPDEMO
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_STRICT( Depth, depth )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Text, text )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Cells, cells )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Runs, runs )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Slots, slots )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Spans, spans )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Docs, docs )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Chunks, chunks )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Pairs, pairs )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Crews, crews )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Trails, trails )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Fleet, fleet )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Depth ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Text ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Cells ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Runs ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Slots ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Spans ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Docs ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Chunks ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Pairs ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Crews ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Trails ),
    SCHEMA_CONFORMANCE_STRICT_ROW( mapdemo, Fleet )
};

const ConformanceStrict * conformance_stricts_mapdemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
