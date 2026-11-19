// oracle.cpp — C++ golden generator and C++ port verifier for BitPhase.schema.
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

#include "BitPhaseWire.h"

using namespace bitphase;

static void die( const char * msg )
{
    std::fprintf( stderr, "FAILED: %s\n", msg );
    std::exit( 1 );
}

static void write_file( const std::string & path, const std::vector<uint8_t> & bytes )
{
    std::ofstream f( path, std::ios::binary );
    if ( !f ) die( ( "open write " + path ).c_str() );
    if ( !bytes.empty() )
    {
        f.write( reinterpret_cast<const char *>( bytes.data() ), static_cast<std::streamsize>( bytes.size() ) );
    }
    if ( !f ) die( ( "flush write " + path ).c_str() );
}

static std::vector<uint8_t> read_file( const std::string & path )
{
    std::ifstream f( path, std::ios::binary | std::ios::ate );
    if ( !f ) die( ( "open read " + path ).c_str() );
    std::streamsize sz = f.tellg();
    f.seekg( 0, std::ios::beg );
    std::vector<uint8_t> out( static_cast<size_t>( sz ) );
    if ( sz > 0 && !f.read( reinterpret_cast<char *>( out.data() ), sz ) )
    {
        die( ( "read " + path ).c_str() );
    }
    return out;
}

static Payload make_variant( int v )
{
    Payload p{};
    if ( v == 0 )
    {
        p.flag = true;
        p.b1 = 1;
        p.b13 = 0x15A7u;
        p.b32 = 0xDEADBEEFu;
        p.b64 = 0xCAFEBABE12345678ULL;
        p.r_i32 = -321;
        p.raw_u128 = ( serialize::uint128_t( 0x89ABCDEF01234567ULL ) << 64 ) | serialize::uint128_t( 0xFEDCBA9876543210ULL );
        p.rng_i128_a = serialize::int128_t( -4321098765LL );
        p.rng_i128_b = -( serialize::int128_t( 1 ) << 95 ) - serialize::int128_t( 123456789 );
        p.f32 = -0.0f;
        p.f64 = -123456.7890125;
        p.cf = -3.25f;
        p.fx = -45 * 65536 - 32768; // -45.5 in Q16.16
        p.ufx = ( 750ULL << 32 ) | ( 1ULL << 30 ); // 750.25 in UQ32.32
        p.items_count = 3;
        p.items[0] = -32768;
        p.items[1] = 0;
        p.items[2] = 32767;
        p.outer_gate = true;
        p.inner_gate = true;
        p.branch_a = -77;
        WideArm & w = p.choice.SelectWide();
        w.lo = 0x8000000000000001ULL;
        w.hi = 0x15555u;
        const char * s = "h\xC3\xA9llo\xF0\x9F\x8C\x8D" "bitphase!"; // 19 UTF-8 bytes
        p.label_length = static_cast<int32_t>( std::strlen( s ) );
        if ( p.label_length != 19 ) die( "v0 label_length != 19" );
        std::memcpy( p.label, s, 19 );
        p.raw_length = 17;
        const uint8_t raw17[17] = { 0x00, 0xff, 0x80, 0x7f, 0x01, 0xfe, 0x55, 0xaa, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0xa5 };
        std::memcpy( p.raw, raw17, 17 );
        p.tail = 0x5u;
    }
    else if ( v == 1 )
    {
        p.flag = false;
        p.b1 = 0;
        p.b13 = 0;
        p.b32 = 1u;
        p.b64 = 0x8000000000000001ULL;
        p.r_i32 = 500;
        p.raw_u128 = serialize::uint128_t( 1 );
        p.rng_i128_a = serialize::int128_t( 5000000000LL );
        p.rng_i128_b = ( serialize::int128_t( 1 ) << 99 ) + serialize::int128_t( 42 );
        uint32_t sub_bits = 1u; // float32 min positive subnormal
        std::memcpy( &p.f32, &sub_bits, 4 );
        p.f64 = -0.0;
        p.cf = 10.0f;
        p.fx = -180 * 65536;
        p.ufx = 0ULL;
        p.items_count = 0;
        p.outer_gate = true;
        p.inner_gate = false;
        p.branch_b = 0x1FFu;
        p.choice.SelectVoidArm();
        p.label_length = 0;
        p.raw_length = 0;
        p.tail = 0x2u;
    }
    else
    {
        p.flag = true;
        p.b1 = 1;
        p.b13 = 0x1FFFu;
        p.b32 = 0xFFFFFFFFu;
        p.b64 = 0xFFFFFFFFFFFFFFFFULL;
        p.r_i32 = -500;
        p.raw_u128 = ~serialize::uint128_t( 0 );
        p.rng_i128_a = serialize::int128_t( -5000000000LL );
        p.rng_i128_b = -( serialize::int128_t( 1 ) << 100 );
        p.f32 = 3.402823466e+38f;
        p.f64 = 1.7976931348623157e+308;
        p.cf = -10.0f;
        p.fx = 180 * 65536;
        p.ufx = 1000ULL << 32;
        p.items_count = 4;
        p.items[0] = 1;
        p.items[1] = -1;
        p.items[2] = 2;
        p.items[3] = -2;
        p.outer_gate = false;
        p.branch_c = 65535u;
        TinyArm & t = p.choice.SelectTiny();
        t.v = 0x55u;
        p.label_length = 1;
        p.label[0] = 'A';
        p.raw_length = 1;
        p.raw[0] = 0x42u;
        p.tail = 0x7u;
    }
    return p;
}

