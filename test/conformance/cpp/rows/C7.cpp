// THE cpp/C7 CELL (docs/roadmap.sexp, "ranged scalar clamp" — now the ranged
// scalar BOUND).
//
// Law: docs/FIXED-FORM-ALGORITHM.md §4.6 (line 341): a record is a positional
// image and the read loop asks nothing about what bytes mean, so a RANGED
// SCALAR's declared min and max are held by nobody in the loop — they are held
// by STRAIGHT-LINE CODE AFTER THE PLAN RUN, and NOT BY PLAN ENTRIES; THE PASS
// RUNS OVER STORAGE, which is what makes ONE pass cover BOTH plans.
//
// THE LAW MOVED (docs/rulings.json strict-only-read; docs/plans/strict-only.md
// decision 1): no read clamps. A ranged scalar past its declared min or max
// refuses the WHOLE load `out_of_range` (the retired `clamped` renamed in
// place), and every row is left at its declared defaults (R4). The row once
// asserted "lands at the bound, clamped == 2"; it now asserts the refusal, as
// test/conformance/go/rows/C7_test.go does on the Go leg.
//
// The production entrypoint is tblfx1::FxRootFixedLoad: it runs the plan
// (TableFixedRun) and then the bound pass (FxRootFixedClamp) over every
// record it reads. The writer's bounds are debug asserts (the C++ leg's rule),
// so the out-of-range vector is FORGED on the wire after a lawful save:
// `renamed` (body +6) carries 5000, past its declared | max = 1000; `gone`
// (body +10) carries -7, under its declared | min = 0 (test/tables/FX1.schema,
// FxRootFixedWriteBody's own offsets).
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
    // THE VECTOR: a lawful record from the production writer, then the two
    // ranged scalars forged past their bounds.
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 1u;
    one.renamed = 500;
    one.gone = 9;
    one.nested.a = 111;
    one.nested.b = 222;
    std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
           "C7: the writer lands the lawful record" );
    const size_t body = (size_t) tblfx1::kTableFixedHeaderBytes + 4 + (size_t) tblfx1::FxRootFixedLayoutBytes + 8;

    // THE CONTROL: the lawful bytes read clean on the identity plan.
    {
        tblfx1::FxRoot back;
        tblfx1::FxRootReset( back );
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1
                   && !r.refused && !r.damaged,
               "C7: the lawful record reads clean on the identity plan" );
        check( back.renamed == 500 && back.gone == 9 && back.nested.a == 111 && back.nested.b == 222,
               "C7: the lawful values land" );
    }

    tblfx1::TableFixedPut32( w.data() + body + 6, (uint32_t) 5000 );
    tblfx1::TableFixedPut32( w.data() + body + 10, (uint32_t) -7 );

    // THE PRODUCTION PATH: FxRootFixedLoad runs the plan and then the bound
    // pass, both over storage. strict-only-read: was "reads, renamed == 1000,
    // gone == 0, clamped == 2, the neighbour untouched"; now the whole load
    // refuses out_of_range and every row is at its declared defaults.
    {
        tblfx1::FxRoot back;
        tblfx1::FxRootReset( back );
        back.renamed = 77; back.gone = 77; back.nested.a = 77; // poison
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == -1,
               "C7: a ranged scalar out of its bounds refuses the whole load on the identity plan" );
        check( r.refused && !r.damaged && r.refuse_reason == tblfx1::out_of_range,
               "C7: the refusal is out_of_range (the retired clamped, renamed in place)" );
        check( back.renamed == 5 && back.gone == 9 && back.nested.a == 1 && back.nested.b == 2,
               "C7: a refused load lands nothing — every field at its declared default (R4)" );
    }

    // THE NEGATIVE CONTROL, in the test and compiling: the plan run ALONE, no
    // straight-line pass after it. The same record, the same plan, and the
    // out-of-range value survives with no refusal — which is what says the
    // bound is held by the pass after the loop and not by a plan entry.
    {
        tblfx1::FxRoot loose;
        tblfx1::FxRootReset( loose );
        tblfx1::TableReport r;
        tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                               w.data() + body, (uint8_t *) &loose, &r );
        check( loose.renamed == 5000 && loose.gone == -7,
               "C7: the plan run alone leaves an out-of-range value standing" );
        // strict-only-read: was "r.clamped == 0"; the plan run alone neither
        // refuses nor damages.
        check( !r.refused && !r.damaged, "C7: the plan run alone refuses nothing" );
    }

    // ONE PASS COVERS BOTH PLANS: an FX2 reader compiles a plan from FX1's
    // layout and holds the same ranged scalar to the same declared bound.
    // strict-only-read: was "reads, renamed_to == 1000, clamped == 1"; now a
    // refusal out_of_range on the compiled plan too.
    {
        tblfx2::FxRoot back;
        tblfx2::FxRootReset( back );
        back.renamed_to = 77; // poison
        tblfx2::TableReport r;
        std::vector<tblfx2::TableFixedEntry> plan( 2048 );
        check( tblfx2::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 2048, NULL, &r ) == -1,
               "C7: the compiled plan refuses the same out-of-range value" );
        check( r.refused && !r.damaged && r.refuse_reason == tblfx2::out_of_range,
               "C7: the compiled plan's refusal is out_of_range too" );
        check( back.renamed_to == 5, "C7: the compiled plan's refused load lands nothing (declared default)" );
    }

    return failures == 0 ? 0 : 1;
}
