// fixedform_fixtures.h — THE VALUES THE FIXED FORM'S CONFORMANCE SET IS MADE OF,
// in ONE place (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-COVERAGE.md).
//
// TWO PROGRAMS READ THIS FILE AND THAT IS THE WHOLE REASON IT EXISTS.
// `fixedform_main.cpp` asserts what a READER makes of these values, and
// `fixedform_dump.cpp` writes the ORACLE BYTES a WRITER must produce from them.
// A fixture built twice is a fixture that can disagree with itself: the oracle
// would pin bytes for a value the test never reads, and the test would assert a
// read of a value no oracle covers, and neither gate would say so.
//
// So: every builder here is a `Fill*` function taking the value by reference,
// every one of them Resets first, and NOTHING in here reads or writes a file.
// A case that needs bytes poked onto a written record pokes them at its own end
// — a poke is damage, and damage is not a fixture.

#ifndef SCHEMA_TEST_FIXEDFORM_FIXTURES_H
#define SCHEMA_TEST_FIXEDFORM_FIXTURES_H

#include <cstdint>
#include <cstring>
#include <new>

#include "FX1Table.h"
#include "FX2Table.h"
#include "FU1Table.h"
#include "FU2Table.h"
#include "FN1Table.h"
#include "FN2Table.h"
#include "V1Table.h"
#include "P1Table.h"
#include "ScalarsTable.h"
#include "F1Table.h"
#include "CaptionTable.h"
#include "W1Table.h"
#include "W2Table.h"
#include "RangesTable.h"
#include "NK1Table.h"
#include "NK2Table.h"
#include "FC1Table.h"
#include "FC2Table.h"
#include "RW2Table.h"
#include "floatnan.h"

// ---------------------------------------------------------------------------
// SELECTING A UNION ARM, THE WAY THE GENERATED HEADER SAYS TO.
//
// A C++ union arm's storage is INDETERMINATE until the arm is constructed —
// the generated header says so in as many words — and on this form that is not
// a C++ nicety, it is a WIRE fact. §3.4's writer is "the type's constant bytes
// memcpy'd and then value stores at constant offsets", and a text field's store
// is `memcpy( b, value.w, 2N )`: the WHOLE DECLARED CAPACITY, not the used
// units. So an arm selected by assigning the tag and filling one field rides
// its own uninitialised bytes onto the wire, in the slack §3.4 requires a
// writer to ZERO.
//
// It is invisible on a round trip — a reader validates the used units only and
// never looks at the slack — and it is exactly what an ORACLE catches, because
// the first oracle written here pinned an indeterminate byte and two builds
// disagreed about it. That is the fixture's bug and not the codec's, and this
// is where it is fixed once for every fixture rather than remembered at each
// arm.
#define SELECT_ARM( holder, arm, Type, tag ) \
    do { ::new ( (void *) &( holder ).arm ) Type{}; ( holder ).type = ( tag ); } while ( 0 )

// ---------------------------------------------------------------------------
// FX1/FX2: the scalar edits (docs/SPEC-TABLES.md §3.4's versioning conformance)

// `label` and `marks` are left AT THEIR DECLARED DEFAULTS on purpose, and the
// oracle pins them so: a `string(N)` riding its non-empty declared default and
// a counted array at a count of ZERO, whose whole bound is slack. The corpus
// binary beside this one (test/tables/fixedform_pin.cpp) fills both, so the
// two ends of the same rows are pinned rather than one of them twice.
inline void FillFx1( tblfx1::FxRoot & v )
{
    tblfx1::FxRootReset( v );
    v.keep = 4242u;
    v.narrow = 60000u;   // AT THE TOP OF uint16: FX2 reads it as uint32, WIDENED
    v.renamed = 606;
    v.gone = 707;
    v.nested.a = 11;
    v.nested.b = 22;
}

