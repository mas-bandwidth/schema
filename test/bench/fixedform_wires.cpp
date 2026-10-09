// THE THREE WIRES, SIDE BY SIDE (docs/SPEC-TABLES.md §3.4).
//
// The paired bench (bench/paired) times the PACKET wire and the form-1 table
// wire over identical logical records. It does NOT time form 3: its C++ table
// runner calls BenchMixedSave/Load (form 1) and nothing else calls
// *FixedSave/*FixedLoad except a conformance test. This binary adds the third
// row, in ONE process, over the SAME 64 records, with the SAME loop structure
// as bench/cpp/bench_main.cpp and bench/tables/cpp/table_main.cpp:
//   - the committed variant corpus drives it (no independent value generator)
//   - a gate runs before every clock: each path must reproduce its own bytes
//   - the loops are escape-barriered and sink-folded
//   - 1 discarded warmup + N measured runs, median reported beside min/max
//
// checks axis, matching bench/paired: packet = SERIALIZE_RELEASE (removed),
// table form 1 and form 3 = contract (their own validation stays in). For form
// 3 that includes §3.4's straight-line read-side bounds, which are what a
// caller gets and therefore what a measurement of the read must carry.
//
// THIS IS NOT THE FORM'S RULING MEASUREMENT. That is fixedform_measure.cpp
// beside this file: the plan-driven reader against straight-line
// constant-offset loads, the ratio the one-reader-path decision was bought
// with. It asks whether the plan costs too much; this asks what the wire is
// worth against the other two.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <chrono>
#include <algorithm>
#include <vector>

#include "BenchWire.h"          // packet codec: bench::ReadBenchMixed / WriteBenchMixed
#include "BenchTable.h"         // form 1: bench::BenchMixedSave / BenchMixedLoad
#include "FixedTableTable.h"    // form 3: bench::FixedTableFixedSave / FixedTableFixedLoad

static volatile uint64_t g_sink = 0;

inline void bench_escape( const void * data )
{
    asm volatile( "" : : "g"( data ) : "memory" );
}

static double time_now()
{
    return std::chrono::duration<double>( std::chrono::steady_clock::now().time_since_epoch() ).count();
}

static const int NumVariants = 64;
static const int MaxRuns = 16;

static int g_runs = 5;
static long g_packet_iters = 400000;
static long g_table_iters  = 200000;
static long g_fixed_iters  = 200000;

struct RunStats { double median, lo, hi, spread_pct; };

static RunStats run_stats( double * rates, int n )
{
    std::vector<double> v( rates, rates + n );
    std::sort( v.begin(), v.end() );
    RunStats s;
    s.median = ( n & 1 ) ? v[n / 2] : 0.5 * ( v[n / 2 - 1] + v[n / 2] );
    s.lo = v.front();
    s.hi = v.back();
    s.spread_pct = ( s.hi - s.lo ) / s.median * 100.0;
    return s;
}

// One row: rate is records/second. Printed as ns/record.
static void report( const char * wire, const char * path, RunStats s, double bytes_per_record )
{
    printf( "ROW\t%s\t%s\t%.3f\t%.3f\t%.3f\t%.2f\t%.3f\n",
            wire, path,
            1e9 / s.median,      // median ns/record
            1e9 / s.hi,          // best (fastest rate -> lowest ns)
            1e9 / s.lo,          // worst
            s.spread_pct,
            bytes_per_record );
}

static bool read_file( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( !f ) { fprintf( stderr, "cannot open %s\n", path ); return false; }
    uint8_t b[65536];
    size_t n;
    while ( ( n = fread( b, 1, sizeof( b ), f ) ) != 0 ) out.insert( out.end(), b, b + n );
    fclose( f );
    return true;
}

static const int64_t BufferSize = 1 << 20;
static uint8_t g_buffer[BufferSize];
static uint8_t g_twin[BufferSize];

static bench::BenchMixed g_instances[NumVariants];
static bench::FixedTable g_fixed_in[NumVariants];
static bench::FixedTable g_fixed_out[NumVariants];

// form-1 table bytes for each record, produced by the codec itself
static std::vector<uint8_t> g_t1[NumVariants];

// form-3 FILE holding all 64 records
static std::vector<uint8_t> g_f3_batch;
// form-3 FILE holding exactly one record, per record (block rides each one)
static std::vector<uint8_t> g_f3_single[NumVariants];

