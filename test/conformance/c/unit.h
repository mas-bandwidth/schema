/* ONE UNIT's glue, as macros (test/conformance/README.md).
 *
 * Included AFTER that unit's generated headers, so TableReport and the
 * name-first surface are in scope. Every unit's translation unit is these two
 * macros and a list of batch_roots; there is no third thing.
 *
 * SCHEMA_CONFORMANCE_CODEC erases one root behind the driver's function
 * pointers, and SCHEMA_CONFORMANCE_UNIT hands the table back through the one
 * external name that unit exports.
 *
 * IT TAKES THE TYPE AND THE FUNCTION BASE SEPARATELY because in C they are
 * spelled differently: a type is PascalCase and a function is snake_case, the
 * packet emitter's convention and so this backend's. Token pasting cannot
 * change case, so a macro that derived one from the other would be a macro
 * that forced the two spellings to be the same. */

#ifndef SCHEMA_CONFORMANCE_UNIT_H
#define SCHEMA_CONFORMANCE_UNIT_H

#include "driver.h"

#define SCHEMA_CONFORMANCE_COPY_REPORT( from, to ) \
    do { \
        ( to )->unknown = ( from ).unknown; \
        ( to )->kind_mismatch = ( from ).kind_mismatch; \
        ( to )->clamped = ( from ).clamped; \
        ( to )->duplicate = ( from ).duplicate; \
        ( to )->malformed = ( from ).malformed; \
        ( to )->refused = ( from ).refused; \
        ( to )->widened = ( from ).widened; \
    } while ( 0 )

