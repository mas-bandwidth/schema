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
//
// THE LAYOUT is what form 1 called the vocabulary block. It is neither §7's
// cooked block nor §19's block form.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FX1Table.h"
#include "FX2Table.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P3Table.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
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
    v_case();
    p_case();
    negative_control();
    layout_validation();
    fuzz_case();
    if ( failures != 0 ) { std::printf( "%d failure(s)\n", failures ); return 1; }
    std::printf( "fixed form: versioning conformance green\n" );
    return 0;
}
