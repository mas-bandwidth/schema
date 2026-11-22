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
/* `dupkey 1` (README, the 2026-11-22 kinds) appends a copy of the ships map's
   first entry, so one key is held twice. The map's entries are copied into
   a larger array of their own with the copy after them, and the map's
   SELF-RELATIVE slot (docs/SPEC-TABLES.md §6.3) is re-aimed there; a ships
   entry is a key and a by-value ShipConfig, relocatable as it stands.
   `dupkey 0` leaves the map as loaded. */
int conformance_write_refusal_set_mapdemo( const char * root, void * value, const char * path,
                                           const char * kind, const char * text )
{
    schema_conformance_holder_Fleet * holder = (schema_conformance_holder_Fleet *) value;
    mapdemo_fleet_t * v;
    const mapdemo_fleet_ships_entry_t * entries;
    mapdemo_fleet_ships_entry_t * moved;
    int32_t count, i;
    if ( strcmp( root, "Fleet" ) != 0 || strcmp( path, "ships" ) != 0 || strcmp( kind, "dupkey" ) != 0 ) { return 0; }
    if ( strcmp( text, "0" ) == 0 ) { return 1; }
    if ( strcmp( text, "1" ) != 0 ) { return 0; }
    v = (mapdemo_fleet_t *) (void *) holder->root;
    count = v->ships.count;
    if ( count < 1 || v->ships.items.value == 0 ) { return 0; }
    entries = (const mapdemo_fleet_ships_entry_t *) (const void *) ( (const uint8_t *) &v->ships.items + v->ships.items.value );
    moved = (mapdemo_fleet_ships_entry_t *) calloc( (size_t) count + 1, sizeof( *moved ) );
    if ( moved == NULL ) { return 0; }
    for ( i = 0; i < count; i++ ) { moved[i] = entries[i]; }
    moved[count] = entries[0];
    v->ships.items.value = (int64_t) ( (uint8_t *) moved - (uint8_t *) &v->ships.items );
    v->ships.count = count + 1;
    return 1;
}
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
