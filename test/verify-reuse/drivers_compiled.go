package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

func verifyCPP(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "cpp", genDir); err != nil {
		return err
	}
	serDir := resolveExistingDir(repoRoot, "SERIALIZE", "../serialize", "build/serialize")
	cppSrc := `#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <string>
#include <vector>
#include "Reuse.h"
#include "ReuseWire.h"
#include "ReuseTable.h"
#include "ReuseBlock.h"

using namespace reuse;

static void die( const char * m ) { std::fprintf( stderr, "FAILED: %s\n", m ); std::exit( 1 ); }

static std::vector<uint8_t> read_all( const std::string & p ) {
    std::ifstream f( p, std::ios::binary | std::ios::ate );
    if ( !f ) die( p.c_str() );
    auto sz = f.tellg(); f.seekg( 0 );
    std::vector<uint8_t> b( static_cast<size_t>( sz ) );
    if ( sz > 0 ) f.read( reinterpret_cast<char *>( b.data() ), sz );
    return b;
}

static int load_int( const std::string & p ) {
    auto b = read_all( p );
    std::string s( reinterpret_cast<const char *>( b.data() ), b.size() );
    return std::stoi( s );
}

struct PoisonRead {
    std::vector<uint8_t> backing;
    const uint8_t * ptr;
    size_t len;
    PoisonRead( const std::vector<uint8_t> & src, size_t lead = 7, size_t tail = 13 ) : len( src.size() ) {
        backing.assign( lead + src.size() + tail, 0xFF );
        if ( !src.empty() ) std::memcpy( backing.data() + lead, src.data(), src.size() );
        ptr = backing.data() + lead;
    }
};

struct PoisonWrite {
    std::vector<uint8_t> backing;
    size_t lead;
    size_t cap;
    size_t tail;
    uint8_t * ptr;
    PoisonWrite( size_t capacity, size_t ld = 7, size_t tl = 13 ) : lead( ld ), cap( capacity ), tail( tl ) {
        backing.assign( ld + capacity + tl, 0xA5 );
        ptr = backing.data() + ld;
    }
    void verify( const char * tag ) const {
        for ( size_t i = 0; i < lead; ++i ) if ( backing[i] != 0xA5 ) die( ( std::string( "lead canary clobbered: " ) + tag ).c_str() );
        for ( size_t i = 0; i < tail; ++i ) if ( backing[lead + cap + i] != 0xA5 ) die( ( std::string( "tail canary clobbered: " ) + tag ).c_str() );
    }
};

int main( int argc, char ** argv ) {
    if ( argc < 2 ) die( "missing corpus dir" );
    std::string dir = argv[1];

    auto pkt_a = read_all( dir + "/pkt_a.bin" );
    int bits_a = load_int( dir + "/pkt_a_bits.txt" );
    auto pkt_b = read_all( dir + "/pkt_b.bin" );
    int bits_b = load_int( dir + "/pkt_b_bits.txt" );
    auto pkt_c = read_all( dir + "/pkt_c_bad.bin" );
    int bits_c = load_int( dir + "/pkt_c_bad_bits.txt" );

    auto var_a = read_all( dir + "/var_a.bin" );
    auto var_a_ret = read_all( dir + "/var_a_retain.bin" );
    auto var_a_ret_rt = read_all( dir + "/var_a_retain_rt.bin" );
    auto var_b = read_all( dir + "/var_b.bin" );
    auto var_c = read_all( dir + "/var_c_bad.bin" );

    auto ann = read_all( dir + "/announce.bin" );
    auto msg_a = read_all( dir + "/msg_a.bin" );
    auto msg_b = read_all( dir + "/msg_b.bin" );
    auto msg_c = read_all( dir + "/msg_c_bad.bin" );

    auto fix_a = read_all( dir + "/fixed_a.bin" );
    auto fix_b = read_all( dir + "/fixed_b.bin" );
    auto fix_c = read_all( dir + "/fixed_c_bad.bin" );

    auto blk_a = read_all( dir + "/block_a.bin" );
    auto cook_b = read_all( dir + "/cook_block_a.bin" );

    // 1. Packet reuse + poison-bracketed offset-8 (serialize::ReadStream/WriteStream require 4-byte word align on some platforms, test offset 8 & check!)
    // Actually serialize C++ uses unaligned/memcpy word loads — test odd offset 7!
    PacketBox pbox;
    auto check_pkt = [&]( const std::vector<uint8_t> & wire, int bits, const std::vector<uint8_t> & want, const char * tag ) {
        PoisonRead pr( wire, 7, 13 );
        // ReadStream requires buffer size multiple of 4 in serialize C++; pad slice length to multiple of 4 while bits stays exact!
        size_t rs_bytes = ( ( pr.len + 3 ) / 4 ) * 4;
        std::vector<uint8_t> padded( 7 + rs_bytes + 13, 0xFF );
        std::memset( padded.data() + 7, 0, rs_bytes );
        std::memcpy( padded.data() + 7, wire.data(), wire.size() );
        (void)rs_bytes; (void)bits;
        serialize::ReadStream rs( pr.ptr, static_cast<int64_t>( pr.len ) );
        if ( !ReadPacketBox( rs, pbox ) ) die( ( std::string( "ReadPacketBox " ) + tag ).c_str() );
        PoisonWrite pw( PacketBoxMaxBytes, 7, 13 );
        std::memset( pw.ptr, 0, PacketBoxMaxBytes );
        serialize::WriteStream ws( pw.ptr, static_cast<int>( PacketBoxMaxBytes ) );
        if ( !WritePacketBox( ws, pbox ) ) die( ( std::string( "WritePacketBox " ) + tag ).c_str() );
        ws.Flush();
        pw.verify( tag );
        int wbytes = ws.GetBytesProcessed();
        if ( static_cast<size_t>( wbytes ) != want.size() || std::memcmp( pw.ptr, want.data(), want.size() ) != 0 ) {
            die( ( std::string( "PacketBox mismatch " ) + tag ).c_str() );
        }
    };
    check_pkt( pkt_a, bits_a, pkt_a, "pkt_A1" );
    check_pkt( pkt_b, bits_b, pkt_b, "pkt_B1" );
    {
        size_t rs_bytes = ( ( pkt_c.size() + 3 ) / 4 ) * 4;
        std::vector<uint8_t> padded( 7 + rs_bytes + 13, 0xFF );
        std::memset( padded.data() + 7, 0, rs_bytes );
        std::memcpy( padded.data() + 7, pkt_c.data(), pkt_c.size() );
        (void)rs_bytes; (void)bits_c;
        PoisonRead pr_c( pkt_c, 7, 13 );
        serialize::ReadStream rs( pr_c.ptr, static_cast<int64_t>( pr_c.len ) );
        if ( ReadPacketBox( rs, pbox ) ) die( "PacketBox C should refuse" );
    }
    check_pkt( pkt_a, bits_a, pkt_a, "pkt_A2" );
    check_pkt( pkt_b, bits_b, pkt_b, "pkt_B2" );

    // 2. Form 1 Variable reuse (reusing same region) + sub-slice [7..7+n]
    std::vector<uint8_t> region( 65536, 0xCC );
    auto check_var = [&]( const std::vector<uint8_t> & wire, const std::vector<uint8_t> & want, const char * tag ) {
        PoisonRead pr( wire, 7, 13 );
        TableReport rep = {};
        const VarDoc * root = VarDocLoad( region.data(), static_cast<int64_t>( region.size() ), pr.ptr, static_cast<int64_t>( pr.len ), &rep );
        if ( !root || rep.damage_reason || rep.refused ) die( ( std::string( "VarDocLoad " ) + tag ).c_str() );
        int64_t m = VarDocMeasure( root );
        PoisonWrite pw( static_cast<size_t>( m ), 7, 13 );
        int64_t wn = VarDocSave( root, pw.ptr, m );
        pw.verify( tag );
        if ( wn != static_cast<int64_t>( want.size() ) || std::memcmp( pw.ptr, want.data(), want.size() ) != 0 ) {
            die( ( std::string( "VarDocSave mismatch " ) + tag ).c_str() );
        }
    };
    check_var( var_a, var_a, "var_A1" );
    check_var( var_b, var_b, "var_B1" );
    {
        PoisonRead pr( var_c, 7, 13 );
        TableReport rep = {};
        if ( VarDocLoad( region.data(), static_cast<int64_t>( region.size() ), pr.ptr, static_cast<int64_t>( pr.len ), &rep ) != nullptr ) {
            die( "VarDocLoad C should refuse" );
        }
    }
    check_var( var_a, var_a, "var_A2" );
    check_var( var_b, var_b, "var_B2" );

    // 3. Form 1 Retain reuse (reusing SAME region + SAME TableRetain store!)
    std::vector<uint8_t> ret_buf( 65536 );
    std::vector<TableRetain::Id> ret_ids( 4096 );
    TableRetain store = {};
    store.bytes = ret_buf.data();
    store.capacity = static_cast<int64_t>( ret_buf.size() );
    store.ids = ret_ids.data();
    store.id_capacity = static_cast<int32_t>( ret_ids.size() );
    auto check_ret = [&]( const std::vector<uint8_t> & wire, const std::vector<uint8_t> & want, bool expect_ret, const char * tag ) {
        PoisonRead pr( wire, 7, 13 );
        TableReport rep = {};
        const VarDoc * root = VarDocLoadRetain( region.data(), static_cast<int64_t>( region.size() ), pr.ptr, static_cast<int64_t>( pr.len ), &store, &rep );
        if ( !root || rep.damage_reason || rep.refused ) die( ( std::string( "VarDocLoadRetain " ) + tag ).c_str() );
        if ( expect_ret && rep.retained <= 0 ) die( "expected retained > 0" );
        if ( !expect_ret && rep.retained != 0 ) die( "expected retained == 0" );
        int64_t m = VarDocMeasureRetain( root, &store );
        PoisonWrite pw( static_cast<size_t>( m ), 7, 13 );
        int64_t wn = VarDocSaveRetain( root, &store, pw.ptr, m, &rep );
        pw.verify( tag );
        if ( wn != static_cast<int64_t>( want.size() ) || std::memcmp( pw.ptr, want.data(), want.size() ) != 0 ) {
            die( ( std::string( "VarDocSaveRetain mismatch " ) + tag ).c_str() );
        }
    };
    check_ret( var_a_ret, var_a_ret_rt, true, "ret_A1" );
    check_ret( var_b, var_b, false, "ret_B1" );
    {
        PoisonRead pr( var_c, 7, 13 );
        TableReport rep = {};
        if ( VarDocLoadRetain( region.data(), static_cast<int64_t>( region.size() ), pr.ptr, static_cast<int64_t>( pr.len ), &store, &rep ) != nullptr ) {
            die( "VarDocLoadRetain C should refuse" );
        }
    }
    check_ret( var_a_ret, var_a_ret_rt, true, "ret_A2" );
    check_ret( var_b, var_b, false, "ret_B2" );

    // 4. Form 2 Message reuse (reusing SAME batch[2] + SAME TableVocabulary)
    std::vector<TableMessageEntry> vocab_entries( 256 );
    TableVocabulary vocab( vocab_entries.data(), static_cast<int64_t>( vocab_entries.size() ) );
    {
        PoisonWrite pw_ann( static_cast<size_t>( AnnounceMeasure() ), 7, 13 );
        int64_t an = Announce( pw_ann.ptr, AnnounceMeasure() );
        pw_ann.verify( "ann_write" );
        if ( an != static_cast<int64_t>( ann.size() ) || std::memcmp( pw_ann.ptr, ann.data(), ann.size() ) != 0 ) die( "Announce mismatch" );
        PoisonRead pr_ann( ann, 7, 13 );
        TableReport rep = {};
        if ( !AnnounceRead( vocab, pr_ann.ptr, static_cast<int64_t>( pr_ann.len ), &rep ) ) die( "AnnounceRead failed" );
    }
    FixedDoc mbatch[2] = {};
    auto check_msg = [&]( const std::vector<uint8_t> & wire, int64_t want_cnt, const std::vector<uint8_t> & want, const char * tag ) {
        PoisonRead pr( wire, 7, 13 );
        TableReport rep = {};
        int64_t cnt = 2;
        if ( !FixedDocLoadMessages( mbatch, &cnt, vocab, pr.ptr, static_cast<int64_t>( pr.len ), &rep ) || cnt != want_cnt ) {
            die( ( std::string( "FixedDocLoadMessages " ) + tag ).c_str() );
        }
        TableReport wrep = {};
        int64_t m = FixedDocMeasureMessages( mbatch, cnt, &wrep );
        PoisonWrite pw( static_cast<size_t>( m ), 7, 13 );
        int64_t wn = FixedDocSaveMessages( mbatch, cnt, pw.ptr, m, &wrep );
        pw.verify( tag );
        if ( wn != static_cast<int64_t>( want.size() ) || std::memcmp( pw.ptr, want.data(), want.size() ) != 0 ) {
            die( ( std::string( "FixedDocSaveMessages mismatch " ) + tag ).c_str() );
        }
    };
    check_msg( msg_a, 2, msg_a, "msg_A1" );
    check_msg( msg_b, 1, msg_b, "msg_B1" );
    {
        PoisonRead pr( msg_c, 7, 13 );
        TableReport rep = {};
        int64_t cnt = 2;
        if ( FixedDocLoadMessages( mbatch, &cnt, vocab, pr.ptr, static_cast<int64_t>( pr.len ), &rep ) ) die( "FixedDocLoadMessages C should refuse" );
    }
    check_msg( msg_a, 2, msg_a, "msg_A2" );
    check_msg( msg_b, 1, msg_b, "msg_B2" );

    // 5. Form 3 Fixed reuse (reusing SAME fbatch[2] + plan + cache)
    FixedDoc fbatch[2] = {};
    TableFixedEntry plan[64] = {};
    TableFixedPlanCache cache = {};
    auto check_fix = [&]( const std::vector<uint8_t> & wire, int64_t want_cnt, const std::vector<uint8_t> & want, const char * tag ) {
        PoisonRead pr( wire, 7, 13 );
        TableReport rep = {};
        int64_t cnt = FixedDocFixedLoad( fbatch, 2, pr.ptr, static_cast<int64_t>( pr.len ), plan, 64, &cache, &rep );
        if ( cnt != want_cnt || rep.damage_reason || rep.refused ) die( ( std::string( "FixedDocFixedLoad " ) + tag ).c_str() );
        int64_t m = FixedDocFixedMeasure( cnt );
        PoisonWrite pw( static_cast<size_t>( m ), 7, 13 );
        int64_t wn = FixedDocFixedSave( fbatch, cnt, pw.ptr, m );
        pw.verify( tag );
        if ( wn != static_cast<int64_t>( want.size() ) || std::memcmp( pw.ptr, want.data(), want.size() ) != 0 ) {
            die( ( std::string( "FixedDocFixedSave mismatch " ) + tag ).c_str() );
        }
    };
    check_fix( fix_a, 2, fix_a, "fix_A1" );
    check_fix( fix_b, 1, fix_b, "fix_B1" );
    {
        PoisonRead pr( fix_c, 7, 13 );
        TableReport rep = {};
        if ( FixedDocFixedLoad( fbatch, 2, pr.ptr, static_cast<int64_t>( pr.len ), plan, 64, &cache, &rep ) >= 0 ) die( "FixedDocFixedLoad C should refuse" );
    }
    check_fix( fix_a, 2, fix_a, "fix_A2" );
    check_fix( fix_b, 1, fix_b, "fix_B2" );

    // 6. Form 4 Block & Cook at non-zero poison-bracketed aligned offset 64
    {
        alignas( 64 ) uint8_t aligned_blk[64 + 256];
        std::memset( aligned_blk, 0xFF, sizeof( aligned_blk ) );
        std::memcpy( aligned_blk + 64, blk_a.data(), blk_a.size() );
        BlockDocBlock::Const blk = {};
        if ( !BlockDocBlockOpen( blk, aligned_blk + 64, static_cast<int64_t>( blk_a.size() ) ) ) die( "BlockDocBlockOpen failed" );
        if ( blk.projection->a != -42 || blk.projection->b != 0x123456789ABCDEF0ULL || blk.projection->nums_count != 3 || blk.projection->perks != 3 ) {
            die( "BlockDocBlock values mismatch" );
        }

        alignas( 64 ) uint8_t aligned_cook[64 + 256];
        std::memset( aligned_cook, 0xFF, sizeof( aligned_cook ) );
        std::memcpy( aligned_cook + 64, cook_b.data(), cook_b.size() );
        const BlockDoc * cdoc = BlockDocOpen( aligned_cook + 64, cook_b.size() );
        if ( !cdoc || cdoc->a != -42 || cdoc->b != 0x123456789ABCDEF0ULL || cdoc->nums_count != 3 || cdoc->perks != 3 ) {
            die( "BlockDocOpen values mismatch" );
        }
        alignas( 64 ) uint8_t aligned_cook_out[64 + 256];
        std::memset( aligned_cook_out, 0xA5, sizeof( aligned_cook_out ) );
        if ( !BlockDocCook( *cdoc, aligned_cook_out + 64, cook_b.size(), TableByteOrder::Little ) ) die( "BlockDocCookFrom failed" );
        for ( int i = 0; i < 64; ++i ) if ( aligned_cook_out[i] != 0xA5 ) die( "cook lead canary clobbered" );
        for ( size_t i = 64 + cook_b.size(); i < sizeof( aligned_cook_out ); ++i ) if ( aligned_cook_out[i] != 0xA5 ) die( "cook tail canary clobbered" );
        if ( std::memcmp( aligned_cook_out + 64, cook_b.data(), cook_b.size() ) != 0 ) die( "BlockDocCookFrom bytes mismatch" );
    }

    std::printf( "REUSE_OK cpp 10\n" );
    return 0;
}
`
	mainCpp := filepath.Join(workDir, "main.cpp")
	binPath := filepath.Join(workDir, "runner")
	if err := os.WriteFile(mainCpp, []byte(cppSrc), 0o644); err != nil {
		return err
	}
	cxx := envOr("CXX", "g++")
	if out, err := runChecked(workDir, nil, cxx, "-std=c++17", "-O2",
		"-I"+genDir, "-I"+serDir,
		mainCpp,
		filepath.Join(genDir, "ReuseTable.cpp"),
		filepath.Join(genDir, "ReuseBlock.cpp"),
		"-o", binPath); err != nil {
		return fmt.Errorf("compile cpp: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, corpusDir)
	return requireToken(out, err, "cpp")
}

func verifyC(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "c", genDir); err != nil {
		return err
	}
	serC := resolveExistingDir(repoRoot, "SERIALIZE_C", "../serialize.c", "build/serialize.c")
	cSrc := `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdalign.h>
#include "Reuse.h"
#include "ReuseWire.h"
#include "ReuseTable.h"
#include "ReuseBlock.h"

static void die( const char * m ) { fprintf( stderr, "FAILED: %s\n", m ); exit( 1 ); }

static uint8_t * read_all( const char * dir, const char * name, size_t * out_len ) {
    char path[1024];
    snprintf( path, sizeof( path ), "%s/%s", dir, name );
    FILE * f = fopen( path, "rb" );
    if ( !f ) die( path );
    fseek( f, 0, SEEK_END );
    long sz = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * b = (uint8_t *) malloc( sz > 0 ? (size_t) sz : 1 );
    if ( sz > 0 && fread( b, 1, (size_t) sz, f ) != (size_t) sz ) die( "fread" );
    fclose( f );
    *out_len = (size_t) sz;
    return b;
}

static int load_int( const char * dir, const char * name ) {
    size_t n = 0;
    uint8_t * b = read_all( dir, name, &n );
    char buf[64] = {0};
    memcpy( buf, b, n < 63 ? n : 63 );
    free( b );
    return atoi( buf );
}

static uint8_t * make_pread( const uint8_t * src, size_t len, size_t lead, size_t tail ) {
    uint8_t * b = (uint8_t *) malloc( lead + len + tail );
    memset( b, 0xFF, lead + len + tail );
    if ( src != NULL && len > 0 ) memcpy( b + lead, src, len );
    return b;
}

static uint8_t * make_pwrite( size_t cap, size_t lead, size_t tail ) {
    uint8_t * b = (uint8_t *) malloc( lead + cap + tail );
    memset( b, 0xA5, lead + cap + tail );
    return b;
}

static void verify_pwrite( const uint8_t * b, size_t cap, size_t lead, size_t tail, const char * tag ) {
    for ( size_t i = 0; i < lead; ++i ) if ( b[i] != 0xA5 ) die( tag );
    for ( size_t i = 0; i < tail; ++i ) if ( b[lead + cap + i] != 0xA5 ) die( tag );
}

int main( int argc, char ** argv ) {
    if ( argc < 2 ) die( "missing dir" );
    const char * dir = argv[1];

    size_t pkt_a_n, pkt_b_n, pkt_c_n, var_a_n, var_ar_n, var_arrt_n, var_b_n, var_c_n;
    size_t ann_n, msg_a_n, msg_b_n, msg_c_n, fix_a_n, fix_b_n, fix_c_n, blk_a_n, cook_b_n;
    uint8_t * pkt_a = read_all( dir, "pkt_a.bin", &pkt_a_n );
    int bits_a = load_int( dir, "pkt_a_bits.txt" );
    uint8_t * pkt_b = read_all( dir, "pkt_b.bin", &pkt_b_n );
    int bits_b = load_int( dir, "pkt_b_bits.txt" );
    uint8_t * pkt_c = read_all( dir, "pkt_c_bad.bin", &pkt_c_n );
    int bits_c = load_int( dir, "pkt_c_bad_bits.txt" );

    uint8_t * var_a = read_all( dir, "var_a.bin", &var_a_n );
    uint8_t * var_ar = read_all( dir, "var_a_retain.bin", &var_ar_n );
    uint8_t * var_arrt = read_all( dir, "var_a_retain_rt.bin", &var_arrt_n );
    uint8_t * var_b = read_all( dir, "var_b.bin", &var_b_n );
    uint8_t * var_c = read_all( dir, "var_c_bad.bin", &var_c_n );

    uint8_t * ann = read_all( dir, "announce.bin", &ann_n );
    uint8_t * msg_a = read_all( dir, "msg_a.bin", &msg_a_n );
    uint8_t * msg_b = read_all( dir, "msg_b.bin", &msg_b_n );
    uint8_t * msg_c = read_all( dir, "msg_c_bad.bin", &msg_c_n );

    uint8_t * fix_a = read_all( dir, "fixed_a.bin", &fix_a_n );
    uint8_t * fix_b = read_all( dir, "fixed_b.bin", &fix_b_n );
    uint8_t * fix_c = read_all( dir, "fixed_c_bad.bin", &fix_c_n );

    uint8_t * blk_a = read_all( dir, "block_a.bin", &blk_a_n );
    uint8_t * cook_b = read_all( dir, "cook_block_a.bin", &cook_b_n );

    /* 1. Packet reuse */
    reuse_packet_box_t pbox;
    memset( &pbox, 0, sizeof( pbox ) );
    const uint8_t * pwires[4] = { pkt_a, pkt_b, pkt_a, pkt_b };
    size_t plens[4] = { pkt_a_n, pkt_b_n, pkt_a_n, pkt_b_n };
    int pbits[4] = { bits_a, bits_b, bits_a, bits_b };
    for ( int step = 0; step < 5; ++step ) {
        if ( step == 2 ) {
            size_t rsb = ( ( pkt_c_n + 3 ) / 4 ) * 4;
            uint8_t * pr = make_pread( NULL, rsb, 7, 13 );
            memset( pr + 7, 0, rsb );
            memcpy( pr + 7, pkt_c, pkt_c_n );
            serialize_read_stream_t rs;
            serialize_read_stream_init( &rs, pr + 7, (int) pkt_c_n ); (void)bits_c;
            if ( reuse_read_packet_box( &rs, &pbox ) ) die( "c pkt C should refuse" );
            free( pr );
            continue;
        }
        int idx = step < 2 ? step : step - 1;
        size_t rsb = ( ( plens[idx] + 3 ) / 4 ) * 4;
        uint8_t * pr = make_pread( NULL, rsb, 7, 13 );
        memset( pr + 7, 0, rsb );
        memcpy( pr + 7, pwires[idx], plens[idx] );
        serialize_read_stream_t rs;
        serialize_read_stream_init( &rs, pr + 7, (int) plens[idx] ); (void)pbits;
        if ( !reuse_read_packet_box( &rs, &pbox ) ) die( "c pkt read failed" );
        uint8_t * pw = make_pwrite( REUSE_PACKET_BOX_MAX_BYTES, 7, 13 );
        memset( pw + 7, 0, REUSE_PACKET_BOX_MAX_BYTES );
        serialize_write_stream_t ws;
        serialize_write_stream_init( &ws, pw + 7, REUSE_PACKET_BOX_MAX_BYTES );
        if ( !reuse_write_packet_box( &ws, &pbox ) ) die( "c pkt write failed" );
        serialize_write_flush( &ws );
        verify_pwrite( pw, REUSE_PACKET_BOX_MAX_BYTES, 7, 13, "c_pkt_w" );
        int wb = (int) serialize_write_bytes_processed( &ws );
        if ( (size_t) wb != plens[idx] || memcmp( pw + 7, pwires[idx], plens[idx] ) != 0 ) die( "c pkt mismatch" );
        free( pr ); free( pw );
    }

    /* 2. Form 1 Variable + Retain reuse */
    uint8_t region[65536];
    reuse_table_ctx_t ctx = {0};
    for ( int step = 0; step < 5; ++step ) {
        if ( step == 2 ) {
            uint8_t * pr = make_pread( var_c, var_c_n, 7, 13 );
            reuse_table_report_t rep = {0};
            if ( reuse_var_doc_load( region, sizeof( region ), pr + 7, (int64_t) var_c_n, &rep ) != NULL ) die( "c var C should refuse" );
            free( pr );
            continue;
        }
        const uint8_t * w = ( step == 0 || step == 3 ) ? var_a : var_b;
        size_t wn = ( step == 0 || step == 3 ) ? var_a_n : var_b_n;
        uint8_t * pr = make_pread( w, wn, 7, 13 );
        reuse_table_report_t rep = {0};
        const reuse_var_doc_t * root = reuse_var_doc_load( region, sizeof( region ), pr + 7, (int64_t) wn, &rep );
        if ( !root || rep.damage_reason || rep.refused ) die( "c var load" );
        uint8_t * pw = make_pwrite( wn, 7, 13 );
        int64_t sn = reuse_var_doc_save( &ctx, root, pw + 7, (int64_t) wn );
        verify_pwrite( pw, wn, 7, 13, "c_var_w" );
        if ( sn != (int64_t) wn || memcmp( pw + 7, w, wn ) != 0 ) die( "c var save mismatch" );
        free( pr ); free( pw );
    }

    uint8_t ret_buf[65536];
    reuse_table_retain_id_t ret_ids[4096];
    reuse_table_retain_t store = {0};
    store.bytes = ret_buf;
    store.capacity = sizeof( ret_buf );
    store.ids = ret_ids;
    store.id_capacity = 4096;
    for ( int step = 0; step < 5; ++step ) {
        if ( step == 2 ) {
            uint8_t * pr = make_pread( var_c, var_c_n, 7, 13 );
            reuse_table_report_t rep = {0};
            if ( reuse_var_doc_load_retain( region, sizeof( region ), pr + 7, (int64_t) var_c_n, &store, &rep ) != NULL ) die( "c ret C should refuse" );
            free( pr );
            continue;
        }
        int is_a = ( step == 0 || step == 3 );
        const uint8_t * w = is_a ? var_ar : var_b;
        size_t wn = is_a ? var_ar_n : var_b_n;
        const uint8_t * want = is_a ? var_arrt : var_b;
        size_t want_n = is_a ? var_arrt_n : var_b_n;
        uint8_t * pr = make_pread( w, wn, 7, 13 );
        reuse_table_report_t rep = {0};
        const reuse_var_doc_t * root = reuse_var_doc_load_retain( region, sizeof( region ), pr + 7, (int64_t) wn, &store, &rep );
        if ( !root || rep.damage_reason || rep.refused ) die( "c ret load" );
        if ( is_a && rep.retained <= 0 ) die( "c ret expected >0" );
        if ( !is_a && rep.retained != 0 ) die( "c ret expected 0" );
        uint8_t * pw = make_pwrite( want_n, 7, 13 );
        int64_t sn = reuse_var_doc_save_retain( root, &store, pw + 7, (int64_t) want_n, &rep );
        verify_pwrite( pw, want_n, 7, 13, "c_ret_w" );
        if ( sn != (int64_t) want_n || memcmp( pw + 7, want, want_n ) != 0 ) die( "c ret save mismatch" );
        free( pr ); free( pw );
    }

    /* 3. Form 2 Message reuse */
    reuse_table_message_entry_t ventries[256];
    reuse_table_vocabulary_t vocab = reuse_table_vocabulary( ventries, 256 );
    {
        uint8_t * pr = make_pread( ann, ann_n, 7, 13 );
        reuse_table_report_t rep = {0};
        if ( !reuse_announce_read( &vocab, pr + 7, (int64_t) ann_n, &rep ) ) die( "c announce_read" );
        free( pr );
    }
    reuse_fixed_doc_t mbatch[2];
    memset( mbatch, 0, sizeof( mbatch ) );
    for ( int step = 0; step < 5; ++step ) {
        if ( step == 2 ) {
            uint8_t * pr = make_pread( msg_c, msg_c_n, 7, 13 );
            reuse_table_report_t rep = {0};
            int64_t cnt = 2;
            if ( reuse_fixed_doc_load_messages( mbatch, &cnt, &vocab, pr + 7, (int64_t) msg_c_n, &rep ) ) die( "c msg C should refuse" );
            free( pr );
            continue;
        }
        int is_a = ( step == 0 || step == 3 );
        const uint8_t * w = is_a ? msg_a : msg_b;
        size_t wn = is_a ? msg_a_n : msg_b_n;
        int64_t want_cnt = is_a ? 2 : 1;
        uint8_t * pr = make_pread( w, wn, 7, 13 );
        reuse_table_report_t rep = {0};
        int64_t cnt = 2;
        if ( !reuse_fixed_doc_load_messages( mbatch, &cnt, &vocab, pr + 7, (int64_t) wn, &rep ) || cnt != want_cnt ) die( "c msg load" );
        reuse_table_report_t wrep = {0};
        uint8_t * pw = make_pwrite( wn, 7, 13 );
        int64_t sn = reuse_fixed_doc_save_messages( mbatch, cnt, pw + 7, (int64_t) wn, &wrep );
        verify_pwrite( pw, wn, 7, 13, "c_msg_w" );
        if ( sn != (int64_t) wn || memcmp( pw + 7, w, wn ) != 0 ) die( "c msg save mismatch" );
        free( pr ); free( pw );
    }

    /* 4. Form 3 Fixed reuse */
    reuse_fixed_doc_t fbatch[2];
    reuse_table_fixed_entry_t plan[64];
    reuse_table_fixed_plan_cache_t cache;
    memset( &cache, 0, sizeof( cache ) );
    for ( int step = 0; step < 5; ++step ) {
        if ( step == 2 ) {
            uint8_t * pr = make_pread( fix_c, fix_c_n, 7, 13 );
            reuse_table_report_t rep = {0};
            if ( reuse_fixed_doc_fixed_load( fbatch, 2, pr + 7, (int64_t) fix_c_n, plan, 64, &cache, &rep ) >= 0 ) die( "c fix C should refuse" );
            free( pr );
            continue;
        }
        int is_a = ( step == 0 || step == 3 );
        const uint8_t * w = is_a ? fix_a : fix_b;
        size_t wn = is_a ? fix_a_n : fix_b_n;
        int64_t want_cnt = is_a ? 2 : 1;
        uint8_t * pr = make_pread( w, wn, 7, 13 );
        reuse_table_report_t rep = {0};
        int64_t cnt = reuse_fixed_doc_fixed_load( fbatch, 2, pr + 7, (int64_t) wn, plan, 64, &cache, &rep );
        if ( cnt != want_cnt || rep.damage_reason || rep.refused ) die( "c fix load" );
        uint8_t * pw = make_pwrite( wn, 7, 13 );
        int64_t sn = reuse_fixed_doc_fixed_save( fbatch, cnt, pw + 7, (int64_t) wn );
        verify_pwrite( pw, wn, 7, 13, "c_fix_w" );
        if ( sn != (int64_t) wn || memcmp( pw + 7, w, wn ) != 0 ) die( "c fix save mismatch" );
        free( pr ); free( pw );
    }

    /* 5. Form 4 Block & Cook at aligned poison-bracketed offset 64 */
    alignas( 64 ) uint8_t ablk[64 + 256];
    memset( ablk, 0xFF, sizeof( ablk ) );
    memcpy( ablk + 64, blk_a, blk_a_n );
    reuse_block_doc_block_const_t blk;
    if ( !reuse_block_doc_block_open_const( &blk, ablk + 64, (int64_t) blk_a_n ) ) die( "c block open" );
    if ( blk.projection->a != -42 || blk.projection->b != 0x123456789ABCDEF0ULL || blk.projection->nums_count != 3 || blk.projection->perks != 3 ) die( "c block vals" );

    alignas( 64 ) uint8_t acook[64 + 256];
    memset( acook, 0xFF, sizeof( acook ) );
    memcpy( acook + 64, cook_b, cook_b_n );
    const reuse_block_doc_t * cdoc = reuse_block_doc_open( acook + 64, cook_b_n );
    if ( !cdoc || cdoc->a != -42 || cdoc->b != 0x123456789ABCDEF0ULL || cdoc->nums_count != 3 || cdoc->perks != 3 ) die( "c cook open" );

    alignas( 64 ) uint8_t acook_out[64 + 256];
    memset( acook_out, 0xA5, sizeof( acook_out ) );
    if ( !reuse_block_doc_cook( cdoc, acook_out + 64, cook_b_n, REUSE_TABLE_BYTE_ORDER_LITTLE ) ) die( "c cook write" );
    verify_pwrite( acook_out, cook_b_n, 64, sizeof( acook_out ) - 64 - cook_b_n, "c_cook_w" );
    if ( memcmp( acook_out + 64, cook_b, cook_b_n ) != 0 ) die( "c cook bytes" );

    printf( "REUSE_OK c 10\n" );
    return 0;
}
`
	mainC := filepath.Join(workDir, "main.c")
	binPath := filepath.Join(workDir, "runner")
	if err := os.WriteFile(mainC, []byte(cSrc), 0o644); err != nil {
		return err
	}
	cc := envOr("CC", "gcc")
	if out, err := runChecked(workDir, nil, cc, "-std=c11", "-O2", "-ffp-contract=off",
		"-I"+genDir, "-I"+serC,
		mainC,
		filepath.Join(genDir, "ReuseTable.c"),
		filepath.Join(genDir, "ReuseBlock.c"),
		filepath.Join(serC, "serialize.c"),
		"-o", binPath, "-lm"); err != nil {
		return fmt.Errorf("compile c: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, corpusDir)
	return requireToken(out, err, "c")
}

func verifyCS(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "cs", genDir); err != nil {
		return err
	}
	serCS := resolveExistingDir(repoRoot, "SERIALIZE_CS", "../serialize.cs", "build/serialize.cs")
	csproj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
    <Compile Include="gen/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, serCS, serCS)
	if err := os.WriteFile(filepath.Join(workDir, "app.csproj"), []byte(csproj), 0o644); err != nil {
		return err
	}
	csSrc := `using System;
using System.IO;
using System.Runtime.InteropServices;
using Serialize;
using R = Reuse;

internal static unsafe class Program
{
    private static void Check(bool ok, string msg) { if (!ok) throw new Exception("FAILED: " + msg); }

    private static byte[] MakePoisonRead(byte[] src, int lead = 7, int tail = 13)
    {
        byte[] b = new byte[lead + src.Length + tail];
        Array.Fill(b, (byte)0xFF);
        Buffer.BlockCopy(src, 0, b, lead, src.Length);
        return b;
    }

    private static byte[] MakePoisonWrite(int cap, int lead = 7, int tail = 13)
    {
        byte[] b = new byte[lead + cap + tail];
        Array.Fill(b, (byte)0xA5);
        return b;
    }

    private static void VerifyCanary(byte[] b, int cap, int lead, int tail, string tag)
    {
        for (int i = 0; i < lead; i++) Check(b[i] == 0xA5, "lead canary " + tag);
        for (int i = 0; i < tail; i++) Check(b[lead + cap + i] == 0xA5, "tail canary " + tag);
    }

    public static int Main(string[] args)
    {
        string dir = args[0];
        byte[] pktA = File.ReadAllBytes(Path.Combine(dir, "pkt_a.bin"));
        int bitsA = int.Parse(File.ReadAllText(Path.Combine(dir, "pkt_a_bits.txt")).Trim());
        byte[] pktB = File.ReadAllBytes(Path.Combine(dir, "pkt_b.bin"));
        int bitsB = int.Parse(File.ReadAllText(Path.Combine(dir, "pkt_b_bits.txt")).Trim());
        byte[] pktC = File.ReadAllBytes(Path.Combine(dir, "pkt_c_bad.bin"));
        int bitsC = int.Parse(File.ReadAllText(Path.Combine(dir, "pkt_c_bad_bits.txt")).Trim());

        byte[] varA = File.ReadAllBytes(Path.Combine(dir, "var_a.bin"));
        byte[] varAR = File.ReadAllBytes(Path.Combine(dir, "var_a_retain.bin"));
        byte[] varARRT = File.ReadAllBytes(Path.Combine(dir, "var_a_retain_rt.bin"));
        byte[] varB = File.ReadAllBytes(Path.Combine(dir, "var_b.bin"));
        byte[] varC = File.ReadAllBytes(Path.Combine(dir, "var_c_bad.bin"));

        byte[] ann = File.ReadAllBytes(Path.Combine(dir, "announce.bin"));
        byte[] msgA = File.ReadAllBytes(Path.Combine(dir, "msg_a.bin"));
        byte[] msgB = File.ReadAllBytes(Path.Combine(dir, "msg_b.bin"));
        byte[] msgC = File.ReadAllBytes(Path.Combine(dir, "msg_c_bad.bin"));

        byte[] fixA = File.ReadAllBytes(Path.Combine(dir, "fixed_a.bin"));
        byte[] fixB = File.ReadAllBytes(Path.Combine(dir, "fixed_b.bin"));
        byte[] fixC = File.ReadAllBytes(Path.Combine(dir, "fixed_c_bad.bin"));

        byte[] blkA = File.ReadAllBytes(Path.Combine(dir, "block_a.bin"));
        byte[] cookB = File.ReadAllBytes(Path.Combine(dir, "cook_block_a.bin"));

        // 1. Packet reuse + ArraySegment sub-slice at odd offset 7
        var pbox = new R.PacketBox();
        var sharedWs = new WriteStream(new byte[R.Schema.PacketBoxMaxBytes]);
        void RunPkt(byte[] w, int bits)
        {
            _ = bits;
            var rs = new ReadStream(w, w.Length);
            Check(R.Schema.ReadPacketBox(rs, pbox) && rs.Ok, "cs ReadPacketBox");
            sharedWs.Reset(new byte[R.Schema.PacketBoxMaxBytes]);
            Check(R.Schema.WritePacketBox(sharedWs, pbox) && sharedWs.Ok, "cs WritePacketBox");
            sharedWs.Flush();
            Check(sharedWs.Data.SequenceEqual(w), "cs pkt bytes");
        }
        RunPkt(pktA, bitsA);
        RunPkt(pktB, bitsB);
        {
            var rs = new ReadStream(pktC, pktC.Length);
            Check(!(R.Schema.ReadPacketBox(rs, pbox) && rs.Ok), "cs pkt C refuse");
        }
        RunPkt(pktA, bitsA);
        RunPkt(pktB, bitsB);

        // 2. Form 1 Variable reuse (reusing same VarDoc instance + sub-sliced Spans)
        var vdoc = new R.VarDoc();
        void RunVar(byte[] w)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            var rep = new R.TableReport();
            Check(R.Schema.VarDocLoad(vdoc, pr.AsSpan(7, w.Length), rep), "cs VarDocLoad");
            byte[] pw = MakePoisonWrite(w.Length, 7, 13);
            long sn = R.Schema.VarDocSave(vdoc, pw.AsSpan(7, w.Length));
            VerifyCanary(pw, w.Length, 7, 13, "cs_var");
            Check(sn == w.Length && pw.AsSpan(7, w.Length).SequenceEqual(w), "cs VarDocSave");
        }
        RunVar(varA);
        RunVar(varB);
        {
            byte[] pr = MakePoisonRead(varC, 7, 13);
            Check(!R.Schema.VarDocLoad(vdoc, pr.AsSpan(7, varC.Length), new R.TableReport()), "cs VarDocLoad C refuse");
        }
        RunVar(varA);
        RunVar(varB);

        // 3. Form 1 Retain reuse (reusing SAME native region + TableRetain)
        IntPtr reg = Marshal.AllocHGlobal(65536);
        IntPtr rbuf = Marshal.AllocHGlobal(65536);
        IntPtr rids = Marshal.AllocHGlobal(4096 * 16);
        try
        {
            var store = new R.TableRetain { Bytes = (byte*)rbuf, Capacity = 65536, Ids = (R.TableRetain.Id*)rids, IdCapacity = 4096 };
            void RunRet(byte[] w, byte[] want, bool expRet)
            {
                byte[] pr = MakePoisonRead(w, 7, 13);
                var rep = new R.TableReport();
                var row = R.Schema.VarDocLoadRetain(reg, 65536, pr.AsSpan(7, w.Length), ref store, rep);
                Check(row != null && !rep.Damaged && !rep.Refused, "cs VarDocLoadRetain");
                Check(expRet ? rep.Retained > 0 : rep.Retained == 0, "cs retained count");
                byte[] pw = MakePoisonWrite(want.Length, 7, 13);
                long sn = R.Schema.VarDocSaveRetain(reg, ref store, pw.AsSpan(7, want.Length), rep);
                VerifyCanary(pw, want.Length, 7, 13, "cs_ret");
                Check(sn == want.Length && pw.AsSpan(7, want.Length).SequenceEqual(want), "cs VarDocSaveRetain");
            }
            RunRet(varAR, varARRT, true);
            RunRet(varB, varB, false);
            {
                byte[] pr = MakePoisonRead(varC, 7, 13);
                var rep = new R.TableReport();
                Check(R.Schema.VarDocLoadRetain(reg, 65536, pr.AsSpan(7, varC.Length), ref store, rep) == null, "cs ret C refuse");
            }
            RunRet(varAR, varARRT, true);
            RunRet(varB, varB, false);
        }
        finally
        {
            Marshal.FreeHGlobal(reg);
            Marshal.FreeHGlobal(rbuf);
            Marshal.FreeHGlobal(rids);
        }

        // 4. Form 2 Message reuse
        var vocab = new R.TableVocabulary();
        {
            byte[] pr = MakePoisonRead(ann, 7, 13);
            Check(R.Schema.AnnounceRead(vocab, pr.AsSpan(7, ann.Length), new R.TableReport()) == R.Schema.TableWire.Verdict.Ok, "cs AnnounceRead");
        }
        var mbatch = new R.FixedDoc[] { new R.FixedDoc(), new R.FixedDoc() };
        void RunMsg(byte[] w, int wantCnt)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            var rep = new R.TableReport();
            Check(R.Schema.FixedDocLoadMessages(mbatch, pr.AsSpan(7, w.Length), vocab, rep, out int cnt) == R.Schema.TableWire.Verdict.Ok && cnt == wantCnt, "cs LoadMessages");
            var slice = cnt == 2 ? mbatch : new R.FixedDoc[] { mbatch[0] };
            byte[] pw = MakePoisonWrite(w.Length, 7, 13);
            long sn = R.Schema.FixedDocSaveMessages(slice, pw.AsSpan(7, w.Length), new R.TableReport());
            VerifyCanary(pw, w.Length, 7, 13, "cs_msg");
            Check(sn == w.Length && pw.AsSpan(7, w.Length).SequenceEqual(w), "cs SaveMessages");
        }
        RunMsg(msgA, 2);
        RunMsg(msgB, 1);
        {
            byte[] pr = MakePoisonRead(msgC, 7, 13);
            Check(R.Schema.FixedDocLoadMessages(mbatch, pr.AsSpan(7, msgC.Length), vocab, new R.TableReport(), out _) != R.Schema.TableWire.Verdict.Ok, "cs msg C refuse");
        }
        RunMsg(msgA, 2);
        RunMsg(msgB, 1);

        // 5. Form 3 Fixed reuse
        var fbatch = new R.FixedDoc[] { new R.FixedDoc(), new R.FixedDoc() };
        var plan = new R.TableFixedEntry[64];
        void RunFix(byte[] w, int wantCnt)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            var rep = new R.TableReport();
            long cnt = R.Schema.FixedDocFixedLoad(fbatch.AsSpan(), pr.AsSpan(7, w.Length), plan.AsSpan(), rep);
            Check(cnt == wantCnt && !rep.Damaged && !rep.Refused, "cs FixedLoad");
            byte[] pw = MakePoisonWrite(w.Length, 7, 13);
            long sn = R.Schema.FixedDocFixedSave(fbatch.AsSpan(0, (int)cnt), pw.AsSpan(7, w.Length));
            VerifyCanary(pw, w.Length, 7, 13, "cs_fix");
            Check(sn == w.Length && pw.AsSpan(7, w.Length).SequenceEqual(w), "cs FixedSave");
        }
        RunFix(fixA, 2);
        RunFix(fixB, 1);
        {
            byte[] pr = MakePoisonRead(fixC, 7, 13);
            Check(R.Schema.FixedDocFixedLoad(fbatch.AsSpan(), pr.AsSpan(7, fixC.Length), plan.AsSpan(), new R.TableReport()) < 0, "cs fix C refuse");
        }
        RunFix(fixA, 2);
        RunFix(fixB, 1);

        // 6. Form 4 Block (aligned +64) & Cook (Span sub-slice at odd offset +7!)
        void* rawBlk = NativeMemory.AlignedAlloc((nuint)(64 + blkA.Length + 64), 64);
        try
        {
            new Span<byte>(rawBlk, 64 + blkA.Length + 64).Fill(0xFF);
            blkA.AsSpan().CopyTo(new Span<byte>((byte*)rawBlk + 64, blkA.Length));
            Check(R.BlockDocBlock.Open(out var blk, (IntPtr)((byte*)rawBlk + 64), blkA.Length), "cs Block.Open");
            Check(blk.Projection.A == -42 && blk.Projection.B == 0x123456789ABCDEF0UL && blk.Projection.NumsCount == 3 && blk.Projection.Perks == 3, "cs Block vals");
        }
        finally
        {
            NativeMemory.AlignedFree(rawBlk);
        }
        void* rawCook = NativeMemory.AlignedAlloc((nuint)(64 + cookB.Length + 64), 64);
        try
        {
            new Span<byte>(rawCook, 64 + cookB.Length + 64).Fill(0xFF);
            cookB.AsSpan().CopyTo(new Span<byte>((byte*)rawCook + 64, cookB.Length));
            Check(R.BlockDocCook.Open(out var cook, new ReadOnlySpan<byte>((byte*)rawCook + 64, cookB.Length)), "cs Cook.Open Span +64");
            Check(cook.Root.A == -42 && cook.Root.B == 0x123456789ABCDEF0UL && cook.Root.NumsCount == 3 && cook.Root.Perks == 3, "cs Cook vals");
        }
        finally
        {
            NativeMemory.AlignedFree(rawCook);
        }

        Console.WriteLine("REUSE_OK cs 10");
        return 0;
    }
}
`
	if err := os.WriteFile(filepath.Join(workDir, "Program.cs"), []byte(csSrc), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(workDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(workDir, "app.csproj"), "--", corpusDir)
	return requireToken(out, err, "cs")
}

func verifyUnity(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "unity", genDir); err != nil {
		return err
	}
	serCS := resolveExistingDir(repoRoot, "SERIALIZE_CS", "../serialize.cs", "build/serialize.cs")
	unityProps := filepath.Join(repoRoot, "test", "unity", "UnityDialect.props")
	hostProps := filepath.Join(repoRoot, "test", "unity", "HostDialect.props")
	libDir := filepath.Join(workDir, "lib")
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	libProj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <SerializeCsRoot>%s</SerializeCsRoot>
  </PropertyGroup>
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>netstandard2.1</TargetFramework>
    <AssemblyName>reuseunity</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="../gen/*.cs" />
  </ItemGroup>
</Project>
`, serCS, unityProps)
	appProj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>net10.0</TargetFramework>
    <AssemblyName>reuse-unity-app</AssemblyName>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
    <ProjectReference Include="../lib/lib.csproj" />
  </ItemGroup>
</Project>
`, hostProps)
	if err := os.WriteFile(filepath.Join(libDir, "lib.csproj"), []byte(libProj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.csproj"), []byte(appProj), 0o644); err != nil {
		return err
	}
	uSrc := `using System;
using System.IO;
using Serialize;
using R = Reuse;

internal static class Program
{
    private static void Check(bool ok, string msg) { if (!ok) throw new Exception("FAILED: " + msg); }

    private static byte[] MakePoisonRead(byte[] src, int lead = 7, int tail = 13)
    {
        byte[] b = new byte[lead + src.Length + tail];
        Array.Fill(b, (byte)0xFF);
        Buffer.BlockCopy(src, 0, b, lead, src.Length);
        return b;
    }

    private static byte[] MakePoisonWrite(int cap, int lead = 7, int tail = 13)
    {
        byte[] b = new byte[lead + cap + tail];
        Array.Fill(b, (byte)0xA5);
        return b;
    }

    private static void VerifyCanary(byte[] b, int cap, int lead, int tail, string tag)
    {
        for (int i = 0; i < lead; i++) Check(b[i] == 0xA5, "lead canary " + tag);
        for (int i = 0; i < tail; i++) Check(b[lead + cap + i] == 0xA5, "tail canary " + tag);
    }

    public static int Main(string[] args)
    {
        string dir = args[0];
        byte[] pktA = File.ReadAllBytes(Path.Combine(dir, "pkt_a.bin"));
        int bitsA = int.Parse(File.ReadAllText(Path.Combine(dir, "pkt_a_bits.txt")).Trim());
        byte[] pktB = File.ReadAllBytes(Path.Combine(dir, "pkt_b.bin"));
        int bitsB = int.Parse(File.ReadAllText(Path.Combine(dir, "pkt_b_bits.txt")).Trim());
        byte[] pktC = File.ReadAllBytes(Path.Combine(dir, "pkt_c_bad.bin"));
        int bitsC = int.Parse(File.ReadAllText(Path.Combine(dir, "pkt_c_bad_bits.txt")).Trim());

        byte[] varA = File.ReadAllBytes(Path.Combine(dir, "var_a.bin"));
        byte[] varAR = File.ReadAllBytes(Path.Combine(dir, "var_a_retain.bin"));
        byte[] varARRT = File.ReadAllBytes(Path.Combine(dir, "var_a_retain_rt.bin"));
        byte[] varB = File.ReadAllBytes(Path.Combine(dir, "var_b.bin"));
        byte[] varC = File.ReadAllBytes(Path.Combine(dir, "var_c_bad.bin"));

        byte[] ann = File.ReadAllBytes(Path.Combine(dir, "announce.bin"));
        byte[] msgA = File.ReadAllBytes(Path.Combine(dir, "msg_a.bin"));
        byte[] msgB = File.ReadAllBytes(Path.Combine(dir, "msg_b.bin"));
        byte[] msgC = File.ReadAllBytes(Path.Combine(dir, "msg_c_bad.bin"));

        byte[] fixA = File.ReadAllBytes(Path.Combine(dir, "fixed_a.bin"));
        byte[] fixB = File.ReadAllBytes(Path.Combine(dir, "fixed_b.bin"));
        byte[] fixC = File.ReadAllBytes(Path.Combine(dir, "fixed_c_bad.bin"));

        byte[] blkA = File.ReadAllBytes(Path.Combine(dir, "block_a.bin"));
        byte[] cookB = File.ReadAllBytes(Path.Combine(dir, "cook_block_a.bin"));

        // 1. Packet reuse
        R.PacketBox pbox = default;
        var uWs = new WriteStream(new byte[R.Schema.PacketBoxMaxBytes]);
        void RunPkt(byte[] w, int bits)
        {
            _ = bits;
            var rs = new ReadStream(w, w.Length);
            Check(R.Schema.ReadPacketBox(rs, ref pbox) && rs.Ok, "unity ReadPacketBox");
            uWs.Reset(new byte[R.Schema.PacketBoxMaxBytes]);
            Check(R.Schema.WritePacketBox(uWs, ref pbox) && uWs.Ok, "unity WritePacketBox");
            uWs.Flush();
            Check(uWs.Data.SequenceEqual(w), "unity pkt bytes");
        }
        RunPkt(pktA, bitsA);
        RunPkt(pktB, bitsB);
        {
            var rs = new ReadStream(pktC, pktC.Length);
            Check(!(R.Schema.ReadPacketBox(rs, ref pbox) && rs.Ok), "unity pkt C refuse");
        }
        RunPkt(pktA, bitsA);
        RunPkt(pktB, bitsB);

        // 2. Form 1 Variable + Retain reuse (reusing ref VarDoc + TableRetain)
        R.VarDoc vdoc = R.Schema.NewVarDoc();
        void RunVar(byte[] w)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            R.Schema.TableReport rep = default;
            Check(R.Schema.VarDocLoad(ref vdoc, pr.AsSpan(7, w.Length), ref rep), "unity VarDocLoad");
            byte[] pw = MakePoisonWrite(w.Length, 7, 13);
            long sn = R.Schema.VarDocSave(ref vdoc, pw.AsSpan(7, w.Length));
            VerifyCanary(pw, w.Length, 7, 13, "unity_var");
            Check(sn == w.Length && pw.AsSpan(7, w.Length).SequenceEqual(w), "unity VarDocSave");
        }
        RunVar(varA);
        RunVar(varB);
        {
            byte[] pr = MakePoisonRead(varC, 7, 13);
            R.Schema.TableReport rep = default;
            Check(!R.Schema.VarDocLoad(ref vdoc, pr.AsSpan(7, varC.Length), ref rep), "unity VarDocLoad C refuse");
        }
        RunVar(varA);
        RunVar(varB);

        var store = new R.Schema.TableRetain(65536, 4096);
        void RunRet(byte[] w, byte[] want, bool expRet)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            R.Schema.TableReport rep = default;
            Check(R.Schema.VarDocLoadRetain(ref vdoc, pr.AsSpan(7, w.Length), store, ref rep), "unity VarDocLoadRetain");
            Check(expRet ? rep.Retained > 0 : rep.Retained == 0, "unity retained");
            byte[] pw = MakePoisonWrite(want.Length, 7, 13);
            long sn = R.Schema.VarDocSaveRetain(vdoc, store, pw.AsSpan(7, want.Length), ref rep);
            VerifyCanary(pw, want.Length, 7, 13, "unity_ret");
            Check(sn == want.Length && pw.AsSpan(7, want.Length).SequenceEqual(want), "unity VarDocSaveRetain");
        }
        RunRet(varAR, varARRT, true);
        RunRet(varB, varB, false);
        {
            byte[] pr = MakePoisonRead(varC, 7, 13);
            R.Schema.TableReport rep = default;
            Check(!R.Schema.VarDocLoadRetain(ref vdoc, pr.AsSpan(7, varC.Length), store, ref rep), "unity ret C refuse");
        }
        RunRet(varAR, varARRT, true);
        RunRet(varB, varB, false);

        // 3. Form 2 Message reuse
        var vocab = new R.Schema.TableVocabulary();
        {
            byte[] pr = MakePoisonRead(ann, 7, 13);
            R.Schema.TableReport rep = default;
            Check(R.Schema.AnnounceRead(vocab, pr.AsSpan(7, ann.Length), ref rep), "unity AnnounceRead");
        }
        var mbatch = new R.FixedDoc[] { R.Schema.NewFixedDoc(), R.Schema.NewFixedDoc() };
        void RunMsg(byte[] w, int wantCnt)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            R.Schema.TableReport rep = default;
            Check(R.Schema.FixedDocLoadMessages(mbatch.AsSpan(), pr.AsSpan(7, w.Length), vocab, ref rep, out int cnt) && cnt == wantCnt, "unity LoadMessages");
            R.Schema.TableReport wrep = default;
            byte[] pw = MakePoisonWrite(w.Length, 7, 13);
            long sn = R.Schema.FixedDocSaveMessages(mbatch.AsSpan(0, cnt), pw.AsSpan(7, w.Length), ref wrep);
            VerifyCanary(pw, w.Length, 7, 13, "unity_msg");
            Check(sn == w.Length && pw.AsSpan(7, w.Length).SequenceEqual(w), "unity SaveMessages");
        }
        RunMsg(msgA, 2);
        RunMsg(msgB, 1);
        {
            byte[] pr = MakePoisonRead(msgC, 7, 13);
            R.Schema.TableReport rep = default;
            Check(!R.Schema.FixedDocLoadMessages(mbatch.AsSpan(), pr.AsSpan(7, msgC.Length), vocab, ref rep, out _), "unity msg C refuse");
        }
        RunMsg(msgA, 2);
        RunMsg(msgB, 1);

        // 4. Form 3 Fixed reuse
        var fbatch = new R.FixedDoc[] { R.Schema.NewFixedDoc(), R.Schema.NewFixedDoc() };
        void RunFix(byte[] w, int wantCnt)
        {
            byte[] pr = MakePoisonRead(w, 7, 13);
            R.Schema.TableReport rep = default;
            long cnt = R.Schema.FixedDocFixedLoad(fbatch.AsSpan(), pr.AsSpan(7, w.Length), ref rep);
            Check(cnt == wantCnt && !rep.Damaged && !rep.Refused, "unity FixedLoad");
            byte[] pw = MakePoisonWrite(w.Length, 7, 13);
            long sn = R.Schema.FixedDocFixedSave(fbatch.AsSpan(0, (int)cnt), pw.AsSpan(7, w.Length));
            VerifyCanary(pw, w.Length, 7, 13, "unity_fix");
            Check(sn == w.Length && pw.AsSpan(7, w.Length).SequenceEqual(w), "unity FixedSave");
        }
        RunFix(fixA, 2);
        RunFix(fixB, 1);
        {
            byte[] pr = MakePoisonRead(fixC, 7, 13);
            R.Schema.TableReport rep = default;
            Check(R.Schema.FixedDocFixedLoad(fbatch.AsSpan(), pr.AsSpan(7, fixC.Length), ref rep) < 0, "unity fix C refuse");
        }
        RunFix(fixA, 2);
        RunFix(fixB, 1);

        // 5. Form 4 Block (+64) & Cook (+8)
        byte[] prBlk = MakePoisonRead(blkA, 64, 13);
        Check(R.BlockDocBlock.Open(out var blk, prBlk, 64, blkA.Length), "unity Block.Open +64");
        Check(blk.A == -42 && blk.B == 0x123456789ABCDEF0UL && blk.NumsCount == 3 && (ulong)blk.Perks == 3, "unity Block vals");

        byte[] prCook = MakePoisonRead(cookB, 8, 13);
        Check(R.BlockDocCook.Open(out var cook, prCook, 8, cookB.Length), "unity Cook.Open +8");
        var root = cook.Cursor();
        Check(root.A == -42 && root.B == 0x123456789ABCDEF0UL && (ulong)root.Perks == 3, "unity Cook vals");

        Console.WriteLine("REUSE_OK unity 10");
        return 0;
    }
}
`
	if err := os.WriteFile(filepath.Join(appDir, "Program.cs"), []byte(uSrc), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(appDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(appDir, "app.csproj"), "--", corpusDir)
	return requireToken(out, err, "unity")
}

func verifyGo(repoRoot, schemaBin, corpusDir, workDir string) error {
	appDir, err := setupGoWorkspace(repoRoot, schemaBin, workDir)
	if err != nil {
		return err
	}
	goSrc := `package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	r "reusegen"
	"github.com/mas-bandwidth/serialize.go/v2"
)

func check(ok bool, msg string) {
	if !ok {
		panic("FAILED: " + msg)
	}
}

func makePRead(src []byte, lead, tail int) ([]byte, []byte) {
	b := bytes.Repeat([]byte{0xFF}, lead+len(src)+tail)
	copy(b[lead:lead+len(src)], src)
	return b, b[lead : lead+len(src) : lead+len(src)]
}

func makePWrite(cap, lead, tail int) ([]byte, []byte) {
	b := bytes.Repeat([]byte{0xA5}, lead+cap+tail)
	return b, b[lead : lead+cap : lead+cap]
}

func verifyCanary(b []byte, cap, lead, tail int, tag string) {
	for i := range lead {
		check(b[i] == 0xA5, "lead canary "+tag)
	}
	for i := range tail {
		check(b[lead+cap+i] == 0xA5, "tail canary "+tag)
	}
}

func mustRead(dir, name string) []byte {
	d, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		panic(err)
	}
	return d
}

func main() {
	dir := os.Args[1]
	pktA := mustRead(dir, "pkt_a.bin")
	pktB := mustRead(dir, "pkt_b.bin")
	pktC := mustRead(dir, "pkt_c_bad.bin")

	varA := mustRead(dir, "var_a.bin")
	varAR := mustRead(dir, "var_a_retain.bin")
	varARRT := mustRead(dir, "var_a_retain_rt.bin")
	varB := mustRead(dir, "var_b.bin")
	varC := mustRead(dir, "var_c_bad.bin")

	ann := mustRead(dir, "announce.bin")
	msgA := mustRead(dir, "msg_a.bin")
	msgB := mustRead(dir, "msg_b.bin")
	msgC := mustRead(dir, "msg_c_bad.bin")

	fixA := mustRead(dir, "fixed_a.bin")
	fixB := mustRead(dir, "fixed_b.bin")
	fixC := mustRead(dir, "fixed_c_bad.bin")

	blkA := mustRead(dir, "block_a.bin")
	cookB := mustRead(dir, "cook_block_a.bin")

	// 1. Packet reuse + sub-slice [7 : 7+n : 7+n]
	var pbox r.PacketBox
	runPkt := func(w []byte, tag string) {
		_, s := makePRead(w, 7, 13)
		rs := serialize.NewReadStream(s)
		check(r.ReadPacketBox(rs, &pbox) == nil, "go ReadPacketBox "+tag)
		back, dst := makePWrite(r.PacketBoxMaxBytes, 7, 13)
		clear(dst)
		ws := serialize.NewWriteStream(dst)
		check(r.WritePacketBox(ws, &pbox) == nil, "go WritePacketBox "+tag)
		ws.Flush()
		verifyCanary(back, r.PacketBoxMaxBytes, 7, 13, tag)
		wb := int((ws.BitsProcessed() + 7) / 8)
		check(wb == len(w) && bytes.Equal(dst[:wb], w), "go pkt bytes "+tag)
	}
	runPkt(pktA, "pktA1")
	runPkt(pktB, "pktB1")
	{
		_, s := makePRead(pktC, 7, 13)
		check(r.ReadPacketBox(serialize.NewReadStream(s), &pbox) != nil, "go pktC refuse")
	}
	runPkt(pktA, "pktA2")
	runPkt(pktB, "pktB2")

	// 2. Form 1 Variable + Retain reuse (reusing region + TableRetain)
	region := make([]byte, 65536)
	runVar := func(w []byte, tag string) {
		_, s := makePRead(w, 7, 13)
		var rep r.TableReport
		v := r.VarDocLoad(region, s, &rep)
		check(v != nil && !rep.Refused(), "go VarDocLoad "+tag)
		back, dst := makePWrite(len(w), 7, 13)
		sn := r.VarDocSave(v, dst)
		verifyCanary(back, len(w), 7, 13, tag)
		check(int(sn) == len(w) && bytes.Equal(dst, w), "go VarDocSave "+tag)
	}
	runVar(varA, "varA1")
	runVar(varB, "varB1")
	{
		_, s := makePRead(varC, 7, 13)
		var rep r.TableReport
		check(r.VarDocLoad(region, s, &rep) == nil, "go varC refuse")
	}
	runVar(varA, "varA2")
	runVar(varB, "varB2")

	store := r.TableRetain{Bytes: make([]byte, 65536), Ids: make([]r.TableRetainID, 4096)}
	runRet := func(w, want []byte, expRet bool, tag string) {
		_, s := makePRead(w, 7, 13)
		var rep r.TableReport
		v := r.VarDocLoadRetain(region, s, &store, &rep)
		check(v != nil && !rep.Refused(), "go LoadRetain "+tag)
		check(expRet == (rep.Retained > 0), "go retained "+tag)
		back, dst := makePWrite(len(want), 7, 13)
		sn := r.VarDocSaveRetain(v, &store, dst, &rep)
		verifyCanary(back, len(want), 7, 13, tag)
		check(int(sn) == len(want) && bytes.Equal(dst, want), "go SaveRetain "+tag)
	}
	runRet(varAR, varARRT, true, "retA1")
	runRet(varB, varB, false, "retB1")
	{
		_, s := makePRead(varC, 7, 13)
		var rep r.TableReport
		check(r.VarDocLoadRetain(region, s, &store, &rep) == nil, "go retC refuse")
	}
	runRet(varAR, varARRT, true, "retA2")
	runRet(varB, varB, false, "retB2")

	// 3. Form 2 Message reuse
	vocab := r.TableVocabulary{Entries: make([]r.TableMessageEntry, 256), MaxBytes: 65536}
	{
		_, s := makePRead(ann, 7, 13)
		var rep r.TableReport
		check(r.AnnounceRead(&vocab, s, &rep), "go AnnounceRead")
	}
	var mbatch [2]r.FixedDoc
	runMsg := func(w []byte, wantCnt int64, tag string) {
		_, s := makePRead(w, 7, 13)
		var rep r.TableReport
		cnt, ok := r.FixedDocLoadMessages(mbatch[:], &vocab, s, &rep)
		check(ok && cnt == wantCnt, "go LoadMessages "+tag)
		var wrep r.TableReport
		back, dst := makePWrite(len(w), 7, 13)
		ptrs := make([]*r.FixedDoc, cnt)
		for i := range cnt {
			ptrs[i] = &mbatch[i]
		}
		sn := r.FixedDocSaveMessages(ptrs, dst, &wrep)
		verifyCanary(back, len(w), 7, 13, tag)
		check(int(sn) == len(w) && bytes.Equal(dst, w), "go SaveMessages "+tag)
	}
	runMsg(msgA, 2, "msgA1")
	runMsg(msgB, 1, "msgB1")
	{
		_, s := makePRead(msgC, 7, 13)
		var rep r.TableReport
		_, ok := r.FixedDocLoadMessages(mbatch[:], &vocab, s, &rep)
		check(!ok, "go msgC refuse")
	}
	runMsg(msgA, 2, "msgA2")
	runMsg(msgB, 1, "msgB2")

	// 4. Form 3 Fixed reuse
	var fbatch [2]r.FixedDoc
	var plan [64]r.TableFixedEntry
	runFix := func(w []byte, wantCnt int64, tag string) {
		_, s := makePRead(w, 7, 13)
		var rep r.TableReport
		cnt := r.FixedDocFixedLoad(fbatch[:], s, plan[:], &rep)
		check(cnt == wantCnt && !rep.Refused(), "go FixedLoad "+tag)
		back, dst := makePWrite(len(w), 7, 13)
		sn := r.FixedDocFixedSave(fbatch[:cnt], dst)
		verifyCanary(back, len(w), 7, 13, tag)
		check(int(sn) == len(w) && bytes.Equal(dst, w), "go FixedSave "+tag)
	}
	runFix(fixA, 2, "fixA1")
	runFix(fixB, 1, "fixB1")
	{
		_, s := makePRead(fixC, 7, 13)
		var rep r.TableReport
		check(r.FixedDocFixedLoad(fbatch[:], s, plan[:], &rep) < 0, "go fixC refuse")
	}
	runFix(fixA, 2, "fixA2")
	runFix(fixB, 1, "fixB2")

	// 5. Form 4 Block & Cook at aligned non-zero offset 64
	raw := make([]byte, 64+512)
	for i := range raw {
		raw[i] = 0xFF
	}
	off := int((-uintptr(unsafe.Pointer(&raw[0]))) & 63) + 64
	copy(raw[off:], blkA)
	var blk r.BlockDocBlock
	check(r.BlockDocBlockOpen(&blk, unsafe.Pointer(&raw[off]), int64(len(blkA))), "go BlockOpen")
	check(blk.Projection.A == -42 && blk.Projection.B == 0x123456789ABCDEF0 && blk.Projection.NumsCount == 3 && blk.Projection.Perks == 3, "go Block vals")

	for i := range raw {
		raw[i] = 0xFF
	}
	copy(raw[off:], cookB)
	var cook r.BlockDocCook
	check(r.BlockDocOpen(&cook, unsafe.Pointer(&raw[off]), int64(len(cookB))), "go CookOpen")
	check(cook.Root().A == -42 && cook.Root().B == 0x123456789ABCDEF0 && cook.Root().NumsCount == 3 && cook.Root().Perks == 3, "go Cook vals")

	fmt.Println("REUSE_OK go 10")
}
`
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), mustFmtGo(goSrc), 0o644); err != nil {
		return err
	}
	out, err := runChecked(appDir, nil, "go", "run", ".", corpusDir)
	return requireToken(out, err, "go")
}