template <typename T>
static std::vector<uint8_t> encode_pkt( const T & val, bool ( *write_fn )( serialize::WriteStream &, const T & ), int & out_bits )
{
    uint8_t buf[512] = {};
    serialize::WriteStream ws( buf, sizeof( buf ) );
    if ( !write_fn( ws, val ) )
    {
        die( "encode_pkt failed" );
    }
    out_bits = ws.GetBitsProcessed();
    ws.Flush();
    int nbytes = ws.GetBytesProcessed();
    return std::vector<uint8_t>( buf, buf + nbytes );
}

template <typename T>
static bool check_roundtrip( const std::vector<uint8_t> & raw, T & reuse_val,
                             bool ( *read_fn )( serialize::ReadStream &, T & ),
                             bool ( *write_fn )( serialize::WriteStream &, const T & ) )
{
    std::vector<uint8_t> padded( raw.size() + 16, 0 );
    if ( !raw.empty() ) std::memcpy( padded.data(), raw.data(), raw.size() );
    serialize::ReadStream rs( padded.data(), static_cast<int>( raw.size() ) );
    if ( !read_fn( rs, reuse_val ) )
    {
        return false;
    }
    int bits = 0;
    std::vector<uint8_t> back = encode_pkt( reuse_val, write_fn, bits );
    return back == raw;
}

template <typename T>
static bool check_refused( const std::vector<uint8_t> & raw,
                           bool ( *read_fn )( serialize::ReadStream &, T & ) )
{
    std::vector<uint8_t> padded( raw.size() + 16, 0 );
    if ( !raw.empty() ) std::memcpy( padded.data(), raw.data(), raw.size() );
    T tmp{};
    serialize::ReadStream rs( padded.data(), static_cast<int>( raw.size() ) );
    return !read_fn( rs, tmp );
}

static uint64_t shim_pattern( int k )
{
    if ( k == 0 ) return 0ULL;
    if ( k == 64 ) return ~0ULL;
    uint64_t mask = ( k == 63 ) ? 0x7FFFFFFFFFFFFFFFULL : ( ( 1ULL << k ) - 1ULL );
    return 0x5A5AA5A5C3C33C3CULL & mask;
}

template <typename T>
static void set_shim( T & pkt, int k )
{
    pkt.shim = static_cast<decltype( pkt.shim )>( shim_pattern( k ) );
}

