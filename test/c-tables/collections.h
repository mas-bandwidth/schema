/* C collection parity against the C++ file-wire goldens and LoadMeasure. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CHECK(x) do { if(!(x)) { fprintf(stderr,"%s:%d: %s\n",name,__LINE__,#x); return 0; } } while(0)
#define COLLECTION_CASE(TYPE,FN,NAME,NEED) \
static int check_##NAME(void) { \
 const char * name=#NAME; FILE * input; uint8_t wire[65536],saved[65536]; size_t bytes; int64_t needed,text_bytes; \
 uint8_t * region; char * text; const TYPE * root; TYPE##Builder builder; TableReport report={0}; \
 input=fopen("testdata/wire/tables/" #NAME ".bin","rb");CHECK(input!=NULL);bytes=fread(wire,1,sizeof(wire),input);fclose(input); \
 needed=FN##_load_measure(wire,(int64_t)bytes);CHECK(needed==NEED); \
 region=(uint8_t *)malloc((size_t)needed);CHECK(region!=NULL); \
 root=FN##_load(region,needed,wire,(int64_t)bytes,&report);CHECK(root!=NULL); \
 CHECK(!report.malformed && !report.unknown && !report.kind_mismatch && !report.clamped && !report.duplicate && !report.widened); \
 CHECK(FN##_save(NULL,root,saved,sizeof(saved))==(int64_t)bytes);CHECK(!memcmp(wire,saved,bytes)); \
 CHECK(FN##_builder_init(&builder));CHECK(FN##_load_builder(&builder,wire,(int64_t)bytes,&report));CHECK(FN##_builder_lock(&builder)); \
 CHECK(FN##_save(NULL,(const TYPE *)(const void *)builder.region,saved,sizeof(saved))==(int64_t)bytes);CHECK(!memcmp(wire,saved,bytes)); \
 FN##_builder_shutdown(&builder); \
 text_bytes=FN##_to_json_measure(root);CHECK(text_bytes>0 && text_bytes<1048576);text=(char *)malloc((size_t)text_bytes+1);CHECK(text!=NULL); \
 CHECK(FN##_to_json(root,text,text_bytes)==text_bytes); \
 CHECK(FN##_builder_init(&builder));CHECK(FN##_from_json(&builder,text,text_bytes,&report)); \
 CHECK(!report.malformed && !report.unknown && !report.kind_mismatch && !report.clamped && !report.duplicate);CHECK(FN##_builder_lock(&builder)); \
 CHECK(FN##_save(NULL,(const TYPE *)(const void *)builder.region,saved,sizeof(saved))==(int64_t)bytes);CHECK(!memcmp(wire,saved,bytes)); \
 FN##_builder_shutdown(&builder);free(text);free(region);return 1; \
}
