#include "collections.h"
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

COLLECTION_CASE(Depth,depth,map_depth,192)
COLLECTION_CASE(Text,text,map_text,216)
COLLECTION_CASE(Cells,cells,map_cells,96)
COLLECTION_CASE(Runs,runs,map_runs,88)
COLLECTION_CASE(Slots,slots,map_slots,64)
COLLECTION_CASE(Spans,spans,map_spans,112)
COLLECTION_CASE(Docs,docs,map_docs,168)
COLLECTION_CASE(Chunks,chunks,map_chunks,136)
COLLECTION_CASE(Pairs,pairs,map_pairs,136)
COLLECTION_CASE(Crews,crews,map_crews,176)
COLLECTION_CASE(Trails,trails,map_trails,168)
COLLECTION_CASE(Fleet,fleet,map_full,640)
COLLECTION_CASE(Fleet,fleet,map_empty,88)

static int check_segmented_map(void) {
 const char * name="segmented_map"; WideRowBuilder builder; WideRow * root; const WideRow * locked; uint32_t i;
 Item * first=NULL; TableCtx ctx; TableSequenceCursor cursor; const WideRowEntriesEntry * entry;
 CHECK(wide_row_builder_init(&builder));root=wide_row_builder_root(&builder);
 for(i=256;i>0;i--) { Item * slot=wide_row_entries_insert(&builder.main,&root->entries,i);CHECK(slot!=NULL);slot->count=(int32_t)i;if(i==256)first=slot; }
 CHECK(first!=NULL && first->count==256);CHECK(wide_row_entries_find_mut(&builder.arena,&root->entries,256)==first);
 CHECK(wide_row_entries_insert(&builder.main,&root->entries,256)==first);CHECK(first->count==0);first->count=256;
 CHECK(wide_row_entries_erase(&builder.arena,&root->entries,33));CHECK(wide_row_entries_erase(&builder.arena,&root->entries,193));
 CHECK(root->entries.count==254);ctx.arena=&builder.arena;cursor=wide_row_entries_each(&ctx,&root->entries);
 entry=(const WideRowEntriesEntry *)table_sequence_next(&cursor);CHECK(entry!=NULL && entry->key==256);
 CHECK(wide_row_builder_lock(&builder));locked=(const WideRow *)(const void *)builder.region;
 cursor=wide_row_entries_each(NULL,&locked->entries);i=0;
 while((entry=(const WideRowEntriesEntry *)table_sequence_next(&cursor))!=NULL) { CHECK(entry->key>i && entry->value.count==(int32_t)entry->key);i=entry->key; }
 CHECK(i==256 && wide_row_entries_find(&locked->entries,33)==NULL && wide_row_entries_find(&locked->entries,193)==NULL);
 wide_row_builder_shutdown(&builder);return 1;
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
