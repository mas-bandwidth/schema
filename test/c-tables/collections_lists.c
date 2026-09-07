#include "collections.h"
#include "HoldersTable.h"
#include "MigrateTable.h"
#include "ReportTable.h"
#include "SaveTable.h"
#include "SharedTable.h"

COLLECTION_CASE(Save,save,list_tables,168)
COLLECTION_CASE(Save,save,list_scalars,80)
COLLECTION_CASE(Save,save,list_empty,88)
COLLECTION_CASE(Save,save,list_erased,128)
COLLECTION_CASE(Mixed,mixed,list_mixed,152)
COLLECTION_CASE(Album,album,list_shared,88)
COLLECTION_CASE(Album,album,list_before_pointer,104)
COLLECTION_CASE(Sheet,sheet,list_nested,184)
COLLECTION_CASE(Army,army,list_of_maps,120)
COLLECTION_CASE(Unbounded,unbounded,list_migrates,56)

static int check_large_list(void) {
 const char * name="large_list"; BytesBuilder builder; Bytes * root;
 int32_t i; const Bytes * locked; uint8_t * wire,*region; int64_t size,need; TableReport report={0};
 CHECK(bytes_builder_init(&builder));root=bytes_builder_root(&builder);
 for(i=0;i<100000;i++) { uint8_t * slot=bytes_data_add(&builder.main,&root->data);CHECK(slot!=NULL);*slot=(uint8_t)i; }
 CHECK(root->data.count==100000);CHECK(bytes_builder_lock(&builder));locked=(const Bytes *)(const void *)builder.region;
 CHECK(*bytes_data_at(&locked->data,99999)==(uint8_t)99999);CHECK(bytes_data_at(&locked->data,100000)==NULL);
 size=bytes_measure(NULL,locked);CHECK(size>100000);wire=(uint8_t *)malloc((size_t)size);CHECK(wire!=NULL);
 CHECK(bytes_save(NULL,locked,wire,size)==size);need=bytes_load_measure(wire,size);CHECK(need>100000 && need<101000);
 region=(uint8_t *)malloc((size_t)need);CHECK(region!=NULL);locked=bytes_load(region,need,wire,size,&report);
 CHECK(locked!=NULL && !report.malformed && !report.clamped && locked->data.count==100000);
 for(i=0;i<100000;i++) {CHECK(*bytes_data_at(&locked->data,i)==(uint8_t)i);}
 free(region);free(wire);bytes_builder_shutdown(&builder);return 1;
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
