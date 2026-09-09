// THE ID TABLE'S DISTINCTNESS AND RESOLVE, held to ONE ANSWER across every
// structure TableOpen carries (docs/SPEC-TABLES.md §3: "a table that carries
// one id twice is malformed", and "a reader RESOLVES THE TABLE ONCE, at open").
//
// TableOpen answers distinctness THREE ways — a BITMAP over the compile-time
// slots for every id this build can name, a hashed PROBE over a stack array
// for the foreign subset, the pairwise walk above kTableIdRefBound — and the
// whole property is that a reader cannot tell which one ran. In the same pass
// it RESOLVES every entry into its compile-time slot, which is what every
// body's dispatch then reads. This test is the gate on both: it builds tables
// BY HAND, drives TableOpen directly, holds the verdict against an independent
// O(n^2) reference over the same bytes, and holds every resolved slot against
// the hash that TableIdSlotOf computes from the id.
//
// The check is READ SIDE, so it is unconditional and this file is compiled
// BOTH ways, with and without NDEBUG, by its Makefile leg. Nothing here
// asserts: a failure prints the case and returns non-zero, so a release build
// gates exactly as a debug build does.
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
//     reference — the sweep is what proves the walks agree rather than that
//     each one is separately plausible.
//   - THE KNOWN SET: the ids this build can name, which take the slot bitmap
//     and no probe slot at all — the whole set, a repeat planted at every
//     position of it, the THREE RESERVED IDS the language holds back, and the
//     known and foreign sets interleaved with the repeat on either side.
//   - THE SLOT MAP'S OWN HOSTILE SHAPE: foreign ids aimed by inverting
//     kTableIdSlotMultiplier at one home of the slot map's probe, on both
//     sides of the bound. Each must still answer kTableIdSlotUnknown.
//   - A TRAILER AT THE READER'S MAXIMUM, and one entry past it.
//   - AND WHAT THE RESOLVE IS FOR: a body naming an id this build cannot name,
//     at every kind the four skip rules cover, which must be skipped by its
//     kind and counted `unknown` and nothing else — and the kinds that are not
//     skippable at all, which must be framing damage.
//
// It names ONE generated unit, and one root of it only to press Load: the
// checks are properties of the wire's trailer and of §4's unknown rule, not of
// any field of any schema.

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
using tabledemo::kTableIdRefBound;
using tabledemo::kTableIdProbeShift;
using tabledemo::kTableIdSlotUnknown;
using tabledemo::kTableIdSlotCount;
using tabledemo::kTableIdSlotId;
using tabledemo::TableIdSlotOf;
using tabledemo::kTableIdSlotMultiplier;
using tabledemo::kTableIdSlotShift;
using tabledemo::kTableIdSlotProbeSize;
using tabledemo::TableReport;
using tabledemo::Debuff;
using tabledemo::DebuffLoad;
using tabledemo::kTableIdProbeSlots;

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
            // THE RESOLVE IS THE SAME ANSWER THE HASH GIVES, on both sides of
            // kTableIdRefBound: under it the slot came out of the array
            // TableOpen filled, over it slot_of hashes the id on the spot, and
            // the two must never differ. Every table this file builds goes
            // through here, the whole sweep included.
            const uint16_t got_slot = table.slot_of( (uint64_t) i + 1 );
            const uint16_t want_slot = TableIdSlotOf( ids[i] );
            if ( got_slot != want_slot )
            {
                printf( "FAIL %s: entry %d resolved to slot %u, the hash says %u\n",
                        name, (int) i, (unsigned) got_slot, (unsigned) want_slot );
                g_failures++;
                return;
            }
            // and a slot that is not kTableIdSlotUnknown names THIS id
            if ( got_slot != kTableIdSlotUnknown )
            {
                if ( (int) got_slot >= (int) kTableIdSlotCount || kTableIdSlotId[ got_slot ] != ids[i] )
                {
                    printf( "FAIL %s: entry %d resolved to a slot that names another id\n", name, (int) i );
                    g_failures++;
                    return;
                }
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
        // id * k == ( slot << kTableIdProbeShift ) | i, so the top eight bits are slot for all of them
        const uint64_t target = ( (uint64_t) slot << kTableIdProbeShift ) | (uint64_t) i;
        ids.push_back( inv * target );
    }
    return ids;
}

