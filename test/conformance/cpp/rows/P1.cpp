// P1: identity == compiled for tblp1::Chain
//
// The identity plan and a plan compiled from this build's own layout must
// land the same fields and move the same counters (§5.9 P1, FIXED-FORM-
// ALGORITHM.md:939).

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <algorithm>
#include <vector>

#include "P1Table.h"

static bool reports_eq( const tblp1::TableReport & a, const tblp1::TableReport & b )
{
    return a.unknown == b.unknown && a.kind_mismatch == b.kind_mismatch &&
           a.widened == b.widened && a.clamped == b.clamped &&
           a.malformed == b.malformed && a.refused == b.refused;
}

static bool chains_eq( const tblp1::Chain & a, const tblp1::Chain & b )
{
    if ( a.name_length != b.name_length || a.name_length < 0 || a.name_length > 16 ) return false;
    if ( std::memcmp( a.name, b.name, (size_t) a.name_length ) != 0 ) return false;
    return a.link.value == b.link.value &&
           a.link.tag_length == b.link.tag_length &&
           a.link.tag_length >= 0 && a.link.tag_length <= 8 &&
           std::memcmp( a.link.tag, b.link.tag, (size_t) a.link.tag_length ) == 0;
}

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

int main()
{
    // Build a Chain value and save it to a fixed-form file.
    tblp1::Chain value = {};
    tblp1::ChainReset( value );
    std::strcpy( value.name, "twelve chars!" ); // 12 chars: > 8, <= 16
    value.name_length = 12;
    value.link.value = 42;
    std::strcpy( value.link.tag, "tag" );
    value.link.tag_length = 3;

    const int64_t need = tblp1::ChainFixedMeasure( 1 );
    std::vector<uint8_t> file( (size_t) need + 32, 0 );
    check( tblp1::ChainFixedSave( &value, 1, file.data(), (int64_t) file.size() ) == need,
           "save one record" );

    // ---- identity path ----
    tblp1::Chain id_value = {};
    tblp1::TableReport id_report = {};
    tblp1::TableFixedEntry id_plan[8192];
    const int64_t id_n = tblp1::ChainFixedLoad( &id_value, 1, file.data(), need,
                                                 id_plan, 8192, NULL, &id_report );

    // ---- compiled path: parse the layout, compile a plan, run it ----
    tblp1::Chain co_value = {};
    tblp1::TableReport co_report = {};
    const uint8_t * layout = file.data() + tblp1::kTableFixedHeaderBytes + 4;
    const uint32_t layout_bytes = tblp1::TableFixedGet32( file.data() + tblp1::kTableFixedHeaderBytes );
    tblp1::TableFixedLayoutView view;
    tblp1::TableMessageReason why = tblp1::layout_malformed;
    check( tblp1::TableFixedParseLayout( layout, (int64_t) layout_bytes, view, why ),
           "layout parse" );
    tblp1::TableFixedEntry plan[8192];
    int32_t guarded = 0;
    uint32_t fill_at = 0;
    int32_t fill_count = 0;
    const int32_t made = tblp1::TableFixedCompile( view, tblp1::ChainFixedLayout,
                                                    (int32_t) tblp1::ChainFixedLayoutBytes,
                                                    tblp1::ChainFixedDst,
                                                    tblp1::ChainFixedCover, tblp1::ChainFixedCoverCount,
                                                    plan, 8192, &guarded, &fill_at, &fill_count, &co_report );
    check( made > 0, "compiled plan has entries" );
    const uint8_t * rec = layout + layout_bytes;
    tblp1::Chain defaults = {};
    tblp1::ChainReset( defaults );
    const tblp1::TableFixedFill * fill = ( fill_count > 0 )
        ? (const tblp1::TableFixedFill *) (const void *) ( (const uint8_t *) plan + fill_at )
        : NULL;
    tblp1::TableFixedFillRun( fill, fill_count, (const uint8_t *) &defaults, (uint8_t *) &co_value );
    tblp1::TableFixedRun( plan, made, guarded, rec + 8, (uint8_t *) &co_value, &co_report );

    // ---- assert both paths agree ----
    check( id_n == 1, "identity loads one record" );
    check( reports_eq( id_report, co_report ), "identity and compiled reports agree" );
    check( chains_eq( id_value, co_value ), "identity and compiled field values agree" );

    // ---- negative control: break name bound 16 -> 8 in a copy of the plan ----
    {
        tblp1::Chain ctrl_id = {};
        tblp1::TableReport ctrl_ir = {};
        tblp1::TableFixedEntry ctrl_plan[tblp1::ChainFixedPlanCount];
        std::memcpy( ctrl_plan, tblp1::ChainFixedPlan, sizeof( ctrl_plan ) );
        ctrl_plan[0].size = 8; // name bound 16 -> 8
        tblp1::TableFixedFillRun( NULL, 0, NULL, (uint8_t *) &ctrl_id );
        tblp1::TableFixedRun( ctrl_plan, tblp1::ChainFixedPlanCount,
                              tblp1::ChainFixedPlanGuarded, rec + 8, (uint8_t *) &ctrl_id, &ctrl_ir );
        // The compiled path still reads the full name; the broken identity
        // path truncates. The two should disagree on the name field.
        const int32_t min_len = std::min( ctrl_id.name_length, co_value.name_length );
        const bool names_differ = ctrl_id.name_length != co_value.name_length ||
            ( min_len > 0 && std::memcmp( ctrl_id.name, co_value.name, (size_t) min_len ) != 0 );
        check( names_differ, "control: broken name bound makes identity and compiled disagree" );
    }

    if ( failures == 0 )
    {
        std::printf( "P1 identity == compiled: PASS\n" );
        return 0;
    }
    std::printf( "P1 identity == compiled: FAIL (%d assertion%s)\n", failures, failures == 1 ? "" : "s" );
    return 1;
}
