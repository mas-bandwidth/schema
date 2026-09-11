// THE FIXED FORM'S CROSS-LANGUAGE BYTE ORACLE (docs/SPEC-TABLES.md §3.4).
//
// The C++ backend is the REFERENCE for this form, so the reference is what
// writes the bytes and every port matches them. This binary writes one form-3
// FILE per root, with values set by hand so nothing passes by accident, and a
// port's leg proves itself against those files two ways:
//
//   THE WRITE   read the file, save the values back, and the bytes must be
//               identical. That reaches every field: a byte a port encodes
//               differently is a byte that does not come back.
//   THE READ    read a file written under ANOTHER schema's block, which is the
//               plan path and the whole of what §3.4's versioning invariant is
//               worth.
//
// It writes and says nothing else, so a port's leg can diff the files without
// parsing a word of output.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "FX1Table.h"
#include "FX2Table.h"
#include "P1Table.h"
#include "P3Table.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "FXWTable.h"
#include "FU1Table.h"
#include "FU2Table.h"

// ---- rowan/cpp-versioning-numbers: BEGIN ----------------------------------
// THE VERSIONING LAW'S CORPUS (docs/FIXED-FORM-VERSIONING-TESTS.md): `old_<row>.bin`
// and `new_<row>.bin` per row, the two read columns' shared corpus, so every leg
// reads the SAME bytes the C++ reference wrote and the card on a port's job names
// one file. The values are the ones test/tables/versioning_numbers.cpp asserts.
#include "serialize.h"
#include "VOLD_array_bounded_growTable.h"
#include "VNEW_array_bounded_growTable.h"
#include "VOLD_array_fixed_growTable.h"
#include "VNEW_array_fixed_growTable.h"
#include "VOLD_array_elem_widenTable.h"
#include "VNEW_array_elem_widenTable.h"
#include "VOLD_constant_growTable.h"
#include "VNEW_constant_growTable.h"
#include "VOLD_string_growTable.h"
#include "VNEW_string_growTable.h"
#include "VOLD_wstring_growTable.h"
#include "VNEW_wstring_growTable.h"
#include "VOLD_bytes_growTable.h"
#include "VNEW_bytes_growTable.h"
#include "VOLD_int_widenTable.h"
#include "VNEW_int_widenTable.h"
#include "VOLD_uint_widenTable.h"
#include "VNEW_uint_widenTable.h"
#include "VOLD_float_widenTable.h"
#include "VNEW_float_widenTable.h"
#include "VOLD_range_widenTable.h"
#include "VNEW_range_widenTable.h"
#include "VOLD_bits_growTable.h"
#include "VNEW_bits_growTable.h"
#include "VOLD_fixed_I_growTable.h"
#include "VNEW_fixed_I_growTable.h"
#include "VOLD_optional_addTable.h"
#include "VNEW_optional_addTable.h"
#include "VOLD_floorTable.h"
#include "VMID_floorTable.h"
#include "VNEW_floorTable.h"
#include "VOLD_lineage_mergeTable.h"
#include "VBRA_lineage_mergeTable.h"
#include "VBRB_lineage_mergeTable.h"
#include "VNEW_lineage_mergeTable.h"
// ---- rowan/cpp-versioning-numbers: END ------------------------------------
// ==== BEGIN rowan/cpp-versioning-lists: the LIST rows lineage pairs ====
// docs/FIXED-FORM-VERSIONING-TESTS.md: two schemas per row, one table name,
// one package each, so both generations of one lineage compile into this binary.
#include"VOLD_field_appendTable.h"
#include "VNEW_field_appendTable.h"
#include"VOLD_field_deprecateTable.h"
#include "VNEW_field_deprecateTable.h"
#include"VOLD_field_undeprecateTable.h"
#include "VNEW_field_undeprecateTable.h"
#include"VOLD_enum_appendTable.h"
#include "VNEW_enum_appendTable.h"
#include"VOLD_enum_widthTable.h"
#include "VNEW_enum_widthTable.h"
#include"VOLD_union_appendTable.h"
#include "VNEW_union_appendTable.h"
#include"VOLD_union_arm_payload_widenTable.h"
#include "VNEW_union_arm_payload_widenTable.h"
#include"VOLD_flags_appendTable.h"
#include "VNEW_flags_appendTable.h"
#include"VOLD_keyed_array_enum_appendTable.h"
#include "VNEW_keyed_array_enum_appendTable.h"
#include"VOLD_nested_appendTable.h"
#include "VNEW_nested_appendTable.h"
#include"VOLD_rename_without_wasTable.h"
#include "VNEW_rename_without_wasTable.h"
// ==== END rowan/cpp-versioning-lists ===================================

static bool spill( const char * dir, const char * name, const std::vector<uint8_t> & data )
{
    char path[1024];
    std::snprintf( path, sizeof( path ), "%s/%s", dir, name );
    FILE * f = std::fopen( path, "wb" );
    if ( !f ) { std::fprintf( stderr, "cannot write %s\n", path ); return false; }
    const bool ok = std::fwrite( data.data(), 1, data.size(), f ) == data.size();
    return std::fclose( f ) == 0 && ok;
}

// ---- rowan/corpus-manifest: BEGIN -----------------------------------------
// THE MANIFEST (docs/FIXED-FORM-ALGORITHM.md §5.7 step 1, ruling #32).
//
// Four legs in a row had to OPEN THIS FILE to learn what the corpus rows carry
// — that `int_widen`'s lead and trail are 0xAAAAAAAA and 0xBBBBBBBB on the wire
// while the schema declares 1 and 2, which table is a row's root when a schema
// declares two, how many records a row has, the names of the non-pair files —
// and §5.7 forbids a port reading the reference's emitter for exactly the
// reason that it then copies its accidents. So the emitter SAYS what it wrote:
// `manifest.txt`, one line per corpus file, beside the bytes.
//
// Every value below is stored through MS / MSI / MSE / MSEI / MSTR / MWCPY,
// which perform the assignment AND record the line from the SAME expression,
// so a changed value changes both or neither. `emit` finalises the row from the
// file name and the type, so the row, the side, the root and the record count
// cannot be stated wrongly by hand either.
#include <string>
#include <typeinfo>

struct ManifestRow
{
    std::string file, row, side, root;
    long long records = 0;
    std::vector<std::string> values;
};

static std::vector<ManifestRow> g_manifest;     // the rows, in the order written
static std::vector<std::string> g_values;       // the row being written

// `v[0].nested.a` -> `r0.nested.a`, and a subscript that is a LOOP VARIABLE or
// an enum takes the value the call site hands over, in order, so the path holds
// the index the bytes hold and never the variable's name.
static std::string man_path( const char * expr, std::initializer_list<long long> idx )
{
    std::string out;
    const long long * next = idx.begin();
    for ( const char * p = expr; *p; )
    {
        if ( *p == '[' )
        {
            const char * q = p + 1;
            while ( *q && *q != ']' ) { q++; }
            const std::string inside( p + 1, (size_t) ( q - p - 1 ) );
            bool digits = !inside.empty();
            for ( size_t i = 0; i < inside.size(); ++i )
            {
                if ( inside[i] < '0' || inside[i] > '9' ) { digits = false; }
            }
            char buf[32];
            if ( digits ) { std::snprintf( buf, sizeof( buf ), "[%s]", inside.c_str() ); }
            else
            {
                const long long n = next != idx.end() ? *next++ : -1;
                std::snprintf( buf, sizeof( buf ), "[%lld]", n );
            }
            out += buf;
            p = *q ? q + 1 : q;
            continue;
        }
        out += *p++;
    }
    // the RECORD subscript at the head is the record number: `v[1].` -> `r1.`
    const size_t open = out.find( '[' );
    const size_t close = out.find( ']' );
    if ( open != std::string::npos && close != std::string::npos && close > open )
    {
        bool ident = open > 0;
        for ( size_t i = 0; i < open; ++i )
        {
            const char c = out[i];
            if ( !( ( c >= 'a' && c <= 'z' ) || ( c >= 'A' && c <= 'Z' ) || c == '_' ) ) { ident = false; }
        }
        if ( ident ) { out = "r" + out.substr( open + 1, close - open - 1 ) + out.substr( close + 1 ); }
    }
    return out;
}

