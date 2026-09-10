// THE FIXED FORM'S VERSIONING CONFORMANCE (docs/SPEC-TABLES.md §3.4, "held by
// test"). The invariant §3.4 states before anything else is that a fixed
// record is positional BY PLAN and the positions are the WRITER's LAYOUT, never
// the reader's own declaration — "we must not ever break versioning in fixed
// tables", the project owner. This file is what that sentence is worth.
//
// Every case below reads a record through the ONE plan-driven path, and the
// only thing that differs between them is which plan the loop was handed.
//
//   1. SAME SCHEMA           the identity plan, a static constant
//   2. AN OLDER WRITER       a field this reader has and that record does not:
//                            the prefill's declared default stands
//   3. A NEWER WRITER        a field this reader cannot name, stepped over by
//                            the size its entry states; and a whole nested
//                            TYPE this reader cannot name, stepped over by its
//                            layout size
//   4. A RENAMED FIELD       arriving under `was =`, silently
//   5. A WIDENED FIELD       uint16 into uint32, decoded exactly, counted
//   6. AN ENUM AND A UNION   a variant and an arm inserted IN THE MIDDLE,
//                            remapped by name and never by position (V1/V2)
//   7. AN OPTIONAL           P1's value against P3's `?T` (§2.3's rule, and
//                            what this form does to it)
//   8. THE NEGATIVE CONTROL  a reader given the WRONG PLAN for a record, which
//                            must come out wrong. A test that never watched
//                            the wrong plan fail never checked the right one.
//   9. THE LAYOUT'S OWN      one corrupted-layout case per NAMED RULE §3.4
//      VALIDATION            holds an untrusted peer's layout to, each
//                            breaking exactly one thing in a layout this
//                            reader accepts and each refused under its own
//                            name with nothing decoded
//  10. THE UNION ARMS        a TEXT field of each flavour and a COUNTED ARRAY
//                            under an arm, tag 0, and an arm the other side
//                            cannot name (FU1/FU2)
//  11. THE SCALAR FAMILY     the fixed-point widths, the 128-bit integers, a
//                            ranged field whose bounds TIGHTENED, and the NaN
//                            bit patterns across the f32 -> f64 rung
//  12. THE FRAME             a file of zero records, of many, one with bytes
//                            left over, and one cut short
//
// The whole matrix these cases fill, cell by cell, is docs/FIXED-FORM-COVERAGE.md.
//
// THE LAYOUT is what form 1 called the vocabulary block. It is neither §7's
// cooked block nor §19's block form.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cstdlib>
#include <initializer_list>
#include <vector>

// THE VALUES ARE NOT DECLARED HERE. They are in fixedform_fixtures.h, which
// test/tables/fixedform_dump.cpp reads too: what this file asserts a READER
// makes of a fixture, that file pins the BYTES a WRITER produces from the same
// fixture, and a fixture built twice in two places is a fixture that can
// disagree with itself.
#include "fixedform_fixtures.h"

// the OTHER generations, which only the read side needs
#include "V2Table.h"
#include "P3Table.h"
#include "Scalars2Table.h"
#include "F2Table.h"
#include "W2Table.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// ---------------------------------------------------------------------------
// THE KNOWN-RED LIST, AND WHY A GATE CARRIES ONE
//
// A fixture written for a bug the REFERENCE still has is the only kind of
// fixture that proves the bug is real, so it goes in the shared set the day it
// is written and not the day the fix lands — every leg is a twin of this file,
// so a fixture added here is a fixture in all nine, and a leg that quietly
// disagrees with the reference is exactly what this set exists to find.
//
// SO THE RED IS NAMED AND REPORTED, NEVER HIDDEN. A case listed below may fail
// and the run still ends green, with the failure PRINTED and the fix it waits
// on named beside it. And the list is held from BOTH ENDS: a listed case that
// starts PASSING turns the run RED, because a known-red nobody deletes is a
// gate that has stopped covering something. Deleting the entry is part of
// landing the fix.
struct KnownRed
{
    const char * key;
    const char * waits_on;
    int reached;
    int failed;
};

static KnownRed known_red[] = {
    // THE PLAN ENTRY'S `arg` LANE CARRIES TWO THINGS (docs/SPEC-TABLES.md §3.4,
    // "THE ONE READER PATH: A PLAN"). A guarded entry's `arg` is the union
    // ARM ORDINAL the read loop tests the tag against; a `text` entry's `arg`
    // is the field's FLAVOUR — utf8, wide, or bytes. They are one byte, so a
    // text field under a union arm cannot carry both:
    //
    //   the IDENTITY plan stamps the arm ordinal over the flavour, so a
    //   `string(N)` under arm 2 is read as WIDE text: its capacity halves and
    //   its length is clamped, silently;
    //
    //   the COMPILED plan writes the flavour over the arm ordinal, so the
    //   guard test compares the tag against a flavour and the entry is SKIPPED
    //   whenever the two numbers differ — the field is dropped and nothing
    //   counts.
    //
    // Both are silent, both are wrong values out of a clean read, and neither
    // is reachable by any fixture that has no text under an arm.
    { "arg-lane/identity/text-flavour-under-an-arm", "reference fix 12 (the plan entry's arg lane)", 0, 0 },
    { "arg-lane/compiled/text-dropped-under-an-arm", "reference fix 12 (the plan entry's arg lane)", 0, 0 },
    // §3.4's op table names a `clamp` op — "reconstruct against the writer's
    // declared range and apply the reader's own, `clamped` counts if it fired"
    // — and the reference's op set has no such op: a ranged field whose reader
    // declares TIGHTER bounds than the writer is copied through unclamped, so
    // a value outside the reader's own declared range lands as if it were in
    // it. Scalars/Scalars2 is the pair whose bounds tighten, and this is the
    // cell that says the op is missing rather than untested.
    { "clamp-op/compiled/tightened-bounds-do-not-clamp", "a `clamp` op in the reference's read loop (§3.4's op table)", 0, 0 },
    // §3.4's op table: a `text` entry applies "the content rules of §3" and "a
    // violation is `malformed`". The reference's text op validates the LENGTH
    // and never the CONTENT, so invalid UTF-8 in a `string(N)`'s used bytes is
    // read through as if it were text.
    { "text-content/identity/invalid-utf8-is-not-malformed", "UTF-8 validation in the reference's text op (§3.4's op table)", 0, 0 },
    // THE RUN COPY'S 17..31-BYTE BRANCH, and it is the worst thing in this
    // file. §3.4 requires the run copy to be "OVERLAPPING UNALIGNED WORD MOVES
    // AND NOT A CALL", and the reference's branch for a run of more than
    // sixteen bytes performs a sixteen, a second sixteen, and then a
    // THIRTY-TWO anchored at the run's END. For a run of 17..31 bytes that
    // last move is anchored BEFORE the run begins: it reads `32 - n` bytes in
    // front of the source and writes `32 - n` bytes in front of the
    // destination, and it reads up to `32 - n` bytes PAST the record body —
    // which for the last record of a file is past the buffer.
    //
    // So one defect wears five faces on `Scalars` alone: `span`'s high half,
    // `weights_count`, `seeds_count`, `pose.heading` and `spawn_present` are
    // each clobbered by a LATER entry's run, and `spawn_present` is read from
    // outside the record. Every one of them is SILENT — the report comes back
    // six zeros and a clean verdict.
    //
    // IT IS ALSO A DIVERGENCE THAT POINTS THE WRONG WAY. A leg whose run copy
    // is a plain `memcpy`, or that has no such micro-optimization at all, is
    // GREEN here: C++ and C are the two legs that carry the branch, so the
    // reference is the odd one out and the conformance set is what says so.
    { "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours", "the run copy's 17..31-byte branch (internal/codegen/cpptable/fixedruntime.go and ctable's twin)", 0, 0 },
    // §3.4's `?T` row: "the payload rides WHOLE whether or not it is present",
    // "ZERO on write when the flag is 0, IGNORED on read". IGNORED is the word
    // that matters. The reference copies the payload whatever the flag says, so
    // an absent optional over a peer's NON-ZERO residue hands a caller a value
    // its writer never sent, under a present flag that correctly reads false.
    // A caller that checks the flag is fine; a caller that reads the payload
    // first — which is what a `Reset`-then-fill prefill invites — is not.
    { "optional/absent-payload-residue-is-copied", "the `?T` payload gated on the present byte (§3.4's `?T` row: IGNORED on read)", 0, 0 },
    // AN ORDINAL PAST THE LAST VARIANT, AND A TAG PAST THE LAST ARM. §3.4 fixes
    // what an ordinal MEANS — "the variant's POSITION IN THE LAYOUT, from 1,
    // and 0 is None" — and says nothing about one that names no position. The
    // two plans have already answered differently: the COMPILED plan resolves
    // through its remap and lands `None`, which is §4's answer for a name this
    // reader does not have; the IDENTITY plan copies the byte, so a caller gets
    // an enum value and a union tag that name nothing, into a typed slot, with
    // no counter moved.
    //
    // SO THESE TWO ARE PINNED ON THE PROPERTY AND NOT ON THE ANSWER. Whichever
    // way the ruling goes, THE TWO PATHS MUST AGREE — a form whose cost does
    // not move with the writer's version (§3.4's whole design statement) is a
    // form whose ANSWERS do not either. That assertion survives either ruling
    // and it is red today.
    // A BOOL BYTE OUTSIDE {0, 1}. §3.4 gives `bool` a `C` of 1 and says nothing
    // about the other 254 values, and the read is a COPY — so the byte lands in
    // the caller's storage as it stood on the wire. In C++ that storage is a
    // `bool`, a `bool` holding 2 is outside its own type's value set, and every
    // read of it after that is UNDEFINED. It is a one-instruction fix on the
    // read side (normalise to `byte != 0`) and it needs the ruling first,
    // because normalising is a decision about the WIRE and not about C++: nine
    // ports have to answer a stranger's byte the same way.
    { "bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool", "a ruling on what a bool byte outside {0, 1} means, and a normalise on the read side to match it", 0, 0 },
    { "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant", "a ruling: §3.4 does not say what an ordinal naming no variant means, and the identity and compiled plans answer differently", 0, 0 },
    // §4: a kind mismatch is "skipped, NEVER MISDECODED, counted", and the
    // field takes its declared default. `Scalars2` respells `angle` from a
    // fixed(16, 16) to a plain int32; the read counts the mismatch and hands
    // back the writer's raw scaled integer anyway.
    //
    // THIS ONE IS NOT YET SEPARATED FROM THE RUN COPY ABOVE, and saying so is
    // the honest state of it: `angle` sits four bytes into the body, inside the
    // window that branch clobbers, so a spill from a neighbouring run could
    // deposit exactly these bytes. The fix for the run copy may close this row
    // too, and until one of them lands neither can be blamed alone.
    { "kind-mismatch/compiled/a-moved-kind-is-decoded-anyway", "the run copy's branch first, then a re-read: `angle` is inside the window that branch clobbers and the two cannot be told apart until it is fixed", 0, 0 },
    { "ordinal-bound/paths-disagree/union-tag-past-the-last-arm", "a ruling: §3.4 does not say what a tag naming no arm means, and the identity and compiled plans answer differently", 0, 0 },
};

// ---------------------------------------------------------------------------
// A KNOWN RED THAT IS ALSO A FAULT, AND WHY IT IS SKIPPED IN ONE BINARY OF TWO
//
// `run-copy/...` is not merely a wrong value: the broken branch READS PAST THE
// RECORD BODY, and for the last record of a file that is past the buffer. The
// plain binary reports it as the wrong values it produces; the SANITIZED twin
// cannot report it at all, because a heap-buffer-overflow halts the process and
// every case behind it goes unrun.
//
// SO THE FIXTURE IS SKIPPED IN THE SANITIZED TWIN AND THE FAULT IS WATCHED
// SEPARATELY, never dropped. `make tables-fixedform` runs the sanitized twin
// once more with `SCHEMA_FIXEDFORM_FAULT=1`, which puts the fixture back, and
// REQUIRES the sanitizer to name the overflow. That gate is held from both ends
// exactly as the known-red list is: the day the run copy is fixed, the fault
// stops happening, that run stops failing, and the target goes red until this
// skip and its Makefile line are deleted together.
//
// A skip nobody watches fail is a skip that has quietly become a hole.
static bool sanitized_skip( const char * what, std::initializer_list<const char *> keys )
{
#if defined( SCHEMA_FIXEDFORM_SANITIZED )
    const char * fault = std::getenv( "SCHEMA_FIXEDFORM_FAULT" );
    if ( fault != NULL && fault[0] != '\0' ) { return false; } // the caller asked for the fault
    std::printf( "KNOWN-FAULT (skipped in the sanitized twin, run with SCHEMA_FIXEDFORM_FAULT=1 to see it): %s\n", what );
    for ( const char * key : keys )
    {
        for ( KnownRed & r : known_red )
        {
            if ( std::strcmp( r.key, key ) == 0 ) { r.reached++; r.failed++; }
        }
    }
    return true;
#else
    (void) what; (void) keys;
    return false;
#endif
}

