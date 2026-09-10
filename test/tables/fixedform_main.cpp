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
//                            cannot name (FM1/FM2)
//  11. THE SCALAR FAMILY     the fixed-point widths, the 128-bit integers, a
//                            ranged field whose bounds TIGHTENED, and the NaN
//                            bit patterns across the f32 -> f64 rung
//  12. THE FRAME             a file of zero records, of many, one with bytes
//                            left over, and one cut short
//  13. THE NARROW KINDS      int8/int16/uint8/uint16 spelled as themselves
//                            (NK1/NK2)
//  14. A COMPRESSED FLOAT    IEEE-754 bytes, not a quantized index (FC1/FC2)
//  15. bits(N) ACROSS        a second generation of RangedWidths (RW2)
//      GENERATIONS
//  16. THE PLAN'S SHAPE      partitioned, adjacent copies coalesced
//  17. A KIND THAT MOVED     fixed(16, 16) -> int32 on a 12-byte body, so
//                            RED-8 is visible without RED-3's clobber (KM1/KM2)
//  18. A `was =` CHAIN       label -> caption -> title, each keeping the
//                            FIRST wire name (WC1/WC2/WC3)
//
// The whole matrix these cases fill, cell by cell, is docs/FIXED-FORM-COVERAGE.md.
//
// THE LAYOUT is what form 1 called the vocabulary block. It is neither §7's
// cooked block nor §19's block form.
#include <cstdint>
#include <cstdio>
#include <cstring>
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
#include "UT1Table.h"
#include "UT2Table.h"
#include "NK2Table.h"
#include "FC2Table.h"
#include "RW2Table.h"
#include "KM2Table.h"
#include "WC2Table.h"
#include "WC3Table.h"

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
    // THE PLAN ENTRY'S `arg` LANE ONCE CARRIED TWO THINGS AND NOW CARRIES ONE.
    // A guarded entry's `arg` was the union ARM ORDINAL the read loop tests the
    // tag against and a `text` entry's `arg` was the field's FLAVOUR, and they
    // were one byte — so a text field under a union arm could not carry both,
    // and the two plans lost opposite halves of it in silence. FM1/FM2 is the
    // pair that made it reachable and the two entries that named it here are
    // DELETED, which is what landing a fix costs: `tables: the guard's ordinal
    // and the text op's flavour are two lanes`.
    // §3.4's op table names a `clamp` op — "reconstruct against the writer's
    // declared range and apply the reader's own, `clamped` counts if it fired"
    // — and the reference's op set has no such op: a ranged field whose reader
    // declares TIGHTER bounds than the writer is copied through unclamped, so
    // a value outside the reader's own declared range lands as if it were in
    // it. Scalars/Scalars2 is the pair whose bounds tighten, and this is the
    // cell that says the op is missing rather than untested.
    // REDS THE TIP DELETED, and the both-ends check is why they are not in this
    // list: invalid UTF-8 is malformed (`e53bfede`); the run copy never touches
    // a byte outside its run (#842 `83613f77`); a moved kind is skipped; the
    // bounds pass makes identity and compiled agree on an ordinal past the last
    // variant and a tag past the last arm; bytes(N) under a compiled plan lands;
    // a `clamp` op exists and tightened bounds fire it. Their assertions are
    // ordinary checks now.
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
};

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
    // A `bytes(N)` IS AN ARRAY OF u8 ON THIS WIRE (§3.4), so its destination row
    // is an ARRAY's — the buffer, and the live length beside it — and not a
    // text field's, which is the other way round. Only a COMPILED plan reads
    // those columns, so only FX2's read of this record can tell.
    one.blob[0] = 0xDE; one.blob[1] = 0xAD; one.blob[2] = 0xBE; one.blob[3] = 0xEF;
    one.blob_length = 4;

    std::vector<uint8_t> w1( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w1.data(), (int64_t) w1.size() ) == (int64_t) w1.size(), "FX1 save" );

    // 1. SAME SCHEMA — the identity plan
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, NULL, &r );
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
        const int64_t n = tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, NULL, &r );
        check( n == 1, "older writer: one record" );
        check( back.keep == 4242u, "older writer: an unmoved field" );
        check( back.narrow == 40000u, "WIDENED: uint16 into uint32, exactly" );
        check( r.widened == 1, "WIDENED: one widened counts" );
        check( back.renamed_to == 321, "RENAMED: `was =` keeps the wire id" );
        check( back.added == 11, "MISSING: a field the writer does not carry takes its declared default" );
        check( back.extra.x == 0 && back.extra.y == 0, "MISSING: a whole nested type takes its defaults" );
        check( back.nested.a == 111 && back.nested.b == 222, "older writer: the nesting" );
        check( back.blob_length == 4 && back.blob[0] == 0xDE && back.blob[1] == 0xAD &&
               back.blob[2] == 0xBE && back.blob[3] == 0xEF,
               "BYTES(N): the compiled plan lands the buffer in the buffer and the length in the length" );
        check( back.blob[4] == 0 && back.blob[5] == 0, "BYTES(N): the slack past the live length is zero" );
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
        const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r );
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
    const int64_t n = tblv2::CfgFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 8192, NULL, &r );
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
    const int64_t n = tblp3::ChainFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r );
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
        tblfm1::MarkRoot r;
        FillFm1Wide( r );
        std::vector<uint8_t> f = fu_file( r, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: wide" );
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &rep );
        check( n == 1, "ARM/identity/wide: one record" );
        check( back.mark.type == tblfm1::MarkType::Wide, "ARM/identity/wide: the tag" );
        check( back.mark.wide.n == 31 && back.id == 1001u && back.after == 9, "ARM/identity/wide: the scalars beside the text" );
        check( back.mark.wide.w_length == 3 && back.mark.wide.w[0] == (char16_t) 0x0041 &&
               back.mark.wide.w[1] == (char16_t) 0x0142 && back.mark.wide.w[2] == (char16_t) 0x0043 &&
               back.mark.wide.w[3] == (char16_t) 0,
               "ARM/identity/wide: a wstring(4) under ARM 1 keeps its FLAVOUR, so the terminator lands at TWICE the length" );
        check( rep.unknown == 0 && rep.kind_mismatch == 0 && !rep.malformed && !rep.refused,
               "ARM/identity/wide: a clean read moves no counter" );
    }
    {
        tblfm1::MarkRoot r;
        FillFm1Narrow( r );
        std::vector<uint8_t> f = fu_file( r, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: narrow" );
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 1024 );
        const int64_t n = tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &rep );
        check( n == 1, "ARM/identity/narrow: one record" );
        check( back.mark.type == tblfm1::MarkType::Narrow, "ARM/identity/narrow: the tag" );
        check( back.mark.narrow.n == 55, "ARM/identity/narrow: the scalar beside the text" );
        check( back.mark.narrow.s_length == 5 && std::strcmp( back.mark.narrow.s, "hello" ) == 0 && rep.clamped == 0,
               "ARM/identity/narrow: a string(6) under ARM 2 keeps its own bound, so five used bytes are not clamped to three" );
    }
    {
        tblfm1::MarkRoot r;
        FillFm1Raw( r );
        std::vector<uint8_t> f = fu_file( r, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: raw" );
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 1024 );
        check( tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &rep ) == 1,
               "ARM/identity/raw: one record" );
        check( back.mark.type == tblfm1::MarkType::Raw && back.mark.raw.d_length == 4 && back.mark.raw.n == 77 &&
               back.mark.raw.d[0] == 0xDEu && back.mark.raw.d[3] == 0xEFu,
               "ARM/identity/raw: bytes(4) at USED == MAX, and no terminator is written past it" );
        check( rep.clamped == 0, "ARM/identity/raw: a used length at the bound is not a clamp" );
    }
    {
        tblfm1::MarkRoot r;
        FillFm1List( r );
        std::vector<uint8_t> f = fu_file( r, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: list" );
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 1024 );
        check( tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &rep ) == 1,
               "ARM/identity/list: one record" );
        check( back.mark.type == tblfm1::MarkType::List && back.mark.list.items_count == 2 &&
               back.mark.list.items[0] == 11 && back.mark.list.items[1] == 22 && back.mark.list.n == 99,
               "ARM/identity/list: a COUNTED ARRAY under an arm, at MAX" );
        check( rep.clamped == 0, "ARM/identity/list: a count at the bound is not a clamp" );
    }
    {
        // TAG 0 IS `None` AND IT IS NOT AN ARM. Every guarded entry's tag test
        // fails, so the arm storage keeps the prefill and no arm's bytes are
        // read into another arm's slot.
        tblfm1::MarkRoot r;
        tblfm1::MarkRootReset( r );
        r.id = 1005u;
        r.after = 4;
        std::vector<uint8_t> f = fu_file( r, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: none" );
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 1024 );
        check( tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &rep ) == 1,
               "ARM/identity/none: one record" );
        check( back.mark.type == tblfm1::MarkType::None, "TAG 0: `None` is not an arm and selects none of them" );
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
        tblfm1::MarkRoot r;
        FillFm1Narrow( r );
        std::vector<uint8_t> f = fu_file( r, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: for the bad tag" );
        const size_t body = (size_t) tblfm1::kTableFixedHeaderBytes + 4 + (size_t) tblfm1::MarkRootFixedLayoutBytes + 8;
        f[body + 4] = 9u; // the tag byte, past the last arm
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 1024 );
        check( tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &rep ) == 1,
               "TAG PAST THE ARMS: still one record" );
        check( back.id == 1002u && back.after == 9, "TAG PAST THE ARMS: the fields either side of the union still land" );
        check( !rep.malformed && !rep.refused, "TAG PAST THE ARMS: never damage and never a refusal" );

        // the same bytes down the OTHER path, and the two answers compared
        tblfm2::MarkRoot two;
        tblfm2::TableReport rep2;
        std::vector<tblfm2::TableFixedEntry> plan2( 4096 );
        check( tblfm2::MarkRootFixedLoad( &two, 1, f.data(), (int64_t) f.size(), plan2.data(), 4096, NULL, &rep2 ) == 1,
               "TAG PAST THE ARMS: the compiled plan reads the record too" );
        check( two.mark.type == tblfm2::MarkType::None,
               "TAG PAST THE ARMS/compiled: a tag naming no arm resolves to None, which is §4's answer for a name this reader has not got" );
        check( (int) back.mark.type == (int) two.mark.type,
               "a tag of 9 over a four-armed union: identity and compiled agree (the bounds pass)" );
    }

    // ---- 2. THE COMPILED PLAN, both directions ---------------------------
    //
    // FM2 inserts `skip` as arm 2, so every ordinal past the first moves: an
    // arm's ordinal on the wire is never the arm's ordinal in the reader.
    {
        tblfm1::MarkRoot one;
        FillFm1Narrow( one );
        std::vector<uint8_t> f = fu_file( one, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: narrow for FM2" );

        tblfm2::MarkRoot back;
        tblfm2::TableReport rep;
        std::vector<tblfm2::TableFixedEntry> plan( 4096 );
        const int64_t n = tblfm2::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &rep );
        check( n == 1, "ARM/compiled/older writer: one record" );
        check( back.mark.type == tblfm2::MarkType::Narrow,
               "ARM/compiled: an arm is resolved BY NAME, so ordinal 2 lands on ordinal 3" );
        check( back.mark.narrow.n == 55, "ARM/compiled: the scalar beside the text lands under the arm's own guard" );
        check( back.tail == 3, "ARM/compiled: a field the writer does not carry takes its declared default" );
        check( back.mark.narrow.s_length == 5 && std::strcmp( back.mark.narrow.s, "hello" ) == 0,
               "ARM/compiled: an arm's text lands, because the guard tests the tag against the ARM ORDINAL and not against the flavour" );
        check( !rep.malformed && !rep.refused, "ARM/compiled/older writer: no damage and no refusal" );
    }
    {
        tblfm1::MarkRoot one;
        FillFm1Wide( one );
        std::vector<uint8_t> f = fu_file( one, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: wide for FM2" );
        tblfm2::MarkRoot back;
        tblfm2::TableReport rep;
        std::vector<tblfm2::TableFixedEntry> plan( 4096 );
        check( tblfm2::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &rep ) == 1,
               "ARM/compiled/wide: one record" );
        check( back.mark.type == tblfm2::MarkType::Wide, "ARM/compiled/wide: an UNMOVED arm's tag" );
        check( back.mark.wide.w_length == 3 && back.mark.wide.w[1] == (char16_t) 0x0142,
               "ARM/compiled/wide: an UNMOVED arm's text lands, ordinal and flavour both intact" );
    }
    {
        // AN ARM THIS READER HAS NO NAME FOR. FM2 selects `skip`; FM1 has no
        // such arm, so no entry of FM1's plan answers that tag, the union keeps
        // its prefill, and the fields either side of it still land.
        tblfm2::MarkRoot two;
        tblfm2::MarkRootReset( two );
        two.id = 2001u;
        two.after = 6;
        two.tail = 8;
        two.mark.type = tblfm2::MarkType::Skip;
        two.mark.skip.e = 42;
        std::vector<uint8_t> f = fu_file( two, tblfm2::MarkRootFixedMeasure, tblfm2::MarkRootFixedSave, "FM2 save: skip" );

        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 4096 );
        const int64_t n = tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &rep );
        check( n == 1, "ARM/compiled/newer writer: one record" );
        check( back.mark.type == tblfm1::MarkType::None,
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
        tblfm2::MarkRoot two;
        tblfm2::MarkRootReset( two );
        two.id = 2002u;
        two.after = 5;
        two.mark.type = tblfm2::MarkType::List;
        two.mark.list.items[0] = 71;
        two.mark.list.items[1] = 72;
        two.mark.list.items_count = 2;
        two.mark.list.n = 13;
        std::vector<uint8_t> f = fu_file( two, tblfm2::MarkRootFixedMeasure, tblfm2::MarkRootFixedSave, "FM2 save: list" );

        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 4096 );
        check( tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &rep ) == 1,
               "ARM/compiled/list: one record" );
        check( back.mark.type == tblfm1::MarkType::List, "ARM/compiled/list: ordinal 5 lands on ordinal 4, by name" );
        check( back.mark.list.items_count == 2 && back.mark.list.items[0] == 71 && back.mark.list.items[1] == 72 &&
               back.mark.list.n == 13, "ARM/compiled/list: a COUNTED ARRAY under an arm survives the plan compile" );
    }
    {
        // bytes(N) UNDER A COMPILED PLAN, BOTH DIRECTIONS (GAP-5 was the
        // newer-writer half). The raw arm is flavour 3; FM2 inserted skip so
        // its ordinal moved from 3 to 4.
        tblfm1::MarkRoot one;
        FillFm1Raw( one );
        std::vector<uint8_t> f = fu_file( one, tblfm1::MarkRootFixedMeasure, tblfm1::MarkRootFixedSave, "FM1 save: raw for FM2" );
        tblfm2::MarkRoot back;
        tblfm2::TableReport rep;
        std::vector<tblfm2::TableFixedEntry> plan( 4096 );
        check( tblfm2::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &rep ) == 1,
               "ARM/compiled/raw/older writer: one record" );
        check( back.mark.type == tblfm2::MarkType::Raw, "ARM/compiled/raw: ordinal 3 lands on ordinal 4, by name" );
        check( back.tail == 3 && !rep.malformed && !rep.refused,
               "ARM/compiled/raw/older writer: the field behind the arm, no damage" );
        check( back.mark.raw.d_length == 4 && back.mark.raw.d[0] == 0xDEu && back.mark.raw.d[3] == 0xEFu &&
               back.mark.raw.n == 77,
               "ARM/compiled/raw: a compiled plan walks bytes(N) as kind 14" );
    }
    {
        tblfm2::MarkRoot two;
        FillFm2Raw( two );
        std::vector<uint8_t> f = fu_file( two, tblfm2::MarkRootFixedMeasure, tblfm2::MarkRootFixedSave, "FM2 save: raw" );
        tblfm1::MarkRoot back;
        tblfm1::TableReport rep;
        std::vector<tblfm1::TableFixedEntry> plan( 4096 );
        check( tblfm1::MarkRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &rep ) == 1,
               "ARM/compiled/raw/newer writer: one record" );
        check( back.mark.type == tblfm1::MarkType::Raw, "ARM/compiled/raw/newer: ordinal 4 lands on ordinal 3, by name" );
        check( back.after == 5 && !rep.malformed && !rep.refused,
               "ARM/compiled/raw/newer writer: the field behind the arm, no damage" );
        check( back.mark.raw.d_length == 4 && back.mark.raw.d[0] == 0xCAu && back.mark.raw.d[3] == 0xBEu &&
               back.mark.raw.n == 88,
               "bytes(N) ACROSS GENERATIONS: the compiled plan's kind-14 walk lands the used bytes" );
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
        const int64_t n = scalardemo::SimStateFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &r );
        check( n == 1, "SCALARS/identity: one record" );
        check( back.tilt == one.tilt && back.angle == one.angle && back.position == one.position &&
               back.ticks == one.ticks && back.ratio == one.ratio &&
               back.speed == one.speed && back.frames == one.frames && back.scale == one.scale,
               "SCALARS/identity: the fixed-point family at every storage width" );
        // `span` is a ufixed(48, 16) AT ITS FULL EXTENT, and the entry behind it
        // is a twenty-byte run: the run copy's third move is anchored twelve
        // bytes in FRONT of it and writes `speed`'s raw bytes over span's high
        // half. A value that fits in 48 bits is the only one that shows it.
        check( back.span == one.span, "ufixed(48, 16) at full extent: the following 20-byte run writes over its high four bytes" );
        check( back.reach == one.reach && back.mass == one.mass && back.flux == one.flux &&
               back.energy == one.energy && back.entity_id == one.entity_id,
               "SCALARS/identity: int128 and uint128, the low half then the high" );
        check( back.samples[0] == one.samples[0] && back.samples[1] == one.samples[1] &&
               back.samples[2] == one.samples[2], "SCALARS/identity: a FIXED array of a fixed-point type" );
        check( back.weights[0] == 256u && back.weights[3] == 2048u,
               "SCALARS/identity: a COUNTED array at MAX, its elements" );
        check( back.weights_count == 4, "a counted array's COUNT: the 24-byte run behind it starts eight bytes early and writes two elements over it" );
        check( back.axes[scalardemo::Axis::X] == one.axes[scalardemo::Axis::X] &&
               back.axes[scalardemo::Axis::Y] == one.axes[scalardemo::Axis::Y],
               "SCALARS/identity: an ENUM-KEYED array of a 64-bit fixed-point type" );
        check( back.seeds[0] == one.seeds[0] && back.seeds[1] == one.seeds[1],
               "SCALARS/identity: a COUNTED array of uint128, its elements" );
        check( back.seeds_count == 2, "a counted array of uint128: its COUNT is written over by the 20-byte run behind it" );
        check( back.pose.x == one.pose.x, "SCALARS/identity: the nested type's leading field" );
        check( back.pose.heading == one.pose.heading, "a nested type's LAST field: the optional behind it runs twelve bytes early and writes the tail of pose.y over it" );
        check( back.spawn.x == one.spawn.x && back.spawn.heading == one.spawn.heading,
               "SCALARS/identity: an OPTIONAL of a nested type, its payload" );
        // AND THIS ONE IS NOT ONLY A WRONG VALUE. The move that lands on the
        // present flag reads eleven bytes PAST the record body, which for the
        // last record of a file is past the buffer — so what this flag holds is
        // whatever followed the allocation. The sanitized twin of this binary
        // is what turns that from a wrong answer into a named fault.
        check( back.spawn_present, "an OPTIONAL's PRESENT FLAG, read from past the end of the record body: whatever follows the buffer decides it" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && !r.malformed && !r.refused,
               "SCALARS/identity: a clean read moves no counter" );
    }

    // 2. THE COMPILED PLAN: the same record read by the later build
    {
        scalardemo2::SimState back;
        scalardemo2::TableReport r;
        std::vector<scalardemo2::TableFixedEntry> plan( 4096 );
        const int64_t n = scalardemo2::SimStateFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &r );
        check( n == 1, "SCALARS/compiled: one record" );
        check( back.ratio == one.ratio && back.span == one.span &&
               back.frames == one.frames && back.scale == one.scale,
               "SCALARS/compiled: the unchanged widths land through a compiled plan" );
        // `tilt` IS THE FIRST FIELD OF THE BODY, at destination offset zero, and
        // the run copy's broken branch anchors its last move BEFORE the run — so
        // on this plan the field at offset zero is written from bytes in front
        // of its own source. It is the same defect the identity plan shows four
        // fields further in, met at the other end of the record.
        check( back.tilt == one.tilt, "the field at DESTINATION OFFSET ZERO: the run copy's third move is anchored in front of the struct, so tilt is written from the wrong bytes" );
        check( back.reach == one.reach && back.energy == one.energy && back.mass == one.mass,
               "SCALARS/compiled: the 128-bit fields land" );
        check( back.pose.x == one.pose.x && back.spawn.x == one.spawn.x,
               "SCALARS/compiled: the nested type and the optional's payload" );
        check( back.spawn_present, "the same present flag through a COMPILED plan: the defect is in the copy primitive, so the plan it came from does not matter" );
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
        check( back.angle == 0, "`angle` respelled fixed(16, 16) -> int32: kind_mismatch is counted AND the raw scale is handed back, where §4 requires the declared default" );
        check( !r.malformed && !r.refused, "SCALARS/compiled: no damage and no refusal" );
        // §3.4's op table names a `clamp`: "reconstruct against the writer's
        // declared range and apply the reader's own, `clamped` counts if it
        // fired". `position` rides 20000 whole units and this reader declares
        // [-1000, 1000]; `speed` rides 500 against [0, 10].
        check( back.position == (int64_t) 1000 * 65536 && back.speed == 10u * 65536u && r.clamped == 2,
               "a ranged field whose reader's bounds TIGHTENED is clamped, counted twice" );
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
        check( tblf1::FloatsFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
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
        check( tblf2::FloatsFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
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
    return tblp1::ChainFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r );
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

    // 4. AND THE USED UNITS ARE NOT EXEMPT. Damage is one field's: the record
    //    still reads, the field takes its declared default, one malformed counts
    //    (e53bfede; text_content_case is the same ruling on FX1).
    {
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        f[bytes_at + 2] = 0xFFu; // a lead byte UTF-8 never uses, INSIDE the length
        tblp1::Chain back; tblp1::TableReport r;
        const int64_t n = p1_read( f, back, r );
        check( n == 1 && r.malformed && !r.refused,
               "ill-formed UTF-8 inside a string(N)'s USED bytes is malformed; the record still reads" );
        check( back.name_length == 0 && back.name[0] == 0,
               "ill-formed UTF-8: the damaged field reads its declared default" );
    }
    {
        std::vector<uint8_t> f = p1_file( "chain", 5 );
        f[bytes_at + 2] = 0x00u; // an INTERIOR ZERO BYTE, inside the length
        tblp1::Chain back; tblp1::TableReport r;
        const int64_t n = p1_read( f, back, r );
        check( n == 1 && r.malformed && !r.refused,
               "an interior zero byte inside a string(N)'s USED bytes is malformed; the record still reads" );
        check( back.name_length == 0 && back.name[0] == 0,
               "interior zero: the damaged field reads its declared default" );
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
        check( tblfx1::FxRootFixedLoad( back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 0,
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
        check( tblfx1::FxRootFixedLoad( back, 3, three.data(), (int64_t) three.size(), plan.data(), 1024, NULL, &r ) == 3,
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
        const int64_t n = tblfx1::FxRootFixedLoad( back, 2, three.data(), (int64_t) three.size(), plan.data(), 1024, NULL, &r );
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
        const int64_t n = tblfx1::FxRootFixedLoad( back, 3, ragged.data(), (int64_t) ragged.size(), plan.data(), 1024, NULL, &r );
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
            const int64_t n = tblfx1::FxRootFixedLoad( back, 3, three.data(), (int64_t) cut, plan.data(), 1024, NULL, &r );
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
    return tblfn1::FnRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &r );
}

static int64_t fn2_read( const std::vector<uint8_t> & f, tblfn2::FnRoot & back, tblfn2::TableReport & r )
{
    std::vector<tblfn2::TableFixedEntry> plan( 4096 );
    return tblfn2::FnRootFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 4096, NULL, &r );
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
        check( (int) a.tier == (int) b.tier,
               "an ordinal of 9 over a two-variant enum: identity and compiled agree (the bounds pass)" );
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
    // The tip's clamp pass does `if ( value.opt_present )` after the copy. A
    // present byte of 7 is a `bool` holding 7, and UBSan aborts the sanitized
    // twin on that load. The plain binary holds the row; the flag pokes above
    // already reach the same known-red under asan.
#if !defined( SCHEMA_FIXEDFORM_SANITIZED )
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
#endif

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
    // FM1 names, and it is listed under the same key.
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
        check( (int) a.picks[0].type == (int) b.picks[0].type,
               "a bad tag on ONE ELEMENT of an array of unions: identity and compiled agree (the bounds pass)" );
    }
}

// ---------------------------------------------------------------------------

// A TOP-LEVEL wstring(N) (docs/SPEC-TABLES.md §3.4's `wstring(N)` row).
//
// FLAVOUR 2 HAD NO ORACLE BYTES ANYWHERE IN THIS PROJECT. FM1's `wide` arm is
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
    check( wide::StampFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
           "WSTRING: one record" );
    check( back.label_length == 4, "WSTRING: the used length comes back in CODE UNITS" );
    check( back.label[0] == (char16_t) 0x0041 && back.label[1] == (char16_t) 0x00E9 &&
           back.label[2] == (char16_t) 0x4E2D,
           "WSTRING: every code unit comes back, high bytes included" );
    check( back.label[3] == (char16_t) 0x20AC,
           "WSTRING: U+20AC, both bytes non-zero, rides as one code unit" );
    check( back.seq == 90210u, "WSTRING: the field behind the payload lands at 2N and not at N" );
    check( r.clamped == 0 && !r.malformed && !r.refused, "WSTRING: a clean read moves no counter" );

    // A LONE SURROGATE IS DAMAGE on this tip (the same content pass as UTF-8):
    // the field reads its declared default, one malformed counts, the rest of
    // the record stands.
    {
        std::vector<uint8_t> g = f;
        g[body + 4 + 6] = 0x3D; // U+D83D little-endian, a high surrogate with no low half
        g[body + 4 + 7] = 0xD8;
        wide::Stamp b2;
        wide::TableReport r2;
        check( wide::StampFixedLoad( &b2, 1, g.data(), (int64_t) g.size(), plan.data(), 1024, NULL, &r2 ) == 1 &&
               r2.malformed && !r2.refused,
               "WSTRING: a lone surrogate is malformed; the record still reads" );
        check( b2.label_length == 0 && b2.seq == 90210u,
               "WSTRING: the damaged field reads its default; seq behind it stands" );
    }

    // A LENGTH PAST THE FIELD'S OWN BOUND, in CODE UNITS: `count`'s work on the
    // length (§3.4's `text` op), so it clamps to the bound and counts.
    {
        std::vector<uint8_t> g = f;
        wide::TableFixedPut32( g.data() + body, 99u );
        wide::Stamp b2;
        wide::TableReport r2;
        check( wide::StampFixedLoad( &b2, 1, g.data(), (int64_t) g.size(), plan.data(), 1024, NULL, &r2 ) == 1 &&
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
        check( tblw1::VesselFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
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
        check( tblw2::ShipFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "TABLE RENAMED: a W1 Vessel reads into a W2 Ship" );
        check( back.caps == (tblw2::Caps) tblw2::Caps_Crouch && back.hull == 250,
               "TABLE RENAMED: every field lands under the old name's hash" );
        check( r.unknown == 0 && r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "TABLE RENAMED: `was` on a TABLE is not an evolution event at all" );
    }
    {
        // THE OTHER DIRECTION (GAP-4): a W2 Ship read as a W1 Vessel. The
        // flags field is the same kind; the table's `was` keeps the hash.
        tblw2::Ship two;
        FillW2( two );
        std::vector<uint8_t> f2( (size_t) tblw2::ShipFixedMeasure( 1 ) );
        check( tblw2::ShipFixedSave( &two, 1, f2.data(), (int64_t) f2.size() ) == (int64_t) f2.size(), "W2 save" );
        tblw1::Vessel back;
        tblw1::TableReport r;
        std::vector<tblw1::TableFixedEntry> plan( 1024 );
        check( tblw1::VesselFixedLoad( &back, 1, f2.data(), (int64_t) f2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "FLAGS/compiled/newer writer: a W2 Ship reads into a W1 Vessel" );
        check( back.caps == (tblw1::Caps) tblw1::Caps_Jump && back.hull == 400,
               "FLAGS/compiled/newer writer: the raw mask and the field beside it" );
        check( back.name_length == 9 && std::strcmp( back.name, "discovery" ) == 0,
               "FLAGS/compiled/newer writer: the string beside the mask" );
        check( r.unknown == 0 && !r.malformed && !r.refused,
               "FLAGS/compiled/newer writer: a table rename is not an evolution event in this direction either" );
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
    check( tabledemo::RangedWidthsFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
           "BITS: one record" );
    check( back.b8 == 0xFFu && back.b16 == 0xFFFFu && back.b32 == 0xFFFFFFFFu,
           "BITS: the widths that fill a uint32 exactly" );
    check( back.b64 == 0xFFFFFFFFFFFFFFFFull, "BITS: the width that fills a uint64 exactly" );
    check( back.b12 == 0x0FFFu && back.b48 == 0x0000FFFFFFFFFFFFull,
           "BITS: the two widths that do NOT fill their storage ride in the whole of it anyway" );
    check( r.clamped == 0 && !r.malformed && !r.refused, "BITS: a value at the width's own bound is not a clamp" );

    {
        // A SECOND GENERATION (GAP-2). Storage width is fixed by N, so there
        // is no widened-bits rung; RW2 adds a bits(24) and a tail, which is
        // the edit a compiled plan can get wrong.
        tblrw2::RangedWidths newer;
        tblrw2::TableReport rn;
        std::vector<tblrw2::TableFixedEntry> plan2( 1024 );
        check( tblrw2::RangedWidthsFixedLoad( &newer, 1, f.data(), (int64_t) f.size(), plan2.data(), 1024, NULL, &rn ) == 1,
               "BITS/compiled/older writer: one record" );
        check( newer.b8 == 0xFFu && newer.b12 == 0x0FFFu && newer.b48 == 0x0000FFFFFFFFFFFFull,
               "BITS/compiled/older writer: every width of the first generation lands" );
        check( newer.b24 == 0u && newer.tail == 3,
               "BITS/compiled/older writer: the added bits field and the tail take their declared defaults" );
        check( rn.unknown == 0 && !rn.malformed && !rn.refused, "BITS/compiled/older writer: no damage" );
    }
    {
        tblrw2::RangedWidths two;
        FillBits2( two );
        std::vector<uint8_t> f2( (size_t) tblrw2::RangedWidthsFixedMeasure( 1 ) );
        check( tblrw2::RangedWidthsFixedSave( &two, 1, f2.data(), (int64_t) f2.size() ) == (int64_t) f2.size(), "RW2 save" );
        tabledemo::RangedWidths older;
        tabledemo::TableReport ro;
        std::vector<tabledemo::TableFixedEntry> plan3( 1024 );
        check( tabledemo::RangedWidthsFixedLoad( &older, 1, f2.data(), (int64_t) f2.size(), plan3.data(), 1024, NULL, &ro ) == 1,
               "BITS/compiled/newer writer: one record" );
        check( older.b8 == 0xFFu && older.b12 == 0x0FFFu && older.b64 == 0xFFFFFFFFFFFFFFFFull,
               "BITS/compiled/newer writer: the first generation's widths still land" );
        check( ro.unknown == 2 && !ro.malformed && !ro.refused,
               "BITS/compiled/newer writer: `b24` and `tail` are the two names this reader has not got" );
    }
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
    const int64_t n = tblfx1::FxRootFixedLoad( &right, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r2 );
    check( n == 1 && right.nested.a == 33 && right.nested.b == 44,
           "NEGATIVE CONTROL: the loader compiles a plan from the layout and gets it right" );

    // A LAYOUT THAT IS NOT A LAYOUT IS REFUSED BY NAME, whole, and never damage.
    // Every rule has its own case in layout_validation() below; this one is
    // here because it is also the check that a refusal MOVES NO COUNTER.
    {
        std::vector<uint8_t> broken = w2;
        // THE ENTRY COUNT, which is the layout's own first four bytes and not
        // the u32 LENGTH in front of them: a length that no longer matches the
        // file is a different rule with a different name.
        broken[tblfx1::kTableFixedHeaderBytes + 4] ^= 0xFFu;
        tblfx1::FxRoot v;
        tblfx1::TableReport r3;
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, broken.data(), (int64_t) broken.size(), plan.data(), 1024, NULL, &r3 );
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
            const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, other.data(), (int64_t) other.size(), plan.data(), 1024, NULL, &r4 );
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
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, lying.data(), (int64_t) lying.size(), plan.data(), 1024, NULL, &r6 );
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
        const int64_t bad = tblfx1::FxRootFixedLoad( &v, 1, w2.data(), (int64_t) w2.size(), tiny, 1, NULL, &r5 );
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
    const int64_t n = tblfx1::FxRootFixedLoad( &v, 1, broken.data(), (int64_t) broken.size(), plan.data(), 1024, NULL, &r );
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
        check( tblfx1::FxRootFixedLoad( &v, 1, good.data(), (int64_t) good.size(), plan.data(), 1024, NULL, &r ) == 1 && !r.refused,
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
                                                       plan.data(), (int32_t) plan.size(), NULL, &r );
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

// ---------------------------------------------------------------------------

// THE SLACK IS ZERO (docs/SPEC-TABLES.md §3.4). A `string(N)` shorter than N
// and a `[..N]T` with unused slots are DECLARED bytes carrying no value, and
// what rides in them is the TEMPLATE'S ZEROS — never whatever the writer's own
// storage happened to hold past the used length or the live count.
//
// Copying the whole span instead is not a cosmetic difference: for an array of
// a type with declared defaults, the unused slots put the ELEMENT'S DEFAULT
// IMAGE on the wire, so a value nobody wrote rides as if somebody had; and for
// text, a field written twice with two different values does not compare equal
// on the second write. The Elixir port found both.
//
// THE CONTROL IS THE STAIN. The storage past the used length and the live count
// is filled with a byte that appears nowhere else in a clean record; the test
// first proves the stain IS there in the storage (or it would be checking
// nothing), then proves the WIRE carries none of it, and then proves that a
// whole-span copy of the same storage WOULD have carried it — which is the
// wrong behaviour, watched failing.
static void slack_case()
{
    tblfx1::FxRoot v;
    tblfx1::FxRootReset( v );
    v.keep = 11u;
    v.narrow = 22u;
    v.renamed = 33;
    v.gone = 44;
    v.nested.a = 55;
    v.nested.b = 66;
    std::memset( v.label, 0xAA, sizeof( v.label ) );
    v.label[0] = 'h';
    v.label[1] = 'i';
    v.label_length = 2;
    for ( int k = 0; k < 4; ++k ) { v.marks[k] = 0x5A5A5A5A; }
    v.marks[0] = 7;
    v.marks_count = 1;

    // the stain is really in the storage, so nothing below is vacuous
    check( (uint8_t) v.label[2] == 0xAAu, "CONTROL: the text slack really is stained in storage" );
    check( v.marks[1] == 0x5A5A5A5A, "CONTROL: the array slack really is stained in storage" );

    std::vector<uint8_t> file( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "slack: the record saves" );
    const uint8_t * body = file.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
    const size_t body_bytes = (size_t) tblfx1::FxRootFixedBodyBytes;
    check( std::memchr( body, 0xAA, body_bytes ) == NULL,
           "SLACK IS ZERO: not one stained TEXT byte reached the wire" );
    check( std::memchr( body, 0x5A, body_bytes ) == NULL,
           "SLACK IS ZERO: not one stained ARRAY byte reached the wire" );

    // NEGATIVE CONTROL: the same storage copied WHOLE — which is what the
    // writer did before this fix — carries the stain, so the two checks above
    // discriminate and are not passing for some other reason.
    {
        uint8_t whole[sizeof( v.label )];
        std::memcpy( whole, v.label, sizeof( v.label ) );
        check( std::memchr( whole, 0xAA, sizeof( whole ) ) != NULL,
               "NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain" );
        int32_t marks[4];
        std::memcpy( marks, v.marks, sizeof( marks ) );
        check( marks[3] == 0x5A5A5A5A,
               "NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain" );
    }

    // and the record still reads back as itself: the used length and the live
    // count are what the reader validates, and both are inside the bound, so
    // nothing clamps.
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 1024, NULL, &r ) == 1,
               "slack: the record reads" );
        check( back.label_length == 2 && back.label[0] == 'h' && back.label[1] == 'i' && back.label[2] == 0,
               "slack: the used length reads, and the buffer terminates at it" );
        check( back.marks_count == 1 && back.marks[0] == 7, "slack: the live count reads" );
        check( back.marks[1] == 0 && back.marks[2] == 0 && back.marks[3] == 0,
               "slack: an unused slot lands as the wire's zero and not as some writer's leftover" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "slack: a clean read moves no counter" );
    }
}

