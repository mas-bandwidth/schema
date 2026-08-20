// WireReflect.h — HAND-WRITTEN STAND-IN for what the schema compiler would
// emit from Wire.schema. EXPERIMENT (issue #105); the compiler emits nothing
// like this today.
//
// Shape: one descriptor struct per field, one Reflect<T> per type. Every value
// in here is something the compiler already computes while emitting
// WireWire.h — the declared bounds, the resolution, the field name, the
// storage member. Nothing is invented.
//
// THINGS SCHEMA COULD NOT MECHANICALLY DERIVE — none in this file. Every
// member below is a transcription of a fact already in the IR. The two static
// member functions (utf8_valid / interior_null) forward to helpers the emitter
// ALREADY writes into WireWire.h; a real emitter would name them directly.

#pragma once

#include "schema_reflect.h"

#include "Wire.h"
#include "WireWire.h"

namespace example {
namespace reflect {

using schema_reflect::Kind;

// ---- table TestData ------------------------------------------------------

struct TestData_a           { static constexpr Kind kind = Kind::RangedInt;       static constexpr auto ptr = &TestData::a; SCHEMA_FIELD_REF( TestData, a )                      static constexpr const char * name = "a";                      static constexpr int64_t min = -100, max = 100; };
struct TestData_b           { static constexpr Kind kind = Kind::RangedInt;       static constexpr auto ptr = &TestData::b; SCHEMA_FIELD_REF( TestData, b )                      static constexpr const char * name = "b";                      static constexpr int64_t min = -100, max = 100; };
struct TestData_c           { static constexpr Kind kind = Kind::RangedInt;       static constexpr auto ptr = &TestData::c; SCHEMA_FIELD_REF( TestData, c )                      static constexpr const char * name = "c";                      static constexpr int64_t min = -100, max = 150; };
struct TestData_d           { static constexpr Kind kind = Kind::Bits;            static constexpr auto ptr = &TestData::d; SCHEMA_FIELD_REF( TestData, d )                      static constexpr const char * name = "d";                      static constexpr int bits = 8; };
struct TestData_e           { static constexpr Kind kind = Kind::Bits;            static constexpr auto ptr = &TestData::e; SCHEMA_FIELD_REF( TestData, e )                      static constexpr const char * name = "e";                      static constexpr int bits = 8; };
struct TestData_f           { static constexpr Kind kind = Kind::Bits;            static constexpr auto ptr = &TestData::f; SCHEMA_FIELD_REF( TestData, f )                      static constexpr const char * name = "f";                      static constexpr int bits = 8; };
struct TestData_g           { static constexpr Kind kind = Kind::Bool;            static constexpr auto ptr = &TestData::g; SCHEMA_FIELD_REF( TestData, g )                      static constexpr const char * name = "g"; };
struct TestData_items       { static constexpr Kind kind = Kind::Vector;          static constexpr auto ptr = &TestData::items; SCHEMA_FIELD_REF( TestData, items )                  static constexpr auto count_ptr = &TestData::items_count; SCHEMA_FIELD_COUNT_REF( TestData, items_count )
                              static constexpr const char * name = "items";       static constexpr int64_t max_count = 16;                       static constexpr int64_t min = 0, max = 255; };
struct TestData_float       { static constexpr Kind kind = Kind::Float;           static constexpr auto ptr = &TestData::float_value; SCHEMA_FIELD_REF( TestData, float_value )            static constexpr const char * name = "float_value"; };
struct TestData_cfloat      { static constexpr Kind kind = Kind::CompressedFloat; static constexpr auto ptr = &TestData::compressed_float_value; SCHEMA_FIELD_REF( TestData, compressed_float_value ) static constexpr const char * name = "compressed_float_value";
                              static constexpr float fmin = 0.0f, fmax = 10.0f, resolution = 0.01f; };
struct TestData_double      { static constexpr Kind kind = Kind::Double;          static constexpr auto ptr = &TestData::double_value; SCHEMA_FIELD_REF( TestData, double_value )           static constexpr const char * name = "double_value"; };
struct TestData_int8        { static constexpr Kind kind = Kind::BitsNarrow;      static constexpr auto ptr = &TestData::int8_value; SCHEMA_FIELD_REF( TestData, int8_value )             static constexpr const char * name = "int8_value";             static constexpr int bits = 8; };
struct TestData_int16       { static constexpr Kind kind = Kind::BitsNarrow;      static constexpr auto ptr = &TestData::int16_value; SCHEMA_FIELD_REF( TestData, int16_value )            static constexpr const char * name = "int16_value";            static constexpr int bits = 16; };
struct TestData_uint8       { static constexpr Kind kind = Kind::BitsNarrow;      static constexpr auto ptr = &TestData::uint8_value; SCHEMA_FIELD_REF( TestData, uint8_value )            static constexpr const char * name = "uint8_value";            static constexpr int bits = 8; };
struct TestData_uint16      { static constexpr Kind kind = Kind::BitsNarrow;      static constexpr auto ptr = &TestData::uint16_value; SCHEMA_FIELD_REF( TestData, uint16_value )           static constexpr const char * name = "uint16_value";           static constexpr int bits = 16; };
struct TestData_uint32      { static constexpr Kind kind = Kind::Bits;            static constexpr auto ptr = &TestData::uint32_value; SCHEMA_FIELD_REF( TestData, uint32_value )           static constexpr const char * name = "uint32_value";           static constexpr int bits = 32; };
struct TestData_uint64      { static constexpr Kind kind = Kind::Bits;            static constexpr auto ptr = &TestData::uint64_value; SCHEMA_FIELD_REF( TestData, uint64_value )           static constexpr const char * name = "uint64_value";           static constexpr int bits = 64; };
struct TestData_int64_full  { static constexpr Kind kind = Kind::BitsNarrow;      static constexpr auto ptr = &TestData::int64_full; SCHEMA_FIELD_REF( TestData, int64_full )             static constexpr const char * name = "int64_full";             static constexpr int bits = 64; };
struct TestData_int64_range { static constexpr Kind kind = Kind::RangedInt64;     static constexpr auto ptr = &TestData::int64_range; SCHEMA_FIELD_REF( TestData, int64_range )            static constexpr const char * name = "int64_range";
                              static constexpr int64_t min = -1000000000000ll, max = 1000000000000ll; };
struct TestData_align       { static constexpr Kind kind = Kind::Align;           static constexpr const char * name = ""; };
struct TestData_fixed_bytes { static constexpr Kind kind = Kind::Bytes;           static constexpr auto ptr = &TestData::fixed_bytes; SCHEMA_FIELD_REF( TestData, fixed_bytes )            static constexpr const char * name = "fixed_bytes"; };
struct TestData_text        { static constexpr Kind kind = Kind::String;          static constexpr auto ptr = &TestData::text; SCHEMA_FIELD_REF( TestData, text )                   static constexpr auto len_ptr = &TestData::text_length; SCHEMA_FIELD_LEN_REF( TestData, text_length )
                              static constexpr const char * name = "text";        static constexpr int64_t max_length = 255;
                              // SCHEMA_REFLECT_INLINE: these forward to the helpers the emitter ALREADY writes
                              // into WireWire.h, which are themselves always_inline. Without the demand the
                              // forwarder becomes a real out-of-line call the emitted path does not have.
                              static SCHEMA_REFLECT_INLINE bool utf8_valid( const uint8_t * b, int32_t n ) { return schema_utf8_valid( b, n ); }
                              static SCHEMA_REFLECT_INLINE bool interior_null( const uint8_t * b, int32_t n ) { return schema_interior_null( b, n ); } };

// ---- table CompressedProbe ----------------------------------------------

struct CompressedProbe_boundary { static constexpr Kind kind = Kind::CompressedFloat; static constexpr auto ptr = &CompressedProbe::boundary; SCHEMA_FIELD_REF( CompressedProbe, boundary ) static constexpr const char * name = "boundary";
                                  static constexpr float fmin = 0.0f, fmax = 10.0f, resolution = 0.01f; };
struct CompressedProbe_offset   { static constexpr Kind kind = Kind::CompressedFloat; static constexpr auto ptr = &CompressedProbe::offset; SCHEMA_FIELD_REF( CompressedProbe, offset )   static constexpr const char * name = "offset";
                                  static constexpr float fmin = -5.0f, fmax = 5.0f, resolution = 0.001f; };

} // namespace reflect
} // namespace example

namespace schema_reflect {

template <> struct Reflect<example::TestData>
{
    static constexpr const char * name = "TestData";
    using fields = FieldList<
        example::reflect::TestData_a,
        example::reflect::TestData_b,
        example::reflect::TestData_c,
        example::reflect::TestData_d,
        example::reflect::TestData_e,
        example::reflect::TestData_f,
        example::reflect::TestData_g,
        example::reflect::TestData_items,
        example::reflect::TestData_float,
        example::reflect::TestData_cfloat,
        example::reflect::TestData_double,
        example::reflect::TestData_int8,
        example::reflect::TestData_int16,
        example::reflect::TestData_uint8,
        example::reflect::TestData_uint16,
        example::reflect::TestData_uint32,
        example::reflect::TestData_uint64,
        example::reflect::TestData_int64_full,
        example::reflect::TestData_int64_range,
        example::reflect::TestData_align,
        example::reflect::TestData_fixed_bytes,
        example::reflect::TestData_text >;
};

template <> struct Reflect<example::CompressedProbe>
{
    static constexpr const char * name = "CompressedProbe";
    using fields = FieldList<
        example::reflect::CompressedProbe_boundary,
        example::reflect::CompressedProbe_offset >;
};

} // namespace schema_reflect