inline void FillFx2( tblfx2::FxRoot & v )
{
    tblfx2::FxRootReset( v );
    v.keep = 5150u;
    v.narrow = 70000u;   // PAST uint16: a value only the wider generation holds
    v.renamed_to = 808;
    v.added = 909;
    v.nested.a = 33;
    v.nested.b = 44;
    v.extra.x = 55;
    v.extra.y = 66;
}

// ---------------------------------------------------------------------------
// FU1/FU2: TEXT OF EACH FLAVOUR AND A COUNTED ARRAY, UNDER A UNION ARM
//
// One arm per text flavour in ordinal order, so the arm's ORDINAL and the
// field's FLAVOUR disagree at every arm but the first — which is the whole
// point of the pair, the plan entry carrying both in one lane.

inline void FillFu1Wide( tblfu1::MarkRoot & r )
{
    tblfu1::MarkRootReset( r );
    r.id = 1001u;
    r.after = 9;
    SELECT_ARM( r.mark, wide, tblfu1::MarkWide, tblfu1::MarkType::Wide );
    // A CODE UNIT WITH A NON-ZERO HIGH BYTE, on purpose: a wide field read as
    // narrow puts its terminator at the length rather than at twice it, which
    // for ASCII-only content lands on a high byte that was already zero and
    // changes nothing. U+0142 is what makes the misplacement visible.
    r.mark.wide.w[0] = (char16_t) 0x0041;
    r.mark.wide.w[1] = (char16_t) 0x0142;
    r.mark.wide.w[2] = (char16_t) 0x0043;
    r.mark.wide.w_length = 3;
    r.mark.wide.n = 31;
}

inline void FillFu1Narrow( tblfu1::MarkRoot & r )
{
    tblfu1::MarkRootReset( r );
    r.id = 1002u;
    r.after = 9;
    SELECT_ARM( r.mark, narrow, tblfu1::MarkNarrow, tblfu1::MarkType::Narrow );
    // FIVE USED BYTES OF A string(6): past the three a WIDE reading of the same
    // bound would allow, which is what makes the flavour visible in the LENGTH
    // and not only in the terminator.
    std::strcpy( r.mark.narrow.s, "hello" );
    r.mark.narrow.s_length = 5;
    r.mark.narrow.n = 55;
}

inline void FillFu1Raw( tblfu1::MarkRoot & r )
{
    tblfu1::MarkRootReset( r );
    r.id = 1003u;
    r.after = 9;
    SELECT_ARM( r.mark, raw, tblfu1::MarkRaw, tblfu1::MarkType::Raw );
    r.mark.raw.d[0] = 0xDEu; r.mark.raw.d[1] = 0xADu;
    r.mark.raw.d[2] = 0xBEu; r.mark.raw.d[3] = 0xEFu;
    r.mark.raw.d_length = 4;  // USED == MAX: no slack behind it at all
    r.mark.raw.n = 77;
}

inline void FillFu1List( tblfu1::MarkRoot & r )
{
    tblfu1::MarkRootReset( r );
    r.id = 1004u;
    r.after = 9;
    SELECT_ARM( r.mark, list, tblfu1::MarkList, tblfu1::MarkType::List );
    r.mark.list.items[0] = 11;
    r.mark.list.items[1] = 22;
    r.mark.list.items_count = 2;   // AT MAX
    r.mark.list.n = 99;
}

// TAG 0 IS `None` AND IT IS NOT AN ARM: the whole arm extent is declared slack
// and a writer zero-fills it (§3.4's union row).
inline void FillFu1None( tblfu1::MarkRoot & r )
{
    tblfu1::MarkRootReset( r );
    r.id = 1005u;
    r.after = 4;
}

// AN ARM FU1 HAS NO NAME FOR.
inline void FillFu2Skip( tblfu2::MarkRoot & r )
{
    tblfu2::MarkRootReset( r );
    r.id = 2001u;
    r.after = 6;
    r.tail = 8;
    SELECT_ARM( r.mark, skip, tblfu2::MarkSkip, tblfu2::MarkType::Skip );
    r.mark.skip.e = 42;
}

