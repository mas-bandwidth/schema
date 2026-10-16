#include <cstdio>
#include <vector>
#include <chrono>
#include "BenchTable.h"
#include "FixedTableTable.h"
using namespace bench;
static FixedTable g_v[64];
static TableFixedEntry g_plan[4096];
static inline void esc( void * p ) { asm volatile( "" : : "g"( p ) : "memory" ); }
static double now() { return std::chrono::duration<double>( std::chrono::steady_clock::now().time_since_epoch() ).count(); }
int main( int, char ** argv )
{
    FILE * f = fopen( argv[1], "rb" ); std::vector<uint8_t> b( 1 << 20 ); size_t n = fread( b.data(), 1, b.size(), f ); fclose( f ); b.resize( n );
    TableReport r; if ( FixedTableFixedLoad( g_v, 64, b.data(), (int64_t) n, g_plan, 4096, NULL, &r ) != 64 ) { puts( "load fail" ); return 1; }
    const long iters = 2000000;
    const uint32_t lb = TableFixedGet32( b.data() + kTableFixedHeaderBytes );
    const uint8_t * recs = b.data() + kTableFixedHeaderBytes + 4 + lb;
    for ( int pass = 0; pass < 5; ++pass ) {
    double best[3] = { 1e9, 1e9, 1e9 };
    for ( int round = 0; round < 3; ++round ) {
        double t = now();
        for ( long i = 0; i < iters; i += 64 ) { TableReport rr; for ( int k = 0; k < 64; ++k ) FixedTableFixedClamp( g_v[k], &rr ); esc( g_v ); esc( &rr ); }
        double c = ( now() - t ) / iters * 1e9; if ( c < best[0] ) best[0] = c;
        t = now();
        for ( long i = 0; i < iters; i += 64 ) { TableReport rr; for ( int k = 0; k < 64; ++k ) TableFixedRun( FixedTableFixedPlan, FixedTableFixedPlanCount, FixedTableFixedPlanGuarded, recs + k * FixedTableFixedRecordBytes + 8, (uint8_t *) &g_v[k], &rr, FixedTableFixedGuards ); esc( g_v ); esc( &rr ); }
        c = ( now() - t ) / iters * 1e9; if ( c < best[1] ) best[1] = c;
        t = now();
        for ( long i = 0; i < iters; i += 64 ) { TableReport rr; FixedTableFixedLoad( g_v, 64, b.data(), (int64_t) n, g_plan, 4096, NULL, &rr ); esc( g_v ); }
        c = ( now() - t ) / iters * 1e9; if ( c < best[2] ) best[2] = c;
    }
    printf( "clamp_only %.1f run_only %.1f load %.1f\n", best[0], best[1], best[2] );
    }
    return 0;
}
