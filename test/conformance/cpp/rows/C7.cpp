// THE cpp/C7 CELL (docs/roadmap.sexp, "ranged scalar clamp").
//
// Law: docs/FIXED-FORM-ALGORITHM.md §4.6 (line 341): a record is a positional
// image and the read loop asks nothing about what bytes mean, so a RANGED
// SCALAR's declared min and max are held by nobody in the loop — they are held
// by STRAIGHT-LINE CODE AFTER THE PLAN RUN, and NOT BY PLAN ENTRIES; THE PASS
// RUNS OVER STORAGE, which is what makes ONE pass cover BOTH plans.
// docs/FIXED-FORM-ALGORITHM.md:351 — "A RANGED SCALAR clamps to its declared
// min and max, COUNT clamped."
//
// The production entrypoint is tblfx1::FxRootFixedLoad: it runs the plan
// (TableFixedRun) and then the clamp pass (FxRootFixedClamp, through
// FxRootFixedClampBody) over every record it reads. This test writes an
// out-of-range value through the production WRITER (FxRootFixedSave — the
// write side's bounds are debug-only by rule, so a caller CAN land one on the
// wire), reads it back through the production reader, and holds the loaded
// value and the clamped counter to the law.
//
// The byte vector is built by the writer, so its derivation is the law the
// writer implements: `renamed` carries 5000, one past its declared | max = 1000;
// `gone` carries -7, one under its declared | min = 0 (test/tables/FX1.schema).
//
// Exit 0 green / exit 1 red, one printed line per assertion.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FX1Table.h"
#include "FX2Table.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures++; }
}

int main()
{
    // THE VECTOR: written by the production writer, the poison in-range values
    // the writer's debug-only bounds cannot refuse.
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 1u;
    one.renamed = 5000;  // declared | min = 0, max = 1000
    one.gone = -7;       // and the low end of the same declaration
    one.nested.a = 111;
    one.nested.b = 222;
    std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
           "C7: the writer lands the vector" );

    // THE PRODUCTION PATH: FxRootFixedLoad runs the plan and then the clamp
    // pass, both over storage.
    {
        tblfx1::FxRoot back;
        tblfx1::FxRootReset( back );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "C7: the record reads on the identity plan" );
        check( back.renamed == 1000, "C7: a value past max lands at max" );
        check( back.gone == 0, "C7: a value under min lands at min" );
        check( r.clamped == 2, "C7: two clamps, counted" );
        check( back.nested.a == 111 && back.nested.b == 222, "C7: an in-range neighbour is untouched" );
    }

    // THE NEGATIVE CONTROL, in the test and compiling: the plan run ALONE, no
    // straight-line pass after it. The same record, the same plan, and the
    // out-of-range value survives — which is what says the bound is held by
    // the pass after the loop and not by a plan entry.
    {
        tblfx1::FxRoot loose;
        tblfx1::FxRootReset( loose );
        tblfx1::TableReport r;
        const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
        tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                               body, (uint8_t *) &loose, &r );
        check( loose.renamed == 5000 && loose.gone == -7,
               "C7: the plan run alone leaves an out-of-range value standing" );
        check( r.clamped == 0, "C7: the plan run alone counts nothing" );
    }

    // ONE PASS COVERS BOTH PLANS: an FX2 reader compiles a plan from FX1's
    // layout and clamps the same ranged scalar to the same declared max.
    {
        tblfx2::FxRoot back;
        tblfx2::TableReport r;
        std::vector<tblfx2::TableFixedEntry> plan( 2048 );
        check( tblfx2::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 2048, NULL, &r ) == 1,
               "C7: the record reads on the compiled plan" );
        check( back.renamed_to == 1000, "C7: the compiled plan clamps to the same max" );
        check( r.clamped == 1, "C7: one clamp — `gone` is a field FX2 cannot name" );
    }

    return failures == 0 ? 0 : 1;
}