// ---------------------------------------------------------------------------

// A `bytes(N)`'s DESTINATION ROW IS AN ARRAY'S (docs/SPEC-TABLES.md §3.4).
// `bytes(N)` rides as an array of u8, so the plan compiler lands it through the
// ARRAY case: the count goes to the row's aux and the elements to the row's
// dst. A text field's row is the other way round — dst the length, aux the
// buffer — and under that convention a `bytes(N)` hands the compiler a count
// destination that is the BUFFER'S FIRST FOUR BYTES and an element destination
// that is the LENGTH FIELD.
//
// THE NEGATIVE CONTROL IS THE OLD ROW, and it is built by swapping the two
// columns back on a copy of this build's own rows and compiling the same plan
// from the same layout. The row is found by its shape and not by its index —
// stride one and a live count is a `bytes(N)` and nothing else — so the control
// does not quietly stop pointing at it the day a field moves.
static void bytes_row_case()
{
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.blob[0] = 0xDE; one.blob[1] = 0xAD; one.blob[2] = 0xBE; one.blob[3] = 0xEF;
    one.blob_length = 4;
    std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "bytes row: FX1 save" );

    const size_t rows = sizeof( tblfx2::FxRootFixedDst ) / sizeof( tblfx2::FxRootFixedDst[0] );
    std::vector<tblfx2::TableFixedDst> swapped( tblfx2::FxRootFixedDst, tblfx2::FxRootFixedDst + rows );
    size_t found = rows;
    for ( size_t i = 0; i < rows; ++i )
    {
        if ( swapped[i].stride == 1u && swapped[i].counted != 0u ) { found = i; break; }
    }
    check( found != rows, "bytes row: the `bytes(N)` row is the one with stride one and a live count" );
    const uint32_t d = swapped[found].dst;
    swapped[found].dst = swapped[found].aux; // the TEXT convention, as it was
    swapped[found].aux = d;

    tblfx2::TableFixedLayoutView theirs;
    tblfx2::TableMessageReason why = tblfx2::layout_malformed;
    check( tblfx2::TableFixedParseLayout( tblfx1::FxRootFixedLayout, tblfx1::FxRootFixedLayoutBytes, theirs, why ),
           "bytes row: FX1's layout parses" );

    const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
    for ( int pass = 0; pass < 2; ++pass )
    {
        const tblfx2::TableFixedDst * rowset = pass == 0 ? tblfx2::FxRootFixedDst : swapped.data();
        std::vector<tblfx2::TableFixedEntry> plan( 2048 );
        int32_t guarded = 0;
        tblfx2::TableReport compile_report;
        const int32_t made = tblfx2::TableFixedCompile( theirs, tblfx2::FxRootFixedLayout, (int32_t) tblfx2::FxRootFixedLayoutBytes,
                                                        rowset, plan.data(), 2048, &guarded, &compile_report );
        check( made > 0, "bytes row: the plan compiles either way — the rows are not what refuses" );
        // THE DESTINATION IS OVERSIZED ON PURPOSE. The wrong rows put an
        // element destination where the LENGTH FIELD is, and six bytes of
        // elements past a four-byte field is a step outside the storage — so
        // the control gives it room to be wrong, and what reports the bug is
        // the value that comes back rather than the sanitizer.
        std::vector<uint64_t> storage( sizeof( tblfx2::FxRoot ) / 8 + 16, 0 );
        tblfx2::FxRoot * back = (tblfx2::FxRoot *) (void *) storage.data();
        tblfx2::FxRootReset( *back );
        tblfx2::TableReport r;
        tblfx2::TableFixedRun( plan.data(), made, guarded, body, (uint8_t *) back, &r );
        const bool right = back->blob_length == 4 && back->blob[0] == 0xDE && back->blob[1] == 0xAD &&
                           back->blob[2] == 0xBE && back->blob[3] == 0xEF;
        if ( pass == 0 )
        {
            check( right, "BYTES(N): the array row lands the buffer in the buffer and the length in the length" );
        }
        else
        {
            check( !right, "NEGATIVE CONTROL: the text row really does write the count into the buffer" );
        }
    }
}

