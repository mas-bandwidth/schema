#include "collections.h"
#define COLLECTION_REPORT listdemo_table_report_t
#include "HoldersTable.h"
#include "MigrateTable.h"
#include "ReportTable.h"
#include "SaveTable.h"
#include "SharedTable.h"

COLLECTION_CASE(listdemo_save_t,listdemo_save,list_tables,168)
COLLECTION_CASE(listdemo_save_t,listdemo_save,list_scalars,80)
COLLECTION_CASE(listdemo_save_t,listdemo_save,list_empty,88)
COLLECTION_CASE(listdemo_save_t,listdemo_save,list_erased,128)
COLLECTION_CASE(listdemo_mixed_t,listdemo_mixed,list_mixed,152)
COLLECTION_CASE(listdemo_album_t,listdemo_album,list_shared,88)
COLLECTION_CASE(listdemo_album_t,listdemo_album,list_before_pointer,104)
COLLECTION_CASE(listdemo_sheet_t,listdemo_sheet,list_nested,184)
COLLECTION_CASE(listdemo_army_t,listdemo_army,list_of_maps,120)
COLLECTION_CASE(listdemo_unbounded_t,listdemo_unbounded,list_migrates,56)

static int check_large_list(void) {
 const char * name="large_list"; listdemo_bytes_builder_t builder; listdemo_bytes_t * root;
 int32_t i; const listdemo_bytes_t * locked; uint8_t * wire,*region; int64_t size,need; listdemo_table_report_t report={0};
 CHECK(listdemo_bytes_builder_init(&builder));root=listdemo_bytes_builder_root(&builder);
 for(i=0;i<100000;i++) { uint8_t * slot=listdemo_bytes_data_add(&builder.main,&root->data);CHECK(slot!=NULL);*slot=(uint8_t)i; }
 CHECK(root->data.count==100000);CHECK(listdemo_bytes_builder_lock(&builder));locked=(const listdemo_bytes_t *)(const void *)builder.region;
 CHECK(*listdemo_bytes_data_at(&locked->data,99999)==(uint8_t)99999);CHECK(listdemo_bytes_data_at(&locked->data,100000)==NULL);
 size=listdemo_bytes_measure(NULL,locked);CHECK(size>100000);wire=(uint8_t *)malloc((size_t)size);CHECK(wire!=NULL);
 CHECK(listdemo_bytes_save(NULL,locked,wire,size)==size);need=listdemo_bytes_load_measure(wire,size);CHECK(need>100000 && need<101000);
 region=(uint8_t *)malloc((size_t)need);CHECK(region!=NULL);locked=listdemo_bytes_load(region,need,wire,size,&report);
 CHECK(locked!=NULL && !report.malformed && !report.clamped && locked->data.count==100000);
 for(i=0;i<100000;i++) {CHECK(*listdemo_bytes_data_at(&locked->data,i)==(uint8_t)i);}
 free(region);free(wire);listdemo_bytes_builder_shutdown(&builder);return 1;
}

int main(void) {
 if(!check_large_list()) { return 1; }
 if(!check_list_tables()) { return 1; }
 if(!check_list_scalars()) { return 1; }
 if(!check_list_empty()) { return 1; }
 if(!check_list_erased()) { return 1; }
 if(!check_list_mixed()) { return 1; }
 if(!check_list_shared()) { return 1; }
 if(!check_list_before_pointer()) { return 1; }
 if(!check_list_nested()) { return 1; }
 if(!check_list_of_maps()) { return 1; }
 if(!check_list_migrates()) { return 1; }
 puts("C lists: wire, region sizes, builder, lock, and JSON match the C++ goldens"); return 0;
}
