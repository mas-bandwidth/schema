/* c/P2: write-read-write byte-identical, row assertion (docs/SPEC-TABLES.md:6120).
 *
 * The matrix cell c/P2 says the C leg must not move a value across the variable
 * and message forms. This file runs the round trip for the backenddemo unit's
 * six message vectors: load a variable-form body, save it as a message, and
 * compare against the pinned message bytes; then load the message form with
 * the connection's announcement, save it as a variable form, and compare
 * against the pinned variable bytes. Red if one byte differs in either
 * direction.
 *
 * This is a standalone C11 file. It reaches the generated backenddemo codec
 * the same way the conformance driver does, through BackendTable.h.
 */

#include "BackendTable.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures = 0;

static void check( int ok, const char * what )
{
    if ( !ok )
    {
        fprintf( stderr, "FAIL: %s\n", what );
        failures++;
    }
}

static uint8_t * slurp( const char * path, size_t * out )
{
    FILE * file = fopen( path, "rb" );
    long size;
    uint8_t * data;
    if ( file == NULL )
    {
        fprintf( stderr, "FAIL: cannot open %s: %s\n", path, strerror( errno ) );
        exit( 1 );
    }
    if ( fseek( file, 0, SEEK_END ) != 0 )
    {
        fclose( file );
        return NULL;
    }
    size = ftell( file );
    if ( size < 0 )
    {
        fclose( file );
        return NULL;
    }
    rewind( file );
    data = (uint8_t *) malloc( (size_t) size );
    if ( data == NULL )
    {
        fclose( file );
        return NULL;
    }
    if ( (size_t) size > 0 && fread( data, 1, (size_t) size, file ) != (size_t) size )
    {
        free( data );
        fclose( file );
        return NULL;
    }
    fclose( file );
    *out = (size_t) size;
    return data;
}

/* For each root we test the same two-direction round trip. The generated
 * surface spells measure/load/save with singular verbs for the variable form
 * and plural verbs for the message form, and loading a message needs the
 * connection's announcement to build the vocabulary. */
#define P2_ONE_DIRECTION( TYPE, FN, var_path, msg_path, announce_path, label ) \
    do { \
        uint8_t * variable = NULL; size_t variable_bytes = 0; \
        uint8_t * message = NULL; size_t message_bytes = 0; \
        uint8_t * announce = NULL; size_t announce_bytes = 0; \
        TYPE value; \
        TableReport report; \
        int64_t need, written; \
        uint8_t saved[2048]; \
        \
        variable = slurp( var_path, &variable_bytes ); \
        message = slurp( msg_path, &message_bytes ); \
        announce = slurp( announce_path, &announce_bytes ); \
        \
        /* forward: variable form -> message form */ \
        memset( &report, 0, sizeof( report ) ); \
        memset( &value, 0, sizeof( value ) ); \
        FN##_reset( &value ); \
        check( FN##_load( &value, variable, (int64_t) variable_bytes, &report ), label ": load variable form" ); \
        need = FN##_measure_messages( &value, 1, &report ); \
        check( need == (int64_t) message_bytes, label ": message size matches golden" ); \
        written = FN##_save_messages( &value, 1, saved, need, &report ); \
        check( written == need && memcmp( saved, message, message_bytes ) == 0, label ": variable->message byte-identical" ); \
        \
        /* reverse: message form -> variable form */ \
        { \
            TableMessageEntry entries[kTableMessageEntriesHere]; \
            TableVocabulary vocabulary = table_vocabulary( entries, kTableMessageEntriesHere ); \
            int64_t count = 1; \
            memset( &report, 0, sizeof( report ) ); \
            check( announce_read( &vocabulary, announce, (int64_t) announce_bytes, &report ), label ": read announcement" ); \
            memset( &value, 0, sizeof( value ) ); \
            FN##_reset( &value ); \
            check( FN##_load_messages( &value, &count, &vocabulary, message, (int64_t) message_bytes, &report ), label ": load message form" ); \
            check( count == 1, label ": exactly one message body" ); \
            need = FN##_measure( &value ); \
            check( need == (int64_t) variable_bytes, label ": variable size matches golden" ); \
            written = FN##_save( &value, saved, need ); \
            check( written == need && memcmp( saved, variable, variable_bytes ) == 0, label ": message->variable byte-identical" ); \
        } \
        \
        free( variable ); \
        free( message ); \
        free( announce ); \
    } while ( 0 )

int main( void )
{
    P2_ONE_DIRECTION( LoginRequest, login_request,
                      "testdata/wire/tables/login_full.bin",
                      "testdata/wire/tables/login_full_message.bin",
                      "testdata/wire/tables/backend_conn.bin",
                      "P2 login_full" );

    P2_ONE_DIRECTION( LoginRequest, login_request,
                      "testdata/wire/tables/login_default.bin",
                      "testdata/wire/tables/login_default_message.bin",
                      "testdata/wire/tables/backend_conn.bin",
                      "P2 login_default" );

    P2_ONE_DIRECTION( MatchResult, match_result,
                      "testdata/wire/tables/match_full.bin",
                      "testdata/wire/tables/match_full_message.bin",
                      "testdata/wire/tables/backend_conn.bin",
                      "P2 match_full" );

    P2_ONE_DIRECTION( MatchResult, match_result,
                      "testdata/wire/tables/match_default.bin",
                      "testdata/wire/tables/match_default_message.bin",
                      "testdata/wire/tables/backend_conn.bin",
                      "P2 match_default" );

    P2_ONE_DIRECTION( StorePurchase, store_purchase,
                      "testdata/wire/tables/store_full.bin",
                      "testdata/wire/tables/store_full_message.bin",
                      "testdata/wire/tables/backend_conn.bin",
                      "P2 store_full" );

    P2_ONE_DIRECTION( StorePurchase, store_purchase,
                      "testdata/wire/tables/store_default.bin",
                      "testdata/wire/tables/store_default_message.bin",
                      "testdata/wire/tables/backend_conn.bin",
                      "P2 store_default" );

    if ( failures == 0 )
    {
        printf( "TestRowP2: write-read-write byte-identical green\n" );
        return 0;
    }
    return 1;
}