static std::string man_quote( const std::string & raw )
{
    std::string out = "\"";
    for ( size_t i = 0; i < raw.size(); ++i )
    {
        const unsigned char c = (unsigned char) raw[i];
        if ( c == '"' || c == '\\' || c == ',' ) { out += '\\'; out += (char) c; }
        else if ( c >= 0x20 && c < 0x7F ) { out += (char) c; }
        else { char b[8]; std::snprintf( b, sizeof( b ), "\\x%02X", c ); out += b; }
    }
    return out + "\"";
}

// THE VALUE'S TEXT. Integers in decimal, a float by its BITS as well as its
// digits (a signalling NaN's payload is the value, the FU ruling), a bool as
// true/false, text quoted, a code unit in hex.
inline std::string man_text( bool v ) { return v ? "true" : "false"; }
inline std::string man_text( char16_t v ) { char b[16]; std::snprintf( b, sizeof( b ), "0x%04X", (unsigned) v ); return b; }
static std::string man_dec( long long v ) { char b[32]; std::snprintf( b, sizeof( b ), "%lld", v ); return b; }
static std::string man_udec( unsigned long long v ) { char b[32]; std::snprintf( b, sizeof( b ), "%llu", v ); return b; }
inline std::string man_text( signed char v ) { return man_dec( v ); }
inline std::string man_text( unsigned char v ) { return man_udec( v ); }
inline std::string man_text( short v ) { return man_dec( v ); }
inline std::string man_text( unsigned short v ) { return man_udec( v ); }
inline std::string man_text( int v ) { return man_dec( v ); }
inline std::string man_text( unsigned int v ) { return man_udec( v ); }
inline std::string man_text( long v ) { return man_dec( v ); }
inline std::string man_text( unsigned long v ) { return man_udec( v ); }
inline std::string man_text( long long v ) { return man_dec( v ); }
inline std::string man_text( unsigned long long v ) { return man_udec( v ); }

inline std::string man_text( float v )
{
    uint32_t bits; std::memcpy( &bits, &v, 4 );
    char b[64]; std::snprintf( b, sizeof( b ), "%.9g|0x%08X", (double) v, bits );
    return b;
}

inline std::string man_text( double v )
{
    uint64_t bits; std::memcpy( &bits, &v, 8 );
    char b[80]; std::snprintf( b, sizeof( b ), "%.17g|0x%016llX", v, (unsigned long long) bits );
    return b;
}

static std::string man_wtext( const char16_t * src, size_t n )
{
    std::string out = "u\"";
    for ( size_t i = 0; i < n; ++i )
    {
        char b[16];
        const unsigned u = (unsigned) src[i];
        if ( u >= 0x20 && u < 0x7F && u != '"' && u != '\\' && u != ',' ) { out += (char) u; }
        else { std::snprintf( b, sizeof( b ), "\\u%04X", u ); out += b; }
    }
    return out + "\"";
}

static void man_add( const std::string & path, const std::string & text )
{
    g_values.push_back( path + "=" + text );
}

// the ASSIGNMENT IS THE RECORD: one expression, stored and said
#define MS( LV, VAL )         do { man_add( man_path( #LV, {} ), man_text( (LV) = (VAL) ) ); } while ( 0 )
#define MSI( LV, VAL, ... )   do { man_add( man_path( #LV, { __VA_ARGS__ } ), man_text( (LV) = (VAL) ) ); } while ( 0 )
#define MSE( LV, VAL )        do { man_add( man_path( #LV, {} ), man_dec( (long long) ( (LV) = (VAL) ) ) ); } while ( 0 )
#define MSEI( LV, VAL, ... )  do { man_add( man_path( #LV, { __VA_ARGS__ } ), man_dec( (long long) ( (LV) = (VAL) ) ) ); } while ( 0 )
#define MSTR( LV, VAL )       do { std::strcpy( LV, VAL ); man_add( man_path( #LV, {} ), man_quote( VAL ) ); } while ( 0 )
#define MSTRI( LV, VAL, ... ) do { std::strcpy( LV, VAL ); man_add( man_path( #LV, { __VA_ARGS__ } ), man_quote( VAL ) ); } while ( 0 )

// the wide copy, with the source array's own length: a unit count, never bytes
template <size_t N>
static void man_wcopy( char16_t * dst, const char16_t ( & src )[N], const std::string & path )
{
    std::memcpy( dst, src, N * sizeof( char16_t ) );
    man_add( path, man_wtext( src, N ) );
}
#define MWCPY( LV, SRC )      man_wcopy( LV, SRC, man_path( #LV, {} ) )

// THE ROOT'S NAME FROM THE TYPE ITSELF (Itanium `N6tblfx16FxRootE`, or MSVC's
// `struct tblfx1::FxRoot`): a renamed table renames its manifest line too.
static std::string man_root_of( const char * type_name )
{
    const std::string s( type_name );
    const size_t sep = s.rfind( "::" );
    if ( sep != std::string::npos ) { return s.substr( sep + 2 ); }
    std::string last;
    size_t i = 0;
    while ( i < s.size() )
    {
        if ( s[i] >= '1' && s[i] <= '9' )
        {
            size_t len = 0;
            while ( i < s.size() && s[i] >= '0' && s[i] <= '9' ) { len = len * 10 + (size_t) ( s[i] - '0' ); i++; }
            if ( i + len <= s.size() ) { last = s.substr( i, len ); i += len; continue; }
            break;
        }
        i++;
    }
    return last.empty() ? s : last;
}

// `old_array_bounded_grow.bin` -> side `old`, row `array_bounded_grow`; a file
// with no side prefix is a NON-PAIR row and says so.
static void man_row_and_side( const std::string & file, std::string & row, std::string & side )
{
    const size_t dot = file.rfind( ".bin" );
    const std::string stem = dot == std::string::npos ? file : file.substr( 0, dot );
    const char * sides[5] = { "old_", "new_", "mid_", "a_", "b_" };
    for ( int k = 0; k < 5; ++k )
    {
        const std::string p( sides[k] );
        if ( stem.size() > p.size() && stem.compare( 0, p.size(), p ) == 0 )
        {
            side = p.substr( 0, p.size() - 1 );
            row = stem.substr( p.size() );
            return;
        }
    }
    side = "none";
    row = stem;
}

static void man_finish( const char * file, const char * root, long long records )
{
    ManifestRow r;
    r.file = file;
    r.root = root;
    r.records = records;
    man_row_and_side( r.file, r.row, r.side );
    r.values.swap( g_values );
    g_manifest.push_back( r );
}

