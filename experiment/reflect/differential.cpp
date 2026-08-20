// differential.cpp — EXPERIMENT (issue #105). WIRE IDENTITY.
//
// Drives the compiler-emitted Write*/Read* and the ONE generic
// schema_reflect::Write/Read over the same value sweep and compares BIT
// PATTERNS and WIRE BYTES — never tolerances.
//
// Per sample, six independent checks:
//   1  wire length agrees
//   2  wire bytes agree, byte for byte
//   3  emitted-read(emitted-wire) == generic-read(generic-wire), field bits
//   4  cross: generic-read(emitted-wire) == emitted-read(generic-wire)
//   5  both reads agree on their success/failure verdict
//   6  a corrupted / truncated buffer is refused (or accepted) identically
//
// The sweep is deterministic (xorshift64*, fixed seed) and mixes three
// generators: declared bounds and their neighbours, hostile bit patterns
// (all-ones, sign bits, NaN, Inf, subnormals, on-quantum and off-quantum
// compressed-float values), and uniform random. Built with NDEBUG so the
// writer-trusted asserts are inert and OUT-OF-RANGE values reach the
// arithmetic — that truncation is part of what has to agree.

#include <cstdio>
#include <cstring>
#include <cstdlib>
#include <cstdint>
#include <cmath>

#include "WireWire.h"
#include "ObjectsWire.h"

#include "schema_reflect.h"
#include "WireReflect.h"
#include "ObjectsReflect.h"

static uint64_t g_checks = 0;
static uint64_t g_fail = 0;

static void fail( const char * what, int sample )
{
    if ( g_fail < 20 )
    {
        printf( "DIVERGENCE sample %d: %s\n", sample, what );
    }
    g_fail++;
}

#define CHECK( cond, what, sample ) do { g_checks++; if ( !(cond) ) fail( what, sample ); } while (0)

// ---- deterministic value source -----------------------------------------

struct Rng
{
    uint64_t s;
    explicit Rng( uint64_t seed ) : s( seed ? seed : 0x9E3779B97F4A7C15ull ) {}
    uint64_t next()
    {
        s ^= s >> 12; s ^= s << 25; s ^= s >> 27;
        return s * 0x2545F4914F6CDD1Dull;
    }
    uint32_t u32() { return uint32_t( next() >> 32 ); }
    int32_t range( int32_t lo, int32_t hi ) { return lo + int32_t( u32() % uint32_t( int64_t( hi ) - lo + 1 ) ); }
};

// The hostile float pool: every value whose float32 bit pattern is interesting
// to a bit-exact comparison, plus the FMA-boundary values SPEC §7.2 gate 7
// pins for compressed floats.
static const float g_hostile_floats[] = {
    0.0f, -0.0f, 1.0f, -1.0f, 0.005f, -4.8585f, 2.5f, 0.01f, 9.99f, 10.0f, -5.0f, 5.0f,
    1e-45f, 1.17549435e-38f, 3.4028235e38f, -3.4028235e38f,
    0.1f, 0.3f, 0.7f, 1.0f / 3.0f, 123.456f, -123.456f,
};
static const int g_hostile_float_count = int( sizeof( g_hostile_floats ) / sizeof( g_hostile_floats[0] ) );

static float pick_float( Rng & r, int mode )
{
    if ( mode == 0 ) return g_hostile_floats[ r.u32() % uint32_t( g_hostile_float_count ) ];
#ifndef EXP_LEGAL_ONLY
    // any bit pattern, NaN/Inf included. The assert-enabled leg cannot use these:
    // serialize_compressed_float asserts finiteness of the value it is handed.
    if ( mode == 1 ) { uint32_t bits = r.u32(); float f; memcpy( &f, &bits, 4 ); return f; }
#endif
    return float( int32_t( r.u32() % 20001 ) - 10000 ) * 0.001f;
}

static double pick_double( Rng & r, int mode )
{
    if ( mode == 1 ) { uint64_t bits = r.next(); double d; memcpy( &d, &bits, 8 ); return d; }
    return double( pick_float( r, mode ) );
}

// ---- field-by-field bit comparison (padding-proof) ------------------------