func verifyRust(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	genSrcDir := filepath.Join(genDir, "src")
	if err := generateReuse(repoRoot, schemaBin, "", "rust", genSrcDir); err != nil {
		return err
	}
	serRS := resolveExistingDir(repoRoot, "SERIALIZE_RS", "../serialize.rs", "build/serialize.rs")
	genToml := fmt.Sprintf("[package]\nname = \"reusegen\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[features]\ndefault = [\"block\", \"cook\"]\nblock = []\ncook = []\n\n[dependencies]\nserialize = { package = \"serialize-official\", path = %q }\n", serRS)
	if err := os.WriteFile(filepath.Join(genDir, "Cargo.toml"), []byte(genToml), 0o644); err != nil {
		return err
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(filepath.Join(appDir, "src"), 0o755); err != nil {
		return err
	}
	appToml := fmt.Sprintf("[package]\nname = \"reuseapp\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\nreusegen = { path = \"../gen\" }\nserialize = { package = \"serialize-official\", path = %q }\n", serRS)
	if err := os.WriteFile(filepath.Join(appDir, "Cargo.toml"), []byte(appToml), 0o644); err != nil {
		return err
	}
	rsSrc := `use std::fs;
use std::path::Path;
use reusegen as r;
use serialize::{ReadStream, WriteStream};

fn check(ok: bool, msg: &str) {
    if !ok { panic!("FAILED: {}", msg); }
}

fn make_pread(src: &[u8], lead: usize, tail: usize) -> Vec<u8> {
    let mut b = vec![0xFFu8; lead + src.len() + tail];
    b[lead..lead + src.len()].copy_from_slice(src);
    b
}

fn make_pwrite(cap: usize, lead: usize, tail: usize) -> Vec<u8> {
    vec![0xA5u8; lead + cap + tail]
}

fn verify_canary(b: &[u8], cap: usize, lead: usize, tail: usize, tag: &str) {
    for i in 0..lead { check(b[i] == 0xA5, tag); }
    for i in 0..tail { check(b[lead + cap + i] == 0xA5, tag); }
}

fn read_int(dir: &Path, name: &str) -> usize {
    fs::read_to_string(dir.join(name)).unwrap().trim().parse().unwrap()
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let dir = Path::new(&args[1]);

    let pkt_a = fs::read(dir.join("pkt_a.bin")).unwrap();
    let bits_a = read_int(dir, "pkt_a_bits.txt");
    let pkt_b = fs::read(dir.join("pkt_b.bin")).unwrap();
    let bits_b = read_int(dir, "pkt_b_bits.txt");
    let pkt_c = fs::read(dir.join("pkt_c_bad.bin")).unwrap();
    let bits_c = read_int(dir, "pkt_c_bad_bits.txt");

    let var_a = fs::read(dir.join("var_a.bin")).unwrap();
    let var_ar = fs::read(dir.join("var_a_retain.bin")).unwrap();
    let var_arrt = fs::read(dir.join("var_a_retain_rt.bin")).unwrap();
    let var_b = fs::read(dir.join("var_b.bin")).unwrap();
    let var_c = fs::read(dir.join("var_c_bad.bin")).unwrap();

    let ann = fs::read(dir.join("announce.bin")).unwrap();
    let msg_a = fs::read(dir.join("msg_a.bin")).unwrap();
    let msg_b = fs::read(dir.join("msg_b.bin")).unwrap();
    let msg_c = fs::read(dir.join("msg_c_bad.bin")).unwrap();

    let fix_a = fs::read(dir.join("fixed_a.bin")).unwrap();
    let fix_b = fs::read(dir.join("fixed_b.bin")).unwrap();
    let fix_c = fs::read(dir.join("fixed_c_bad.bin")).unwrap();

    let blk_a = fs::read(dir.join("block_a.bin")).unwrap();
    let cook_b = fs::read(dir.join("cook_block_a.bin")).unwrap();

    // 1. Packet reuse + sub-slice &pr[7..7+w.len()]
    let mut pbox = r::PacketBox::default();
    fn run_pkt(pbox: &mut r::PacketBox, w: &[u8], tag: &str) {
        let pr = make_pread(w, 7, 13);
        let mut rs = ReadStream::new(&pr[7..7 + w.len()], w.len());
        check(r::read_packet_box(&mut rs, pbox).is_ok(), tag);
        let cap = r::PACKET_BOX_MAX_BYTES;
        let mut pw = make_pwrite(cap, 7, 13);
        pw[7..7 + cap].fill(0);
        let wb = {
            let mut ws = WriteStream::new(&mut pw[7..7 + cap]);
            check(r::write_packet_box(&mut ws, pbox).is_ok(), tag);
            ws.flush();
            ws.data().len()
        };
        verify_canary(&pw, cap, 7, 13, tag);
        check(wb == w.len() && &pw[7..7 + wb] == w, tag);
    }
    let _ = (bits_a, bits_b, bits_c);
    run_pkt(&mut pbox, &pkt_a, "rs_pktA1");
    run_pkt(&mut pbox, &pkt_b, "rs_pktB1");
    {
        let pr = make_pread(&pkt_c, 7, 13);
        let mut rs = ReadStream::new(&pr[7..7 + pkt_c.len()], pkt_c.len());
        check(r::read_packet_box(&mut rs, &mut pbox).is_err(), "rs_pktC");
    }
    run_pkt(&mut pbox, &pkt_a, "rs_pktA2");
    run_pkt(&mut pbox, &pkt_b, "rs_pktB2");

    // 2. Form 1 Variable + Retain reuse
    for w in [&var_a, &var_b] {
        let pr = make_pread(w, 7, 13);
        let mut rep = r::TableReport::default();
        let doc = r::var_doc_load(&pr[7..7 + w.len()], &mut rep).expect("rs var_doc_load");
        let out = r::var_doc_save(&doc);
        check(&out == w, "rs var_doc_save");
    }
    {
        let pr = make_pread(&var_c, 7, 13);
        let mut rep = r::TableReport::default();
        check(r::var_doc_load(&pr[7..7 + var_c.len()], &mut rep).is_none(), "rs varC refuse");
    }

    let mut store = r::TableRetain { bytes: Vec::with_capacity(65536), ids: Vec::with_capacity(4096), ..Default::default() };
    fn run_ret(store: &mut r::TableRetain, w: &[u8], want: &[u8], exp_ret: bool, tag: &str) {
        let pr = make_pread(w, 7, 13);
        let mut rep = r::TableReport::default();
        let doc = r::var_doc_load_retain(&pr[7..7 + w.len()], store, &mut rep).expect(tag);
        check(exp_ret == (rep.retained > 0), tag);
        let out = r::var_doc_save_retain(&doc, store, &mut rep);
        check(out.as_slice() == want, tag);
    }
    run_ret(&mut store, &var_ar, &var_arrt, true, "rs_retA1");
    run_ret(&mut store, &var_b, &var_b, false, "rs_retB1");
    {
        let pr = make_pread(&var_c, 7, 13);
        let mut rep = r::TableReport::default();
        check(r::var_doc_load_retain(&pr[7..7 + var_c.len()], &mut store, &mut rep).is_none(), "rs_retC");
    }
    run_ret(&mut store, &var_ar, &var_arrt, true, "rs_retA2");
    run_ret(&mut store, &var_b, &var_b, false, "rs_retB2");

    // 3. Form 2 Message reuse
    let mut ventries = vec![r::TableMessageEntry::default(); 256];
    let mut vocab = r::TableVocabulary::new(&mut ventries);
    {
        let pr = make_pread(&ann, 7, 13);
        let mut rep = r::TableReport::default();
        check(r::announce_read(&mut vocab, &pr[7..7 + ann.len()], &mut rep), "rs announce_read");
    }
    let mut mbatch = [r::fixed_doc_message_default(), r::fixed_doc_message_default()];
    fn run_msg(mbatch: &mut [r::FixedDocRow; 2], vocab: &r::TableVocabulary<'_>, w: &[u8], want_cnt: usize, tag: &str) {
        let pr = make_pread(w, 7, 13);
        let mut rep = r::TableReport::default();
        let (cnt, ok) = r::fixed_doc_load_messages(mbatch, vocab, &pr[7..7 + w.len()], &mut rep);
        check(ok && cnt == want_cnt, tag);
        let mut pw = make_pwrite(w.len(), 7, 13);
        let mut wrep = r::TableReport::default();
        let sn = r::fixed_doc_save_messages(&mbatch[..cnt], &mut pw[7..7 + w.len()], &mut wrep);
        verify_canary(&pw, w.len(), 7, 13, tag);
        check(sn == w.len() as i64 && &pw[7..7 + w.len()] == w, tag);
    }
    run_msg(&mut mbatch, &vocab, &msg_a, 2, "rs_msgA1");
    run_msg(&mut mbatch, &vocab, &msg_b, 1, "rs_msgB1");
    {
        let pr = make_pread(&msg_c, 7, 13);
        let mut rep = r::TableReport::default();
        let (_, ok) = r::fixed_doc_load_messages(&mut mbatch, &vocab, &pr[7..7 + msg_c.len()], &mut rep);
        check(!ok, "rs_msgC");
    }
    run_msg(&mut mbatch, &vocab, &msg_a, 2, "rs_msgA2");
    run_msg(&mut mbatch, &vocab, &msg_b, 1, "rs_msgB2");

    // 4. Form 3 Fixed reuse
    let mut fbatch = [r::FixedDocRow::default(), r::FixedDocRow::default()];
    let mut plan = [r::TableFixedEntry::default(); 64];
    let mut remap = [0u16; 64];
    fn run_fix(fbatch: &mut [r::FixedDocRow; 2], plan: &mut [r::TableFixedEntry; 64], remap: &mut [u16; 64], w: &[u8], want_cnt: usize, tag: &str) {
        let pr = make_pread(w, 7, 13);
        let mut rep = r::TableFixedReport::default();
        let cnt = r::fixed_doc_fixed_load(fbatch, &pr[7..7 + w.len()], plan, remap, &mut rep).expect(tag);
        check(cnt == want_cnt, tag);
        let mut pw = make_pwrite(w.len(), 7, 13);
        let sn = r::fixed_doc_fixed_save(&fbatch[..cnt], &mut pw[7..7 + w.len()]).expect(tag);
        verify_canary(&pw, w.len(), 7, 13, tag);
        check(sn == w.len() && &pw[7..7 + w.len()] == w, tag);
    }
    run_fix(&mut fbatch, &mut plan, &mut remap, &fix_a, 2, "rs_fixA1");
    run_fix(&mut fbatch, &mut plan, &mut remap, &fix_b, 1, "rs_fixB1");
    {
        let pr = make_pread(&fix_c, 7, 13);
        let mut rep = r::TableFixedReport::default();
        check(r::fixed_doc_fixed_load(&mut fbatch, &pr[7..7 + fix_c.len()], &mut plan, &mut remap, &mut rep).is_none(), "rs_fixC");
    }
    run_fix(&mut fbatch, &mut plan, &mut remap, &fix_a, 2, "rs_fixA2");
    run_fix(&mut fbatch, &mut plan, &mut remap, &fix_b, 1, "rs_fixB2");

    // 5. Form 4 Block & Cook at aligned offset +64
    #[repr(align(64))]
    struct Aligned([u8; 512]);
    let mut ablk = Aligned([0xFF; 512]);
    ablk.0[64..64 + blk_a.len()].copy_from_slice(&blk_a);
    unsafe {
        let blk = r::BlockDocBlock::open(ablk.0.as_ptr().add(64), blk_a.len() as i64).expect("rs block open");
        let p = blk.projection();
        check(p.a == -42 && p.b == 0x123456789ABCDEF0 && p.nums_count == 3 && p.perks == 3, "rs block vals");
    }
    let mut acook = Aligned([0xFF; 512]);
    acook.0[64..64 + cook_b.len()].copy_from_slice(&cook_b);
    unsafe {
        let cook = r::BlockDocCook::open(acook.0.as_ptr().add(64), cook_b.len() as u64).expect("rs cook open");
        let root = &*cook.root();
        check(root.a == -42 && root.b == 0x123456789ABCDEF0 && root.nums_count == 3 && root.perks == 3, "rs cook vals");
        let mut acook_out = Aligned([0xA5; 512]);
        check(r::block_doc_cook_from(root, &mut acook_out.0[64..64 + cook_b.len()], 1), "rs cook write");
        verify_canary(&acook_out.0, cook_b.len(), 64, 512 - 64 - cook_b.len(), "rs_cook_w");
        check(&acook_out.0[64..64 + cook_b.len()] == cook_b.as_slice(), "rs cook bytes");
    }

    println!("REUSE_OK rust 10");
}
`
	if err := os.WriteFile(filepath.Join(appDir, "src", "main.rs"), []byte(rsSrc), 0o644); err != nil {
		return err
	}
	var extraEnv []string
	if rb := os.Getenv("RUSTUP_BIN"); rb != "" {
		extraEnv = append(extraEnv, "PATH="+rb+":"+os.Getenv("PATH"))
	}
	cargoBin := resolveTool(repoRoot, "CARGO", "", "cargo")
	if rb := os.Getenv("RUSTUP_BIN"); rb != "" && fileExists(filepath.Join(rb, "cargo")) {
		cargoBin = filepath.Join(rb, "cargo")
	}
	out, err := runChecked(appDir, extraEnv, cargoBin, "run", "--quiet", "--release", "--", corpusDir)
	return requireToken(out, err, "rust")
}

func verifyJava(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "java", genDir); err != nil {
		return err
	}
	classesDir := filepath.Join(workDir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return err
	}
	javaSrc := `import java.nio.file.*;
import java.util.*;
import reuse.*;

public final class Main {
    static void check(boolean ok, String msg) {
        if (!ok) throw new RuntimeException("FAILED: " + msg);
    }

    static byte[] makePRead(byte[] src, int lead, int tail) {
        byte[] b = new byte[lead + src.length + tail];
        Arrays.fill(b, (byte) 0xFF);
        System.arraycopy(src, 0, b, lead, src.length);
        return b;
    }

    static byte[] makePWrite(int cap, int lead, int tail) {
        byte[] b = new byte[lead + cap + tail];
        Arrays.fill(b, (byte) 0xA5);
        return b;
    }

    static void verifyCanary(byte[] b, int cap, int lead, int tail, String tag) {
        for (int i = 0; i < lead; i++) check(b[i] == (byte) 0xA5, "lead canary " + tag);
        for (int i = 0; i < tail; i++) check(b[lead + cap + i] == (byte) 0xA5, "tail canary " + tag);
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args[0]);
        byte[] pktA = Files.readAllBytes(dir.resolve("pkt_a.bin"));
        int bitsA = Integer.parseInt(Files.readString(dir.resolve("pkt_a_bits.txt")).trim());
        byte[] pktB = Files.readAllBytes(dir.resolve("pkt_b.bin"));
        int bitsB = Integer.parseInt(Files.readString(dir.resolve("pkt_b_bits.txt")).trim());
        byte[] pktC = Files.readAllBytes(dir.resolve("pkt_c_bad.bin"));
        int bitsC = Integer.parseInt(Files.readString(dir.resolve("pkt_c_bad_bits.txt")).trim());

        byte[] varA = Files.readAllBytes(dir.resolve("var_a.bin"));
        byte[] varAR = Files.readAllBytes(dir.resolve("var_a_retain.bin"));
        byte[] varARRT = Files.readAllBytes(dir.resolve("var_a_retain_rt.bin"));
        byte[] varB = Files.readAllBytes(dir.resolve("var_b.bin"));
        byte[] varC = Files.readAllBytes(dir.resolve("var_c_bad.bin"));

        byte[] ann = Files.readAllBytes(dir.resolve("announce.bin"));
        byte[] msgA = Files.readAllBytes(dir.resolve("msg_a.bin"));
        byte[] msgB = Files.readAllBytes(dir.resolve("msg_b.bin"));
        byte[] msgC = Files.readAllBytes(dir.resolve("msg_c_bad.bin"));

        byte[] fixA = Files.readAllBytes(dir.resolve("fixed_a.bin"));
        byte[] fixB = Files.readAllBytes(dir.resolve("fixed_b.bin"));
        byte[] fixC = Files.readAllBytes(dir.resolve("fixed_c_bad.bin"));

        byte[] blkA = Files.readAllBytes(dir.resolve("block_a.bin"));
        byte[] cookB = Files.readAllBytes(dir.resolve("cook_block_a.bin"));

        // 1. Packet reuse + bitOffset=56 (byte offset 7) inside poison-bracketed byte[]
        Reuse.PacketBox pbox = new Reuse.PacketBox();
        Object[][] pktSteps = { {pktA, bitsA}, {pktB, bitsB}, {pktC, bitsC}, {pktA, bitsA}, {pktB, bitsB} };
        for (int s = 0; s < pktSteps.length; s++) {
            byte[] w = (byte[]) pktSteps[s][0];
            int bits = (Integer) pktSteps[s][1];
            byte[] pr = makePRead(w, 7, 13);
            if (s == 2) {
                int consumed = Reuse.readPacketBoxBits(pbox, pr, 7 * 8, 7 * 8 + bits);
                check(consumed < 0, "java pktC refuse");
                continue;
            }
            int consumed = Reuse.readPacketBoxBits(pbox, pr, 7 * 8, 7 * 8 + bits);
            check(consumed == 7 * 8 + bits, "java readPacketBoxBits");
            int wcap = (((7 * 8 + Reuse.PACKET_BOX_MAX_BITS + 63) / 64) * 8) - 7;
            byte[] pw = makePWrite(wcap, 7, 16);
            Arrays.fill(pw, 7, 7 + wcap, (byte) 0);
            int wbits = Reuse.writePacketBoxBits(pbox, pw, 7 * 8);
            verifyCanary(pw, wcap, 7, 16, "java_pkt");
            check(wbits == 7 * 8 + bits && Arrays.equals(Arrays.copyOfRange(pw, 7, 7 + w.length), w), "java writePacketBoxBits");
        }

        // 2. Form 1 Retain reuse (reusing TableRetain store + Saver)
        TableVariable.Saver saver = new TableVariable.Saver();
        for (byte[] w : new byte[][]{varA, varB}) {
            TableVariable.Report rep = new TableVariable.Report();
            VarDocVariable.Value v = VarDocVariable.load(w, rep);
            check(v != null && !rep.damaged && !rep.refused, "java VarDoc.load");
            check(Arrays.equals(VarDocVariable.save(v, saver), w), "java VarDoc.save");
        }
        check(VarDocVariable.load(varC, new TableVariable.Report()) == null, "java varC refuse");

        TableRetain store = new TableRetain(65536, 4096);
        for (int s = 0; s < 5; s++) {
            if (s == 2) {
                check(VarDocRetain.loadRetain(varC, store, new TableVariable.Report()) == null, "java retC refuse");
                continue;
            }
            boolean isA = (s == 0 || s == 3);
            byte[] w = isA ? varAR : varB;
            byte[] want = isA ? varARRT : varB;
            TableVariable.Report rep = new TableVariable.Report();
            VarDocVariable.Value v = VarDocRetain.loadRetain(w, store, rep);
            check(v != null && !rep.damaged && !rep.refused, "java loadRetain");
            check(isA ? rep.retained > 0 : rep.retained == 0, "java retained");
            check(Arrays.equals(VarDocRetain.saveRetain(v, store, rep), want), "java saveRetain");
        }

        // 3. Form 2 Message reuse (reusing batch[2] + TableVocabulary)
        TableVocabulary vocab = new TableVocabulary(); vocab.entries = new TableVocabulary.Entry[256]; for (int i = 0; i < 256; i++) vocab.entries[i] = new TableVocabulary.Entry();
        check(TableVocabulary.announceRead(vocab, ann, new TableVocabulary.Report()), "java announceRead");
        FixedDocFixed.Value[] mbatch = { new FixedDocFixed.Value(), new FixedDocFixed.Value() };
        for (int s = 0; s < 5; s++) {
            if (s == 2) {
                { TableVocabulary.Report mrepC = new TableVocabulary.Report(); check(FixedDocMessage.loadMessages(mbatch, vocab, msgC, mrepC) <= 0 && (mrepC.damaged || mrepC.refused), "java msgC refuse"); }
                continue;
            }
            boolean isA = (s == 0 || s == 3);
            byte[] w = isA ? msgA : msgB;
            int wantCnt = isA ? 2 : 1;
            int cnt = FixedDocMessage.loadMessages(mbatch, vocab, w, new TableVocabulary.Report());
            check(cnt == wantCnt, "java loadMessages");
            FixedDocFixed.Value[] sub = cnt == 2 ? mbatch : new FixedDocFixed.Value[]{ mbatch[0] };
            byte[] out = new byte[w.length];
            int sn = FixedDocMessage.saveMessages(sub, out, new TableVocabulary.Report());
            check(sn == w.length && Arrays.equals(out, w), "java saveMessages");
        }

        // 4. Form 3 Fixed reuse (reusing fbatch[2] + plan + remap)
        FixedDocFixed.Value[] fbatch = { new FixedDocFixed.Value(), new FixedDocFixed.Value() };
        TableFixed.Entry[] plan = new TableFixed.Entry[64];
        for (int i = 0; i < plan.length; i++) plan[i] = new TableFixed.Entry();
        short[] remap = new short[64];
        byte[] fimage = new byte[FixedDocFixed.bodyBytes];
        for (int s = 0; s < 5; s++) {
            if (s == 2) {
                check(FixedDocFixed.load(fbatch, 2, fixC, plan, remap, fimage, new TableFixed.Report()) < 0, "java fixC refuse");
                continue;
            }
            boolean isA = (s == 0 || s == 3);
            byte[] w = isA ? fixA : fixB;
            int wantCnt = isA ? 2 : 1;
            TableFixed.Report rep = new TableFixed.Report();
            int cnt = FixedDocFixed.load(fbatch, 2, w, plan, remap, fimage, rep);
            check(cnt == wantCnt && !rep.damaged && !rep.refused, "java Fixed.load");
            byte[] out = new byte[w.length];
            int sn = FixedDocFixed.save(fbatch, cnt, out);
            check(sn == w.length && Arrays.equals(out, w), "java Fixed.save");
        }

        // 5. Form 4 Block (+64) & Cook (+7) inside poison-bracketed byte[]
        byte[] prBlk = makePRead(blkA, 64, 13);
        BlockDocBlock blk = BlockDocBlock.open(prBlk, 64, blkA.length);
        check(blk != null && BlockDocBlock.a(blk.data(), blk.base()) == -42 && BlockDocBlock.b(blk.data(), blk.base()) == 0x123456789ABCDEF0L && BlockDocBlock.numsCount(blk.data(), blk.base()) == 3 && BlockDocBlock.perks(blk.data(), blk.base()) == 3L, "java Block.open +64");

        byte[] prCookBad = makePRead(cookB, 7, 13);
        check(BlockDocCook.open(prCookBad, 7, cookB.length) == null, "java Cook +7 unaligned refuse");
        byte[] prCook = makePRead(cookB, 8, 13);
        BlockDocCook cook = BlockDocCook.open(prCook, 8, cookB.length);
        check(cook != null, "java Cook.open +7");
        int root = cook.root();
        check(BlockDocRow.a(cook.data(), root) == -42 && BlockDocRow.b(cook.data(), root) == 0x123456789ABCDEF0L && BlockDocRow.numsCount(cook.data(), root) == 3 && BlockDocRow.perks(cook.data(), root) == 3L, "java Cook vals");

        System.out.println("REUSE_OK java 10");
    }
}
`
	mainJava := filepath.Join(workDir, "Main.java")
	if err := os.WriteFile(mainJava, []byte(javaSrc), 0o644); err != nil {
		return err
	}
	genJavaFiles, _ := filepath.Glob(filepath.Join(genDir, "*.java"))
	javac := resolveTool(repoRoot, "JAVAC", "dist/jdk-25.0.2/bin/javac", "javac")
	javaBin := resolveTool(repoRoot, "JAVA", "dist/jdk-25.0.2/bin/java", "java")
	cArgs := append([]string{"-d", classesDir, mainJava}, genJavaFiles...)
	if out, err := runChecked(workDir, nil, javac, cArgs...); err != nil {
		return fmt.Errorf("javac: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, javaBin, "-cp", classesDir, "Main", corpusDir)
	return requireToken(out, err, "java")
}

func verifyDart(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "dart", genDir); err != nil {
		return err
	}
	dartSrc := `import 'dart:io';
import 'dart:typed_data';
import 'gen/Reuse.dart';
import 'gen/ReuseWire.dart' as w;
import 'gen/ReuseFixed.dart' as fx;
import 'gen/ReuseBlock.dart';
import 'gen/ReuseCook.dart';

void check(bool ok, String msg) {
  if (!ok) throw StateError('FAILED: $msg');
}

bool bytesEq(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

(Uint8List, Uint8List) makePRead(Uint8List src, [int lead = 7, int tail = 13]) {
  final b = Uint8List(lead + src.length + tail)..fillRange(0, lead + src.length + tail, 0xFF);
  b.setRange(lead, lead + src.length, src);
  final view = Uint8List.sublistView(b, lead, lead + src.length);
  check(view.offsetInBytes == lead, 'sublistView offsetInBytes');
  return (b, view);
}

(Uint8List, Uint8List) makePWrite(int cap, [int lead = 7, int tail = 13]) {
  final b = Uint8List(lead + cap + tail)..fillRange(0, lead + cap + tail, 0xA5);
  final view = Uint8List.sublistView(b, lead, lead + cap);
  check(view.offsetInBytes == lead, 'write sublistView offsetInBytes');
  return (b, view);
}

void verifyCanary(Uint8List b, int cap, int lead, int tail, String tag) {
  for (var i = 0; i < lead; i++) {
    check(b[i] == 0xA5, 'lead canary $tag');
  }
  for (var i = 0; i < tail; i++) {
    check(b[lead + cap + i] == 0xA5, 'tail canary $tag');
  }
}

void main(List<String> args) {
  final dir = args[0];
  Uint8List rd(String n) => File('$dir/$n').readAsBytesSync();
  int ri(String n) => int.parse(File('$dir/$n').readAsStringSync().trim());

  final pktA = rd('pkt_a.bin');
  final bitsA = ri('pkt_a_bits.txt');
  final pktB = rd('pkt_b.bin');
  final bitsB = ri('pkt_b_bits.txt');
  final pktC = rd('pkt_c_bad.bin');
  final bitsC = ri('pkt_c_bad_bits.txt');

  final varA = rd('var_a.bin');
  final varAR = rd('var_a_retain.bin');
  final varARRT = rd('var_a_retain_rt.bin');
  final varB = rd('var_b.bin');
  final varC = rd('var_c_bad.bin');

  final ann = rd('announce.bin');
  final msgA = rd('msg_a.bin');
  final msgB = rd('msg_b.bin');
  final msgC = rd('msg_c_bad.bin');

  final fixA = rd('fixed_a.bin');
  final fixB = rd('fixed_b.bin');
  final fixC = rd('fixed_c_bad.bin');

  final blkA = rd('block_a.bin');
  final cookB = rd('cook_block_a.bin');

  // 1. Packet reuse + ByteData.sublistView at odd offset 7
  final pbox = PacketBox();
  void runPkt(Uint8List wBytes, int bits, String tag) {
    final (_, rv) = makePRead(wBytes, 7, 13);
    final bd = ByteData.sublistView(rv);
    check(bd.offsetInBytes == 7, 'bd offsetInBytes');
    check(readPacketBox(pbox, bd, bits), 'dart readPacketBox $tag');
    final (back, wv) = makePWrite(packetBoxMaxBytes, 7, 13);
    wv.fillRange(0, packetBoxMaxBytes, 0);
    final wb = writePacketBox(pbox, ByteData.sublistView(wv));
    verifyCanary(back, packetBoxMaxBytes, 7, 13, tag);
    check(wb == wBytes.length && bytesEq(Uint8List.sublistView(wv, 0, wb), wBytes), 'dart writePacketBox $tag');
  }
  runPkt(pktA, bitsA, 'pktA1');
  runPkt(pktB, bitsB, 'pktB1');
  {
    final (_, rv) = makePRead(pktC, 7, 13);
    check(!readPacketBox(pbox, ByteData.sublistView(rv), bitsC), 'dart pktC refuse');
  }
  runPkt(pktA, bitsA, 'pktA2');
  runPkt(pktB, bitsB, 'pktB2');

  // 2. Form 1 Variable + Retain reuse + Uint8List.sublistView at odd offset 7
  for (final wBytes in [varA, varB]) {
    final (_, rv) = makePRead(wBytes, 7, 13);
    final rep = w.TableWireReport();
    final v = w.varDocWireLoad(rv, rep);
    check(!rep.refused, 'dart varDocWireLoad');
    check(bytesEq(w.varDocWireSave(v), wBytes), 'dart varDocWireSave');
  }
  {
    final (_, rv) = makePRead(varC, 7, 13);
    final rep = w.TableWireReport();
    w.varDocWireLoad(rv, rep);
    check(rep.refused, 'dart varC refuse');
  }

  final store = w.TableRetain(65536, List<int>.filled(4096, 0));
  void runRet(Uint8List wBytes, Uint8List want, bool expRet, String tag) {
    final (_, rv) = makePRead(wBytes, 7, 13);
    final rep = w.TableWireReport();
    final v = w.varDocWireLoadRetain(rv, store, rep);
    check(!rep.refused, 'dart loadRetain $tag');
    check(expRet == (rep.retained > 0), 'dart retained $tag');
    check(bytesEq(w.varDocWireSaveRetain(v, store, rep), want), 'dart saveRetain $tag');
  }
  runRet(varAR, varARRT, true, 'retA1');
  runRet(varB, varB, false, 'retB1');
  {
    final (_, rv) = makePRead(varC, 7, 13);
    final rep = w.TableWireReport();
    w.varDocWireLoadRetain(rv, store, rep);
    check(rep.refused, 'dart retC refuse');
  }
  runRet(varAR, varARRT, true, 'retA2');
  runRet(varB, varB, false, 'retB2');

  // 3. Form 2 Message reuse
  final vocab = fx.TableVocabulary(entries: List<fx.TableMessageEntry>.generate(256, (_) => fx.TableMessageEntry(0, 0)));
  {
    final (_, rv) = makePRead(ann, 7, 13);
    check(fx.announceRead(vocab, rv, fx.TableReport()), 'dart announceRead');
  }
  final mbatch = <fx.FixedDoc>[fx.FixedDoc(), fx.FixedDoc()];
  void runMsg(Uint8List wBytes, int wantCnt, String tag) {
    final (_, rv) = makePRead(wBytes, 7, 13);
    final rep = fx.TableReport();
    final cnt = fx.fixedDocLoadMessages(mbatch, rv, vocab, rep);
    check(cnt == wantCnt && !rep.refused, 'dart loadMessages $tag');
    final (back, wv) = makePWrite(wBytes.length, 7, 13);
    final sn = fx.fixedDocSaveMessages(mbatch.sublist(0, cnt), wv, fx.TableReport());
    verifyCanary(back, wBytes.length, 7, 13, tag);
    check(sn == wBytes.length && bytesEq(wv, wBytes), 'dart saveMessages $tag');
  }
  runMsg(msgA, 2, 'msgA1');
  runMsg(msgB, 1, 'msgB1');
  {
    final (_, rv) = makePRead(msgC, 7, 13);
    final mrepC = fx.TableReport();
    check(fx.fixedDocLoadMessages(mbatch, rv, vocab, mrepC) <= 0 && mrepC.refused, 'dart msgC refuse');
  }
  runMsg(msgA, 2, 'msgA2');
  runMsg(msgB, 1, 'msgB2');

  // 4. Form 3 Fixed reuse
  final fbatch = <fx.FixedDoc>[fx.FixedDoc(), fx.FixedDoc()];
  final plan = fx.fixedDocFixedNewPlan();
  void runFix(Uint8List wBytes, int wantCnt, String tag) {
    final (_, rv) = makePRead(wBytes, 7, 13);
    final rep = fx.TableFixedReport();
    final cnt = fx.fixedDocFixedLoad(fbatch, 2, rv, rv.length, plan, rep);
    check(cnt == wantCnt && rep.refused == 0, 'dart fixedLoad $tag');
    final (back, wv) = makePWrite(wBytes.length, 7, 13);
    final sn = fx.fixedDocFixedSave(fbatch, cnt, wv);
    verifyCanary(back, wBytes.length, 7, 13, tag);
    check(sn == wBytes.length && bytesEq(wv, wBytes), 'dart fixedSave $tag');
  }
  runFix(fixA, 2, 'fixA1');
  runFix(fixB, 1, 'fixB1');
  {
    final (_, rv) = makePRead(fixC, 7, 13);
    check(fx.fixedDocFixedLoad(fbatch, 2, rv, rv.length, plan, fx.TableFixedReport()) < 0, 'dart fixC refuse');
  }
  runFix(fixA, 2, 'fixA2');
  runFix(fixB, 1, 'fixB2');

  // 5. Form 4 Block (+64) & Cook (+8)
  final (blkBack, _) = makePRead(blkA, 64, 13);
  final blk = BlockDocBlock.open(blkBack, 64, blkA.length);
  check(blk != null && blk.view.getInt32(blk.base + 24, Endian.little) == -42 && blk.view.getUint64(blk.base + 32, Endian.little) == 0x123456789ABCDEF0 && blk.view.getInt32(blk.base + 72, Endian.little) == 3 && blk.view.getUint64(blk.base + 80, Endian.little) == 3, 'dart Block.open +64');

  final (cookBack, _) = makePRead(cookB, 8, 13);
  final cook = BlockDocCook.open(cookBack, 8, cookB.length);
  check(cook != null, 'dart Cook.open +8');
  final r0 = cook!.cursor();
  check(r0.a == -42 && r0.b == 0x123456789ABCDEF0 && r0.numsAt(0) == 11 && r0.perks == 3, 'dart Cook vals');

  print('REUSE_OK dart 10');
}
`
	mainDart := filepath.Join(workDir, "main.dart")
	if err := os.WriteFile(mainDart, []byte(dartSrc), 0o644); err != nil {
		return err
	}
	dartBin := resolveTool(repoRoot, "DART", "dist/dart-sdk/bin/dart", "dart")
	out, err := runChecked(workDir, nil, dartBin, "run", mainDart, corpusDir)
	return requireToken(out, err, "dart")
}

func verifyElixir(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateReuse(repoRoot, schemaBin, "", "elixir", genDir); err != nil {
		return err
	}
	ebinDir := filepath.Join(workDir, "ebin")
	if err := os.MkdirAll(ebinDir, 0o755); err != nil {
		return err
	}
	var extraEnv []string
	beamPath := envOr("BEAM_PATH", filepath.Join(repoRoot, "dist/otp-29.0.5/bin")+":"+filepath.Join(repoRoot, "dist/elixir-1.20.4/bin"))
	extraEnv = append(extraEnv, "PATH="+beamPath+":"+os.Getenv("PATH"))
	elixircBin := resolveTool(repoRoot, "ELIXIRC", "dist/elixir-1.20.4/bin/elixirc", "elixirc")
	elixirBin := resolveTool(repoRoot, "ELIXIR", "dist/elixir-1.20.4/bin/elixir", "elixir")

	exFiles, _ := filepath.Glob(filepath.Join(genDir, "*.ex"))
	// Compile runtimes before modules that depend on them
	var ordered []string
	for _, rt := range []string{"PacketRuntime.ex", "BuildVersion.ex", "FixedRuntime.ex", "TableRuntime.ex", "BlockRuntime.ex", "CookRuntime.ex"} {
		p := filepath.Join(genDir, rt)
		if fileExists(p) {
			ordered = append(ordered, p)
		}
	}
	for _, f := range exFiles {
		found := slices.Contains(ordered, f)
		if !found {
			ordered = append(ordered, f)
		}
	}
	cArgs := append([]string{"-o", ebinDir}, ordered...)
	if out, err := runChecked(workDir, extraEnv, elixircBin, cArgs...); err != nil {
		return fmt.Errorf("elixirc: %w\n%s", err, out)
	}

	exSrc := `defmodule ReuseRunner do
  alias Reuse.Table, as: T
  alias Reuse.TableMessage, as: M

  def check(true, _msg), do: :ok
  def check(_, msg), do: raise("FAILED: #{msg}")

  def sub7(bin) do
    pad_lead = :binary.copy(<<0xFF>>, 7 + 64)
    pad_tail = :binary.copy(<<0xFF>>, 13 + 64)
    full = <<pad_lead::binary, bin::binary, pad_tail::binary>>
    :binary.part(full, byte_size(pad_lead), byte_size(bin))
  end

  def run(dir) do
    rd = fn n -> File.read!(Path.join(dir, n)) end
    ri = fn n -> rd.(n) |> String.trim() |> String.to_integer() end

    pkt_a = rd.("pkt_a.bin"); bits_a = ri.("pkt_a_bits.txt")
    pkt_b = rd.("pkt_b.bin"); bits_b = ri.("pkt_b_bits.txt")
    pkt_c = rd.("pkt_c_bad.bin"); bits_c = ri.("pkt_c_bad_bits.txt")

    var_a = rd.("var_a.bin")
    var_ar = rd.("var_a_retain.bin")
    var_arrt = rd.("var_a_retain_rt.bin")
    var_b = rd.("var_b.bin")
    var_c = rd.("var_c_bad.bin")

    ann = rd.("announce.bin")
    msg_a = rd.("msg_a.bin")
    msg_b = rd.("msg_b.bin")
    msg_c = rd.("msg_c_bad.bin")

    fix_a = rd.("fixed_a.bin")
    fix_b = rd.("fixed_b.bin")
    fix_c = rd.("fixed_c_bad.bin")

    blk_a = rd.("block_a.bin")
    cook_b = rd.("cook_block_a.bin")

    # 1. Packet sub-binary reads & round-trip
    for {w, bits} <- [{pkt_a, bits_a}, {pkt_b, bits_b}] do
      {:ok, val} = Reuse.Reuse.read_packet_box(sub7(w), bits)
      {:ok, out} = Reuse.Reuse.write_packet_box(val)
      check(out == w, "elixir pkt")
    end
    check(Reuse.Reuse.read_packet_box(sub7(pkt_c), bits_c) == :error, "elixir pkt_c refuse")

    # 2. Form 1 Variable + Retain reuse (reusing same retain map across A -> B -> C -> A -> B)
    for w <- [var_a, var_b] do
      {:ok, val, _rep} = T.variable_load("VarDoc", sub7(w))
      {:ok, out} = T.save("VarDoc", val)
      check(out == w, "elixir var")
    end
    check(match?({:error, _, _}, T.variable_load("VarDoc", sub7(var_c))), "elixir var_c refuse")

    retain0 = %{cap: 65536, id_cap: 4096, records: [], used: 0, ids: []}
    rep0 = %{damaged: false, refused: 0, unknown: 0, kind_changed: 0, out_of_range: 0, widened: 0, repeated_key: 0, retained: 0, retain_lost: 0}
    {:ok, va, rep1, retain1} = T.load_retain("VarDoc", sub7(var_ar), retain0, rep0)
    check(rep1.retained > 0, "elixir retA1 retained")
    {:ok, out_ar, _, _} = T.save_retain("VarDoc", va, retain1, rep1)
    check(out_ar == var_arrt, "elixir retA1 bytes")

    {:ok, vb, rep2, retain2} = T.load_retain("VarDoc", sub7(var_b), retain1, rep0)
    check(rep2.retained == 0, "elixir retB1 retained==0")
    {:ok, out_b, _, _} = T.save_retain("VarDoc", vb, retain2, rep2)
    check(out_b == var_b, "elixir retB1 bytes")

    check(elem(T.load_retain("VarDoc", sub7(var_c), retain2, rep0), 0) == :error, "elixir retC refuse")

    {:ok, va2, rep3, retain3} = T.load_retain("VarDoc", sub7(var_ar), retain2, rep0)
    {:ok, out_ar2, _, _} = T.save_retain("VarDoc", va2, retain3, rep3)
    check(out_ar2 == var_arrt, "elixir retA2")

    {:ok, vb2, rep4, retain4} = T.load_retain("VarDoc", sub7(var_b), retain3, rep0)
    {:ok, out_b2, _, _} = T.save_retain("VarDoc", vb2, retain4, rep4)
    check(out_b2 == var_b, "elixir retB2")

    # 3. Form 2 Message reuse (reusing vocab across A -> B -> C -> A -> B)
    {:ok, vocab} = M.announce_read(sub7(ann))
    for {w, cnt} <- [{msg_a, 2}, {msg_b, 1}, {msg_a, 2}, {msg_b, 1}] do
      {:ok, vals} = M.fixed_doc_load_messages(vocab, sub7(w))
      check(length(vals) == cnt, "elixir msg count")
      {:ok, out} = M.fixed_doc_save_messages(vocab, vals)
      check(out == w, "elixir msg bytes")
    end
    check(elem(M.fixed_doc_load_messages(vocab, sub7(msg_c)), 0) in [:error, :malformed], "elixir msgC refuse")

    # 4. Form 3 Fixed sub-binary + multi-row
    for {w, cnt} <- [{fix_a, 2}, {fix_b, 1}, {fix_a, 2}, {fix_b, 1}] do
      {:ok, vals, _} = Reuse.ReuseFixed.fixed_doc_fixed_load(sub7(w))
      check(length(vals) == cnt, "elixir fix count")
      out = Reuse.ReuseFixed.fixed_doc_fixed_save(vals)
      check(out == w, "elixir fix bytes")
    end
    check(match?({:error, _, _}, Reuse.ReuseFixed.fixed_doc_fixed_load(sub7(fix_c))), "elixir fixC refuse")

    # 5. Form 4 Block (lead=64) & Cook (lead=8) + sub-binary + unaligned refusal
    blk_pad = <<:binary.copy(<<0xFF>>, 64)::binary, blk_a::binary, :binary.copy(<<0xFF>>, 13)::binary>>
    blk_sub = :binary.part(blk_pad, 64, byte_size(blk_a))
    check(Reuse.ReuseBlock.block_open_block_doc(blk_sub, 7) == :error, "elixir Block +7 unaligned refuse")
    {:ok, blk} = Reuse.ReuseBlock.block_open_block_doc(blk_sub, 64)
    check(Reuse.BlockRuntime.int(blk.base, 24, 4) == -42 and Reuse.BlockRuntime.uint(blk.base, 32, 8) == 0x123456789ABCDEF0 and Reuse.BlockRuntime.i32(blk.base, 72) == 3, "elixir Block +64")

    cook_pad = <<:binary.copy(<<0xFF>>, 8)::binary, cook_b::binary, :binary.copy(<<0xFF>>, 13)::binary>>
    cook_sub = :binary.part(cook_pad, 8, byte_size(cook_b))
    check(Reuse.ReuseCook.cook_open_block_doc(cook_sub, 7) == :error, "elixir Cook +7 unaligned refuse")
    {:ok, cook} = Reuse.ReuseCook.cook_open_block_doc(cook_sub, 8)
    check(Reuse.ReuseCook.block_doc_node_a(cook.region, 0) == -42 and Reuse.ReuseCook.block_doc_node_b(cook.region, 0) == 0x123456789ABCDEF0 and Reuse.ReuseCook.block_doc_node_nums_count(cook.region, 0) == 3, "elixir Cook +8")

    IO.puts("REUSE_OK elixir 10")
  end
end

[dir] = System.argv()
ReuseRunner.run(dir)
`
	mainExs := filepath.Join(workDir, "main.exs")
	if err := os.WriteFile(mainExs, []byte(exSrc), 0o644); err != nil {
		return err
	}
	out, err := runChecked(workDir, extraEnv, elixirBin, "-pa", ebinDir, mainExs, corpusDir)
	return requireToken(out, err, "elixir")
}