static void check_red( bool ok, const char * key, const char * what )
{
    for ( KnownRed & r : known_red )
    {
        if ( std::strcmp( r.key, key ) != 0 ) { continue; }
        r.reached++;
        if ( !ok ) { r.failed++; std::printf( "KNOWN-RED [%s]: %s\n", r.key, what ); }
        return;
    }
    check( ok, what ); // an unlisted key is an ordinary check, never a pass
}

// THE GATE'S OWN REPORT. It prints every listed case and what it is waiting
// for, so a green run still says out loud what this form does not do yet.
static int known_red_report()
{
    int bad = 0;
    std::printf( "KNOWN-RED, %d case(s), each printed above and each waiting on a named fix:\n",
                 (int) ( sizeof( known_red ) / sizeof( known_red[0] ) ) );
    for ( const KnownRed & r : known_red )
    {
        std::printf( "  %-52s  %s  <- %s\n", r.key,
                     r.failed > 0 ? "RED  " : ( r.reached > 0 ? "GREEN" : "UNRUN" ), r.waits_on );
        if ( r.reached == 0 )
        {
            std::printf( "FAIL: KNOWN-RED case %s was never reached: the fixture that carries it is gone\n", r.key );
            bad++;
        }
        else if ( r.failed == 0 )
        {
            std::printf( "FAIL: KNOWN-RED case %s PASSES now — delete it from known_red[] as part of landing %s\n",
                         r.key, r.waits_on );
            bad++;
        }
    }
    return bad;
}

// ---------------------------------------------------------------------------

static void fx_case()
{
    // an FX1 record, every field off its default so nothing passes by accident
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 4242u;
    one.narrow = 40000u;      // a uint16 value the widened read must reproduce
    one.renamed = 321;
    one.gone = 654;
    one.nested.a = 111;
    one.nested.b = 222;

    std::vector<uint8_t> w1( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w1.data(), (int64_t) w1.size() ) == (int64_t) w1.size(), "FX1 save" );

    // 1. SAME SCHEMA — the identity plan
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, &r );
        check( n == 1, "same schema: one record" );
        check( back.keep == 4242u && back.narrow == 40000u && back.renamed == 321 && back.gone == 654, "same schema: the scalars" );
        check( back.nested.a == 111 && back.nested.b == 222, "same schema: the nesting" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "same schema: a clean read moves no counter" );
    }

    // 2, 4, 5. FX2 READS FX1 — a plan compiled from FX1's layout
    {
        tblfx2::FxRoot back;
        tblfx2::TableReport r;
        std::vector<tblfx2::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, &r );
        check( n == 1, "older writer: one record" );
        check( back.keep == 4242u, "older writer: an unmoved field" );
        check( back.narrow == 40000u, "WIDENED: uint16 into uint32, exactly" );
        check( r.widened == 1, "WIDENED: one widened counts" );
        check( back.renamed_to == 321, "RENAMED: `was =` keeps the wire id" );
        check( back.added == 11, "MISSING: a field the writer does not carry takes its declared default" );
        check( back.extra.x == 0 && back.extra.y == 0, "MISSING: a whole nested type takes its defaults" );
        check( back.nested.a == 111 && back.nested.b == 222, "older writer: the nesting" );
        check( r.unknown == 1, "older writer: `gone` is the one field this reader cannot name" );
        check( r.kind_mismatch == 0 && !r.malformed && !r.refused, "older writer: nothing else fired" );
    }

    // 3. FX1 READS FX2 — an unknown field and an unknown NESTED TYPE
    {
        tblfx2::FxRoot two;
        tblfx2::FxRootReset( two );
        two.keep = 5150u;
        two.narrow = 70000u;
        two.renamed_to = 808;
        two.added = 909;
        two.nested.a = 33;
        two.nested.b = 44;
        two.extra.x = 55;
        two.extra.y = 66;
        std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
        check( tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "FX2 save" );

        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, &r );
        check( n == 1, "newer writer: one record" );
        check( back.keep == 5150u, "newer writer: an unmoved field lands past the unknowns" );
        check( back.renamed == 808, "newer writer: `was =` reads the other way too" );
        check( back.gone == 9, "newer writer: a field the writer dropped takes its declared default" );
        check( back.nested.a == 33 && back.nested.b == 44, "newer writer: the nesting lands past the unknown type" );
        // `added` is an unknown FIELD; `extra` is an unknown nested TYPE, and
        // stepping over it by its layout size is what puts `nested` in the
        // right place above
        check( r.unknown == 2, "newer writer: two names this reader does not have" );
        check( r.kind_mismatch == 1, "newer writer: uint32 into uint16 is a kind that moved, not a widening" );
        check( back.narrow == 3, "newer writer: a narrowing leaves the declared default" );
        check( !r.malformed && !r.refused, "newer writer: no damage and no refusal" );
    }
}

// ---------------------------------------------------------------------------

static void v_case()
{
    tblv1::Cfg one;
    tblv1::CfgReset( one );
    one.a = 42;
    std::strcpy( one.name, "hello" );
    one.name_length = 5;
    one.grade = tblv1::Grade::Gold;         // V2 inserts Silver BEFORE Gold
    one.effect.type = tblv1::EffectType::Ward;
    one.effect.ward.charge = 0.75f;         // V2 inserts hex BEFORE ward
    one.tokens[tblv1::Slot::Alpha] = 21;
    one.tokens[tblv1::Slot::Delta] = 24;    // V2 slides Beta and keeps Delta
    one.tier_present = true;
    one.tier = 77;

    std::vector<uint8_t> w( (size_t) tblv1::CfgFixedMeasure( 1 ) );
    check( tblv1::CfgFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "V1 save" );

    tblv2::Cfg back;
    tblv2::TableReport r;
    std::vector<tblv2::TableFixedEntry> plan( 8192 );
    const int64_t n = tblv2::CfgFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 8192, &r );
    check( n == 1, "V2 reads V1: one record" );
    check( back.grade == tblv2::Grade::Gold, "ENUM: a variant inserted in the middle is remapped by NAME" );
    check( back.effect.type == tblv2::EffectType::Ward, "UNION: an arm inserted in the middle is remapped by NAME" );
    check( back.effect.ward.charge == 0.75f, "UNION: the arm's payload lands" );
    check( std::strcmp( back.title, "hello" ) == 0 && back.title_length == 5, "RENAMED: title reads name's bytes" );
    check( back.tokens[tblv2::Slot::Alpha] == 21, "KEYED: a slot whose key did not move" );
    check( back.tokens[tblv2::Slot::Delta] == 24, "KEYED: a slot whose key SLID keeps its value" );
    check( back.tokens[tblv2::Slot::Sigma] == 0, "KEYED: a key the writer has no name for takes its default" );
    check( back.tier_present && back.tier == 77, "OPTIONAL: the present flag and the payload" );
    check( back.c, "MISSING: V2's own `c` takes its declared default" );
    check( back.a == 5.0f, "KIND MOVED: int32 -> float32 leaves the declared default" );
    check( !r.malformed && !r.refused, "V2 reads V1: no damage and no refusal" );
}

// ---------------------------------------------------------------------------

static void p_case()
{
    // P1 nests Link BY VALUE and P3 marks the same field `?Link`. On FORM 1
    // those two are wire-identical (§2.3). ON THIS FORM THEY ARE NOT: an
    // optional carries a present byte in front of a payload that rides whole,
    // so the layout says kind 35 on one side and kind 13 on the other, and the
    // edit is REPORTED rather than silent. That is the departure from §2.3
    // this form makes, and it is here so that it is a pinned fact and not a
    // surprise.
    tblp1::Chain one;
    tblp1::ChainReset( one );
    std::strcpy( one.name, "chain" );
    one.name_length = 5;
    one.link.value = 500;

    std::vector<uint8_t> w( (size_t) tblp1::ChainFixedMeasure( 1 ) );
    check( tblp1::ChainFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "P1 save" );

    tblp3::Chain back;
    tblp3::TableReport r;
    std::vector<tblp3::TableFixedEntry> plan( 1024 );
    const int64_t n = tblp3::ChainFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, &r );
    check( n == 1, "P3 reads P1: one record" );
    check( std::strcmp( back.name, "chain" ) == 0, "P3 reads P1: the plain field lands" );
    check( r.kind_mismatch == 1, "OPTIONAL vs VALUE is a reported kind on this form, never a silent reread" );
}

// ---------------------------------------------------------------------------

// THE UNION ARMS (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-COVERAGE.md). FX1/FX2
// hold the scalar edits and V1/V2 hold one arm inserted in the middle; neither
// puts TEXT or a COUNTED ARRAY under an arm, and an arm is where the plan
// entry's `arg` lane has to carry two things at once. So: one arm per text
// flavour, in ordinal order, so ordinal and flavour disagree everywhere but the
// first arm; an arm carrying a counted array; the tag-0 case; and an arm the
// other generation has no name for.
//
// A guarded entry's op is tested BOTH WAYS, because the two halves of the arg
// lane fail in opposite directions: the identity plan loses the FLAVOUR and the
// compiled plan loses the ORDINAL.

template <typename Root, typename Measure, typename Save>
static std::vector<uint8_t> fu_file( const Root & r, Measure measure, Save save, const char * what )
{
    std::vector<uint8_t> f( (size_t) measure( 1 ) );
    check( save( &r, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), what );
    return f;
}