template <>
void set_shim<Phase0>( Phase0 &, int ) {}

template <typename T>
static void emit_phase( int k, const std::string & dir, std::ostringstream & manifest,
                        int u_v0,
                        bool ( *read_fn )( serialize::ReadStream &, T & ),
                        bool ( *write_fn )( serialize::WriteStream &, const T & ) )
{
    std::string type_name = "Phase" + std::to_string( k );
    T reuse{};
    std::vector<uint8_t> v0_bytes;
    int v0_bits = 0;
    for ( int v = 0; v < 3; ++v )
    {
        T pkt{};
        set_shim( pkt, k );
        pkt.body = make_variant( v );
        int bits = 0;
        std::vector<uint8_t> bytes = encode_pkt( pkt, write_fn, bits );
        if ( !check_roundtrip( bytes, reuse, read_fn, write_fn ) )
        {
            die( ( "self roundtrip failed " + type_name ).c_str() );
        }
        std::string fname = "valid_p" + std::to_string( k ) + "_v" + std::to_string( v ) + ".bin";
        write_file( dir + "/" + fname, bytes );
        manifest << "valid\t" << type_name << "\t" << fname << "\n";
        if ( v == 0 )
        {
            v0_bytes = bytes;
            v0_bits = bits;
        }
    }

    // Verify exact bit offsets on v0:
    // After align: label (8 + 19*8 = 160 bits) + raw (8 + 17*8 = 144 bits) + tail (3 bits) = 307 bits!
    int align_end = v0_bits - 307;
    int pre_align = k + u_v0;
    if ( align_end % 8 != 0 ) die( "align_end not byte-aligned" );
    int align_pad_bits = align_end - pre_align;
    if ( align_pad_bits < 1 || align_pad_bits > 7 ) die( "unexpected align_pad_bits" );
    int trail_pad_bits = static_cast<int>( v0_bytes.size() ) * 8 - v0_bits;
    if ( trail_pad_bits < 1 || trail_pad_bits > 7 ) die( "unexpected trail_pad_bits" );

    int const_bit = pre_align - 13;    // first bit of const(0xA5, 8)
    int reserved_bit = pre_align - 3;  // middle bit of reserved(5)
    int align_bit = pre_align;         // first pad bit of align
    int trailpad_bit = v0_bits;        // first trailing stream pad bit

    auto emit_flip = [&]( const char * kind, int bit_idx ) {
        std::vector<uint8_t> bad = v0_bytes;
        bad[bit_idx >> 3] ^= static_cast<uint8_t>( 1u << ( bit_idx & 7 ) );
        if ( !check_refused( bad, read_fn ) )
        {
            die( ( std::string( "C++ failed to refuse " ) + kind + " at " + type_name ).c_str() );
        }
        std::string fname = std::string( "hostile_" ) + kind + "_p" + std::to_string( k ) + ".bin";
        write_file( dir + "/" + fname, bad );
        manifest << "refuse\t" << type_name << "\t" << fname << "\n";
    };

    emit_flip( "reserved", reserved_bit );
    emit_flip( "const", const_bit );
    emit_flip( "align", align_bit );
    emit_flip( "trailpad", trailpad_bit );

    std::vector<uint8_t> extra = v0_bytes;
    extra.push_back( 0x00u );
    if ( !check_refused( extra, read_fn ) ) die( "C++ failed to refuse trailbyte" );
    std::string extra_name = "hostile_trailbyte_p" + std::to_string( k ) + ".bin";
    write_file( dir + "/" + extra_name, extra );
    manifest << "refuse\t" << type_name << "\t" << extra_name << "\n";
}

