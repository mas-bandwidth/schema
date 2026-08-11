// golden.cpp — the A side: writes each space message body through the REAL
// hand-written encoders (game/Source/Messages.h WriteMessage, vendored
// core_serialize) and dumps the bytes. The golden is the real code path,
// untouched.
#include "Constants.h"
#include "Messages.h"
#include "shared.h"
#include <cstdio>
#include <cstring>
#include <cstdlib>

// core.h declares this and core.cpp defines it; the harness links no space
// .cpps, so it supplies the definition — asserts stay LIVE and abort loudly.
static void harness_assert( const char * condition, const char * function, const char * file, int line )
{
    fprintf( stderr, "assert failed: %s at %s (%s:%d)\n", condition, function, file, line );
    abort();
}
void (*core_assert_function_pointer)( const char *, const char *, const char *, int ) = harness_assert;

static uint8_t buffer[NETWORK_MAX_MESSAGE_SIZE];

static void dump( const char * name, int idx, const uint8_t * p, int n )
{
    char path[512];
    snprintf( path, sizeof(path), OUT_DIR "/%s-%d.bin", name, idx );
    FILE * f = fopen( path, "wb" );
    if ( !f ) { fprintf( stderr, "cannot open %s\n", path ); return; }
    fwrite( p, 1, (size_t) n, f );
    fclose( f );
    printf( "A %s-%d: %d bytes\n", name, idx, n );
}

static int write_msg( Message * m )
{
    memset( buffer, 0, sizeof(buffer) );
    int bytes = 0;
    WriteMessage( m, &bytes, buffer );
    return bytes;
}

int main()
{
    for ( int i = 0; i < 3; i++ )
    {
        TestMessage m; m.sequence = TESTS[i].seq; m.a = TESTS[i].a; m.b = TESTS[i].b; m.c = TESTS[i].c;
        dump( "test", i, buffer, write_msg( &m ) );
    }
    for ( int i = 0; i < 4; i++ )
    {
        static BlockMessage m;
        m.dataBytes = BLOCK_LENS[i];
        fill_pattern( m.data, m.dataBytes, (uint64_t)( i + 1 ) );
        dump( "block", i, buffer, write_msg( &m ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        SynchronizeMessage m; m.clientFrame = SYNCS[i].frame; m.synchronizeSequence = SYNCS[i].seq;
        dump( "synchronize", i, buffer, write_msg( &m ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        TimescaleMessage m; m.timescale = TSS[i].ts; m.rtt = TSS[i].rtt; m.jitter = TSS[i].jitter;
        dump( "timescale", i, buffer, write_msg( &m ) );
    }
    {
        HeartbeatMessage m;
        dump( "heartbeat", 0, buffer, write_msg( &m ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        static UpdateConfigMessage m;
        m.size = UCS[i].size; m.hash = UCS[i].hash;
        fill_pattern( m.data, m.size, (uint64_t)( 100 + i ) );
        dump( "updateconfig", i, buffer, write_msg( &m ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        PlayerObjectMessage m; m.handle.SetValue( HANDLES[i] );
        dump( "playerobject", i, buffer, write_msg( &m ) );
    }
    return 0;
}