static bool bits_eq_f( float a, float b ) { uint32_t x, y; memcpy( &x, &a, 4 ); memcpy( &y, &b, 4 ); return x == y; }
static bool bits_eq_d( double a, double b ) { uint64_t x, y; memcpy( &x, &a, 8 ); memcpy( &y, &b, 8 ); return x == y; }

static bool same( const example::TestData & a, const example::TestData & b )
{
    if ( a.a != b.a || a.b != b.b || a.c != b.c ) return false;
    if ( a.d != b.d || a.e != b.e || a.f != b.f || a.g != b.g ) return false;
    if ( a.items_count != b.items_count ) return false;
    for ( int i = 0; i < a.items_count; i++ ) { if ( a.items[i] != b.items[i] ) return false; }
    if ( !bits_eq_f( a.float_value, b.float_value ) ) return false;
    if ( !bits_eq_f( a.compressed_float_value, b.compressed_float_value ) ) return false;
    if ( !bits_eq_d( a.double_value, b.double_value ) ) return false;
    if ( a.int8_value != b.int8_value || a.int16_value != b.int16_value ) return false;
    if ( a.uint8_value != b.uint8_value || a.uint16_value != b.uint16_value ) return false;
    if ( a.uint32_value != b.uint32_value || a.uint64_value != b.uint64_value ) return false;
    if ( a.int64_full != b.int64_full || a.int64_range != b.int64_range ) return false;
    if ( memcmp( a.fixed_bytes, b.fixed_bytes, 17 ) != 0 ) return false;
    if ( a.text_length != b.text_length ) return false;
    if ( memcmp( a.text, b.text, size_t( a.text_length ) ) != 0 ) return false;
    return true;
}

static bool same_vec( const example::Vec3 & a, const example::Vec3 & b )
{
    return bits_eq_d( a.x, b.x ) && bits_eq_d( a.y, b.y ) && bits_eq_d( a.z, b.z );
}

static bool same( const example::ShipData_Deep & a, const example::ShipData_Deep & b )
{
    if ( a.ship_type != b.ship_type || a.flags != b.flags || a.team != b.team ) return false;
    if ( !same_vec( a.position, b.position ) || !same_vec( a.linear_velocity, b.linear_velocity ) ) return false;
    if ( !same_vec( a.angular_velocity, b.angular_velocity ) ) return false;
    if ( !same_vec( a.stick_current, b.stick_current ) || !same_vec( a.stick_velocity, b.stick_velocity ) ) return false;
    if ( !bits_eq_d( a.rotation.x, b.rotation.x ) || !bits_eq_d( a.rotation.y, b.rotation.y ) ) return false;
    if ( !bits_eq_d( a.rotation.z, b.rotation.z ) || !bits_eq_d( a.rotation.w, b.rotation.w ) ) return false;
    if ( !bits_eq_f( a.health, b.health ) || !bits_eq_f( a.thrust, b.thrust ) ) return false;
    if ( !bits_eq_f( a.laser_cooldown, b.laser_cooldown ) || !bits_eq_f( a.missile_cooldown, b.missile_cooldown ) ) return false;
    if ( !bits_eq_f( a.speed_current, b.speed_current ) || !bits_eq_f( a.speed_velocity, b.speed_velocity ) ) return false;
    if ( !bits_eq_f( a.sensitivity_current, b.sensitivity_current ) || !bits_eq_f( a.sensitivity_velocity, b.sensitivity_velocity ) ) return false;
    if ( !bits_eq_f( a.roll_current, b.roll_current ) || !bits_eq_f( a.roll_velocity, b.roll_velocity ) ) return false;
    if ( !bits_eq_f( a.aim_current, b.aim_current ) || !bits_eq_f( a.aim_velocity, b.aim_velocity ) ) return false;
    if ( a.laser_index != b.laser_index || a.missile_index != b.missile_index ) return false;
    if ( a.target.object_id != b.target.object_id || a.target.object_sequence != b.target.object_sequence ) return false;
    if ( !bits_eq_d( a.lock_start_time, b.lock_start_time ) ) return false;
    return true;
}

