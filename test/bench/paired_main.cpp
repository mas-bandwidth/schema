// Build the table half from the unchanged canonical packet corpus. No field
// names or independent value generator live here. Each decoded object is
// saved on both wires, and table load -> packet save must reproduce its source.
#include <algorithm>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>
#include "BenchWire.h"
#include "BenchTable.h"

static bool read_file( const char * path, std::vector<uint8_t> & data )
{
    FILE * f = std::fopen( path, "rb" );
    if ( !f ) return false;
    uint8_t b[65536];
    size_t n;
    while ( ( n = std::fread( b, 1, sizeof( b ), f ) ) != 0 )
        data.insert( data.end(), b, b + n );
    const bool ok = !std::ferror( f );
    std::fclose( f );
    return ok;
}

static bool pin_or_verify( const char * path, const std::vector<uint8_t> & data, bool pin )
{
    if ( !pin )
    {
        std::vector<uint8_t> got;
        if ( read_file( path, got ) && got == data ) return true;
        std::fprintf( stderr, "paired corpus mismatch: %s\n", path );
        return false;
    }
    FILE * f = std::fopen( path, "wb" );
    if ( !f ) return false;
    const bool ok = std::fwrite( data.data(), 1, data.size(), f ) == data.size();
    return std::fclose( f ) == 0 && ok;
}

int main( int argc, char ** argv )
{
    if ( argc > 2 || ( argc == 2 && std::strcmp( argv[1], "pin" ) && std::strcmp( argv[1], "verify" ) ) )
    {
        std::fprintf( stderr, "usage: %s [pin|verify]\n", argv[0] );
        return 1;
    }
    const bool pin = argc == 2 && !std::strcmp( argv[1], "pin" );
    constexpr size_t Count = 64, Capacity = 65536;
    std::vector<uint8_t> packet, packet_golden;
    if ( !read_file( "bench/corpus/variants/bench_mixed.variants.bin", packet ) ||
         !read_file( "testdata/wire/bench_mixed.bin", packet_golden ) ||
         packet_golden.empty() || packet.size() != Count * packet_golden.size() ||
         std::memcmp( packet.data(), packet_golden.data(), packet_golden.size() ) )
    {
        std::fprintf( stderr, "canonical packet corpus is missing or not 64 records with its pinned first record\n" );
        return 1;
    }
    const size_t stride = packet_golden.size();
    std::vector<uint8_t> table, lengths, golden;
    size_t smallest = Capacity, largest = 0;
    alignas( 8 ) uint8_t source[Capacity + 8] = {}, roundtrip[Capacity] = {}, wire[Capacity] = {}, twin[Capacity] = {};
    if ( stride > Capacity ) return 1;
    for ( size_t k = 0; k < Count; ++k )
    {
        std::memcpy( source, packet.data() + k * stride, stride );
        bench::BenchMixed value;
        serialize::ReadStream rs( source, (int) stride );
        if ( !bench::ReadBenchMixed( rs, value ) ) return 1;
        const int64_t size = bench::BenchMixedSave( value, wire, Capacity );
        if ( size <= 0 || size > (int64_t) Capacity || bench::BenchMixedMeasure( value ) != size ) return 1;
        bench::BenchMixed decoded;
        bench::BenchMixedReset( decoded );
        bench::TableReport report;
        if ( !bench::BenchMixedLoad( decoded, wire, size, &report ) || report.malformed ||
             report.unknown || report.kind_mismatch || report.clamped || report.duplicate )
        {
            std::fprintf( stderr, "table record %zu failed clean load\n", k );
            return 1;
        }
        if ( bench::BenchMixedSave( decoded, twin, Capacity ) != size || std::memcmp( wire, twin, (size_t) size ) ) return 1;
        serialize::WriteStream ws( roundtrip, Capacity );
        if ( !bench::WriteBenchMixed( ws, decoded ) ) return 1;
        ws.Flush();
        if ( ws.GetBytesProcessed() != (int64_t) stride || std::memcmp( source, roundtrip, stride ) )
        {
            std::fprintf( stderr, "record %zu: table changed the canonical packet's logical data\n", k );
            return 1;
        }
        for ( unsigned shift = 0; shift != 32; shift += 8 ) lengths.push_back( uint8_t( uint32_t( size ) >> shift ) );
        table.insert( table.end(), wire, wire + size );
        if ( k == 0 ) golden.assign( wire, wire + size );
        smallest = std::min( smallest, (size_t) size );
        largest = std::max( largest, (size_t) size );
    }
    if ( !pin_or_verify( "bench/paired/corpus/bench_table.bin", golden, pin ) ||
         !pin_or_verify( "bench/paired/corpus/bench_table.variants.bin", table, pin ) ||
         !pin_or_verify( "bench/paired/corpus/bench_table.lengths", lengths, pin ) ) return 1;
    std::printf( "paired corpus %s: 64 identical logical records; packet %zu bytes, table %zu..%zu (mean %.6f) bytes\n",
                 pin ? "pinned" : "verified", stride, smallest, largest, double( table.size() ) / Count );
    return 0;
}
