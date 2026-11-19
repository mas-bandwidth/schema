package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func verifyCPP(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "cpp", genDir); err != nil {
		return err
	}
	serDir := resolveExistingDir(repoRoot, "SERIALIZE", "../serialize", "build/serialize")
	cppSrc := `#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <string>
#include <vector>
#include "CanonicalTable.h"

using namespace canonical;

static void die( const char * m ) { std::fprintf( stderr, "FAILED: %s\n", m ); std::exit( 1 ); }

static std::vector<uint8_t> read_all( const std::string & p ) {
    std::ifstream f( p, std::ios::binary | std::ios::ate );
    if ( !f ) die( p.c_str() );
    auto sz = f.tellg(); f.seekg( 0 );
    std::vector<uint8_t> b( static_cast<size_t>( sz ) );
    if ( sz > 0 ) f.read( reinterpret_cast<char *>( b.data() ), sz );
    return b;
}

static void check_builder( BundleBuilder & b, const std::vector<uint8_t> & want_wire, const std::vector<uint8_t> & want_json, const char * tag ) {
    std::vector<uint8_t> wbuf( 4096 );
    int64_t wn = BundleSave( b, wbuf.data(), static_cast<int64_t>( wbuf.size() ) );
    if ( wn != static_cast<int64_t>( want_wire.size() ) || std::memcmp( wbuf.data(), want_wire.data(), want_wire.size() ) != 0 ) {
        die( ( std::string( "wire mismatch " ) + tag ).c_str() );
    }
    if ( !b.Lock() ) die( "Lock failed" );
    std::vector<char> jbuf( 8192 );
    int64_t jn = BundleToJson( b.AsConst(), jbuf.data(), static_cast<int64_t>( jbuf.size() ) );
    if ( jn != static_cast<int64_t>( want_json.size() ) || std::memcmp( jbuf.data(), want_json.data(), want_json.size() ) != 0 ) {
        die( ( std::string( "json mismatch " ) + tag ).c_str() );
    }
}

static void populate_native( BundleBuilder & b, int mode ) {
    Bundle * r = b.GetRoot();
    struct KVStr { const char * k; int32_t v; };
    const KVStr by_name[8] = {
        { "", 10 }, { "Z", -5 }, { "a", 20 }, { "ab", 0 }, { "abc", -7 },
        { "long-unbounded-key-alpha", 12345 }, { "\xEF\xBD\xA1", 111 }, { "\xF0\x9F\x98\x80", 222 }
    };
    const int ord8[3][8] = { {0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3} };
    for ( int i = 0; i < 8; ++i ) {
        const auto & e = by_name[ord8[mode][i]];
        *BundleByNameInsert( b.main, r->by_name, e.k ) = e.v;
    }

    const KVStr by_code[5] = { { "Alpha", 1 }, { "a", 5 }, { "ab", 4 }, { "beta", 2 }, { "gamma", 3 } };
    const int ord5[3][5] = { {0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1} };
    for ( int i = 0; i < 5; ++i ) {
        const auto & e = by_code[ord5[mode][i]];
        *BundleByCodeInsert( b.main, r->by_code, e.k ) = e.v;
    }

    struct KVId { int64_t k; const char * lbl; int32_t sc; };
    const KVId by_id[5] = {
        { -9000000000000LL, "min-far", -99 }, { -1LL, "minus-one", -1 }, { 0LL, "", 0 },
        { 2LL, "two", 2 }, { 9000000000000LL, "max-far", 99 }
    };
    const int ord5b[3][5] = { {0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1} };
    for ( int i = 0; i < 5; ++i ) {
        const auto & e = by_id[ord5b[mode][i]];
        Child * c = BundleByIdInsert( b.main, r->by_id, e.k );
        c->label_length = static_cast<int32_t>( std::strlen( e.lbl ) );
        std::memcpy( c->label, e.lbl, static_cast<size_t>( c->label_length ) );
        c->score = e.sc;
    }

    struct KVUid { uint64_t k; uint32_t v; };
    const KVUid by_uid[7] = {
        { 0ULL, 0 }, { 1ULL, 11 }, { 255ULL, 2550 }, { 256ULL, 2560 },
        { 9223372036854775807ULL, 700 }, { 9223372036854775808ULL, 800 }, { 18446744073709551614ULL, 900 }
    };
    const int ord7[3][7] = { {0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1} };
    for ( int i = 0; i < 7; ++i ) {
        const auto & e = by_uid[ord7[mode][i]];
        *BundleByUidInsert( b.main, r->by_uid, e.k ) = e.v;
    }

    r->by_tier[Tier::Bronze] = 100;
    r->by_tier[Tier::Silver] = 200;
    r->by_tier[Tier::Gold] = 300;
    r->by_tier[Tier::Platinum] = 400;

    const char * outer_keys[3] = { "outer-a", "outer-m", "outer-z" };
    const int ord3[3][3] = { {0,1,2}, {2,1,0}, {1,2,0} };
    for ( int i = 0; i < 3; ++i ) {
        int idx = ord3[mode][i];
        auto * inner = BundleNestedInsert( b.main, r->nested, outer_keys[idx] );
        if ( idx == 0 ) {
            struct KVI { int32_t k, v; } items[4] = { {-100,-10}, {0,0}, {7,70}, {42,420} };
            int io[3][4] = { {0,1,2,3}, {3,2,1,0}, {2,0,3,1} };
            for ( int j = 0; j < 4; ++j ) *BundleNestedEntryValueInsert( b.main, *inner, items[io[mode][j]].k ) = items[io[mode][j]].v;
        } else if ( idx == 1 ) {
            if ( mode == 0 ) { *BundleNestedEntryValueInsert( b.main, *inner, -5 ) = 55; *BundleNestedEntryValueInsert( b.main, *inner, 12 ) = 120; }
            else { *BundleNestedEntryValueInsert( b.main, *inner, 12 ) = 120; *BundleNestedEntryValueInsert( b.main, *inner, -5 ) = 55; }
        }
    }

    std::string s1 = std::string( "shared-" ) + std::string( "blob" );
    std::string s2 = std::string( "shared-" ) + std::string( "blob" );
    TableStringSlot ts1 = b.AllocString( static_cast<int64_t>( s1.size() ) );
    TableStringSlot ts2 = b.AllocString( static_cast<int64_t>( s2.size() ) );
    std::memcpy( ts1.data, s1.data(), s1.size() );
    std::memcpy( ts2.data, s2.data(), s2.size() );
    r->text_a = ts1.ref;
    r->text_b = ts2.ref;

    const uint8_t raw4[4] = { 0xde, 0xad, 0xbe, 0xef };
    TableBytesSlot bs1 = b.AllocBytes( 4 );
    TableBytesSlot bs2 = b.AllocBytes( 4 );
    std::memcpy( bs1.data, raw4, 4 );
    std::memcpy( bs2.data, raw4, 4 );
    r->blob_a = bs1.ref;
    r->blob_b = bs2.ref;
}

int main( int argc, char ** argv ) {
    if ( argc != 2 ) return 2;
    std::string dir = argv[1];
    auto want_wire = read_all( dir + "/canonical.bin" );
    auto want_json = read_all( dir + "/canonical.json" );
    const char * jfiles[3] = { "json_sorted.json", "json_reverse.json", "json_shuffled.json" };
    for ( int i = 0; i < 3; ++i ) {
        auto jraw = read_all( dir + "/" + jfiles[i] );
        BundleBuilder b;
        TableReport rep;
        if ( !BundleFromJson( b, reinterpret_cast<const char *>( jraw.data() ), static_cast<int64_t>( jraw.size() ), &rep ) || rep.damaged ) {
            die( jfiles[i] );
        }
        check_builder( b, want_wire, want_json, jfiles[i] );
    }
    for ( int m = 0; m < 3; ++m ) {
        BundleBuilder b;
        populate_native( b, m );
        check_builder( b, want_wire, want_json, "native" );
    }
    std::printf( "CANONICAL_OK cpp 6\n" );
    return 0;
}
`
	mainCPP := filepath.Join(workDir, "main.cpp")
	if err := os.WriteFile(mainCPP, []byte(cppSrc), 0o644); err != nil {
		return err
	}
	binPath := filepath.Join(workDir, "runner")
	cxx := envOr("CXX", "c++")
	if out, err := runChecked(workDir, nil, cxx, "-std=c++17", "-O2", "-ffp-contract=off",
		"-I"+serDir, "-I"+genDir, mainCPP, filepath.Join(genDir, "CanonicalTable.cpp"), "-o", binPath); err != nil {
		return fmt.Errorf("compile cpp canonical: %v\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, corpusDir)
	return requireToken(out, err, "cpp")
}

func verifyC(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "c", genDir); err != nil {
		return err
	}
	serC := resolveExistingDir(repoRoot, "SERIALIZE_C", "../serialize.c", "build/serialize.c")
	cSrc := `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "CanonicalTable.h"

static void die( const char * m ) { fprintf( stderr, "FAILED: %s\n", m ); exit( 1 ); }

static uint8_t * read_all( const char * path, size_t * out_len ) {
    FILE * f = fopen( path, "rb" );
    if ( !f ) die( path );
    fseek( f, 0, SEEK_END );
    long sz = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * b = (uint8_t *)calloc( (size_t)sz + 1, 1 );
    if ( sz > 0 && fread( b, 1, (size_t)sz, f ) != (size_t)sz ) die( path );
    fclose( f );
    *out_len = (size_t)sz;
    return b;
}

static void check_builder( canonical_bundle_builder_t * b, const uint8_t * want_wire, size_t ww_len, const uint8_t * want_json, size_t wj_len, const char * tag ) {
    if ( !canonical_bundle_builder_lock( b ) ) die( "lock" );
    const canonical_bundle_t * root = (const canonical_bundle_t *)b->region;
    uint8_t wbuf[4096];
    int64_t wn = canonical_bundle_save( NULL, root, wbuf, sizeof( wbuf ) );
    if ( wn != (int64_t)ww_len || memcmp( wbuf, want_wire, ww_len ) != 0 ) die( tag );
    char jbuf[8192];
    int64_t jn = canonical_bundle_to_json( root, jbuf, sizeof( jbuf ) );
    if ( jn != (int64_t)wj_len || memcmp( jbuf, want_json, wj_len ) != 0 ) die( tag );
}

static void populate_native( canonical_bundle_builder_t * b, int mode ) {
    canonical_bundle_t * r = canonical_bundle_builder_root( b );
    struct { const char * k; int32_t v; } by_name[8] = {
        { "", 10 }, { "Z", -5 }, { "a", 20 }, { "ab", 0 }, { "abc", -7 },
        { "long-unbounded-key-alpha", 12345 }, { "\xEF\xBD\xA1", 111 }, { "\xF0\x9F\x98\x80", 222 }
    };
    const int ord8[3][8] = { {0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3} };
    for ( int i = 0; i < 8; ++i ) {
        int idx = ord8[mode][i];
        *canonical_bundle_by_name_insert( &b->main, &r->by_name, by_name[idx].k ) = by_name[idx].v;
    }
    struct { const char * k; int32_t v; } by_code[5] = {
        { "Alpha", 1 }, { "a", 5 }, { "ab", 4 }, { "beta", 2 }, { "gamma", 3 }
    };
    const int ord5[3][5] = { {0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1} };
    for ( int i = 0; i < 5; ++i ) {
        int idx = ord5[mode][i];
        *canonical_bundle_by_code_insert( &b->main, &r->by_code, by_code[idx].k ) = by_code[idx].v;
    }
    struct { int64_t k; const char * lbl; int32_t sc; } by_id[5] = {
        { -9000000000000LL, "min-far", -99 }, { -1LL, "minus-one", -1 }, { 0LL, "", 0 },
        { 2LL, "two", 2 }, { 9000000000000LL, "max-far", 99 }
    };
    const int ord5b[3][5] = { {0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1} };
    for ( int i = 0; i < 5; ++i ) {
        int idx = ord5b[mode][i];
        canonical_child_t * c = canonical_bundle_by_id_insert( &b->main, &r->by_id, by_id[idx].k );
        c->label_length = (int32_t)strlen( by_id[idx].lbl );
        memcpy( c->label, by_id[idx].lbl, (size_t)c->label_length );
        c->score = by_id[idx].sc;
    }
    struct { uint64_t k; uint32_t v; } by_uid[7] = {
        { 0ULL, 0 }, { 1ULL, 11 }, { 255ULL, 2550 }, { 256ULL, 2560 },
        { 9223372036854775807ULL, 700 }, { 9223372036854775808ULL, 800 }, { 18446744073709551614ULL, 900 }
    };
    const int ord7[3][7] = { {0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1} };
    for ( int i = 0; i < 7; ++i ) {
        int idx = ord7[mode][i];
        *canonical_bundle_by_uid_insert( &b->main, &r->by_uid, by_uid[idx].k ) = by_uid[idx].v;
    }
    r->by_tier[0] = 100; r->by_tier[1] = 200; r->by_tier[2] = 300; r->by_tier[3] = 400;
    const char * okeys[3] = { "outer-a", "outer-m", "outer-z" };
    const int ord3[3][3] = { {0,1,2}, {2,1,0}, {1,2,0} };
    for ( int i = 0; i < 3; ++i ) {
        int idx = ord3[mode][i];
        canonical_table_map_t * inner = canonical_bundle_nested_insert( &b->main, &r->nested, okeys[idx] );
        if ( idx == 0 ) {
            struct { int32_t k, v; } items[4] = { {-100,-10}, {0,0}, {7,70}, {42,420} };
            const int io[3][4] = { {0,1,2,3}, {3,2,1,0}, {2,0,3,1} };
            for ( int j = 0; j < 4; ++j ) *canonical_bundle_nested_entry_value_insert( &b->main, inner, items[io[mode][j]].k ) = items[io[mode][j]].v;
        } else if ( idx == 1 ) {
            if ( mode == 0 ) { *canonical_bundle_nested_entry_value_insert( &b->main, inner, -5 ) = 55; *canonical_bundle_nested_entry_value_insert( &b->main, inner, 12 ) = 120; }
            else { *canonical_bundle_nested_entry_value_insert( &b->main, inner, 12 ) = 120; *canonical_bundle_nested_entry_value_insert( &b->main, inner, -5 ) = 55; }
        }
    }
    char s1[16], s2[16];
    snprintf( s1, sizeof( s1 ), "%s%s", "shared-", "blob" );
    snprintf( s2, sizeof( s2 ), "%s%s", "shared-", "blob" );
    canonical_table_string_emplace( &b->main, &r->text_a, s1, 11 );
    canonical_table_string_emplace( &b->main, &r->text_b, s2, 11 );
    const uint8_t raw4[4] = { 0xde, 0xad, 0xbe, 0xef };
    uint8_t * ba = canonical_table_bytes_emplace( &b->main, &r->blob_a, 4 );
    uint8_t * bb = canonical_table_bytes_emplace( &b->main, &r->blob_b, 4 );
    memcpy( ba, raw4, 4 );
    memcpy( bb, raw4, 4 );
}

int main( int argc, char ** argv ) {
    if ( argc != 2 ) return 2;
    char p[1024];
    size_t ww_len = 0, wj_len = 0;
    snprintf( p, sizeof( p ), "%s/canonical.bin", argv[1] );
    uint8_t * ww = read_all( p, &ww_len );
    snprintf( p, sizeof( p ), "%s/canonical.json", argv[1] );
    uint8_t * wj = read_all( p, &wj_len );
    const char * jfiles[3] = { "json_sorted.json", "json_reverse.json", "json_shuffled.json" };
    for ( int i = 0; i < 3; ++i ) {
        size_t jl = 0;
        snprintf( p, sizeof( p ), "%s/%s", argv[1], jfiles[i] );
        uint8_t * jr = read_all( p, &jl );
        canonical_bundle_builder_t b;
        memset( &b, 0, sizeof( b ) );
        if ( !canonical_bundle_builder_init( &b ) ) die( "init" );
        canonical_table_report_t rep;
        memset( &rep, 0, sizeof( rep ) );
        if ( !canonical_bundle_from_json( &b, (const char *)jr, (int64_t)jl, &rep ) || rep.refused ) die( jfiles[i] );
        check_builder( &b, ww, ww_len, wj, wj_len, jfiles[i] );
        canonical_bundle_builder_shutdown( &b );
        free( jr );
    }
    for ( int m = 0; m < 3; ++m ) {
        canonical_bundle_builder_t b;
        memset( &b, 0, sizeof( b ) );
        if ( !canonical_bundle_builder_init( &b ) ) die( "init" );
        populate_native( &b, m );
        check_builder( &b, ww, ww_len, wj, wj_len, "native" );
        canonical_bundle_builder_shutdown( &b );
    }
    free( ww ); free( wj );
    printf( "CANONICAL_OK c 6\n" );
    return 0;
}
`
	mainC := filepath.Join(workDir, "main.c")
	if err := os.WriteFile(mainC, []byte(cSrc), 0o644); err != nil {
		return err
	}
	binPath := filepath.Join(workDir, "runner")
	cc := envOr("CC", "cc")
	if out, err := runChecked(workDir, nil, cc, "-std=c99", "-O2", "-ffp-contract=off",
		"-I"+genDir, "-I"+serC, mainC, filepath.Join(genDir, "CanonicalTable.c"), filepath.Join(serC, "serialize.c"), "-o", binPath, "-lm"); err != nil {
		return fmt.Errorf("compile c canonical: %v\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, corpusDir)
	return requireToken(out, err, "c")
}

func csCanonicalProgram(isUnity bool) string {
	langTag := "cs"
	extraUsing := ""
	blobType := "C.TableBlob"
	copyCodeKey := "Array.Copy(kb, e.Key, kb.Length);"
	copyChildLbl := "Array.Copy(lb, c.Label, lb.Length);"
	setTiers := "b.ByTier[(int)C.Tier.Bronze] = 100; b.ByTier[(int)C.Tier.Silver] = 200; b.ByTier[(int)C.Tier.Gold] = 300; b.ByTier[(int)C.Tier.Platinum] = 400;"
	copyNestedKey := "Array.Copy(kb, e.Key, kb.Length);"
	repDecl := "var rep = new C.TableReport();"
	fromJsonCall := "C.Schema.BundleFromJson(b, jraw, rep)"
	if isUnity {
		langTag = "unity"
		extraUsing = "using static Canonical.Buffers;\n"
		blobType = "C.Schema.TableBlob"
		copyCodeKey = "Array.Copy(kb, e.Key, kb.Length);"
		copyChildLbl = "lb.CopyTo(c.Label.AsSpan());"
		setTiers = "b.ByTier[(int)C.Tier.Bronze] = 100; b.ByTier[(int)C.Tier.Silver] = 200; b.ByTier[(int)C.Tier.Gold] = 300; b.ByTier[(int)C.Tier.Platinum] = 400;"
		copyNestedKey = "Array.Copy(kb, e.Key, kb.Length);"
		repDecl = "var rep = default(C.Schema.TableReport);"
		fromJsonCall = "C.Schema.BundleFromJson(b, jraw, ref rep)"
	}
	return fmt.Sprintf(`using System;
using System.IO;
using System.Text;
%susing C = Canonical;

static class Program
{
    static T[] Reorder<T>(T[] items, int[] ord)
    {
        T[] res = new T[ord.Length];
        for (int i = 0; i < ord.Length; i++) res[i] = items[ord[i]];
        return res;
    }

    static C.BundleByNameEntry MkName(string k, int v)
    {
        byte[] kb = Encoding.UTF8.GetBytes(k);
        var e = new C.BundleByNameEntry();
        e.Key = kb;
        e.KeyLength = kb.Length;
        e.Value = v;
        return e;
    }

    static C.BundleByCodeEntry MkCode(string k, int v)
    {
        byte[] kb = Encoding.UTF8.GetBytes(k);
        var e = new C.BundleByCodeEntry();
        %s
        e.KeyLength = kb.Length;
        e.Value = v;
        return e;
    }

    static C.BundleByIdEntry MkId(long k, string lbl, int sc)
    {
        byte[] lb = Encoding.UTF8.GetBytes(lbl);
        var e = new C.BundleByIdEntry();
        e.Key = k;
        var c = e.Value;
        %s
        c.LabelLength = lb.Length;
        c.Score = sc;
        e.Value = c;
        return e;
    }

    static C.BundleByUidEntry MkUid(ulong k, uint v)
    {
        var e = new C.BundleByUidEntry();
        e.Key = k;
        e.Value = v;
        return e;
    }

    static C.BundleNestedEntryValueEntry MkInner(int k, int v)
    {
        var e = new C.BundleNestedEntryValueEntry();
        e.Key = k;
        e.Value = v;
        return e;
    }

    static C.BundleNestedEntry MkNested(string k, C.BundleNestedEntryValueEntry[] vals)
    {
        byte[] kb = Encoding.UTF8.GetBytes(k);
        var e = new C.BundleNestedEntry();
        %s
        e.KeyLength = kb.Length;
        e.Value = vals;
        e.ValueCount = vals.Length;
        return e;
    }

    static C.Bundle BuildNative(int mode)
    {
        int[][] ord8 = new[] { new[]{0,1,2,3,4,5,6,7}, new[]{7,6,5,4,3,2,1,0}, new[]{7,4,2,6,0,5,1,3} };
        int[][] ord5 = new[] { new[]{0,1,2,3,4}, new[]{4,3,2,1,0}, new[]{2,4,0,3,1} };
        int[][] ord5b = new[] { new[]{0,1,2,3,4}, new[]{4,3,2,1,0}, new[]{3,0,4,2,1} };
        int[][] ord7 = new[] { new[]{0,1,2,3,4,5,6}, new[]{6,5,4,3,2,1,0}, new[]{5,3,0,6,2,4,1} };
        int[][] ord3 = new[] { new[]{0,1,2}, new[]{2,1,0}, new[]{1,2,0} };
        int[][] ord4 = new[] { new[]{0,1,2,3}, new[]{3,2,1,0}, new[]{2,0,3,1} };
        int[][] ord2 = new[] { new[]{0,1}, new[]{1,0}, new[]{1,0} };

        var b = new C.Bundle();
        b.ByName = Reorder(new[] {
            MkName("", 10), MkName("Z", -5), MkName("a", 20), MkName("ab", 0), MkName("abc", -7),
            MkName("long-unbounded-key-alpha", 12345), MkName("\uff61", 111), MkName("\U0001F600", 222)
        }, ord8[mode]);
        b.ByNameCount = b.ByName.Length;

        b.ByCode = Reorder(new[] {
            MkCode("Alpha", 1), MkCode("a", 5), MkCode("ab", 4), MkCode("beta", 2), MkCode("gamma", 3)
        }, ord5[mode]);
        b.ByCodeCount = b.ByCode.Length;

        b.ById = Reorder(new[] {
            MkId(-9000000000000L, "min-far", -99), MkId(-1L, "minus-one", -1), MkId(0L, "", 0),
            MkId(2L, "two", 2), MkId(9000000000000L, "max-far", 99)
        }, ord5b[mode]);
        b.ByIdCount = b.ById.Length;

        b.ByUid = Reorder(new[] {
            MkUid(0UL, 0), MkUid(1UL, 11), MkUid(255UL, 2550), MkUid(256UL, 2560),
            MkUid(9223372036854775807UL, 700), MkUid(9223372036854775808UL, 800), MkUid(18446744073709551614UL, 900)
        }, ord7[mode]);
        b.ByUidCount = b.ByUid.Length;

        %s

        b.Nested = Reorder(new[] {
            MkNested("outer-a", Reorder(new[] { MkInner(-100, -10), MkInner(0, 0), MkInner(7, 70), MkInner(42, 420) }, ord4[mode])),
            MkNested("outer-m", Reorder(new[] { MkInner(-5, 55), MkInner(12, 120) }, ord2[mode])),
            MkNested("outer-z", Array.Empty<C.BundleNestedEntryValueEntry>())
        }, ord3[mode]);
        b.NestedCount = b.Nested.Length;

        string s1 = string.Concat("shared-", new string("blob".ToCharArray()));
        string s2 = string.Concat("shared-", new string("blob".ToCharArray()));
        if (object.ReferenceEquals(s1, s2)) throw new Exception("FAILED: strings interned");
        b.TextA = new %s(Encoding.UTF8.GetBytes(s1));
        b.TextB = new %s(Encoding.UTF8.GetBytes(s2));
        b.BlobA = new %s(new byte[] { 0xde, 0xad, 0xbe, 0xef });
        b.BlobB = new %s(new byte[] { 0xde, 0xad, 0xbe, 0xef });
        return b;
    }

    static void CheckBundle(C.Bundle b, byte[] wantWire, byte[] wantJson, string tag)
    {
        byte[] wbuf = new byte[4096];
        long wn = C.Schema.BundleSave(b, wbuf);
        if (wn < 0 || !wbuf.AsSpan(0, (int)wn).SequenceEqual(wantWire))
            throw new Exception("FAILED: wire " + tag + " wn=" + wn);
        byte[] jbuf = new byte[8192];
        long jn = C.Schema.BundleToJson(b, jbuf);
        if (jn < 0 || !jbuf.AsSpan(0, (int)jn).SequenceEqual(wantJson))
            throw new Exception("FAILED: json " + tag + "\ngot=" + Encoding.UTF8.GetString(jbuf, 0, Math.Max(0, (int)jn)));
    }

    static int Main(string[] args)
    {
        string dir = args[0];
        byte[] wantWire = File.ReadAllBytes(Path.Combine(dir, "canonical.bin"));
        byte[] wantJson = File.ReadAllBytes(Path.Combine(dir, "canonical.json"));
        foreach (string jf in new[] { "json_sorted.json", "json_reverse.json", "json_shuffled.json" })
        {
            byte[] jraw = File.ReadAllBytes(Path.Combine(dir, jf));
            var b = new C.Bundle();
            %s
            if (!%s || rep.Damaged) throw new Exception("FAILED: FromJson " + jf);
            CheckBundle(b, wantWire, wantJson, jf);
        }
        for (int m = 0; m < 3; m++)
        {
            CheckBundle(BuildNative(m), wantWire, wantJson, "native_" + m);
        }
        Console.WriteLine("CANONICAL_OK %s 6");
        return 0;
    }
}
`, extraUsing, copyCodeKey, copyChildLbl, copyNestedKey, setTiers, blobType, blobType, blobType, blobType, repDecl, fromJsonCall, langTag)
}

func verifyCS(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "cs", genDir); err != nil {
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
	if err := os.WriteFile(filepath.Join(workDir, "Program.cs"), []byte(csCanonicalProgram(false)), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(workDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(workDir, "app.csproj"), "--", corpusDir)
	return requireToken(out, err, "cs")
}

func verifyUnity(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "unity", genDir); err != nil {
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
    <AssemblyName>canonicalunity</AssemblyName>
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
    <AssemblyName>canonical-unity-app</AssemblyName>
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
	if err := os.WriteFile(filepath.Join(appDir, "Program.cs"), []byte(csCanonicalProgram(true)), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(appDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(appDir, "app.csproj"), "--", corpusDir)
	return requireToken(out, err, "unity")
}

func verifyGo(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "go", genDir); err != nil {
		return err
	}
	shimRoot := filepath.Join(workDir, "schemashim")
	if err := os.MkdirAll(filepath.Join(shimRoot, "tables"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(shimRoot, "go.mod"), []byte("module github.com/mas-bandwidth/schema/v2\n\ngo 1.23\n"), 0o644); err != nil {
		return err
	}
	if err := os.Symlink(filepath.Join(repoRoot, "tables", "goruntime"), filepath.Join(shimRoot, "tables", "goruntime")); err != nil && !os.IsExist(err) {
		return err
	}
	genMod := fmt.Sprintf("module canonicalgen\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => %s\n", shimRoot)
	if err := os.WriteFile(filepath.Join(genDir, "go.mod"), []byte(genMod), 0o644); err != nil {
		return err
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	appMod := fmt.Sprintf("module canonicalapp\n\ngo 1.23\n\nrequire (\n\tcanonicalgen v0.0.0\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n)\n\nreplace canonicalgen => ../gen\nreplace github.com/mas-bandwidth/schema/v2 => %s\n", shimRoot)
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appMod), 0o644); err != nil {
		return err
	}
	goSrc := `package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	c "canonicalgen"
)

func checkBuilder(b *c.BundleBuilder, wantWire, wantJSON []byte, tag string) {
	if !b.Lock() {
		panic("FAILED: Lock " + tag)
	}
	root := b.AsConst()
	wbuf := make([]byte, 4096)
	wn := c.BundleSave(root, wbuf)
	if wn < 0 || !bytes.Equal(wbuf[:wn], wantWire) {
		panic(fmt.Sprintf("FAILED: wire %s wn=%d", tag, wn))
	}
	jbuf := make([]byte, 8192)
	jn := c.BundleToJSON(root, jbuf)
	if jn < 0 || !bytes.Equal(jbuf[:jn], wantJSON) {
		panic(fmt.Sprintf("FAILED: json %s", tag))
	}
}

func populateNative(b *c.BundleBuilder, mode int) {
	r := b.GetRoot()
	byName := []struct{ k string; v int32 }{
		{"", 10}, {"Z", -5}, {"a", 20}, {"ab", 0}, {"abc", -7},
		{"long-unbounded-key-alpha", 12345}, {"\uff61", 111}, {"\U0001F600", 222},
	}
	ord8 := [3][8]int{{0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3}}
	for _, idx := range ord8[mode] {
		*c.BundleByNameInsert(&b.Main, &r.ByName, byName[idx].k) = byName[idx].v
	}
	byCode := []struct{ k string; v int32 }{{"Alpha", 1}, {"a", 5}, {"ab", 4}, {"beta", 2}, {"gamma", 3}}
	ord5 := [3][5]int{{0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1}}
	for _, idx := range ord5[mode] {
		*c.BundleByCodeInsert(&b.Main, &r.ByCode, byCode[idx].k) = byCode[idx].v
	}
	byID := []struct{ k int64; lbl string; sc int32 }{
		{-9000000000000, "min-far", -99}, {-1, "minus-one", -1}, {0, "", 0}, {2, "two", 2}, {9000000000000, "max-far", 99},
	}
	ord5b := [3][5]int{{0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1}}
	for _, idx := range ord5b[mode] {
		ch := c.BundleByIDInsert(&b.Main, &r.ByID, byID[idx].k)
		copy(ch.Label[:], byID[idx].lbl)
		ch.LabelLength = int32(len(byID[idx].lbl))
		ch.Score = byID[idx].sc
	}
	byUID := []struct{ k uint64; v uint32 }{
		{0, 0}, {1, 11}, {255, 2550}, {256, 2560},
		{9223372036854775807, 700}, {9223372036854775808, 800}, {18446744073709551614, 900},
	}
	ord7 := [3][7]int{{0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1}}
	for _, idx := range ord7[mode] {
		*c.BundleByUIDInsert(&b.Main, &r.ByUID, byUID[idx].k) = byUID[idx].v
	}
	r.ByTier = [4]uint32{100, 200, 300, 400}

	okeys := [3]string{"outer-a", "outer-m", "outer-z"}
	ord3 := [3][3]int{{0,1,2}, {2,1,0}, {1,2,0}}
	for _, idx := range ord3[mode] {
		inner := c.BundleNestedInsert(&b.Main, &r.Nested, okeys[idx])
		if idx == 0 {
			items := [4]struct{ k, v int32 }{{-100, -10}, {0, 0}, {7, 70}, {42, 420}}
			io := [3][4]int{{0,1,2,3}, {3,2,1,0}, {2,0,3,1}}
			for _, j := range io[mode] {
				*c.BundleNestedEntryValueInsert(&b.Main, inner, items[j].k) = items[j].v
			}
		} else if idx == 1 {
			if mode == 0 {
				*c.BundleNestedEntryValueInsert(&b.Main, inner, -5) = 55
				*c.BundleNestedEntryValueInsert(&b.Main, inner, 12) = 120
			} else {
				*c.BundleNestedEntryValueInsert(&b.Main, inner, 12) = 120
				*c.BundleNestedEntryValueInsert(&b.Main, inner, -5) = 55
			}
		}
	}
	s1 := strings.Join([]string{"shared-", "blob"}, "")
	s2 := strings.Join([]string{"shared-", "blob"}, "")
	bs1, ref1 := b.Main.AllocString(int64(len(s1)))
	bs2, ref2 := b.Main.AllocString(int64(len(s2)))
	copy(bs1, s1)
	copy(bs2, s2)
	r.TextA = ref1
	r.TextB = ref2

	bb1, bref1 := b.Main.AllocBytes(4)
	bb2, bref2 := b.Main.AllocBytes(4)
	copy(bb1, []byte{0xde, 0xad, 0xbe, 0xef})
	copy(bb2, []byte{0xde, 0xad, 0xbe, 0xef})
	r.BlobA = bref1
	r.BlobB = bref2
}

func main() {
	dir := os.Args[1]
	wantWire, _ := os.ReadFile(filepath.Join(dir, "canonical.bin"))
	wantJSON, _ := os.ReadFile(filepath.Join(dir, "canonical.json"))
	for _, jf := range []string{"json_sorted.json", "json_reverse.json", "json_shuffled.json"} {
		jraw, _ := os.ReadFile(filepath.Join(dir, jf))
		var b c.BundleBuilder
		if !b.Init() {
			panic("Init")
		}
		var rep c.TableReport
		if !c.BundleFromJSON(&b, jraw, &rep) || rep.Refused() {
			panic("FAILED: FromJSON " + jf)
		}
		checkBuilder(&b, wantWire, wantJSON, jf)
		b.Shutdown()
	}
	for m := 0; m < 3; m++ {
		var b c.BundleBuilder
		if !b.Init() {
			panic("Init")
		}
		populateNative(&b, m)
		checkBuilder(&b, wantWire, wantJSON, fmt.Sprintf("native_%d", m))
		b.Shutdown()
	}
	fmt.Println("CANONICAL_OK go 6")
}
`
	if err := os.WriteFile(filepath.Join(appDir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return err
	}
	out, err := runChecked(appDir, nil, "go", "run", ".", corpusDir)
	return requireToken(out, err, "go")
}

func verifyRust(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	genSrcDir := filepath.Join(genDir, "src")
	if err := generateCanonical(repoRoot, schemaBin, "", "rust", genSrcDir); err != nil {
		return err
	}
	serRS := resolveExistingDir(repoRoot, "SERIALIZE_RS", "../serialize.rs", "build/serialize.rs")
	genToml := fmt.Sprintf("[package]\nname = \"canonicalgen\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[features]\ncook = []\nblock = []\n\n[dependencies]\nserialize = { package = \"serialize-official\", path = %q }\n", serRS)
	if err := os.WriteFile(filepath.Join(genDir, "Cargo.toml"), []byte(genToml), 0o644); err != nil {
		return err
	}
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(filepath.Join(appDir, "src"), 0o755); err != nil {
		return err
	}
	appToml := "[package]\nname = \"canonicalapp\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\ncanonicalgen = { path = \"../gen\" }\n"
	if err := os.WriteFile(filepath.Join(appDir, "Cargo.toml"), []byte(appToml), 0o644); err != nil {
		return err
	}
	rsSrc := `use canonicalgen as c;
use std::fs;
use std::path::Path;
use std::rc::Rc;

fn reorder<T: Clone>(items: &[T], ord: &[usize]) -> Vec<T> {
    ord.iter().map(|&i| items[i].clone()).collect()
}

fn mk_code(k: &str, v: i32) -> c::BundleByCodeEntry {
    c::BundleByCodeEntry { key: k.to_string(), value: v }
}

fn mk_id(k: i64, lbl: &str, sc: i32) -> c::BundleByIdEntry {
    c::BundleByIdEntry { key: k, value: c::Child { label: lbl.to_string(), score: sc } }
}

fn mk_nested(k: &str, vals: Vec<c::BundleNestedEntryValueEntry>) -> c::BundleNestedEntry {
    c::BundleNestedEntry { key: k.to_string(), value: vals }
}

fn build_native(mode: usize) -> c::Bundle {
    let ord8 = [[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]];
    let ord5 = [[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]];
    let ord5b = [[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]];
    let ord7 = [[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]];
    let ord3 = [[0,1,2], [2,1,0], [1,2,0]];
    let ord4 = [[0,1,2,3], [3,2,1,0], [2,0,3,1]];
    let ord2 = [[0,1], [1,0], [1,0]];

    let mut b = c::Bundle::default();
    let names = [
        c::BundleByNameEntry { key: "".to_string(), value: 10 },
        c::BundleByNameEntry { key: "Z".to_string(), value: -5 },
        c::BundleByNameEntry { key: "a".to_string(), value: 20 },
        c::BundleByNameEntry { key: "ab".to_string(), value: 0 },
        c::BundleByNameEntry { key: "abc".to_string(), value: -7 },
        c::BundleByNameEntry { key: "long-unbounded-key-alpha".to_string(), value: 12345 },
        c::BundleByNameEntry { key: "\u{ff61}".to_string(), value: 111 },
        c::BundleByNameEntry { key: "\u{1f600}".to_string(), value: 222 },
    ];
    b.by_name = reorder(&names, &ord8[mode]);

    let codes = [mk_code("Alpha", 1), mk_code("a", 5), mk_code("ab", 4), mk_code("beta", 2), mk_code("gamma", 3)];
    b.by_code = reorder(&codes, &ord5[mode]);

    let ids = [
        mk_id(-9000000000000, "min-far", -99), mk_id(-1, "minus-one", -1), mk_id(0, "", 0),
        mk_id(2, "two", 2), mk_id(9000000000000, "max-far", 99),
    ];
    b.by_id = reorder(&ids, &ord5b[mode]);

    let uids = [
        c::BundleByUidEntry { key: 0, value: 0 },
        c::BundleByUidEntry { key: 1, value: 11 },
        c::BundleByUidEntry { key: 255, value: 2550 },
        c::BundleByUidEntry { key: 256, value: 2560 },
        c::BundleByUidEntry { key: 9223372036854775807, value: 700 },
        c::BundleByUidEntry { key: 9223372036854775808, value: 800 },
        c::BundleByUidEntry { key: 18446744073709551614, value: 900 },
    ];
    b.by_uid = reorder(&uids, &ord7[mode]);

    b.by_tier = vec![100, 200, 300, 400];

    let in0 = [
        c::BundleNestedEntryValueEntry { key: -100, value: -10 },
        c::BundleNestedEntryValueEntry { key: 0, value: 0 },
        c::BundleNestedEntryValueEntry { key: 7, value: 70 },
        c::BundleNestedEntryValueEntry { key: 42, value: 420 },
    ];
    let in1 = [
        c::BundleNestedEntryValueEntry { key: -5, value: 55 },
        c::BundleNestedEntryValueEntry { key: 12, value: 120 },
    ];
    let nested = [
        mk_nested("outer-a", reorder(&in0, &ord4[mode])),
        mk_nested("outer-m", reorder(&in1, &ord2[mode])),
        mk_nested("outer-z", Vec::new()),
    ];
    b.nested = reorder(&nested, &ord3[mode]);

    let s1: Rc<str> = Rc::from(format!("{}{}", "shared-", "blob"));
    let s2: Rc<str> = Rc::from(format!("{}{}", "shared-", "blob"));
    assert!(!Rc::ptr_eq(&s1, &s2));
    b.text_a = Some(s1);
    b.text_b = Some(s2);

    let b1: Rc<[u8]> = Rc::from(vec![0xdeu8, 0xad, 0xbe, 0xef]);
    let b2: Rc<[u8]> = Rc::from(vec![0xdeu8, 0xad, 0xbe, 0xef]);
    assert!(!Rc::ptr_eq(&b1, &b2));
    b.blob_a = Some(b1);
    b.blob_b = Some(b2);
    b
}

fn check_bundle(b: &c::Bundle, want_wire: &[u8], want_json: &[u8], tag: &str) {
    let w = c::bundle_save_checked(b).expect(tag);
    if w != want_wire {
        panic!("FAILED: wire {}", tag);
    }
    let j = c::bundle_to_json_checked(b).expect(tag);
    if j.as_bytes() != want_json {
        panic!("FAILED: json {}\ngot={}", tag, j);
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let dir = Path::new(&args[1]);
    let want_wire = fs::read(dir.join("canonical.bin")).unwrap();
    let want_json = fs::read(dir.join("canonical.json")).unwrap();
    for jf in ["json_sorted.json", "json_reverse.json", "json_shuffled.json"] {
        let jraw = fs::read(dir.join(jf)).unwrap();
        let mut rep = c::TableReport::default();
        let b = c::bundle_from_json_bytes(&jraw, &mut rep).expect(jf);
        assert!(!rep.refused);
        check_bundle(&b, &want_wire, &want_json, jf);
    }
    for m in 0..3 {
        let b = build_native(m);
        check_bundle(&b, &want_wire, &want_json, &format!("native_{}", m));
    }
    println!("CANONICAL_OK rust 6");
}
`
	if err := os.WriteFile(filepath.Join(appDir, "src", "main.rs"), []byte(rsSrc), 0o644); err != nil {
		return err
	}
	var extraEnv []string
	if rb := os.Getenv("RUSTUP_BIN"); rb != "" {
		extraEnv = append(extraEnv, "PATH="+rb+":"+os.Getenv("PATH"))
	}
	out, err := runChecked(appDir, extraEnv, "cargo", "run", "--quiet", "--release", "--", corpusDir)
	return requireToken(out, err, "rust")
}

func verifyJava(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "java", genDir); err != nil {
		return err
	}
	javaSrc := `import canonical.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

public final class Main {
    static <T> List<T> reorder(List<T> items, int[] ord) {
        List<T> out = new ArrayList<>(ord.length);
        for (int idx : ord) out.add(items.get(idx));
        return out;
    }

    static BundleByNameEntryVariable.Value mkName(String k, int v) {
        var e = new BundleByNameEntryVariable.Value();
        e.key = k; e.value = v; return e;
    }
    static BundleByCodeEntryVariable.Value mkCode(String k, int v) {
        var e = new BundleByCodeEntryVariable.Value();
        e.key = k; e.value = v; return e;
    }
    static BundleByIdEntryVariable.Value mkId(long k, String lbl, int sc) {
        var e = new BundleByIdEntryVariable.Value();
        e.key = k; e.value.label = lbl; e.value.score = sc; return e;
    }
    static BundleByUidEntryVariable.Value mkUid(long k, int v) {
        var e = new BundleByUidEntryVariable.Value();
        e.key = k; e.value = v; return e;
    }
    static BundleNestedEntryValueEntryVariable.Value mkInner(int k, int v) {
        var e = new BundleNestedEntryValueEntryVariable.Value();
        e.key = k; e.value = v; return e;
    }
    static BundleNestedEntryVariable.Value mkNested(String k, List<BundleNestedEntryValueEntryVariable.Value> vals) {
        var e = new BundleNestedEntryVariable.Value();
        e.key = k; e.value = vals; return e;
    }

    static BundleVariable.Value buildNative(int mode) {
        int[][] ord8 = {{0,1,2,3,4,5,6,7}, {7,6,5,4,3,2,1,0}, {7,4,2,6,0,5,1,3}};
        int[][] ord5 = {{0,1,2,3,4}, {4,3,2,1,0}, {2,4,0,3,1}};
        int[][] ord5b = {{0,1,2,3,4}, {4,3,2,1,0}, {3,0,4,2,1}};
        int[][] ord7 = {{0,1,2,3,4,5,6}, {6,5,4,3,2,1,0}, {5,3,0,6,2,4,1}};
        int[][] ord3 = {{0,1,2}, {2,1,0}, {1,2,0}};
        int[][] ord4 = {{0,1,2,3}, {3,2,1,0}, {2,0,3,1}};
        int[][] ord2 = {{0,1}, {1,0}, {1,0}};

        var b = new BundleVariable.Value();
        b.byName = reorder(List.of(
            mkName("", 10), mkName("Z", -5), mkName("a", 20), mkName("ab", 0), mkName("abc", -7),
            mkName("long-unbounded-key-alpha", 12345), mkName("\uff61", 111), mkName("\uD83D\uDE00", 222)
        ), ord8[mode]);
        b.byCode = reorder(List.of(
            mkCode("Alpha", 1), mkCode("a", 5), mkCode("ab", 4), mkCode("beta", 2), mkCode("gamma", 3)
        ), ord5[mode]);
        b.byId = reorder(List.of(
            mkId(-9000000000000L, "min-far", -99), mkId(-1L, "minus-one", -1), mkId(0L, "", 0),
            mkId(2L, "two", 2), mkId(9000000000000L, "max-far", 99)
        ), ord5b[mode]);
        b.byUid = reorder(List.of(
            mkUid(0L, 0), mkUid(1L, 11), mkUid(255L, 2550), mkUid(256L, 2560),
            mkUid(9223372036854775807L, 700), mkUid(Long.parseUnsignedLong("9223372036854775808"), 800),
            mkUid(Long.parseUnsignedLong("18446744073709551614"), 900)
        ), ord7[mode]);
        b.byTier = new ArrayList<>(List.of(100, 200, 300, 400));
        b.nested = reorder(List.of(
            mkNested("outer-a", reorder(List.of(mkInner(-100, -10), mkInner(0, 0), mkInner(7, 70), mkInner(42, 420)), ord4[mode])),
            mkNested("outer-m", reorder(List.of(mkInner(-5, 55), mkInner(12, 120)), ord2[mode])),
            mkNested("outer-z", new ArrayList<>())
        ), ord3[mode]);

        String s1 = new StringBuilder("shared-").append("blob").toString();
        String s2 = new StringBuilder("shared-").append("blob").toString();
        if (s1 == s2) throw new RuntimeException("FAILED: s1 == s2");
        b.textA = s1;
        b.textB = s2;
        b.blobA = new byte[]{(byte)0xde, (byte)0xad, (byte)0xbe, (byte)0xef};
        b.blobB = new byte[]{(byte)0xde, (byte)0xad, (byte)0xbe, (byte)0xef};
        return b;
    }

    static void checkBundle(BundleVariable.Value b, byte[] wantWire, byte[] wantJson, String tag) {
        byte[] w = BundleVariable.save(b);
        if (w == null || !Arrays.equals(w, wantWire)) throw new RuntimeException("FAILED: wire " + tag);
        byte[] j = BundleVariable.toJson(b);
        if (j == null || !Arrays.equals(j, wantJson)) {
            throw new RuntimeException("FAILED: json " + tag + "\ngot=" + (j == null ? "null" : new String(j, StandardCharsets.UTF_8)));
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Path.of(args[0]);
        byte[] wantWire = Files.readAllBytes(dir.resolve("canonical.bin"));
        byte[] wantJson = Files.readAllBytes(dir.resolve("canonical.json"));
        for (String jf : new String[]{"json_sorted.json", "json_reverse.json", "json_shuffled.json"}) {
            byte[] jraw = Files.readAllBytes(dir.resolve(jf));
            var rep = new TableVariable.Report();
            var b = BundleVariable.fromJson(jraw, rep);
            if (b == null || rep.damaged || rep.refused) throw new RuntimeException("FAILED: fromJson " + jf);
            checkBundle(b, wantWire, wantJson, jf);
        }
        for (int m = 0; m < 3; m++) {
            checkBundle(buildNative(m), wantWire, wantJson, "native_" + m);
        }
        System.out.println("CANONICAL_OK java 6");
    }
}
`
	mainJava := filepath.Join(workDir, "Main.java")
	if err := os.WriteFile(mainJava, []byte(javaSrc), 0o644); err != nil {
		return err
	}
	classesDir := filepath.Join(workDir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return err
	}
	javac := resolveTool(repoRoot, "JAVAC", "dist/jdk-21.0.12.1/Contents/Home/bin/javac", "javac")
	javaBin := resolveTool(repoRoot, "JAVA", "dist/jdk-21.0.12.1/Contents/Home/bin/java", "java")
	genEntries, _ := filepath.Glob(filepath.Join(genDir, "*.java"))
	args := append([]string{"--release", "17", "-d", classesDir, mainJava}, genEntries...)
	if out, err := runChecked(workDir, nil, javac, args...); err != nil {
		return fmt.Errorf("javac: %v\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, javaBin, "-ea", "-cp", classesDir, "Main", corpusDir)
	return requireToken(out, err, "java")
}

func verifyDart(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "dart", genDir); err != nil {
		return err
	}
	dartSrc := `import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import './gen/CanonicalWire.dart' as cw;

List<T> reorder<T>(List<T> items, List<int> ord) => [for (final i in ord) items[i]];

cw.BundleByNameEntry mkName(String k, int v) => cw.BundleByNameEntry()..key = k..value = v;
cw.BundleByCodeEntry mkCode(String k, int v) => cw.BundleByCodeEntry()..key = k..value = v;
cw.BundleByIdEntry mkId(int k, String lbl, int sc) =>
    cw.BundleByIdEntry()..key = k..value = (cw.Child()..label = lbl..score = sc);
cw.BundleByUidEntry mkUid(int k, int v) => cw.BundleByUidEntry()..key = k..value = v;
cw.BundleNestedEntryValueEntry mkInner(int k, int v) => cw.BundleNestedEntryValueEntry()..key = k..value = v;
cw.BundleNestedEntry mkNested(String k, List<cw.BundleNestedEntryValueEntry> vals) =>
    cw.BundleNestedEntry()..key = k..value = vals;

cw.Bundle buildNative(int mode) {
  const ord8 = [[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]];
  const ord5 = [[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]];
  const ord5b = [[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]];
  const ord7 = [[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]];
  const ord3 = [[0,1,2], [2,1,0], [1,2,0]];
  const ord4 = [[0,1,2,3], [3,2,1,0], [2,0,3,1]];
  const ord2 = [[0,1], [1,0], [1,0]];

  final b = cw.Bundle();
  b.byName = reorder([
    mkName('', 10), mkName('Z', -5), mkName('a', 20), mkName('ab', 0), mkName('abc', -7),
    mkName('long-unbounded-key-alpha', 12345), mkName('\uff61', 111), mkName('\u{1f600}', 222),
  ], ord8[mode]);
  b.byCode = reorder([
    mkCode('Alpha', 1), mkCode('a', 5), mkCode('ab', 4), mkCode('beta', 2), mkCode('gamma', 3),
  ], ord5[mode]);
  b.byId = reorder([
    mkId(-9000000000000, 'min-far', -99), mkId(-1, 'minus-one', -1), mkId(0, '', 0),
    mkId(2, 'two', 2), mkId(9000000000000, 'max-far', 99),
  ], ord5b[mode]);
  // In Dart 64-bit int is two's complement; 9223372036854775808 is -9223372036854775808 and 18446744073709551614 is -2.
  b.byUid = reorder([
    mkUid(0, 0), mkUid(1, 11), mkUid(255, 2550), mkUid(256, 2560),
    mkUid(9223372036854775807, 700), mkUid(-9223372036854775808, 800), mkUid(-2, 900),
  ], ord7[mode]);
  b.byTier = [100, 200, 300, 400];
  b.nested = reorder([
    mkNested('outer-a', reorder([mkInner(-100, -10), mkInner(0, 0), mkInner(7, 70), mkInner(42, 420)], ord4[mode])),
    mkNested('outer-m', reorder([mkInner(-5, 55), mkInner(12, 120)], ord2[mode])),
    mkNested('outer-z', <cw.BundleNestedEntryValueEntry>[]),
  ], ord3[mode]);

  b.textA = ['shared-', 'blob'].join();
  b.textB = ['shared-', 'blob'].join();
  b.blobA = Uint8List.fromList([0xde, 0xad, 0xbe, 0xef]);
  b.blobB = Uint8List.fromList([0xde, 0xad, 0xbe, 0xef]);
  return b;
}

bool bytesEq(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

void checkBundle(cw.Bundle b, Uint8List wantWire, Uint8List wantJson, String tag) {
  final w = cw.bundleWireSave(b);
  if (!bytesEq(w, wantWire)) throw StateError('FAILED: wire $tag');
  final j = Uint8List.fromList(utf8.encode(cw.bundleToJson(b)));
  if (!bytesEq(j, wantJson)) throw StateError('FAILED: json $tag');
}

void main(List<String> args) {
  final dir = args[0];
  final wantWire = File('$dir/canonical.bin').readAsBytesSync();
  final wantJson = File('$dir/canonical.json').readAsBytesSync();
  for (final jf in ['json_sorted.json', 'json_reverse.json', 'json_shuffled.json']) {
    final jtext = File('$dir/$jf').readAsStringSync();
    final rep = cw.TableTextReport();
    final b = cw.bundleFromJson(jtext, rep);
    if (rep.refused) throw StateError('FAILED: fromJson $jf');
    checkBundle(b, wantWire, wantJson, jf);
  }
  for (var m = 0; m < 3; m++) {
    checkBundle(buildNative(m), wantWire, wantJson, 'native_$m');
  }
  print('CANONICAL_OK dart 6');
}
`
	mainDart := filepath.Join(workDir, "main.dart")
	if err := os.WriteFile(mainDart, []byte(dartSrc), 0o644); err != nil {
		return err
	}
	dartBin := resolveTool(repoRoot, "DART", "dist/dart-sdk-3.13.2/bin/dart", "dart")
	out, err := runChecked(workDir, nil, dartBin, "--enable-asserts", mainDart, corpusDir)
	return requireToken(out, err, "dart")
}

func verifyElixir(repoRoot, schemaBin, corpusDir, workDir string) error {
	genDir := filepath.Join(workDir, "gen")
	if err := generateCanonical(repoRoot, schemaBin, "", "elixir", genDir); err != nil {
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

	exFiles := []string{
		filepath.Join(genDir, "Canonical.ex"),
		filepath.Join(genDir, "FixedRuntime.ex"),
		filepath.Join(genDir, "CanonicalFixed.ex"),
		filepath.Join(genDir, "TableRuntime.ex"),
		filepath.Join(genDir, "Table.ex"),
	}
	cArgs := append([]string{"-o", ebinDir}, exFiles...)
	if out, err := runChecked(workDir, extraEnv, elixircBin, cArgs...); err != nil {
		return fmt.Errorf("elixirc: %v\n%s", err, out)
	}

	exSrc := `defmodule CanonicalRunner do
  alias Canonical.Table, as: T

  def reorder(list, ord), do: Enum.map(ord, &Enum.at(list, &1))

  def build_native(mode) do
    ord8 = Enum.at([[0,1,2,3,4,5,6,7], [7,6,5,4,3,2,1,0], [7,4,2,6,0,5,1,3]], mode)
    ord5 = Enum.at([[0,1,2,3,4], [4,3,2,1,0], [2,4,0,3,1]], mode)
    ord5b = Enum.at([[0,1,2,3,4], [4,3,2,1,0], [3,0,4,2,1]], mode)
    ord7 = Enum.at([[0,1,2,3,4,5,6], [6,5,4,3,2,1,0], [5,3,0,6,2,4,1]], mode)
    ord3 = Enum.at([[0,1,2], [2,1,0], [1,2,0]], mode)
    ord4 = Enum.at([[0,1,2,3], [3,2,1,0], [2,0,3,1]], mode)
    ord2 = Enum.at([[0,1], [1,0], [1,0]], mode)

    by_name =
      [{"", 10}, {"Z", -5}, {"a", 20}, {"ab", 0}, {"abc", -7}, {"long-unbounded-key-alpha", 12345}, {<<0xEF, 0xBD, 0xA1>>, 111}, {<<0xF0, 0x9F, 0x98, 0x80>>, 222}]
      |> reorder(ord8)
      |> Enum.reduce(%{}, fn {k, v}, acc -> Map.put(acc, k, v) end)

    by_code =
      [{"Alpha", 1}, {"a", 5}, {"ab", 4}, {"beta", 2}, {"gamma", 3}]
      |> reorder(ord5)
      |> Enum.reduce(%{}, fn {k, v}, acc -> Map.put(acc, k, v) end)

    by_id =
      [
        {"-9000000000000", %{"label" => "min-far", "score" => -99}},
        {"-1", %{"label" => "minus-one", "score" => -1}},
        {"0", %{"label" => "", "score" => 0}},
        {"2", %{"label" => "two", "score" => 2}},
        {"9000000000000", %{"label" => "max-far", "score" => 99}}
      ]
      |> reorder(ord5b)
      |> Enum.reduce(%{}, fn {k, v}, acc -> Map.put(acc, k, v) end)

    by_uid =
      [
        {"0", 0}, {"1", 11}, {"255", 2550}, {"256", 2560},
        {"9223372036854775807", 700}, {"9223372036854775808", 800}, {"18446744073709551614", 900}
      ]
      |> reorder(ord7)
      |> Enum.reduce(%{}, fn {k, v}, acc -> Map.put(acc, k, v) end)

    by_tier =
      [{"Bronze", 100}, {"Silver", 200}, {"Gold", 300}, {"Platinum", 400}]
      |> reorder(ord4)
      |> Enum.reduce(%{}, fn {k, v}, acc -> Map.put(acc, k, v) end)

    in0 = [{"-100", -10}, {"0", 0}, {"7", 70}, {"42", 420}] |> reorder(ord4) |> Map.new()
    in1 = [{"-5", 55}, {"12", 120}] |> reorder(ord2) |> Map.new()
    nested =
      [{"outer-a", in0}, {"outer-m", in1}, {"outer-z", %{}}]
      |> reorder(ord3)
      |> Enum.reduce(%{}, fn {k, v}, acc -> Map.put(acc, k, v) end)

    s1 = :binary.copy(Enum.join(["shared-", "blob"]))
    s2 = :binary.copy(Enum.join(["shared-", "blob"]))
    b1 = :binary.copy(<<0xDE, 0xAD, 0xBE, 0xEF>>)
    b2 = :binary.copy(<<0xDE, 0xAD, 0xBE, 0xEF>>)

    %{
      "by_name" => by_name,
      "by_code" => by_code,
      "by_id" => by_id,
      "by_uid" => by_uid,
      "by_tier" => by_tier,
      "nested" => nested,
      "text_a" => s1,
      "text_b" => s2,
      "blob_a" => b1,
      "blob_b" => b2
    }
  end

  def check_bundle(val, want_wire, want_json, tag) do
    case T.save("Bundle", val) do
      {:ok, w} when w == want_wire -> :ok
      other -> raise "FAILED: wire #{tag}: #{inspect(other)}"
    end

    case T.to_json("Bundle", val) do
      {:ok, j} when j == want_json -> :ok
      other -> raise "FAILED: json #{tag}: #{inspect(other)}"
    end
  end

  def run(dir) do
    want_wire = File.read!(Path.join(dir, "canonical.bin"))
    want_json = File.read!(Path.join(dir, "canonical.json"))

    for jf <- ["json_sorted.json", "json_reverse.json", "json_shuffled.json"] do
      jraw = File.read!(Path.join(dir, jf))
      case T.from_json("Bundle", jraw) do
        {:ok, val, _rep} -> check_bundle(val, want_wire, want_json, jf)
        other -> raise "FAILED: from_json #{jf}: #{inspect(other)}"
      end
    end

    for m <- 0..2 do
      check_bundle(build_native(m), want_wire, want_json, "native_#{m}")
    end

    IO.puts("CANONICAL_OK elixir 6")
  end
end

[dir] = System.argv()
CanonicalRunner.run(dir)
`
	mainExs := filepath.Join(workDir, "main.exs")
	if err := os.WriteFile(mainExs, []byte(exSrc), 0o644); err != nil {
		return err
	}
	out, err := runChecked(workDir, extraEnv, elixirBin, "-pa", ebinDir, mainExs, corpusDir)
	return requireToken(out, err, "elixir")
}
