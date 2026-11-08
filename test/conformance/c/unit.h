/* ONE UNIT's glue, as macros (test/conformance/README.md).
 *
 * Included AFTER that unit's generated headers, so its report type and the
 * name-first surface are in scope. Every unit's translation unit is these two
 * macros and a list of batch_roots; there is no third thing.
 *
 * SCHEMA_CONFORMANCE_CODEC erases one root behind the driver's function
 * pointers, and SCHEMA_CONFORMANCE_UNIT hands the table back through the one
 * external name that unit exports.
 *
 * IT TAKES THE SCHEMA NAME AND THE FUNCTION BASE SEPARATELY: the schema name
 * (RootConfig) is what the manifest rows are keyed by, and token pasting cannot
 * change case. Every C spelling is the function base under the unit's package
 * (SPEC §6.3): the type is <package>_<base>_t, a function <package>_<base>_<verb>,
 * and the runtime's own names take the same prefix. Each unit's file defines
 * SCHEMA_CONFORMANCE_PACKAGE (and its upper-case twin) before including this. */

#ifndef SCHEMA_CONFORMANCE_UNIT_H
#define SCHEMA_CONFORMANCE_UNIT_H

#include "driver.h"

#define SCHEMA_CONFORMANCE_CAT_( a, b ) a##_##b
#define SCHEMA_CONFORMANCE_CAT( a, b ) SCHEMA_CONFORMANCE_CAT_( a, b )
/* a name in this unit's package, and a constant in it */
#define SCHEMA_CONFORMANCE_Q( name ) SCHEMA_CONFORMANCE_CAT( SCHEMA_CONFORMANCE_PACKAGE, name )
#define SCHEMA_CONFORMANCE_QU( name ) SCHEMA_CONFORMANCE_CAT( SCHEMA_CONFORMANCE_PACKAGE_UPPER, name )

/* THE READ NAME (docs/SPEC-TABLES.md §6.5): the plain name is the one read,
   which returns the whole value or refuses the whole load. */
#define SCHEMA_CONFORMANCE_READ( verb ) verb

#define SCHEMA_CONFORMANCE_COPY_REPORT( from, to ) \
    do { \
        ( to )->unknown = ( from ).unknown; \
        ( to )->refused = ( from ).refused; \
        ( to )->widened = ( from ).widened; \
        ( to )->blob_shared = ( from ).blob_shared; \
    } while ( 0 )

/* THE FILE READ AND WRITE OF A FIXED ROOT (docs/SPEC-TABLES.md §3.4,
   test/conformance/README.md): a fixed root's file is form 3, read by
   <base>_fixed_load, which refuses a form-1 file as previous_form, and written
   by <base>_fixed_save. A fixed root past the 65536-byte ceiling has no file
   form and keeps form 1 through the plain names. */
