// candidate.cpp — the B side: writes the same instances through the
// schema-GENERATED encoders (gen/SpaceMessages.h) against MODERN serialize,
// and dumps the bytes for comparison against the A goldens.
#include "MessagesWire.h"
#include "shared.h"
#include <cstdio>
#include <cstring>

using namespace space;

static uint8_t buffer[262144];

static void dump( const char * name, int idx, const uint8_t * p, int n )
{
    char path[512];
    snprintf( path, sizeof(path), OUT_DIR "/%s-%d.bin", name, idx );
    FILE * f = fopen( path, "wb" );
    if ( !f ) { fprintf( stderr, "cannot open %s\n", path ); return; }
    fwrite( p, 1, (size_t) n, f );
    fclose( f );
    printf( "B %s-%d: %d bytes\n", name, idx, n );
}

template <typename F>
static int write_body( F f )
{
    memset( buffer, 0, sizeof(buffer) );
    serialize::WriteStream stream( buffer, sizeof(buffer) );
    if ( !f( stream ) ) { fprintf( stderr, "write failed\n" ); return -1; }
    stream.Flush();
    int bytes = stream.GetBytesProcessed();
    // space's WriteMessage pads an empty body to 1 byte (Messages.h:334) —
    // mirror that packet-layer rule here so the empty Heartbeat compares.
    if ( bytes < 1 )
        bytes = 1;
    return bytes;
}

int main()
{
    for ( int i = 0; i < 3; i++ )
    {
        Test m; m.sequence = TESTS[i].seq; m.a = TESTS[i].a; m.b = TESTS[i].b; m.c = TESTS[i].c;
        dump( "test", i, buffer, write_body( [&]( serialize::WriteStream & s ){ return WriteTest( s, m ); } ) );
    }
    for ( int i = 0; i < 4; i++ )
    {
        static Block m;
        m.data_length = BLOCK_LENS[i];
        fill_pattern( m.data, m.data_length, (uint64_t)( i + 1 ) );
        dump( "block", i, buffer, write_body( [&]( serialize::WriteStream & s ){ return WriteBlock( s, m ); } ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        Synchronize m; m.client_frame = SYNCS[i].frame; m.synchronize_sequence = SYNCS[i].seq;
        dump( "synchronize", i, buffer, write_body( [&]( serialize::WriteStream & s ){ return WriteSynchronize( s, m ); } ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        Timescale m; m.time_scale = TSS[i].ts; m.rtt = (uint32_t) TSS[i].rtt; m.jitter = (uint32_t) TSS[i].jitter;
        dump( "timescale", i, buffer, write_body( [&]( serialize::WriteStream & s ){ return WriteTimescale( s, m ); } ) );
    }
    {
        Heartbeat m;
        dump( "heartbeat", 0, buffer, write_body( [&]( serialize::WriteStream & s ){ return WriteHeartbeat( s, m ); } ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        static UpdateConfig m;
        m.hash = UCS[i].hash; m.data_length = UCS[i].size;
        fill_pattern( m.data, m.data_length, (uint64_t)( 100 + i ) );
        dump( "updateconfig", i, buffer, write_body( [&]( serialize::WriteStream & s ){ return WriteUpdateConfig( s, m ); } ) );
    }
    for ( int i = 0; i < 3; i++ )
    {
        PlayerObject m; m.handle = HANDLES[i];
        dump( "playerobject", i, buffer, write_body( [&]( serialize::WriteStream & s ){ return WritePlayerObject( s, m ); } ) );
    }
    return 0;
}