inline void FillFu2List( tblfu2::MarkRoot & r )
{
    tblfu2::MarkRootReset( r );
    r.id = 2002u;
    r.after = 5;
    SELECT_ARM( r.mark, list, tblfu2::MarkList, tblfu2::MarkType::List );
    r.mark.list.items[0] = 71;
    r.mark.list.items[1] = 72;
    r.mark.list.items_count = 2;
    r.mark.list.n = 13;
}

inline void FillFu2Narrow( tblfu2::MarkRoot & r )
{
    tblfu2::MarkRootReset( r );
    r.id = 2003u;
    r.after = 5;
    r.tail = 8;
    SELECT_ARM( r.mark, narrow, tblfu2::MarkNarrow, tblfu2::MarkType::Narrow );
    std::strcpy( r.mark.narrow.s, "world" );
    r.mark.narrow.s_length = 5;
    r.mark.narrow.n = 66;
}

// A `bytes(N)` UNDER A NEWER WRITER (docs/FIXED-FORM-COVERAGE.md GAP-5): FU1
// reads this through a compiled plan. The raw arm slid from ordinal 3 to 4.
inline void FillFu2Raw( tblfu2::MarkRoot & r )
{
    tblfu2::MarkRootReset( r );
    r.id = 2004u;
    r.after = 5;
    r.tail = 8;
    SELECT_ARM( r.mark, raw, tblfu2::MarkRaw, tblfu2::MarkType::Raw );
    r.mark.raw.d[0] = 0xCAu; r.mark.raw.d[1] = 0xFEu;
    r.mark.raw.d[2] = 0xBAu; r.mark.raw.d[3] = 0xBEu;
    r.mark.raw.d_length = 4;
    r.mark.raw.n = 88;
}

// ---------------------------------------------------------------------------
// FN1/FN2: the bool, the optional, the enum, the ARRAY OF UNIONS, the
// THREE-DEEP nesting and the arm that holds an array

inline void FillFn1( tblfn1::FnRoot & v )
{
    tblfn1::FnRootReset( v );
    v.flag = true;
    v.tier = tblfn1::Tier::Gold;   // ordinal 2 here, ordinal 3 in FN2
    v.opt_present = true;
    v.opt.z = 111;
    v.picks_count = 2;                            // AT MAX
    SELECT_ARM( v.picks[0], a, tblfn1::CellA, tblfn1::PickType::A );  // the NARROW arm, in front of slack
    v.picks[0].a.n = 201;
    SELECT_ARM( v.picks[1], b, tblfn1::CellB, tblfn1::PickType::B );  // the WIDEST arm, the one with the array
    v.picks[1].b.pts[0] = 301;
    v.picks[1].b.pts[1] = 302;
    v.picks[1].b.m = 303;
    v.deep.x = 41;
    v.deep.mid.y = 42;
    v.deep.mid.deeper.z = 43;                     // THREE DEEP
    std::strcpy( v.label, "abcd" );
    v.label_length = 4;                           // USED == MAX
    v.after = 9;
}

// THE ABSENT OPTIONAL, and its payload left at the declared default: what a
// WRITER produces here is the present byte zero and the payload ZERO-FILLED
// (§3.4's slack rule), which is the byte picture the residue case damages.
inline void FillFn1Absent( tblfn1::FnRoot & v )
{
    FillFn1( v );
    v.opt_present = false;
    v.opt.z = 0;
}