static bool man_write( const char * dir )
{
    std::string text;
    for ( size_t i = 0; i < g_manifest.size(); ++i )
    {
        const ManifestRow & r = g_manifest[i];
        char head[512];
        std::snprintf( head, sizeof( head ), "file=%s row=%s side=%s root=%s records=%lld values=",
                       r.file.c_str(), r.row.c_str(), r.side.c_str(), r.root.c_str(), r.records );
        text += head;
        for ( size_t k = 0; k < r.values.size(); ++k )
        {
            if ( k != 0 ) { text += ","; }
            text += r.values[k];
        }
        text += "\n";
    }
    const std::vector<uint8_t> bytes( text.begin(), text.end() );
    return spill( dir, "manifest.txt", bytes );
}
// ---- rowan/corpus-manifest: END -------------------------------------------

template <typename T, typename Measure, typename Save>
static bool emit( const char * dir, const char * name, const std::vector<T> & values, Measure measure, Save save )
{
    std::vector<uint8_t> out( (size_t) measure( (int64_t) values.size() ) );
    if ( save( values.data(), (int64_t) values.size(), out.data(), (int64_t) out.size() ) != (int64_t) out.size() )
    {
        std::fprintf( stderr, "%s: save refused\n", name );
        return false;
    }
    // ---- rowan/corpus-manifest ----
    // the row's own line, from the file name, the TYPE and the record count:
    // three things a hand-written manifest gets wrong and this one cannot
    man_finish( name, man_root_of( typeid( T ).name() ).c_str(), (long long) values.size() );
    return spill( dir, name, out );
}


// A FLOAT RIDES AS ITS BIT PATTERN (docs/SPEC-TABLES.md §3, §4, schema#480), so
// a SIGNALLING NaN has to be put in place through its bits: a literal would be
// quieted by the compiler before the writer ever saw it.
static float float_from_bits( uint32_t bits ) { float f; std::memcpy( &f, &bits, 4 ); return f; }
static double double_from_bits( uint64_t bits ) { double d; std::memcpy( &d, &bits, 8 ); return d; }

// ---- FX1 / FX2: the versioning pair ---------------------------------------
//
// The five edits FX1.schema names, and the values are the ones a port's leg
// asserts on the other side of the plan.

static bool fx1_file( const char * dir )
{
    std::vector<tblfx1::FxRoot> v( 2 );
    tblfx1::FxRootReset( v[0] );
    MS( v[0].keep, 4242u );
    MS( v[0].narrow, 40000u ); // a uint16 value FX2's WIDENED read must reproduce
    MS( v[0].renamed, 321 );
    MS( v[0].gone, 654 );
    MS( v[0].nested.a, 111 );
    MS( v[0].nested.b, 222 );
    // A SHORT STRING AND A PARTLY-USED ARRAY, which is where §3.4's "the slack
    // is zero" is worth pinning: the bytes past the used length and past the
    // live count are the template's zeros in every port or they are not the
    // same bytes.
    MSTR( v[0].label, "fx1" );
    MS( v[0].label_length, 3 );
    MS( v[0].marks[0], 101 );
    MS( v[0].marks[1], 202 );
    MS( v[0].marks_count, 2 );
    // a `bytes(N)` PARTLY USED: an array of u8 on this wire, so the slack past
    // the live length is the template's zeros here too
    MS( v[0].blob[0], 0xDE ); MS( v[0].blob[1], 0xAD ); MS( v[0].blob[2], 0xBE ); MS( v[0].blob[3], 0xEF );
    MS( v[0].blob_length, 4 );
    tblfx1::FxRootReset( v[1] );
    MS( v[1].keep, 1u );
    MS( v[1].narrow, 2u );
    MS( v[1].renamed, 3 );
    MS( v[1].gone, 4 );
    MS( v[1].nested.a, 5 );
    MS( v[1].nested.b, 6 );
    MS( v[1].label_length, 0 ); // nothing used at all: the WHOLE span is slack
    MS( v[1].marks_count, 0 );
    MS( v[1].blob_length, 0 );
    return emit( dir, "fx1.bin", v, tblfx1::FxRootFixedMeasure, tblfx1::FxRootFixedSave );
}

static bool fx2_file( const char * dir )
{
    std::vector<tblfx2::FxRoot> v( 1 );
    tblfx2::FxRootReset( v[0] );
    MS( v[0].keep, 5150u );
    MS( v[0].narrow, 70000u ); // wider than FX1 holds: a kind that MOVED, not a widening
    MS( v[0].renamed_to, 808 );
    MS( v[0].added, 909 );
    MS( v[0].nested.a, 33 );
    MS( v[0].nested.b, 44 );
    MS( v[0].extra.x, 55 );
    MS( v[0].extra.y, 66 );
    MSTR( v[0].label, "fx2" );
    MS( v[0].label_length, 3 );
    MS( v[0].marks[0], 303 );
    MS( v[0].marks_count, 1 );
    MS( v[0].blob[0], 0x01 ); MS( v[0].blob[1], 0x02 ); MS( v[0].blob[2], 0x03 );
    MS( v[0].blob_length, 3 );
    return emit( dir, "fx2.bin", v, tblfx2::FxRootFixedMeasure, tblfx2::FxRootFixedSave );
}

// ---- P1 / P3: a value against an optional ----------------------------------

static bool p1_file( const char * dir )
{
    std::vector<tblp1::Chain> v( 1 );
    tblp1::ChainReset( v[0] );
    MSTR( v[0].name, "chain-one" );
    MS( v[0].name_length, 9 );
    MS( v[0].link.value, 77 );
    MSTR( v[0].link.tag, "tagged" );
    MS( v[0].link.tag_length, 6 );
    return emit( dir, "p1.bin", v, tblp1::ChainFixedMeasure, tblp1::ChainFixedSave );
}

static bool p3_file( const char * dir )
{
    std::vector<tblp3::Chain> v( 2 );
    tblp3::ChainReset( v[0] );
    MSTR( v[0].name, "present" );
    MS( v[0].name_length, 7 );
    MS( v[0].link_present, true );
    MS( v[0].link.value, 88 );
    MSTR( v[0].link.tag, "here" );
    MS( v[0].link.tag_length, 4 );
    tblp3::ChainReset( v[1] );
    MSTR( v[1].name, "absent" );
    MS( v[1].name_length, 6 );
    MS( v[1].link_present, false );
    // THE PAYLOAD RIDES WHOLE WHETHER OR NOT IT IS PRESENT (§3.4), and when the
    // flag is 0 what rides is ZERO. These stores are here to prove it: the
    // storage carries values, the flag says absent, and the file's bytes for
    // this payload are the template's zeros all the same.
    MS( v[1].link.value, 99 );
    MSTR( v[1].link.tag, "still" );
    MS( v[1].link.tag_length, 5 );
    return emit( dir, "p3.bin", v, tblp3::ChainFixedMeasure, tblp3::ChainFixedSave );
}

// ---- the rich shape: keyed arrays nesting keyed arrays, and an optional ----