// ---- sample generators ---------------------------------------------------

// EXP_LEGAL_ONLY: the assert-enabled build. The writer-trusted asserts in both
// the emitted and the generic path fire on an out-of-declared-range value, so
// the assert-on leg clamps every generator into the declared range and proves
// the ASSERTS agree too; the NDEBUG leg leaves them out of range on purpose.
template <typename T> static inline T LEGAL_I32( T x, T lo, T hi )
{
#ifdef EXP_LEGAL_ONLY
    return x < lo ? lo : ( x > hi ? hi : x );
#else
    (void) lo; (void) hi; return x;
#endif
}

// mode 0: declared bounds and their neighbours   1: hostile bit patterns   2: uniform
static int32_t edge_int( Rng & r, int mode, int32_t lo, int32_t hi )
{
#ifdef EXP_LEGAL_ONLY
    if ( mode == 1 ) { return r.range( lo, hi ); }
#endif
    if ( mode == 0 )
    {
        static const int k = 8;
        switch ( r.u32() % k )
        {
            case 0: return lo;
            case 1: return hi;
            case 2: return lo + 1;
            case 3: return hi - 1;
            case 4: return 0;
            case 5: return LEGAL_I32( lo - 1, lo, hi );   // OUT OF RANGE on purpose (NDEBUG: both sides must truncate identically)
            case 6: return LEGAL_I32( hi + 1, lo, hi );   // OUT OF RANGE on purpose
            default: return r.range( lo, hi );
        }
    }
    if ( mode == 1 ) return int32_t( r.u32() );
    return r.range( lo, hi );
}

static void fill( example::TestData & v, Rng & r, int mode )
{
    v.a = edge_int( r, mode, -100, 100 );
    v.b = edge_int( r, mode, -100, 100 );
    v.c = edge_int( r, mode, -100, 150 );
    v.d = LEGAL_I32( int32_t( r.u32() ), 0, 255 ); v.e = LEGAL_I32( int32_t( r.u32() ), 0, 255 ); v.f = LEGAL_I32( int32_t( r.u32() ), 0, 255 );     // full 32-bit garbage into bits(8) fields
    v.g = ( r.u32() & 1 ) != 0;
    v.items_count = r.range( 0, 16 );                 // structural: must stay in the buffer
    for ( int i = 0; i < v.items_count; i++ ) { v.items[i] = edge_int( r, mode, 0, 255 ); }
    v.float_value = pick_float( r, mode );
    v.compressed_float_value = pick_float( r, mode );
    v.double_value = pick_double( r, mode );
    v.int8_value = int8_t( r.u32() );
    v.int16_value = int16_t( r.u32() );
    v.uint8_value = uint8_t( r.u32() );
    v.uint16_value = uint16_t( r.u32() );
    v.uint32_value = r.u32();
    v.uint64_value = r.next();
    v.int64_full = int64_t( r.next() );
    if ( mode == 0 )
    {
        static const int64_t lo = -1000000000000ll, hi = 1000000000000ll;
        switch ( r.u32() % 6 )
        {
            case 0: v.int64_range = lo; break;
            case 1: v.int64_range = hi; break;
            case 2: v.int64_range = LEGAL_I32( lo - 1, lo, hi ); break;   // OUT OF RANGE on purpose
            case 3: v.int64_range = LEGAL_I32( hi + 1, lo, hi ); break;   // OUT OF RANGE on purpose
            case 4: v.int64_range = 0; break;
            default: v.int64_range = int64_t( r.next() ) % hi; break;
        }
    }
    else if ( mode == 1 ) { v.int64_range = LEGAL_I32( int64_t( r.next() ), -1000000000000ll, 1000000000000ll ); }
    else { v.int64_range = int64_t( r.next() % 2000000000001ull ) - 1000000000000ll; }
    for ( int i = 0; i < 17; i++ ) { v.fixed_bytes[i] = uint8_t( r.u32() ); }
    v.text_length = r.range( 0, 255 );                // structural
    for ( int i = 0; i < v.text_length; i++ )
    {
        // mode 1 deliberately plants interior nulls and invalid UTF-8; the read
        // must refuse identically on both sides
#ifdef EXP_LEGAL_ONLY
        v.text[i] = char( uint8_t( 0x21 + ( r.u32() % 94 ) ) );   // printable ASCII: no interior null, valid UTF-8
#else
        v.text[i] = char( mode == 1 ? uint8_t( r.u32() ) : uint8_t( 0x61 + ( r.u32() % 26 ) ) );
#endif
    }
    v.text[v.text_length] = 0;
}

