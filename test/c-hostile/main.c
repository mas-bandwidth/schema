/* THE HOSTILE-CORPUS CASE BINARY of the C and C++ ports (docs/HOSTILE-CORPUS.md).
 *
 *   case <carrier> <schema> <table> <input-file>
 *
 * Answers one manifest line on stdout in the shape test/c-hostile/hostile.h
 * names. The case runs on its own thread with a fixed 512 KiB stack, so a
 * stack-depth input needs only a modest size. The input is handed to the
 * reader in an allocation of exactly its own length, so a read past its end is
 * a sanitizer report rather than a silent success. Reads one file: no network,
 * no clock. The C++ port links this same file against its own units, which
 * export the same hc_bindings_<unit> names. */

#define _POSIX_C_SOURCE 200809L

#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "hostile.h"

typedef const hc_binding_t * ( *hc_unit_t )( int * count );

const hc_binding_t * hc_bindings_tabledemo( int * count );
const hc_binding_t * hc_bindings_messagedemo( int * count );
const hc_binding_t * hc_bindings_blobdemo( int * count );
const hc_binding_t * hc_bindings_graphdemo( int * count );
const hc_binding_t * hc_bindings_scalardemo( int * count );
const hc_binding_t * hc_bindings_streamdemo( int * count );
const hc_binding_t * hc_bindings_mapdemo( int * count );
const hc_binding_t * hc_bindings_listdemo( int * count );
const hc_binding_t * hc_bindings_probe( int * count );

static const hc_unit_t units[] = {
    hc_bindings_tabledemo, hc_bindings_messagedemo, hc_bindings_blobdemo,
    hc_bindings_graphdemo, hc_bindings_scalardemo, hc_bindings_streamdemo,
    hc_bindings_mapdemo,   hc_bindings_listdemo,    hc_bindings_probe,
};

static int answered;

static void hc_head( const char * line )
{
    fputs( line, stdout );
    fputc( '\n', stdout );
    answered = 1;
}

void hc_accept_text( const char * text, int64_t size )
{
    hc_head( "accept text" );
    fwrite( text, 1, (size_t) size, stdout );
}

void hc_accept_bytes( const uint8_t * bytes, int64_t size )
{
    hc_head( "accept bytes" );
    fwrite( bytes, 1, (size_t) size, stdout );
}

void hc_accept_none( void ) { hc_head( "accept -" ); }

void hc_refuse( const char * reason )
{
    printf( "refuse %s\n", reason );
    answered = 1;
}

void hc_error( const char * what )
{
    printf( "error %s\n", what );
    answered = 1;
}

const char * hc_reason_name( int reason )
{
    static char other[32];
    switch ( reason )
    {
        case 1: return "newer_form";
        case 2: return "message_form_as_file";
        case 3: return "no_vocabulary";
        case 4: return "second_announcement";
        case 5: return "vocabulary_too_large";
        case 6: return "batch_too_large";
        case 21: return "non_finite_float";
        default: break;
    }
    snprintf( other, sizeof( other ), "reason_%d", reason );
    return other;
}

void hc_trace( int refused, int malformed, int reason, int landed )
{
    if ( getenv( "HC_TRACE" ) != NULL )
    {
        fprintf( stderr, "report: refused=%d malformed=%d reason=%d landed=%d\n", refused, malformed, reason, landed );
    }
}

void hc_exercise_save( int64_t size, int64_t ( *save )( const void *, uint8_t *, int64_t ), const void * value )
{
    uint8_t * buffer;
    if ( size < 0 ) { return; }
    buffer = (uint8_t *) malloc( (size_t) ( size ? size : 1 ) );
    if ( buffer == NULL ) { return; }
    (void) save( value, buffer, size );
    free( buffer );
}

typedef struct
{
    const char * carrier;
    const char * schema;
    const char * table;
    const uint8_t * data;
    int64_t size;
} hc_case_t;

static void hc_skip( const char * carrier, const char * why )
{
    printf( "skip %s: %s\n", carrier, why );
    answered = 1;
}