static void fu_case()
{
    // ---- 1. THE IDENTITY PLAN over each arm ------------------------------
    //
    // The reader's own hash, so the plan is the static constant the compiler
    // wrote. Every value must come back exactly, text included.
    {
        tblfu1::MarkRoot r;
        FillFu1Wide( r );
        std::vector<uint8_t> f = fu_file( r, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: wide" );
        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &rep );
        check( n == 1, "ARM/identity/wide: one record" );
        check( back.mark.type == tblfu1::MarkType::Wide, "ARM/identity/wide: the tag" );
        check( back.mark.wide.n == 31 && back.id == 1001u && back.after == 9, "ARM/identity/wide: the scalars beside the text" );
        check_red( back.mark.wide.w_length == 3 && back.mark.wide.w[0] == (char16_t) 0x0041 &&
                   back.mark.wide.w[1] == (char16_t) 0x0142 && back.mark.wide.w[2] == (char16_t) 0x0043 &&
                   back.mark.wide.w[3] == (char16_t) 0,
                   "arg-lane/identity/text-flavour-under-an-arm",
                   "wstring(4) under ARM 1 read as narrow text: the terminator lands at the length instead of twice it" );
        check( rep.unknown == 0 && rep.kind_mismatch == 0 && !rep.malformed && !rep.refused,
               "ARM/identity/wide: a clean read moves no counter" );
    }
    {
        tblfu1::MarkRoot r;
        FillFu1Narrow( r );
        std::vector<uint8_t> f = fu_file( r, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: narrow" );
        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &rep );
        check( n == 1, "ARM/identity/narrow: one record" );
        check( back.mark.type == tblfu1::MarkType::Narrow, "ARM/identity/narrow: the tag" );
        check( back.mark.narrow.n == 55, "ARM/identity/narrow: the scalar beside the text" );
        check_red( back.mark.narrow.s_length == 5 && std::strcmp( back.mark.narrow.s, "hello" ) == 0 && rep.clamped == 0,
                   "arg-lane/identity/text-flavour-under-an-arm",
                   "string(6) under ARM 2 read as WIDE text: the bound halves and the length is clamped to 3" );
    }
    {
        tblfu1::MarkRoot r;
        FillFu1Raw( r );
        std::vector<uint8_t> f = fu_file( r, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: raw" );
        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        check( tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &rep ) == 1,
               "ARM/identity/raw: one record" );
        check( back.mark.type == tblfu1::MarkType::Raw && back.mark.raw.d_length == 4 && back.mark.raw.n == 77 &&
               back.mark.raw.d[0] == 0xDEu && back.mark.raw.d[3] == 0xEFu,
               "ARM/identity/raw: bytes(4) at USED == MAX, and no terminator is written past it" );
        check( rep.clamped == 0, "ARM/identity/raw: a used length at the bound is not a clamp" );
    }
    {
        tblfu1::MarkRoot r;
        FillFu1List( r );
        std::vector<uint8_t> f = fu_file( r, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: list" );
        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        check( tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &rep ) == 1,
               "ARM/identity/list: one record" );
        check( back.mark.type == tblfu1::MarkType::List && back.mark.list.items_count == 2 &&
               back.mark.list.items[0] == 11 && back.mark.list.items[1] == 22 && back.mark.list.n == 99,
               "ARM/identity/list: a COUNTED ARRAY under an arm, at MAX" );
        check( rep.clamped == 0, "ARM/identity/list: a count at the bound is not a clamp" );
    }
    {
        // TAG 0 IS `None` AND IT IS NOT AN ARM. Every guarded entry's tag test
        // fails, so the arm storage keeps the prefill and no arm's bytes are
        // read into another arm's slot.
        tblfu1::MarkRoot r;
        tblfu1::MarkRootReset( r );
        r.id = 1005u;
        r.after = 4;
        std::vector<uint8_t> f = fu_file( r, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: none" );
        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        check( tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &rep ) == 1,
               "ARM/identity/none: one record" );
        check( back.mark.type == tblfu1::MarkType::None, "TAG 0: `None` is not an arm and selects none of them" );
        check( back.id == 1005u && back.after == 4, "TAG 0: the fields either side of the union still land" );
    }
    {
        // A TAG BEYOND THE ARMS, poked onto the wire. `Mark` has four arms, so
        // `9` names none of them, and §3.4 does not say what that means — it
        // fixes what a tag from `1` to the arm count means and what `0` means
        // and stops there.
        //
        // WHAT IS NOT OPTIONAL IS THAT THE TWO READER PATHS AGREE. This form's
        // design statement is that there is ONE reader path and the only thing
        // that differs is which plan it was handed; two plans that answer one
        // byte differently is the performance cliff read back as a CORRECTNESS
        // cliff, where what a caller gets depends on whether the peer happened
        // to be this build. So this pins the two answers AGAINST EACH OTHER and
        // leaves the ruling free to go either way.
        tblfu1::MarkRoot r;
        FillFu1Narrow( r );
        std::vector<uint8_t> f = fu_file( r, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: for the bad tag" );
        const size_t body = (size_t) tblfu1::kTableFixedHeaderBytes + 4 + (size_t) tblfu1::MarkRootFixedLayoutBytes + 8;
        f[body + 4] = 9u; // the tag byte, past the last arm
        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 1024 );
        check( tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &rep ) == 1,
               "TAG PAST THE ARMS: still one record" );
        check( back.id == 1002u && back.after == 9, "TAG PAST THE ARMS: the fields either side of the union still land" );
        check( !rep.malformed && !rep.refused, "TAG PAST THE ARMS: never damage and never a refusal" );

        // the same bytes down the OTHER path, and the two answers compared
        tblfu2::MarkRoot two;
        tblfu2::TableReport rep2;
        std::vector<tblfu2::TableFixedEntry> plan2( 4096 );
        check( tblfu2::MarkRootFixedLoad( &two, 1, f.data(), (int64_t) f.size(), plan2.data(), 4096, &rep2 ) == 1,
               "TAG PAST THE ARMS: the compiled plan reads the record too" );
        check( two.mark.type == tblfu2::MarkType::None,
               "TAG PAST THE ARMS/compiled: a tag naming no arm resolves to None, which is §4's answer for a name this reader has not got" );
        check_red( (int) back.mark.type == (int) two.mark.type,
                   "ordinal-bound/paths-disagree/union-tag-past-the-last-arm",
                   "a tag of 9 over a four-armed union: the compiled plan lands None and the identity plan copies the 9 into the tag slot" );
    }

    // ---- 2. THE COMPILED PLAN, both directions ---------------------------
    //
    // FU2 inserts `skip` as arm 2, so every ordinal past the first moves: an
    // arm's ordinal on the wire is never the arm's ordinal in the reader.
    {
        tblfu1::MarkRoot one;
        FillFu1Narrow( one );
        std::vector<uint8_t> f = fu_file( one, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: narrow for FU2" );

        tblfu2::MarkRoot back;
        tblfu2::TableReport rep;
        std::vector<tblfu2::TableFixedEntry> plan( 4096 );
        const int64_t n = tblfu2::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &rep );
        check( n == 1, "ARM/compiled/older writer: one record" );
        check( back.mark.type == tblfu2::MarkType::Narrow,
               "ARM/compiled: an arm is resolved BY NAME, so ordinal 2 lands on ordinal 3" );
        check( back.mark.narrow.n == 55, "ARM/compiled: the scalar beside the text lands under the arm's own guard" );
        check( back.tail == 3, "ARM/compiled: a field the writer does not carry takes its declared default" );
        check_red( back.mark.narrow.s_length == 5 && std::strcmp( back.mark.narrow.s, "hello" ) == 0,
                   "arg-lane/compiled/text-dropped-under-an-arm",
                   "string(6) under an arm: the compiled entry's guard tests the tag against the FLAVOUR, so the text is dropped" );
        check( !rep.malformed && !rep.refused, "ARM/compiled/older writer: no damage and no refusal" );
    }
    {
        tblfu1::MarkRoot one;
        FillFu1Wide( one );
        std::vector<uint8_t> f = fu_file( one, tblfu1::MarkRootFixedMeasure, tblfu1::MarkRootFixedSave, "FU1 save: wide for FU2" );
        tblfu2::MarkRoot back;
        tblfu2::TableReport rep;
        std::vector<tblfu2::TableFixedEntry> plan( 4096 );
        check( tblfu2::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &rep ) == 1,
               "ARM/compiled/wide: one record" );
        check( back.mark.type == tblfu2::MarkType::Wide, "ARM/compiled/wide: an UNMOVED arm's tag" );
        check_red( back.mark.wide.w_length == 3 && back.mark.wide.w[1] == (char16_t) 0x0142,
                   "arg-lane/compiled/text-dropped-under-an-arm",
                   "wstring(4) under ARM 1: the guard tests the tag against flavour 2, so an arm at ordinal 1 drops its text" );
    }
    {
        // AN ARM THIS READER HAS NO NAME FOR. FU2 selects `skip`; FU1 has no
        // such arm, so no entry of FU1's plan answers that tag, the union keeps
        // its prefill, and the fields either side of it still land.
        tblfu2::MarkRoot two;
        tblfu2::MarkRootReset( two );
        two.id = 2001u;
        two.after = 6;
        two.tail = 8;
        two.mark.type = tblfu2::MarkType::Skip;
        two.mark.skip.e = 42;
        std::vector<uint8_t> f = fu_file( two, tblfu2::MarkRootFixedMeasure, tblfu2::MarkRootFixedSave, "FU2 save: skip" );

        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 4096 );
        const int64_t n = tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &rep );
        check( n == 1, "ARM/compiled/newer writer: one record" );
        check( back.mark.type == tblfu1::MarkType::None,
               "AN ARM WITH NO NAME HERE: the union keeps its prefill and never another arm's bytes" );
        check( back.id == 2001u && back.after == 6,
               "AN ARM WITH NO NAME HERE: the arm's whole extent is stepped over, so `after` still lands" );
        check( rep.unknown == 1, "AN ARM WITH NO NAME HERE: `tail` is the one FIELD this reader cannot name" );
        check( !rep.malformed && !rep.refused, "ARM/compiled/newer writer: no damage and no refusal" );
    }
    {
        // AND THE COUNTED ARRAY UNDER AN ARM, THROUGH A COMPILED PLAN: the
        // count entry and the elements behind it are all guarded, so the arm's
        // ordinal has to survive a `count` op as well as a `copy`.
        tblfu2::MarkRoot two;
        tblfu2::MarkRootReset( two );
        two.id = 2002u;
        two.after = 5;
        two.mark.type = tblfu2::MarkType::List;
        two.mark.list.items[0] = 71;
        two.mark.list.items[1] = 72;
        two.mark.list.items_count = 2;
        two.mark.list.n = 13;
        std::vector<uint8_t> f = fu_file( two, tblfu2::MarkRootFixedMeasure, tblfu2::MarkRootFixedSave, "FU2 save: list" );

        tblfu1::MarkRoot back;
        tblfu1::TableReport rep;
        std::vector<tblfu1::TableFixedEntry> plan( 4096 );
        check( tblfu1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &rep ) == 1,
               "ARM/compiled/list: one record" );
        check( back.mark.type == tblfu1::MarkType::List, "ARM/compiled/list: ordinal 5 lands on ordinal 4, by name" );
        check( back.mark.list.items_count == 2 && back.mark.list.items[0] == 71 && back.mark.list.items[1] == 72 &&
               back.mark.list.n == 13, "ARM/compiled/list: a COUNTED ARRAY under an arm survives the plan compile" );
    }
}

// ---------------------------------------------------------------------------

// THE SCALAR FAMILY (docs/SPEC-TABLES.md §3.4's `C` table, docs/FIXED-FORM-COVERAGE.md).
// FX1/FX2 and V1/V2 between them carry uint16, uint32, int32, float32, bool, an
// enum, a string and the containers; NOTHING in the set carried the fixed-point
// family, the 128-bit integers, or a ranged field at all until this case. They
// are the widths whose STORAGE IMAGE is the thing this form rides — a
// `ufixed(8, 8)` is a uint16 in all nine ports and a `bits(12)` a uint32 — so a
// port that models one of them differently is a port whose records nobody else
// reads, and a fixed root is where that is visible in one memcmp.
//
// Scalars/Scalars2 is the pair that already exists for exactly these edits on
// form 1 (`angle` respelled as a plain integer, `position` and `speed` with
// TIGHTER bounds, `flux` narrowed, `ticks` and `entity_id` gone), so this case
// runs the same pair through form 3 rather than declaring a tenth schema.

static void s_case()
{
    if ( sanitized_skip( "SCALARS: the run copy reads past the record body, which halts a sanitized process",
                         { "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                           "clamp-op/compiled/tightened-bounds-do-not-clamp",
                           "kind-mismatch/compiled/a-moved-kind-is-decoded-anyway" } ) )
    {
        return;
    }

    scalardemo::SimState one;
    FillScalars( one );
    std::vector<uint8_t> f( (size_t) scalardemo::SimStateFixedMeasure( 1 ) );
    check( scalardemo::SimStateFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(),
           "Scalars save" );

    // 1. THE IDENTITY PLAN over the whole family, every value off its default
    {
        scalardemo::SimState back;
        scalardemo::TableReport r;
        std::vector<scalardemo::TableFixedEntry> plan( 4096 );
        const int64_t n = scalardemo::SimStateFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &r );
        check( n == 1, "SCALARS/identity: one record" );
        check( back.tilt == one.tilt && back.angle == one.angle && back.position == one.position &&
               back.ticks == one.ticks && back.ratio == one.ratio &&
               back.speed == one.speed && back.frames == one.frames && back.scale == one.scale,
               "SCALARS/identity: the fixed-point family at every storage width" );
        // `span` is a ufixed(48, 16) AT ITS FULL EXTENT, and the entry behind it
        // is a twenty-byte run: the run copy's third move is anchored twelve
        // bytes in FRONT of it and writes `speed`'s raw bytes over span's high
        // half. A value that fits in 48 bits is the only one that shows it.
        check_red( back.span == one.span,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "ufixed(48, 16) at full extent: the following 20-byte run writes over its high four bytes" );
        check( back.reach == one.reach && back.mass == one.mass && back.flux == one.flux &&
               back.energy == one.energy && back.entity_id == one.entity_id,
               "SCALARS/identity: int128 and uint128, the low half then the high" );
        check( back.samples[0] == one.samples[0] && back.samples[1] == one.samples[1] &&
               back.samples[2] == one.samples[2], "SCALARS/identity: a FIXED array of a fixed-point type" );
        check( back.weights[0] == 256u && back.weights[3] == 2048u,
               "SCALARS/identity: a COUNTED array at MAX, its elements" );
        check_red( back.weights_count == 4,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "a counted array's COUNT: the 24-byte run behind it starts eight bytes early and writes two elements over it" );
        check( back.axes[scalardemo::Axis::X] == one.axes[scalardemo::Axis::X] &&
               back.axes[scalardemo::Axis::Y] == one.axes[scalardemo::Axis::Y],
               "SCALARS/identity: an ENUM-KEYED array of a 64-bit fixed-point type" );
        check( back.seeds[0] == one.seeds[0] && back.seeds[1] == one.seeds[1],
               "SCALARS/identity: a COUNTED array of uint128, its elements" );
        check_red( back.seeds_count == 2,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "a counted array of uint128: its COUNT is written over by the 20-byte run behind it" );
        check( back.pose.x == one.pose.x, "SCALARS/identity: the nested type's leading field" );
        check_red( back.pose.heading == one.pose.heading,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "a nested type's LAST field: the optional behind it runs twelve bytes early and writes the tail of pose.y over it" );
        check( back.spawn.x == one.spawn.x && back.spawn.heading == one.spawn.heading,
               "SCALARS/identity: an OPTIONAL of a nested type, its payload" );
        // AND THIS ONE IS NOT ONLY A WRONG VALUE. The move that lands on the
        // present flag reads eleven bytes PAST the record body, which for the
        // last record of a file is past the buffer — so what this flag holds is
        // whatever followed the allocation. The sanitized twin of this binary
        // is what turns that from a wrong answer into a named fault.
        check_red( back.spawn_present,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "an OPTIONAL's PRESENT FLAG, read from past the end of the record body: whatever follows the buffer decides it" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "SCALARS/identity: a clean read moves no counter" );
    }

    // 2. THE COMPILED PLAN: the same record read by the later build
    {
        scalardemo2::SimState back;
        scalardemo2::TableReport r;
        std::vector<scalardemo2::TableFixedEntry> plan( 4096 );
        const int64_t n = scalardemo2::SimStateFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &r );
        check( n == 1, "SCALARS/compiled: one record" );
        check( back.ratio == one.ratio && back.span == one.span &&
               back.frames == one.frames && back.scale == one.scale,
               "SCALARS/compiled: the unchanged widths land through a compiled plan" );
        // `tilt` IS THE FIRST FIELD OF THE BODY, at destination offset zero, and
        // the run copy's broken branch anchors its last move BEFORE the run — so
        // on this plan the field at offset zero is written from bytes in front
        // of its own source. It is the same defect the identity plan shows four
        // fields further in, met at the other end of the record.
        check_red( back.tilt == one.tilt,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "the field at DESTINATION OFFSET ZERO: the run copy's third move is anchored in front of the struct, so tilt is written from the wrong bytes" );
        check( back.reach == one.reach && back.energy == one.energy && back.mass == one.mass,
               "SCALARS/compiled: the 128-bit fields land" );
        check( back.pose.x == one.pose.x && back.spawn.x == one.spawn.x,
               "SCALARS/compiled: the nested type and the optional's payload" );
        check_red( back.spawn_present,
                   "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
                   "the same present flag through a COMPILED plan: the defect is in the copy primitive, so the plan it came from does not matter" );
        check( r.unknown == 2, "SCALARS/compiled: `ticks` and `entity_id` are the two names this reader has not got" );
        check( r.kind_mismatch == 2, "SCALARS/compiled: `angle` respelled and `flux` narrowed are two kinds that MOVED" );
        check( back.flux == 0,
               "SCALARS/compiled: a kind that moved leaves the declared default, never a reinterpretation" );
        // §4 IS UNCONDITIONAL ABOUT THIS: a kind mismatch is "skipped, NEVER
        // MISDECODED, counted" and the field takes its declared default. Both
        // `angle` and `flux` moved kind and both were counted — the report says
        // `kind_mismatch == 2` above — and `flux` defaults while `angle` comes
        // back holding the writer's raw fixed(16, 16) scale reinterpreted as an
        // int32. A counter that fired next to a value that landed anyway is the
        // worst of the two failures, because the report says the reader knew.
        check_red( back.angle == 0,
                   "kind-mismatch/compiled/a-moved-kind-is-decoded-anyway",
                   "`angle` respelled fixed(16, 16) -> int32: kind_mismatch is counted AND the raw scale is handed back, where §4 requires the declared default" );
        check( !r.malformed && !r.refused, "SCALARS/compiled: no damage and no refusal" );
        // §3.4's op table names a `clamp`: "reconstruct against the writer's
        // declared range and apply the reader's own, `clamped` counts if it
        // fired". `position` rides 20000 whole units and this reader declares
        // [-1000, 1000]; `speed` rides 500 against [0, 10].
        check_red( back.position == (int64_t) 1000 * 65536 && back.speed == 10u * 65536u && r.clamped == 2,
                   "clamp-op/compiled/tightened-bounds-do-not-clamp",
                   "a ranged field whose reader's bounds TIGHTENED is copied through unclamped: no `clamp` op exists" );
    }
}