static void fill_vec( example::Vec3 & v, Rng & r, int mode )
{
    v.x = pick_double( r, mode ); v.y = pick_double( r, mode ); v.z = pick_double( r, mode );
}

static void fill( example::ShipData_Deep & v, Rng & r, int mode )
{
    v.ship_type = example::ShipType( uint8_t( LEGAL_I32( int32_t( mode == 0 ? ( r.u32() % 8 ) : ( r.u32() & 0xff ) ), 0, 5 ) ) );
    fill_vec( v.position, r, mode );
    v.rotation.x = pick_double( r, mode ); v.rotation.y = pick_double( r, mode );
    v.rotation.z = pick_double( r, mode ); v.rotation.w = pick_double( r, mode );
    fill_vec( v.linear_velocity, r, mode );
    v.flags = LEGAL_I32( int32_t( r.u32() ), 0, 15 );                                // full 64-bit garbage into a bits(4) field
    v.team = example::Team( uint8_t( LEGAL_I32( int32_t( mode == 0 ? ( r.u32() % 5 ) : ( r.u32() & 0xff ) ), 0, 2 ) ) );
    v.health = pick_float( r, mode );
    v.thrust = pick_float( r, mode );
    fill_vec( v.angular_velocity, r, mode );
    v.laser_cooldown = pick_float( r, mode );
    v.missile_cooldown = pick_float( r, mode );
    v.speed_current = pick_float( r, mode );
    v.speed_velocity = pick_float( r, mode );
    fill_vec( v.stick_current, r, mode );
    fill_vec( v.stick_velocity, r, mode );
    v.sensitivity_current = pick_float( r, mode );
    v.sensitivity_velocity = pick_float( r, mode );
    v.roll_current = pick_float( r, mode );
    v.roll_velocity = pick_float( r, mode );
    v.aim_current = pick_float( r, mode );
    v.aim_velocity = pick_float( r, mode );
    v.laser_index = int8_t( edge_int( r, mode, 0, 15 ) );
    v.missile_index = int8_t( edge_int( r, mode, 0, 15 ) );
    v.target.object_id = edge_int( r, mode, 0, int32_t( example::MaxObjects - 1 ) );
    v.target.object_sequence = uint8_t( r.u32() );
    v.lock_start_time = pick_double( r, mode );
}

// ---- the differential ----------------------------------------------------