inline void FillFn2( tblfn2::FnRoot & v )
{
    tblfn2::FnRootReset( v );
    v.flag = true;
    v.tier = tblfn2::Tier::Silver;  // A VARIANT FN1 HAS NO NAME FOR
    v.opt_present = true;
    v.opt.z = 5;
    v.opt.added = 6;
    v.picks_count = 2;
    SELECT_ARM( v.picks[0], c, tblfn2::CellC, tblfn2::PickType::C );  // AN ARM FN1 HAS NO NAME FOR
    v.picks[0].c.q = 77;
    SELECT_ARM( v.picks[1], b, tblfn2::CellB, tblfn2::PickType::B );  // ordinal 2 in FN1, ordinal 3 here
    v.picks[1].b.pts[0] = 8;
    v.picks[1].b.pts[1] = 9;
    v.picks[1].b.m = 10;
    v.deep.x = 1;
    v.deep.mid.y = 2;
    v.deep.mid.deeper.z = 3;
    v.deep.mid.deeper.added = 4;
    std::strcpy( v.caption, "wxyz" );
    v.caption_length = 4;
    v.after = 6;
    v.tail = 8;
}

// ---------------------------------------------------------------------------
// A TOP-LEVEL wstring(N) (docs/SPEC-TABLES.md §3.4's `wstring(N)` row).
//
// FLAVOUR 2 HAD NO ORACLE BYTES ANYWHERE. FU1's `wide` arm is the flavour under
// a union; this is the flavour AT THE ROOT, where the length is in CODE UNITS
// and the payload is two bytes each — the one row of the text family whose
// length and byte extent are different numbers, so a leg that treats the length
// as bytes writes a record half the size and no other fixture says so.

inline void FillWideStamp( wide::Stamp & v )
{
    wide::StampReset( v );
    v.label[0] = (char16_t) 0x0041;   // 'A', one byte's worth of a two-byte unit
    v.label[1] = (char16_t) 0x00E9;   // U+00E9, a high byte inside the BMP's low half
    v.label[2] = (char16_t) 0x4E2D;   // U+4E2D, both bytes non-zero
    v.label[3] = (char16_t) 0xD83D;   // A LONE SURROGATE: a code unit, not a code point
    v.label_length = 4;               // USED == MAX
    v.seq = 90210u;
}

// ---------------------------------------------------------------------------
// Scalars: the fixed-point family, the 128-bit integers and the containers

inline void FillScalars( scalardemo::SimState & v )
{
    scalardemo::SimStateReset( v );
    v.tilt = 0x70;                               // fixed(4, 4): Q4.4 raw, at max
    v.angle = 90 * 65536;                        // fixed(16, 16)
    v.position = (int64_t) 20000 * 65536;        // PAST Scalars2's own max of 1000
    v.reach = serialize::int128_t( 123456789 );
    v.ticks = 4242;                              // gone in Scalars2: one unknown
    v.ratio = 0xA5u;
    v.speed = 500u * 65536u;                     // PAST Scalars2's own max of 10
    v.span = 0x0000FFFFFFFFFFFFull;              // ufixed(48, 16) AT ITS FULL EXTENT
    v.mass = ( serialize::uint128_t( 7ull ) << 64 ) | serialize::uint128_t( 9ull );
    v.frames = 0xFFFFFFFFu;                      // ufixed(32, 0) at its full extent
    v.flux = ( serialize::int128_t( 1 ) << 100 ); // int128, a value only 128 bits hold
    v.energy = serialize::int128_t( -5000000000ll );
    v.entity_id = ( serialize::uint128_t( 0xDEADBEEFull ) << 64 ) | serialize::uint128_t( 0xFEEDFACEull );
    v.scale = 2 * 65536;
    v.samples[0] = 65536; v.samples[1] = -65536; v.samples[2] = 32768;
    v.weights[0] = 256u; v.weights[1] = 512u; v.weights[2] = 1024u; v.weights[3] = 2048u;
    v.weights_count = 4;                         // A COUNTED ARRAY AT MAX
    v.axes[scalardemo::Axis::X] = (int64_t) 1 << 40;
    v.axes[scalardemo::Axis::Y] = -( (int64_t) 1 << 40 );
    v.axes[scalardemo::Axis::Z] = 0;
    v.seeds[0] = serialize::uint128_t( 11u ); v.seeds[1] = serialize::uint128_t( 22u );
    v.seeds_count = 2;
    v.pose.x = 4 * 65536; v.pose.y = -4 * 65536; v.pose.heading = 90u * 65536u;
    v.spawn_present = true;                      // AN OPTIONAL OF A NESTED TYPE, PRESENT
    v.spawn.x = 65536; v.spawn.y = 65536; v.spawn.heading = 1u;
}