static bool keyed_file( const char * dir )
{
    std::vector<tabledemo::KeyedConfig> v( 2 );
    for ( int k = 0; k < 2; ++k )
    {
        tabledemo::KeyedConfigReset( v[k] );
        for ( int t = 0; t < 3; ++t )
        {
            MSI( v[k].teams.slots[t].spawn_count, 4 + t + k * 10, (long long) ( k ), (long long) ( t ) );
            const char * names[3] = { "red", "blue", "green" };
            MSTRI( v[k].teams.slots[t].banner, names[t], (long long) ( k ), (long long) ( t ) );
            MSI( v[k].teams.slots[t].banner_length, (int32_t) std::strlen( names[t] ), (long long) ( k ), (long long) ( t ) );
            MSI( v[k].scores.per_team[t], 1000 * ( t + 1 ) + k, (long long) ( k ), (long long) ( t ) );
        }
        for ( int h = 0; h < 3; ++h )
        {
            MSI( v[k].hulls.slots[h].health, 100.0f + (float) h + (float) k, (long long) ( k ), (long long) ( h ) );
            MSI( v[k].hulls.slots[h].mass, 1.5f * (float) ( h + 1 ), (long long) ( k ), (long long) ( h ) );
            for ( int w = 0; w < 3; ++w )
            {
                MSI( v[k].hulls.slots[h].turrets.slots[w].damage, 10.0f + (float) ( h * 3 + w ), (long long) ( k ), (long long) ( h ), (long long) ( w ) );
                MSI( v[k].hulls.slots[h].turrets.slots[w].cooldown, 0.25f * (float) ( w + 1 ), (long long) ( k ), (long long) ( h ), (long long) ( w ) );
                MSI( v[k].hulls.slots[h].turrets.slots[w].gunner_present, ( ( h + w ) % 2 ) == 0, (long long) ( k ), (long long) ( h ), (long long) ( w ) );
                MSI( v[k].hulls.slots[h].turrets.slots[w].gunner.reaction, 0.2f + 0.1f * (float) w, (long long) ( k ), (long long) ( h ), (long long) ( w ) );
                MSI( v[k].hulls.slots[h].turrets.slots[w].gunner.tracking, ( w % 2 ) == 1, (long long) ( k ), (long long) ( h ), (long long) ( w ) );
            }
        }
    }
    return emit( dir, "keyed.bin", v, tabledemo::KeyedConfigFixedMeasure, tabledemo::KeyedConfigFixedSave );
}

// ---- the packed corpus's root: counted arrays, an enum with a declared
// default, a fixed array of floats, and an optional section inside a record ---

static bool pack_file( const char * dir )
{
    std::vector<tabledemo::PackConfig> v( 2 );
    for ( int k = 0; k < 2; ++k )
    {
        tabledemo::PackConfigReset( v[k] );
        MSI( v[k].version, 7u + (uint32_t) k, (long long) ( k ) );
        MSI( v[k].global.tick_rate, 120u - (uint32_t) k, (long long) ( k ) );
        MSEI( v[k].global.difficulty, k == 0 ? tabledemo::Difficulty::Hard : tabledemo::Difficulty::Easy, (long long) ( k ) );
        const char * note = k == 0 ? "first build" : "second build";
        MSTRI( v[k].global.build_note, note, (long long) ( k ) );
        MSI( v[k].global.build_note_length, (int32_t) std::strlen( note ), (long long) ( k ) );
        for ( int i = 0; i < 3; ++i ) { MSI( v[k].global.spawn_delays[i], 0.5f * (float) ( i + 1 + k ), (long long) ( k ), (long long) ( i ) ); }
        for ( int s = 0; s < 3; ++s )
        {
            const char * names[3] = { "fighter", "bomber", "scout" };
            MSTRI( v[k].ships.slots[s].display_name, names[s], (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].display_name_length, (int32_t) std::strlen( names[s] ), (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].health, 100.0f + (float) ( s * 10 + k ), (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].mass, 1.0f + 0.25f * (float) s, (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].hardpoints_count, s + 1, (long long) ( k ), (long long) ( s ) );
            for ( int h = 0; h < s + 1; ++h ) { MSI( v[k].ships.slots[s].hardpoints[h], h + 1, (long long) ( k ), (long long) ( s ), (long long) ( h ) ); }
            MSI( v[k].ships.slots[s].gunner_present, ( s % 2 ) == 0, (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].gunner.reaction, 0.2f + 0.05f * (float) s, (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].gunner.tracking, ( s % 2 ) == 1, (long long) ( k ), (long long) ( s ) );
            const char * calls[3] = { "ace", "hammer", "ghost" };
            MSTRI( v[k].ships.slots[s].gunner.callsign, calls[s], (long long) ( k ), (long long) ( s ) );
            MSI( v[k].ships.slots[s].gunner.callsign_length, (int32_t) std::strlen( calls[s] ), (long long) ( k ), (long long) ( s ) );
            MSI( v[k].thresholds.slots[s], 100 * ( s + 1 ) + k, (long long) ( k ), (long long) ( s ) );
        }
        MSI( v[k].reserves_count, 2, (long long) ( k ) );
        for ( int r = 0; r < 2; ++r )
        {
            const char * names[2] = { "spare-a", "spare-b" };
            MSTRI( v[k].reserves[r].display_name, names[r], (long long) ( k ), (long long) ( r ) );
            MSI( v[k].reserves[r].display_name_length, (int32_t) std::strlen( names[r] ), (long long) ( k ), (long long) ( r ) );
            MSI( v[k].reserves[r].health, 50.0f + (float) r, (long long) ( k ), (long long) ( r ) );
            MSI( v[k].reserves[r].mass, 2.0f, (long long) ( k ), (long long) ( r ) );
            MSI( v[k].reserves[r].hardpoints_count, 1, (long long) ( k ), (long long) ( r ) );
            MSI( v[k].reserves[r].hardpoints[0], 8, (long long) ( k ), (long long) ( r ) );
            MSI( v[k].reserves[r].gunner_present, false, (long long) ( k ), (long long) ( r ) );
        }
    }
    return emit( dir, "pack.bin", v, tabledemo::PackConfigFixedMeasure, tabledemo::PackConfigFixedSave );
}

// ---- the WIDE TEXT unit (docs/SPEC-TABLES.md §3.4, kind 33) ----
//
// The wide flavour's ONLY oracle bytes. A length in CODE UNITS and 2N bytes
// behind it, at the body's head and again at a NESTED offset, with a narrow
// `string(8)` between them so a leg that counted bytes where it owed units
// comes out wrong here and nowhere else has to catch it.
static bool fxw_file( const char * dir )
{
    std::vector<tblfxw::FxWide> v( 2 );

    tblfxw::FxWideReset( v[0] );
    // seven BASIC-PLANE code units, one short of the bound
    const char16_t hello[7] = { u'h', u'e', u'l', u'l', u'o', u' ', u'!' };
    MWCPY( v[0].caption, hello );
    MS( v[0].caption_length, 7 );
    MSTR( v[0].label, "narrow" );
    MS( v[0].label_length, 6 );
    const char16_t inner0[4] = { u'a', u'b', u'c', u'd' };
    MWCPY( v[0].inner.text, inner0 );
    MS( v[0].inner.text_length, 4 );
    MS( v[0].seq, 41 );

    tblfxw::FxWideReset( v[1] );
    // AN ASTRAL PAIR AND THE TWO BASIC-PLANE ENDS OF THE RANGE: a surrogate
    // pair is TWO code units and the length counts both, which is the number
    // a leg using bytes gets wrong by a factor of two.
    const char16_t astral[5] = { u'\uE000', 0xD83D, 0xDE00, u'\uFFFF', u'z' };
    MWCPY( v[1].caption, astral );
    MS( v[1].caption_length, 5 );
    MS( v[1].label_length, 0 ); // empty, and its eight bytes are slack
    MS( v[1].inner.text_length, 0 );
    MS( v[1].seq, 1000 ); // the declared max, so the clamp has no false case here

    return emit( dir, "fxw.bin", v, tblfxw::FxWideFixedMeasure, tblfxw::FxWideFixedSave );
}