template <typename T, typename EmitW, typename EmitR>
static void sweep( const char * label, int samples, uint64_t seed, EmitW emit_write, EmitR emit_read, int max_bytes )
{
    Rng r( seed );
    uint8_t buf_e[2048];
    uint8_t buf_g[2048];
    uint8_t buf_c[2048];

    for ( int s = 0; s < samples; s++ )
    {
        const int mode = s % 3;
        T v{};
        fill( v, r, mode );

        memset( buf_e, 0xCD, sizeof( buf_e ) );
        memset( buf_g, 0xCD, sizeof( buf_g ) );

        int ne = 0, ng = 0;
        bool we = false, wg = false;
        {
            serialize::WriteStream ws( buf_e, max_bytes );
            we = emit_write( ws, v );
            ws.Flush();
            ne = ws.GetBytesProcessed();
        }
        {
            serialize::WriteStream ws( buf_g, max_bytes );
            wg = schema_reflect::Write( ws, v );
            ws.Flush();
            ng = ws.GetBytesProcessed();
        }

        CHECK( we == wg, "write verdict differs", s );
        CHECK( ne == ng, "wire length differs", s );
        CHECK( ne == ng && memcmp( buf_e, buf_g, size_t( ne ) ) == 0, "wire bytes differ", s );

        // reads: emitted from the emitted wire, generic from the generic wire
        T re{}, rg{};
        bool oe = false, og = false;
        { serialize::ReadStream rs( buf_e, ne ); oe = emit_read( rs, re ); }
        { serialize::ReadStream rs( buf_g, ng ); og = schema_reflect::Read( rs, rg ); }
        CHECK( oe == og, "read verdict differs", s );
        CHECK( !oe || same( re, rg ), "read result bits differ", s );

        // cross: each reader over the OTHER writer's wire
        T xe{}, xg{};
        bool xoe = false, xog = false;
        { serialize::ReadStream rs( buf_g, ng ); xoe = emit_read( rs, xe ); }
        { serialize::ReadStream rs( buf_e, ne ); xog = schema_reflect::Read( rs, xg ); }
        CHECK( xoe == oe && xog == og, "cross read verdict differs", s );
        CHECK( !xoe || same( xe, re ), "cross read (emitted over generic wire) differs", s );
        CHECK( !xog || same( xg, rg ), "cross read (generic over emitted wire) differs", s );

        // hostile: corrupt one byte and re-read both ways; the verdicts and any
        // successful result must still agree
        if ( ne > 0 )
        {
            memcpy( buf_c, buf_e, size_t( ne ) );
            const int off = int( r.u32() % uint32_t( ne ) );
            buf_c[off] = uint8_t( buf_c[off] ^ ( 1u << ( r.u32() % 8 ) ) );
            T ce{}, cg{};
            bool coe = false, cog = false;
            { serialize::ReadStream rs( buf_c, ne ); coe = emit_read( rs, ce ); }
            { serialize::ReadStream rs( buf_c, ne ); cog = schema_reflect::Read( rs, cg ); }
            CHECK( coe == cog, "corrupted-wire verdict differs", s );
            CHECK( !coe || same( ce, cg ), "corrupted-wire result differs", s );
        }

        // hostile: truncate and re-read both ways
        if ( ne > 1 )
        {
            const int trunc = 1 + int( r.u32() % uint32_t( ne - 1 ) );
            T te{}, tg{};
            bool toe = false, tog = false;
            { serialize::ReadStream rs( buf_e, trunc ); toe = emit_read( rs, te ); }
            { serialize::ReadStream rs( buf_e, trunc ); tog = schema_reflect::Read( rs, tg ); }
            CHECK( toe == tog, "truncated-wire verdict differs", s );
            CHECK( !toe || same( te, tg ), "truncated-wire result differs", s );
        }
    }

    printf( "  %-16s %7d samples swept\n", label, samples );
}

int main( int argc, char ** argv )
{
    int samples = ( argc > 1 ) ? atoi( argv[1] ) : 200000;

    printf( "schema #105 experiment — wire differential: emitted per-type vs one generic function over reflection\n" );
    printf( "  build: %s asserts\n",
#ifdef NDEBUG
            "NDEBUG (writer-trusted; out-of-range values reach the arithmetic)"
#else
            "enabled"
#endif
    );

    sweep<example::TestData>( "TestData", samples, 0xC0FFEEull,
        []( serialize::WriteStream & s, const example::TestData & v ) { return example::WriteTestData( s, v ); },
        []( serialize::ReadStream & s, example::TestData & v ) { return example::ReadTestData( s, v ); },
        int( example::TestDataMaxBytes ) );

    sweep<example::ShipData_Deep>( "ShipData_Deep", samples, 0xBADF00Dull,
        []( serialize::WriteStream & s, const example::ShipData_Deep & v ) { return example::WriteShipData_Deep( s, v ); },
        []( serialize::ReadStream & s, example::ShipData_Deep & v ) { return example::ReadShipData_Deep( s, v ); },
        int( example::ShipData_DeepMaxBytes ) );

    printf( "\n  checks: %llu\n", (unsigned long long) g_checks );
    printf( "  divergences: %llu\n", (unsigned long long) g_fail );
    printf( "  %s\n", g_fail == 0 ? "WIRE IDENTICAL" : "*** WIRE DIVERGED ***" );
    return g_fail == 0 ? 0 : 1;
}
