// schema_reflect.h — EXPERIMENT (issue #105). The generic side of the question:
// ONE write function and ONE read function, written over a reflection surface,
// replacing the per-type Write*/Read* the compiler emits today.
//
// Nothing in this file is per-type. It is library code that would live in the
// runtime (or in a single emitted prelude), written once. The per-type material
// is the Reflect<T> specialization, which is the thing schema would emit — see
// WireReflect.h / ObjectsReflect.h for a hand-written stand-in.
//
// The generic functions target exactly the primitives the emitted code calls:
// write_bits / read_int / read_int64 / read_bits / write_bool / read_bool /
// write_float / write_double / serialize_compressed_float / write_align /
// write_bytes. No new runtime surface is introduced.
//
// Two build switches, both measured — see the report:
//
//   SCHEMA_REFLECT_WRITE_FLAT_FOLD   the write walk accumulates with `&=`
//                                    instead of short-circuiting with `&&`
//   SCHEMA_REFLECT_ACCESSOR          member access goes through per-field
//                                    accessors instead of pointers to members

#pragma once

#include <cstdint>
#include <cstddef>
#include <type_traits>

#include "serialize.h"

namespace schema_reflect {

// The inlining spelling the emitted code uses (ObjectsWire.h's
// SCHEMA_WRITE_INLINE / SCHEMA_READ_INLINE), reproduced here so the two sides
// of the comparison differ ONLY in how the field walk is expressed
// (BENCH-STANDARD §3.2 — same call sites on both sides).
#if defined( _MSC_VER )
#define SCHEMA_REFLECT_INLINE __forceinline
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_REFLECT_INLINE inline __attribute__(( always_inline ))
#else
#define SCHEMA_REFLECT_INLINE inline
#endif

// ---------------------------------------------------------------------------
// The reflected field kinds. One per distinct emission codepath in the C++
// backend — this list is a transcription of the emitter's switch, not a design.
// ---------------------------------------------------------------------------

enum class Kind : uint8_t
{
    RangedInt,        // ranged integer in the int32 domain          [min, max]
    RangedInt64,      // ranged integer in the int64 domain          [min, max]
    Bits,             // raw bits, storage is exactly uint32/uint64  bits(N)
    BitsNarrow,       // raw bits into narrower / signed storage     int8/int16/uint8/uint16/int64
    Bool,             // 1 bit
    Float,            // 32 raw bits
    Double,           // 64 raw bits
    CompressedFloat,  // [min, max] @ resolution
    Enum,             // ranged integer, cast to/from the enum type
    Nested,           // a declared type with its own Reflect<>
    Vector,           // count-prefixed array of a ranged-int element
    Bytes,            // byte-aligned [N]uint8 bulk
    String,           // string(N): length prefix + bytes + read-side validation
    Align,            // no storage: an alignment point on the wire
};

// ---------------------------------------------------------------------------
// bits_required, at compile time. serialize::bits_required is a runtime inline
// (it uses __builtin_clz), so the generic path needs a constexpr twin. Values
// are identical by construction: 0 when min == max, else the bit length of the
// unsigned difference. Measured: nothing survives to run time — no clz and no
// call to bits_required appears in any probe, on either side.
// ---------------------------------------------------------------------------

constexpr int bit_length( uint64_t v )
{
    return v == 0 ? 0 : 1 + bit_length( v >> 1 );
}

constexpr int bits_required32( int64_t min, int64_t max )
{
    return min == max ? 0 : bit_length( uint64_t( uint32_t( uint32_t( max ) - uint32_t( min ) ) ) );
}

constexpr int bits_required64( int64_t min, int64_t max )
{
    return min == max ? 0 : bit_length( uint64_t( max ) - uint64_t( min ) );
}

// forward: the member/element type traits the access macros need
template <typename M> struct member_of;
template <typename C, typename M> struct member_of<M C::*> { using type = M; };
template <typename P> using member_t = typename member_of<typename std::remove_cv<P>::type>::type;

template <typename A> struct element_of;
template <typename E, size_t N> struct element_of<E[N]> { using type = E; static constexpr size_t count = N; };

// ---------------------------------------------------------------------------
// HOW THE GENERIC CODE REACHES THE MEMBER — the finding this experiment turned
// up, and the switch that acts on it.
//
// `value.*ptr` and `value.field` are the same address, but they are NOT the
// same to the optimizer. clang attaches a STRUCT-PATH TBAA tag to a direct
// member access — !{ base type, access type, byte offset } — and only a bare
// scalar tag — !{ "long long", "long long", 0 } — to a pointer-to-member
// access. A bare scalar tag may-alias every other access of that scalar type,
// including the ReadStream's own int64 cursor, so each store into the object
// forces the stream state to be reloaded and each load from the object blocks a
// store from being sunk.
//
// SCHEMA_REFLECT_ACCESSOR routes access through per-field accessors whose
// BODIES perform the member access, so the load and the store — and therefore
// the struct-path tag — live inside the accessor. Returning a reference is NOT
// enough: the tag is attached where the load or store is written, so the
// accessor has to carry the value, not the address.
//
// The accessor lines are exactly what an emitter would write out per field; the
// macro is shorthand for them, not a trick.
// ---------------------------------------------------------------------------

// (Every accessor is a template on the object type so that its body is only
// checked when the walk actually uses it — a field's `elem_set` is nonsense for
// a scalar member and must not be diagnosed unless something asks for it.)
#define SCHEMA_FIELD_REF( T, M )                                                                                           \
    static SCHEMA_REFLECT_INLINE auto & ref( T & v ) { return v.M; }                                                       \
    static SCHEMA_REFLECT_INLINE const auto & ref( const T & v ) { return v.M; }                                           \
    template <typename O> static SCHEMA_REFLECT_INLINE auto get( const O & v ) { return v.M; }                             \
    template <typename O, typename X> static SCHEMA_REFLECT_INLINE void set( O & v, X x ) { v.M = static_cast<decltype( v.M )>( x ); } \
    template <typename O, typename X> static SCHEMA_REFLECT_INLINE void elem_set( O & v, int32_t i, X x ) { v.M[i] = static_cast<typename std::remove_reference<decltype( v.M[0] )>::type>( x ); } \
    template <typename O> static SCHEMA_REFLECT_INLINE auto elem_get( const O & v, int32_t i ) { return v.M[i]; }

#define SCHEMA_FIELD_COUNT_REF( T, M )                                                                                     \
    template <typename O> static SCHEMA_REFLECT_INLINE auto count_get( const O & v ) { return v.M; }                        \
    template <typename O, typename X> static SCHEMA_REFLECT_INLINE void count_set( O & v, X x ) { v.M = static_cast<decltype( v.M )>( x ); }

#define SCHEMA_FIELD_LEN_REF( T, M )                                                                                       \
    template <typename O> static SCHEMA_REFLECT_INLINE auto len_get( const O & v ) { return v.M; }                          \
    template <typename O, typename X> static SCHEMA_REFLECT_INLINE void len_set( O & v, X x ) { v.M = static_cast<decltype( v.M )>( x ); }

#ifdef SCHEMA_REFLECT_ACCESSOR
#define SCHEMA_MEM( F, obj )            F::ref( obj )
#define SCHEMA_GET( F, obj )            F::get( obj )
#define SCHEMA_SET( F, obj, x )         F::set( obj, x )
#define SCHEMA_ELEM_GET( F, obj, i )    F::elem_get( obj, i )
#define SCHEMA_ELEM_SET( F, obj, i, x ) F::elem_set( obj, i, x )
#define SCHEMA_COUNT_GET( F, obj )      F::count_get( obj )
#define SCHEMA_COUNT_SET( F, obj, x )   F::count_set( obj, x )
#define SCHEMA_LEN_GET( F, obj )        F::len_get( obj )
#define SCHEMA_LEN_SET( F, obj, x )     F::len_set( obj, x )
#else
#define SCHEMA_MEM( F, obj )            ( ( obj ).*F::ptr )
#define SCHEMA_GET( F, obj )            ( ( obj ).*F::ptr )
#define SCHEMA_SET( F, obj, x )         ( ( obj ).*F::ptr = static_cast<member_t<decltype( F::ptr )>>( x ) )
#define SCHEMA_ELEM_GET( F, obj, i )    ( ( ( obj ).*F::ptr )[i] )
#define SCHEMA_ELEM_SET( F, obj, i, x ) ( ( ( obj ).*F::ptr )[i] = static_cast<typename element_of<member_t<decltype( F::ptr )>>::type>( x ) )
#define SCHEMA_COUNT_GET( F, obj )      ( ( obj ).*F::count_ptr )
#define SCHEMA_COUNT_SET( F, obj, x )   ( ( obj ).*F::count_ptr = static_cast<member_t<decltype( F::count_ptr )>>( x ) )
#define SCHEMA_LEN_GET( F, obj )        ( ( obj ).*F::len_ptr )
#define SCHEMA_LEN_SET( F, obj, x )     ( ( obj ).*F::len_ptr = static_cast<member_t<decltype( F::len_ptr )>>( x ) )
#endif

// ---------------------------------------------------------------------------
// The field list. Reflect<T>::fields is a FieldList of descriptor types; each
// descriptor is a struct of static constexpr members (see WireReflect.h).
// ---------------------------------------------------------------------------

template <typename... Fields> struct FieldList {};

// Reflect<T> — the per-type surface. THIS is what schema would emit.
template <typename T> struct Reflect;

// ---------------------------------------------------------------------------
// THE GENERIC WRITE — one function, every type.
// ---------------------------------------------------------------------------

template <typename T> SCHEMA_REFLECT_INLINE bool Write( serialize::WriteStream & stream, const T & value );

template <typename F, typename T>
SCHEMA_REFLECT_INLINE bool WriteField( serialize::WriteStream & stream, const T & value )
{
    if constexpr ( F::kind == Kind::Align )
    {
        (void) value;
        write_align( stream );
    }
    else
    {
        using MT = member_t<decltype( F::ptr )>;

        if constexpr ( F::kind == Kind::RangedInt )
        {
            constexpr int bits = bits_required32( F::min, F::max );
            serialize_assert( int32_t( SCHEMA_GET( F, value ) ) >= int32_t( F::min ) && int32_t( SCHEMA_GET( F, value ) ) <= int32_t( F::max ) );
            write_bits( stream, uint32_t( SCHEMA_GET( F, value ) ) - uint32_t( int32_t( F::min ) ), bits );
        }
        else if constexpr ( F::kind == Kind::RangedInt64 )
        {
            constexpr int bits = bits_required64( F::min, F::max );
            serialize_assert( int64_t( SCHEMA_GET( F, value ) ) >= int64_t( F::min ) && int64_t( SCHEMA_GET( F, value ) ) <= int64_t( F::max ) );
            write_bits( stream, uint64_t( SCHEMA_GET( F, value ) ) - uint64_t( int64_t( F::min ) ), bits );
        }
        else if constexpr ( F::kind == Kind::Bits )
        {
            write_bits( stream, SCHEMA_GET( F, value ), F::bits );
        }
        else if constexpr ( F::kind == Kind::BitsNarrow )
        {
            using U = typename std::make_unsigned<MT>::type;
            write_bits( stream, U( SCHEMA_GET( F, value ) ), F::bits );
        }
        else if constexpr ( F::kind == Kind::Bool )
        {
            write_bool( stream, SCHEMA_GET( F, value ) );
        }
        else if constexpr ( F::kind == Kind::Float )
        {
            write_float( stream, SCHEMA_GET( F, value ) );
        }
        else if constexpr ( F::kind == Kind::Double )
        {
            write_double( stream, SCHEMA_GET( F, value ) );
        }
        else if constexpr ( F::kind == Kind::CompressedFloat )
        {
            float compressed_value = SCHEMA_GET( F, value );
            serialize_compressed_float( stream, compressed_value, F::fmin, F::fmax, F::resolution );
        }
        else if constexpr ( F::kind == Kind::Enum )
        {
            constexpr int bits = bits_required32( 0, F::max );
            serialize_assert( int32_t( SCHEMA_GET( F, value ) ) >= int32_t( 0 ) && int32_t( SCHEMA_GET( F, value ) ) <= int32_t( F::max ) );
            write_bits( stream, uint32_t( SCHEMA_GET( F, value ) ), bits );
        }
        else if constexpr ( F::kind == Kind::Nested )
        {
            // F::declared is the DECLARED type; MT is the storage type, which
            // may be the cpp_native mapping (SPEC §4.2). schema knows both;
            // C++ cannot recover the declared type from the storage type.
            if ( !Write( stream, static_cast<const typename F::declared &>( SCHEMA_MEM( F, value ) ) ) )
            {
                return false;
            }
        }
        else if constexpr ( F::kind == Kind::Vector )
        {
            constexpr int count_bits = bits_required32( 0, F::max_count );
            constexpr int elem_bits = bits_required32( F::min, F::max );
            const int32_t count = int32_t( SCHEMA_COUNT_GET( F, value ) );
            serialize_assert( count >= int32_t( 0 ) && count <= int32_t( F::max_count ) );
            write_bits( stream, uint32_t( count ), count_bits );
            for ( int32_t i = 0; i < count; i++ )
            {
                serialize_assert( int32_t( SCHEMA_ELEM_GET( F, value, i ) ) >= int32_t( F::min ) && int32_t( SCHEMA_ELEM_GET( F, value, i ) ) <= int32_t( F::max ) );
                write_bits( stream, uint32_t( SCHEMA_ELEM_GET( F, value, i ) ) - uint32_t( int32_t( F::min ) ), elem_bits );
            }
        }
        else if constexpr ( F::kind == Kind::Bytes )
        {
            write_bytes( stream, SCHEMA_MEM( F, value ), int( element_of<MT>::count ) );
        }
        else if constexpr ( F::kind == Kind::String )
        {
            constexpr int len_bits = bits_required32( 0, F::max_length );
            const int32_t length = int32_t( SCHEMA_LEN_GET( F, value ) );
            for ( int32_t i = 0; i < length; i++ )
            {
                serialize_assert( SCHEMA_ELEM_GET( F, value, i ) != 0 );
            }
            serialize_assert( F::utf8_valid( reinterpret_cast<const uint8_t *>( SCHEMA_MEM( F, value ) ), length ) );
            serialize_assert( length >= int32_t( 0 ) && length <= int32_t( F::max_length ) );
            write_bits( stream, uint32_t( length ), len_bits );
            write_bytes( stream, SCHEMA_MEM( F, value ), int( length ) );
        }
        else
        {
            static_assert( F::kind != F::kind, "unhandled field kind" );
        }
    }
    return true;
}

// The `&&` fold below is a short-circuit chain, one conditional branch per
// field. The compiler-emitted WRITE function has no such chain — write_* cannot
// fail — and LLVM's block-frequency analysis prices each Ok/Err split at ~even
// odds, so by the time the walk reaches a later field the call site is
// classified COLD and held to ColdCallSiteThreshold (45) instead of the hot
// threshold (250). The INLINE COST is unchanged; only the threshold moves.
//
// SCHEMA_REFLECT_WRITE_FLAT_FOLD replaces the short-circuit with a
// non-short-circuiting `&=` accumulation: same result value, no branch per
// field. Deviation, stated plainly: on a failure it keeps writing the remaining
// fields instead of returning at once. Harmless for the write direction (the
// writer is trusted; the only fallible write primitive,
// serialize_compressed_float, cannot fail on a conforming declaration) but it
// IS a semantic difference, and a production emitter would restrict the flat
// fold to types whose write path has no fallible field.
template <typename T, typename... Fields>
SCHEMA_REFLECT_INLINE bool WriteFieldList( serialize::WriteStream & stream, const T & value, FieldList<Fields...> )
{
#ifdef SCHEMA_REFLECT_WRITE_FLAT_FOLD
    bool ok = true;
    ( ( ok &= WriteField<Fields>( stream, value ) ), ... );
    return ok;
#else
    return ( WriteField<Fields>( stream, value ) && ... );
#endif
}

template <typename T>
SCHEMA_REFLECT_INLINE bool Write( serialize::WriteStream & stream, const T & value )
{
    return WriteFieldList( stream, value, typename Reflect<T>::fields{} );
}

// ---------------------------------------------------------------------------
// THE GENERIC READ — one function, every type.
// ---------------------------------------------------------------------------

template <typename T> SCHEMA_REFLECT_INLINE bool Read( serialize::ReadStream & stream, T & value );

template <typename F, typename T>
SCHEMA_REFLECT_INLINE bool ReadField( serialize::ReadStream & stream, T & value )
{
    if constexpr ( F::kind == Kind::Align )
    {
        (void) value;
        read_align( stream );
    }
    else
    {
        using MT = member_t<decltype( F::ptr )>;

        if constexpr ( F::kind == Kind::RangedInt )
        {
            int32_t range_value = 0;
            read_int( stream, range_value, int32_t( F::min ), int32_t( F::max ) );
            SCHEMA_SET( F, value, range_value );
        }
        else if constexpr ( F::kind == Kind::RangedInt64 )
        {
            int64_t range_value = 0;
            read_int64( stream, range_value, int64_t( F::min ), int64_t( F::max ) );
            SCHEMA_SET( F, value, range_value );
        }
        else if constexpr ( F::kind == Kind::Bits )
        {
            MT raw_value = 0;
            read_bits( stream, raw_value, F::bits );
            SCHEMA_SET( F, value, raw_value );
        }
        else if constexpr ( F::kind == Kind::BitsNarrow )
        {
            typename std::conditional<( F::bits > 32 ), uint64_t, uint32_t>::type raw_value = 0;
            read_bits( stream, raw_value, F::bits );
            SCHEMA_SET( F, value, raw_value );
        }
        else if constexpr ( F::kind == Kind::Bool )
        {
            bool bool_value = false;
            read_bool( stream, bool_value );
            SCHEMA_SET( F, value, bool_value );
        }
        else if constexpr ( F::kind == Kind::Float )
        {
            float float_value = 0.0f;
            read_float( stream, float_value );
            SCHEMA_SET( F, value, float_value );
        }
        else if constexpr ( F::kind == Kind::Double )
        {
            double double_value = 0.0;
            read_double( stream, double_value );
            SCHEMA_SET( F, value, double_value );
        }
        else if constexpr ( F::kind == Kind::CompressedFloat )
        {
            float compressed_value = 0.0f;
            serialize_compressed_float( stream, compressed_value, F::fmin, F::fmax, F::resolution );
            SCHEMA_SET( F, value, compressed_value );
        }
        else if constexpr ( F::kind == Kind::Enum )
        {
            int32_t enum_value = 0;
            read_int( stream, enum_value, 0, int32_t( F::max ) );
            SCHEMA_SET( F, value, MT( enum_value ) );
        }
        else if constexpr ( F::kind == Kind::Nested )
        {
            if ( !Read( stream, static_cast<typename F::declared &>( SCHEMA_MEM( F, value ) ) ) )
            {
                return false;
            }
        }
        else if constexpr ( F::kind == Kind::Vector )
        {
            int32_t count = 0;
            read_int( stream, count, 0, int32_t( F::max_count ) );
            SCHEMA_COUNT_SET( F, value, count );
            for ( int32_t i = 0; i < count; i++ )
            {
                int32_t element = 0;
                read_int( stream, element, int32_t( F::min ), int32_t( F::max ) );
                SCHEMA_ELEM_SET( F, value, i, element );
            }
        }
        else if constexpr ( F::kind == Kind::Bytes )
        {
            read_bytes( stream, SCHEMA_MEM( F, value ), int( element_of<MT>::count ) );
        }
        else if constexpr ( F::kind == Kind::String )
        {
            int32_t length = 0;
            read_int( stream, length, 0, int32_t( F::max_length ) );
            SCHEMA_LEN_SET( F, value, length );
            read_bytes( stream, SCHEMA_MEM( F, value ), int( length ) );
            if ( F::interior_null( reinterpret_cast<const uint8_t *>( SCHEMA_MEM( F, value ) ), length ) )
            {
                return false; // an interior null is content the read refuses (SPEC §4.7)
            }
            SCHEMA_ELEM_SET( F, value, length, 0 );
        }
        else
        {
            static_assert( F::kind != F::kind, "unhandled field kind" );
        }
    }
    return true;
}

template <typename T, typename... Fields>
SCHEMA_REFLECT_INLINE bool ReadFieldList( serialize::ReadStream & stream, T & value, FieldList<Fields...> )
{
    return ( ReadField<Fields>( stream, value ) && ... );
}

template <typename T>
SCHEMA_REFLECT_INLINE bool Read( serialize::ReadStream & stream, T & value )
{
    return ReadFieldList( stream, value, typename Reflect<T>::fields{} );
}

} // namespace schema_reflect