// ---------------------------------------------------------------------------

// THE FLOAT BIT PATTERNS AND THE ONE FLOAT RUNG (docs/SPEC-TABLES.md §3.4's `C`
// table: "the IEEE-754 bit pattern, with no canonicalisation"; §4's widened
// ladder). F1/F2 is the pair that already carries a signalling NaN, a payload
// NaN and the f32 -> f64 respelling for form 1; nothing ran it through form 3,
// so `kTableFixedWidenF` — the only float rung the plan has — had no case at
// all and a port could quiet a NaN through a hardware conversion in silence.

static void fl_case()
{
    tblf1::Floats one;
    tblf1::FloatsReset( one );
    build_golden_floats_nan( one );

    std::vector<uint8_t> f( (size_t) tblf1::FloatsFixedMeasure( 1 ) );
    check( tblf1::FloatsFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "F1 save" );

    {
        tblf1::Floats back;
        tblf1::TableReport r;
        std::vector<tblf1::TableFixedEntry> plan( 1024 );
        check( tblf1::FloatsFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 1,
               "FLOATS/identity: one record" );
        check( bits_of_float( back.signalling ) == kFloatSignalling &&
               bits_of_float( back.payload ) == kFloatPayload &&
               bits_of_float( back.negative ) == kFloatNegative,
               "FLOATS/identity: a float32 rides as its BIT PATTERN, quiet bit and payload intact" );
        check( bits_of_double( back.quiet ) == kDoubleQuiet && bits_of_double( back.wide ) == kDoubleWide,
               "FLOATS/identity: a float64's bit pattern, signalling at sixty-four bits" );
        check( bits_of_float( back.samples[0] ) == kFloatSample0 &&
               bits_of_float( back.samples[2] ) == kFloatSample2 && back.after == 7,
               "FLOATS/identity: a NaN as an ARRAY ELEMENT, and the field after it" );
        check( r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "FLOATS/identity: a clean read moves no counter" );
    }
    {
        // THE WIDENED RUNG: three float32 fields respelled float64. The
        // conversion is the one place a hardware cast would quiet a signalling
        // NaN, so the pin is on the DOUBLE's bits and not on `isnan`.
        tblf2::Floats back;
        tblf2::TableReport r;
        std::vector<tblf2::TableFixedEntry> plan( 1024 );
        check( tblf2::FloatsFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 1,
               "FLOATS/compiled: one record" );
        check( r.widened == 3, "WIDENED RUNG: three float32 fields into float64, counted" );
        check( bits_of_double( back.quiet ) == kDoubleQuiet && bits_of_double( back.wide ) == kDoubleWide,
               "WIDENED RUNG: the fields that did not move are not widened" );
        check( bits_of_float( back.samples[0] ) == kFloatSample0 && back.after == 7,
               "WIDENED RUNG: the float32 array beside them is unmoved" );
        // The payload NaN is QUIET already, so its widening is the one a
        // hardware conversion and a bit-surgery widening agree on: 23 payload
        // bits into the top of the double's 52.
        check( bits_of_double( back.payload ) == ( 0x7FF8000000000000ull | ( (uint64_t) ( kFloatPayload & 0x007FFFFFu ) << 29 ) ),
               "WIDENED RUNG: a quiet NaN's payload rides in the top of the double's mantissa" );
    }
}

// ---------------------------------------------------------------------------

// THE TEXT CONTENT RULES (docs/SPEC-TABLES.md §3, kind 12: "WELL-FORMED UTF-8
// with no zero byte among them ... Ill-formed content is `malformed`"; §3.4:
// "A `string(N)`'s UTF-8 validity (§3) is checked over its stated length, not
// over `N`"). The rules are over the USED UNITS and the slack is exempt, which
// is a distinction only a test can hold: a reader that checked the whole bound
// would refuse records that are perfectly readable, and one that checked
// nothing would hand a caller bytes no other port would accept.
//
// The bytes are poked onto a written record, which is what an OTHER PORT's
// writer is: this reader has no way to produce them and has to be held to the
// wire anyway.

static const size_t kP1BodyAt = (size_t) tblp1::kTableFixedHeaderBytes + 4 + (size_t) tblp1::ChainFixedLayoutBytes + 8;

static std::vector<uint8_t> p1_file( const char * name, int32_t used )
{
    tblp1::Chain one;
    FillP1( one, name, used );
    std::vector<uint8_t> f( (size_t) tblp1::ChainFixedMeasure( 1 ) );
    check( tblp1::ChainFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "P1 save for text content" );
    return f;
}

static int64_t p1_read( std::vector<uint8_t> & f, tblp1::Chain & back, tblp1::TableReport & r )
{
    std::vector<tblp1::TableFixedEntry> plan( 1024 );
    return tblp1::ChainFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r );
}

static void text_case()
{
    // A string(16)'s wire is `int32 length` then sixteen bytes, so the length
    // is at the body's start and the bytes follow it.
    const size_t len_at = kP1BodyAt;
    const size_t bytes_at = kP1BodyAt + 4;

    // 1. THE THREE USED EXTENTS, and each is a clean read
    {
        std::vector<uint8_t> f = p1_file( "", 0 );
        tblp1::Chain back; tblp1::TableReport r;
        check( p1_read( f, back, r ) == 1 && back.name_length == 0 && back.name[0] == 0 && r.clamped == 0,
               "TEXT: a used length of ZERO, and the whole bound is slack" );
    }
    {
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        tblp1::Chain back; tblp1::TableReport r;
        check( p1_read( f, back, r ) == 1 && back.name_length == 5 && std::strcmp( back.name, "chain" ) == 0 && r.clamped == 0,
               "TEXT: a used length BELOW the bound" );
    }
    {
        std::vector<uint8_t> f = p1_file( "0123456789abcdef", 16 );
        tblp1::Chain back; tblp1::TableReport r;
        check( p1_read( f, back, r ) == 1 && back.name_length == 16 &&
               std::strcmp( back.name, "0123456789abcdef" ) == 0 && r.clamped == 0,
               "TEXT: a used length EXACTLY at the bound, terminated in the storage's extra unit" );
    }

    // 2. A LENGTH PAST THE FIELD'S OWN BOUND, poked on the wire. §3.4's `text`
    //    op does "`count`'s work on the length", so it is clamped to the
    //    reader's bound and `clamped` counts; the bytes past the bound were
    //    never in the record to begin with.
    {
        std::vector<uint8_t> f = p1_file( "0123456789abcdef", 16 );
        tblp1::TableFixedPut32( f.data() + len_at, 99u );
        tblp1::Chain back; tblp1::TableReport r;
        check( p1_read( f, back, r ) == 1 && back.name_length == 16 && r.clamped == 1,
               "TEXT: a length past the field's bound is CLAMPED to the bound and counted" );
    }
    {
        // AND A NEGATIVE ONE, which is the same lane read as signed
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        tblp1::TableFixedPut32( f.data() + len_at, 0xFFFFFFFFu );
        tblp1::Chain back; tblp1::TableReport r;
        check( p1_read( f, back, r ) == 1 && back.name_length == 0 && r.clamped == 1,
               "TEXT: a NEGATIVE length reads as zero and counts one clamp" );
    }

    // 3. THE SLACK IS EXEMPT. Non-zero bytes PAST the used length are a peer's
    //    business and this reader reads the record correctly: not `malformed`,
    //    not a refusal, no counter (§3.4's slack rule).
    {
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        f[bytes_at + 9] = 0xFFu; // ill-formed UTF-8, and in the SLACK
        tblp1::Chain back; tblp1::TableReport r;
        check( p1_read( f, back, r ) == 1 && back.name_length == 5 && std::strcmp( back.name, "chain" ) == 0,
               "SLACK: a byte past the used length is read through, and the text is still the text" );
        check( !r.malformed && !r.refused && r.clamped == 0,
               "SLACK: non-zero slack is NOT malformed, NOT a refusal, and moves no counter" );
    }

    // 4. AND THE USED UNITS ARE NOT EXEMPT
    {
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        f[bytes_at + 2] = 0xFFu; // a lead byte UTF-8 never uses, INSIDE the length
        tblp1::Chain back; tblp1::TableReport r;
        const int64_t n = p1_read( f, back, r );
        check_red( n < 0 && r.malformed,
                   "text-content/identity/invalid-utf8-is-not-malformed",
                   "ill-formed UTF-8 inside a string(N)'s USED bytes is read through: the text op checks the length and never the content" );
    }
    {
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        f[bytes_at + 2] = 0x00u; // an INTERIOR ZERO BYTE, inside the length
        tblp1::Chain back; tblp1::TableReport r;
        const int64_t n = p1_read( f, back, r );
        check_red( n < 0 && r.malformed,
                   "text-content/identity/invalid-utf8-is-not-malformed",
                   "an interior zero byte inside a string(N)'s USED bytes is read through: §3 refuses it and this op does not" );
    }
}

// ---------------------------------------------------------------------------

// THE FRAME (docs/SPEC-TABLES.md §3.4, "THE FRAMING"). A file is a header, a
// layout behind its length, and then records BACK TO BACK TO THE END OF THE
// FILE with no count — so the count is arithmetic and "BYTES LEFT OVER ARE
// `malformed`". Nothing in the set had read a file of anything but exactly one
// record, which leaves the arithmetic itself untested in both directions.

