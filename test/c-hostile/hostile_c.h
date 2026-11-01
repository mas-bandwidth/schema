/* THE C PORT'S BINDING MACROS (docs/HOSTILE-CORPUS.md). A C unit's translation
 * unit includes its own generated headers, defines HC_PACKAGE and
 * HC_PACKAGE_UPPER, includes this file, and lists its roots with HC_FIXED /
 * HC_VAR / HC_MESSAGE / HC_ANNOUNCE / HC_PACKET; the answer functions and the
 * binding row are hostile.h's. */

#ifndef SCHEMA_C_HOSTILE_C_H
#define SCHEMA_C_HOSTILE_C_H

#include "hostile.h"

#define HC_CAT_( a, b ) a##_##b
#define HC_CAT( a, b ) HC_CAT_( a, b )
#define HC_Q( name ) HC_CAT( HC_PACKAGE, name )
#define HC_QU( name ) HC_CAT( HC_PACKAGE_UPPER, name )

/* The verdict off a tolerant read's report, as the engine gives it: a named
   refusal is its reason, a read that did not land (or reported damage) is
   `damaged`, and anything else is accepted. */
#define HC_VERDICT_REFUSED( report, landed ) \
    ( hc_trace( ( report ).refused, ( report ).malformed, ( report ).reason, ( landed ) ), ( report ).refused ? ( hc_refuse( hc_reason_name( ( report ).reason ) ), 1 ) \
                         : ( !( landed ) || ( report ).malformed ) ? ( hc_refuse( "damaged" ), 1 ) : 0 )

