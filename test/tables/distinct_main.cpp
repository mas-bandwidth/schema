// THE ID TABLE'S DISTINCTNESS CHECK, held to ONE VERDICT across BOTH of the
// walks TableOpen carries (docs/SPEC-TABLES.md §3: "a table that carries one
// id twice is malformed").
//
// TableOpen answers the question two ways — a hashed PROBE over a stack array
// up to kTableProbeBound entries, the pairwise walk above it — and the whole
// property is that a reader cannot tell which one ran. This test is the gate
// on that: it builds tables BY HAND, drives TableOpen directly, and holds the
// verdict against an independent O(n^2) reference over the same bytes.
//
// The check is READ SIDE, so it is unconditional (§3.4) and this file is
// compiled BOTH ways, with and without NDEBUG, by its Makefile leg. Nothing
// here asserts: a failure prints the case and returns non-zero, so a release
// build gates exactly as a debug build does.
//
// The cases that matter, and why each one is here:
//
//   - a table at EXACTLY the bound, all distinct              (probe, accept)
//   - a table one PAST the bound, all distinct             (fallback, accept)
//   - a repeat of entry 0 in the LAST slot, on both paths       (both, damaged)
//   - a repeat in the middle and an adjacent repeat, on both paths
//   - an ALL-COLLIDING table: n distinct ids the Fibonacci multiply lands in
//     ONE slot, which is the cluster a hostile writer builds. Accepted when
//     distinct, damaged when one of them repeats.
//   - id 0 and id 0xFFFFFFFFFFFFFFFF as ORDINARY entries, because an entry is
//     fnv1a64( name ) and nothing else (§3): no 64-bit value is a sentinel,
//     which is why the probe carries an occupancy bitmap.
//   - a sweep over every count from 0 to the bound + 4, each with no repeat
//     and then with a repeat planted at every position, compared against the
//     reference — the sweep is what proves the two walks agree rather than
//     that each one is separately plausible.
//
// It names ONE generated unit, for TableOpen and the id table, and no field of
// it: the check is a property of the wire's trailer, not of any schema.

#include <cstdio>
#include <cstdint>
#include <cstring>
#include <vector>

#include "TablesTable.h"

using tabledemo::TableIdTable;
using tabledemo::TableOpen;
using tabledemo::TableOpenVerdict;
using tabledemo::TableOpenOk;
using tabledemo::TableOpenDamaged;
using tabledemo::kTableProbeBound;
using tabledemo::kTableProbeSlots;

static int g_failures = 0;

// A WIRE THAT IS NOTHING BUT A TRAILER: the form byte, an empty root body, the
// entries, the count. TableOpen reads the form byte and the trailer and hands
// back the body's length, and it never walks the body, so an empty one is the
// smallest wire that puts a given id table in front of the check.
static std::vector<uint8_t> wire_of( const std::vector<uint64_t> & ids )
{
    std::vector<uint8_t> w;
    w.push_back( 1 );                                   // the FORM BYTE (§3)
    for ( size_t i = 0; i < ids.size(); i++ )
    {
        for ( int b = 0; b < 8; b++ ) { w.push_back( (uint8_t) ( ids[i] >> ( 8 * b ) ) ); }
    }
    const uint64_t count = (uint64_t) ids.size();
    for ( int b = 0; b < 8; b++ ) { w.push_back( (uint8_t) ( count >> ( 8 * b ) ) ); }
    return w;
}

// THE REFERENCE VERDICT, written the slowest way there is on purpose: every
// pair, no hash, no bound. It is what the two walks in TableOpen are held to.
static bool has_repeat( const std::vector<uint64_t> & ids )
{
    for ( size_t i = 1; i < ids.size(); i++ )
    {
        for ( size_t j = 0; j < i; j++ )
        {
            if ( ids[j] == ids[i] ) { return true; }
        }
    }
    return false;
}

