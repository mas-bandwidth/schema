/* A REFUSED WRITE WRITES NOTHING (docs/SPEC.md §5.1), the C leg's twin of
   test/tables/main.cpp's test_refusal_writes_nothing.

   Every C writer that refuses in every build — a count or a length past its
   storage, a NaN, a value that does not fit the capacity — refuses before its
   first byte, so the caller's buffer is exactly what it handed over. The buffer
   is POISONED, each writer is handed a value it refuses with an earlier field
   (version_note) that a writer working front to back would already have
   stored, and the poison must be intact. tables-c-refusal-writes-nothing builds
   this file twice, as is and -DNDEBUG: none of these refusals is an assert. */
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "TablesTable.h"

static int failures = 0;
static void check( int ok, const char * what )
{
    if ( !ok ) { printf( "FAILED: %s\n", what ); failures++; }
}

static uint8_t buffer[16384];

static int poison_intact( void )
{
    size_t i;
    for ( i = 0; i < sizeof( buffer ); i++ )
    {
        if ( buffer[i] != 0xA5 ) { return 0; }
    }
    return 1;
}

static void poison( void ) { memset( buffer, 0xA5, sizeof( buffer ) ); }

static tabledemo_root_config_t root;

int main( void )
{
    int64_t need, message_need;
    tabledemo_root_config_reset( &root );
    memcpy( root.version_note, "abc", 3 );
    root.version_note_length = 3;
    root.weapons_count = 1;

    /* the baseline: each writer takes the value, so each refusal below is the
       one field changed */
    need = tabledemo_root_config_measure( &root );
    check( need > 0 && tabledemo_root_config_save( &root, buffer, (int64_t) sizeof( buffer ) ) == need, "the plain save takes the baseline" );
    check( tabledemo_root_config_fixed_save( &root, 1, buffer, (int64_t) sizeof( buffer ) ) == tabledemo_root_config_fixed_measure( 1 ), "the fixed save takes the baseline" );
    message_need = tabledemo_root_config_measure_messages( &root, 1, NULL );
    check( message_need > 0 && tabledemo_root_config_save_messages( &root, 1, buffer, (int64_t) sizeof( buffer ), NULL ) == message_need, "the message save takes the baseline" );

    /* a count past its storage */
    root.weapons_count = 9;
    poison();
    check( tabledemo_root_config_save( &root, buffer, (int64_t) sizeof( buffer ) ) == -1, "the plain save refuses a count past storage" );
    check( poison_intact(), "the plain save's refused count wrote nothing" );
    poison();
    check( tabledemo_root_config_fixed_save( &root, 1, buffer, (int64_t) sizeof( buffer ) ) == -1, "the fixed save refuses a count past storage" );
    check( poison_intact(), "the fixed save's refused count wrote nothing" );
    poison();
    check( tabledemo_root_config_save_messages( &root, 1, buffer, (int64_t) sizeof( buffer ), NULL ) == -1, "the message save refuses a count past storage" );
    check( poison_intact(), "the message save's refused count wrote nothing" );
    root.weapons_count = 1;

    /* a NaN after the text */
    root.weapons[0].damage = NAN;
    poison();
    check( tabledemo_root_config_save( &root, buffer, (int64_t) sizeof( buffer ) ) == -1, "the plain save refuses a NaN" );
    check( poison_intact(), "the plain save's refused NaN wrote nothing" );
    poison();
    check( tabledemo_root_config_fixed_save( &root, 1, buffer, (int64_t) sizeof( buffer ) ) == -1, "the fixed save refuses a NaN" );
    check( poison_intact(), "the fixed save's refused NaN wrote nothing" );
    poison();
    check( tabledemo_root_config_save_messages( &root, 1, buffer, (int64_t) sizeof( buffer ), NULL ) == -1, "the message save refuses a NaN" );
    check( poison_intact(), "the message save's refused NaN wrote nothing" );
    root.weapons[0].damage = 21.0f;

    /* one byte short of what the value needs */
    poison();
    check( tabledemo_root_config_save( &root, buffer, need - 1 ) == -1, "the plain save refuses a short buffer" );
    check( poison_intact(), "the plain save's short buffer wrote nothing" );
    poison();
    check( tabledemo_root_config_save_messages( &root, 1, buffer, message_need - 1, NULL ) == -1, "the message save refuses a short buffer" );
    check( poison_intact(), "the message save's short buffer wrote nothing" );

    if ( failures != 0 ) { return 1; }
#ifdef NDEBUG
    printf( "C refusals write nothing (-DNDEBUG)\n" );
#else
    printf( "C refusals write nothing\n" );
#endif
    return 0;
}