static void * hc_run( void * arg )
{
    const hc_case_t * c = (const hc_case_t *) arg;
    const hc_binding_t * b = NULL;
    size_t u;
    int i, n;
    for ( u = 0; u < sizeof( units ) / sizeof( units[0] ) && b == NULL; u++ )
    {
        const hc_binding_t * rows = units[u]( &n );
        for ( i = 0; i < n; i++ )
        {
            if ( strcmp( rows[i].schema, c->schema ) == 0 && strcmp( rows[i].table, c->table ) == 0 ) { b = &rows[i]; break; }
        }
    }
    if ( b == NULL ) { printf( "unbound no C binding for %s %s\n", c->schema, c->table ); answered = 1; return NULL; }
    if ( strcmp( c->carrier, "wire" ) == 0 )
    {
        if ( b->wire ) b->wire( c->data, c->size ); else hc_skip( c->carrier, "no file-form load for this root" );
    }
    else if ( strcmp( c->carrier, "wire_retain" ) == 0 )
    {
        if ( b->wire_retain ) b->wire_retain( c->data, c->size );
        else hc_skip( c->carrier, b->wire_retain_skip ? b->wire_retain_skip : "no retaining load for this root" );
    }
    else if ( strcmp( c->carrier, "text" ) == 0 )
    {
        if ( b->text ) b->text( c->data, c->size ); else hc_skip( c->carrier, "no text read for this root" );
    }
    else if ( strcmp( c->carrier, "message" ) == 0 || strcmp( c->carrier, "message_novocab" ) == 0 )
    {
        if ( b->message ) b->message( c->data, c->size, strcmp( c->carrier, "message" ) == 0 );
        else { printf( "unbound no C message binding for %s %s\n", c->schema, c->table ); answered = 1; }
    }
    else if ( strncmp( c->carrier, "announce", 8 ) == 0 )
    {
        if ( b->announce ) b->announce( c->data, c->size, c->carrier );
        else { printf( "unbound no C announce binding for %s %s\n", c->schema, c->table ); answered = 1; }
    }
    else if ( strcmp( c->carrier, "packet" ) == 0 )
    {
        if ( b->packet ) b->packet( c->data, c->size );
        else { printf( "unbound no C packet binding for %s %s\n", c->schema, c->table ); answered = 1; }
    }
    else
    {
        printf( "unbound unknown carrier %s\n", c->carrier );
        answered = 1;
    }
    return NULL;
}

int main( int argc, char ** argv )
{
    hc_case_t c;
    FILE * f;
    long length;
    uint8_t * data;
    pthread_attr_t attr;
    pthread_t thread;
    if ( argc != 5 )
    {
        fprintf( stderr, "usage: %s <carrier> <schema> <table> <input-file>\n", argv[0] );
        return 2;
    }
    f = fopen( argv[4], "rb" );
    if ( f == NULL || fseek( f, 0, SEEK_END ) != 0 || ( length = ftell( f ) ) < 0 || fseek( f, 0, SEEK_SET ) != 0 )
    {
        fprintf( stderr, "cannot read %s\n", argv[4] );
        return 2;
    }
    /* exactly the input's length (one byte for an empty input, which no
       read may touch): a read past it is a sanitizer report */
    data = (uint8_t *) malloc( (size_t) ( length > 0 ? length : 1 ) );
    if ( data == NULL || ( length > 0 && fread( data, 1, (size_t) length, f ) != (size_t) length ) )
    {
        fprintf( stderr, "cannot read %s\n", argv[4] );
        return 2;
    }
    fclose( f );
    c.carrier = argv[1];
    c.schema = argv[2];
    c.table = argv[3];
    c.data = data;
    c.size = (int64_t) length;
    if ( pthread_attr_init( &attr ) != 0 || pthread_attr_setstacksize( &attr, 512 * 1024 ) != 0 ||
         pthread_create( &thread, &attr, hc_run, &c ) != 0 || pthread_join( thread, NULL ) != 0 )
    {
        fprintf( stderr, "cannot start the case thread\n" );
        return 2;
    }
    pthread_attr_destroy( &attr );
    free( data );
    if ( !answered ) { hc_error( "no answer" ); }
    fflush( stdout );
    return 0;
}