static void expect( const char * name, const std::vector<uint64_t> & ids )
{
    const std::vector<uint8_t> w = wire_of( ids );
    TableIdTable table;
    int64_t body_bytes = -1;
    const TableOpenVerdict got = TableOpen( w.data(), (int64_t) w.size(), table, body_bytes );
    const TableOpenVerdict want = has_repeat( ids ) ? TableOpenDamaged : TableOpenOk;
    if ( got != want )
    {
        printf( "FAIL %s: count %d, verdict %d, expected %d\n",
                name, (int) ids.size(), (int) got, (int) want );
        g_failures++;
        return;
    }
    if ( want == TableOpenOk )
    {
        // a clean table also has to hand back the body it framed, and every
        // entry has to read back where the trailer put it.
        if ( body_bytes != 0 )
        {
            printf( "FAIL %s: body_bytes %lld, expected 0\n", name, (long long) body_bytes );
            g_failures++;
            return;
        }
        if ( table.count != (int64_t) ids.size() )
        {
            printf( "FAIL %s: table.count %lld, expected %d\n",
                    name, (long long) table.count, (int) ids.size() );
            g_failures++;
            return;
        }
        for ( size_t i = 0; i < ids.size(); i++ )
        {
            if ( table.at( (uint64_t) i + 1 ) != ids[i] )
            {
                printf( "FAIL %s: entry %d read back wrong\n", name, (int) i );
                g_failures++;
                return;
            }
        }
    }
}

// A DISTINCT ID PER POSITION, spread so the probe does ordinary work.
static std::vector<uint64_t> spread( int n )
{
    std::vector<uint64_t> ids;
    for ( int i = 0; i < n; i++ ) { ids.push_back( 0x9E3779B97F4A7C15ull * (uint64_t) ( i + 1 ) ^ 0x0123456789ABCDEFull ); }
    return ids;
}

// THE HOSTILE SHAPE. The probe's hash is the multiply TableIds::bucket_of
// interns with, and a multiply by an ODD constant is INVERTIBLE mod 2^64 — so
// a writer who wants every entry in one slot solves for it, and these are the
// ids that come out. n of them make one cluster of length n, which is the
// worst case the emitter's comment costs out.
static uint64_t multiply_inverse()
{
    const uint64_t k = 0x9E3779B97F4A7C15ull;
    uint64_t x = k;                                          // exact mod 2^3 for every odd k
    for ( int i = 0; i < 6; i++ ) { x = x * ( 2 - k * x ); }  // Newton on the 2-adics
    return x;
}

static std::vector<uint64_t> all_colliding( int n, uint32_t slot )
{
    const uint64_t inv = multiply_inverse();
    std::vector<uint64_t> ids;
    for ( int i = 0; i < n; i++ )
    {
        // id * k == ( slot << 56 ) | i, so the top eight bits are slot for all of them
        const uint64_t target = ( (uint64_t) slot << 56 ) | (uint64_t) i;
        ids.push_back( inv * target );
    }
    return ids;
}

static void check_collision_setup( const std::vector<uint64_t> & ids, uint32_t slot )
{
    for ( size_t i = 0; i < ids.size(); i++ )
    {
        const uint32_t s = uint32_t( ( ids[i] * 0x9E3779B97F4A7C15ull ) >> 56 ) & uint32_t( kTableProbeSlots - 1 );
        if ( s != slot )
        {
            printf( "FAIL collision setup: entry %d lands in slot %u, not %u\n", (int) i, s, slot );
            g_failures++;
            return;
        }
    }
}