// ---------------------------------------------------------------------------

// TWO LANES, BECAUSE THEY ARE TWO FACTS (docs/SPEC-TABLES.md §3.4). A plan
// entry carries the ordinal the GUARD byte must hold for the entry to run, and
// the argument the entry's OWN OP takes — for a text entry, its flavour. They
// had one lane between them, and a `string(N)` under a union's arm could be
// guarded correctly or read with the right flavour and could not be both.
//
// UT1's string sits under the SECOND arm on purpose: a `string(N)`'s flavour is
// 1 and the second arm's ordinal is 2, so the collision does not merely lose a
// fact — it turns a byte string into a WIDE one, halving the bound and
// terminating two bytes at a time.
static void union_text_case()
{
    tblut1::UtRoot one;
    tblut1::UtRootReset( one );
    one.head = 42;
    one.tail = 99;
    one.pick.type = tblut1::PickType::B;   // the SECOND arm
    std::strcpy( one.pick.b.label, "seven77" ); // seven characters, so the slack is real
    one.pick.b.label_length = 7;
    one.pick.b.m = 1234;

    std::vector<uint8_t> w( (size_t) tblut1::UtRootFixedMeasure( 1 ) );
    check( tblut1::UtRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "UT1 save" );

    // THE IDENTITY PLAN: the guard holds the arm ordinal, the flavour is the
    // text op's own, and the two are read out of different lanes.
    {
        check( tblut1::UtRootFixedPlan[3].op == tblut1::kTableFixedText, "two lanes: the plan's fourth entry is the text" );
        check( tblut1::UtRootFixedPlan[3].arg == 2, "two lanes: arg is the SECOND arm's ordinal" );
        check( tblut1::UtRootFixedPlan[3].meta == tblut1::kTableFixedTextUtf8, "two lanes: meta is the utf8 flavour" );

        tblut1::UtRoot back;
        tblut1::TableReport r;
        std::vector<tblut1::TableFixedEntry> plan( 1024 );
        check( tblut1::UtRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "two lanes: the record reads" );
        check( back.pick.type == tblut1::PickType::B, "two lanes: the arm" );
        check( back.pick.b.label_length == 7 && std::strcmp( back.pick.b.label, "seven77" ) == 0,
               "two lanes: the arm's string(8), whole" );
        check( back.pick.b.m == 1234 && back.head == 42 && back.tail == 99, "two lanes: the rest of the record" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "two lanes: a clean read moves no counter" );
    }

    // THE NEGATIVE CONTROL — the bug itself, watched failing. The flavour is
    // put back into the guard's lane, which is exactly what the one shared lane
    // did, and the SAME record read by the SAME loop comes out wrong: the arm's
    // ordinal 2 reads as kTableFixedTextWide, so the length is halved and the
    // terminator lands two bytes early.
    {
        std::vector<tblut1::TableFixedEntry> shared( tblut1::UtRootFixedPlan,
                                                     tblut1::UtRootFixedPlan + tblut1::UtRootFixedPlanCount );
        shared[3].meta = shared[3].arg; // ONE LANE, as it was
        tblut1::UtRoot wrong;
        tblut1::UtRootReset( wrong );
        tblut1::TableReport r;
        const uint8_t * body = w.data() + tblut1::kTableFixedHeaderBytes + 4 + tblut1::UtRootFixedLayoutBytes + 8;
        tblut1::TableFixedRun( shared.data(), tblut1::UtRootFixedPlanCount, tblut1::UtRootFixedPlanGuarded,
                               body, (uint8_t *) &wrong, &r );
        check( wrong.pick.b.label_length != 7 || std::strcmp( wrong.pick.b.label, "seven77" ) != 0,
               "NEGATIVE CONTROL: one shared lane really does read the arm's string wrong" );
    }

    // A COMPILED PLAN: UT2 inserted an arm IN THE MIDDLE, so the same string is
    // arm 3 here and arm 2 in the record. The guard must hold THEIR ordinal
    // while the tag this reader stores is MY ordinal, and the flavour is
    // neither number.
    {
        tblut2::UtRoot back;
        tblut2::TableReport r;
        std::vector<tblut2::TableFixedEntry> plan( 1024 );
        check( tblut2::UtRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "two lanes, compiled: the record reads" );
        check( back.pick.type == tblut2::PickType::B, "two lanes, compiled: the arm is remapped by NAME, not by ordinal" );
        check( back.pick.b.label_length == 7 && std::strcmp( back.pick.b.label, "seven77" ) == 0,
               "two lanes, compiled: the arm's string(8), whole" );
        check( back.pick.b.m == 1234 && back.head == 42 && back.tail == 99, "two lanes, compiled: the rest of the record" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "two lanes, compiled: a clean read moves no counter" );
    }

    // and the other direction: UT1 reads a UT2 record whose arm is `b`, which
    // in that record is ordinal 3.
    {
        tblut2::UtRoot two;
        tblut2::UtRootReset( two );
        two.head = 7;
        two.tail = 8;
        two.pick.type = tblut2::PickType::B;
        std::strcpy( two.pick.b.label, "third" );
        two.pick.b.label_length = 5;
        two.pick.b.m = 555;
        std::vector<uint8_t> w2( (size_t) tblut2::UtRootFixedMeasure( 1 ) );
        check( tblut2::UtRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "UT2 save" );

        tblut1::UtRoot back;
        tblut1::TableReport r;
        std::vector<tblut1::TableFixedEntry> plan( 1024 );
        check( tblut1::UtRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "two lanes, back: the record reads" );
        check( back.pick.type == tblut1::PickType::B, "two lanes, back: arm 3 lands as arm 2, by name" );
        check( back.pick.b.label_length == 5 && std::strcmp( back.pick.b.label, "third" ) == 0,
               "two lanes, back: the arm's string(8), whole" );
        check( back.pick.b.m == 555, "two lanes, back: the arm's other field" );
    }
}