// ---- FU1 / FU2: TEXT UNDER A UNION ARM, AND THE TWO WIDENING RUNGS ---------
//
// The nested-union pair was the one fixture in this corpus with NO oracle bytes
// (schema#876, card 15): every leg pinned FU1 by writing it itself and reading
// it back, so a leg whose bytes differ from the reference's by one byte passed
// in silence. These two files are those bytes.
//
// The record set is set by hand and reaches, in one file:
//   BOTH ARMS            the second arm (a `string(8)` between two scalars) and
//                        the first (a bare scalar), so the arms' differing
//                        sizes make the second arm's offsets its own
//   THE SIGNED RUNG      `mark int16` at -1, at INT16_MIN and at INT16_MAX, so
//                        the SIGN-EXTENDED read into FU2's `int32` has a true
//                        case at both ends and a false one: a leg that zero-
//                        extends lands 65535 for -1 and cannot hide it
//   THE FLOAT RUNG       `heat float32` as a SIGNALLING NaN with the smallest
//                        payload, as a negative signalling NaN with a rich one,
//                        and as an ordinary 1.5 beside them — the rung where a
//                        leg that widens through a hardware conversion quiets
//                        the NaN and loses the payload
//   THE TWO FORGED BYTES `flag` and `note`'s presence, which §3.4 spells as one
//                        byte that is true when it is NOT ZERO; the writer puts
//                        1 here and the leg's own forging case does the rest

static bool fu1_file( const char * dir )
{
    std::vector<tblfu1::FuRoot> v( 3 );

    // the SECOND arm, with text between its two scalars
    tblfu1::FuRootReset( v[0] );
    MS( v[0].flag, true );
    MS( v[0].note, 44 );
    MS( v[0].note_present, true );
    MSE( v[0].pick.type, tblfu1::PickType::Labelled );
    MS( v[0].pick.labelled.lead, 101 );
    MSTR( v[0].pick.labelled.label, "hello" );
    MS( v[0].pick.labelled.label_length, 5 );
    MS( v[0].pick.labelled.trail, 202 );
    MS( v[0].tail, 11 );
    MS( v[0].mark, -1 );                                   // the rung's headline case
    MS( v[0].heat, float_from_bits( 0x7F800001u ) );       // signalling, smallest payload

    // the FIRST arm, a bare scalar, and the optional ABSENT
    tblfu1::FuRootReset( v[1] );
    MS( v[1].flag, false );
    MS( v[1].note_present, false );
    MS( v[1].note, 0 );
    MSE( v[1].pick.type, tblfu1::PickType::Plain );
    MS( v[1].pick.plain.n, 303 );
    MS( v[1].tail, 12 );
    MS( v[1].mark, -32768 );                               // INT16_MIN: every sign bit set
    MS( v[1].heat, float_from_bits( 0xFFA5A5A5u ) );       // negative, signalling, rich payload

    // the SECOND arm again with its text WHOLLY UNUSED, so the eight bytes of
    // the span are slack and a positive `mark` gives the sign rung a false case
    tblfu1::FuRootReset( v[2] );
    MS( v[2].flag, true );
    MS( v[2].note, -7 );
    MS( v[2].note_present, true );
    MSE( v[2].pick.type, tblfu1::PickType::Labelled );
    MS( v[2].pick.labelled.lead, -5 );
    MS( v[2].pick.labelled.label_length, 0 );
    MS( v[2].pick.labelled.trail, 606 );
    MS( v[2].tail, 13 );
    MS( v[2].mark, 32767 );                                // INT16_MAX: positive, nothing to extend
    MS( v[2].heat, 1.5f );                                 // an ordinary float beside the NaNs

    return emit( dir, "fu1.bin", v, tblfu1::FuRootFixedMeasure, tblfu1::FuRootFixedSave );
}

// FU2's own bytes, for the OTHER direction: identity under FU2, and
// OLD-REFUSES-NEW under FU1. COMPILE from the lock (algorithm §5.3): FU1's
// lineage is itself alone, so fu2.bin is layout_newer before any record.
// The two rungs run FORWARD only — fu1.bin into FU2, asserted after the
// compiled read. `extra` is a field the older reader never locked.
static bool fu2_file( const char * dir )
{
    std::vector<tblfu2::FuRoot> v( 2 );

    tblfu2::FuRootReset( v[0] );
    MS( v[0].flag, true );
    MS( v[0].note, 55 );
    MS( v[0].note_present, true );
    MSE( v[0].pick.type, tblfu2::PickType::Labelled );
    MS( v[0].pick.labelled.lead, 111 );
    MSTR( v[0].pick.labelled.label, "wide" );
    MS( v[0].pick.labelled.label_length, 4 );
    MS( v[0].pick.labelled.trail, 222 );
    MS( v[0].tail, 21 );
    MS( v[0].mark, -70000 );                               // past everything an int16 holds
    MS( v[0].heat, double_from_bits( 0x7FF00DEFACED0001ull ) ); // signalling at sixty-four bits
    MS( v[0].extra, 909 );

    tblfu2::FuRootReset( v[1] );
    MS( v[1].flag, false );
    MS( v[1].note_present, false );
    MSE( v[1].pick.type, tblfu2::PickType::Plain );
    MS( v[1].pick.plain.n, 404 );
    MS( v[1].tail, 22 );
    MS( v[1].mark, 70000 );
    MS( v[1].heat, -2.25 );
    MS( v[1].extra, -11 );

    return emit( dir, "fu2.bin", v, tblfu2::FuRootFixedMeasure, tblfu2::FuRootFixedSave );
}

