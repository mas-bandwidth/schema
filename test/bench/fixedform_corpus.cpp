// THE FIXED FORM'S CORPUS, written by the C++ REFERENCE (docs/SPEC-TABLES.md
// §3.4). The paired bench's own sixty-four logical records — decoded from the
// canonical PACKET corpus, so no field name and no independent value generator
// lives here — saved with the reference's form-3 writer.
//
// It answers ONE file — <out.bin>, the bytes another port's writer must produce
// EXACTLY and its reader must read back.
// The VALUES those bytes carry are stated by the PACKET WIRE and not by a file
// beside them: a port reads the same sixty-four records off the canonical
// packet corpus with its own packet codec and off this file with its fixed one,
// and the two have to agree field for field. That is §3.4's PAIRED CORPUS in
// its own terms, and it is a stronger oracle than a JSON dump of the same
// values, because the two decoders share no code at all.
//
// The reference also reads its own file back through its own plan-driven loop
// before writing it, so a corpus that ships is one the reference agrees
// with end to end.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>
#include "BenchWire.h"
#include "BenchTable.h"
#include "FixedTableTable.h"

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

int main( int argc, char ** argv )
{
    if ( argc != 3 ) { std::fprintf( stderr, "usage: %s <variants.bin> <out.bin>\n", argv[0] ); return 1; }
    std::vector<uint8_t> packet;
    if ( !read_file( argv[1], packet ) ) { std::fprintf( stderr, "cannot read %s\n", argv[1] ); return 1; }
    constexpr size_t Count = 64;
    const size_t stride = packet.size() / Count;
    if ( stride * Count != packet.size() ) { std::fprintf( stderr, "not 64 records\n" ); return 1; }

    std::vector<bench::FixedTable> values( Count );
    alignas( 8 ) uint8_t source[65536] = {};
    for ( size_t k = 0; k < Count; ++k )
    {
        std::memcpy( source, packet.data() + k * stride, stride );
        bench::BenchMixed value;
        serialize::ReadStream rs( source, (int) stride );
        if ( !bench::ReadBenchMixed( rs, value ) ) { std::fprintf( stderr, "packet read %zu failed\n", k ); return 1; }
        values[k].value = value;
    }

    const int64_t need = bench::FixedTableFixedMeasure( (int64_t) Count );
    std::vector<uint8_t> out( (size_t) need );
    const int64_t wrote = bench::FixedTableFixedSave( values.data(), (int64_t) Count, out.data(), need );
    if ( wrote != need ) { std::fprintf( stderr, "fixed save failed: %lld\n", (long long) wrote ); return 1; }

    FILE * f = std::fopen( argv[2], "wb" );
    if ( !f ) return 1;
    const bool ok = std::fwrite( out.data(), 1, out.size(), f ) == out.size();
    if ( std::fclose( f ) != 0 || !ok ) return 1;

    std::printf( "fixed corpus: %zu records, layout %lld bytes, body %lld, record %lld, total %lld, hash 0x%016llx\n",
                 Count, (long long) bench::FixedTableFixedLayoutBytes,
                 (long long) bench::FixedTableFixedBodyBytes,
                 (long long) bench::FixedTableFixedRecordBytes,
                 (long long) need, (unsigned long long) bench::FixedTableFixedHash );

    // and it reads back through the reference's OWN plan-driven loop
    std::vector<bench::FixedTable> back( Count );
    std::vector<bench::TableFixedEntry> plan( 8192 );
    bench::TableReport report;
    const int64_t n = bench::FixedTableFixedLoad( back.data(), (int64_t) Count, out.data(), need,
                                                  plan.data(), (int32_t) plan.size(), &report );
    if ( n != (int64_t) Count ) { std::fprintf( stderr, "fixed load failed: %lld\n", (long long) n ); return 1; }
    for ( size_t k = 0; k < Count; ++k )
    {
        alignas( 8 ) uint8_t a[65536] = {}, b[65536] = {};
        const int64_t sa = bench::BenchMixedSave( values[k].value, a, sizeof( a ) );
        const int64_t sb = bench::BenchMixedSave( back[k].value, b, sizeof( b ) );
        if ( sa <= 0 || sa != sb || std::memcmp( a, b, (size_t) sa ) )
        {
            std::fprintf( stderr, "record %zu did not survive the fixed round trip\n", k );
            return 1;
        }
    }
    std::printf( "reference round trip: 64/64 identical; unknown %d kind_mismatch %d clamped %d widened %d\n",
                 (int) report.unknown, (int) report.kind_mismatch, (int) report.clamped, (int) report.widened );

    return 0;
}