int main()
{
    const int bound = (int) kTableProbeBound;

    // ---- the two paths, clean ----
    expect( "at the bound", spread( bound ) );
    expect( "one past the bound", spread( bound + 1 ) );
    expect( "well under the bound", spread( 53 ) );
    expect( "well over the bound", spread( bound * 3 ) );

    // ---- entry 0 repeated in the LAST slot, on both paths ----
    {
        std::vector<uint64_t> ids = spread( bound );
        ids[ bound - 1 ] = ids[0];
        expect( "probe: first id repeated last", ids );
    }
    {
        std::vector<uint64_t> ids = spread( bound + 1 );
        ids[ bound ] = ids[0];
        expect( "fallback: first id repeated last", ids );
    }

    // ---- a repeat in the middle, and an adjacent repeat, on both paths ----
    {
        std::vector<uint64_t> ids = spread( bound );
        ids[ bound / 2 ] = ids[ 3 ];
        expect( "probe: middle repeat", ids );
        ids = spread( bound );
        ids[ 41 ] = ids[ 40 ];
        expect( "probe: adjacent repeat", ids );
    }
    {
        std::vector<uint64_t> ids = spread( bound + 1 );
        ids[ bound / 2 ] = ids[ 3 ];
        expect( "fallback: middle repeat", ids );
        ids = spread( bound + 1 );
        ids[ 41 ] = ids[ 40 ];
        expect( "fallback: adjacent repeat", ids );
    }

    // ---- the smallest tables there are ----
    expect( "empty table", std::vector<uint64_t>() );
    expect( "one entry", std::vector<uint64_t>( 1, 0x1111ull ) );
    {
        std::vector<uint64_t> ids;
        ids.push_back( 7 ); ids.push_back( 7 );
        expect( "two entries, both the same", ids );
        ids[1] = 8;
        expect( "two entries, distinct", ids );
    }

    // ---- NO 64-BIT VALUE IS A SENTINEL (§3) ----
    {
        std::vector<uint64_t> ids = spread( 20 );
        ids[0] = 0;                             // a hash of 0 is an ordinary id
        ids[1] = 0xFFFFFFFFFFFFFFFFull;         // the node table's reserved id
        ids[2] = 0xFFFFFFFFFFFFFFFEull;         // the announcement's build version
        expect( "probe: 0 and the reserved ids as ordinary entries", ids );
        ids[19] = 0;
        expect( "probe: 0 repeated", ids );
        ids[19] = 0xFFFFFFFFFFFFFFFFull;
        expect( "probe: the reserved id repeated", ids );
    }
    {
        std::vector<uint64_t> ids = spread( bound + 2 );
        ids[0] = 0;
        ids[1] = 0xFFFFFFFFFFFFFFFFull;
        expect( "fallback: 0 and the reserved id as ordinary entries", ids );
        ids[ bound + 1 ] = 0;
        expect( "fallback: 0 repeated", ids );
    }

    // ---- THE HOSTILE CLUSTER: every entry in one slot ----
    for ( uint32_t slot = 0; slot < 3; slot++ )
    {
        const uint32_t s = slot == 2 ? (uint32_t) ( kTableProbeSlots - 1 ) : slot;
        std::vector<uint64_t> ids = all_colliding( bound, s );
        check_collision_setup( ids, s );
        expect( "probe: every id in one slot, distinct", ids );
        ids[ bound - 1 ] = ids[0];
        expect( "probe: every id in one slot, first repeated last", ids );
        ids = all_colliding( bound, s );
        ids[1] = ids[0];
        expect( "probe: every id in one slot, repeat at the front", ids );
        // the same shape one past the bound, where the pairwise walk runs and
        // the hash is not consulted at all
        ids = all_colliding( bound + 1, s );
        expect( "fallback: every id in one slot, distinct", ids );
        ids[ bound ] = ids[0];
        expect( "fallback: every id in one slot, first repeated last", ids );
    }

    // A CLUSTER THAT WRAPS the slot array: the probe's linear step is masked,
    // so a cluster starting at the last slot has to walk over the end and find
    // the repeat on the other side.
    {
        std::vector<uint64_t> ids = all_colliding( bound, (uint32_t) ( kTableProbeSlots - 1 ) );
        ids[ bound / 2 ] = ids[ bound / 2 - 1 ];
        expect( "probe: wrapping cluster, repeat inside it", ids );
    }

    // ---- THE SWEEP: every count either side of the bound, clean and with a
    // repeat planted at every position, against the reference verdict ----
    for ( int n = 0; n <= bound + 4; n++ )
    {
        expect( "sweep clean", spread( n ) );
        for ( int at = 1; at < n; at++ )
        {
            std::vector<uint64_t> ids = spread( n );
            ids[at] = ids[ at / 2 ];
            expect( "sweep planted", ids );
            if ( g_failures > 8 ) { printf( "too many failures, stopping\n" ); return 1; }
        }
    }

    // the same sweep over the hostile cluster, which is the shape where the
    // probe does the most work it can be made to do
    for ( int n = 0; n <= bound + 2; n++ )
    {
        expect( "hostile sweep clean", all_colliding( n, 5 ) );
        if ( n > 1 )
        {
            std::vector<uint64_t> ids = all_colliding( n, 5 );
            ids[ n - 1 ] = ids[0];
            expect( "hostile sweep planted", ids );
        }
    }

    if ( g_failures != 0 )
    {
        printf( "DISTINCTNESS GATE FAILED: %d case(s)\n", g_failures );
        return 1;
    }
    printf( "distinctness gate: both walks agree with the pairwise reference on every table (bound %d, slots %d)\n",
            bound, (int) kTableProbeSlots );
    return 0;
}