static void check_collision_setup( const std::vector<uint64_t> & ids, uint32_t slot )
{
    for ( size_t i = 0; i < ids.size(); i++ )
    {
        const uint32_t s = uint32_t( ( ids[i] * 0x9E3779B97F4A7C15ull ) >> kTableIdProbeShift ) & uint32_t( kTableIdProbeSlots - 1 );
        if ( s != slot )
        {
            printf( "FAIL collision setup: entry %d lands in slot %u, not %u\n", (int) i, s, slot );
            g_failures++;
            return;
        }
    }
}

// ---- THE KNOWN SET: the ids this build CAN name (docs/SPEC-TABLES.md §3) ----
//
// A known id takes no probe slot at all: its repeat is a repeated SLOT, which
// is one bit of a bitmap over a set whose size is a compile-time fact. That is
// a SECOND walk inside the fast path, so it needs its own cases — the ones the
// probe's cases cannot reach.

// every id the unit can spell, in slot order
static std::vector<uint64_t> known_ids( int n )
{
    std::vector<uint64_t> ids;
    for ( int i = 0; i < n && i < (int) kTableIdSlotCount; i++ ) { ids.push_back( kTableIdSlotId[i] ); }
    return ids;
}

// THE SLOT MAP'S OWN HOSTILE SHAPE. TableIdSlotOf lands an id at
// ( id * kTableIdSlotMultiplier ) >> kTableIdSlotShift and walks forward to the
// first empty slot, so a writer who wants a MISS to do the most work it can be
// made to do aims every foreign id at the same home. The multiply is odd and
// therefore invertible, so these are the ids that come out. Each of them MUST
// still answer kTableIdSlotUnknown: the map is closed over the unit's set and
// a collision is not a match.
static uint64_t odd_inverse( uint64_t k )
{
    uint64_t x = k;                                          // exact mod 2^3 for every odd k
    for ( int i = 0; i < 6; i++ ) { x = x * ( 2 - k * x ); }  // Newton on the 2-adics
    return x;
}

static std::vector<uint64_t> slot_map_colliding( int n, uint32_t home )
{
    const uint64_t inv = odd_inverse( kTableIdSlotMultiplier );
    std::vector<uint64_t> ids;
    for ( int i = 0; i < n; i++ )
    {
        const uint64_t target = ( (uint64_t) home << kTableIdSlotShift ) | (uint64_t) i;
        const uint64_t id = inv * target;
        // an id the unit CAN name would make this case something else: skip it
        if ( TableIdSlotOf( id ) == kTableIdSlotUnknown ) { ids.push_back( id ); }
    }
    return ids;
}