#define SCHEMA_CONFORMANCE_FILE_FIXED( TYPE, FN ) \
    static int schema_conformance_load_##TYPE( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report ) \
    { \
        static SCHEMA_CONFORMANCE_Q(table_fixed_entry_t) plan[4096]; \
        SCHEMA_CONFORMANCE_Q(table_report_t) inner; \
        int64_t n; \
        memset( &inner, 0, sizeof( inner ) ); \
        n = SCHEMA_CONFORMANCE_Q(FN##_fixed_load)( (SCHEMA_CONFORMANCE_Q(FN##_t) *) value, 1, bytes, size, plan, 4096, NULL, &inner ); \
        if ( n != 1 ) { inner.refused = 1; SCHEMA_CONFORMANCE_Q(FN##_reset)( (SCHEMA_CONFORMANCE_Q(FN##_t) *) value ); } \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return n == 1; \
    } \
    static int64_t schema_conformance_measure_##TYPE( const void * value ) \
    { \
        (void) value; \
        return SCHEMA_CONFORMANCE_Q(FN##_fixed_measure)( 1 ); \
    } \
    static int64_t schema_conformance_save_##TYPE( const void * value, uint8_t * buffer, int64_t capacity ) \
    { \
        return SCHEMA_CONFORMANCE_Q(FN##_fixed_save)( (const SCHEMA_CONFORMANCE_Q(FN##_t) *) value, 1, buffer, capacity ); \
    }

#define SCHEMA_CONFORMANCE_FILE_FORM1( TYPE, FN ) \
    static int schema_conformance_load_##TYPE( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report ) \
    { \
        SCHEMA_CONFORMANCE_Q(table_report_t) inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = SCHEMA_CONFORMANCE_Q(SCHEMA_CONFORMANCE_READ(FN##_load))( (SCHEMA_CONFORMANCE_Q(FN##_t) *) value, bytes, size, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    static int64_t schema_conformance_measure_##TYPE( const void * value ) \
    { \
        return SCHEMA_CONFORMANCE_Q(FN##_measure)( (const SCHEMA_CONFORMANCE_Q(FN##_t) *) value ); \
    } \
    static int64_t schema_conformance_save_##TYPE( const void * value, uint8_t * buffer, int64_t capacity ) \
    { \
        return SCHEMA_CONFORMANCE_Q(FN##_save)( (const SCHEMA_CONFORMANCE_Q(FN##_t) *) value, buffer, capacity ); \
    }

#define SCHEMA_CONFORMANCE_CODEC_IN( TYPE, FN, FORM ) \
    static int64_t schema_conformance_message_fuzz_##TYPE(const uint8_t * wire,int64_t bytes,uint8_t ** saved,ConformanceReport * report,int * loaded,int64_t * extent) \
    { \
     static SCHEMA_CONFORMANCE_Q(table_message_entry_t) entries[SCHEMA_CONFORMANCE_QU(K_TABLE_MESSAGE_ENTRIES_HERE)];static SCHEMA_CONFORMANCE_Q(table_vocabulary_t) vocabulary; \
     SCHEMA_CONFORMANCE_Q(table_report_t) inner={0},ignored={0};int64_t count=256,size=-1;int ok; \
     if(!vocabulary.announced){vocabulary=SCHEMA_CONFORMANCE_Q(table_vocabulary)(entries,SCHEMA_CONFORMANCE_QU(K_TABLE_MESSAGE_ENTRIES_HERE));if(!SCHEMA_CONFORMANCE_Q(announce_read)(&vocabulary,SCHEMA_CONFORMANCE_QU(K_TABLE_ANNOUNCE),SCHEMA_CONFORMANCE_QU(K_TABLE_ANNOUNCE_BYTES),&inner))return -1;} \
    static SCHEMA_CONFORMANCE_Q(FN##_t) values[256];int i; \
     for(i=0;i<256;i++)SCHEMA_CONFORMANCE_Q(FN##_reset)(values+i); \
     ok=SCHEMA_CONFORMANCE_Q(SCHEMA_CONFORMANCE_READ(FN##_load_messages))(values,&count,&vocabulary,wire,bytes,&inner);*extent=-1;*loaded=ok; \
     if(!ok){inner.refused=1;SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return -1;} \
     size=SCHEMA_CONFORMANCE_Q(FN##_measure_messages)(values,count,&ignored); \
     if(size>=0){*saved=(uint8_t *)malloc((size_t)(size?size:1));if(!*saved)size=-1;else size=SCHEMA_CONFORMANCE_Q(FN##_save_messages)(values,count,*saved,size,&ignored);} \
    SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return size; \
    } \
    static int schema_conformance_message_##TYPE(const uint8_t * a,int64_t an,const uint8_t * b,int64_t n,uint8_t ** out,int64_t * size,ConformanceReport * report) \
    { \
     SCHEMA_CONFORMANCE_Q(table_message_entry_t) entries[SCHEMA_CONFORMANCE_QU(K_TABLE_MESSAGE_ENTRIES_HERE)];SCHEMA_CONFORMANCE_Q(table_vocabulary_t) v=SCHEMA_CONFORMANCE_Q(table_vocabulary)(entries,SCHEMA_CONFORMANCE_QU(K_TABLE_MESSAGE_ENTRIES_HERE)); \
     SCHEMA_CONFORMANCE_Q(table_report_t) inner={0};int64_t count=1;int ok=0;SCHEMA_CONFORMANCE_Q(FN##_t) value; \
     if(!SCHEMA_CONFORMANCE_Q(announce_read)(&v,a,an,&inner))goto done; \
     if(!SCHEMA_CONFORMANCE_Q(SCHEMA_CONFORMANCE_READ(FN##_load_messages))(&value,&count,&v,b,n,&inner))goto done; \
     *size=SCHEMA_CONFORMANCE_Q(FN##_measure_messages)(&value,count,&inner);if(*size<0)goto done; \
     *out=(uint8_t *)malloc((size_t)*size);if(!*out)goto done; \
     ok=SCHEMA_CONFORMANCE_Q(FN##_save_messages)(&value,count,*out,*size,&inner)==*size; \
     done:;SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);return ok; \
    } \
    static SCHEMA_CONFORMANCE_Q(FN##_t) schema_conformance_storage_##TYPE; \
    static void * schema_conformance_make_##TYPE( void ) \
    { \
        memset( &schema_conformance_storage_##TYPE, 0, sizeof( schema_conformance_storage_##TYPE ) ); \
        SCHEMA_CONFORMANCE_Q(FN##_reset)( &schema_conformance_storage_##TYPE ); \
        return &schema_conformance_storage_##TYPE; \
    } \
    SCHEMA_CONFORMANCE_FILE_##FORM( TYPE, FN ) \
    static int schema_conformance_load_variable_##TYPE( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report ) \
    { \
        SCHEMA_CONFORMANCE_Q(table_report_t) inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = SCHEMA_CONFORMANCE_Q(FN##_load)( (SCHEMA_CONFORMANCE_Q(FN##_t) *) value, bytes, size, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    static int64_t schema_conformance_cook_measure_##TYPE(const void * value) \
    { return SCHEMA_CONFORMANCE_Q(FN##_cook_measure)((const SCHEMA_CONFORMANCE_Q(FN##_t) *)value); } \
    static int schema_conformance_cook_##TYPE(const void * value,void * buffer,uint64_t capacity,int big) \
    { return SCHEMA_CONFORMANCE_Q(FN##_cook)((const SCHEMA_CONFORMANCE_Q(FN##_t) *)value,buffer,capacity,big ? SCHEMA_CONFORMANCE_QU(TABLE_BYTE_ORDER_BIG) : SCHEMA_CONFORMANCE_QU(TABLE_BYTE_ORDER_LITTLE)); } \
    static int schema_conformance_from_json_##TYPE( void * value, const char * text, int64_t bytes, ConformanceReport * report ) \
    { \
        SCHEMA_CONFORMANCE_Q(table_report_t) inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = SCHEMA_CONFORMANCE_Q(SCHEMA_CONFORMANCE_READ(FN##_from_json))( (SCHEMA_CONFORMANCE_Q(FN##_t) *) value, text, bytes, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    /* DAMAGE THE WRITER MUST REFUSE (docs/SPEC-TABLES.md §16.14 W0), put into \
       a top-level text field in code because no read delivers it: the bytes \
       61 FF 62 in a narrow field, the units 0061 D800 0062 in a wide one, \
       reached through the root's descriptor by name, offset and length */ \
    static int schema_conformance_damage_##TYPE( void * value, const char * field, int wide ) \
    { \
        const SCHEMA_CONFORMANCE_Q(table_type_info_t) * info = SCHEMA_CONFORMANCE_Q(FN##_table_type)(); \
        int32_t i; \
        for ( i = 0; i < info->num_fields; i++ ) \
        { \
            const SCHEMA_CONFORMANCE_Q(table_field_info_t) * f = &info->fields[i]; \
            const int32_t length = 3; \
            static const uint8_t bytes[3] = { 0x61, 0xFF, 0x62 }; \
            static const uint16_t units[3] = { 0x0061, 0xD800, 0x0062 }; \
            if ( strcmp( f->name, field ) != 0 ) { continue; } \
            if ( !f->counted || f->count_offset == 0xffffffffu || f->array_bound < 3 ) { return 0; } \
            if ( wide ) { memcpy( (uint8_t *) value + f->offset, units, sizeof( units ) ); } \
            else { memcpy( (uint8_t *) value + f->offset, bytes, sizeof( bytes ) ); } \
            memcpy( (uint8_t *) value + f->count_offset, &length, sizeof( length ) ); \
            return 1; \
        } \
        return 0; \
    } \
    static int64_t schema_conformance_to_json_##TYPE( const void * value, char * buffer, int64_t capacity ) \
    { \
        return buffer != NULL ? SCHEMA_CONFORMANCE_Q(FN##_to_json)( (const SCHEMA_CONFORMANCE_Q(FN##_t) *) value, buffer, capacity ) \
                              : SCHEMA_CONFORMANCE_Q(FN##_to_json_measure)( (const SCHEMA_CONFORMANCE_Q(FN##_t) *) value ); \
    }


/* a fixed root with a file form, and one past the ceiling with none */
#define SCHEMA_CONFORMANCE_CODEC( TYPE, FN ) SCHEMA_CONFORMANCE_CODEC_IN( TYPE, FN, FIXED )
#define SCHEMA_CONFORMANCE_CODEC_FORM1( TYPE, FN ) SCHEMA_CONFORMANCE_CODEC_IN( TYPE, FN, FORM1 )

/* Variable roots are loaded into driver-owned regions. The generated load
   itself neither allocates nor follows a reference. A variable table has no
   message form (docs/SPEC-TABLES.md §3.3), so its row carries no message
   codec. */
#define SCHEMA_CONFORMANCE_GRAPH_CODEC( TYPE, FN ) \
    static int64_t schema_conformance_retain_fuzz_##TYPE(const uint8_t * wire,int64_t bytes,uint8_t ** saved,ConformanceReport * report,int * loaded,int64_t * extent) \
    { \
     static uint8_t store[1<<20];static SCHEMA_CONFORMANCE_Q(table_retain_id_t) ids[(1<<20)/8];SCHEMA_CONFORMANCE_Q(table_retain_t) retain={0};SCHEMA_CONFORMANCE_Q(table_report_t) inner={0};uint8_t * region;const SCHEMA_CONFORMANCE_Q(FN##_t) * root;int64_t size=-1; \
     retain.bytes=store;retain.capacity=sizeof(store);retain.ids=ids;retain.id_capacity=(1<<20)/8;*extent=SCHEMA_CONFORMANCE_Q(FN##_load_measure)(wire,bytes);region=(uint8_t *)malloc((size_t)(*extent>0?*extent:1));if(!region)return -1; \
     root=SCHEMA_CONFORMANCE_Q(FN##_load_retain)(region,*extent,wire,bytes,&retain,&inner);*loaded=root!=NULL;if(root)size=SCHEMA_CONFORMANCE_Q(FN##_measure_retain)(root,&retain); \
     if(size>=0){*saved=(uint8_t *)malloc((size_t)(size?size:1));if(!*saved)size=-1;else size=SCHEMA_CONFORMANCE_Q(FN##_save_retain)(root,&retain,*saved,size,&inner);}free(region);SCHEMA_CONFORMANCE_COPY_REPORT(inner,report);report->retained=inner.retained;report->retain_lost=inner.retain_lost;return size; \
    } \
    typedef struct { SCHEMA_CONFORMANCE_Q(FN##_t) empty; const SCHEMA_CONFORMANCE_Q(FN##_t) * root; uint8_t * region; } schema_conformance_holder_##TYPE; \
    static schema_conformance_holder_##TYPE schema_conformance_storage_##TYPE; \
    static void * schema_conformance_make_##TYPE(void) \
    { \
        schema_conformance_holder_##TYPE * holder=&schema_conformance_storage_##TYPE; \
        free(holder->region); holder->region=NULL; SCHEMA_CONFORMANCE_Q(FN##_reset)(&holder->empty); holder->root=&holder->empty; \
        return holder; \
    } \
    static int schema_conformance_load_##TYPE(void * value,const uint8_t * bytes,int64_t size,ConformanceReport * report) \
    { \
        schema_conformance_holder_##TYPE * holder=(schema_conformance_holder_##TYPE *)value; \
        SCHEMA_CONFORMANCE_Q(table_report_t) inner; int64_t extent=SCHEMA_CONFORMANCE_Q(FN##_load_measure)(bytes,size); const SCHEMA_CONFORMANCE_Q(FN##_t) * root; \
        memset(&inner,0,sizeof(inner)); \
        if(extent>0) { holder->region=(uint8_t *)malloc((size_t)extent); } \
        root=SCHEMA_CONFORMANCE_Q(SCHEMA_CONFORMANCE_READ(FN##_load))(holder->region,extent,bytes,size,&inner); \
        if(root!=NULL) { holder->root=root; } \
        SCHEMA_CONFORMANCE_COPY_REPORT(inner,report); return root!=NULL; \
    } \
    static int64_t schema_conformance_load_measure_##TYPE(const uint8_t * wire,int64_t bytes) \
    { return SCHEMA_CONFORMANCE_Q(FN##_load_measure)(wire,bytes); } \
    static int64_t schema_conformance_measure_##TYPE(const void * value) \
    { return SCHEMA_CONFORMANCE_Q(FN##_measure)(NULL,((const schema_conformance_holder_##TYPE *)value)->root); } \
    static int64_t schema_conformance_save_##TYPE(const void * value,uint8_t * buffer,int64_t capacity) \
    { return SCHEMA_CONFORMANCE_Q(FN##_save)(NULL,((const schema_conformance_holder_##TYPE *)value)->root,buffer,capacity); } \
    static int64_t schema_conformance_cook_measure_##TYPE(const void * value) \
    { return SCHEMA_CONFORMANCE_Q(FN##_cook_measure)(NULL,((const schema_conformance_holder_##TYPE *)value)->root); } \
    static int schema_conformance_cook_##TYPE(const void * value,void * buffer,uint64_t capacity,int big) \
    { return SCHEMA_CONFORMANCE_Q(FN##_cook)(NULL,((const schema_conformance_holder_##TYPE *)value)->root,buffer,capacity,big ? SCHEMA_CONFORMANCE_QU(TABLE_BYTE_ORDER_BIG) : SCHEMA_CONFORMANCE_QU(TABLE_BYTE_ORDER_LITTLE)); } \
    static int schema_conformance_from_json_##TYPE(void * value,const char * text,int64_t bytes,ConformanceReport * report) \
    { \
        schema_conformance_holder_##TYPE * holder=(schema_conformance_holder_##TYPE *)value; \
        SCHEMA_CONFORMANCE_Q(FN##_builder_t) builder; SCHEMA_CONFORMANCE_Q(table_report_t) inner; int ok; \
        memset(&inner,0,sizeof(inner)); \
        if(!SCHEMA_CONFORMANCE_Q(FN##_builder_init)(&builder)) { SCHEMA_CONFORMANCE_Q(FN##_builder_shutdown)(&builder); return 0; } \
        ok=SCHEMA_CONFORMANCE_Q(SCHEMA_CONFORMANCE_READ(FN##_from_json))(&builder,text,bytes,&inner); \
        if(ok) { ok=SCHEMA_CONFORMANCE_Q(FN##_builder_lock)(&builder); } \
        if(ok) { free(holder->region); holder->region=builder.region; holder->root=(const SCHEMA_CONFORMANCE_Q(FN##_t) *)(const void *)builder.region; builder.region=NULL; } \
        SCHEMA_CONFORMANCE_Q(FN##_builder_shutdown)(&builder); SCHEMA_CONFORMANCE_COPY_REPORT(inner,report); return ok; \
    } \
    static int64_t schema_conformance_to_json_##TYPE(const void * value,char * buffer,int64_t capacity) \
    { return SCHEMA_CONFORMANCE_Q(FN##_to_json)(((const schema_conformance_holder_##TYPE *)value)->root,buffer,capacity); } \
    /* a region slot is SELF-RELATIVE (§6.3): the alias names the source's node \
       by the delta from its own address, and the alias's old node goes unreached */ \
    static int schema_conformance_share_##TYPE(void * value,const char * source,const char * alias) \
    { \
        schema_conformance_holder_##TYPE * holder=(schema_conformance_holder_##TYPE *)value; \
        const SCHEMA_CONFORMANCE_Q(table_type_info_t) * info=SCHEMA_CONFORMANCE_Q(FN##_table_type)(); \
        SCHEMA_CONFORMANCE_Q(table_ref_t) * from=NULL, * to=NULL; uint8_t * root; int32_t i; \
        if(holder->region==NULL||holder->root!=(const SCHEMA_CONFORMANCE_Q(FN##_t) *)(const void *)holder->region) { return 0; } \
        root=holder->region; \
        for(i=0;i<info->num_fields;i++) \
        { \
            const SCHEMA_CONFORMANCE_Q(table_field_info_t) * f=&info->fields[i]; \
            if(strcmp(f->name,source)==0) { from=(SCHEMA_CONFORMANCE_Q(table_ref_t) *)(void *)(root+f->offset); } \
            if(strcmp(f->name,alias)==0) { to=(SCHEMA_CONFORMANCE_Q(table_ref_t) *)(void *)(root+f->offset); } \
        } \
        if(from==NULL||to==NULL||from==to||from->value==0||to->value==0) { return 0; } \
        to->value=(int64_t)(((uint8_t *)from+from->value)-(uint8_t *)to); \
        return 1; \
    }

#define SCHEMA_CONFORMANCE_GRAPH_ROW( UNIT, TYPE ) \
    { #UNIT, #TYPE, NULL, NULL, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE, schema_conformance_load_measure_##TYPE, schema_conformance_cook_measure_##TYPE, schema_conformance_cook_##TYPE, schema_conformance_retain_fuzz_##TYPE, NULL, NULL, schema_conformance_share_##TYPE }

#define SCHEMA_CONFORMANCE_ROW( UNIT, TYPE ) \
    { #UNIT, #TYPE, schema_conformance_message_fuzz_##TYPE, schema_conformance_message_##TYPE, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, \
      schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE, NULL, schema_conformance_cook_measure_##TYPE, schema_conformance_cook_##TYPE, NULL, schema_conformance_damage_##TYPE, \
      schema_conformance_load_variable_##TYPE, NULL }

#endif /* SCHEMA_CONFORMANCE_UNIT_H */
