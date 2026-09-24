// CELL cpp/R1 — "COMPILE lays the lineage down as static data at build time,
// oldest first and the current layout last, from the lock"
// (docs/FIXED-FORM-ALGORITHM.md step 3, docs/roadmap.sexp:1152).
//
// Every generated C++ fixed table carries a constexpr array of layout hashes
// (T##FixedLineage[]) and a constexpr count (T##FixedLineageCount).
// The array MUST hold the hashes in oldest-first order with the reader's own
// layout (T##FixedHash) last. This test verifies that property for the
// build-time generated versioning tables.
//
// Build and run:
//   c++ -std=c++17 -Wall -Ibuild/tables-generated/vnum
//       test/conformance/cpp/rows/R1.cpp -o build/rows-cpp-R1
//       && ./build/rows-cpp-R1

#include <cstdio>
#include <cstdint>

// The versioning table headers (all in build/tables-generated/vnum/).
// Each one nests its constants in its own package namespace.
#include "VNEW_int_widenTable.h"      // vnew_int_widen
#include "VNEW_float_widenTable.h"    // vnew_float_widen
#include "VNEW_uint_widenTable.h"     // vnew_uint_widen
#include "VNEW_range_widenTable.h"    // vnew_range_widen
#include "VNEW_constant_growTable.h"  // vnew_constant_grow
#include "VNEW_string_growTable.h"    // vnew_string_grow
#include "VNEW_bytes_growTable.h"     // vnew_bytes_grow
#include "VNEW_bits_growTable.h"      // vnew_bits_grow
#include "VNEW_array_bounded_growTable.h"   // vnew_array_bounded_grow
#include "VNEW_array_elem_widenTable.h"     // vnew_array_elem_widen
#include "VNEW_array_fixed_growTable.h"     // vnew_array_fixed_grow (uses VecFixed prefix)
#include "VNEW_hostile_boolTable.h"         // vnew_hostile_bool (uses Hbb prefix)
#include "VNEW_optional_addTable.h"         // vnew_optional_add (uses OptLeaf prefix)
#include "VNEW_unknown_censusTable.h"       // vnew_unknown_census (uses Item prefix)
#include "VNEW_cfloat_range_widenTable.h"   // vnew_cfloat_range_widen
#include "VNEW_cfloat_res_refineTable.h"    // vnew_cfloat_res_refine
#include "VNEW_wstring_growTable.h"         // vnew_wstring_grow

// Floor / lineage merge — these define the same constant names in different
// namespaces, so we handle them in explicit blocks.
#include "VNEW_floorTable.h"          // vnew_floor — uses Floored prefix
#include "VMID_floorTable.h"          // vmid_floor — uses Floored prefix (same name!)
#include "VBRA_lineage_mergeTable.h"  // vbra_lineage_merge — uses Merged prefix
#include "VBRB_lineage_mergeTable.h"  // vbrb_lineage_merge — uses Merged prefix
#include "VNEW_lineage_mergeTable.h"  // vnew_lineage_merge — uses Merged prefix (same name!)

static int failures = 0;

static void check( const char * name, bool ok )
{
    if ( !ok )
    {
        std::fprintf( stderr, "FAIL %s\n", name );
        ++failures;
    }
    else
    {
        std::printf( "ok %s\n", name );
    }
}

