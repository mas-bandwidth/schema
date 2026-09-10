// THE OPTIONAL CORPUS, written by the C++ REFERENCE (docs/SPEC-TABLES.md
// §3.4). The fixed form is the ONE form where `?T` and a plain `T` are not the
// same bytes — an optional rides as kind 35, a present byte in front of a
// payload that rides WHOLE — so the present byte's POSITION is a wire fact,
// and a port that placed it one byte off would round trip its own records
// perfectly and disagree with every peer.
//
// This program writes that disagreement's oracle: the reference's own bytes for
// records whose optionals are present, absent, and mixed, at every place a
// present byte can ride — a nested type at the root, a scalar, an enum, and
// one INSIDE A NESTED TABLE, where the flag rides at the nested body's own
// offsets and not the root's.
//
//   p1.bin    one tblp1::Chain — `link` nests Link BY VALUE (kind 13)
//   p3.bin    two tblp3::Chain — `link` is `?Link` (kind 35), present then absent
//   fo1.bin   four tblfo1::OptRoot — every combination FO1.schema names
//
// The values are stated in FULL in the comments at each record and rebuilt
// field for field by the JavaScript leg (test/js-tables/fixedform.mjs), which
// then checks its own bytes against these files and reads these files back. A
// reader and a writer that share one offset mistake round trip perfectly and
// are both wrong, which is why the bytes come from the OTHER language.
//
// The reference reads each file back through its own plan-driven loop before
// writing it, so a corpus that ships is one the reference agrees with end to
// end.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <new>
#include <vector>
#include "P1Table.h"
#include "P3Table.h"
#include "FO1Table.h"

static bool write_file( const char * path, const std::vector<uint8_t> & data )
{
    FILE * f = std::fopen( path, "wb" );
    if ( !f ) return false;
    const bool ok = std::fwrite( data.data(), 1, data.size(), f ) == data.size();
    return std::fclose( f ) == 0 && ok;
}

// set_text lays a string(N) member down the way every port must: the bytes,
// then the USED LENGTH beside them.
template <int N>
static void set_text( char ( & dst )[N], int32_t & length, const char * s )
{
    std::memset( dst, 0, sizeof( dst ) );
    const size_t n = std::strlen( s );
    std::memcpy( dst, s, n );
    length = (int32_t) n;
}