// ---------------------------------------------------------------------------
// F1: the IEEE-754 bit patterns (§3.4's float row: "with no canonicalisation")

inline void FillFloats( tblf1::Floats & v )
{
    tblf1::FloatsReset( v );
    build_golden_floats_nan( v );
}

// ---------------------------------------------------------------------------
// V1: the enum, the keyed array and the union arm

inline void FillV1( tblv1::Cfg & v )
{
    tblv1::CfgReset( v );
    v.a = 42;
    v.b = 3.5f;
    v.mode = tblv1::Mode::Alpha;
    std::strcpy( v.name, "vee" );
    v.name_length = 3;
    v.inner.factor = 7.5f;
    v.items[0] = 1; v.items[1] = 2; v.items[2] = 3;
    v.items_count = 3;
    v.grade = tblv1::Grade::Gold;   // ordinal 2 here, ordinal 3 in V2
    v.grades[0] = tblv1::Grade::Bronze;
    v.grades[1] = tblv1::Grade::Gold;
    v.grades_count = 2;
}

// ---------------------------------------------------------------------------
// P1: the text-content proving ground — a string(16) at three used extents

inline void FillP1( tblp1::Chain & v, const char * name, int32_t used )
{
    tblp1::ChainReset( v );
    std::strcpy( v.name, name );
    v.name_length = used;
    v.link.value = 500;
    std::strcpy( v.link.tag, "t" );
    v.link.tag_length = 1;
}

// ---------------------------------------------------------------------------
// W1/W2: `flags`, the DECLARED DEFAULTS of the text family, and a TABLE renamed
//
// §3.4's `flags` row is "8, the raw mask, as §3 carries it", and nothing in the
// set carried a `flags` field at all. Beside it W1 carries the other half of §4
// that only the text family has: a `string(N)`, a `bytes(N)` and a `flags` each
// with a NON-ZERO DECLARED DEFAULT, which is what an absent field of that kind
// must land on. And W2 renames the TABLE — `was = "Vessel"` — which is a rename
// at a level the field-level pairs cannot reach.

inline void FillW1( tblw1::Vessel & v )
{
    tblw1::VesselReset( v );
    std::strcpy( v.name, "endeavour" );
    v.name_length = 9;
    v.tag[0] = 0x7Fu; v.tag[1] = 0x80u; v.tag[2] = 0x00u; v.tag[3] = 0xFFu;
    v.tag_length = 4;                                  // USED == MAX, and a ZERO BYTE INSIDE IT:
                                                       // `bytes(N)` is not text and §3's content
                                                       // rules are not its business
    v.caps = (tblw1::Caps) ( tblw1::Caps_Crouch );     // OFF its declared default, on purpose
    std::strcpy( v.badge.label, "gold" );
    v.badge.label_length = 4;
    v.hull = 250;
}

// W2 written, W1 read: the COMPILED-NEWER column for `flags` (GAP-4). The
// table is renamed; the mask is the same kind. A flags field gaining a bit
// is not a wire event — this is the direction the existing pair had not run.
inline void FillW2( tblw2::Ship & v )
{
    tblw2::ShipReset( v );
    std::strcpy( v.name, "discovery" );
    v.name_length = 9;
    v.tag[0] = 0x11u; v.tag[1] = 0x22u; v.tag[2] = 0x33u; v.tag[3] = 0x44u;
    v.tag_length = 4;
    v.caps = (tblw2::Caps) ( tblw2::Caps_Jump );
    std::strcpy( v.badge.label, "iron" );
    v.badge.label_length = 4;
    v.hull = 400;
}

