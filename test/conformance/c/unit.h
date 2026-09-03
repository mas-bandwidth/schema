/* ONE UNIT's glue, as macros (test/conformance/README.md).
 *
 * Included AFTER that unit's generated headers, so TableReport and the
 * name-first surface are in scope. Every unit's translation unit is these two
 * macros and a list of roots; there is no third thing.
 *
 * SCHEMA_CONFORMANCE_CODEC erases one root behind the driver's function
 * pointers, and SCHEMA_CONFORMANCE_UNIT hands the table back through the one
 * external name that unit exports. */

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
    } while ( 0 )

#define SCHEMA_CONFORMANCE_CODEC( TYPE ) \
    static TYPE schema_conformance_storage_##TYPE; \
    static void * schema_conformance_make_##TYPE( void ) \
    { \
        memset( &schema_conformance_storage_##TYPE, 0, sizeof( schema_conformance_storage_##TYPE ) ); \
        TYPE##Reset( &schema_conformance_storage_##TYPE ); \
        return &schema_conformance_storage_##TYPE; \
    } \
    static int schema_conformance_load_##TYPE( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report ) \
    { \
        TableReport inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = TYPE##Load( (TYPE *) value, bytes, size, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    static int64_t schema_conformance_measure_##TYPE( const void * value ) \
    { \
        return TYPE##Measure( (const TYPE *) value ); \
    } \
    static int64_t schema_conformance_save_##TYPE( const void * value, uint8_t * buffer, int64_t capacity ) \
    { \
        return TYPE##Save( (const TYPE *) value, buffer, capacity ); \
    } \
    static int schema_conformance_from_json_##TYPE( void * value, const char * text, int64_t bytes, ConformanceReport * report ) \
    { \
        TableReport inner; \
        int ok; \
        memset( &inner, 0, sizeof( inner ) ); \
        ok = TYPE##FromJson( (TYPE *) value, text, bytes, &inner ); \
        SCHEMA_CONFORMANCE_COPY_REPORT( inner, report ); \
        return ok; \
    } \
    static int64_t schema_conformance_to_json_##TYPE( const void * value, char * buffer, int64_t capacity ) \
    { \
        return buffer != NULL ? TYPE##ToJson( (const TYPE *) value, buffer, capacity ) \
                              : TYPE##ToJsonMeasure( (const TYPE *) value ); \
    }

#define SCHEMA_CONFORMANCE_ROW( UNIT, TYPE ) \
    { #UNIT, #TYPE, schema_conformance_load_##TYPE, schema_conformance_measure_##TYPE, \
      schema_conformance_save_##TYPE, schema_conformance_from_json_##TYPE, \
      schema_conformance_to_json_##TYPE, schema_conformance_make_##TYPE }

#endif /* SCHEMA_CONFORMANCE_UNIT_H */
