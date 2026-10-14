// test/conformance/cpp/rows/OptionalScalarsValidData.cpp — the cpp leg's
// assertion of the cpp/optional-scalars cell: "Optional scalar and enum
// fields: valid-data write/read acceptance" (docs/roadmap.sexp
// optional-scalars/cpp/valid-data).
//
// LAW (the file ./repo holds is the law; this comment is the pointer):
// docs/FIXED-FORM-ALGORITHM.md:1682 (§7 proof 1) — "Read a file and save it
// back; the bytes must be identical" — over a record that carries a `?T`
// (contract §2.3; §3.4 record, ?T): an optional carries ONE PRESENT BYTE in
// front of a payload that rides WHOLE, and when the flag is 0 what rides is
// the template's ZEROS, never the caller's storage (§3.4's absent-optional
// zero, fix 13 of the same page). The roadmap's cited evidence
// (test/tables/fixedform_main.cpp:196) covers a PRESENT `?int32` under the
// V1->V2 COMPILED plan only; the remaining valid-data surface this file pins
// is the optional ENUM, the ABSENT flag with a STAINED payload in storage,
// PRESENT with default content (presence is not content), and the
// byte-identical re-save — all on the IDENTITY plan, the write/read path
// itself.
//
// THE UNIT: tblv1::Cfg (test/tables/V1.schema) is the conformance tree's one
// fixed table carrying an optional scalar (`tier ?int32`) and an optional
// enum (`mark ?Grade`) side by side. THE VECTOR IS THE GENERATED WRITER'S OWN
// OUTPUT — tblv1::CfgFixedSave, the production path, not a hand-derivation —
// and the reader is tblv1::CfgFixedLoad on the file's own hash, so the
// IDENTITY plan walks it. The call site in the tree is
// test/tables/fixedform_main.cpp:183-184 (v_case), which drives the same
// pair; the emitter is internal/codegen/cpptable/fixedform.go:255.
//
// THE WIRE OFFSETS ARE THE LAW'S NUMBERS, derived by walking Cfg's fields in
// declaration order at each field's wire bytes (§3.4 record): a int32 @0,
// b f32 @4, mode u8 @8, name string(32) len+units @9..44, inner f32 @45,
// items count+8x4 @49..84, grade u8 @85, grades count+4x1 @86..93,
// podium 3x1 @94, slots count+6x4 @97..124, tally 3x4 @125, effect tag+arm
// @137..141, bank 4xCell(16) @142..205, tokens 4x4 @206, ranks 4x1 @222,
// ledger 3x4 @226, extra ?Inner present+4 @238..242 — so the OPTIONAL SCALAR
// rides present @243 payload LE32 @244..247 and the OPTIONAL ENUM rides
// present @248 payload u8 @249, and the walk ends at 250, which the emitted
// tblv1::CfgFixedBodyBytes names and this file asserts. Grade's ordinals are
// None=0, Bronze=1, Gold=2 (V1.h: "variants dense from 1").
//
// THE ROW IS THE ONLY FILE THIS CARD TOUCHES. It is standalone and is not
// wired into the leg's runner (wiring rows/ into a leg's runner is its own
// card), so the RUN command names the include path the unit needs, exactly
// the -I the Makefile gives build/conformance-cpp for it (CONFORMANCE_INCLUDES,
// Makefile:4651); the generated fixed form is header-only, so no source list:
//
//   c++ -std=c++17 -Wall -Ibuild/tables-generated/v1 \
//       test/conformance/cpp/rows/OptionalScalarsValidData.cpp \
//       -o build/rows-cpp-OptionalScalarsValidData && \
//       ./build/rows-cpp-OptionalScalarsValidData
//
// NEGATIVE CONTROL (behavioural, compiles): in
// build/tables-generated/v1/V1Table.h move the ONE constant the law governs —
// the optional scalar's payload store `TableFixedPut32( b + 244, ... )` in
// CfgFixedWriteBody — to b + 245, and this test goes RED (the wire byte at
// 244 is no longer 0x4D and the read-back tier is no longer 77); restore and
// it is GREEN. Recorded in RESULT.md as control:.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "V1Table.h"

// ---- the assertion printer, one line per assertion --------------------------

static int g_failures = 0;

static void check( bool ok, const char * what )
{
    if ( ok ) { std::printf( "ok   %s\n", what ); }
    else      { std::printf( "FAIL %s\n", what ); ++g_failures; }
}

// the body of the file's first record: header, layout length, layout, then
// the record's own 8-byte hash — every piece named by the emitted constants
static const uint8_t * first_body( const std::vector<uint8_t> & file )
{
    return file.data() + tblv1::kTableFixedHeaderBytes + 4 + tblv1::CfgFixedLayoutBytes + 8;
}

// the wire positions the derivation above fixes, named once
static const int TIER_PRESENT_AT = 243;
static const int TIER_PAYLOAD_AT = 244; // LE32, 244..247
static const int MARK_PRESENT_AT = 248;
static const int MARK_PAYLOAD_AT = 249;

