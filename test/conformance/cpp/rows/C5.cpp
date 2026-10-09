// C5 — WIDE TEXT CODE UNITS: the content rules apply to the USED units v,
// never to the full storage or to 2N (docs/FIXED-FORM-ALGORITHM.md:333
// "wide code units over v, never 2N, an astral pair counting two (fix 7)").
//
// The production path is wide::StampFixedLoad -> TableFixedRun ->
// TableFixedApply kTableFixedText -> StampFixedClamp -> StampFixedClampBody,
// which calls TableUtf16Valid(value.label, value.label_length) — that is
// the content rule applied over v (the used length in code units), not over
// the full storage (4 code units for wstring(4)). Code past the used length
// is not content-validated.
//
// THE VECTOR, built here: a one-record form-3 file written by the unit's
// own FixedSave, with a lone surrogate forged at a position PAST the used
// length v=2. The read succeeds because the content check only walks the
// first two code units. The control forges the same surrogate INSIDE the
// used range — the read then sets malformed and refuses the field.

#include <cstdio>
#include <cstdint>
#include <cstring>
#include <vector>

#include "CaptionTable.h" // wide (examples-wide): Stamp with label wstring(4)

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures++; }
}

static void wide_units_case()
{
    wide::Stamp v;
    wide::StampReset( v );
    v.label[0] = u'a';
    v.label[1] = u'b';
    v.label_length = 2;       // two code units USED
    v.seq = 0xC0FFEEu;

    std::vector<uint8_t> wire( (size_t) wide::StampFixedMeasure( 1 ) );
    check( wide::StampFixedSave( &v, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
           "wide units: the one-record form-3 file saves through the unit's own writer" );

    // record 0's body, then the label text payload starts at src+4 (past the
    // SLE(4) length word whose src is 0)
    const size_t body = (size_t) wide::kTableFixedHeaderBytes + 4
                      + (size_t) wide::StampFixedLayoutBytes + 8;
    uint8_t * const label_text = wire.data() + body + 4;   // the code-unit payload

    // CLEAN: two valid code units, the read succeeds and the neighbours stand
    {
        wide::Stamp back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        wide::TableReport r;
        std::vector<wide::TableFixedEntry> plan( 1024 );
        const int64_t n = wide::StampFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed,
               "wide units clean: the record with two code units reads whole, "
               "no malformed and no refusal" );
        check( r.clamped == 0 && r.widened == 0,
               "wide units clean: the two code units are in the cap — no counter moves" );
        check( back.label_length == 2 && back.label[0] == u'a' && back.label[1] == u'b',
               "wide units clean: the two used code units land" );
        check( back.label[2] == 0 && back.label[2] == u'\0',
               "wide units clean: terminated at the used length — label[2] is the "
               "read's own zero store" );
        check( back.seq == 0xC0FFEEu,
               "wide units clean: the neighbour field stands" );
    }

    // FORGE A LONE SURROGATE PAST THE USED LENGTH: the label carries 2 valid
    // code units at positions 0-1, and a lone high surrogate 0xD800 at
    // position 2 — but since v=2, the content check (TableUtf16Valid) never
    // walks position 2. The read SUCCEEDS and the field is not damaged.
    label_text[4] = 0x00; label_text[5] = 0xD8; // surrogate at code-unit position 2, LE
    {
        wide::Stamp back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        wide::TableReport r;
        std::vector<wide::TableFixedEntry> plan( 1024 );
        const int64_t n = wide::StampFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && !r.malformed,
               "wide units overhang: a surrogate past the used length v=2 is "
               "not content the check walks — the read succeeds, malformed is false (C5)" );
        check( back.label_length == 2 && back.label[0] == u'a' && back.label[1] == u'b',
               "wide units overhang: the two used code units still land at the wire's values" );
        check( back.seq == 0xC0FFEEu,
               "wide units overhang: the neighbour field stands" );
    }

    // RESTORE THE CLEAN TEXT AT POSITION 2
    label_text[4] = 0xC1; label_text[5] = 0xAB; // poison value

    // CONTROL — FORGE THE SURROGATE INSIDE THE USED RANGE: the label carries
    // a lone surrogate at position 1 (still v=2). The content check walks
    // it, finds invalid UTF-16, and sets malformed. The field is refused and
    // the record stands.
    label_text[2] = 0x00; label_text[3] = 0xD8; // surrogate at code-unit position 1, LE
    {
        wide::Stamp back;
        std::memset( (void *) &back, 0xAB, sizeof( back ) );
        wide::TableReport r;
        std::vector<wide::TableFixedEntry> plan( 1024 );
        const int64_t n = wide::StampFixedLoad(
            &back, 1, wire.data(), (int64_t) wire.size(), plan.data(), 1024, NULL, &r );
        check( n == 1 && !r.refused && r.malformed,
               "wide units inside: a surrogate INSIDE the used length v=2 IS "
               "content the walk checks — the read sets malformed (C5 control)" );
        check( back.label_length == 0,
               "wide units inside: the damaged field reads its declared default — "
               "used length is zero" );
        check( back.label[0] == 0,
               "wide units inside: the damaged field's text storage is zeroed" );
        check( back.seq == 0xC0FFEEu,
               "wide units inside: the neighbour field still stands through the damage" );
    }
}

int main()
{
    wide_units_case();
    if ( failures != 0 )
    {
        std::printf( "C5: %d assertion(s) failed\n", failures );
        return 1;
    }
    std::printf( "C5: wide text code units — content validated over v, never 2N, "
                 "on the cpp leg\n" );
    return 0;
}