/* The text writer's answer, digested by the driver. */
#define HC_TO_JSON( FN, root ) \
    do { \
        int64_t hc_need = HC_Q( FN##_to_json_measure )( root ); \
        char * hc_text; \
        if ( hc_need < 0 ) { hc_error( "to_json_measure refused the decoded value" ); break; } \
        hc_text = (char *) malloc( (size_t) hc_need + 1 ); \
        if ( hc_text == NULL ) { hc_error( "out of memory" ); break; } \
        if ( HC_Q( FN##_to_json )( root, hc_text, hc_need + 1 ) != hc_need ) { free( hc_text ); hc_error( "to_json disagreed with its measure" ); break; } \
        hc_accept_text( hc_text, hc_need ); \
        free( hc_text ); \
    } while ( 0 )

/* A FIXED root (relocatable, bounded storage): read into the caller's value.
   C has no retaining load for such a root (its load_retain is a compile-time
   refusal), so its wire_retain carrier answers skip. */
#define HC_FIXED( FN ) \
    static int64_t hc_save_##FN( const void * v, uint8_t * b, int64_t n ) { return HC_Q( FN##_save )( (const HC_Q( FN##_t ) *) v, b, n ); } \
    static void hc_accepted_##FN( const HC_Q( FN##_t ) * value ) \
    { \
        hc_exercise_save( HC_Q( FN##_measure )( value ), hc_save_##FN, value ); \
        HC_TO_JSON( FN, value ); \
    } \
    static void hc_wire_##FN( const uint8_t * data, int64_t size ) \
    { \
        HC_Q( FN##_t ) * value = (HC_Q( FN##_t ) *) calloc( 1, sizeof( HC_Q( FN##_t ) ) ); \
        HC_Q( table_report_t ) report; \
        int ok; \
        memset( &report, 0, sizeof( report ) ); \
        if ( value == NULL ) { hc_error( "out of memory" ); return; } \
        HC_Q( FN##_reset )( value ); \
        ok = HC_Q( FN##_load_tolerant )( value, data, size, &report ); \
        if ( !HC_VERDICT_REFUSED( report, ok ) ) { hc_accepted_##FN( value ); } \
        free( value ); \
    } \
    static void hc_text_##FN( const uint8_t * data, int64_t size ) \
    { \
        HC_Q( FN##_t ) * value = (HC_Q( FN##_t ) *) calloc( 1, sizeof( HC_Q( FN##_t ) ) ); \
        HC_Q( table_report_t ) report; \
        int ok; \
        memset( &report, 0, sizeof( report ) ); \
        if ( value == NULL ) { hc_error( "out of memory" ); return; } \
        HC_Q( FN##_reset )( value ); \
        ok = HC_Q( FN##_from_json_tolerant )( value, (const char *) data, size, &report ); \
        if ( !HC_VERDICT_REFUSED( report, ok ) ) { hc_accepted_##FN( value ); } \
        free( value ); \
    }

/* a fixed root's row fields; a unit adds .message, .announce or .packet */
#define HC_FIXED_FIELDS( SCHEMA, TABLE, FN ) \
    .schema = SCHEMA, .table = TABLE, .wire = hc_wire_##FN, .text = hc_text_##FN, \
    .wire_retain_skip = "C has no retaining load for a fixed-storage root (its load_retain is a compile-time refusal)"

/* A VARIABLE root: read into a caller-owned region sized by load_measure; its
   text is read into a builder whose lock packs the same region form. */
#define HC_VAR( FN ) \
    static int64_t hc_save_##FN( const void * v, uint8_t * b, int64_t n ) { return HC_Q( FN##_save )( NULL, (const HC_Q( FN##_t ) *) v, b, n ); } \
    static void hc_accepted_##FN( const HC_Q( FN##_t ) * root ) \
    { \
        hc_exercise_save( HC_Q( FN##_measure )( NULL, root ), hc_save_##FN, root ); \
        HC_TO_JSON( FN, root ); \
    } \
    static void hc_wire_##FN( const uint8_t * data, int64_t size ) \
    { \
        int64_t extent = HC_Q( FN##_load_measure )( data, size ); \
        uint8_t * region = (uint8_t *) malloc( (size_t) ( extent > 0 ? extent : 1 ) ); \
        HC_Q( table_report_t ) report; \
        const HC_Q( FN##_t ) * root; \
        memset( &report, 0, sizeof( report ) ); \
        if ( region == NULL ) { hc_error( "out of memory" ); return; } \
        root = HC_Q( FN##_load_tolerant )( region, extent, data, size, &report ); \
        if ( !HC_VERDICT_REFUSED( report, root != NULL ) ) { hc_accepted_##FN( root ); } \
        free( region ); \
    } \
    static void hc_wire_retain_##FN( const uint8_t * data, int64_t size ) \
    { \
        enum { roomy = 1 << 20 }; \
        int64_t extent = HC_Q( FN##_load_measure )( data, size ); \
        uint8_t * region = (uint8_t *) malloc( (size_t) ( extent > 0 ? extent : 1 ) ); \
        HC_Q( table_retain_t ) retain; \
        HC_Q( table_report_t ) report, save_report; \
        const HC_Q( FN##_t ) * root; \
        memset( &retain, 0, sizeof( retain ) ); \
        memset( &report, 0, sizeof( report ) ); \
        memset( &save_report, 0, sizeof( save_report ) ); \
        retain.bytes = (uint8_t *) malloc( roomy ); \
        retain.capacity = roomy; \
        retain.ids = (HC_Q( table_retain_id_t ) *) calloc( roomy / 8, sizeof( HC_Q( table_retain_id_t ) ) ); \
        retain.id_capacity = roomy / 8; \
        if ( region == NULL || retain.bytes == NULL || retain.ids == NULL ) { hc_error( "out of memory" ); } \
        else \
        { \
            root = HC_Q( FN##_load_retain )( region, extent, data, size, &retain, &report ); \
            if ( !HC_VERDICT_REFUSED( report, root != NULL ) ) \
            { \
                int64_t need = HC_Q( FN##_measure_retain )( root, &retain ); \
                if ( need >= 0 ) \
                { \
                    uint8_t * saved = (uint8_t *) malloc( (size_t) ( need ? need : 1 ) ); \
                    if ( saved != NULL ) { (void) HC_Q( FN##_save_retain )( root, &retain, saved, need, &save_report ); } \
                    free( saved ); \
                } \
                hc_accepted_##FN( root ); \
            } \
        } \
        free( retain.bytes ); \
        free( retain.ids ); \
        free( region ); \
    } \
    static void hc_text_##FN( const uint8_t * data, int64_t size ) \
    { \
        HC_Q( FN##_builder_t ) builder; \
        HC_Q( table_report_t ) report; \
        int ok; \
        memset( &report, 0, sizeof( report ) ); \
        if ( !HC_Q( FN##_builder_init )( &builder ) ) { HC_Q( FN##_builder_shutdown )( &builder ); hc_error( "builder_init refused" ); return; } \
        ok = HC_Q( FN##_from_json_tolerant )( &builder, (const char *) data, size, &report ); \
        if ( !HC_VERDICT_REFUSED( report, ok ) ) \
        { \
            if ( !HC_Q( FN##_builder_lock )( &builder ) ) { hc_error( "builder_lock refused the decoded value" ); } \
            else { hc_accepted_##FN( (const HC_Q( FN##_t ) *) (const void *) builder.region ); } \
        } \
        HC_Q( FN##_builder_shutdown )( &builder ); \
    }

#define HC_VAR_FIELDS( SCHEMA, TABLE, FN ) \
    .schema = SCHEMA, .table = TABLE, .wire = hc_wire_##FN, .wire_retain = hc_wire_retain_##FN, .text = hc_text_##FN

/* The MESSAGE FORM of a fixed root (docs/SPEC-TABLES.md §3.3): this unit's own
   announcement is read first (unless the carrier withholds it), then one body
   is decoded into a batch of capacity one. */
#define HC_MESSAGE( FN ) \
    static void hc_message_##FN( const uint8_t * data, int64_t size, int announced ) \
    { \
        enum { capacity = 4096 }; \
        HC_Q( table_message_entry_t ) * entries = (HC_Q( table_message_entry_t ) *) calloc( capacity, sizeof( HC_Q( table_message_entry_t ) ) ); \
        HC_Q( FN##_t ) * value = (HC_Q( FN##_t ) *) calloc( 1, sizeof( HC_Q( FN##_t ) ) ); \
        HC_Q( table_vocabulary_t ) vocabulary; \
        HC_Q( table_report_t ) report; \
        int64_t count = 1; \
        int ok; \
        memset( &report, 0, sizeof( report ) ); \
        if ( entries == NULL || value == NULL ) { free( entries ); free( value ); hc_error( "out of memory" ); return; } \
        vocabulary = HC_Q( table_vocabulary )( entries, capacity ); \
        if ( announced && !HC_Q( announce_read )( &vocabulary, HC_QU( K_TABLE_ANNOUNCE ), HC_QU( K_TABLE_ANNOUNCE_BYTES ), &report ) ) \
        { \
            free( entries ); free( value ); hc_error( "the unit could not read its own announcement" ); return; \
        } \
        memset( &report, 0, sizeof( report ) ); \
        HC_Q( FN##_reset )( value ); \
        ok = HC_Q( FN##_load_messages_tolerant )( value, &count, &vocabulary, data, size, &report ); \
        if ( !HC_VERDICT_REFUSED( report, ok && count == 1 ) ) { hc_accepted_##FN( value ); } \
        free( entries ); \
        free( value ); \
    }

/* The unit's announcement read (docs/SPEC-TABLES.md §3.3), under the bound
   the carrier names. */
#define HC_ANNOUNCE() \
    static void hc_announce( const uint8_t * data, int64_t size, const char * carrier ) \
    { \
        enum { capacity = 4096 }; \
        HC_Q( table_message_entry_t ) * entries = (HC_Q( table_message_entry_t ) *) calloc( capacity, sizeof( HC_Q( table_message_entry_t ) ) ); \
        HC_Q( table_vocabulary_t ) vocabulary; \
        HC_Q( table_report_t ) report; \
        int ok; \
        memset( &report, 0, sizeof( report ) ); \
        if ( entries == NULL ) { hc_error( "out of memory" ); return; } \
        vocabulary = HC_Q( table_vocabulary )( entries, capacity ); \
        if ( strcmp( carrier, "announce_bytes" ) == 0 ) { vocabulary.max_bytes = 8; } \
        if ( strcmp( carrier, "announce_entries" ) == 0 ) { vocabulary.max_entries = 1; } \
        if ( strcmp( carrier, "announce_second" ) == 0 ) \
        { \
            (void) HC_Q( announce_read )( &vocabulary, data, size, &report ); \
            memset( &report, 0, sizeof( report ) ); \
        } \
        ok = HC_Q( announce_read )( &vocabulary, data, size, &report ); \
        if ( !HC_VERDICT_REFUSED( report, ok ) ) { hc_accept_none(); } \
        free( entries ); \
    }

/* A PACKET type (the type wire, serialize.c): read from bit 0 of the input,
   then re-written by the port's own writer, whose bytes the driver digests. */
#define HC_PACKET( FN ) \
    static void hc_packet_##FN( const uint8_t * data, int64_t size ) \
    { \
        enum { room = 1 << 20 }; \
        int64_t words = ( size + 8 + 3 ) / 4; /* the stream reads up to 8 bytes past the data */ \
        uint32_t * storage = (uint32_t *) calloc( (size_t) words, 4 ); \
        uint32_t * encoded = (uint32_t *) calloc( room / 4, 4 ); \
        HC_Q( FN##_t ) * value = (HC_Q( FN##_t ) *) calloc( 1, sizeof( HC_Q( FN##_t ) ) ); \
        serialize_read_stream_t r; \
        serialize_write_stream_t w; \
        if ( storage == NULL || encoded == NULL || value == NULL || size > INT32_MAX ) \
        { \
            free( storage ); free( encoded ); free( value ); hc_error( "out of memory" ); return; \
        } \
        memcpy( storage, data, (size_t) size ); \
        serialize_read_stream_init( &r, (const uint8_t *) storage, (int) size ); \
        if ( !HC_Q( read_##FN )( &r, value ) ) { hc_refuse( "packet_refused" ); } \
        else \
        { \
            serialize_write_stream_init( &w, (uint8_t *) encoded, room ); \
            if ( !HC_Q( write_##FN )( &w, value ) ) { hc_error( "the packet writer refused the decoded value" ); } \
            else \
            { \
                serialize_write_flush( &w ); \
                hc_accept_bytes( (const uint8_t *) encoded, serialize_write_bytes_processed( &w ) ); \
            } \
        } \
        free( storage ); free( encoded ); free( value ); \
    }

#endif /* SCHEMA_C_HOSTILE_C_H */
