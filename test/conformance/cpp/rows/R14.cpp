// cpp/R14 — EVERY COMPILED ENTRY'S REACH IS IN THE WRITER'S COORDINATES: a
// union tag grown 1 -> 8 reads the writer's tag at the WRITER's width and
// compiles whole.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15, settled
// 2026-11-25): when a union's tag widens between the writer's layout and the
// reader's, the union's None `const` LANDS my_tag bytes in the reader's
// storage but reads the writer's raw tag at THE WRITER'S TAG WIDTH, their_tag
// bytes, never my_tag. Its source reach is src + their_tag, inside the
// writer's record. A reach at the reader's width reads the arm's payload into
// the tag and refuses a lawful file `layout_record_too_large`.
//
// THE PRODUCTION PATH this row drives, every stage generated into the unit's
// header (build/tables-generated/fu1/FU1Table.h, emitted from
// internal/codegen/cpptable/fixedruntime.go):
//
//   TableFixedParseLayout   the layout's own rules (§1.1)
//   TableFixedCompile       the plan compiler: case 15 lays the None const
//                           (src their_at, size my_tag, sign their_tag) and
//                           TableFixedPush bounds its reach at src + sign
//                           against the writer's record
//   TableFixedRefuseScan    the check pass the load runs before the run
//   TableFixedRun           the one read loop; the None const reads the
//                           writer's raw tag at `sign` bytes and counts a tag
//                           past the writer's arm set out_of_range
//
// The lineage lane (<Name>FixedLineageInit, internal/codegen/cpptable/
// fixedform.go) is this same TableFixedCompile call over a lock entry's
// bytes: `made >= 0` lays a ready plan, `-2` records layout_record_too_large
// on the entry. So the lane for MINE(8) is ready exactly when the compile
// below answers made >= 0.
//
// THE VECTOR (one for every leg). A layout block is a u32LE entry count then
// 17-byte entries: id u64LE, kind u8, size u32LE, children u32LE. Kind 13 a
// table, 15 a union (size = tag + widest arm), 8 a u32.
//   THEIRS, the writer: [1,13,5,1] [7,15,5,1] [9,8,4,0] — a record of 5
//     bytes, one union whose tag is 1 byte over one u32 arm.
//   MINE(tag), the reader: [1,13,tag+4,1] [7,15,tag+4,1] [9,8,4,0], its
//     storage the union's tag at 0 (tag bytes) and the u32 arm at `tag`.
// The vector is forged here because no real schema grows a tag past 4 bytes
// (8 needs more than 2^32 arms), so no corpus file can carry it.
//
// BUILD/RUN (flags per make/conformance-rows.mk; tools/conformancerows builds it):
//   c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
//     -Ibuild/tables-generated/fu1 test/conformance/cpp/rows/R14.cpp
// Exit 0 green, exit 1 red; one printed line per assertion.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FU1Table.h"

namespace fx = tblfu1;

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s: R14: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures = 1; }
}

static void put_entry( std::vector<uint8_t> & b, uint64_t id, uint8_t kind, uint32_t size, uint32_t children )
{
    const size_t at = b.size();
    b.resize( at + 17 );
    fx::TableFixedPut64( b.data() + at, id );
    b[at + 8] = kind;
    fx::TableFixedPut32( b.data() + at + 9, size );
    fx::TableFixedPut32( b.data() + at + 13, children );
}

// THEIRS when tag == 0 (the writer's 1-byte tag), MINE(tag) otherwise.
static std::vector<uint8_t> layout( uint32_t tag )
{
    const uint32_t t = tag == 0u ? 1u : tag;
    std::vector<uint8_t> b( 4 );
    fx::TableFixedPut32( b.data(), 3u );
    put_entry( b, 1u, 13u, t + 4u, 1u );
    put_entry( b, 7u, 15u, t + 4u, 1u );
    put_entry( b, 9u, 8u, 4u, 0u );
    return b;
}

struct Compiled
{
    std::vector<fx::TableFixedEntry> plan = std::vector<fx::TableFixedEntry>( 512 );
    int32_t made = 0;
    int32_t guarded = 0;
    fx::TableReport report;
};

static Compiled compile( uint32_t tag )
{
    Compiled out;
    const std::vector<uint8_t> theirs_bytes = layout( 0u );
    const std::vector<uint8_t> mine_bytes = layout( tag );
    // MY storage rows, by my layout's entry index: the table at 0, the
    // union's arms at its own offset 0 with the tag at aux 0, the u32 arm
    // past the tag.
    fx::TableFixedDst d[3];
    d[2].dst = tag;
    fx::TableFixedLayoutView theirs;
    fx::TableMessageReason why = fx::layout_malformed;
    check( fx::TableFixedParseLayout( theirs_bytes.data(), (int64_t) theirs_bytes.size(), theirs, why ),
           "the writer's layout parses (all of §1.1)" );
    check( fx::TableFixedEntryAt( theirs, 0 ).size == 5u, "the writer's declared record is 5 bytes" );
    uint32_t fill_at = 0;
    int32_t fill_count = 0;
    out.made = fx::TableFixedCompile( theirs, mine_bytes.data(), (int32_t) mine_bytes.size(), d, nullptr, 0,
                                      out.plan.data(), (int32_t) out.plan.size(), &out.guarded,
                                      &fill_at, &fill_count, &out.report );
    return out;
}