// THE PLAIN NARROW INTEGER KINDS SPELLED AS THEMSELVES (GAP-1). The C++ ABI
// pads an int8 in front of an int16; the wire does not. C is 1+2+1+2+4+4+4+4
// = 22, and a port that stored the record as the struct would spend 24.

static void nk_case()
{
    check( tblnk1::NarrowFixedBodyBytes == 1 + 2 + 1 + 2 + 4 + 4 + 1 + 1,
           "NARROW: C is the declared storage width, nothing padded between fields" );

    tblnk1::Narrow one;
    FillNk1( one );
    std::vector<uint8_t> f( (size_t) tblnk1::NarrowFixedMeasure( 1 ) );
    check( tblnk1::NarrowFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "NK1 save" );

    {
        tblnk1::Narrow back;
        tblnk1::TableReport r;
        std::vector<tblnk1::TableFixedEntry> plan( 1024 );
        check( tblnk1::NarrowFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "NARROW/identity: one record" );
        check( back.i8 == (int8_t) -128 && back.i16 == (int16_t) 32767 && back.u8 == 255u && back.u16 == 1u,
               "NARROW/identity: int8, int16, uint8, uint16 spelled as themselves" );
        check( back.i32 == -1 && back.u32 == 0xFFFFFFFFu && back.gone == (int8_t) 42 && back.after == (int8_t) 9,
               "NARROW/identity: the 32-bit kinds beside them, and the field behind" );
        check( r.unknown == 0 && !r.malformed && !r.refused, "NARROW/identity: a clean read moves no counter" );
    }
    {
        tblnk2::Narrow back;
        tblnk2::TableReport r;
        std::vector<tblnk2::TableFixedEntry> plan( 1024 );
        check( tblnk2::NarrowFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "NARROW/compiled/older writer: one record" );
        check( back.i8 == (int8_t) -128 && back.u16 == 1u && back.u32 == 0xFFFFFFFFu && back.after == (int8_t) 9,
               "NARROW/compiled/older writer: every kind this side still has, lands" );
        check( back.extra == (int8_t) 11, "NARROW/compiled/older writer: the appended field takes its declared default" );
        check( r.unknown == 1 && !r.malformed && !r.refused,
               "NARROW/compiled/older writer: `gone` is the one name this reader has not got" );
    }
    {
        tblnk2::Narrow two;
        FillNk2( two );
        std::vector<uint8_t> f2( (size_t) tblnk2::NarrowFixedMeasure( 1 ) );
        check( tblnk2::NarrowFixedSave( &two, 1, f2.data(), (int64_t) f2.size() ) == (int64_t) f2.size(), "NK2 save" );
        tblnk1::Narrow back;
        tblnk1::TableReport r;
        std::vector<tblnk1::TableFixedEntry> plan( 1024 );
        check( tblnk1::NarrowFixedLoad( &back, 1, f2.data(), (int64_t) f2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "NARROW/compiled/newer writer: one record" );
        check( back.i8 == (int8_t) 127 && back.i16 == (int16_t) -32768 && back.u8 == 1u && back.after == (int8_t) 6,
               "NARROW/compiled/newer writer: the kinds both sides share" );
        check( back.gone == (int8_t) 9, "NARROW/compiled/newer writer: a field the writer does not carry takes its declared default" );
        check( r.unknown == 1 && !r.malformed && !r.refused,
               "NARROW/compiled/newer writer: `extra` is the one name this reader has not got" );
    }
}

// A COMPRESSED FLOAT RIDES AS THE IEEE FLOAT, NOT AS A QUANTIZED INDEX (GAP-3).
// 2.5 on a [0, 10] @ 0.01 grid is integer 250 on the packet wire; here it is
// four IEEE bytes. 1.234 is off the grid: a quantizing port would snap it.

static void cf_case()
{
    check( tblfc1::ProbeFixedBodyBytes == 4 + 4 + 4 + 4,
           "COMPRESSED: C is 4, the float, not the quantized index's width" );

    tblfc1::Probe one;
    FillFc1( one );
    std::vector<uint8_t> f( (size_t) tblfc1::ProbeFixedMeasure( 1 ) );
    check( tblfc1::ProbeFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "FC1 save" );

    {
        tblfc1::Probe back;
        tblfc1::TableReport r;
        std::vector<tblfc1::TableFixedEntry> plan( 1024 );
        check( tblfc1::ProbeFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "COMPRESSED/identity: one record" );
        check( bits_of_float( back.plain ) == bits_of_float( 3.5f ),
               "COMPRESSED/identity: the plain float32 beside it is IEEE" );
        check( bits_of_float( back.on_grid ) == bits_of_float( 2.5f ),
               "COMPRESSED/identity: an ON-GRID value rides as the float, not as integer 250" );
        check( bits_of_float( back.off_grid ) == bits_of_float( 1.234f ),
               "COMPRESSED/identity: an OFF-GRID value is not snapped to the 0.01 grid" );
        check( back.after == 9 && r.unknown == 0 && !r.malformed && !r.refused,
               "COMPRESSED/identity: the field behind, and a clean read" );
    }
    {
        tblfc2::Probe back;
        tblfc2::TableReport r;
        std::vector<tblfc2::TableFixedEntry> plan( 1024 );
        check( tblfc2::ProbeFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "COMPRESSED/compiled/older writer: one record" );
        check( bits_of_float( back.on_grid ) == bits_of_float( 2.5f ) &&
               bits_of_float( back.off_grid ) == bits_of_float( 1.234f ),
               "COMPRESSED/compiled/older writer: both compressed fields land as IEEE" );
        check( back.extra == 0.0f && back.tail == 3 && back.after == 9,
               "COMPRESSED/compiled/older writer: the appended fields take their declared defaults" );
        check( r.unknown == 0 && !r.malformed && !r.refused, "COMPRESSED/compiled/older writer: no damage" );
    }
    {
        tblfc2::Probe two;
        FillFc2( two );
        std::vector<uint8_t> f2( (size_t) tblfc2::ProbeFixedMeasure( 1 ) );
        check( tblfc2::ProbeFixedSave( &two, 1, f2.data(), (int64_t) f2.size() ) == (int64_t) f2.size(), "FC2 save" );
        tblfc1::Probe back;
        tblfc1::TableReport r;
        std::vector<tblfc1::TableFixedEntry> plan( 1024 );
        check( tblfc1::ProbeFixedLoad( &back, 1, f2.data(), (int64_t) f2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "COMPRESSED/compiled/newer writer: one record" );
        check( bits_of_float( back.plain ) == bits_of_float( -4.25f ) &&
               bits_of_float( back.on_grid ) == bits_of_float( 0.01f ) &&
               bits_of_float( back.off_grid ) == bits_of_float( 9.999f ) && back.after == 6,
               "COMPRESSED/compiled/newer writer: the fields this reader has, as IEEE" );
        check( r.unknown == 2 && !r.malformed && !r.refused,
               "COMPRESSED/compiled/newer writer: `extra` and `tail` are the two names this reader has not got" );
    }
}

// THE PLAN IS PARTITIONED AND ADJACENT COPIES ARE COALESCED (GAP-13, GAP-14).
// §3.4 calls the partition "a requirement and not an optimization". A broken
// partition is also a wrong value, which the value assertions catch; this is
// the assertion against the plan itself. Neighbours are never merged across
// the split.

static void check_plan_shape( const tblfn1::TableFixedEntry * plan, int32_t count, int32_t guarded, const char * what )
{
    check( guarded >= 0 && guarded <= count, what );
    for ( int32_t i = 0; i < guarded; ++i )
    {
        check( plan[i].guard == tblfn1::kTableFixedNoGuard, what );
    }
    for ( int32_t i = guarded; i < count; ++i )
    {
        check( plan[i].guard != tblfn1::kTableFixedNoGuard, what );
    }
    for ( int32_t i = 0; i + 1 < count; ++i )
    {
        if ( i + 1 == guarded ) { continue; } // the split: coalescing never crosses it
        const tblfn1::TableFixedEntry & a = plan[i];
        const tblfn1::TableFixedEntry & b = plan[i + 1];
        const bool would_merge = a.op == tblfn1::kTableFixedCopy && b.op == tblfn1::kTableFixedCopy &&
                                 a.guard == b.guard && a.arg == b.arg &&
                                 a.src + a.size == b.src && a.dst + a.size == b.dst;
        check( !would_merge, what );
    }
}

static void plan_case()
{
    check( tblfn1::FnRootFixedPlanGuarded < tblfn1::FnRootFixedPlanCount && tblfn1::FnRootFixedPlanGuarded > 0,
           "PLAN/identity: FN1 has unguarded entries AND guarded ones, so the split is a real boundary" );
    check_plan_shape( tblfn1::FnRootFixedPlan, tblfn1::FnRootFixedPlanCount, tblfn1::FnRootFixedPlanGuarded,
                      "PLAN/identity: unguarded first, then the arms; adjacent copies coalesced inside each half" );

    tblfn1::TableFixedLayoutView parsed;
    tblfn1::TableMessageReason why = tblfn1::layout_malformed;
    check( tblfn1::TableFixedParseLayout( tblfn2::FnRootFixedLayout, tblfn2::FnRootFixedLayoutBytes, parsed, why ),
           "PLAN/compiled: FN2's layout parses as a peer" );
    std::vector<tblfn1::TableFixedEntry> plan( 4096 );
    int32_t guarded = 0;
    tblfn1::TableReport r;
    const int32_t made = tblfn1::TableFixedCompile( parsed, tblfn1::FnRootFixedLayout,
                                                    (int32_t) tblfn1::FnRootFixedLayoutBytes, tblfn1::FnRootFixedDst,
                                                    plan.data(), 4096, &guarded, &r );
    check( made > 0 && guarded >= 0 && guarded <= made,
           "PLAN/compiled: a plan compiled from FN2 for an FN1 reader" );
    check_plan_shape( plan.data(), made, guarded,
                      "PLAN/compiled/newer writer: partitioned and coalesced" );

    // THE OTHER DIRECTION. The runtime is the first header's (FN1's); FN2's
    // destination rows are the same struct, so they cast.
    tblfn1::TableFixedLayoutView parsed1;
    tblfn1::TableMessageReason why1 = tblfn1::layout_malformed;
    check( tblfn1::TableFixedParseLayout( tblfn1::FnRootFixedLayout, tblfn1::FnRootFixedLayoutBytes, parsed1, why1 ),
           "PLAN/compiled/older writer: FN1's layout parses" );
    std::vector<tblfn1::TableFixedEntry> plan2( 4096 );
    int32_t guarded2 = 0;
    tblfn1::TableReport r2;
    const int32_t made2 = tblfn1::TableFixedCompile( parsed1, tblfn2::FnRootFixedLayout,
                                                     (int32_t) tblfn2::FnRootFixedLayoutBytes,
                                                     reinterpret_cast<const tblfn1::TableFixedDst *>( tblfn2::FnRootFixedDst ),
                                                     plan2.data(), 4096, &guarded2, &r2 );
    check( made2 > 0 && guarded2 >= 0 && guarded2 <= made2,
           "PLAN/compiled/older writer: a plan compiled from FN1 for an FN2 reader" );
    check_plan_shape( plan2.data(), made2, guarded2,
                      "PLAN/compiled/older writer: partitioned and coalesced" );
}

// A KIND THAT MOVED, OFF RED-3'S WINDOW (RED-8). The compile path already
// reports kind_mismatch and pushes no copy when kinds differ
// (TableFixedCompileEntry). Scalars2's `angle` still comes back holding the
// writer's raw Q16.16 because it sits inside a 17..31-byte run the copy
// clobbers. This body is 12 bytes, so that branch is not in the plan.

static void km_case()
{
    check( tblkm1::ProbeFixedBodyBytes == 4 + 4 + 4,
           "KIND MOVED: KM1 is 12 bytes, outside the 17..31-byte run-copy window" );
    check( tblkm2::ProbeFixedBodyBytes == 4 + 4 + 4 + 1,
           "KIND MOVED: KM2 is 13 bytes, still outside that window" );

    tblkm1::Probe one;
    FillKm1( one );
    std::vector<uint8_t> f( (size_t) tblkm1::ProbeFixedMeasure( 1 ) );
    check( tblkm1::ProbeFixedSave( &one, 1, f.data(), (int64_t) f.size() ) == (int64_t) f.size(), "KM1 save" );

    {
        tblkm1::Probe back;
        tblkm1::TableReport r;
        std::vector<tblkm1::TableFixedEntry> plan( 1024 );
        check( tblkm1::ProbeFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "KIND MOVED/identity: one record" );
        check( back.keep == 424 && back.angle == 45 * 65536 && back.tail == 99,
               "KIND MOVED/identity: keep, the Q16.16 angle, and tail" );
        check( r.kind_mismatch == 0 && !r.malformed && !r.refused,
               "KIND MOVED/identity: a clean read moves no counter" );
    }
    {
        tblkm2::Probe back;
        tblkm2::TableReport r;
        std::vector<tblkm2::TableFixedEntry> plan( 1024 );
        check( tblkm2::ProbeFixedLoad( &back, 1, f.data(), (int64_t) f.size(), plan.data(), 1024, NULL, &r ) == 1,
               "KIND MOVED/compiled/older writer: one record" );
        check( back.keep == 424 && back.tail == 99,
               "KIND MOVED/compiled/older writer: the unmoved fields land" );
        check( back.extra == (int8_t) 11,
               "KIND MOVED/compiled/older writer: the appended field takes its declared default" );
        check( r.kind_mismatch == 1,
               "KIND MOVED/compiled/older writer: `angle` respelled is one kind that moved" );
        // §4: skipped, NEVER MISDECODED, counted. The declared default is 0.
        // If this comes back as 45 * 65536, the misdecode is independent of
        // RED-3 and belongs on this row as well as on Scalars.
        check( back.angle == 0,
               "KIND MOVED/compiled/older writer: a kind that moved leaves the declared default, never the raw scale" );
        check( !r.malformed && !r.refused, "KIND MOVED/compiled/older writer: no damage" );
    }
    {
        tblkm2::Probe two;
        FillKm2( two );
        std::vector<uint8_t> f2( (size_t) tblkm2::ProbeFixedMeasure( 1 ) );
        check( tblkm2::ProbeFixedSave( &two, 1, f2.data(), (int64_t) f2.size() ) == (int64_t) f2.size(), "KM2 save" );
        tblkm1::Probe back;
        tblkm1::TableReport r;
        std::vector<tblkm1::TableFixedEntry> plan( 1024 );
        check( tblkm1::ProbeFixedLoad( &back, 1, f2.data(), (int64_t) f2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "KIND MOVED/compiled/newer writer: one record" );
        check( back.keep == 515 && back.tail == 88,
               "KIND MOVED/compiled/newer writer: the unmoved fields land" );
        check( r.kind_mismatch == 1 && r.unknown == 1,
               "KIND MOVED/compiled/newer writer: `angle` moved and `extra` is a name this reader has not got" );
        check( back.angle == 0,
               "KIND MOVED/compiled/newer writer: a kind that moved leaves the declared default" );
        check( !r.malformed && !r.refused, "KIND MOVED/compiled/newer writer: no damage" );
    }
}

// A `was =` CHAIN THAT KEEPS THE FIRST WIRE NAME (docs/SPEC-TABLES.md §5,
// docs/USAGE.md). WasName is a single name; that is how a chain is spelled,
// not a missing list. WC1 `label` -> WC2 `caption | was = "label"` -> WC3
// `title | was = "label"`. A first-generation record resolves at the third.

static void wc_case()
{
    check( tblwc1::RootFixedBodyBytes == 4 + 4 + 4,
           "WAS CHAIN: WC1 is 12 bytes" );
    check( tblwc2::RootFixedBodyBytes == 4 + 4 + 4 + 1,
           "WAS CHAIN: WC2 is 13 bytes" );
    check( tblwc3::RootFixedBodyBytes == 4 + 4 + 4 + 1 + 1,
           "WAS CHAIN: WC3 is 14 bytes, still outside the 17..31-byte run-copy window" );

    tblwc1::Root one;
    FillWc1( one );
    std::vector<uint8_t> f1( (size_t) tblwc1::RootFixedMeasure( 1 ) );
    check( tblwc1::RootFixedSave( &one, 1, f1.data(), (int64_t) f1.size() ) == (int64_t) f1.size(), "WC1 save" );

    {
        tblwc1::Root back;
        tblwc1::TableReport r;
        std::vector<tblwc1::TableFixedEntry> plan( 1024 );
        check( tblwc1::RootFixedLoad( &back, 1, f1.data(), (int64_t) f1.size(), plan.data(), 1024, NULL, &r ) == 1,
               "WAS CHAIN/identity: one record" );
        check( back.keep == 4242u && back.label == 321 && back.after == 7u,
               "WAS CHAIN/identity: keep, label, after" );
        check( r.unknown == 0 && !r.malformed && !r.refused, "WAS CHAIN/identity: a clean read" );
    }
    {
        tblwc2::Root back;
        tblwc2::TableReport r;
        std::vector<tblwc2::TableFixedEntry> plan( 1024 );
        check( tblwc2::RootFixedLoad( &back, 1, f1.data(), (int64_t) f1.size(), plan.data(), 1024, NULL, &r ) == 1,
               "WAS CHAIN/one hop: one record" );
        check( back.caption == 321 && back.keep == 4242u && back.after == 7u,
               "WAS CHAIN/one hop: WC2 `caption | was = \"label\"` reads WC1" );
        check( back.extra == (int8_t) 11 && r.unknown == 0 && !r.malformed && !r.refused,
               "WAS CHAIN/one hop: extra defaults, no damage" );
    }
    {
        // THE CHAIN. WC3's `title` still says `was = "label"`, the first name.
        tblwc3::Root back;
        tblwc3::TableReport r;
        std::vector<tblwc3::TableFixedEntry> plan( 1024 );
        check( tblwc3::RootFixedLoad( &back, 1, f1.data(), (int64_t) f1.size(), plan.data(), 1024, NULL, &r ) == 1,
               "WAS CHAIN/two hops: one record" );
        check( back.title == 321 && back.keep == 4242u && back.after == 7u,
               "WAS CHAIN/two hops: WC3 `title | was = \"label\"` reads a WC1 record" );
        check( back.extra == (int8_t) 11 && back.more == (int8_t) 13,
               "WAS CHAIN/two hops: fields WC1 does not carry take their declared defaults" );
        check( r.unknown == 0 && !r.malformed && !r.refused,
               "WAS CHAIN/two hops: the first name still resolves, no damage" );
    }

    tblwc2::Root two;
    FillWc2( two );
    std::vector<uint8_t> f2( (size_t) tblwc2::RootFixedMeasure( 1 ) );
    check( tblwc2::RootFixedSave( &two, 1, f2.data(), (int64_t) f2.size() ) == (int64_t) f2.size(), "WC2 save" );
    {
        tblwc3::Root back;
        tblwc3::TableReport r;
        std::vector<tblwc3::TableFixedEntry> plan( 1024 );
        check( tblwc3::RootFixedLoad( &back, 1, f2.data(), (int64_t) f2.size(), plan.data(), 1024, NULL, &r ) == 1,
               "WAS CHAIN/middle to last: one record" );
        check( back.title == 808 && back.extra == (int8_t) 42 && back.keep == 5150u,
               "WAS CHAIN/middle to last: WC2 writes the first name's hash, WC3 reads it" );
        check( back.more == (int8_t) 13 && r.unknown == 0 && !r.malformed && !r.refused,
               "WAS CHAIN/middle to last: more defaults, no damage" );
    }

    tblwc3::Root three;
    FillWc3( three );
    std::vector<uint8_t> f3( (size_t) tblwc3::RootFixedMeasure( 1 ) );
    check( tblwc3::RootFixedSave( &three, 1, f3.data(), (int64_t) f3.size() ) == (int64_t) f3.size(), "WC3 save" );
    {
        tblwc1::Root back;
        tblwc1::TableReport r;
        std::vector<tblwc1::TableFixedEntry> plan( 1024 );
        check( tblwc1::RootFixedLoad( &back, 1, f3.data(), (int64_t) f3.size(), plan.data(), 1024, NULL, &r ) == 1,
               "WAS CHAIN/newer writer: one record" );
        check( back.label == 909 && back.keep == 606u && back.after == 5u,
               "WAS CHAIN/newer writer: the first name reads the other way too" );
        check( r.unknown == 2 && !r.malformed && !r.refused,
               "WAS CHAIN/newer writer: `extra` and `more` are the two names this reader has not got" );
    }
}



// THE BOUNDS THE READ LOOP DOES NOT HOLD (docs/SPEC-TABLES.md §3.4). A fixed
// record is a positional image and the one read loop moves bytes: it asks
// nothing about what they mean. Two things a declaration bounds are therefore
// not the loop's at all — a RANGED SCALAR's declared min and max, and an
// ORDINAL's set: a union tag past the arm count, an enum ordinal past the
// enum's top value.
//
// They are held by STRAIGHT-LINE CODE in the generated decode, after the copy,
// and never by plan entries: an entry per bounded field is a test on every read
// of every record, which is the cost the identity plan exists to avoid. The
// pass runs over STORAGE, so ONE pass covers the identity plan and a plan
// compiled from a stranger's layout alike.
//
// THE POISON IS WRITTEN THROUGH THE WRITER, not poked into the bytes: the write
// side's own bounds are debug-only by rule and a range is not one of them, so a
// caller CAN put an out-of-range value on the wire and a reader is what has to
// answer for it.
static void bounds_case()
{
    // 1. A RANGED SCALAR, both ends, on the identity path.
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 1u;
    one.renamed = 5000;  // declared | min = 0, max = 1000
    one.gone = -7;       // and the low end of the same declaration
    one.nested.a = 111;
    one.nested.b = 222;
    std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "bounds: FX1 save" );
    {
        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "bounds: the record reads" );
        check( back.renamed == 1000, "RANGE: a value past max lands at max" );
        check( back.gone == 0, "RANGE: a value under min lands at min" );
        check( r.clamped == 2, "RANGE: two clamps, counted" );
        check( back.nested.a == 111 && back.nested.b == 222, "RANGE: an in-range neighbour is untouched" );
    }

    // THE NEGATIVE CONTROL: the read loop ALONE, with no straight-line pass
    // after it. The same record, the same plan, and the out-of-range value
    // survives — which is what says the bound is held by the pass and not by
    // something else that would have caught it anyway.
    {
        tblfx1::FxRoot loose;
        tblfx1::FxRootReset( loose );
        tblfx1::TableReport r;
        const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
        tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                               body, (uint8_t *) &loose, &r );
        check( loose.renamed == 5000 && loose.gone == -7,
               "NEGATIVE CONTROL: the loop alone really does leave an out-of-range value standing" );
        check( r.clamped == 0, "NEGATIVE CONTROL: and counts nothing" );
    }

    // 1b. A COMPILED PLAN FROM THIS BUILD'S OWN LAYOUT. The identity plan
    // copies; a compiled plan clamps. The loop alone, without the storage
    // pass, must already hold the range — which is what says the clamp is a
    // plan entry on this path, and only on this path.
    {
        tblfx1::TableFixedLayoutView parsed;
        tblfx1::TableMessageReason why = tblfx1::layout_malformed;
        check( tblfx1::TableFixedParseLayout( tblfx1::FxRootFixedLayout, tblfx1::FxRootFixedLayoutBytes, parsed, why ),
               "bounds, compiled-own: this build's layout parses" );
        std::vector<tblfx1::TableFixedEntry> compiled( 1024 );
        int32_t guarded = 0;
        tblfx1::TableReport cr;
        const int32_t made = tblfx1::TableFixedCompile( parsed, tblfx1::FxRootFixedLayout, (int32_t) tblfx1::FxRootFixedLayoutBytes,
                                                        tblfx1::FxRootFixedDst, compiled.data(), 1024, &guarded, &cr );
        check( made > 0, "bounds, compiled-own: the plan compiles" );
        int32_t clamp_ops = 0;
        for ( int32_t i = 0; i < made; ++i )
        {
            if ( compiled[(size_t) i].op == tblfx1::kTableFixedClamp ) { clamp_ops++; }
        }
        check( clamp_ops >= 2, "bounds, compiled-own: ranged scalars are clamp ops" );

        tblfx1::FxRoot held;
        tblfx1::FxRootReset( held );
        tblfx1::TableReport r;
        const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
        tblfx1::TableFixedRun( compiled.data(), made, guarded, body, (uint8_t *) &held, &r );
        check( held.renamed == 1000 && held.gone == 0,
               "COMPILED CLAMP: the loop alone holds a ranged integer" );
        check( r.clamped == 2, "COMPILED CLAMP: and counts the same two" );
        check( held.nested.a == 111 && held.nested.b == 222, "COMPILED CLAMP: an in-range neighbour is untouched" );
    }

    // 2. THE SAME NUMBERS THROUGH A COMPILED PLAN. FX2 reads the same record
    // through a plan compiled from FX1's layout, and the bound is the same one.
    {
        tblfx2::FxRoot back;
        tblfx2::TableReport r;
        std::vector<tblfx2::TableFixedEntry> plan( 2048 );
        check( tblfx2::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 2048, NULL, &r ) == 1,
               "bounds, compiled: the record reads" );
        check( back.renamed_to == 1000, "RANGE, compiled: the same clamp through a compiled plan" );
        check( r.clamped == 1, "RANGE, compiled: one clamp — `gone` is a field FX2 cannot name" );
    }

    // 3. A UNION TAG PAST THE ARM COUNT lands None and counts.
    {
        tblut1::UtRoot v;
        tblut1::UtRootReset( v );
        v.head = 1;
        v.tail = 2;
        v.pick.type = (tblut1::PickType) 7; // UT1 declares two arms
        std::vector<uint8_t> uw( (size_t) tblut1::UtRootFixedMeasure( 1 ) );
        check( tblut1::UtRootFixedSave( &v, 1, uw.data(), (int64_t) uw.size() ) == (int64_t) uw.size(), "bounds: UT1 save" );

        tblut1::UtRoot back;
        tblut1::TableReport r;
        std::vector<tblut1::TableFixedEntry> plan( 1024 );
        check( tblut1::UtRootFixedLoad( &back, 1, uw.data(), (int64_t) uw.size(), plan.data(), 1024, NULL, &r ) == 1,
               "bounds: the union record reads" );
        check( back.pick.type == tblut1::PickType::None, "ORDINAL: a tag past the arm count lands None" );
        check( r.clamped == 1, "ORDINAL: and counts as a clamp" );
        check( back.head == 1 && back.tail == 2, "ORDINAL: the rest of the record stands" );
    }

    // 4. AN ENUM ORDINAL PAST THE ENUM'S TOP VALUE lands None and counts.
    {
        tblv1::Cfg v;
        tblv1::CfgReset( v );
        v.grade = (tblv1::Grade) 9; // V1's Grade tops out at Gold
        std::vector<uint8_t> vw( (size_t) tblv1::CfgFixedMeasure( 1 ) );
        check( tblv1::CfgFixedSave( &v, 1, vw.data(), (int64_t) vw.size() ) == (int64_t) vw.size(), "bounds: V1 save" );

        tblv1::Cfg back;
        tblv1::TableReport r;
        std::vector<tblv1::TableFixedEntry> plan( 8192 );
        check( tblv1::CfgFixedLoad( &back, 1, vw.data(), (int64_t) vw.size(), plan.data(), 8192, NULL, &r ) == 1,
               "bounds: the enum record reads" );
        check( back.grade == tblv1::Grade::None, "ORDINAL: an ordinal past the enum's top value lands None" );
        check( r.clamped == 1, "ORDINAL: and counts as a clamp" );
    }

    // 5. LIVE COUNT, NEVER SLACK. A counted array of ranged integers: the
    // compiled plan must not emit one clamp per declared slot (Go #852), and
    // the loop alone must leave a live out-of-range element standing so the
    // storage pass can count it. Identity and compiled-own then match.
    {
        tblv1::Cfg v;
        tblv1::CfgReset( v );
        v.a = 5000;           // | min = 0, max = 1000
        v.items_count = 1;
        v.items[0] = 300;     // | min = 0, max = 255
        std::vector<uint8_t> vw( (size_t) tblv1::CfgFixedMeasure( 1 ) );
        check( tblv1::CfgFixedSave( &v, 1, vw.data(), (int64_t) vw.size() ) == (int64_t) vw.size(), "live-count: V1 save" );

        tblv1::Cfg identity;
        tblv1::TableReport ir;
        std::vector<tblv1::TableFixedEntry> iplan( 8192 );
        check( tblv1::CfgFixedLoad( &identity, 1, vw.data(), (int64_t) vw.size(), iplan.data(), 8192, NULL, &ir ) == 1,
               "live-count: identity reads" );
        check( identity.a == 1000 && identity.items[0] == 255, "live-count, identity: both live values clamp" );
        check( ir.clamped == 2, "live-count, identity: two clamps, slack never" );

        tblv1::TableFixedLayoutView parsed;
        tblv1::TableMessageReason why = tblv1::layout_malformed;
        check( tblv1::TableFixedParseLayout( tblv1::CfgFixedLayout, tblv1::CfgFixedLayoutBytes, parsed, why ),
               "live-count: this build's layout parses" );
        std::vector<tblv1::TableFixedEntry> compiled( 8192 );
        int32_t guarded = 0;
        tblv1::TableReport cr;
        const int32_t made = tblv1::TableFixedCompile( parsed, tblv1::CfgFixedLayout, (int32_t) tblv1::CfgFixedLayoutBytes,
                                                       tblv1::CfgFixedDst, compiled.data(), 8192, &guarded, &cr );
        check( made > 0, "live-count: the plan compiles" );
        int32_t clamp_ops = 0;
        for ( int32_t i = 0; i < made; ++i )
        {
            if ( compiled[(size_t) i].op == tblv1::kTableFixedClamp ) { clamp_ops++; }
        }
        check( clamp_ops >= 1 && clamp_ops < 8,
               "live-count: compiled plan does not emit one clamp per counted-array slot" );

        tblv1::Cfg held;
        tblv1::CfgReset( held );
        tblv1::TableReport r;
        const uint8_t * body = vw.data() + tblv1::kTableFixedHeaderBytes + 4 + tblv1::CfgFixedLayoutBytes + 8;
        tblv1::TableFixedRun( compiled.data(), made, guarded, body, (uint8_t *) &held, &r );
        check( held.a == 1000, "live-count, compiled loop: the scalar clamp op fired" );
        check( held.items[0] == 300, "live-count, compiled loop: the live array element is not a plan clamp" );
        check( r.clamped == 1, "live-count, compiled loop: one clamp, the scalar's" );
        tblv1::CfgFixedClamp( held, &r );
        check( held.items[0] == 255, "live-count, compiled pass: the live element clamps after the copy" );
        check( r.clamped == 2, "live-count: identity and compiled count the same two" );
    }
}

