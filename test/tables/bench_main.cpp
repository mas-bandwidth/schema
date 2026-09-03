// THE TABLE WIRE's C++ BENCH, and the reason it exists is comparison rather
// than a number: it is the LIKE-FOR-LIKE half of test/go-tables/bench_test.go —
// same golden, same three operations plus the round trip, same order, same warm
// buffer — so a port's numbers stand beside the reference's over the same
// bytes and the ratio means something.
//
// What it measures is the generated codec and nothing around it: no file I/O in
// the loop, no allocation, no framing. The question a ratio answers is whether
// a consumer of this format pays for the language or for the format, and a
// harness that timed anything else would answer a different one.
//
// It is an ITERATION instrument (BENCH-STANDARD.md's distinction): it runs on a
// workstation to compare two ports on one machine, and it certifies nothing.

#include <chrono>
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <vector>

#include "TablesTable.h"

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) return false;
    fseek( file, 0, SEEK_END );
    long size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out.resize( size > 0 ? (size_t) size : 0 );
    bool ok = size == 0 || fread( out.data(), 1, (size_t) size, file ) == (size_t) size;
    fclose( file );
    return ok;
}

// One measured run: `iterations` passes over `body`, reported as nanoseconds
// per operation. The clock is read twice and never inside the loop.
template <typename Body>
static double measure( int64_t iterations, Body body )
{
    const std::chrono::steady_clock::time_point start = std::chrono::steady_clock::now();
    for ( int64_t i = 0; i < iterations; i++ ) { body(); }
    const std::chrono::steady_clock::time_point end = std::chrono::steady_clock::now();
    return (double) std::chrono::duration_cast<std::chrono::nanoseconds>( end - start ).count() / (double) iterations;
}

int main( int argc, char ** argv )
{
    const char * path = argc > 1 ? argv[1] : "testdata/wire/tables/root_full.bin";
    int64_t iterations = argc > 2 ? strtoll( argv[2], NULL, 10 ) : 2000000;

    std::vector<uint8_t> wire;
    if ( !slurp( path, wire ) )
    {
        fprintf( stderr, "bench: cannot read %s\n", path );
        return 1;
    }

    static tabledemo::RootConfig value;
    tabledemo::TableReport report;
    if ( !tabledemo::RootConfigLoad( value, wire.data(), (int64_t) wire.size(), &report ) )
    {
        fprintf( stderr, "bench: the golden does not load\n" );
        return 1;
    }
    const int64_t size = tabledemo::RootConfigMeasure( value );
    std::vector<uint8_t> buffer( (size_t) size * 2 );

    // a warm pass, so neither leg pays a first-touch page fault inside a timing
    // window the other does not
    for ( int i = 0; i < 1000; i++ )
    {
        tabledemo::RootConfigLoad( value, wire.data(), (int64_t) wire.size(), &report );
        tabledemo::RootConfigSave( value, buffer.data(), size );
    }

    volatile int64_t escape = 0;

    const double load = measure( iterations, [&] {
        tabledemo::TableReport inner;
        escape += tabledemo::RootConfigLoad( value, wire.data(), (int64_t) wire.size(), &inner ) ? 1 : 0;
    } );
    tabledemo::RootConfigLoad( value, wire.data(), (int64_t) wire.size(), &report );

    const double measured = measure( iterations, [&] {
        escape += tabledemo::RootConfigMeasure( value );
    } );

    const double saved = measure( iterations, [&] {
        escape += tabledemo::RootConfigSave( value, buffer.data(), size );
    } );

    const double round = measure( iterations, [&] {
        tabledemo::TableReport inner;
        tabledemo::RootConfigLoad( value, wire.data(), (int64_t) wire.size(), &inner );
        const int64_t bytes = tabledemo::RootConfigMeasure( value );
        escape += tabledemo::RootConfigSave( value, buffer.data(), bytes );
    } );

    printf( "RootConfigLoad       %8.2f ns/op\n", load );
    printf( "RootConfigMeasure    %8.2f ns/op\n", measured );
    printf( "RootConfigSave       %8.2f ns/op\n", saved );
    printf( "RootConfigRoundTrip  %8.2f ns/op\n", round );
    (void) escape;
    return 0;
}