// THE LOAD'S ORDER over one writer record: the check pass, then the run.
static void run( const Compiled & c, const uint8_t ( &record5 )[5], uint8_t * image, size_t bytes, fx::TableReport & r )
{
    // the record sits in a zeroed buffer wider than 5 bytes, so a reach past
    // the writer's record reads zeros and never past the allocation
    uint8_t src[16] = {};
    std::memcpy( src, record5, 5 );
    std::memset( image, 0xA5, bytes );
    r = fx::TableReport();
    fx::TableMessageReason why = fx::layout_malformed;
    if ( fx::TableFixedRefuseScan( c.plan.data(), c.made, c.guarded, src, nullptr, why ) )
    {
        r.refused = true;
        r.refuse_reason = fx::wire_damaged;
        return;
    }
    fx::TableFixedRun( c.plan.data(), c.made, c.guarded, src, image, &r );
}

int main()
{
    // ---- 1. THE CELL: THEIRS against MINE(8) compiles WHOLE ----------------
    const Compiled grown = compile( 8u );
    check( grown.made == 3, "the grown tag (1 -> 8) compiles whole: None const, the arm's tag const, the u32's copy" );
    check( grown.made != -2 && grown.report.reason != fx::layout_record_too_large,
           "not layout_record_too_large: no entry reaches past the writer's record of 5" );
    check( !grown.report.refused, "nothing refused on the compile's report" );
    {
        const fx::TableFixedEntry * none = nullptr;
        const fx::TableFixedEntry * copy = nullptr;
        for ( int32_t i = 0; i < grown.made; ++i )
        {
            const fx::TableFixedEntry & e = grown.plan[(size_t) i];
            if ( e.op == fx::kTableFixedConst && e.aux == 0u && e.dstsize != 0u ) { none = &e; }
            if ( e.op == fx::kTableFixedCopy ) { copy = &e; }
        }
        // ---- 2. THE None CONST: src 0, lands 8, reads the writer's 1 -------
        check( none != nullptr, "the union's None const is in the plan" );
        if ( none != nullptr )
        {
            check( none->src == 0u, "the None const reads the writer's tag at its offset 0" );
            check( none->size == 8u, "the None const LANDS my 8-byte tag (size 8)" );
            check( none->sign == 1u, "the None const reads the WRITER's 1-byte tag (sign 1), never 8" );
            check( none->dstsize == 1u, "it carries the writer's arm count, 1" );
            check( none->guard == fx::kTableFixedNoGuard, "the None const is unguarded" );
            check( (uint64_t) none->src + (uint64_t) none->sign <= 5u,
                   "its reach, 0 + 1, is inside the writer's record of 5" );
        }
        check( copy != nullptr && copy->src == 1u && copy->size == 4u && copy->dst == 8u,
               "the u32 arm copies 4 bytes from src 1 (past the WRITER's 1-byte tag) to my image 8" );
    }

    // ---- 3. RUN the plan the grown tag lays --------------------------------
    {
        static const uint8_t arm1[5] = { 1u, 0x44u, 0x33u, 0x22u, 0x11u };
        static const uint8_t none0[5] = { 0u, 0u, 0u, 0u, 0u };
        static const uint8_t forged[5] = { 2u, 0u, 0u, 0u, 0u };
        uint8_t image[12];
        uint64_t tag = 0;
        uint32_t u = 0;
        fx::TableReport r;

        run( grown, arm1, image, sizeof( image ), r );
        std::memcpy( &tag, image, 8 );
        std::memcpy( &u, image + 8, 4 );
        check( !r.refused, "arm 1 reads with no refusal" );
        check( tag == 1u, "my 8-byte tag lands exactly 1 (high bytes zero; the payload is never read as the tag)" );
        check( u == 0x11223344u, "the u32 lands whole past my tag: 0x11223344" );

        run( grown, none0, image, sizeof( image ), r );
        std::memcpy( &tag, image, 8 );
        check( !r.refused && tag == 0u, "tag 0 reads as None in my 8-byte tag" );

        run( grown, forged, image, sizeof( image ), r );
        check( r.refused && r.refuse_reason == fx::out_of_range,
               "a forged writer tag 2 (past one arm) refuses out_of_range (§4.6)" );
    }

    // ---- 4. THE TWINS: MINE(1) and MINE(2) compile whole too ---------------
    {
        const Compiled one = compile( 1u );
        check( one.made > 0 && !one.report.refused, "THEIRS against MINE(1) (the same layout) compiles whole" );
        const Compiled two = compile( 2u );
        check( two.made > 0 && !two.report.refused, "THEIRS against MINE(2) (the tag grown 1 -> 2) compiles whole" );
    }

    // ---- 5. THE LINEAGE LANE: the same compile the lineage init runs -------
    {
        const Compiled lane = compile( 8u );
        check( lane.made >= 0 && lane.report.reason != fx::layout_record_too_large,
               "the lineage lane for MINE(8) lays a ready plan (made >= 0, no reason recorded)" );
    }

    if ( failures != 0 )
    {
        std::printf( "R14: FAILED\n" );
        return 1;
    }
    std::printf( "R14: a grown union tag reads the writer's tag width and compiles whole — green\n" );
    return 0;
}