static void frame_case()
{
    tblfx1::FxRoot rows[3];
    for ( int i = 0; i < 3; ++i )
    {
        tblfx1::FxRootReset( rows[i] );
        rows[i].keep = (uint32_t) ( 100 + i );
        rows[i].narrow = (uint16_t) ( 200 + i );
        rows[i].nested.a = 300 + i;
    }
    std::vector<tblfx1::TableFixedEntry> plan( 1024 );

    // ZERO RECORDS: a file that is a header and a layout and nothing else
    {
        std::vector<uint8_t> f( (size_t) tblfx1::FxRootFixedMeasure( 0 ) );
        check( tblfx1::FxRootFixedSave( rows, 0, f.data(), (int64_t) f.size() ) == (int64_t) f.size(),
               "FRAME: a file of zero records saves" );
        tblfx1::FxRoot back[1];
        tblfx1::TableReport r;
        check( tblfx1::FxRootFixedLoad( back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 0,
               "FRAME: a file of zero records reads as zero records, not as damage" );
        check( !r.malformed && !r.refused, "FRAME: zero records is not damage and not a refusal" );
    }

    // THREE RECORDS: the count is arithmetic over the layout's record size
    std::vector<uint8_t> three( (size_t) tblfx1::FxRootFixedMeasure( 3 ) );
    check( tblfx1::FxRootFixedSave( rows, 3, three.data(), (int64_t) three.size() ) == (int64_t) three.size(),
           "FRAME: a file of three records saves" );
    {
        tblfx1::FxRoot back[3];
        tblfx1::TableReport r;
        check( tblfx1::FxRootFixedLoad( back, 3, three.data(), (int64_t) three.size(), plan.data(), 1024, &r ) == 3,
               "FRAME: three records read as three" );
        check( back[0].keep == 100u && back[1].keep == 101u && back[2].keep == 102u &&
               back[0].nested.a == 300 && back[2].nested.a == 302,
               "FRAME: each record's own values, in order" );
    }
    {
        // THE CALLER'S ARRAY IS SMALLER THAN THE FILE, and this is the one row
        // of the framing §3.4 does not state at all. §3.3 states it for a
        // BATCH — a count above the caller's capacity on the read side is a
        // REFUSAL BY NAME, `batch_too_large`, nothing decoded and no counter
        // moved — and the reference applies that rule to a FILE by precedent.
        // It is pinned here because a rule carried by precedent and by no test
        // is a rule the next port guesses at, and the obvious guess is the
        // other one: read what fits and return the count.
        tblfx1::FxRoot back[2];
        tblfx1::TableReport r;
        const int64_t n = tblfx1::FxRootFixedLoad( back, 2, three.data(), (int64_t) three.size(), plan.data(), 1024, &r );
        check( n < 0 && r.refused && r.reason == tblfx1::batch_too_large,
               "FRAME: a caller's capacity below the file's record count is a REFUSAL BY NAME, not a partial read" );
        check( !r.malformed && r.unknown == 0,
               "FRAME: and that refusal decodes nothing, moves no counter and reports no damage" );
    }
    {
        // BYTES LEFT OVER ARE `malformed` — §3's rule, for §3's reason: the two
        // ends of the file have met and they disagree.
        std::vector<uint8_t> ragged = three;
        ragged.push_back( 0u );
        tblfx1::FxRoot back[3];
        tblfx1::TableReport r;
        const int64_t n = tblfx1::FxRootFixedLoad( back, 3, ragged.data(), (int64_t) ragged.size(), plan.data(), 1024, &r );
        check( n < 0 && r.malformed && !r.refused, "FRAME: one byte left over is `malformed`, not a refusal" );
    }
    {
        // A BUFFER CUT SHORT, at EVERY length from nothing to one byte less
        // than the whole. The count is arithmetic and the arithmetic is stated,
        // so what each cut owes is stated too rather than merely "not a crash":
        //
        //   the records begin at 16 + 4 + the layout, and each is 8 + the body
        //
        // A cut that lands ON a record boundary is a SHORTER FILE and reads
        // exactly the records in front of it; a cut that lands anywhere else
        // has BYTES LEFT OVER and is `malformed` (§3.4). Neither is a refusal:
        // the form byte and the layout were both fine.
        const size_t records_at = (size_t) tblfx1::kTableFixedHeaderBytes + 4 + (size_t) tblfx1::FxRootFixedLayoutBytes;
        const size_t stride = 8 + (size_t) tblfx1::FxRootFixedBodyBytes;
        int64_t boundaries = 0, ragged = 0;
        bool broke = false;
        for ( size_t cut = 0; cut < three.size() && !broke; ++cut )
        {
            tblfx1::FxRoot back[3];
            tblfx1::TableReport r;
            const int64_t n = tblfx1::FxRootFixedLoad( back, 3, three.data(), (int64_t) cut, plan.data(), 1024, &r );
            if ( cut >= records_at && ( cut - records_at ) % stride == 0 )
            {
                const int64_t want = (int64_t) ( ( cut - records_at ) / stride );
                if ( n != want || r.malformed )
                {
                    check( false, "FRAME: a cut ON a record boundary is a shorter file and reads the records in front of it" );
                    broke = true;
                    break;
                }
                for ( int64_t i = 0; i < n; ++i )
                {
                    if ( back[i].keep != (uint32_t) ( 100 + i ) )
                    {
                        check( false, "FRAME: a shorter file's records are the front of the longer one's" );
                        broke = true;
                        break;
                    }
                }
                boundaries++;
                continue;
            }
            if ( n >= 0 ) { check( false, "FRAME: a cut with bytes left over decoded a record" ); broke = true; break; }
            if ( !r.malformed && !r.refused ) { check( false, "FRAME: a cut answered neither damage nor a refusal" ); broke = true; break; }
            ragged++;
        }
        check( !broke && boundaries == 3 && ragged == (int64_t) three.size() - 3,
               "FRAME: every truncation answers — three land on a boundary and read what is in front of them, and the rest are damage" );
    }
}

// ---------------------------------------------------------------------------

// THE SIX SHAPES NOTHING ELSE REACHED (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-COVERAGE.md): a BOOL, an OPTIONAL's present byte, an ENUM
// ORDINAL, an ARRAY OF UNIONS, a THREE-DEEP nesting, and an arm holding an
// array. See FN1.schema for why each one is a row a leg gets a WRONG VALUE out
// of rather than an error.
//
// Every poke below is onto a record this build WROTE, which is what another
// port's writer is: this reader cannot produce these bytes and has to be held
// to them anyway.

// the body of the single record of an FN1 file
static const size_t kFn1BodyAt = (size_t) tblfn1::kTableFixedHeaderBytes + 4 + (size_t) tblfn1::FnRootFixedLayoutBytes + 8;

// the offsets §3.4's `C` table fixes, in declared order and nothing padded
// between them: bool 1, the enum's ordinal 1, the optional's present byte and
// its payload, the count and then MAX elements of tag-plus-widest-arm.
static const size_t kFnFlagAt    = 0;
static const size_t kFnTierAt    = 1;
static const size_t kFnPresentAt = 2;
static const size_t kFnPayloadAt = 3;
static const size_t kFnCountAt   = 7;
static const size_t kFnPick0TagAt = 11;

static std::vector<uint8_t> fn1_file( const tblfn1::FnRoot & v )
{
    std::vector<uint8_t> f( (size_t) tblfn1::FnRootFixedMeasure( 1 ) );
    check( tblfn1::FnRootFixedSave( &v, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "FN1 save" );
    return f;
}

static int64_t fn1_read( const std::vector<uint8_t> & f, tblfn1::FnRoot & back, tblfn1::TableReport & r )
{
    std::vector<tblfn1::TableFixedEntry> plan( 4096 );
    return tblfn1::FnRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &r );
}

static int64_t fn2_read( const std::vector<uint8_t> & f, tblfn2::FnRoot & back, tblfn2::TableReport & r )
{
    std::vector<tblfn2::TableFixedEntry> plan( 4096 );
    return tblfn2::FnRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, &r );
}