static void run_slot_cases( int bound )
{
    // ---- the known set, and a repeat inside it. It is taken UNDER the
    // resolved-trailer bound on purpose: over it the pairwise walk runs and
    // the slot bitmap is not the structure under test. The whole set, when it
    // is larger than the bound, rides in the "maximum" cases below.
    {
        std::vector<uint64_t> ids = known_ids( bound < (int) kTableIdSlotCount ? bound : (int) kTableIdSlotCount );
        expect( "known: every id the build can name", ids );
        if ( ids.size() > 1 )
        {
            std::vector<uint64_t> dup = ids;
            dup[ dup.size() - 1 ] = dup[0];
            expect( "known: first id repeated last", dup );
            dup = ids;
            dup[1] = dup[0];
            expect( "known: adjacent repeat at the front", dup );
            dup = ids;
            dup[ dup.size() / 2 ] = dup[ dup.size() / 2 - 1 ];
            expect( "known: repeat in the middle", dup );
        }
        // a repeat planted at EVERY position of the known set
        for ( size_t at = 1; at < ids.size(); at++ )
        {
            std::vector<uint64_t> dup = ids;
            dup[at] = ids[ at / 2 ];
            expect( "known sweep planted", dup );
        }
    }

    // ---- THE THREE RESERVED IDS, which are known ids like any other ----
    {
        std::vector<uint64_t> ids;
        ids.push_back( 0xFFFFFFFFFFFFFFFFull ); // the node table's (§3.1)
        ids.push_back( 0xFFFFFFFFFFFFFFFEull ); // the announcement's build version (§3.3)
        ids.push_back( 0xFFFFFFFFFFFFFFFDull ); // the announcement's vocabulary (§3.3)
        expect( "reserved: all three, distinct", ids );
        for ( int a = 0; a < 3; a++ )
        {
            for ( int b = 0; b < 3; b++ )
            {
                std::vector<uint64_t> dup = ids;
                dup[a] = ids[b];
                expect( a == b ? "reserved: unchanged" : "reserved: one replaced by another", dup );
            }
        }
        // and beside foreign entries, on both sides of the bound
        std::vector<uint64_t> mixed = spread( bound - 3 );
        for ( size_t i = 0; i < ids.size(); i++ ) { mixed.push_back( ids[i] ); }
        expect( "reserved: at the bound beside foreign ids", mixed );
        mixed[ mixed.size() - 1 ] = mixed[ mixed.size() - 2 ];
        expect( "reserved: repeated at the bound", mixed );
        mixed = spread( bound + 1 );
        for ( size_t i = 0; i < ids.size(); i++ ) { mixed.push_back( ids[i] ); }
        expect( "fallback: reserved beside foreign ids", mixed );
        mixed[ mixed.size() - 1 ] = mixed[0];
        expect( "fallback: a foreign id repeated after the reserved ones", mixed );
    }

    // ---- KNOWN AND FOREIGN INTERLEAVED, the repeat on either side ----
    {
        std::vector<uint64_t> ids;
        const std::vector<uint64_t> k = known_ids( bound / 2 );
        const std::vector<uint64_t> f = spread( bound / 2 );
        for ( size_t i = 0; i < k.size() && i < f.size(); i++ ) { ids.push_back( k[i] ); ids.push_back( f[i] ); }
        expect( "mixed: known and foreign interleaved", ids );
        for ( size_t at = 1; at < ids.size(); at++ )
        {
            std::vector<uint64_t> dup = ids;
            dup[at] = ids[ at - 1 ];
            expect( "mixed: adjacent repeat, planted at every position", dup );
        }
        // a KNOWN id repeated across a long run of foreign ones, and back
        std::vector<uint64_t> far = ids;
        far[ far.size() - 1 ] = far[0];
        expect( "mixed: the first known id repeated last", far );
        far = ids;
        far[ far.size() - 2 ] = far[1];
        expect( "mixed: the first foreign id repeated last", far );
    }

    // ---- THE SLOT MAP'S HOSTILE CLUSTER: foreign ids aimed at one home ----
    for ( uint32_t home = 0; home < 3; home++ )
    {
        const uint32_t h = home == 2 ? (uint32_t) ( kTableIdSlotProbeSize - 1 ) : home;
        std::vector<uint64_t> ids = slot_map_colliding( bound, h );
        if ( ids.size() < 2 ) { continue; }
        expect( "slot map: foreign ids all aimed at one home, distinct", ids );
        std::vector<uint64_t> dup = ids;
        dup[ dup.size() - 1 ] = dup[0];
        expect( "slot map: foreign ids all aimed at one home, first repeated last", dup );
        dup = ids;
        dup[1] = dup[0];
        expect( "slot map: foreign ids all aimed at one home, repeat at the front", dup );
        // the same shape ONE PAST the bound, where slot_of hashes per reference
        std::vector<uint64_t> over = slot_map_colliding( bound + 8, h );
        if ( (int) over.size() > bound )
        {
            expect( "slot map: one past the bound, aimed at one home", over );
            over[ over.size() - 1 ] = over[0];
            expect( "slot map: one past the bound, repeated", over );
        }
    }

    // ---- A KNOWN ID AIMED AT A FOREIGN ID'S HOME, and the reverse. The two
    // walks are separate structures, so an id in one must never be found by
    // the other: a foreign id that lands on a known id's probe home is still
    // unknown, and a known id never enters the foreign probe at all.
    {
        std::vector<uint64_t> ids = known_ids( 8 );
        const std::vector<uint64_t> collide = slot_map_colliding( 8, uint32_t( ( kTableIdSlotId[0] * kTableIdSlotMultiplier ) >> kTableIdSlotShift ) );
        for ( size_t i = 0; i < collide.size(); i++ ) { ids.push_back( collide[i] ); }
        expect( "crossed: foreign ids at a known id's home", ids );
        if ( ids.size() > 1 )
        {
            std::vector<uint64_t> dup = ids;
            dup[ dup.size() - 1 ] = dup[0];
            expect( "crossed: the known id repeated last", dup );
        }
    }

    // ---- A TRAILER AT EXACTLY THE READER'S MAXIMUM, and one past it, built
    // out of the known set repeated as far as it goes plus foreign filler ----
    {
        std::vector<uint64_t> ids = known_ids( bound < (int) kTableIdSlotCount ? bound : (int) kTableIdSlotCount );
        const std::vector<uint64_t> f = spread( bound + 4 );
        for ( size_t i = 0; (int) ids.size() < bound && i < f.size(); i++ ) { ids.push_back( f[i] ); }
        expect( "maximum: a full trailer, known set then foreign", ids );
        std::vector<uint64_t> dup = ids;
        dup[ bound - 1 ] = dup[0];
        expect( "maximum: a full trailer with the first id repeated last", dup );
        ids.push_back( f[ f.size() - 1 ] );
        expect( "maximum: one past the maximum", ids );
        dup = ids;
        dup[ dup.size() - 1 ] = dup[0];
        expect( "maximum: one past the maximum, repeated", dup );
    }
}