// ---- rowan/cpp-versioning-numbers: BEGIN ----------------------------------
// ONE FILE PER SIDE PER ROW. `emit` above takes a vector and the unit's measure
// and save, so every row below is one statement and its values are visible.
#define VROW( NS, TBL, NAME, ... )                                                           \
    do {                                                                                     \
        std::vector<NS::TBL> v( 1 );                                                          \
        NS::TBL##Reset( v[0] );                                                               \
        __VA_ARGS__                                                                           \
        if ( !emit( dir, NAME, v, NS::TBL##FixedMeasure, NS::TBL##FixedSave ) ) { return false; } \
    } while ( 0 )

static bool versioning_numbers_files( const char * dir )
{
    VROW( vold_array_bounded_grow, ArrayBoundedGrow, "old_array_bounded_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].vals_count, 4 );
          for ( int i = 0; i < 4; ++i ) { MSI( v[0].vals[i], 1000 + i, (long long) ( i ) ); } MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_array_bounded_grow, ArrayBoundedGrow, "new_array_bounded_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].vals_count, 8 );
          for ( int i = 0; i < 8; ++i ) { MSI( v[0].vals[i], 2000 + i, (long long) ( i ) ); } MS( v[0].trail, 0xBBBBBBBBu ); );

    // the element is a NESTED TYPE with NONZERO defaults (x = 7, y = 9), so the
    // reader's slack slots can tell an ELEMENT-DEFAULT prefill (bill §12.6)
    // apart from a plain zero fill. No written element is ever 7 or 9.
    VROW( vold_array_fixed_grow, ArrayFixedGrow, "old_array_fixed_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu );
          for ( int i = 0; i < 4; ++i ) { MSI( v[0].vals[i].x, -500 - i, (long long) ( i ) ); MSI( v[0].vals[i].y, -600 - i, (long long) ( i ) ); }
          MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_array_fixed_grow, ArrayFixedGrow, "new_array_fixed_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu );
          for ( int i = 0; i < 8; ++i ) { MSI( v[0].vals[i].x, 3000 + i, (long long) ( i ) ); MSI( v[0].vals[i].y, 4000 + i, (long long) ( i ) ); }
          MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_array_elem_widen, ArrayElemWiden, "old_array_elem_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].vals_count, 4 ); MS( v[0].vals[0], -1 ); MS( v[0].vals[1], INT16_MIN );
          MS( v[0].vals[2], INT16_MAX ); MS( v[0].vals[3], 0 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_array_elem_widen, ArrayElemWiden, "new_array_elem_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].vals_count, 4 );
          for ( int i = 0; i < 4; ++i ) { MSI( v[0].vals[i], 100000 + i, (long long) ( i ) ); } MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_constant_grow, ConstantGrow, "old_constant_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].vals_count, 4 );
          for ( int i = 0; i < 4; ++i ) { MSI( v[0].vals[i], 7000 + i, (long long) ( i ) ); } MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_constant_grow, ConstantGrow, "new_constant_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].vals_count, 8 );
          for ( int i = 0; i < 8; ++i ) { MSI( v[0].vals[i], 8000 + i, (long long) ( i ) ); } MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_string_grow, StringGrow, "old_string_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MSTR( v[0].text, "abcdefgh" ); MS( v[0].text_length, 8 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_string_grow, StringGrow, "new_string_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MSTR( v[0].text, "0123456789abcdef" ); MS( v[0].text_length, 16 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    const char16_t wsrc[8] = { u'h', u'e', u'l', u'l', u'o', 0xD83D, 0xDE00, u'￿' };
    VROW( vold_wstring_grow, WstringGrow, "old_wstring_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MWCPY( v[0].text, wsrc ); MS( v[0].text_length, 8 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_wstring_grow, WstringGrow, "new_wstring_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); for ( int i = 0; i < 16; ++i ) { MSI( v[0].text[i], (char16_t) ( u'a' + i ), (long long) ( i ) ); }
          MS( v[0].text_length, 16 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_bytes_grow, BytesGrow, "old_bytes_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); for ( int i = 0; i < 8; ++i ) { MSI( v[0].blob[i], (uint8_t) ( 0xF0 + i ), (long long) ( i ) ); }
          MS( v[0].blob_length, 8 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_bytes_grow, BytesGrow, "new_bytes_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); for ( int i = 0; i < 16; ++i ) { MSI( v[0].blob[i], (uint8_t) i, (long long) ( i ) ); }
          MS( v[0].blob_length, 16 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    // THE THREE VALUES A WIDENING GETS WRONG, all three in ONE file as three records
    {
        std::vector<vold_int_widen::IntWiden> v( 3 );
        const int16_t values[3] = { -1, INT16_MIN, INT16_MAX };
        for ( int k = 0; k < 3; ++k )
        {
            vold_int_widen::IntWidenReset( v[(size_t) k] );
            MSI( v[(size_t) k].lead, 0xAAAAAAAAu, (long long) ( (size_t) k ) );
            MSI( v[(size_t) k].v, values[k], (long long) ( (size_t) k ) );
            MSI( v[(size_t) k].trail, 0xBBBBBBBBu, (long long) ( (size_t) k ) );
        }
        if ( !emit( dir, "old_int_widen.bin", v, vold_int_widen::IntWidenFixedMeasure,
                    vold_int_widen::IntWidenFixedSave ) ) { return false; }
    }
    VROW( vnew_int_widen, IntWiden, "new_int_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 100000 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_uint_widen, UintWiden, "old_uint_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 0xFFFFu ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_uint_widen, UintWiden, "new_uint_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 0xDEADBEEFu ); MS( v[0].trail, 0xBBBBBBBBu ); );

    // A SIGNALLING NaN WITH A PAYLOAD: the bits are the value (the FU ruling)
    {
        const uint32_t kSignalling = 0x7F8ABCDEu;
        float f; std::memcpy( &f, &kSignalling, 4 );
        VROW( vold_float_widen, FloatWiden, "old_float_widen.bin",
              MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, f ); MS( v[0].trail, 0xBBBBBBBBu ); );
    }
    VROW( vnew_float_widen, FloatWiden, "new_float_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 1.0e300 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_range_widen, RangeWiden, "old_range_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 100 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_range_widen, RangeWiden, "new_range_widen.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 150 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_bits_grow, BitsGrow, "old_bits_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 0xFFu ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_bits_grow, BitsGrow, "new_bits_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 0xFFFu ); MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_fixed_i_grow, FixedIGrow, "old_fixed_I_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, -1 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_fixed_i_grow, FixedIGrow, "new_fixed_I_grow.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].v, 1000 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    VROW( vold_optional_add, OptionalAdd, "old_optional_add.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].link.value, 777 ); MS( v[0].trail, 0xBBBBBBBBu ); );
    VROW( vnew_optional_add, OptionalAdd, "new_optional_add.bin",
          MS( v[0].lead, 0xAAAAAAAAu ); MS( v[0].link_present, false ); MS( v[0].link.value, 0 ); MS( v[0].trail, 0xBBBBBBBBu ); );

    // THE FLOOR'S LINEAGE: three layouts, oldest first. `old_floor.bin` is the
    // file below a floor of 1; `mid_floor.bin` the file AT it; `new_floor.bin`
    // the reader's own.
    VROW( vold_floor, Floored, "old_floor.bin", MS( v[0].a, 11 ); );
    VROW( vmid_floor, Floored, "mid_floor.bin", MS( v[0].a, 11 ); MS( v[0].b, 22 ); );
    VROW( vnew_floor, Floored, "new_floor.bin", MS( v[0].a, 11 ); MS( v[0].b, 22 ); MS( v[0].c, 33 ); );

    // THE BRANCH CASE: both pre-merge writers, and the merged build's own
    VROW( vold_lineage_merge, Merged, "old_lineage_merge.bin", MS( v[0].anchor, 50 ); );
    VROW( vbra_lineage_merge, Merged, "a_lineage_merge.bin", MS( v[0].anchor, 100 ); MS( v[0].from_a, 111 ); );
    VROW( vbrb_lineage_merge, Merged, "b_lineage_merge.bin", MS( v[0].anchor, 200 ); MS( v[0].from_b, 222 ); );
    VROW( vnew_lineage_merge, Merged, "new_lineage_merge.bin", MS( v[0].anchor, 300 ); MS( v[0].from_a, 1 ); MS( v[0].from_b, 2 ); );
    return true;
}
// ---- rowan/cpp-versioning-numbers: END ------------------------------------
// ==== BEGIN rowan/cpp-versioning-lists: THE LISTS ROWS' LINEAGE PAIRS ======
//
// docs/FIXED-FORM-VERSIONING-TESTS.md, the LIST rows. One pair of files per
// row: `old_<row>.bin` written by the OLD schema's writer and `new_<row>.bin`
// by the NEW one's, the two schemas differing by EXACTLY the row's definition
// change and nothing else. The reference's two read columns (NEW-READS-OLD and
// OLD-REFUSES-NEW) live in test/tables/versioning_lists.cpp; every other leg
// reads these same bytes, so the values below are set by hand and none of them
// is a default.

static bool vlists_files( const char * dir )
{
    // ---- field_append: {x,y,z} -> {x,y,z,w} --------------------------------
    {
        std::vector<vold_field_append::Lineage> o( 1 );
        vold_field_append::LineageReset( o[0] );
        MS( o[0].x, 11 ); MS( o[0].y, 22 ); MS( o[0].z, 33 );
        if ( !emit( dir, "old_field_append.bin", o, vold_field_append::LineageFixedMeasure,
                    vold_field_append::LineageFixedSave ) ) { return false; }

        std::vector<vnew_field_append::Lineage> n( 1 );
        vnew_field_append::LineageReset( n[0] );
        MS( n[0].x, 44 ); MS( n[0].y, 55 ); MS( n[0].z, 66 ); MS( n[0].w, 777 );
        if ( !emit( dir, "new_field_append.bin", n, vnew_field_append::LineageFixedMeasure,
                    vnew_field_append::LineageFixedSave ) ) { return false; }
    }

    // ---- field_deprecate: {a,b,c} -> {a, b deprecated, c} ------------------
    {
        std::vector<vold_field_deprecate::Lineage> o( 1 );
        vold_field_deprecate::LineageReset( o[0] );
        MS( o[0].a, 1 ); MS( o[0].b, 2 ); MS( o[0].c, 3 );
        if ( !emit( dir, "old_field_deprecate.bin", o, vold_field_deprecate::LineageFixedMeasure,
                    vold_field_deprecate::LineageFixedSave ) ) { return false; }

        std::vector<vnew_field_deprecate::Lineage> n( 1 );
        vnew_field_deprecate::LineageReset( n[0] );
        MS( n[0].a, 4 ); MS( n[0].b, 5 ); MS( n[0].c, 6 );
        if ( !emit( dir, "new_field_deprecate.bin", n, vnew_field_deprecate::LineageFixedMeasure,
                    vnew_field_deprecate::LineageFixedSave ) ) { return false; }
    }

    // ---- field_undeprecate: {a, b deprecated, c} -> {a,b,c} ----------------
    {
        std::vector<vold_field_undeprecate::Lineage> o( 1 );
        vold_field_undeprecate::LineageReset( o[0] );
        MS( o[0].a, 1 ); MS( o[0].b, 2 ); MS( o[0].c, 3 );
        if ( !emit( dir, "old_field_undeprecate.bin", o, vold_field_undeprecate::LineageFixedMeasure,
                    vold_field_undeprecate::LineageFixedSave ) ) { return false; }

        std::vector<vnew_field_undeprecate::Lineage> n( 1 );
        vnew_field_undeprecate::LineageReset( n[0] );
        MS( n[0].a, 4 ); MS( n[0].b, 5 ); MS( n[0].c, 6 );
        if ( !emit( dir, "new_field_undeprecate.bin", n, vnew_field_undeprecate::LineageFixedMeasure,
                    vnew_field_undeprecate::LineageFixedSave ) ) { return false; }
    }

    // ---- enum_append: Tier {Bronze,Silver,Gold} -> + Platinum --------------
    //
    // THE NEW FILE LEADS WITH A VALUE THE OLD READER CAN NAME (Gold), because
    // the refusal is a property of the LAYOUT and not of a record's values: an
    // old reader must refuse this file even though its first record holds
    // nothing it could not have held. The second record holds Platinum.
    {
        std::vector<vold_enum_append::Lineage> o( 1 );
        vold_enum_append::LineageReset( o[0] );
        MSE( o[0].tier, vold_enum_append::Tier::Gold ); MS( o[0].seq, 9 );
        if ( !emit( dir, "old_enum_append.bin", o, vold_enum_append::LineageFixedMeasure,
                    vold_enum_append::LineageFixedSave ) ) { return false; }

        std::vector<vnew_enum_append::Lineage> n( 2 );
        vnew_enum_append::LineageReset( n[0] );
        MSE( n[0].tier, vnew_enum_append::Tier::Gold ); MS( n[0].seq, 10 );
        vnew_enum_append::LineageReset( n[1] );
        MSE( n[1].tier, vnew_enum_append::Tier::Platinum ); MS( n[1].seq, 11 );
        if ( !emit( dir, "new_enum_append.bin", n, vnew_enum_append::LineageFixedMeasure,
                    vnew_enum_append::LineageFixedSave ) ) { return false; }
    }

    // ---- enum_width: 255 variants -> 256, the ordinal 1 byte -> 2 ----------
    {
        std::vector<vold_enum_width::Lineage> o( 1 );
        vold_enum_width::LineageReset( o[0] );
        MSE( o[0].tier, vold_enum_width::Wide::V200 ); MS( o[0].seq, 12 );
        if ( !emit( dir, "old_enum_width.bin", o, vold_enum_width::LineageFixedMeasure,
                    vold_enum_width::LineageFixedSave ) ) { return false; }

        std::vector<vnew_enum_width::Lineage> n( 2 );
        vnew_enum_width::LineageReset( n[0] );
        MSE( n[0].tier, vnew_enum_width::Wide::V200 ); MS( n[0].seq, 13 );
        vnew_enum_width::LineageReset( n[1] );
        // the 256th variant: the ordinal the OLD side's byte cannot hold
        MSE( n[1].tier, vnew_enum_width::Wide::V256 ); MS( n[1].seq, 14 );
        if ( !emit( dir, "new_enum_width.bin", n, vnew_enum_width::LineageFixedMeasure,
                    vnew_enum_width::LineageFixedSave ) ) { return false; }
    }

    // ---- union_append: Pick {alpha,beta} -> + gamma ------------------------
    {
        std::vector<vold_union_append::Lineage> o( 1 );
        vold_union_append::LineageReset( o[0] );
        MSE( o[0].pick.type, vold_union_append::PickType::Alpha );
        MS( o[0].pick.alpha.m, 7 );
        MS( o[0].seq, 15 );
        if ( !emit( dir, "old_union_append.bin", o, vold_union_append::LineageFixedMeasure,
                    vold_union_append::LineageFixedSave ) ) { return false; }

        std::vector<vnew_union_append::Lineage> n( 2 );
        vnew_union_append::LineageReset( n[0] );
        MSE( n[0].pick.type, vnew_union_append::PickType::Alpha );
        MS( n[0].pick.alpha.m, 8 );
        MS( n[0].seq, 16 );
        vnew_union_append::LineageReset( n[1] );
        MSE( n[1].pick.type, vnew_union_append::PickType::Gamma );
        MS( n[1].pick.gamma.p, 9 );
        MS( n[1].seq, 17 );
        if ( !emit( dir, "new_union_append.bin", n, vnew_union_append::LineageFixedMeasure,
                    vnew_union_append::LineageFixedSave ) ) { return false; }
    }

    // ---- union_arm_payload_widen: arm alpha {x} -> {x,y} -------------------
    {
        std::vector<vold_union_arm_payload_widen::Lineage> o( 1 );
        vold_union_arm_payload_widen::LineageReset( o[0] );
        MSE( o[0].pick.type, vold_union_arm_payload_widen::PickType::Alpha );
        MS( o[0].pick.alpha.x, 21 );
        MS( o[0].seq, 18 );
        if ( !emit( dir, "old_union_arm_payload_widen.bin", o, vold_union_arm_payload_widen::LineageFixedMeasure,
                    vold_union_arm_payload_widen::LineageFixedSave ) ) { return false; }

        std::vector<vnew_union_arm_payload_widen::Lineage> n( 1 );
        vnew_union_arm_payload_widen::LineageReset( n[0] );
        MSE( n[0].pick.type, vnew_union_arm_payload_widen::PickType::Alpha );
        MS( n[0].pick.alpha.x, 22 );
        MS( n[0].pick.alpha.y, 23 );
        MS( n[0].seq, 19 );
        if ( !emit( dir, "new_union_arm_payload_widen.bin", n, vnew_union_arm_payload_widen::LineageFixedMeasure,
                    vnew_union_arm_payload_widen::LineageFixedSave ) ) { return false; }
    }

    // ---- flags_append: Caps {Jump,Crouch} -> + Fly -------------------------
    {
        std::vector<vold_flags_append::Lineage> o( 1 );
        vold_flags_append::LineageReset( o[0] );
        MS( o[0].caps, vold_flags_append::Caps_Jump | vold_flags_append::Caps_Crouch );
        MS( o[0].seq, 20 );
        if ( !emit( dir, "old_flags_append.bin", o, vold_flags_append::LineageFixedMeasure,
                    vold_flags_append::LineageFixedSave ) ) { return false; }

        std::vector<vnew_flags_append::Lineage> n( 1 );
        vnew_flags_append::LineageReset( n[0] );
        MS( n[0].caps, vnew_flags_append::Caps_Jump | vnew_flags_append::Caps_Fly );
        MS( n[0].seq, 21 );
        if ( !emit( dir, "new_flags_append.bin", n, vnew_flags_append::LineageFixedMeasure,
                    vnew_flags_append::LineageFixedSave ) ) { return false; }
    }

    // ---- keyed_array_enum_append: [Tier]int32, Tier gains Platinum --------
    {
        std::vector<vold_keyed_array_enum_append::Lineage> o( 1 );
        vold_keyed_array_enum_append::LineageReset( o[0] );
        MSI( o[0].slots[vold_keyed_array_enum_append::Tier::Bronze].n, 101, (long long) ( vold_keyed_array_enum_append::Tier::Bronze ) );
        MSI( o[0].slots[vold_keyed_array_enum_append::Tier::Silver].n, 102, (long long) ( vold_keyed_array_enum_append::Tier::Silver ) );
        MSI( o[0].slots[vold_keyed_array_enum_append::Tier::Gold].n, 103, (long long) ( vold_keyed_array_enum_append::Tier::Gold ) );
        MS( o[0].seq, 22 );
        if ( !emit( dir, "old_keyed_array_enum_append.bin", o, vold_keyed_array_enum_append::LineageFixedMeasure,
                    vold_keyed_array_enum_append::LineageFixedSave ) ) { return false; }

        std::vector<vnew_keyed_array_enum_append::Lineage> n( 1 );
        vnew_keyed_array_enum_append::LineageReset( n[0] );
        MSI( n[0].slots[vnew_keyed_array_enum_append::Tier::Bronze].n, 201, (long long) ( vnew_keyed_array_enum_append::Tier::Bronze ) );
        MSI( n[0].slots[vnew_keyed_array_enum_append::Tier::Silver].n, 202, (long long) ( vnew_keyed_array_enum_append::Tier::Silver ) );
        MSI( n[0].slots[vnew_keyed_array_enum_append::Tier::Gold].n, 203, (long long) ( vnew_keyed_array_enum_append::Tier::Gold ) );
        MSI( n[0].slots[vnew_keyed_array_enum_append::Tier::Platinum].n, 204, (long long) ( vnew_keyed_array_enum_append::Tier::Platinum ) );
        MS( n[0].seq, 23 );
        if ( !emit( dir, "new_keyed_array_enum_append.bin", n, vnew_keyed_array_enum_append::LineageFixedMeasure,
                    vnew_keyed_array_enum_append::LineageFixedSave ) ) { return false; }
    }

    // ---- nested_append: Vec {x,y,z} -> {x,y,z,w}, inside Lineage -----------
    {
        std::vector<vold_nested_append::Lineage> o( 1 );
        vold_nested_append::LineageReset( o[0] );
        MS( o[0].v.x, 1 ); MS( o[0].v.y, 2 ); MS( o[0].v.z, 3 ); MS( o[0].seq, 24 );
        if ( !emit( dir, "old_nested_append.bin", o, vold_nested_append::LineageFixedMeasure,
                    vold_nested_append::LineageFixedSave ) ) { return false; }

        std::vector<vnew_nested_append::Lineage> n( 1 );
        vnew_nested_append::LineageReset( n[0] );
        MS( n[0].v.x, 4 ); MS( n[0].v.y, 5 ); MS( n[0].v.z, 6 ); MS( n[0].v.w, 7 ); MS( n[0].seq, 25 );
        if ( !emit( dir, "new_nested_append.bin", n, vnew_nested_append::LineageFixedMeasure,
                    vnew_nested_append::LineageFixedSave ) ) { return false; }
    }

    // ---- rename_without_was: `a` -> `b was = "a"` -------------------------
    {
        std::vector<vold_rename_without_was::Lineage> o( 1 );
        vold_rename_without_was::LineageReset( o[0] );
        MS( o[0].a, 31 ); MS( o[0].seq, 26 );
        if ( !emit( dir, "old_rename_without_was.bin", o, vold_rename_without_was::LineageFixedMeasure,
                    vold_rename_without_was::LineageFixedSave ) ) { return false; }

        std::vector<vnew_rename_without_was::Lineage> n( 1 );
        vnew_rename_without_was::LineageReset( n[0] );
        MS( n[0].b, 32 ); MS( n[0].seq, 27 );
        if ( !emit( dir, "new_rename_without_was.bin", n, vnew_rename_without_was::LineageFixedMeasure,
                    vnew_rename_without_was::LineageFixedSave ) ) { return false; }
    }

    return true;
}

// ==== END rowan/cpp-versioning-lists =======================================

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { std::fprintf( stderr, "usage: %s <outdir>\n", argv[0] ); return 1; }
    const char * dir = argv[1];
    if ( !fx1_file( dir ) || !fx2_file( dir ) || !p1_file( dir ) || !p3_file( dir ) ||
         !keyed_file( dir ) || !pack_file( dir ) || !fxw_file( dir ) ||
         !fu1_file( dir ) || !fu2_file( dir ) ) { return 1; }
    // ---- rowan/cpp-versioning-numbers: BEGIN ----
    if ( !versioning_numbers_files( dir ) ) { return 1; }
    // ---- rowan/cpp-versioning-numbers: END ----
    // ==== BEGIN rowan/cpp-versioning-lists ====
    if ( !vlists_files( dir ) ) { return 1; }
    // ==== END rowan/cpp-versioning-lists =====
    // ---- rowan/corpus-manifest ----
    // THE MANIFEST, beside the bytes it describes (§5.7 step 1, ruling #32)
    if ( !man_write( dir ) ) { return 1; }
    return 0;
}
