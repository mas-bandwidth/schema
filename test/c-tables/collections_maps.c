#include "collections.h"
#define COLLECTION_REPORT mapdemo_table_report_t
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

COLLECTION_CASE(mapdemo_depth_t,mapdemo_depth,map_depth,192)
COLLECTION_CASE(mapdemo_text_t,mapdemo_text,map_text,216)
COLLECTION_CASE(mapdemo_cells_t,mapdemo_cells,map_cells,96)
COLLECTION_CASE(mapdemo_runs_t,mapdemo_runs,map_runs,88)
COLLECTION_CASE(mapdemo_slots_t,mapdemo_slots,map_slots,64)
COLLECTION_CASE(mapdemo_spans_t,mapdemo_spans,map_spans,112)
COLLECTION_CASE(mapdemo_docs_t,mapdemo_docs,map_docs,168)
COLLECTION_CASE(mapdemo_chunks_t,mapdemo_chunks,map_chunks,136)
COLLECTION_CASE(mapdemo_pairs_t,mapdemo_pairs,map_pairs,136)
COLLECTION_CASE(mapdemo_crews_t,mapdemo_crews,map_crews,176)
COLLECTION_CASE(mapdemo_trails_t,mapdemo_trails,map_trails,168)
COLLECTION_CASE(mapdemo_fleet_t,mapdemo_fleet,map_full,640)
COLLECTION_CASE(mapdemo_fleet_t,mapdemo_fleet,map_empty,88)

static int check_segmented_map(void) {
 const char * name="segmented_map"; mapdemo_wide_row_builder_t builder; mapdemo_wide_row_t * root; const mapdemo_wide_row_t * locked; uint32_t i;
 mapdemo_item_t * first=NULL; mapdemo_table_ctx_t ctx; mapdemo_table_sequence_cursor_t cursor; const mapdemo_wide_row_entries_entry_t * entry;
 CHECK(mapdemo_wide_row_builder_init(&builder));root=mapdemo_wide_row_builder_root(&builder);
 for(i=256;i>0;i--) { mapdemo_item_t * slot=mapdemo_wide_row_entries_insert(&builder.main,&root->entries,i);CHECK(slot!=NULL);slot->count=(int32_t)i;if(i==256)first=slot; }
 CHECK(first!=NULL && first->count==256);CHECK(mapdemo_wide_row_entries_find_mut(&builder.arena,&root->entries,256)==first);
 CHECK(mapdemo_wide_row_entries_insert(&builder.main,&root->entries,256)==first);CHECK(first->count==0);first->count=256;
 CHECK(mapdemo_wide_row_entries_erase(&builder.arena,&root->entries,33));CHECK(mapdemo_wide_row_entries_erase(&builder.arena,&root->entries,193));
 CHECK(root->entries.count==254);ctx.arena=&builder.arena;cursor=mapdemo_wide_row_entries_each(&ctx,&root->entries);
 entry=(const mapdemo_wide_row_entries_entry_t *)mapdemo_table_sequence_next(&cursor);CHECK(entry!=NULL && entry->key==256);
 CHECK(mapdemo_wide_row_builder_lock(&builder));locked=(const mapdemo_wide_row_t *)(const void *)builder.region;
 cursor=mapdemo_wide_row_entries_each(NULL,&locked->entries);i=0;
 while((entry=(const mapdemo_wide_row_entries_entry_t *)mapdemo_table_sequence_next(&cursor))!=NULL) { CHECK(entry->key>i && entry->value.count==(int32_t)entry->key);i=entry->key; }
 CHECK(i==256 && mapdemo_wide_row_entries_find(&locked->entries,33)==NULL && mapdemo_wide_row_entries_find(&locked->entries,193)==NULL);
 mapdemo_wide_row_builder_shutdown(&builder);return 1;
}

int main(void) {
 if(!check_segmented_map()) { return 1; }
 if(!check_map_depth()) { return 1; }
 if(!check_map_text()) { return 1; }
 if(!check_map_cells()) { return 1; }
 if(!check_map_runs()) { return 1; }
 if(!check_map_slots()) { return 1; }
 if(!check_map_spans()) { return 1; }
 if(!check_map_docs()) { return 1; }
 if(!check_map_chunks()) { return 1; }
 if(!check_map_pairs()) { return 1; }
 if(!check_map_crews()) { return 1; }
 if(!check_map_trails()) { return 1; }
 if(!check_map_full()) { return 1; }
 if(!check_map_empty()) { return 1; }
 puts("C maps: wire, region sizes, builder, lock, and JSON match the C++ goldens"); return 0;
}