static void fn_case()
{
    tblfn1::FnRoot one;
    FillFn1( one );
    const std::vector<uint8_t> clean = fn1_file( one );

    // ---- 1. THE IDENTITY PLAN over all six shapes at once -----------------
    {
        tblfn1::FnRoot back;
        tblfn1::TableReport r;
        check( fn1_read( clean, back, r ) == 1, "FN/identity: one record" );
        check( back.flag, "FN/identity: a BOOL, which nothing in the set had in a fixed root" );
        check( back.tier == tblfn1::Tier::Gold, "FN/identity: an ENUM at its own ordinal" );
        check( back.opt_present && back.opt.z == 111, "FN/identity: an OPTIONAL, present, flag and payload" );
        check( back.picks_count == 2, "FN/identity: an ARRAY OF UNIONS, its count, at MAX" );
        check( back.picks[0].type == tblfn1::PickType::A && back.picks[0].a.n == 201,
               "FN/identity: element 0 is the NARROW arm, riding in front of declared slack" );
        check( back.picks[1].type == tblfn1::PickType::B && back.picks[1].b.pts[0] == 301 &&
               back.picks[1].b.pts[1] == 302 && back.picks[1].b.m == 303,
               "FN/identity: element 1 is the WIDEST arm, and it is the one holding an ARRAY" );
        check( back.deep.x == 41 && back.deep.mid.y == 42 && back.deep.mid.deeper.z == 43,
               "FN/identity: THREE-DEEP nesting, every level's own field" );
        check( back.label_length == 4 && std::strcmp( back.label, "abcd" ) == 0,
               "FN/identity: a string(4) at USED == MAX" );
        check( back.after == 9, "FN/identity: the field behind all of it still lands" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 &&
               !r.malformed && !r.refused, "FN/identity: a clean read moves no counter" );
    }

    // ---- 2. THE COMPILED PLAN, the newer build reading the older ----------
    {
        tblfn2::FnRoot back;
        tblfn2::TableReport r;
        check( fn2_read( clean, back, r ) == 1, "FN/compiled/older writer: one record" );
        check( back.flag, "FN/compiled: the bool" );
        // Gold is ordinal 2 in FN1 and ordinal 3 in FN2, because FN2 inserted
        // Silver in the middle. Under a positional read every stored Gold would
        // come back Silver; it rides as the hash of "Gold", so it does not.
        check( back.tier == tblfn2::Tier::Gold,
               "FN/compiled: an ENUM VARIANT resolved BY NAME, so ordinal 2 lands on ordinal 3" );
        check( back.opt_present && back.opt.z == 111 && back.opt.added == 33,
               "FN/compiled: an OPTIONAL whose PAYLOAD TYPE GREW: the old field lands and the new one defaults" );
        check( back.picks_count == 2, "FN/compiled: the array of unions' count" );
        check( back.picks[0].type == tblfn2::PickType::A && back.picks[0].a.n == 201,
               "FN/compiled: an arm at an UNMOVED ordinal, per element" );
        check( back.picks[1].type == tblfn2::PickType::B && back.picks[1].b.pts[1] == 302 && back.picks[1].b.m == 303,
               "FN/compiled: the WIDEST arm slid from ordinal 2 to 3 and its array came with it" );
        check( back.deep.x == 41 && back.deep.mid.y == 42 && back.deep.mid.deeper.z == 43 &&
               back.deep.mid.deeper.added == 33,
               "FN/compiled: THREE-DEEP, with the INNERMOST type resized — every offset behind it moved" );
        check( back.caption_length == 4 && std::strcmp( back.caption, "abcd" ) == 0,
               "FN/compiled: a renamed text field arrives under the id its `was` names" );
        check( back.after == 9 && back.tail == 3,
               "FN/compiled: the appended field takes its declared default" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "FN/compiled/older writer: no unknown, no damage, no refusal" );
    }

    // ---- 3. THE COMPILED PLAN, the older build reading the newer ----------
    {
        tblfn2::FnRoot two;
        FillFn2( two );
        std::vector<uint8_t> f( (size_t) tblfn2::FnRootFixedMeasure( 1 ) );
        check( tblfn2::FnRootFixedSave( &two, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "FN2 save" );

        tblfn1::FnRoot back;
        tblfn1::TableReport r;
        check( fn1_read( f, back, r ) == 1, "FN/compiled/newer writer: one record" );
        check( back.tier == tblfn1::Tier::None,
               "AN ENUM VARIANT WITH NO NAME HERE: `Silver` reads None, never a neighbouring variant" );
        check( back.picks[0].type == tblfn1::PickType::None,
               "AN ARM WITH NO NAME HERE, INSIDE AN ARRAY: element 0 reads None, never another arm's bytes" );
        check( back.picks[1].type == tblfn1::PickType::B && back.picks[1].b.pts[0] == 8 && back.picks[1].b.m == 10,
               "AND THE ELEMENT BESIDE IT STILL LANDS: an unknown arm in one slot is not an unknown array" );
        check( back.picks_count == 2, "FN/compiled/newer writer: the count is the writer's" );
        check( back.deep.mid.deeper.z == 3,
               "THREE-DEEP: the innermost type's own field lands though the type grew a field behind it" );
        check( back.label_length == 4 && std::strcmp( back.label, "wxyz" ) == 0,
               "FN/compiled/newer writer: the rename resolves in the other direction too" );
        check( back.after == 6, "FN/compiled/newer writer: the field behind the growth" );
        check( r.unknown == 3,
               "THREE NAMES THIS READER HAS NOT GOT: the variant `Silver`, the arm `c`, and the field `tail`" );
        check( !r.malformed && !r.refused, "FN/compiled/newer writer: no damage and no refusal" );
    }

    // ---- 4. A BOOL BYTE THAT IS NEITHER 0 NOR 1 --------------------------
    //
    // §3.4 gives `bool` a `C` of 1 and §3 gives it two values, and NOTHING
    // anywhere says what a reader owes for the other 254. On this form that is
    // not an academic hole: the read is a COPY, so the byte lands in the
    // caller's storage as it stood on the wire, and in C++ that storage is a
    // `bool`. A `bool` holding 2 is a value its own type does not admit, and
    // EVERY LATER READ OF IT IS UNDEFINED — the sanitized twin of this binary
    // says so by name, which is how this row was found.
    //
    // SO THE TEST READS THE BYTE AND NOT THE BOOL. `memcpy` out of the storage
    // is defined whatever the storage holds; `a.flag` is not, and a test that
    // tripped the sanitizer would be a test nobody could run under one.
    {
        for ( uint8_t poke : { (uint8_t) 2u, (uint8_t) 0xFFu } )
        {
            std::vector<uint8_t> f = clean;
            f[kFn1BodyAt + kFnFlagAt] = poke;
            tblfn1::FnRoot a; tblfn1::TableReport ra;
            tblfn2::FnRoot b; tblfn2::TableReport rb;
            check( fn1_read( f, a, ra ) == 1 && fn2_read( f, b, rb ) == 1, "BOOL BYTE: both paths read the record" );
            uint8_t ident = 0, compiled = 0;
            std::memcpy( &ident, &a.flag, 1 );
            std::memcpy( &compiled, &b.flag, 1 );
            check( ident == compiled,
                   "BOOL BYTE: a byte outside {0, 1} answers the SAME on the identity plan and on a compiled one" );
            check( !ra.malformed && !ra.refused && !rb.malformed && !rb.refused,
                   "BOOL BYTE: a byte outside {0, 1} is not damage and not a refusal" );
            check_red( ident <= 1u,
                       "bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool",
                       "a bool byte that is neither 0 nor 1 is copied through: the caller's `bool` then holds a value its type does not admit and every read of it is undefined" );
            std::printf( "  NOTE: a bool byte of %3u lands in the caller's bool storage as %u\n",
                         (unsigned) poke, (unsigned) ident );
        }
    }

    // ---- 5. AN ENUM ORDINAL PAST THE LAST VARIANT ------------------------
    //
    // §3.4: "the ORDINAL is the variant's POSITION IN THE LAYOUT, from 1, and
    // 0 is None". `Tier` has two variants, so `9` names no position. The two
    // paths have already answered differently and that is the assertion.
    {
        std::vector<uint8_t> f = clean;
        f[kFn1BodyAt + kFnTierAt] = 9u;
        tblfn1::FnRoot a; tblfn1::TableReport ra;
        tblfn2::FnRoot b; tblfn2::TableReport rb;
        check( fn1_read( f, a, ra ) == 1 && fn2_read( f, b, rb ) == 1, "ORDINAL PAST THE VARIANTS: both paths read the record" );
        check( b.tier == tblfn2::Tier::None,
               "ORDINAL PAST THE VARIANTS/compiled: it resolves to None, which is §4's answer for a name this reader has not got" );
        check( !ra.malformed && !ra.refused, "ORDINAL PAST THE VARIANTS: never damage and never a refusal" );
        check( a.flag && a.after == 9, "ORDINAL PAST THE VARIANTS: the fields either side of it still land" );
        check_red( (int) a.tier == (int) b.tier,
                   "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant",
                   "an ordinal of 9 over a two-variant enum: the compiled plan lands None and the identity plan copies the 9 into the enum slot" );
    }

    // ---- 6. A PRESENT BYTE THAT IS NEITHER 0 NOR 1 -----------------------
    //
    // §4 settles the MEANING where §3.4's bool row does not: an optional "that
    // rode reads as PRESENT, WHATEVER THE CONTENT". A present byte of 7 is a
    // byte that rode, so it is present.
    //
    // WHAT IS NOT SETTLED IS THE VALUE THE CALLER IS HANDED. The present flag
    // is kind 35's one byte and the read is a copy, so the caller's storage —
    // a C++ `bool` — ends up holding 7, exactly as the plain bool above does.
    // It is the same row, met at the other of the two places this form spends
    // a byte on a two-valued thing, and both are listed under one key because
    // one ruling closes both.
    {
        std::vector<uint8_t> f = clean;
        f[kFn1BodyAt + kFnPresentAt] = 7u;
        tblfn1::FnRoot a; tblfn1::TableReport ra;
        tblfn2::FnRoot b; tblfn2::TableReport rb;
        check( fn1_read( f, a, ra ) == 1 && fn2_read( f, b, rb ) == 1, "PRESENT BYTE 7: both paths read the record" );
        uint8_t ident = 0, compiled = 0;
        std::memcpy( &ident, &a.opt_present, 1 );
        std::memcpy( &compiled, &b.opt_present, 1 );
        check( ident != 0u && compiled != 0u,
               "PRESENT BYTE 7: a present byte that rode reads PRESENT, whatever its content (§4)" );
        check( ident == compiled, "PRESENT BYTE 7: and the two reader paths agree about it" );
        check( a.opt.z == 111, "PRESENT BYTE 7: the payload behind it is the payload" );
        check( !ra.malformed && !ra.refused, "PRESENT BYTE 7: not damage and not a refusal" );
        check_red( ident <= 1u,
                   "bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool",
                   "the OPTIONAL's present byte takes the same copy as a plain bool: a present byte of 7 lands in the caller's `bool` storage as 7" );
    }

    // ---- 7. AN ABSENT OPTIONAL OVER NON-ZERO RESIDUE ---------------------
    //
    // §3.4's `?T` row: "the payload rides WHOLE whether or not it is present",
    // "ZERO on write when the flag is 0, IGNORED ON READ". A peer that leaves
    // garbage under a cleared flag is a peer this reader must read correctly,
    // and reading it correctly means the payload keeps the reader's own
    // PREFILL — the declared default — and never the residue.
    {
        tblfn1::FnRoot absent;
        FillFn1Absent( absent );
        std::vector<uint8_t> f = fn1_file( absent );
        check( f[kFn1BodyAt + kFnPresentAt] == 0u, "ABSENT OPTIONAL: the writer cleared the present byte" );
        check( f[kFn1BodyAt + kFnPayloadAt] == 0u,
               "ABSENT OPTIONAL: and ZERO-FILLED the payload behind it, which is §3.4's write rule" );
        for ( size_t i = 0; i < 4; ++i ) { f[kFn1BodyAt + kFnPayloadAt + i] = 0x7Fu; }

        tblfn1::FnRoot a; tblfn1::TableReport ra;
        check( fn1_read( f, a, ra ) == 1, "ABSENT OPTIONAL: the record still reads" );
        check( !a.opt_present, "ABSENT OPTIONAL: the flag is the flag, and it reads absent" );
        check( !ra.malformed && !ra.refused && ra.clamped == 0,
               "ABSENT OPTIONAL: non-zero residue is NOT damage, NOT a refusal and moves no counter" );
        check( a.after == 9, "ABSENT OPTIONAL: the fields behind it are unmoved" );
        // 30 is Deep3's declared default for `z`, which is what the prefill put
        // there and what "IGNORED on read" means the loop must leave alone.
        check_red( a.opt.z == 30,
                   "optional/absent-payload-residue-is-copied",
                   "an ABSENT optional over non-zero residue: the payload is copied anyway, so a caller reading it without the flag gets 0x7F7F7F7F where the declared default should be" );

        tblfn2::FnRoot b; tblfn2::TableReport rb;
        check( fn2_read( f, b, rb ) == 1 && !b.opt_present,
               "ABSENT OPTIONAL/compiled: the flag reads absent through a compiled plan too" );
        check_red( b.opt.z == 30,
                   "optional/absent-payload-residue-is-copied",
                   "the same residue through a COMPILED plan: the payload is not gated on the flag on either path" );
    }

    // ---- 8. A COUNT PAST THE READER'S OWN Max ----------------------------
    //
    // §3.4's `count` op: "read the count, clamp it to the reader's own `Max`,
    // store it; `clamped` counts if it fired". The elements past `Max` were
    // never in the record, so this is a clamp and never a refusal.
    {
        std::vector<uint8_t> f = clean;
        tblfn1::TableFixedPut32( f.data() + kFn1BodyAt + kFnCountAt, 99u );
        tblfn1::FnRoot a; tblfn1::TableReport ra;
        check( fn1_read( f, a, ra ) == 1, "COUNT PAST Max: the record reads" );
        check( a.picks_count == 2 && ra.clamped == 1,
               "COUNT PAST Max/identity: clamped to the reader's own bound, and counted once" );
        check( !ra.malformed && !ra.refused, "COUNT PAST Max: a clamp is not damage and not a refusal" );

        tblfn2::FnRoot b; tblfn2::TableReport rb;
        check( fn2_read( f, b, rb ) == 1 && b.picks_count == 2 && rb.clamped == 1,
               "COUNT PAST Max/compiled: the same clamp and the same count through a compiled plan" );
    }
    {
        // AND A NEGATIVE COUNT, which is the same four bytes read as signed
        std::vector<uint8_t> f = clean;
        tblfn1::TableFixedPut32( f.data() + kFn1BodyAt + kFnCountAt, 0xFFFFFFFFu );
        tblfn1::FnRoot a; tblfn1::TableReport ra;
        check( fn1_read( f, a, ra ) == 1 && a.picks_count == 0 && ra.clamped == 1,
               "COUNT NEGATIVE: reads as zero elements and counts one clamp" );
    }

    // ---- 9. A TAG PAST THE ARMS, PER ELEMENT OF THE ARRAY ----------------
    //
    // The tag test is per ELEMENT, not per field: element 0's tag is poked and
    // element 1 must be untouched. The two paths' disagreement is the same one
    // FU1 names, and it is listed under the same key.
    {
        std::vector<uint8_t> f = clean;
        f[kFn1BodyAt + kFnPick0TagAt] = 9u;
        tblfn1::FnRoot a; tblfn1::TableReport ra;
        tblfn2::FnRoot b; tblfn2::TableReport rb;
        check( fn1_read( f, a, ra ) == 1 && fn2_read( f, b, rb ) == 1, "ELEMENT TAG PAST THE ARMS: both paths read the record" );
        check( b.picks[0].type == tblfn2::PickType::None,
               "ELEMENT TAG PAST THE ARMS/compiled: element 0 resolves to None" );
        check( a.picks[1].type == tblfn1::PickType::B && a.picks[1].b.m == 303 &&
               b.picks[1].type == tblfn2::PickType::B && b.picks[1].b.m == 303,
               "ELEMENT TAG PAST THE ARMS: the OTHER element is untouched, on both paths" );
        check_red( (int) a.picks[0].type == (int) b.picks[0].type,
                   "ordinal-bound/paths-disagree/union-tag-past-the-last-arm",
                   "a bad tag on ONE ELEMENT of an array of unions: the two plans answer it differently, exactly as they do on a plain union field" );
    }
}

// ---------------------------------------------------------------------------

// A TOP-LEVEL wstring(N) (docs/SPEC-TABLES.md §3.4's `wstring(N)` row).
//
// FLAVOUR 2 HAD NO ORACLE BYTES ANYWHERE IN THIS PROJECT. FU1's `wide` arm is
// the flavour under a union, where the plan entry's `arg` lane is already
// broken; this is the flavour AT THE ROOT, where nothing else is wrong and the
// one thing that can be is the arithmetic: the LENGTH is in CODE UNITS and the
// payload is `2N` BYTES, so a leg that treats the length as a byte count writes
// a record half the size, and no other fixture in the set says so.

static void wstring_case()
{
    wide::Stamp one;
    FillWideStamp( one );

    std::vector<uint8_t> f( (size_t) wide::StampFixedMeasure( 1 ) );
    check( wide::StampFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "wide Stamp save" );

    // THE ARITHMETIC ITSELF, off the constant and not off the bytes: a
    // `wstring(4)` is four bytes of length and EIGHT of payload, and `seq`
    // behind it. A leg that spent four is a leg whose every later field moved.
    check( wide::StampFixedBodyBytes == 4 + 2 * 4 + 4,
           "WSTRING: C is 4 for the length in CODE UNITS plus 2N bytes of payload" );

    const size_t body = (size_t) wide::kTableFixedHeaderBytes + 4 + (size_t) wide::StampFixedLayoutBytes + 8;
    check( wide::TableFixedGet32( f.data() + body ) == 4u,
           "WSTRING: the length on the wire is the USED CODE UNITS, not the used bytes" );
    // U+00E9 is the unit whose two bytes differ, so a leg that wrote one byte
    // per unit or swapped the pair is visible in this one comparison.
    check( f[body + 4 + 2] == 0xE9u && f[body + 4 + 3] == 0x00u,
           "WSTRING: each code unit is TWO BYTES, LITTLE-ENDIAN" );

    wide::Stamp back;
    wide::TableReport r;
    std::vector<wide::TableFixedEntry> plan( 1024 );
    check( wide::StampFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 1,
           "WSTRING: one record" );
    check( back.label_length == 4, "WSTRING: the used length comes back in CODE UNITS" );
    check( back.label[0] == (char16_t) 0x0041 && back.label[1] == (char16_t) 0x00E9 &&
           back.label[2] == (char16_t) 0x4E2D,
           "WSTRING: every code unit comes back, high bytes included" );
    // A LONE SURROGATE IS A CODE UNIT AND NOT A CODE POINT. §3.4 checks the
    // content rules over the stated length in CODE UNITS, and a UTF-16 code
    // unit sequence's validity is not a UTF-8 question: an unpaired surrogate
    // is what a text field carries when a peer split a pair at a bound, and
    // this reader must hand it back rather than invent a replacement.
    check( back.label[3] == (char16_t) 0xD83D,
           "WSTRING: a LONE SURROGATE is a code unit and rides as one" );
    check( back.seq == 90210u, "WSTRING: the field behind the payload lands at 2N and not at N" );
    check( r.clamped == 0 && !r.malformed && !r.refused, "WSTRING: a clean read moves no counter" );

    // A LENGTH PAST THE FIELD'S OWN BOUND, in CODE UNITS: `count`'s work on the
    // length (§3.4's `text` op), so it clamps to the bound and counts.
    {
        std::vector<uint8_t> g = f;
        wide::TableFixedPut32( g.data() + body, 99u );
        wide::Stamp b2;
        wide::TableReport r2;
        check( wide::StampFixedLoad( &b2, 1, g.data(), (int64_t) g.size(), plan.data(), 1024, &r2 ) == 1 &&
               b2.label_length == 4 && r2.clamped == 1,
               "WSTRING: a length past the bound is CLAMPED to the bound, in code units, and counted" );
    }
}

// ---------------------------------------------------------------------------

// `flags`, THE DECLARED DEFAULTS OF THE TEXT FAMILY, AND A TABLE RENAMED
// (docs/SPEC-TABLES.md §3.4's `flags` row, §4, §5).
//
// W1/W2 is the pair that already carries these edits on form 1; this runs it
// through form 3. What is new to this form is the `flags` row — "8, the raw
// mask" — which nothing in the set carried, and the fact that the DEFAULTS of
// a `string(N)`, a `bytes(N)` and a `flags` are what a reader's PREFILL puts
// there, which is the whole of how this form answers an absent field.

static void w_case()
{
    tblw1::Vessel one;
    FillW1( one );
    std::vector<uint8_t> f( (size_t) tblw1::VesselFixedMeasure( 1 ) );
    check( tblw1::VesselFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "W1 save" );

    {
        tblw1::Vessel back;
        tblw1::TableReport r;
        std::vector<tblw1::TableFixedEntry> plan( 1024 );
        check( tblw1::VesselFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 1,
               "FLAGS/identity: one record" );
        check( back.caps == one.caps, "FLAGS: the raw mask rides and comes back, off its declared default" );
        check( back.name_length == 9 && std::strcmp( back.name, "endeavour" ) == 0,
               "FLAGS/identity: the string beside it" );
        // `bytes(N)` IS NOT TEXT. §3's content rules are the text kinds', so a
        // zero byte inside a `bytes(N)`'s USED extent is a value and not damage
        // — which is the one place the three text flavours are not one rule.
        check( back.tag_length == 4 && back.tag[1] == 0x80u && back.tag[2] == 0x00u && back.tag[3] == 0xFFu,
               "BYTES: a ZERO BYTE and a 0x80 inside the used extent are values, not damage" );
        check( !r.malformed && !r.refused && r.clamped == 0, "FLAGS/identity: a clean read moves no counter" );
    }
    {
        // A TABLE RENAMED UNDER `was`. Every record rides under the hash of the
        // OLD name, so a W1 record reads into W2's `Ship` in silence — a rename
        // at a level no field-level pair in this set reaches.
        tblw2::Ship back;
        tblw2::TableReport r;
        std::vector<tblw2::TableFixedEntry> plan( 1024 );
        check( tblw2::ShipFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 1,
               "TABLE RENAMED: a W1 Vessel reads into a W2 Ship" );
        check( back.caps == (tblw2::Caps) tblw2::Caps_Crouch && back.hull == 250,
               "TABLE RENAMED: every field lands under the old name's hash" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "TABLE RENAMED: `was` on a TABLE is not an evolution event at all" );
    }
    {
        // AND THE DEFAULTS THEMSELVES: a reader's prefill is what an absent
        // field lands on, and for these three kinds the declared default is not
        // zero. Nothing on the wire is consulted, which is exactly the claim.
        tblw1::Vessel fresh;
        tblw1::VesselReset( fresh );
        check( std::strcmp( fresh.name, "untitled" ) == 0 && fresh.name_length == 8,
               "DEFAULTS: a string(N)'s declared default is what the prefill puts there" );
        check( fresh.tag_length == 2 && fresh.tag[0] == 0x61u && fresh.tag[1] == 0x62u,
               "DEFAULTS: a bytes(N)'s declared default, and its used length with it" );
        check( fresh.caps == (tblw1::Caps) ( tblw1::Caps_Jump | tblw1::Caps_Fly ),
               "DEFAULTS: a flags field's declared default is a MASK and not zero" );
        check( std::strcmp( fresh.badge.label, "new" ) == 0,
               "DEFAULTS: and a nested type's own text default comes with it" );
    }
}

// ---------------------------------------------------------------------------

// THE `bits(N)` FAMILY (docs/SPEC-TABLES.md §3.4: "the declared storage width,
// 4 for N <= 32 and 8 above"), and it is the row where this form SPENDS BYTES
// ON PURPOSE: "a `bits(12)` costs four bytes here where §3 spends two". The
// width is the DECLARATION's in all nine ports, so a leg that packed a bits(12)
// into two bytes has moved every field behind it — and would still round-trip
// against itself, which is why this is asserted against the CONSTANT and pinned
// in the oracle rather than merely read back.

static void bits_case()
{
    // 4 + 4 + 4 + 8 + 4 + 8: the two that do not fill their storage cost the
    // same as the two that do.
    check( tabledemo::RangedWidthsFixedBodyBytes == 4 + 4 + 4 + 8 + 4 + 8,
           "BITS: C is the DECLARED storage width — four bytes for N <= 32 and eight above, whatever N is" );

    tabledemo::RangedWidths one;
    FillBits( one );
    std::vector<uint8_t> f( (size_t) tabledemo::RangedWidthsFixedMeasure( 1 ) );
    check( tabledemo::RangedWidthsFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "bits save" );

    tabledemo::RangedWidths back;
    tabledemo::TableReport r;
    std::vector<tabledemo::TableFixedEntry> plan( 1024 );
    check( tabledemo::RangedWidthsFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, &r ) == 1,
           "BITS: one record" );
    check( back.b8 == 0xFFu && back.b16 == 0xFFFFu && back.b32 == 0xFFFFFFFFu,
           "BITS: the widths that fill a uint32 exactly" );
    check( back.b64 == 0xFFFFFFFFFFFFFFFFull, "BITS: the width that fills a uint64 exactly" );
    check( back.b12 == 0x0FFFu && back.b48 == 0x0000FFFFFFFFFFFFull,
           "BITS: the two widths that do NOT fill their storage ride in the whole of it anyway" );
    check( r.clamped == 0 && !r.malformed && !r.refused, "BITS: a value at the width's own bound is not a clamp" );
}