// ---------------------------------------------------------------------------

// AN ABSENT OPTIONAL'S PAYLOAD IS THE TEMPLATE'S ZEROS (docs/SPEC-TABLES.md
// §3.4): the payload rides WHOLE whether or not it is present, and when the
// flag is 0 what rides is ZERO. It is one `if` in the writer, and what it buys
// is that a caller's untouched payload storage never reaches the wire — an
// absent optional is a hole in the record and not a window into the writer's
// memory.
//
// THE CONTROL IS THE STAIN, as it is for every other kind of slack: the payload
// storage is filled with a byte a clean record carries nowhere, the test proves
// the stain IS there, then proves the WIRE carries none of it, and then proves
// the SAME payload PRESENT does put those bytes on the wire — so the check is
// discriminating and not passing because the writer never wrote a payload at
// all.
static void absent_optional_case()
{
    tblp3::Chain v;
    tblp3::ChainReset( v );
    std::strcpy( v.name, "absent" );
    v.name_length = 6;
    v.link_present = false;
    v.link.value = 0x5A5A5A;
    std::memset( v.link.tag, 0x5A, sizeof( v.link.tag ) );
    v.link.tag_length = 5;
    check( (uint8_t) v.link.tag[0] == 0x5Au, "CONTROL: the absent payload really is stained in storage" );

    std::vector<uint8_t> w( (size_t) tblp3::ChainFixedMeasure( 1 ) );
    check( tblp3::ChainFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(), "absent optional: the record saves" );
    const uint8_t * body = w.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes + 8;
    const size_t body_bytes = (size_t) tblp3::ChainFixedBodyBytes;
    check( std::memchr( body, 0x5A, body_bytes ) == NULL,
           "ABSENT OPTIONAL: not one byte of the absent payload reached the wire" );

    // and the reader reads what the flag says, with the payload at its defaults
    {
        tblp3::Chain back;
        tblp3::TableReport r;
        std::vector<tblp3::TableFixedEntry> plan( 1024 );
        check( tblp3::ChainFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "absent optional: the record reads" );
        check( !back.link_present, "absent optional: the flag" );
        check( back.link.value == 0 && back.link.tag_length == 0, "absent optional: the payload reads as the wire's zeros" );
        check( r.clamped == 0 && !r.malformed && !r.refused, "absent optional: a clean read moves no counter" );
    }

    // THE DISCRIMINATING HALF: the same payload, PRESENT. Those bytes do reach
    // the wire, so the check above is about the flag and not about the writer
    // never having written a payload.
    {
        tblp3::Chain present = v;
        present.link_present = true;
        present.link.tag_length = 4;
        std::vector<uint8_t> pw( (size_t) tblp3::ChainFixedMeasure( 1 ) );
        check( tblp3::ChainFixedSave( &present, 1, pw.data(), (int64_t) pw.size() ) == (int64_t) pw.size(),
               "absent optional: the present twin saves" );
        const uint8_t * pbody = pw.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes + 8;
        check( std::memchr( pbody, 0x5A, (size_t) tblp3::ChainFixedBodyBytes ) != NULL,
               "NEGATIVE CONTROL: the SAME payload PRESENT really does reach the wire" );
    }
}