// ---- AND THE OTHER HALF: WHAT A RESOLVED SLOT DECIDES ----
//
// Everything above holds the RESOLVE. This holds what the resolve is FOR: a
// body that names an id, and the read that follows. The tables here carry no
// field of the reader's schema — one FOREIGN id at a time, at every kind — so
// the file stays a property of the wire rather than of a declaration, and the
// answer it checks is §4's ordinary one: AN ID THIS BUILD CANNOT NAME IS
// SKIPPED BY ITS KIND AND COUNTED `unknown`, and never anything else.

// a form-1 FILE whose root body is one field under one id, then the terminator
static std::vector<uint8_t> file_of_one_field( uint64_t id, uint8_t kind,
                                               const std::vector<uint8_t> & payload )
{
    std::vector<uint8_t> w;
    w.push_back( 1 );          // the FORM BYTE (§3)
    w.push_back( 1 );          // reference 1: the trailer's only entry
    w.push_back( kind );
    for ( size_t i = 0; i < payload.size(); i++ ) { w.push_back( payload[i] ); }
    w.push_back( 0 );          // the body ENDS AT ITS OWN ZERO REFERENCE
    for ( int b = 0; b < 8; b++ ) { w.push_back( (uint8_t) ( id >> ( 8 * b ) ) ); }
    const uint64_t count = 1;
    for ( int b = 0; b < 8; b++ ) { w.push_back( (uint8_t) ( count >> ( 8 * b ) ) ); }
    return w;
}

static void expect_unknown( const char * name, uint8_t kind, const std::vector<uint8_t> & payload )
{
    // an id NO NAME HASHES TO in this unit: the gate asserts it before using it
    const uint64_t foreign = 0x0123456789ABCDEFull;
    if ( TableIdSlotOf( foreign ) != kTableIdSlotUnknown )
    {
        printf( "FAIL %s: the chosen foreign id is one this build names\n", name );
        g_failures++;
        return;
    }
    const std::vector<uint8_t> w = file_of_one_field( foreign, kind, payload );
    Debuff value;
    TableReport report;
    const bool ok = DebuffLoad( value, w.data(), (int64_t) w.size(), &report );
    if ( !ok || report.malformed || report.unknown != 1 || report.kind_mismatch != 0 ||
         report.widened != 0 || report.clamped != 0 || report.duplicate != 0 || report.refused )
    {
        printf( "FAIL %s: kind %u read ok=%d unknown=%d kind_mismatch=%d malformed=%d\n",
                name, (unsigned) kind, (int) ok, report.unknown, report.kind_mismatch,
                (int) report.malformed );
        g_failures++;
    }
}

static void expect_unskippable( const char * name, uint8_t kind )
{
    const uint64_t foreign = 0x0123456789ABCDEFull;
    std::vector<uint8_t> payload;
    const std::vector<uint8_t> w = file_of_one_field( foreign, kind, payload );
    Debuff value;
    TableReport report;
    DebuffLoad( value, w.data(), (int64_t) w.size(), &report );
    if ( !report.malformed )
    {
        printf( "FAIL %s: kind %u is not skippable and was not framing damage\n",
                name, (unsigned) kind );
        g_failures++;
    }
}

