/* ONE UNIT's glue, as macros (test/conformance/README.md).
 *
 * Included AFTER that unit's generated headers, so TableReport and the
 * name-first surface are in scope. Every unit's translation unit is these two
 * macros and a list of roots; there is no third thing.
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


/* Variable roots are loaded into driver-owned regions. The generated load
   itself neither allocates nor follows a reference. */
#define SCHEMA_CONFORMANCE_GRAPH_CODEC( TYPE, FN ) \
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
    { #UNIT, #TYPE, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE, schema_conformance_load_measure_##TYPE, schema_conformance_cook_measure_##TYPE, schema_conformance_cook_##TYPE }

#define SCHEMA_CONFORMANCE_ROW( UNIT, TYPE ) \
    { #UNIT, #TYPE, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, \
      schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE, NULL, schema_conformance_cook_measure_##TYPE, schema_conformance_cook_##TYPE }

#endif /* SCHEMA_CONFORMANCE_UNIT_H */
