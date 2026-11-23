/* R17 ON THE C LEG (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS
   WIDTH; flags_past_width_test.go). Features declares two variants, so W = 2:
   bit 2 is what a newer build that appended a third variant writes, and this
   reader refuses it by name on every table read (the variable form, the fixed
   form and the message form) and asserts against it on every table save. */
#define _POSIX_C_SOURCE 200809L
#include "ProbeTable.h"
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

#define CHECK( x ) do { if ( !( x ) ) { fprintf( stderr, "line %d: %s\n", __LINE__, #x ); return 1; } } while ( 0 )

static const uint8_t wide_announcement[] = { WIDE_ANNOUNCEMENT };
static const uint8_t wide_inside[] = { WIDE_INSIDE };
static const uint8_t wide_past[] = { WIDE_PAST };

/* the one byte two saves disagree on, the low byte of the mask, or -1 */
static int64_t mask_byte( const uint8_t * a, const uint8_t * b, int64_t n )
{
    int64_t at = -1, i;
    for ( i = 0; i < n; i++ )
    {
        if ( a[i] != b[i] )
        {
            if ( at >= 0 ) { return -1; }
            at = i;
        }
    }
    return at;
}

/* a save of a mask with bit 2 set, in a forked child: true when it aborted */
static int save_aborts( int fixed_form )
{
    pid_t pid;
    int status = 0;
    fflush( stdout );
    fflush( stderr );
    pid = fork();
    if ( pid < 0 ) { return 0; }
    if ( pid == 0 )
    {
        struct rlimit no_core = { 0, 0 };
        Root past;
        static uint8_t out[4096];
        setrlimit( RLIMIT_CORE, &no_core );
        if ( freopen( "/dev/null", "w", stderr ) == NULL ) { _exit( 3 ); }
        root_reset( &past );
        past.direct = ( UINT64_C( 1 ) << 2 ) | 1u;
        if ( fixed_form ) { (void) root_fixed_save( &past, 1, out, (int64_t) sizeof( out ) ); }
        else { (void) root_save( &past, out, (int64_t) sizeof( out ) ); }
        _exit( 0 );
    }
    if ( waitpid( pid, &status, 0 ) != pid ) { return 0; }
    return WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT;
}

int main( void )
{
    Root one, two, back;
    TableReport report;
    TableFixedEntry plan[64];
    static uint8_t a[4096], b[4096];
    int64_t n, at;

    root_reset( &one );
    one.direct = 1u;
    root_reset( &two );
    two.direct = 2u;

    /* THE VARIABLE FORM: the twin reads, the planted bit refuses */
    n = root_measure( &one );
    CHECK( n > 0 && n <= (int64_t) sizeof( a ) && root_measure( &two ) == n );
    CHECK( root_save( &one, a, n ) == n && root_save( &two, b, n ) == n );
    at = mask_byte( a, b, n );
    CHECK( at >= 0 && a[at] == 1u );
    memset( &report, 0, sizeof( report ) );
    root_reset( &back );
    CHECK( root_load( &back, a, n, &report ) && !report.refused && back.direct == 1u );
    a[at] = (uint8_t) ( ( 1u << 2 ) | 1u );
    memset( &report, 0, sizeof( report ) );
    root_reset( &back );
    (void) root_load( &back, a, n, &report );
    CHECK( report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OUT_OF_RANGE );

    /* THE FIXED FORM: the same, on the record */
    n = root_fixed_measure( 1 );
    CHECK( n > 0 && n <= (int64_t) sizeof( a ) );
    CHECK( root_fixed_save( &one, 1, a, n ) == n && root_fixed_save( &two, 1, b, n ) == n );
    at = mask_byte( a, b, n );
    CHECK( at >= 0 && a[at] == 1u );
    memset( &report, 0, sizeof( report ) );
    CHECK( root_fixed_load( &back, 1, a, n, plan, 64, NULL, &report ) == 1 && !report.refused && back.direct == 1u );
    a[at] = (uint8_t) ( ( 1u << 2 ) | 1u );
    memset( &report, 0, sizeof( report ) );
    CHECK( root_fixed_load( &back, 1, a, n, plan, 64, NULL, &report ) < 0 );
    CHECK( report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OUT_OF_RANGE );

    /* THE MESSAGE FORM: a newer writer announces a three-bit mask, and this
       reader holds each value to its own two bits */
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocab = table_vocabulary( entries, kTableMessageEntriesHere );
        int64_t count = 1;
        memset( &report, 0, sizeof( report ) );
        CHECK( announce_read( &vocab, wide_announcement, (int64_t) sizeof( wide_announcement ), &report ) );
        memset( &report, 0, sizeof( report ) );
        CHECK( root_load_messages( &back, &count, &vocab, wide_inside, (int64_t) sizeof( wide_inside ), &report ) );
        CHECK( count == 1 && !report.refused && back.direct == 1u );
        count = 1;
        memset( &report, 0, sizeof( report ) );
        (void) root_load_messages( &back, &count, &vocab, wide_past, (int64_t) sizeof( wide_past ), &report );
        CHECK( report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OUT_OF_RANGE );
    }

#ifndef NDEBUG
    /* AND NO SAVE WRITES ONE: C is an assert leg (docs/SPEC.md §5.1) */
    CHECK( save_aborts( 0 ) );
    CHECK( save_aborts( 1 ) );
#endif
    puts( "flags past W refused on every read and asserted on every save" );
    return 0;
}