// ---------------------------------------------------------------------------

static void negative_control()
{
    // THE WRONG PLAN. FX1's identity plan is correct for an FX1 record and
    // wrong for an FX2 one — FX2 inserts `added` between `renamed_to` and
    // `nested`, so every offset past it has moved. Running FX1's plan over
    // FX2's body is exactly the mistake the hash exists to prevent, and it
    // must come out WRONG. If this ever comes out right, the hash is not
    // carrying anything and the test above proves nothing.
    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 5150u;
    two.narrow = 70000u;
    two.renamed_to = 808;
    two.added = 909;
    two.nested.a = 33;
    two.nested.b = 44;
    std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() );
    const uint8_t * body = w2.data() + tblfx2::kTableFixedHeaderBytes + 4 + tblfx2::FxRootFixedLayoutBytes + 8;

    tblfx1::FxRoot wrong;
    tblfx1::FxRootReset( wrong );
    tblfx1::TableReport r;
    tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded, body, (uint8_t *) &wrong, &r );
    const bool intact = wrong.nested.a == 33 && wrong.nested.b == 44 && wrong.renamed == 808;
    check( !intact, "NEGATIVE CONTROL: the wrong plan must NOT reproduce the record" );

    // and the loader never takes that path: the hash is what selects the plan
    tblfx1::FxRoot right;
    tblfx1::TableReport r2;
    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    const int64_t n = tblfx1::FxRootFixedLoad( &right, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, &r2 );
    check( n == 1 && right.nested.a == 33 && right.nested.b == 44,
           "NEGATIVE CONTROL: the loader compiles a plan from the layout and gets it right" );

    // A LAYOUT THAT IS NOT A LAYOUT IS REFUSED BY NAME, whole, and never damage.
    // Every rule has its own case in layout_validation() below; this one is
    // here because it is also the check that a refusal MOVES NO COUNTER.
    {
        std::vector<uint8_t> broken = w2;
        broken[tblfx1::kTableFixedHeaderBytes] ^= 0xFFu; // the entry count
        tblfx1::FxRoot v;
        tblfx1::TableReport r3;
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, broken.data(), (int64_t) broken.size(), plan.data(), 1024, &r3 );
        check( bad < 0 && r3.refused && r3.reason == tblfx1::layout_count_mismatch, "REFUSED BY NAME: layout_count_mismatch" );
        check( r3.unknown == 0 && r3.kind_mismatch == 0 && !r3.malformed, "REFUSED BY NAME: a refusal moves no counter" );
    }

    // A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL AND NEVER DAMAGE, AND
    // THE NAME SAYS WHICH DIRECTION (docs/SPEC-TABLES.md §3, §3.4). The registry
    // is ordered, so a fixed reader handed form `1` has been handed the
    // VARIABLE form, which is OLDER: calling that `newer_form` would send a
    // caller looking for a build that does not exist. Form `6` is the byte no
    // form defines or reserves, which is the only kind of byte `newer_form` is
    // the honest answer for.
    {
        struct { uint8_t form; tblfx1::TableMessageReason want; const char * what; } rows[3] = {
            { 1, tblfx1::previous_form,      "REFUSED BY NAME: previous_form for the VARIABLE form" },
            { 2, tblfx1::message_form_as_file, "REFUSED BY NAME: message_form_as_file for a batch" },
            { 6, tblfx1::newer_form,         "REFUSED BY NAME: newer_form for a byte no form defines" },
        };
        for ( int i = 0; i < 3; ++i )
        {
            std::vector<uint8_t> other = w2;
            other[0] = rows[i].form;
            tblfx1::FxRoot v;
            tblfx1::TableReport r4;
            const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, other.data(), (int64_t) other.size(), plan.data(), 1024, &r4 );
            check( bad < 0 && r4.refused && r4.reason == rows[i].want, rows[i].what );
            check( !r4.malformed, "REFUSED BY NAME: never damage" );
            check( r4.unknown == 0 && r4.kind_mismatch == 0 && r4.widened == 0 && r4.clamped == 0,
                   "REFUSED BY NAME: a form-byte refusal moves no counter" );
        }
    }

    // THE HEADER NAMES THE LAYOUT ONCE (docs/SPEC-TABLES.md §3): the eight bytes
    // at offset 8 are the LAYOUT's hash, and a header that claims a layout it
    // does not carry is refused. This is the case that proves the header's hash
    // is READ and not merely written.
    {
        std::vector<uint8_t> lying = w2;
        lying[tblfx1::kTableFixedHashAt] ^= 0xFFu;
        tblfx1::FxRoot v;
        tblfx1::TableReport r6;
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, lying.data(), (int64_t) lying.size(), plan.data(), 1024, &r6 );
        check( bad < 0 && r6.refused && r6.reason == tblfx1::layout_malformed,
               "REFUSED BY NAME: a header hash that is not the layout's" );
        check( !r6.malformed, "REFUSED BY NAME: never damage" );
    }

    // A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS A REFUSAL BY NAME, and
    // the codec allocates nothing to get around it.
    {
        tblfx1::FxRoot v;
        tblfx1::TableReport r5;
        tblfx1::TableFixedEntry tiny[1];
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, w2.data(), (int64_t) w2.size(), tiny, 1, &r5 );
        check( bad < 0 && r5.refused && r5.reason == tblfx1::plan_too_large, "REFUSED BY NAME: plan_too_large" );
    }
}