static void run_unknown_cases()
{
    // THE FOUR SKIP RULES (docs/SPEC-TABLES.md §3), each over an id this build
    // cannot name, each answering one unknown and nothing else.
    const uint8_t fixed1[] = { 1, 2, 6, 20, 25 };
    const uint8_t fixed2[] = { 3, 7, 21, 26 };
    const uint8_t fixed4[] = { 4, 8, 10, 22, 27 };
    const uint8_t fixed8[] = { 5, 9, 11, 23, 28 };
    const uint8_t fixed16[] = { 18, 19, 24, 29 };
    const uint8_t lengthed[] = { 12, 13, 14, 16, 31, 32, 33 };
    for ( size_t i = 0; i < sizeof( fixed1 ); i++ )
    { expect_unknown( "unknown: a one-byte kind", fixed1[i], std::vector<uint8_t>( 1, 0 ) ); }
    for ( size_t i = 0; i < sizeof( fixed2 ); i++ )
    { expect_unknown( "unknown: a two-byte kind", fixed2[i], std::vector<uint8_t>( 2, 0 ) ); }
    for ( size_t i = 0; i < sizeof( fixed4 ); i++ )
    { expect_unknown( "unknown: a four-byte kind", fixed4[i], std::vector<uint8_t>( 4, 0 ) ); }
    for ( size_t i = 0; i < sizeof( fixed8 ); i++ )
    { expect_unknown( "unknown: an eight-byte kind", fixed8[i], std::vector<uint8_t>( 8, 0 ) ); }
    for ( size_t i = 0; i < sizeof( fixed16 ); i++ )
    { expect_unknown( "unknown: a sixteen-byte kind", fixed16[i], std::vector<uint8_t>( 16, 0 ) ); }
    for ( size_t i = 0; i < sizeof( lengthed ); i++ )
    { expect_unknown( "unknown: a length-shaped kind", lengthed[i], std::vector<uint8_t>( 1, 0 ) ); }
    // kinds 17 and 30 read ONE LEB128 and stop
    expect_unknown( "unknown: a node index", 17, std::vector<uint8_t>( 1, 7 ) );
    expect_unknown( "unknown: an enum reference", 30, std::vector<uint8_t>( 1, 3 ) );
    // kind 15 is the union: the arm id reference, and 0 is the whole payload
    expect_unknown( "unknown: an empty union", 15, std::vector<uint8_t>( 1, 0 ) );
    {
        std::vector<uint8_t> arm;
        arm.push_back( 1 );  // the arm's id reference
        arm.push_back( 9 );  // its kind
        arm.push_back( 8 );  // its L
        for ( int i = 0; i < 8; i++ ) { arm.push_back( 0 ); }
        expect_unknown( "unknown: a set union arm", 15, arm );
    }
    // A KIND THIS READER DOES NOT KNOW AT ALL IS NOT SKIPPABLE (§3), and 34 is
    // RESERVED BY NAME for float16 and is met only as damage.
    expect_unskippable( "unknown: kind 34, reserved and not this major's", 34 );
    expect_unskippable( "unknown: kind 35", 35 );
    expect_unskippable( "unknown: kind 200", 200 );
    expect_unskippable( "unknown: kind 0", 0 );
}

int main()
{
    const int bound = (int) kTableIdRefBound;

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
        const uint32_t s = slot == 2 ? (uint32_t) ( kTableIdProbeSlots - 1 ) : slot;
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
        std::vector<uint64_t> ids = all_colliding( bound, (uint32_t) ( kTableIdProbeSlots - 1 ) );
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

    // ---- everything the SLOT MAP brought: the known set, the reserved ids,
    // the two walks crossed, and the hostile shapes aimed at each ----
    run_slot_cases( bound );

    // ---- and what the resolved slot DECIDES: an id this build cannot name,
    // counted unknown and skipped by its kind ----
    run_unknown_cases();

    if ( g_failures != 0 )
    {
        printf( "DISTINCTNESS GATE FAILED: %d case(s)\n", g_failures );
        return 1;
    }
    printf( "distinctness gate: both walks agree with the pairwise reference on every table, "
            "and every resolved slot agrees with the hash (bound %d, probe slots %d, id slots %d over %d)\n",
            bound, (int) kTableIdProbeSlots, (int) kTableIdSlotCount, (int) kTableIdSlotProbeSize );
    return 0;
}