// ---------------------------------------------------------------------------

// THE ONE CONTENT RULE THE WIRE HAS (docs/SPEC-TABLES.md §3, §4), on this
// form's terms. A `string(N)`'s used bytes are well-formed UTF-8 with no zero
// among them — the same rule SPEC.md §4.7 puts on the packet wire, where the
// whole read refuses. HERE THE RECORD IS POSITIONAL, so the damage is one
// field's and the reader does not lose the others to it: THE FIELD READS ITS
// DECLARED DEFAULT, one `malformed` counts, and the rest of the record stands.
//
// The check runs over the USED LENGTH and over nothing else. The slack carries
// no meaning, and reading a whole declared bound to check bytes that mean
// nothing is the cost this form exists to avoid.
//
// THE POISON IS WRITTEN THROUGH THE WRITER, because it can be: the write side's
// bounds are the used length and the live count, both debug-only, and neither
// is a content rule. A caller CAN put a byte on this wire that is not text, and
// the reader is what has to answer for it.
static void text_content_case()
{
    struct Case { const char * what; const char * bytes; int32_t length; };
    const Case cases[] = {
        { "a lone 0xFF is no lead byte",            "\xFF",     1 },
        { "a truncated two-byte sequence",          "\xC3",     1 },
        { "a bare continuation byte",               "\x80",     1 },
        { "an overlong encoding of NUL",            "\xC0\x80", 2 },
        { "an INTERIOR NULL among the used bytes",  "a\0b",     3 },
    };
    for ( size_t k = 0; k < sizeof( cases ) / sizeof( cases[0] ); ++k )
    {
        tblfx1::FxRoot v;
        tblfx1::FxRootReset( v );
        v.keep = 1234u;
        v.nested.a = 7;
        std::memcpy( v.label, cases[k].bytes, (size_t) cases[k].length );
        v.label_length = cases[k].length;

        std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
        check( tblfx1::FxRootFixedSave( &v, 1, w.data(), (int64_t) w.size() ) == (int64_t) w.size(),
               "text content: the record saves" );

        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "text content: the record reads" );
        check( r.malformed, cases[k].what );
        check( back.label_length == 2 && std::strcmp( back.label, "fx" ) == 0,
               "TEXT CONTENT: the damaged field reads its DECLARED DEFAULT" );
        check( back.keep == 1234u && back.nested.a == 7,
               "TEXT CONTENT: and the rest of the record stands — the damage is one field's" );
        check( !r.refused, "TEXT CONTENT: damage is not a refusal" );

        // THE NEGATIVE CONTROL: the read loop alone, with no content pass after
        // it. The same record, the same plan, and the bytes that are not text
        // stand in the storage with nothing said.
        if ( k == 0 )
        {
            tblfx1::FxRoot loose;
            tblfx1::FxRootReset( loose );
            tblfx1::TableReport r2;
            const uint8_t * body = w.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes + 8;
            tblfx1::TableFixedRun( tblfx1::FxRootFixedPlan, tblfx1::FxRootFixedPlanCount, tblfx1::FxRootFixedPlanGuarded,
                                   body, (uint8_t *) &loose, &r2 );
            check( loose.label_length == 1 && (uint8_t) loose.label[0] == 0xFFu,
                   "NEGATIVE CONTROL: the loop alone really does leave a byte that is not text standing" );
            check( !r2.malformed, "NEGATIVE CONTROL: and says nothing about it" );
        }
    }

    // AND WELL-FORMED TEXT IS UNTOUCHED, which is what makes the cases above
    // discriminating: multi-byte UTF-8 inside the bound reads back whole.
    {
        tblfx1::FxRoot v;
        tblfx1::FxRootReset( v );
        std::memcpy( v.label, "\xC3\xA9t\xC3\xA9", 5 ); // "été"
        v.label_length = 5;
        std::vector<uint8_t> w( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
        tblfx1::FxRootFixedSave( &v, 1, w.data(), (int64_t) w.size() );

        tblfx1::FxRoot back;
        tblfx1::TableReport r;
        std::vector<tblfx1::TableFixedEntry> plan( 1024 );
        check( tblfx1::FxRootFixedLoad( &back, 1, w.data(), (int64_t) w.size(), plan.data(), 1024, NULL, &r ) == 1,
               "text content: the well-formed record reads" );
        check( back.label_length == 5 && std::memcmp( back.label, "\xC3\xA9t\xC3\xA9", 5 ) == 0,
               "TEXT CONTENT: well-formed multi-byte UTF-8 rides whole" );
        check( !r.malformed && r.clamped == 0, "TEXT CONTENT: and moves no counter" );
    }
}