// ---------------------------------------------------------------------------

// THE LAYOUT ARRIVES FROM AN UNTRUSTED PEER (docs/SPEC-TABLES.md §3.4). It is
// the one structure a reader must parse before it knows anything at all, so
// every rule it is held to refuses under ITS OWN NAME, before a single record
// byte is touched. A VALIDATION NOBODY WATCHED FAIL IS A VALIDATION NOBODY
// HAS, so there is one case per named rule, each taking a layout this reader
// accepts and breaking EXACTLY ONE THING in it.
//
// The file is the HEADER (docs/SPEC-TABLES.md §3: the form byte, seven reserved
// zero bytes, the layout hash at 8, the body at 16), then `u32 layout length,
// layout, records` — so the layout starts at byte 20, the entry count is the
// four bytes there, and entry k is the seventeen bytes at 20 + 4 + 17k: id
// (u64), kind (u8), size (u32), children (u32), every number little-endian.

static const size_t kLayoutAt = (size_t) tblfx1::kTableFixedHeaderBytes + 4;
static const size_t kEntry0 = kLayoutAt + 4;

static uint8_t * entry_at( std::vector<uint8_t> & f, size_t k ) { return f.data() + kEntry0 + k * 17; }

// an entry appended to a hand-built layout, for the two rules no single break
// of a real layout reaches
static void put_entry( std::vector<uint8_t> & layout, uint64_t id, uint8_t kind, uint32_t size, uint32_t children )
{
    const size_t at = layout.size();
    layout.resize( at + 17 );
    tblfx1::TableFixedPut64( layout.data() + at, id );
    layout[at + 8] = kind;
    tblfx1::TableFixedPut32( layout.data() + at + 9, size );
    tblfx1::TableFixedPut32( layout.data() + at + 13, children );
}

// a FILE around a hand-built layout: the form byte, the layout's length, the
// layout, and no records — every rule below refuses before a record is reached
static std::vector<uint8_t> file_of( const std::vector<uint8_t> & layout )
{
    std::vector<uint8_t> f( (size_t) tblfx1::kTableFixedHeaderBytes + 4, 0 );
    f[0] = 3; // the fixed form's byte
    tblfx1::TableFixedPut64( f.data() + tblfx1::kTableFixedHashAt,
                             tblfx1::TableFixedHashOf( layout.data(), (uint32_t) layout.size() ) );
    tblfx1::TableFixedPut32( f.data() + tblfx1::kTableFixedHeaderBytes, (uint32_t) layout.size() );
    f.insert( f.end(), layout.begin(), layout.end() );
    return f;
}

static void refuses( std::vector<uint8_t> & broken, tblfx1::TableMessageReason want, const char * what )
{
    tblfx1::FxRoot v;
    tblfx1::FxRootReset( v );
    tblfx1::TableReport r;
    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    const int64_t n = tblfx1::FxRootFixedLoad( &v, 1, broken.data(), (int64_t) broken.size(), plan.data(), 1024, &r );
    check( n < 0 && r.refused && r.reason == want, what );
    // NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read a
    // record would be the damage the refusal exists to prevent.
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed,
           "a layout refusal sets nothing and counts nothing" );
}

static void layout_validation()
{
    // the layout this reader ACCEPTS, which every case below breaks once
    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    std::vector<uint8_t> good( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    check( tblfx2::FxRootFixedSave( &two, 1, good.data(), (int64_t) good.size() ) == (int64_t) good.size(),
           "layout validation: the unbroken file saves" );
    {
        // and it READS, so every refusal below is the ONE break and not the file
        tblfx1::FxRoot v;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &v, 1, good.data(), (int64_t) good.size(), plan.data(), 1024, &r ) == 1 && !r.refused,
               "layout validation: the unbroken file reads, so the breaks below are the breaks" );
    }

    // 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY
    {
        std::vector<uint8_t> f = good;
        const uint32_t count = tblfx1::TableFixedGet32( f.data() + kLayoutAt );
        tblfx1::TableFixedPut32( f.data() + kLayoutAt, count + 1u );
        refuses( f, tblfx1::layout_count_mismatch, "RULE: the entry count fits the layout length exactly" );
    }
    {
        // a count of ZERO is not a layout either: there is no root to walk
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( f.data() + kLayoutAt, 0u );
        refuses( f, tblfx1::layout_count_mismatch, "RULE: an entry count of zero is not a layout" );
    }

    // 2. EVERY KIND IS IN THE CLOSED SET. A fixed form's kind set is CLOSED, so
    //    a kind outside it means a NEWER FORM BYTE — a different form — and not
    //    a newer layout of this one. It is refused, never stepped over.
    {
        std::vector<uint8_t> f = good;
        entry_at( f, 1 )[8] = 200; // a kind no form byte this build carries defines
        refuses( f, tblfx1::layout_kind_unknown, "RULE: a kind outside the closed set is REFUSED, not skipped" );
    }

    // 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table
    {
        std::vector<uint8_t> f = good;
        entry_at( f, 0 )[8] = 14; // an array as the root of a record
        refuses( f, tblfx1::layout_kind_invalid, "RULE: the root entry is a TABLE" );
    }

    // 4. A CONSTANT SIZE MATCHES ITS KIND
    {
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 1 ) + 9, 5u ); // a uint32 leaf in five bytes
        refuses( f, tblfx1::layout_size_mismatch, "RULE: a constant size its kind does not admit" );
    }
    {
        // and a TABLE's size is the SUM of its children's, not a number of its own
        std::vector<uint8_t> f = good;
        const uint32_t body = tblfx1::TableFixedGet32( entry_at( f, 0 ) + 9 );
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 9, body + 4u );
        refuses( f, tblfx1::layout_size_mismatch, "RULE: a table's size is the sum of its fields'" );
    }

    // 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES
    {
        std::vector<uint8_t> f = good;
        const uint32_t kids = tblfx1::TableFixedGet32( entry_at( f, 0 ) + 13 );
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 13, kids + 1u );
        refuses( f, tblfx1::layout_tree_unclosed, "RULE: the tree runs out of layout" );
    }
    {
        // THE OTHER DIRECTION: a tree that closes EARLY leaves entries no walk
        // reaches. It takes a hand-built layout to reach, and that is itself
        // worth stating: dropping a child of a TABLE is caught one rule sooner,
        // by the size that no longer sums, so the only subtree whose loss the
        // size rule cannot see is one that contributes NO size — an enum's
        // variants, at kind 32 and size 0.
        std::vector<uint8_t> layout;
        layout.resize( 4 );
        tblfx1::TableFixedPut32( layout.data(), 4u );
        put_entry( layout, 1u, 13u, 4u, 1u ); // a table of one field
        put_entry( layout, 2u, 30u, 4u, 0u ); // an enum, its TWO variants unreached
        put_entry( layout, 3u, 32u, 0u, 0u );
        put_entry( layout, 4u, 32u, 0u, 0u );
        std::vector<uint8_t> f = file_of( layout );
        refuses( f, tblfx1::layout_tree_unclosed, "RULE: the layout outlasts the tree" );
    }

    // 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW
    {
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 0 ) + 9, 65537u );
        refuses( f, tblfx1::layout_record_too_large, "RULE: a record size past 65536" );
    }
    {
        // A SIZE THAT WOULD WRAP. The children's sizes are summed in 64 bits
        // precisely so a u32 that overflows is CAUGHT rather than wrapped into
        // a small number that then agrees with a parent.
        std::vector<uint8_t> f = good;
        tblfx1::TableFixedPut32( entry_at( f, 1 ) + 9, 0xFFFFFFFFu );
        refuses( f, tblfx1::layout_record_too_large, "RULE: a size that would overflow the sum" );
    }

    // 7. NOTHING NESTED PAST THE READER'S WALK BOUND. A BOUND ON THE WALK AND
    //    NOT ON THE WIRE: the validation recurses, so a layout of a thousand
    //    entries each claiming one child would spend a reader's stack before
    //    any other rule could fire. Nothing in §3.4 fixes the number.
    {
        const uint32_t depth = 4096u; // far past any reader's own bound
        std::vector<uint8_t> layout;
        layout.resize( 4 );
        tblfx1::TableFixedPut32( layout.data(), depth + 1u );
        for ( uint32_t i = 0; i < depth; ++i )
        {
            // a table, then optional wrappers all the way down
            put_entry( layout, 1u, ( i == 0 ) ? 13u : 35u, depth - i, 1u );
        }
        put_entry( layout, 2u, 1u, 1u, 0u ); // a bool at the bottom
        std::vector<uint8_t> f = file_of( layout );
        refuses( f, tblfx1::layout_too_deep, "RULE: a nesting depth past the walk's own bound" );
    }

    // AND THE RESIDUE: bytes that are not a layout at all, which is the one
    // case the seven named rules never reach.
    {
        std::vector<uint8_t> f = file_of( std::vector<uint8_t>( 2, 0 ) );
        refuses( f, tblfx1::layout_malformed, "RULE: fewer bytes than a header is layout_malformed" );
    }
}

// ---------------------------------------------------------------------------

static void fuzz_case()
{
    // A LAYOUT IS A STRANGER'S BYTES, AND THAT IS THIS FORM'S WHOLE RISK. The
    // record carries no lengths and no terminators, so every offset the reader
    // uses is arithmetic over sizes the WRITER wrote down. A layout whose child
    // sizes do not sum to its parent's, or whose tree is a chain ten thousand
    // deep, is not a hypothetical — it is one flipped byte away.
    //
    // So: every byte of a form-3 file, flipped one bit at a time, handed to a
    // reader of the other generation. The reader must answer one of three ways
    // and never a fourth — a refusal by name, a malformed read, or a read that
    // lands values. UNDER ASAN "never a fourth" includes never touching a byte
    // outside the buffer, which is what this is really for. The named rules
    // above say which refusal a given break earns; this says there is no break
    // that earns none of them.
    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 5150u;
    two.narrow = 70000u;
    two.renamed_to = 808;
    two.added = 909;
    two.nested.a = 33;
    two.nested.b = 44;
    two.extra.x = 55;
    two.extra.y = 66;
    std::vector<uint8_t> clean( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    tblfx2::FxRootFixedSave( &two, 1, clean.data(), (int64_t) clean.size() );

    std::vector<tblfx1::TableFixedEntry> plan( 1024 );
    int64_t refused = 0, damaged = 0, read = 0;
    for ( size_t at = 0; at < clean.size(); ++at )
    {
        for ( int bit = 0; bit < 8; ++bit )
        {
            std::vector<uint8_t> hit = clean;
            hit[at] = (uint8_t) ( hit[at] ^ ( 1u << bit ) );
            tblfx1::FxRoot v;
            tblfx1::TableReport r;
            const int64_t n = tblfx1::FxRootFixedLoad( &v, 1, hit.data(), (int64_t) hit.size(),
                                                       plan.data(), (int32_t) plan.size(), &r );
            if ( n < 0 )
            {
                if ( r.refused ) { refused++; check( !r.malformed, "fuzz: a refusal is never damage" ); }
                else { damaged++; check( r.malformed, "fuzz: a negative read that is not a refusal is damage" ); }
                continue;
            }
            read += n;
            check( !r.refused, "fuzz: a read that returned records was not refused" );
        }
    }
    std::printf( "fuzz: %zu bytes x 8 bits — %lld refused by name, %lld malformed, %lld records read, 0 divergences\n",
                 clean.size(), (long long) refused, (long long) damaged, (long long) read );
}

int main()
{
    std::printf( "FX1 FxRoot: body %lld, layout %lld, identity plan %d entries\n",
                 (long long) tblfx1::FxRootFixedBodyBytes, (long long) tblfx1::FxRootFixedLayoutBytes,
                 (int) tblfx1::FxRootFixedPlanCount );
    fx_case();
    fu_case();
    fn_case();
    wstring_case();
    w_case();
    bits_case();
    v_case();
    s_case();
    fl_case();
    text_case();
    frame_case();
    p_case();
    negative_control();
    layout_validation();
    fuzz_case();
    failures += known_red_report();
    if ( failures != 0 ) { std::printf( "%d failure(s)\n", failures ); return 1; }
    std::printf( "fixed form: versioning conformance green\n" );
    return 0;
}