static void generate_corpus( const std::string & dir )
{
    std::ostringstream manifest;

    // 1. 0-bit degenerate struct EmptyZero
    EmptyZero ez{};
    int ez_bits = 0;
    std::vector<uint8_t> ez_bytes = encode_pkt( ez, WriteEmptyZero, ez_bits );
    if ( ez_bits != 0 || !ez_bytes.empty() ) die( "EmptyZero not 0 bits" );
    if ( !check_roundtrip( ez_bytes, ez, ReadEmptyZero, WriteEmptyZero ) ) die( "EmptyZero roundtrip" );
    write_file( dir + "/valid_empty.bin", ez_bytes );
    manifest << "valid\tEmptyZero\tvalid_empty.bin\n";

    std::vector<uint8_t> ez_bad = { 0x00u };
    if ( !check_refused( ez_bad, ReadEmptyZero ) ) die( "EmptyZero did not refuse 1-byte pad" );
    write_file( dir + "/hostile_empty_pad.bin", ez_bad );
    manifest << "refuse\tEmptyZero\thostile_empty_pad.bin\n";

    // Compute exact u_v0 (bits before align in Payload v0):
    // In Phase0, total_bits = align_end + 307, and for Phase0 vs Phase1 vs Phase7 we can deduce u_v0 directly!
    Phase0 p0{}; p0.body = make_variant( 0 );
    Phase1 p1{}; set_shim( p1, 1 ); p1.body = make_variant( 0 );
    Phase7 p7{}; set_shim( p7, 7 ); p7.body = make_variant( 0 );
    int b0 = 0, b1 = 0, b7 = 0;
    encode_pkt( p0, WritePhase0, b0 );
    encode_pkt( p1, WritePhase1, b1 );
    encode_pkt( p7, WritePhase7, b7 );
    // Notice align_end(k) = ((k + u_v0 + 7) / 8) * 8. Find the unique u_v0 matching b0, b1, b7!
    int u_v0 = -1;
    for ( int cand = 1; cand < 2000; ++cand )
    {
        int a0 = ( ( 0 + cand + 7 ) / 8 ) * 8 + 307;
        int a1 = ( ( 1 + cand + 7 ) / 8 ) * 8 + 307;
        int a7 = ( ( 7 + cand + 7 ) / 8 ) * 8 + 307;
        if ( a0 == b0 && a1 == b1 && a7 == b7 )
        {
            // Disambiguate mod 8: if cand+0 didn't cross byte boundary from cand+1, check Phase31..63 or exact sum:
            // Wait: ((cand+7)/8)*8 vs ((cand+8)/8)*8 jumps by 8 ONLY when cand % 8 == 0!
            // Let's check exact cand across 0..7 using a 1..7 probe: in fact, we know cand % 8 == 4 -> a0=a1, a7=a0+8!
            // Wait: that holds for cand%8 in {1,2,3,4,5,6,7}! Let's compute exact u_v0 by counting or probing:
            u_v0 = cand;
            break;
        }
    }
    // Exact bit count of v0 before align:
    // 1+1+13+32+64+10+128+34+102+32+64+11+25+42 + (3+48) + (1+1+8) + (2+64+17) + 8 + 5 = 716 bits!
    u_v0 = 716;
    if ( ( ( u_v0 + 7 ) / 8 ) * 8 + 307 != b0 )
    {
        std::fprintf( stderr, "u_v0=%d gave %d, b0=%d\n", u_v0, ( ( u_v0 + 7 ) / 8 ) * 8 + 307, b0 );
        die( "u_v0 mismatch" );
    }

    emit_phase<Phase0>( 0, dir, manifest, u_v0, ReadPhase0, WritePhase0 );
    emit_phase<Phase1>( 1, dir, manifest, u_v0, ReadPhase1, WritePhase1 );
    emit_phase<Phase7>( 7, dir, manifest, u_v0, ReadPhase7, WritePhase7 );
    emit_phase<Phase31>( 31, dir, manifest, u_v0, ReadPhase31, WritePhase31 );
    emit_phase<Phase32>( 32, dir, manifest, u_v0, ReadPhase32, WritePhase32 );
    emit_phase<Phase33>( 33, dir, manifest, u_v0, ReadPhase33, WritePhase33 );
    emit_phase<Phase56>( 56, dir, manifest, u_v0, ReadPhase56, WritePhase56 );
    emit_phase<Phase63>( 63, dir, manifest, u_v0, ReadPhase63, WritePhase63 );

    std::string mstr = manifest.str();
    std::vector<uint8_t> mbytes( mstr.begin(), mstr.end() );
    write_file( dir + "/manifest.tsv", mbytes );
    std::printf( "GENERATED 66 vectors in %s\n", dir.c_str() );
}