// ---------------------------------------------------------------------------
// THE `bits(N)` FAMILY (docs/SPEC-TABLES.md §3.4: "the declared storage width,
// 4 for N <= 32 and 8 above").
//
// THIS IS THE ROW WHERE THIS FORM SPENDS BYTES ON PURPOSE and says so: "a
// `bits(12)` costs four bytes here where §3 spends two". So the storage width
// is the DECLARATION's and not the value's, in all nine ports, and a leg that
// packed a bits(12) into two bytes has moved every field behind it. Every value
// below FILLS its declared width, so a narrower store is visible in the bytes
// and not only in the arithmetic.

inline void FillBits( tabledemo::RangedWidths & v )
{
    tabledemo::RangedWidthsReset( v );
    v.b8  = 0xFFu;                    // full at 8 bits, in a uint32
    v.b16 = 0xFFFFu;                  // full at 16, in a uint32
    v.b32 = 0xFFFFFFFFu;              // full at 32, in a uint32
    v.b64 = 0xFFFFFFFFFFFFFFFFull;    // full at 64, in a uint64
    v.b12 = 0x0FFFu;                  // full at 12, in a uint32 — the row §3.4 names
    v.b48 = 0x0000FFFFFFFFFFFFull;    // full at 48, in a uint64
}

inline void FillBits2( tblrw2::RangedWidths & v )
{
    tblrw2::RangedWidthsReset( v );
    v.b8  = 0xFFu;
    v.b16 = 0xFFFFu;
    v.b32 = 0xFFFFFFFFu;
    v.b64 = 0xFFFFFFFFFFFFFFFFull;
    v.b12 = 0x0FFFu;
    v.b48 = 0x0000FFFFFFFFFFFFull;
    v.b24 = 0x00FFFFFFu;              // a bits field the first generation cannot name
    v.tail = 8;
}

// ---------------------------------------------------------------------------
// NK1/NK2: the plain narrow integer kinds spelled as themselves (GAP-1).
//
// The C++ ABI pads an int8 in front of an int16; the wire does not. A port
// that memcpy'd the whole body would still round-trip against itself and
// would fail the oracle, which is the whole of this pair.

inline void FillNk1( tblnk1::Narrow & v )
{
    tblnk1::NarrowReset( v );
    v.i8  = (int8_t) -128;
    v.i16 = (int16_t) 32767;
    v.u8  = 255u;
    v.u16 = 1u;
    v.i32 = -1;
    v.u32 = 0xFFFFFFFFu;
    v.gone = (int8_t) 42;
    v.after = (int8_t) 9;
}

inline void FillNk2( tblnk2::Narrow & v )
{
    tblnk2::NarrowReset( v );
    v.i8  = (int8_t) 127;
    v.i16 = (int16_t) -32768;
    v.u8  = 1u;
    v.u16 = 65535u;
    v.i32 = 1;
    v.u32 = 0u;
    v.extra = (int8_t) 99;
    v.after = (int8_t) 6;
}

// ---------------------------------------------------------------------------
// FC1/FC2: a compressed float rides as the IEEE float, not as a quantized
// index (GAP-3). `on_grid` is a value the packet wire would spend a small
// integer on; `off_grid` is a value a quantizing port would snap.

inline void FillFc1( tblfc1::Probe & v )
{
    tblfc1::ProbeReset( v );
    v.plain = 3.5f;
    v.on_grid = 2.5f;
    v.off_grid = 1.234f;
    v.after = 9;
}

inline void FillFc2( tblfc2::Probe & v )
{
    tblfc2::ProbeReset( v );
    v.plain = -4.25f;
    v.on_grid = 0.01f;
    v.off_grid = 9.999f;
    v.extra = -1.5f;
    v.after = 6;
    v.tail = 8;
}

#endif // SCHEMA_TEST_FIXEDFORM_FIXTURES_H