#define SCHEMA_CONFORMANCE_CODEC( TYPE, FN ) \
    static int64_t schema_conformance_message_fuzz_##TYPE(const uint8_t * wire,int64_t bytes,uint8_t ** saved,ConformanceReport * report,int * loaded,int64_t * extent) \
    { \
     static TableMessageEntry entries[kTableMessageEntriesHere];static TableVocabulary vocabulary; \
     TableReport inner={0},ignored={0};int64_t count=256,size=-1;int ok; \
     if(!vocabulary.announced){vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);if(!announce_read(&vocabulary,kTableAnnounce,kTableAnnounceBytes,&inner))return -1;} \
    static TYPE values[256];int i; \
     for(i=0;i<256;i++)FN##_reset(values+i); \
     ok=FN##_load_messages(values,&count,&vocabulary,wire,bytes,&inner);*extent=-1;*loaded=1; \
     if(!ok&&!inner.refused)inner.malformed=1; \
     size=FN##_measure_messages(values,1,&ignored); \
     if(size>=0){*saved=(uint8_t *)malloc((size_t)size);if(!*saved)size=-1;else size=FN##_save_messages(values,1,*saved,size,&ignored);} \
    SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return size; \
    } \
    static int schema_conformance_message_##TYPE(const uint8_t * a,int64_t an,const uint8_t * b,int64_t n,uint8_t ** out,int64_t * size,ConformanceReport * report) \
    { \
     TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere); \
     TableReport inner={0};int64_t count=1;int ok=0;TYPE value; \
     if(!announce_read(&v,a,an,&inner))goto done; \
     if(!FN##_load_messages(&value,&count,&v,b,n,&inner))goto done; \
     *size=FN##_measure_messages(&value,count,&inner);if(*size<0)goto done; \
     *out=(uint8_t *)malloc((size_t)*size);if(!*out)goto done; \
     ok=FN##_save_messages(&value,count,*out,*size,&inner)==*size; \
     done:;SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return ok; \
    } \
    static TYPE schema_conformance_storage_##TYPE; \
    static void * schema_conformance_make_##TYPE( void ) \
    { \
        memset( &schema_conformance_storage_##TYPE, 0, sizeof( schema_conformance_storage_##TYPE ) ); \
        FN##_reset( &schema_conformance_storage_##TYPE ); \
        return &schema_conformance_storage_##TYPE; \
    } \
    static int schema_conformance_load_##TYPE( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report ) \
    { \
        TableReport inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = FN##_load( (TYPE *) value, bytes, size, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    static int64_t schema_conformance_measure_##TYPE( const void * value ) \
    { \
        return FN##_measure( (const TYPE *) value ); \
    } \
    static int64_t schema_conformance_save_##TYPE( const void * value, uint8_t * buffer, int64_t capacity ) \
    { \
        return FN##_save( (const TYPE *) value, buffer, capacity ); \
    } \
    static int64_t schema_conformance_cook_measure_##TYPE(const void * value) \
    { return FN##_cook_measure((const TYPE *)value); } \
    static int schema_conformance_cook_##TYPE(const void * value,void * buffer,uint64_t capacity,int big) \
    { return FN##_cook((const TYPE *)value,buffer,capacity,big ? TableByteOrder_Big : TableByteOrder_Little); } \
    static int schema_conformance_from_json_##TYPE( void * value, const char * text, int64_t bytes, ConformanceReport * report ) \
    { \
        TableReport inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = FN##_from_json( (TYPE *) value, text, bytes, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    static int64_t schema_conformance_to_json_##TYPE( const void * value, char * buffer, int64_t capacity ) \
    { \
        return buffer != NULL ? FN##_to_json( (const TYPE *) value, buffer, capacity ) \
                              : FN##_to_json_measure( (const TYPE *) value ); \
    }


/* Variable batch_roots are loaded into driver-owned regions. The generated load
   itself neither allocates nor follows a reference. */
#define SCHEMA_CONFORMANCE_GRAPH_CODEC( TYPE, FN ) \
    static int64_t schema_conformance_retain_fuzz_##TYPE(const uint8_t * wire,int64_t bytes,uint8_t ** saved,ConformanceReport * report,int * loaded,int64_t * extent) \
    { \
     static uint8_t store[1<<20];static TableRetainId ids[(1<<20)/8];TableRetain retain={0};TableReport inner={0};uint8_t * region;const TYPE * root;int64_t size=-1; \
     retain.bytes=store;retain.capacity=sizeof(store);retain.ids=ids;retain.id_capacity=(1<<20)/8;*extent=FN##_load_measure(wire,bytes);region=(uint8_t *)malloc((size_t)(*extent>0?*extent:1));if(!region)return -1; \
     root=FN##_load_retain(region,*extent,wire,bytes,&retain,&inner);*loaded=root!=NULL;if(root)size=FN##_measure_retain(root,&retain); \
     if(size>=0){*saved=(uint8_t *)malloc((size_t)(size?size:1));if(!*saved)size=-1;else size=FN##_save_retain(root,&retain,*saved,size,&inner);}free(region);SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);report->retained=inner.retained;report->retain_lost=inner.retain_lost;return size; \
    } \
    static int64_t schema_conformance_message_fuzz_##TYPE(const uint8_t * wire,int64_t bytes,uint8_t ** saved,ConformanceReport * report,int * loaded,int64_t * extent) \
    { \
     static TableMessageEntry entries[kTableMessageEntriesHere];static TableVocabulary vocabulary; \
     TableReport inner={0},ignored={0};int64_t count=256,size=-1;int ok; \
     if(!vocabulary.announced){vocabulary=table_vocabulary(entries,kTableMessageEntriesHere);if(!announce_read(&vocabulary,kTableAnnounce,kTableAnnounceBytes,&inner))return -1;} \
    const TYPE * batch_roots[256]={NULL};uint8_t * region; \
     *extent=FN##_load_measure_messages(&vocabulary,wire,bytes);region=(uint8_t *)malloc((size_t)(*extent>0?*extent:1));if(!region)return -1; \
     ok=FN##_load_messages(batch_roots,&count,region,*extent,&vocabulary,wire,bytes,&inner);(void)ok;*loaded=batch_roots[0]!=NULL; \
     if(*loaded)size=FN##_measure_messages(batch_roots,1,&ignored); \
     if(size>=0){*saved=(uint8_t *)malloc((size_t)size);if(!*saved)size=-1;else size=FN##_save_messages(batch_roots,1,*saved,size,&ignored);} \
     free(region); \
    SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return size; \
    } \
    static int schema_conformance_message_##TYPE(const uint8_t * a,int64_t an,const uint8_t * b,int64_t n,uint8_t ** out,int64_t * size,ConformanceReport * report) \
    { \
     TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere); \
     TableReport inner={0};int64_t count=1;int ok=0;const TYPE * batch_roots[1]={NULL};uint8_t * region=NULL;int64_t need; \
     if(!announce_read(&v,a,an,&inner))goto done; \
     need=FN##_load_measure_messages(&v,b,n);if(need<0)goto done;region=(uint8_t *)malloc((size_t)need);if(!region)goto done; \
     if(!FN##_load_messages(batch_roots,&count,region,need,&v,b,n,&inner))goto done; \
     *size=FN##_measure_messages(batch_roots,count,&inner);if(*size<0)goto done; \
     *out=(uint8_t *)malloc((size_t)*size);if(!*out)goto done; \
     ok=FN##_save_messages(batch_roots,count,*out,*size,&inner)==*size; \
     done:free(region);SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return ok; \
    } \
    typedef struct { TYPE empty; const TYPE * root; uint8_t * region; } schema_conformance_holder_##TYPE; \
    static schema_conformance_holder_##TYPE schema_conformance_storage_##TYPE; \
    static void * schema_conformance_make_##TYPE(void) \
    { \
        schema_conformance_holder_##TYPE * holder=&schema_conformance_storage_##TYPE; \
        free(holder->region); holder->region=NULL; FN##_reset(&holder->empty); holder->root=&holder->empty; \
        return holder; \
    } \
    static int schema_conformance_load_##TYPE(void * value,const uint8_t * bytes,int64_t size,ConformanceReport * report) \
    { \
        schema_conformance_holder_##TYPE * holder=(schema_conformance_holder_##TYPE *)value; \
        TableReport inner; int64_t extent=FN##_load_measure(bytes,size); const TYPE * root; \
        memset(&inner,0,sizeof(inner)); \
        if(extent>0) { holder->region=(uint8_t *)malloc((size_t)extent); } \
        root=FN##_load(holder->region,extent,bytes,size,&inner); \
        if(root!=NULL) { holder->root=root; } \
        SCHEMA_CONFORMANCE_COPY_REPORT(inner,report); return root!=NULL; \
    } \
    static int64_t schema_conformance_load_measure_##TYPE(const uint8_t * wire,int64_t bytes) \
    { return FN##_load_measure(wire,bytes); } \
    static int64_t schema_conformance_measure_##TYPE(const void * value) \
    { return FN##_measure(NULL,((const schema_conformance_holder_##TYPE *)value)->root); } \
    static int64_t schema_conformance_save_##TYPE(const void * value,uint8_t * buffer,int64_t capacity) \
    { return FN##_save(NULL,((const schema_conformance_holder_##TYPE *)value)->root,buffer,capacity); } \
    static int64_t schema_conformance_cook_measure_##TYPE(const void * value) \
    { return FN##_cook_measure(NULL,((const schema_conformance_holder_##TYPE *)value)->root); } \
    static int schema_conformance_cook_##TYPE(const void * value,void * buffer,uint64_t capacity,int big) \
    { return FN##_cook(NULL,((const schema_conformance_holder_##TYPE *)value)->root,buffer,capacity,big ? TableByteOrder_Big : TableByteOrder_Little); } \
    static int schema_conformance_from_json_##TYPE(void * value,const char * text,int64_t bytes,ConformanceReport * report) \
    { \
        schema_conformance_holder_##TYPE * holder=(schema_conformance_holder_##TYPE *)value; \
        TYPE##Builder builder; TableReport inner; int ok; \
        memset(&inner,0,sizeof(inner)); \
        if(!FN##_builder_init(&builder)) { FN##_builder_shutdown(&builder); return 0; } \
        ok=FN##_from_json(&builder,text,bytes,&inner); \
        if(ok) { ok=FN##_builder_lock(&builder); } \
        if(ok) { free(holder->region); holder->region=builder.region; holder->root=(const TYPE *)(const void *)builder.region; builder.region=NULL; } \
        FN##_builder_shutdown(&builder); SCHEMA_CONFORMANCE_COPY_REPORT(inner,report); return ok; \
    } \
    static int64_t schema_conformance_to_json_##TYPE(const void * value,char * buffer,int64_t capacity) \
    { return FN##_to_json(((const schema_conformance_holder_##TYPE *)value)->root,buffer,capacity); }

#define SCHEMA_CONFORMANCE_GRAPH_ROW( UNIT, TYPE ) \
    { #UNIT, #TYPE, schema_conformance_message_fuzz_##TYPE, schema_conformance_message_##TYPE, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE, schema_conformance_load_measure_##TYPE, schema_conformance_cook_measure_##TYPE, schema_conformance_cook_##TYPE, schema_conformance_retain_fuzz_##TYPE }

#define SCHEMA_CONFORMANCE_ROW( UNIT, TYPE ) \
    { #UNIT, #TYPE, schema_conformance_message_fuzz_##TYPE, schema_conformance_message_##TYPE, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, \
      schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE, NULL, schema_conformance_cook_measure_##TYPE, schema_conformance_cook_##TYPE, NULL }

#endif /* SCHEMA_CONFORMANCE_UNIT_H */
