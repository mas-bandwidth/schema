// W6 plan partition / split -- plan partition / split
//
// The bounds pass counts clamped once per field for a forged ordinal remapped
// to None, on the COMPILED plan exactly as on the identity one (§5.4, row 12).
// The ordinal op lands the value as 0 and this pass counts; a port that counts
// in the op as well counts twice.
//
// VOLD_enum_append defines Tier { Bronze, Silver, Gold } (3 variants).
// A forged ordinal of 4 (past the writer's 3) must land tier=None on both
// plans and count clamped == 1 on both.
#include <cstdio>
#include <cstring>
#include <vector>

#include "VOLD_enum_appendTable.h"
#include "VNEW_enum_appendTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// A forged ordinal ONE PAST the writer's variant count: the OLD side (VOLD)
// has 3 variants (Bronze, Silver, Gold), ordinal 4 is forged.
static constexpr uint8_t kForgedOrdinal = 4;

// Wire offset of the first body byte in a 1-record VOLD file.
static size_t body_offset()
{
    return (size_t) vold_enum_append::kTableFixedHeaderBytes + 4
         + (size_t) vold_enum_append::LineageFixedLayoutBytes + 8;
}

// Load through the IDENTITY plan (VOLD reading its own hash). The ordinal op
// is NOT used on the identity path -- the bounds pass must count clamped.
static void identity_plan_count()
{
    using namespace vold_enum_append;

    Lineage v;
    LineageReset( v );
    v.tier = Tier::Silver;
    v.seq = 9;

    std::vector<uint8_t> file( (size_t) LineageFixedMeasure( 1 ) );
    check( LineageFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "identity: the lawful record saves" );

    // Forge the ordinal past the writer's variant count.
    file[ body_offset() + 0 ] = kForgedOrdinal;

    Lineage back;
    LineageReset( back );
    back.seq = 0x5A5A5A5A; // pre-poison
    TableReport r;
    std::vector<TableFixedEntry> plan( 256 );
    const int64_t n = LineageFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                        plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "identity: the forged ordinal record still loads" );
    check( back.tier == Tier::None,
           "identity: the forged ordinal 4 lands as None" );
    check( back.seq == 9,
           "identity: seq lands from the wire, unchanged" );
    check( r.clamped == 1,
           "identity: clamped == 1 for the forged ordinal" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.duplicate == 0,
           "identity: unknown/kind_mismatch/widened/duplicate remain 0" );
}

// Load through the COMPILED plan (VNEW reading VOLD's hash). The ordinal op
// must count clamped for the forged ordinal, matching the identity plan count.
static void compiled_plan_count()
{
    vold_enum_append::Lineage v;
    vold_enum_append::LineageReset( v );
    v.tier = vold_enum_append::Tier::Silver;
    v.seq = 9;

    std::vector<uint8_t> file( (size_t) vold_enum_append::LineageFixedMeasure( 1 ) );
    check( vold_enum_append::LineageFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "compiled: the lawful record saves" );

    // Forge the ordinal past the writer's variant count.
    file[ body_offset() + 0 ] = kForgedOrdinal;

    vnew_enum_append::Lineage back;
    vnew_enum_append::LineageReset( back );
    back.seq = 0x5A5A5A5A; // pre-poison
    vnew_enum_append::TableReport r;
    std::vector<vnew_enum_append::TableFixedEntry> plan( 256 );
    const int64_t n = vnew_enum_append::LineageFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                          plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "compiled: the forged ordinal record still loads" );
    check( back.tier == vnew_enum_append::Tier::None,
           "compiled: the forged ordinal 4 lands as None" );
    check( back.seq == 9,
           "compiled: seq lands from the wire, unchanged" );
    check( r.clamped == 1,
           "compiled: clamped == 1 for the forged ordinal (compiled plan)" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.duplicate == 0,
           "compiled: unknown/kind_mismatch/widened/duplicate remain 0" );
}

// SAME-FORMAT lawful read through BOTH plans: every counter stays at zero.
static void lawful_no_count()
{
    using namespace vold_enum_append;

    Lineage v;
    LineageReset( v );
    v.tier = Tier::Silver;
    v.seq = 9;

    std::vector<uint8_t> file( (size_t) LineageFixedMeasure( 1 ) );
    check( LineageFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "lawful: the record saves" );

    // ---- identity plan ----
    {
        Lineage back;
        LineageReset( back );
        vold_enum_append::TableReport r;
        std::vector<vold_enum_append::TableFixedEntry> plan( 256 );
        const int64_t n = vold_enum_append::LineageFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                              plan.data(), 256, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed, "lawful: loads on the identity plan" );
        check( back.tier == Tier::Silver, "lawful: tier == Silver on identity" );
        check( back.seq == 9, "lawful: seq == 9 on identity" );
        check( r.clamped == 0, "lawful: clamped == 0 on identity" );
    }

    // ---- compiled plan ----
    {
        vnew_enum_append::Lineage back;
        vnew_enum_append::LineageReset( back );
        vnew_enum_append::TableReport r;
        std::vector<vnew_enum_append::TableFixedEntry> plan( 256 );
        const int64_t n = vnew_enum_append::LineageFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                              plan.data(), 256, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed, "lawful: loads on the compiled plan" );
        check( back.tier == vnew_enum_append::Tier::Silver, "lawful: tier == Silver on compiled (remapped from VOLD Silver)" );
        check( back.seq == 9, "lawful: seq == 9 on compiled" );
        check( r.clamped == 0, "lawful: clamped == 0 on compiled" );
    }
}

int main()
{
    lawful_no_count();
    identity_plan_count();
    compiled_plan_count();
    if ( failures > 0 )
    {
        std::printf( "W6 plan partition / split (C++): %d FAILED\n", failures );
        return 1;
    }
    std::printf( "W6 plan partition / split (C++): a forged ordinal counts clamped == 1 on the compiled plan exactly as on the identity one\n" );
    return 0;
}