int main( int argc, char ** argv )
{
    if ( argc > 1 ) g_runs = atoi( argv[1] );
    if ( argc > 2 ) g_packet_iters = atol( argv[2] );
    if ( argc > 3 ) g_table_iters = atol( argv[3] );
    if ( argc > 4 ) g_fixed_iters = atol( argv[4] );
    if ( g_runs < 1 || g_runs > MaxRuns ) { fprintf( stderr, "runs 1..%d\n", MaxRuns ); return 1; }

    // ---- the corpus: the committed canonical packet variants ----
    std::vector<uint8_t> packet, golden;
    if ( !read_file( "bench/corpus/variants/bench_mixed.variants.bin", packet ) ) return 1;
    if ( !read_file( "testdata/wire/bench_mixed.bin", golden ) ) return 1;
    const int64_t packet_bytes = (int64_t) golden.size();
    if ( (int64_t) packet.size() != NumVariants * packet_bytes ||
         memcmp( packet.data(), golden.data(), (size_t) packet_bytes ) != 0 )
    {
        fprintf( stderr, "corpus is not 64 records with its pinned first record\n" );
        return 1;
    }

    // ---- decode, and GATE every path before any clock ----
    for ( int k = 0; k < NumVariants; k++ )
    {
        serialize::ReadStream rs( packet.data() + k * packet_bytes, (int) packet_bytes );
        if ( !bench::ReadBenchMixed( rs, g_instances[k] ) ) { fprintf( stderr, "packet decode %d failed\n", k ); return 1; }
        serialize::WriteStream ws( g_twin, BufferSize );
        if ( !bench::WriteBenchMixed( ws, g_instances[k] ) ) { fprintf( stderr, "packet encode %d failed\n", k ); return 1; }
        ws.Flush();
        if ( ws.GetBytesProcessed() != packet_bytes ||
             memcmp( g_twin, packet.data() + k * packet_bytes, (size_t) packet_bytes ) != 0 )
        { fprintf( stderr, "packet round-trip %d differs\n", k ); return 1; }
        g_fixed_in[k].value = g_instances[k];
    }

    // form 1: save each record, and prove load->save reproduces it
    double t1_total = 0;
    for ( int k = 0; k < NumVariants; k++ )
    {
        const int64_t n = bench::BenchMixedSave( g_instances[k], g_buffer, BufferSize );
        if ( n <= 0 ) { fprintf( stderr, "form1 save %d failed\n", k ); return 1; }
        g_t1[k].assign( g_buffer, g_buffer + n );
        t1_total += (double) n;
        bench::BenchMixed back;
        bench::TableReport rep;
        if ( !bench::BenchMixedLoad( back, g_t1[k].data(), n, &rep ) || rep.malformed )
        { fprintf( stderr, "form1 load %d failed\n", k ); return 1; }
        const int64_t m = bench::BenchMixedSave( back, g_twin, BufferSize );
        if ( m != n || memcmp( g_twin, g_t1[k].data(), (size_t) n ) != 0 )
        { fprintf( stderr, "form1 round-trip %d differs\n", k ); return 1; }
        // and the cross-wire oracle: form 1 -> packet must be the source bytes
        serialize::WriteStream ws( g_twin, BufferSize );
        if ( !bench::WriteBenchMixed( ws, back ) ) { fprintf( stderr, "form1->packet %d failed\n", k ); return 1; }
        ws.Flush();
        if ( memcmp( g_twin, packet.data() + k * packet_bytes, (size_t) packet_bytes ) != 0 )
        { fprintf( stderr, "form1->packet %d differs\n", k ); return 1; }
    }
    const double t1_bytes_per_record = t1_total / NumVariants;

    // form 3 batch: one FILE of all 64
    {
        const int64_t need = bench::FixedTableFixedMeasure( NumVariants );
        g_f3_batch.resize( (size_t) need );
        const int64_t n = bench::FixedTableFixedSave( g_fixed_in, NumVariants, g_f3_batch.data(), need );
        if ( n != need ) { fprintf( stderr, "form3 batch save failed\n" ); return 1; }
        std::vector<bench::TableFixedEntry> plan( 4096 );
        bench::TableReport rep;
        const int64_t got = bench::FixedTableFixedLoad( g_fixed_out, NumVariants, g_f3_batch.data(), n,
                                                        plan.data(), (int32_t) plan.size(), NULL, &rep );
        if ( got != NumVariants || rep.malformed || rep.refused )
        { fprintf( stderr, "form3 batch load failed (got %lld)\n", (long long) got ); return 1; }
        // cross-wire oracle: every form-3 record must re-encode to its packet source
        for ( int k = 0; k < NumVariants; k++ )
        {
            serialize::WriteStream ws( g_twin, BufferSize );
            if ( !bench::WriteBenchMixed( ws, g_fixed_out[k].value ) ) { fprintf( stderr, "form3->packet %d failed\n", k ); return 1; }
            ws.Flush();
            if ( ws.GetBytesProcessed() != packet_bytes ||
                 memcmp( g_twin, packet.data() + k * packet_bytes, (size_t) packet_bytes ) != 0 )
            { fprintf( stderr, "form3->packet %d DIFFERS\n", k ); return 1; }
        }
    }

    // form 3 single: one FILE per record (the block rides every record)
    const int64_t single_need = bench::FixedTableFixedMeasure( 1 );
    for ( int k = 0; k < NumVariants; k++ )
    {
        g_f3_single[k].resize( (size_t) single_need );
        if ( bench::FixedTableFixedSave( &g_fixed_in[k], 1, g_f3_single[k].data(), single_need ) != single_need )
        { fprintf( stderr, "form3 single save %d failed\n", k ); return 1; }
    }

    const double f3_batch_bytes_per_record = (double) bench::FixedTableFixedMeasure( NumVariants ) / NumVariants;
    const double f3_single_bytes_per_record = (double) single_need;

    printf( "SHAPE\tpacket_bytes=%lld\tform1_mean_bytes=%.4f\tform3_record_bytes=%lld\tform3_layout_bytes=%lld\tform3_batch64_bytes_per_record=%.4f\tform3_single_bytes_per_record=%.1f\tplan_entries=%d\tplan_guarded=%d\n",
            (long long) packet_bytes, t1_bytes_per_record,
            (long long) bench::FixedTableFixedRecordBytes,
            (long long) bench::FixedTableFixedLayoutBytes,
            f3_batch_bytes_per_record, f3_single_bytes_per_record,
            (int) bench::FixedTableFixedPlanCount, (int) bench::FixedTableFixedPlanGuarded );
    fflush( stdout );

    double w[MaxRuns], rt[MaxRuns];

    // ================= PACKET =================
    for ( int run = -1; run < g_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < g_packet_iters; i++ )
        {
            serialize::WriteStream ws( g_buffer, BufferSize );
            if ( !bench::WriteBenchMixed( ws, g_instances[i & ( NumVariants - 1 )] ) ) return 1;
            ws.Flush();
            bench_escape( g_buffer );
            g_sink += (uint64_t) ws.GetBytesProcessed();
        }
        double t = time_now() - start;
        if ( run >= 0 ) w[run] = double( g_packet_iters ) / t;
    }
    {
        bench::BenchMixed out;
        for ( int run = -1; run < g_runs; run++ )
        {
            double start = time_now();
            for ( long i = 0; i < g_packet_iters; i++ )
            {
                const int k = i & ( NumVariants - 1 );
                serialize::ReadStream rs( packet.data() + k * packet_bytes, (int) packet_bytes );
                if ( !bench::ReadBenchMixed( rs, out ) ) return 1;
                serialize::WriteStream ws( g_buffer, BufferSize );
                if ( !bench::WriteBenchMixed( ws, out ) ) return 1;
                ws.Flush();
                bench_escape( g_buffer );
                g_sink += (uint64_t) ws.GetBytesProcessed();
            }
            double t = time_now() - start;
            if ( run >= 0 ) rt[run] = double( g_packet_iters ) / t;
        }
    }
    report( "packet", "write", run_stats( w, g_runs ), (double) packet_bytes );
    report( "packet", "round_trip", run_stats( rt, g_runs ), (double) packet_bytes );
    fflush( stdout );

    // ================= FORM 1 =================
    for ( int run = -1; run < g_runs; run++ )
    {
        double start = time_now();
        for ( long i = 0; i < g_table_iters; i++ )
        {
            const int64_t n = bench::BenchMixedSave( g_instances[i & ( NumVariants - 1 )], g_buffer, BufferSize );
            if ( n <= 0 ) return 1;
            bench_escape( g_buffer );
            g_sink += (uint64_t) n;
        }
        double t = time_now() - start;
        if ( run >= 0 ) w[run] = double( g_table_iters ) / t;
    }
    {
        bench::BenchMixed out;
        for ( int run = -1; run < g_runs; run++ )
        {
            double start = time_now();
            for ( long i = 0; i < g_table_iters; i++ )
            {
                const int k = i & ( NumVariants - 1 );
                bench::TableReport rep;
                if ( !bench::BenchMixedLoad( out, g_t1[k].data(), (int64_t) g_t1[k].size(), &rep ) ) return 1;
                const int64_t n = bench::BenchMixedSave( out, g_buffer, BufferSize );
                if ( n != (int64_t) g_t1[k].size() ) return 1;
                bench_escape( g_buffer );
                g_sink += (uint64_t) n;
            }
            double t = time_now() - start;
            if ( run >= 0 ) rt[run] = double( g_table_iters ) / t;
        }
    }
    report( "form1", "write", run_stats( w, g_runs ), t1_bytes_per_record );
    report( "form1", "round_trip", run_stats( rt, g_runs ), t1_bytes_per_record );
    fflush( stdout );

    // ================= FORM 3, batch of 64 (the FILE form as designed) =====
    // One call carries 64 records; the clock is divided by 64 so the row is
    // per-record like every other row. The block is written ONCE per call.
    {
        const int64_t need = bench::FixedTableFixedMeasure( NumVariants );
        const long calls = g_fixed_iters / NumVariants;
        for ( int run = -1; run < g_runs; run++ )
        {
            double start = time_now();
            for ( long c = 0; c < calls; c++ )
            {
                const int64_t n = bench::FixedTableFixedSave( g_fixed_in, NumVariants, g_buffer, BufferSize );
                if ( n != need ) return 1;
                bench_escape( g_buffer );
                g_sink += (uint64_t) n;
            }
            double t = time_now() - start;
            if ( run >= 0 ) w[run] = double( calls * NumVariants ) / t;
        }
        std::vector<bench::TableFixedEntry> plan( 4096 );
        for ( int run = -1; run < g_runs; run++ )
        {
            double start = time_now();
            for ( long c = 0; c < calls; c++ )
            {
                bench::TableReport rep;
                if ( bench::FixedTableFixedLoad( g_fixed_out, NumVariants, g_f3_batch.data(), (int64_t) g_f3_batch.size(),
                                                  plan.data(), (int32_t) plan.size(), NULL, &rep ) != NumVariants ) return 1;
                const int64_t n = bench::FixedTableFixedSave( g_fixed_out, NumVariants, g_buffer, BufferSize );
                if ( n != need ) return 1;
                bench_escape( g_buffer );
                g_sink += (uint64_t) n;
            }
            double t = time_now() - start;
            if ( run >= 0 ) rt[run] = double( calls * NumVariants ) / t;
        }
        report( "form3_batch64", "write", run_stats( w, g_runs ), f3_batch_bytes_per_record );
        report( "form3_batch64", "round_trip", run_stats( rt, g_runs ), f3_batch_bytes_per_record );
        fflush( stdout );
    }

    // ================= FORM 3, one record per call ========================
    // The same API called the way packet and form 1 are called: one record in,
    // one buffer out. The block rides EVERY record here, which is what it costs
    // when a fixed record is sent alone rather than in a file.
    {
        for ( int run = -1; run < g_runs; run++ )
        {
            double start = time_now();
            for ( long i = 0; i < g_fixed_iters; i++ )
            {
                const int64_t n = bench::FixedTableFixedSave( &g_fixed_in[i & ( NumVariants - 1 )], 1, g_buffer, BufferSize );
                if ( n != single_need ) return 1;
                bench_escape( g_buffer );
                g_sink += (uint64_t) n;
            }
            double t = time_now() - start;
            if ( run >= 0 ) w[run] = double( g_fixed_iters ) / t;
        }
        std::vector<bench::TableFixedEntry> plan( 4096 );
        bench::FixedTable out;
        for ( int run = -1; run < g_runs; run++ )
        {
            double start = time_now();
            for ( long i = 0; i < g_fixed_iters; i++ )
            {
                const int k = i & ( NumVariants - 1 );
                bench::TableReport rep;
                if ( bench::FixedTableFixedLoad( &out, 1, g_f3_single[k].data(), (int64_t) g_f3_single[k].size(),
                                                  plan.data(), (int32_t) plan.size(), NULL, &rep ) != 1 ) return 1;
                const int64_t n = bench::FixedTableFixedSave( &out, 1, g_buffer, BufferSize );
                if ( n != single_need ) return 1;
                bench_escape( g_buffer );
                g_sink += (uint64_t) n;
            }
            double t = time_now() - start;
            if ( run >= 0 ) rt[run] = double( g_fixed_iters ) / t;
        }
        report( "form3_single", "write", run_stats( w, g_runs ), f3_single_bytes_per_record );
        report( "form3_single", "round_trip", run_stats( rt, g_runs ), f3_single_bytes_per_record );
        fflush( stdout );
    }

    fprintf( stderr, "sink %llu\n", (unsigned long long) g_sink );
    return 0;
}