int main( int argc, char ** argv )
{
    if ( argc != 4 ) { std::fprintf( stderr, "usage: %s <p1.bin> <p3.bin> <fo1.bin>\n", argv[0] ); return 1; }

    // ---- p1.bin: `link` NESTS Link BY VALUE ---------------------------------
    {
        tblp1::Chain one;
        tblp1::ChainReset( one );
        set_text( one.name, one.name_length, "chain" );
        one.link.value = 500;
        set_text( one.link.tag, one.link.tag_length, "v" );

        std::vector<uint8_t> out( (size_t) tblp1::ChainFixedMeasure( 1 ) );
        if ( tblp1::ChainFixedSave( &one, 1, out.data(), (int64_t) out.size() ) != (int64_t) out.size() )
        { std::fprintf( stderr, "P1 save failed\n" ); return 1; }

        tblp1::Chain back;
        tblp1::TableReport r;
        std::vector<tblp1::TableFixedEntry> plan( 1024 );
        if ( tblp1::ChainFixedLoad( &back, 1, out.data(), (int64_t) out.size(), plan.data(), 1024, &r ) != 1 )
        { std::fprintf( stderr, "P1 read back failed\n" ); return 1; }
        if ( std::strcmp( back.name, "chain" ) != 0 || back.link.value != 500 )
        { std::fprintf( stderr, "P1 round trip disagrees with itself\n" ); return 1; }
        if ( !write_file( argv[1], out ) ) { std::fprintf( stderr, "cannot write %s\n", argv[1] ); return 1; }
        std::printf( "p1.bin: 1 record, block %d bytes, body %d, record %d, hash 0x%016llx\n",
                     (int) tblp1::ChainFixedLayoutBytes, (int) tblp1::ChainFixedBodyBytes,
                     (int) tblp1::ChainFixedRecordBytes, (unsigned long long) tblp1::ChainFixedHash );
    }

    // ---- p3.bin: `link` IS `?Link` — present, then absent -------------------
    //
    // RECORD 0: name "chain", link PRESENT, link.value 500, link.tag "v"
    // RECORD 1: name "bare",  link ABSENT — and its payload left at the reset's
    //           zeros, which is the only absent payload two ports can agree on:
    //           the storage behind a flag that says absent is not part of the
    //           logical value, so a writer is free to leave anything there.
    {
        tblp3::Chain two[2];
        tblp3::ChainReset( two[0] );
        set_text( two[0].name, two[0].name_length, "chain" );
        two[0].link_present = true;
        two[0].link.value = 500;
        set_text( two[0].link.tag, two[0].link.tag_length, "v" );

        tblp3::ChainReset( two[1] );
        set_text( two[1].name, two[1].name_length, "bare" );
        two[1].link_present = false;

        std::vector<uint8_t> out( (size_t) tblp3::ChainFixedMeasure( 2 ) );
        if ( tblp3::ChainFixedSave( two, 2, out.data(), (int64_t) out.size() ) != (int64_t) out.size() )
        { std::fprintf( stderr, "P3 save failed\n" ); return 1; }

        tblp3::Chain back[2];
        tblp3::TableReport r;
        std::vector<tblp3::TableFixedEntry> plan( 1024 );
        if ( tblp3::ChainFixedLoad( back, 2, out.data(), (int64_t) out.size(), plan.data(), 1024, &r ) != 2 )
        { std::fprintf( stderr, "P3 read back failed\n" ); return 1; }
        if ( !back[0].link_present || back[0].link.value != 500 || back[1].link_present )
        { std::fprintf( stderr, "P3 round trip disagrees with itself\n" ); return 1; }
        if ( !write_file( argv[2], out ) ) { std::fprintf( stderr, "cannot write %s\n", argv[2] ); return 1; }
        std::printf( "p3.bin: 2 records, block %d bytes, body %d, record %d, hash 0x%016llx\n",
                     (int) tblp3::ChainFixedLayoutBytes, (int) tblp3::ChainFixedBodyBytes,
                     (int) tblp3::ChainFixedRecordBytes, (unsigned long long) tblp3::ChainFixedHash );
    }

    // ---- fo1.bin: every place a present byte can ride ----------------------
    {
        using namespace tblfo1;
        OptRoot four[4];

        // RECORD 0 — EVERY OPTIONAL PRESENT, and the union on its first arm
        OptRootReset( four[0] );
        set_text( four[0].name, four[0].name_length, "all" );
        four[0].plain = 101;
        four[0].opt_present = true;
        four[0].opt.v = 11;
        set_text( four[0].opt.tag, four[0].opt.tag_length, "op" );
        four[0].num_present = true;
        four[0].num = 12;
        four[0].mark_present = true;
        four[0].mark = Grade::Gold;
        four[0].wrap.n = 13;
        four[0].wrap.leaf_present = true;
        four[0].wrap.leaf.v = 14;
        set_text( four[0].wrap.leaf.tag, four[0].wrap.leaf.tag_length, "wl" );
        ::new ( (void *) &four[0].effect.boost ) Boost{};
        four[0].effect.type = EffectType::Boost;
        four[0].effect.boost.power = 15;
        four[0].tail = 16;

        // RECORD 1 — EVERY OPTIONAL ABSENT, and the union on its second arm.
        // Nothing behind a zero flag is touched: the reset left it zero and
        // the writer must leave it zero, so the bytes are a function of the
        // VALUE and not of the storage the value happens to sit in.
        OptRootReset( four[1] );
        set_text( four[1].name, four[1].name_length, "none" );
        four[1].plain = 201;
        four[1].wrap.n = 202;
        ::new ( (void *) &four[1].effect.ward ) Ward{};
        four[1].effect.type = EffectType::Ward;
        four[1].effect.ward.charge = 0.5f;
        four[1].tail = 203;

        // RECORD 2 — MIXED, and the union at None: the root's optional absent
        // while the NESTED table's is present, which is the pair that catches a
        // present byte placed at the root's offsets instead of the nested body's
        OptRootReset( four[2] );
        set_text( four[2].name, four[2].name_length, "mix" );
        four[2].plain = 301;
        four[2].opt_present = false;
        four[2].num_present = true;
        four[2].num = -302;
        four[2].mark_present = true;
        four[2].mark = Grade::Bronze;
        four[2].wrap.n = 303;
        four[2].wrap.leaf_present = true;
        four[2].wrap.leaf.v = -304;
        set_text( four[2].wrap.leaf.tag, four[2].wrap.leaf.tag_length, "deep" );
        four[2].tail = 305;

        // RECORD 3 — THE OTHER WAY ROUND: the root's optional present and the
        // nested table's absent
        OptRootReset( four[3] );
        set_text( four[3].name, four[3].name_length, "root only" );
        four[3].plain = 401;
        four[3].opt_present = true;
        four[3].opt.v = 402;
        set_text( four[3].opt.tag, four[3].opt.tag_length, "r" );
        four[3].num_present = false;
        four[3].mark_present = false;
        four[3].wrap.n = 403;
        four[3].wrap.leaf_present = false;
        ::new ( (void *) &four[3].effect.ward ) Ward{};
        four[3].effect.type = EffectType::Ward;
        four[3].effect.ward.charge = -1.25f;
        four[3].tail = 404;

        std::vector<uint8_t> out( (size_t) OptRootFixedMeasure( 4 ) );
        if ( OptRootFixedSave( four, 4, out.data(), (int64_t) out.size() ) != (int64_t) out.size() )
        { std::fprintf( stderr, "FO1 save failed\n" ); return 1; }

        OptRoot back[4];
        TableReport r;
        std::vector<TableFixedEntry> plan( 1024 );
        if ( OptRootFixedLoad( back, 4, out.data(), (int64_t) out.size(), plan.data(), 1024, &r ) != 4 )
        { std::fprintf( stderr, "FO1 read back failed\n" ); return 1; }
        if ( !back[0].opt_present || back[0].opt.v != 11 || back[1].opt_present ||
             !back[2].wrap.leaf_present || back[2].wrap.leaf.v != -304 || back[3].wrap.leaf_present )
        { std::fprintf( stderr, "FO1 round trip disagrees with itself\n" ); return 1; }
        if ( !write_file( argv[3], out ) ) { std::fprintf( stderr, "cannot write %s\n", argv[3] ); return 1; }
        std::printf( "fo1.bin: 4 records, block %d bytes, body %d, record %d, hash 0x%016llx\n",
                     (int) OptRootFixedLayoutBytes, (int) OptRootFixedBodyBytes,
                     (int) OptRootFixedRecordBytes, (unsigned long long) OptRootFixedHash );
    }
    return 0;
}
