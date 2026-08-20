// control_probe.h — EXPERIMENT (issue #105). THE NOISE FLOOR.
//
// Two controls that contain NO reflection at all, so the reflection residual
// can be told apart from "any respelling of the source moves the schedule".
//
//   control_verbatim  — the emitted WriteTestData body, copied character for
//                       character under a new name. The instrument's ZERO
//                       POINT: this must come out instruction-identical.
//
//   control_perfield  — the same statements, each moved into its own
//                       always_inline per-field function called in order. This
//                       is the per-field DECOMPOSITION with no reflection, no
//                       member pointers, no templates over descriptors — so it
//                       isolates what the decomposition alone costs.

#pragma once

#include "WireWire.h"

namespace control {

#define CTRL_INLINE inline __attribute__(( always_inline ))

CTRL_INLINE bool WriteTestData_Verbatim( serialize::WriteStream & stream, const example::TestData & value )
{
    serialize_assert( int32_t( value.a ) >= int32_t( -100 ) && int32_t( value.a ) <= int32_t( 100 ) );
    write_bits( stream, uint32_t( value.a ) - uint32_t( -100 ), 8 );
    serialize_assert( int32_t( value.b ) >= int32_t( -100 ) && int32_t( value.b ) <= int32_t( 100 ) );
    write_bits( stream, uint32_t( value.b ) - uint32_t( -100 ), 8 );
    serialize_assert( int32_t( value.c ) >= int32_t( -100 ) && int32_t( value.c ) <= int32_t( 150 ) );
    write_bits( stream, uint32_t( value.c ) - uint32_t( -100 ), 8 );
    write_bits( stream, value.d, 8 );
    write_bits( stream, value.e, 8 );
    write_bits( stream, value.f, 8 );
    write_bool( stream, value.g );
    serialize_assert( int32_t( value.items_count ) >= int32_t( 0 ) && int32_t( value.items_count ) <= int32_t( 16 ) );
    write_bits( stream, uint32_t( value.items_count ), 5 );
    for ( int32_t i = 0; i < value.items_count; i++ )
    {
        serialize_assert( int32_t( value.items[i] ) >= int32_t( 0 ) && int32_t( value.items[i] ) <= int32_t( 255 ) );
        write_bits( stream, uint32_t( value.items[i] ), 8 );
    }
    write_float( stream, value.float_value );
    {
        float compressed_value = value.compressed_float_value;
        serialize_compressed_float( stream, compressed_value, 0.0f, 10.0f, 0.01f );
    }
    write_double( stream, value.double_value );
    write_bits( stream, uint8_t( value.int8_value ), 8 );
    write_bits( stream, uint16_t( value.int16_value ), 16 );
    write_bits( stream, value.uint8_value, 8 );
    write_bits( stream, value.uint16_value, 16 );
    write_bits( stream, value.uint32_value, 32 );
    write_bits( stream, value.uint64_value, 64 );
    write_bits( stream, value.int64_full, 64 );
    serialize_assert( int64_t( value.int64_range ) >= int64_t( -1000000000000 ) && int64_t( value.int64_range ) <= int64_t( 1000000000000 ) );
    write_bits( stream, uint64_t( value.int64_range ) - uint64_t( -1000000000000 ), 41 );
    write_align( stream );
    write_bytes( stream, value.fixed_bytes, 17 );
    for ( int32_t i = 0; i < value.text_length; i++ )
    {
        serialize_assert( value.text[i] != 0 );
    }
    serialize_assert( example::schema_utf8_valid( reinterpret_cast<const uint8_t *>( value.text ), value.text_length ) );
    serialize_assert( int32_t( value.text_length ) >= int32_t( 0 ) && int32_t( value.text_length ) <= int32_t( 255 ) );
    write_bits( stream, uint32_t( value.text_length ), 8 );
    write_bytes( stream, value.text, value.text_length );
    return true;
}

// ---- the per-field decomposition, no reflection ---------------------------

CTRL_INLINE bool wf_a( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, uint32_t( v.a ) - uint32_t( -100 ), 8 ); return true; }
CTRL_INLINE bool wf_b( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, uint32_t( v.b ) - uint32_t( -100 ), 8 ); return true; }
CTRL_INLINE bool wf_c( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, uint32_t( v.c ) - uint32_t( -100 ), 8 ); return true; }
CTRL_INLINE bool wf_d( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.d, 8 ); return true; }
CTRL_INLINE bool wf_e( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.e, 8 ); return true; }
CTRL_INLINE bool wf_f( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.f, 8 ); return true; }
CTRL_INLINE bool wf_g( serialize::WriteStream & s, const example::TestData & v ) { write_bool( s, v.g ); return true; }
CTRL_INLINE bool wf_items( serialize::WriteStream & s, const example::TestData & v )
{
    write_bits( s, uint32_t( v.items_count ), 5 );
    for ( int32_t i = 0; i < v.items_count; i++ ) { write_bits( s, uint32_t( v.items[i] ), 8 ); }
    return true;
}
CTRL_INLINE bool wf_float( serialize::WriteStream & s, const example::TestData & v ) { write_float( s, v.float_value ); return true; }
CTRL_INLINE bool wf_cfloat( serialize::WriteStream & s, const example::TestData & v )
{
    float compressed_value = v.compressed_float_value;
    serialize_compressed_float( s, compressed_value, 0.0f, 10.0f, 0.01f );
    return true;
}
CTRL_INLINE bool wf_double( serialize::WriteStream & s, const example::TestData & v ) { write_double( s, v.double_value ); return true; }
CTRL_INLINE bool wf_i8( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, uint8_t( v.int8_value ), 8 ); return true; }
CTRL_INLINE bool wf_i16( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, uint16_t( v.int16_value ), 16 ); return true; }
CTRL_INLINE bool wf_u8( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.uint8_value, 8 ); return true; }
CTRL_INLINE bool wf_u16( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.uint16_value, 16 ); return true; }
CTRL_INLINE bool wf_u32( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.uint32_value, 32 ); return true; }
CTRL_INLINE bool wf_u64( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.uint64_value, 64 ); return true; }
CTRL_INLINE bool wf_i64f( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, v.int64_full, 64 ); return true; }
CTRL_INLINE bool wf_i64r( serialize::WriteStream & s, const example::TestData & v ) { write_bits( s, uint64_t( v.int64_range ) - uint64_t( -1000000000000 ), 41 ); return true; }
CTRL_INLINE bool wf_align( serialize::WriteStream & s, const example::TestData & ) { write_align( s ); return true; }
CTRL_INLINE bool wf_bytes( serialize::WriteStream & s, const example::TestData & v ) { write_bytes( s, v.fixed_bytes, 17 ); return true; }
CTRL_INLINE bool wf_text( serialize::WriteStream & s, const example::TestData & v )
{
    write_bits( s, uint32_t( v.text_length ), 8 );
    write_bytes( s, v.text, v.text_length );
    return true;
}

CTRL_INLINE bool WriteTestData_PerField( serialize::WriteStream & s, const example::TestData & v )
{
    return wf_a( s, v ) && wf_b( s, v ) && wf_c( s, v ) && wf_d( s, v ) && wf_e( s, v ) && wf_f( s, v ) &&
           wf_g( s, v ) && wf_items( s, v ) && wf_float( s, v ) && wf_cfloat( s, v ) && wf_double( s, v ) &&
           wf_i8( s, v ) && wf_i16( s, v ) && wf_u8( s, v ) && wf_u16( s, v ) && wf_u32( s, v ) &&
           wf_u64( s, v ) && wf_i64f( s, v ) && wf_i64r( s, v ) && wf_align( s, v ) && wf_bytes( s, v ) &&
           wf_text( s, v );
}

} // namespace control