static void verify_corpus( const std::string & dir )
{
    std::vector<uint8_t> mraw = read_file( dir + "/manifest.tsv" );
    std::istringstream in( std::string( mraw.begin(), mraw.end() ) );
    EmptyZero r_ez{};
    Phase0 r0{}; Phase1 r1{}; Phase7 r7{}; Phase31 r31{};
    Phase32 r32{}; Phase33 r33{}; Phase56 r56{}; Phase63 r63{};
    int count = 0;
    std::string kind, type_name, fname;
    while ( in >> kind >> type_name >> fname )
    {
        std::vector<uint8_t> raw = read_file( dir + "/" + fname );
        bool ok = false;
        if ( kind == "valid" )
        {
            if ( type_name == "EmptyZero" ) ok = check_roundtrip( raw, r_ez, ReadEmptyZero, WriteEmptyZero );
            else if ( type_name == "Phase0" ) ok = check_roundtrip( raw, r0, ReadPhase0, WritePhase0 );
            else if ( type_name == "Phase1" ) ok = check_roundtrip( raw, r1, ReadPhase1, WritePhase1 );
            else if ( type_name == "Phase7" ) ok = check_roundtrip( raw, r7, ReadPhase7, WritePhase7 );
            else if ( type_name == "Phase31" ) ok = check_roundtrip( raw, r31, ReadPhase31, WritePhase31 );
            else if ( type_name == "Phase32" ) ok = check_roundtrip( raw, r32, ReadPhase32, WritePhase32 );
            else if ( type_name == "Phase33" ) ok = check_roundtrip( raw, r33, ReadPhase33, WritePhase33 );
            else if ( type_name == "Phase56" ) ok = check_roundtrip( raw, r56, ReadPhase56, WritePhase56 );
            else if ( type_name == "Phase63" ) ok = check_roundtrip( raw, r63, ReadPhase63, WritePhase63 );
        }
        else
        {
            if ( type_name == "EmptyZero" ) ok = check_refused( raw, ReadEmptyZero );
            else if ( type_name == "Phase0" ) ok = check_refused( raw, ReadPhase0 );
            else if ( type_name == "Phase1" ) ok = check_refused( raw, ReadPhase1 );
            else if ( type_name == "Phase7" ) ok = check_refused( raw, ReadPhase7 );
            else if ( type_name == "Phase31" ) ok = check_refused( raw, ReadPhase31 );
            else if ( type_name == "Phase32" ) ok = check_refused( raw, ReadPhase32 );
            else if ( type_name == "Phase33" ) ok = check_refused( raw, ReadPhase33 );
            else if ( type_name == "Phase56" ) ok = check_refused( raw, ReadPhase56 );
            else if ( type_name == "Phase63" ) ok = check_refused( raw, ReadPhase63 );
        }
        if ( !ok )
        {
            die( ( "cpp failed " + kind + " " + type_name + " " + fname ).c_str() );
        }
        count++;
    }
    if ( count != 66 ) die( "unexpected manifest count" );
    std::printf( "BITPHASE_OK cpp %d\n", count );
}

int main( int argc, char ** argv )
{
    if ( argc != 3 ) die( "usage: oracle <--generate|--verify> <corpus_dir>" );
    std::string mode = argv[1];
    std::string dir = argv[2];
    if ( mode == "--generate" ) generate_corpus( dir );
    else if ( mode == "--verify" ) verify_corpus( dir );
    else die( "unknown mode" );
    return 0;
}
