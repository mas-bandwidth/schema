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
#include "unit.h"
SCHEMA_CONFORMANCE_GRAPH_CODEC(Depth,depth)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Text,text)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Cells,cells)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Runs,runs)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Slots,slots)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Spans,spans)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Docs,docs)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Chunks,chunks)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Pairs,pairs)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Crews,crews)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Trails,trails)
SCHEMA_CONFORMANCE_GRAPH_CODEC(Fleet,fleet)
const ConformanceCodec * conformance_codecs_mapdemo(int * count) {
 static const ConformanceCodec codecs[]={
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Depth),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Text),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Cells),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Runs),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Slots),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Spans),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Docs),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Chunks),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Pairs),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Crews),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Trails),
 SCHEMA_CONFORMANCE_GRAPH_ROW(mapdemo,Fleet),
 }; *count=(int)(sizeof(codecs)/sizeof(codecs[0]));return codecs;
}