// one whole case: save ONE record, pin the two optional slots' WIRE BYTES,
// read it back on the identity plan, pin the landed values and the counters,
// and re-save byte-identical (the law's own sentence)
static void optional_case( bool tier_present, int32_t tier,
                           bool mark_present, tblv1::Grade mark,
                           uint8_t want_flag_tier, uint8_t want_tier_b0,
                           uint8_t want_flag_mark, uint8_t want_mark_byte,
                           bool read_tier_present, int32_t read_tier,
                           bool read_mark_present, tblv1::Grade read_mark,
                           const char * name )
{
    tblv1::Cfg value;
    tblv1::CfgReset( value );
    value.tier_present = tier_present;
    value.tier = tier;       // when absent this is the STAIN fix 13 is about:
    value.mark_present = mark_present;
    value.mark = mark;       // a caller's untouched storage must never ride

    std::vector<uint8_t> file( (size_t) tblv1::CfgFixedMeasure( 1 ) );
    const int64_t wrote = tblv1::CfgFixedSave( &value, 1, file.data(), (int64_t) file.size() );
    check( wrote == (int64_t) file.size(), name );

    // the emitted arithmetic this file's derivation comments on
    check( tblv1::CfgFixedBodyBytes == 250 && tblv1::CfgFixedRecordBytes == 8 + 250,
           "Cfg: the field walk ends at 250 (the emitted CfgFixedBodyBytes)" );
    check( file[0] == tblv1::kTableFixedForm, "the file opens on form 3" );

    const uint8_t * body = first_body( file );

    // THE WIRE BYTES THE LAW GOVERNS: the present byte, then the payload
    // riding whole — the template's zeros when the flag says absent
    check( body[TIER_PRESENT_AT] == want_flag_tier, "tier: the present byte rides at 243" );
    check( body[TIER_PAYLOAD_AT] == want_tier_b0 &&
           body[TIER_PAYLOAD_AT + 1] == 0 && body[TIER_PAYLOAD_AT + 2] == 0 && body[TIER_PAYLOAD_AT + 3] == 0,
           "tier: the payload rides whole at 244..247 — content when present, zeros when absent" );
    check( body[MARK_PRESENT_AT] == want_flag_mark, "mark: the present byte rides at 248" );
    check( body[MARK_PAYLOAD_AT] == want_mark_byte,
           "mark: the ordinal rides whole at 249 — content when present, zero when absent" );

    // THE READ ACCEPTANCE, on the identity plan (the file's hash is this
    // build's own, so LOAD never compiles a thing)
    tblv1::Cfg back;
    tblv1::CfgReset( back );
    tblv1::TableReport r;
    static tblv1::TableFixedEntry plan[4096];
    const int64_t n = tblv1::CfgFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan, 4096, NULL, &r );
    check( n == 1, "the identity read returns the record" );
    check( back.tier_present == read_tier_present && back.tier == read_tier,
           "tier: the flag and the value land — content when present, the declared default when absent" );
    check( back.mark_present == read_mark_present && back.mark == read_mark,
           "mark: the flag and the ordinal land — content when present, None when absent" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 && r.duplicate == 0,
           "a clean optional read moves no counter" );
    check( !r.refused && !r.malformed, "a clean optional read is not a refusal and not damage" );

    // THE LAW'S OWN SENTENCE: read a file and save it back; the bytes must
    // be identical — a byte a port encodes differently is a byte that does
    // not come back
    std::vector<uint8_t> again( (size_t) tblv1::CfgFixedMeasure( 1 ) );
    const int64_t rewrote = tblv1::CfgFixedSave( &back, 1, again.data(), (int64_t) again.size() );
    check( rewrote == wrote && std::memcmp( file.data(), again.data(), (size_t) wrote ) == 0,
           "read a file and save it back: the bytes are identical" );
}

int main( void )
{
    // PRESENT WITH CONTENT: the scalar at 77 (0x4D), the enum at Gold (2)
    optional_case( true, 77, true, tblv1::Grade::Gold,
                   1, 0x4D, 1, 2,
                   true, 77, true, tblv1::Grade::Gold,
                   "present: tier=77, mark=Gold" );

    // ABSENT WITH A STAINED PAYLOAD IN STORAGE: the flags say absent, the
    // storage says 77 and Gold — and not one bit of the stain may ride
    optional_case( false, 77, false, tblv1::Grade::Gold,
                   0, 0x00, 0, 0,
                   false, 0, false, tblv1::Grade::None,
                   "absent: a stained payload rides as the template's zeros and reads as the defaults" );

    // PRESENT WITH DEFAULT CONTENT: presence is not content — the flags ride
    // as 1 behind a payload that happens to equal the declared default, the
    // case a content-based elision would lose
    optional_case( true, 0, true, tblv1::Grade::None,
                   1, 0x00, 1, 0,
                   true, 0, true, tblv1::Grade::None,
                   "present and entirely default: the flags still ride" );

    if ( g_failures != 0 )
    {
        std::printf( "OptionalScalarsValidData: %d assertion(s) failed\n", g_failures );
        return 1;
    }
    std::printf( "OptionalScalarsValidData: all assertions passed\n" );
    return 0;
}