// THE GUARD IS COMPARED AT THE TAG'S WIDTH (docs/SPEC-TABLES.md §3.4). A
// union tag can be two bytes, and comparing only the first of them fires
// arm 1 on a foreign tag of 0x0101 — an ordinal no arm names. This case is
// a hand-built plan so the one-byte compare and the whole-width compare
// meet the same record; no schema has to declare two hundred and fifty six
// arms for the width to exist.
static void guard_width_case()
{
    uint8_t src[4] = { 0x01, 0x01, 0xAA, 0x00 };
    uint8_t dst[4];
    tblfx1::TableFixedEntry plan[2] = {};
    tblfx1::TableReport r = {};

    plan[0].src = 0; plan[0].dst = 0; plan[0].size = 2; plan[0].aux = 0;
    plan[0].guard = tblfx1::kTableFixedNoGuard;
    plan[0].op = tblfx1::kTableFixedCopy;
    plan[0].arg = 0; plan[0].meta = 0; plan[0].dstsize = 0; plan[0].sign = 0;
    plan[0].argw = 1;

    plan[1].src = 2; plan[1].dst = 2; plan[1].size = 1; plan[1].aux = 0;
    plan[1].guard = 0;
    plan[1].op = tblfx1::kTableFixedCopy;
    plan[1].arg = 1; plan[1].meta = 0; plan[1].dstsize = 0; plan[1].sign = 0;
    plan[1].argw = 2;

    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, src, dst, &r );
    check( dst[2] == 0, "GUARD WIDTH: tag 0x0101 at width 2 does not take arm 1" );

    src[1] = 0x00;
    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, src, dst, &r );
    check( dst[2] == 0xAA, "GUARD WIDTH: tag 0x0001 at width 2 takes arm 1" );

    // NEGATIVE CONTROL — the bug itself, watched failing. argw planted at 1
    // is the old one-byte compare, and the SAME 0x0101 record then fires arm 1.
    src[1] = 0x01;
    plan[1].argw = 1;
    std::memset( dst, 0, sizeof( dst ) );
    std::memset( &r, 0, sizeof( r ) );
    tblfx1::TableFixedRun( plan, 2, 1, src, dst, &r );
    check( dst[2] == 0xAA, "NEGATIVE CONTROL: a one-byte compare really does fire arm 1 on 0x0101" );
}

