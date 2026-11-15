// The C JSON writer refuses text that holds a zero byte (docs/SPEC-TABLES.md
// §16.3, §16.14 W0; docs/SPEC.md §5.1 row 8). No read stores U+0000, so a
// value a program built holding one is off the write contract. C is an assert
// leg: the refusal is a debug assert, so each write runs in a child and death
// by SIGABRT is the refusal. The same value without the zero writes as today.
package ctable

import "testing"

func TestCJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Probe
{
    name  string(8)
    rules [..4]string(8)
    names map[string(8)]int32
}
`
	probe := `#define _POSIX_C_SOURCE 200809L

#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include "ProbeTable.h"

enum { kString = 0, kTextList = 1, kMapKey = 2 };
enum { kMeasure = 0, kWrite = 1 };

static int build( int where, int zero, ProbeBuilder * builder )
{
    Probe * root;
    const char * text = zero ? "a\0b" : "abc";
    if ( !probe_builder_init( builder ) ) { return 0; }
    root = probe_builder_root( builder );
    if ( root == NULL ) { return 0; }
    if ( where == kString )
    {
        memcpy( root->name, text, 3 );
        root->name_length = 3;
    }
    else if ( where == kTextList )
    {
        memset( &root->rules[0], 0, sizeof( root->rules[0] ) );
        memcpy( root->rules[0].value, text, 3 );
        root->rules[0].value_length = 3;
        root->rules_count = 1;
    }
    else
    {
        TableCtx ctx;
        TableSequenceCursor cursor;
        ProbeNamesEntry * entry;
        if ( !probe_names_insert( &builder->main, &root->names, "abc" ) ) { return 0; }
        memset( &ctx, 0, sizeof( ctx ) );
        ctx.arena = &builder->arena;
        cursor = probe_names_each( &ctx, &root->names );
        entry = (ProbeNamesEntry *) (void *) (uintptr_t) table_sequence_next( &cursor );
        if ( entry == NULL ) { return 0; }
        memcpy( entry->key, text, 3 );
        entry->key_length = 3;
        entry->value = 7;
    }
    return probe_builder_lock( builder );
}

/* 1 when the writer died on the assert, 0 when it returned a text. */
static int refused( int where, int which )
{
    ProbeBuilder builder;
    const Probe * value;
    pid_t child;
    int status = 0;
    if ( !build( where, 1, &builder ) ) { probe_builder_shutdown( &builder ); return 0; }
    value = (const Probe *) builder.region;
    fflush( stdout );
    fflush( stderr );
    child = fork();
    if ( child < 0 ) { probe_builder_shutdown( &builder ); return 0; }
    if ( child == 0 )
    {
        char text[4096];
        int quiet = open( "/dev/null", O_WRONLY );
        if ( quiet >= 0 ) { dup2( quiet, 2 ); }
        if ( which == kMeasure ) { (void) probe_to_json_measure( value ); }
        else { (void) probe_to_json( value, text, (int64_t) sizeof( text ) ); }
        _exit( 0 );
    }
    if ( waitpid( child, &status, 0 ) != child ) { probe_builder_shutdown( &builder ); return 0; }
    probe_builder_shutdown( &builder );
    return WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT;
}

/* The same value without the zero writes as today. */
static int writes( int where )
{
    ProbeBuilder builder;
    const Probe * value;
    char text[4096];
    int64_t measure, wrote;
    if ( !build( where, 0, &builder ) ) { probe_builder_shutdown( &builder ); return 0; }
    value = (const Probe *) builder.region;
    measure = probe_to_json_measure( value );
    if ( measure <= 0 || measure >= (int64_t) sizeof( text ) ) { probe_builder_shutdown( &builder ); return 0; }
    wrote = probe_to_json( value, text, (int64_t) sizeof( text ) );
    if ( wrote != measure ) { probe_builder_shutdown( &builder ); return 0; }
    text[wrote] = 0;
    probe_builder_shutdown( &builder );
    return strstr( text, "\"abc\"" ) != NULL && strstr( text, "\\u0000" ) == NULL;
}

int main( void )
{
    int where;
    for ( where = kString; where <= kMapKey; where++ )
    {
        if ( !writes( where ) )
        {
            fprintf( stderr, "FAIL: the value without the zero did not write (site %d)\n", where );
            return 1;
        }
        if ( !refused( where, kMeasure ) )
        {
            fprintf( stderr, "FAIL: ToJsonMeasure wrote a zero byte (site %d)\n", where );
            return 1;
        }
        if ( !refused( where, kWrite ) )
        {
            fprintf( stderr, "FAIL: ToJson wrote a zero byte (site %d)\n", where );
            return 1;
        }
    }
    return 0;
}
`
	runCProbeCapture(t, schema, probe)
}
