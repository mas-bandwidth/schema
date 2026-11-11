// THE C++ FIXED FORM CROSSING THE BYTE ORDER (docs/FIXED-FORM-ALGORITHM.md:
// every integer on the wire is little-endian). `write <file>` saves one record;
// `check <file>` loads it and holds every field by value. The
// tables-big-endian-tables row writes on the little-endian host and checks
// under qemu-s390x, then writes under qemu-s390x and checks on the host, so a
// read that lands the wire in host order is red in one direction or the other.
#include <cstdio>
#include <cstring>
#include <cstdint>
#include "FixedEndianTable.h"

using namespace tblfixedendian;

static void fill( ByteOrderProbe & v )
{
    ByteOrderProbeReset( v );
    v.lead = 0x12345678u;
    v.a = 0x9ABC;
    v.b = 0xDEADBEEFu;
    v.c = 0x0123456789ABCDEFull;
    v.d = -2;
    v.g = -300;
    v.e = 1.5f;
    v.f = -2.5;
    v.vals[0] = 1;
    v.vals[1] = 0x01020304u;
    v.vals[2] = 3;
    v.wide[0] = -5;
    v.wide[1] = 0x0102030405060708ll;
    std::memcpy( v.label, "hi", 2 );
    v.label_length = 2;
    v.ok = true;
    v.tail = 0x7F;
}

static int write_file( const char * path )
{
    static uint8_t file[1024];
    ByteOrderProbe v;
    fill( v );
    const int64_t need = ByteOrderProbeFixedMeasure( 1 );
    if ( need <= 0 || need > (int64_t) sizeof( file ) || ByteOrderProbeFixedSave( &v, 1, file, (int64_t) sizeof( file ) ) != need )
    {
        std::printf( "FAIL: fixed endian: save\n" );
        return 1;
    }
    // THE FILE IS LITTLE-ENDIAN WHATEVER THE HOST.
    const uint32_t layout_bytes = TableFixedGet32( file + kTableFixedHeaderBytes );
    const uint8_t * body = file + kTableFixedHeaderBytes + 4 + layout_bytes + 8;
    if ( body[0] != 0x78 || body[1] != 0x56 || body[2] != 0x34 || body[3] != 0x12 )
    {
        std::printf( "FAIL: fixed endian: the saved body is not little-endian\n" );
        return 1;
    }
    FILE * f = std::fopen( path, "wb" );
    if ( f == NULL || std::fwrite( file, 1, (size_t) need, f ) != (size_t) need )
    {
        std::printf( "FAIL: fixed endian: write %s\n", path );
        if ( f != NULL ) { std::fclose( f ); }
        return 1;
    }
    std::fclose( f );
    std::printf( "fixed endian: wrote %s\n", path );
    return 0;
}

static int check_file( const char * path )
{
    static uint8_t file[1024];
    FILE * f = std::fopen( path, "rb" );
    if ( f == NULL )
    {
        std::printf( "FAIL: fixed endian: open %s\n", path );
        return 1;
    }
    const size_t n = std::fread( file, 1, sizeof( file ), f );
    std::fclose( f );
    ByteOrderProbe want;
    fill( want );
    ByteOrderProbe got;
    ByteOrderProbeReset( got );
    static TableFixedEntry plan[256];
    TableReport report;
    if ( ByteOrderProbeFixedLoad( &got, 1, file, (int64_t) n, plan, 256, NULL, &report ) != 1 || report.refused )
    {
        std::printf( "FAIL: fixed endian: load %s\n", path );
        return 1;
    }
    int failures = 0;
#define FIELD( x ) do { if ( !( got.x == want.x ) ) { std::printf( "FAIL: fixed endian: %s\n", #x ); failures++; } } while ( 0 )
    FIELD( lead ); FIELD( a ); FIELD( b ); FIELD( c ); FIELD( d ); FIELD( g ); FIELD( e ); FIELD( f );
    FIELD( vals[0] ); FIELD( vals[1] ); FIELD( vals[2] ); FIELD( wide[0] ); FIELD( wide[1] );
    FIELD( label_length ); FIELD( label[0] ); FIELD( label[1] ); FIELD( label[2] ); FIELD( ok ); FIELD( tail );
#undef FIELD
    if ( failures != 0 ) { return 1; }
    std::printf( "fixed endian: checked %s\n", path );
    return 0;
}

int main( int argc, char ** argv )
{
    if ( argc == 3 && std::strcmp( argv[1], "write" ) == 0 ) { return write_file( argv[2] ); }
    if ( argc == 3 && std::strcmp( argv[1], "check" ) == 0 ) { return check_file( argv[2] ); }
    std::printf( "usage: %s write|check <file>\n", argv[0] );
    return 2;
}