// THE COMPILE IS PAID ONCE PER PEER, NOT ONCE PER RECORD (§3.4). Two loads of
// the same foreign layout compile once; a third load with a different hash
// compiles once more; identity never increments the counter.
static void cache_case()
{
    tblfx1::FxRoot one;
    tblfx1::FxRootReset( one );
    one.keep = 4242u;
    one.narrow = 40000u;
    one.renamed = 321;
    one.gone = 654;
    one.nested.a = 111;
    one.nested.b = 222;
    std::vector<uint8_t> w1( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &one, 1, w1.data(), (int64_t) w1.size() ) == (int64_t) w1.size(), "cache: FX1 save" );

    std::vector<tblfx2::TableFixedEntry> plan( 1024 );
    std::vector<tblfx2::TableFixedEntry> slab( (size_t) tblfx2::kTableFixedPlanCacheCapacity * 1024 );
    tblfx2::TableFixedPlanCache cache;
    tblfx2::TableFixedPlanCacheInit( cache, slab.data(), 1024 );

    tblfx2::FxRoot back;
    tblfx2::TableReport r;
    check( tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, &cache, &r ) == 1,
           "cache: first foreign load" );
    check( cache.compiles == 1, "cache: first foreign load compiled once" );
    check( cache.used == 1 && cache.slots[0].made == 1, "cache: the slot is marked made" );

    tblfx2::TableReport r2;
    check( tblfx2::FxRootFixedLoad( &back, 1, w1.data(), (int64_t) w1.size(), plan.data(), 1024, &cache, &r2 ) == 1,
           "cache: second foreign load" );
    check( cache.compiles == 1, "cache: two loads of the same layout compiled once" );

    std::vector<uint8_t> wB = w1;
    const uint32_t layout_bytes = tblfx2::TableFixedGet32( wB.data() + tblfx2::kTableFixedHeaderBytes );
    // the layout is a u32 count then the entries; flip a byte of entry 0's id
    wB[(size_t) tblfx2::kTableFixedHeaderBytes + 4 + 4] ^= 0x01;
    const uint64_t hashB = tblfx2::TableFixedHashOf( wB.data() + tblfx2::kTableFixedHeaderBytes + 4, layout_bytes );
    tblfx2::TableFixedPut64( wB.data() + tblfx2::kTableFixedHashAt, hashB );
    tblfx2::TableFixedPut64( wB.data() + tblfx2::kTableFixedHeaderBytes + 4 + layout_bytes, hashB );
    tblfx2::TableReport r3;
    check( tblfx2::FxRootFixedLoad( &back, 1, wB.data(), (int64_t) wB.size(), plan.data(), 1024, &cache, &r3 ) == 1,
           "cache: a different hash compiles once more" );
    check( cache.compiles == 2, "cache: the third load compiled once more" );
    check( cache.used == 2 && cache.slots[1].made == 1, "cache: the second slot is marked made" );

    tblfx2::FxRoot two;
    tblfx2::FxRootReset( two );
    two.keep = 1;
    std::vector<uint8_t> w2( (size_t) tblfx2::FxRootFixedMeasure( 1 ) );
    check( tblfx2::FxRootFixedSave( &two, 1, w2.data(), (int64_t) w2.size() ) == (int64_t) w2.size(), "cache: FX2 save" );
    tblfx2::TableReport r4;
    check( tblfx2::FxRootFixedLoad( &back, 1, w2.data(), (int64_t) w2.size(), plan.data(), 1024, &cache, &r4 ) == 1,
           "cache: identity load" );
    check( cache.compiles == 2, "cache: identity never increments the compile counter" );
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
    nk_case();
    cf_case();
    plan_case();
    km_case();
    wc_case();
    v_case();
    s_case();
    fl_case();
    text_case();
    frame_case();
    p_case();
    negative_control();
    slack_case();
    union_text_case();
    bytes_row_case();
    bounds_case();
    absent_optional_case();
    text_content_case();
    guard_width_case();
    cache_case();
    layout_validation();
    fuzz_case();
    failures += known_red_report();
    if ( failures != 0 ) { std::printf( "%d failure(s)\n", failures ); return 1; }
    std::printf( "fixed form: versioning conformance green\n" );
    return 0;
}
