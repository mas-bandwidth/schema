// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go
// finite-only too, same rule as packets"): the patterns no table form carries,
// shared by the two binaries that use them.
//
// testdata/wire/tables/floats_nan.bin is the MUST-REFUSE vector every leg
// reads: the bytes the reference wrote under the old bit-pattern rule
// (schema#480), frozen, and now refused by every read as non_finite_float. No
// writer can produce it any more, so nothing rewrites it; main.cpp holds the
// writer to its refusal of the same instance, and floatnan_main.cpp carries
// the pin's own CLAIM — every read refuses the file, at both widths, and
// lands nothing — which is what the negative control turns red.
//
// Every value here has an all-ones exponent: a signalling NaN, a quiet NaN
// with a payload, both signs, an infinity, at both widths and as array
// elements.

#ifndef SCHEMA_TEST_FLOAT_NAN_H
#define SCHEMA_TEST_FLOAT_NAN_H

#include <cstdint>
#include <cstring>

#include "F1Table.h"

static const uint32_t kFloatSignalling = 0x7F800001u; // quiet bit clear, the smallest payload
static const uint32_t kFloatPayload    = 0x7FC0DEADu; // quiet, and a mantissa past the quiet bit
static const uint32_t kFloatNegative   = 0xFFA5A5A5u; // the sign set, signalling, a rich payload
static const uint32_t kFloatSample0    = 0x7F812345u; // signalling, as an ARRAY element
static const uint32_t kFloatSample2    = 0xFF800042u; // signalling and negative, as an element
static const uint64_t kDoubleQuiet     = 0x7FF8000000000000ull;
static const uint64_t kDoubleWide      = 0x7FF00DEFACED0001ull; // signalling at sixty-four bits
static const uint32_t kFloatInfinity   = 0x7F800000u; // +inf: no payload, still not finite
static const uint64_t kDoubleNegInf    = 0xFFF0000000000000ull; // -inf at sixty-four bits

inline float float_from_bits( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
inline double double_from_bits( uint64_t bits ) { double d; memcpy( &d, &bits, 8 ); return d; }
inline uint32_t bits_of_float( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
inline uint64_t bits_of_double( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

// the instance floats_nan.bin was written from: every float non-finite
inline void build_golden_floats_nan( tblf1::Floats & f )
{
    f.signalling = float_from_bits( kFloatSignalling );
    f.payload = float_from_bits( kFloatPayload );
    f.negative = float_from_bits( kFloatNegative );
    f.quiet = double_from_bits( kDoubleQuiet );
    f.wide = double_from_bits( kDoubleWide );
    f.samples[0] = float_from_bits( kFloatSample0 );
    f.samples[1] = 1.5f; // an ordinary element beside them: nothing here is special-cased
    f.samples[2] = float_from_bits( kFloatSample2 );
    f.past = 7; // a field AFTER the floats, so a read that landed anything shows it
}

#endif
