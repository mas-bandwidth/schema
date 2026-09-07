// A FLOAT RIDES AS ITS BIT PATTERN (docs/SPEC-TABLES.md §3, SPEC.md §4.3,
// schema#480): the pinned instance, shared by the two binaries that use it.
//
// main.cpp WRITES testdata/wire/tables/floats_nan.bin from this instance and
// reads it back, which is what every table golden gets and what the big-endian
// leg turns into a claim about the wire rather than about the machine.
// floatnan_main.cpp carries the pin's own CLAIM — the bits come back, and come
// back through §4's widened rung too — and is what the negative control turns
// red.
//
// Every value here is chosen so a hardware float conversion would MOVE it: the
// quiet bit set on a signalling NaN, or the payload lost outright.

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

inline float float_from_bits( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
inline double double_from_bits( uint64_t bits ) { double d; memcpy( &d, &bits, 8 ); return d; }
inline uint32_t bits_of_float( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
inline uint64_t bits_of_double( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

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
    f.after = 7; // a field AFTER the floats, so the parent can be seen to read on
}

#endif