// One table: verify that count matches sizeof(array)/8 and the last entry
// equals the identity hash. Uses a namespace-qualified reference.
#define ASSERT_NS_TABLE( ns, prefix, label )                                    \
    do {                                                                         \
        using namespace ns;                                                      \
        constexpr int32_t  c   = prefix##FixedLineageCount;                      \
        constexpr uint64_t h   = prefix##FixedHash;                              \
        bool count_ok = c == (int32_t)( sizeof(prefix##FixedLineage) / 8 );      \
        bool last_ok  = prefix##FixedLineage[c - 1] == h;                       \
        bool order_ok = c < 2 || prefix##FixedLineage[0] != h;                  \
        check( label ": count matches size", count_ok );                         \
        check( label ": last == identity", last_ok );                            \
        if ( c >= 2 )                                                            \
            check( label ": oldest != identity", order_ok );                     \
    } while (0)

int main()
{
    // Count 2: VOLD hash + identity hash
    ASSERT_NS_TABLE( vnew_int_widen, IntWiden, "IntWiden/VNEW_int_widen" );
    ASSERT_NS_TABLE( vnew_float_widen, FloatWiden, "FloatWiden/VNEW_float_widen" );
    ASSERT_NS_TABLE( vnew_uint_widen, UintWiden, "UintWiden/VNEW_uint_widen" );
    ASSERT_NS_TABLE( vnew_range_widen, RangeWiden, "RangeWiden/VNEW_range_widen" );
    ASSERT_NS_TABLE( vnew_constant_grow, ConstantGrow, "ConstantGrow/VNEW_constant_grow" );
    ASSERT_NS_TABLE( vnew_string_grow, StringGrow, "StringGrow/VNEW_string_grow" );
    ASSERT_NS_TABLE( vnew_bytes_grow, BytesGrow, "BytesGrow/VNEW_bytes_grow" );
    ASSERT_NS_TABLE( vnew_bits_grow, BitsGrow, "BitsGrow/VNEW_bits_grow" );
    ASSERT_NS_TABLE( vnew_array_elem_widen, ArrayElemWiden, "ArrayElemWiden/VNEW_array_elem_widen" );
    ASSERT_NS_TABLE( vnew_cfloat_range_widen, CfloatRangeWiden, "CfloatRangeWiden/VNEW_cfloat_range_widen" );
    ASSERT_NS_TABLE( vnew_cfloat_res_refine, CfloatResRefine, "CfloatResRefine/VNEW_cfloat_res_refine" );
    ASSERT_NS_TABLE( vnew_hostile_bool, Hbb, "Hbb/VNEW_hostile_bool" );
    ASSERT_NS_TABLE( vnew_wstring_grow, WstringGrow, "WstringGrow/VNEW_wstring_grow" );
    ASSERT_NS_TABLE( vnew_unknown_census, Item, "Item/VNEW_unknown_census" );

    // Count 3: VOLD + VMID + identity
    ASSERT_NS_TABLE( vnew_array_bounded_grow, ArrayBoundedGrow, "ArrayBoundedGrow/VNEW_array_bounded_grow" );
    ASSERT_NS_TABLE( vnew_floor, Floored, "Floored/VNEW_floor" );

    // Count 3: VOLD + VBRA + identity (VBRA_lineage_merge)
    {
        using namespace vbra_lineage_merge;
        constexpr int32_t c = MergedFixedLineageCount;
        check( "Merged/VBRA_lineage_merge: count == 3", c == 3 && (int32_t)( sizeof(MergedFixedLineage) / 8 ) == 3 );
    }

    // Count 4: VOLD + VBRA + VBRB + identity (VNEW_lineage_merge)
    {
        using namespace vnew_lineage_merge;
        constexpr int32_t c = MergedFixedLineageCount;
        constexpr uint64_t h = MergedFixedHash;
        check( "Merged/VNEW_lineage_merge: count == 4", c == 4 && (int32_t)( sizeof(MergedFixedLineage) / 8 ) == 4 );
        check( "Merged/VNEW_lineage_merge: last == identity", MergedFixedLineage[c - 1] == h );
        check( "Merged/VNEW_lineage_merge: oldest != identity", MergedFixedLineage[0] != h );
    }

    // Count 2: VOLD + identity (VMID_floor)
    {
        using namespace vmid_floor;
        constexpr int32_t c = FlooredFixedLineageCount;
        constexpr uint64_t h = FlooredFixedHash;
        check( "Floored/VMID_floor: count == 2", c == 2 && (int32_t)( sizeof(FlooredFixedLineage) / 8 ) == 2 );
        check( "Floored/VMID_floor: last == identity", FlooredFixedLineage[c - 1] == h );
    }

    // Count 1 (singular, VOLD side)
    ASSERT_NS_TABLE( vnew_array_fixed_grow, Vec, "Vec/VNEW_array_fixed_grow" );
    ASSERT_NS_TABLE( vnew_optional_add, OptLeaf, "OptLeaf/VNEW_optional_add" );

    std::printf( "R1: %d failure(s)\n", failures );
    return failures > 0 ? 1 : 0;
}