// The WIDE TEXT gate on the ID-TABLE WIRE: kind 33 (docs/SPEC-TABLES.md §3),
// the table half of `wstring(N)` whose packet half main.cpp replays from the
// serialize corpus.
//
// The two halves are gated apart because they are two wires. The packet half
// is a bit stream with a checked-in vector file and no recovery, so its gate
// replays bytes a person wrote. This half is a LENGTH-FRAMED body whose whole
// answer is a verdict plus five counters (§4), so its gate states the payload
// and the verdict and lets the framing be built: the framing is §3's and every
// other table gate already pins it, while the payload and the counters are
// kind 33's own and are what this program is for.
//
// The vectors are NAMED FROM THE SERIALIZE CORPUS with `-table` on them
// (testdata/conformance/text/wstring.txt), so the two wires' rows line up by
// name and the ones that do NOT carry over are visible by their absence: the
// corpus's group-above-0xFFFF refusals have no case here, because two bytes
// cannot spell one (§3), and its length-field refusals have none either,
// because this wire frames by a byte length and not by a ranged integer.
//
// What DOES carry over is the content rule, and it is the whole point of the
// file: an unpaired surrogate and a zero code unit are refused on both wires,
// and only the SHAPE of the refusal differs — terminal there, one `malformed`
// and the declared default here (SPEC.md §4.12, §3, §4).

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>

#include "CaptionTable.h"

using namespace wide;

static int failures = 0;

#define check( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s (%s:%d)\n", #condition, __FILE__, __LINE__ ); \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

#define check_vector( condition, name )                                       \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s on vector %s (%s:%d)\n",                      \
                    #condition, ( name ), __FILE__, __LINE__ );               \
            failures++;                                                       \
        }                                                                     \
    } while ( 0 )

// ---------------------------------------------------------------------------
// the framing. §3: a body is `id reference, kind, payload` lines and a zero
// reference, and the id table is the last thing in the file, found from the
// END: the final eight bytes are the entry count and the `8 x count` bytes
// before them are the entries, each a little-endian u64.

typedef std::vector<uint8_t> Bytes;

static void put_leb( Bytes & out, uint64_t v )
{
    for ( ;; )
    {
        uint8_t byte = (uint8_t) ( v & 0x7f );
        v >>= 7;
        if ( v != 0 ) { byte |= 0x80; }
        out.push_back( byte );
        if ( v == 0 ) { return; }
    }
}

static void put_u64( Bytes & out, uint64_t v )
{
    for ( int i = 0; i < 8; i++ ) { out.push_back( (uint8_t) ( v >> ( 8 * i ) ) ); }
}

// One field line under one id, its payload verbatim, in a whole file: the form
// byte, the line, the body terminator and a one-entry id table.
static Bytes frame( uint64_t id, uint8_t kind, const Bytes & payload )
{
    Bytes out;
    out.push_back( 1 ); // THE FORM BYTE (§3)
    put_leb( out, 1 );  // reference 1: the first and only id
    out.push_back( kind );
    out.insert( out.end(), payload.begin(), payload.end() );
    put_leb( out, 0 ); // the body terminator
    put_u64( out, id );
    put_u64( out, 1 );
    return out;
}

// A kind 33 FIELD payload: `L`, a BYTE length, then the units two bytes each
// little-endian (§3). `bytes` says what L claims, which is how an ODD L is
// spelled without spelling half a unit.
static Bytes wide_payload( const uint16_t * units, int count, int64_t declared_bytes )
{
    Bytes out;
    put_leb( out, (uint64_t) declared_bytes );
    for ( int i = 0; i < count; i++ )
    {
        out.push_back( (uint8_t) units[i] );
        out.push_back( (uint8_t) ( units[i] >> 8 ) );
    }
    return out;
}

static Bytes wide_field( const uint16_t * units, int count )
{
    return wide_payload( units, count, (int64_t) count * 2 );
}

// the wire ids the vectors name, each `fnv1a64( name )` (§5)
static const uint64_t id_title = 0xda31296c0c1b6029ull; // Caption.title, wstring(7)
static const uint64_t id_line  = 0xbf4ba5ad694f5907ull; // Caption.line, the type Line
static const uint64_t id_lines = 0x5ce3f9a9f1d5001cull; // Caption.lines, [..3]Line
static const uint64_t id_body  = 0xcd4de79bc6c93295ull; // Caption.body, the union Body
static const uint64_t id_text  = 0xfa04f4ef1995407eull; // Line.text, wstring(4)
static const uint64_t id_wide  = 0xa633f1f655715ccaull; // Body.wide, the wstring arm

// ---------------------------------------------------------------------------
// the vectors. Each states the units it puts on the wire and the verdict the
// page gives them, and nothing else: a row is one sentence of §3 or §4.

struct Vector
{
    const char * name;
    const uint16_t * units;
    int count;
    // the verdict: the units the storage must hold afterwards, and the two
    // counters this kind can move
    const uint16_t * expect;
    int expect_count;
    int malformed;
    int clamped;
};

static const uint16_t units_empty[1]      = { 0 };
static const uint16_t units_one[1]        = { 0x0041 };
static const uint16_t units_worked[3]     = { 0x043C, 0x0438, 0x0440 };
static const uint16_t units_ffff[1]       = { 0xFFFF };
static const uint16_t units_below[1]      = { 0xD7FF };
static const uint16_t units_above[1]      = { 0xE000 };
static const uint16_t units_pair[2]       = { 0xD83D, 0xDCA9 };
static const uint16_t units_pair_in[4]    = { 0x0041, 0xD83D, 0xDCA9, 0x0042 };
static const uint16_t units_high_alone[1] = { 0xD83D };
static const uint16_t units_high_first[2] = { 0xD83D, 0x0041 };
static const uint16_t units_high_last[2]  = { 0x0041, 0xD83D };
static const uint16_t units_low_alone[1]  = { 0xDCA9 };
static const uint16_t units_low_first[2]  = { 0xDCA9, 0x0041 };
static const uint16_t units_reversed[2]   = { 0xDCA9, 0xD83D };
static const uint16_t units_nul_only[1]   = { 0x0000 };
static const uint16_t units_nul_first[2]  = { 0x0000, 0x0041 };
static const uint16_t units_nul_last[2]   = { 0x0041, 0x0000 };
static const uint16_t units_seven[7]      = { 0x0041, 0x0042, 0x0043, 0x0044, 0x0045, 0x0046, 0x0047 };
static const uint16_t units_eight[8]      = { 0x0041, 0x0042, 0x0043, 0x0044, 0x0045, 0x0046, 0x0047, 0x0048 };

// THE CLAMP ROWS. The bound is 7 on `title`, so eight units keep seven — and
// where the seventh and eighth are a PAIR, the seventh is dropped with it, so
// the clamp lands six (§3, §16.2). The pair astride the cut is the row that
// separates "keep the first N units" from "never split a pair".
static const uint16_t units_cut_pair[8]   = { 0x0041, 0x0042, 0x0043, 0x0044, 0x0045, 0x0046, 0xD83D, 0xDCA9 };
static const uint16_t units_cut_six[6]    = { 0x0041, 0x0042, 0x0043, 0x0044, 0x0045, 0x0046 };
// and where the pair sits WHOLE inside the bound, nothing is dropped for it:
// the eighth unit alone goes, and the pair at units five and six stays.
static const uint16_t units_pair_inside[8] = { 0x0041, 0x0042, 0x0043, 0x0044, 0xD83D, 0xDCA9, 0x0047, 0x0048 };
static const uint16_t units_pair_kept[7]   = { 0x0041, 0x0042, 0x0043, 0x0044, 0xD83D, 0xDCA9, 0x0047 };

static const Vector vectors[] = {
    // the accepting rows, named from the corpus
    { "wstring-table-empty",                                units_empty,      0, units_empty,    0, 0, 0 },
    { "wstring-table-single-basic-plane-character",         units_one,        1, units_one,      1, 0, 0 },
    { "wstring-table-worked-example",                       units_worked,     3, units_worked,   3, 0, 0 },
    { "wstring-table-accept-group-ffff",                    units_ffff,       1, units_ffff,     1, 0, 0 },
    { "wstring-table-accept-just-below-the-surrogate-block", units_below,     1, units_below,    1, 0, 0 },
    { "wstring-table-accept-just-above-the-surrogate-block", units_above,     1, units_above,    1, 0, 0 },
    { "wstring-table-accept-surrogate-pair",                units_pair,       2, units_pair,     2, 0, 0 },
    { "wstring-table-accept-pair-between-two-basic-plane",  units_pair_in,    4, units_pair_in,  4, 0, 0 },
    { "wstring-table-accept-length-at-the-bound",           units_seven,      7, units_seven,    7, 0, 0 },

    // THE CONTENT RULE (§3, §4), every shape the corpus refuses on the packet
    // wire: here each is framing-class damage, so the field reads its declared
    // default — empty, wide text taking no specified default — ONE malformed
    // counts, and the parent reads on past L.
    { "wstring-table-refuse-first-high-surrogate-alone",    units_high_alone, 1, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-high-surrogate-followed-by-a-non-surrogate", units_high_first, 2, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-high-surrogate-as-the-final-group", units_high_last, 2, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-first-low-surrogate-alone",     units_low_alone,  1, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-low-surrogate-not-preceded-by-a-high-one", units_low_first, 2, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-reversed-pair",                 units_reversed,   2, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-nul-as-the-only-group",         units_nul_only,   1, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-nul-as-the-first-of-two-groups", units_nul_first, 2, units_empty, 0, 1, 0 },
    { "wstring-table-refuse-nul-as-the-final-group",        units_nul_last,   2, units_empty, 0, 1, 0 },

    // THE CLAMP (§3), which is not damage: the length was read and the
    // position after it is known.
    { "wstring-table-clamp-one-past-the-bound",             units_eight,      8, units_seven,    7, 0, 1 },
    { "wstring-table-clamp-never-splits-a-pair",            units_cut_pair,   8, units_cut_six,  6, 0, 1 },
    { "wstring-table-clamp-keeps-a-pair-inside-the-bound",  units_pair_inside, 8, units_pair_kept, 7, 0, 1 },
};

static bool units_equal( const char16_t * got, int32_t got_count, const uint16_t * want, int want_count )
{
    if ( (int) got_count != want_count ) { return false; }
    for ( int i = 0; i < want_count; i++ )
    {
        if ( (uint16_t) got[i] != want[i] ) { return false; }
    }
    return true;
}

// POISON the destination before the read, exactly as the packet gate does:
// generated storage default-initializes to zeros, so a terminator check
// against a fresh object passes whether or not the reader wrote anything at
// index length (SPEC.md §4.12).
static void poison( char16_t * buffer, int n )
{
    for ( int i = 0; i < n; i++ ) { buffer[i] = char16_t( 0x7F ); }
}

// ---------------------------------------------------------------------------
// A FIELD of a table (§3), which is the site the vectors are stated at.

static int field_vectors()
{
    for ( const Vector & v : vectors )
    {
        Caption out;
        poison( out.title, 8 );
        out.title_length = 99;
        TableReport report;
        const Bytes wire = frame( id_title, 33, wide_field( v.units, v.count ) );
        const bool ok = CaptionLoad( out, wire.data(), (int64_t) wire.size(), &report );
        check_vector( ok, v.name );
        check_vector( !report.refused, v.name );
        check_vector( report.malformed == ( v.malformed != 0 ), v.name );
        check_vector( report.clamped == v.clamped, v.name );
        check_vector( report.kind_mismatch == 0 && report.unknown == 0 && report.widened == 0, v.name );
        check_vector( units_equal( out.title, out.title_length, v.expect, v.expect_count ), v.name );
        // THE TERMINATING ZERO UNIT at index length, in every case, damage
        // included: it is the read's work and nothing else's, which is what
        // the poison above makes provable (SPEC.md §4.12, §7.2)
        check_vector( out.title[out.title_length] == 0, v.name );
    }
    return failures;
}

// ---------------------------------------------------------------------------
// AN ODD `L` IS FRAMING DAMAGE (§3): the value is `L / 2` code units, so an
// odd `L` is not a wstring at any length. It has no packet-wire twin — that
// wire frames by a count of groups — so it is stated here alone.

static void odd_length()
{
    static const uint16_t one[1] = { 0x0041 };
    Caption out;
    out.title_length = 99;
    TableReport report;
    Bytes payload = wide_payload( one, 1, 3 ); // L = 3 over two payload bytes
    payload.push_back( 0x00 );                 // the third byte L claims
    const Bytes wire = frame( id_title, 33, payload );
    check( CaptionLoad( out, wire.data(), (int64_t) wire.size(), &report ) );
    check( report.malformed );
    check( report.clamped == 0 );
    check( out.title_length == 0 ); // the declared default
    check( out.title[0] == 0 );
}

// ---------------------------------------------------------------------------
// THE DEFAULT AND THE COUNT (§3). Wide text takes no specified default
// (SPEC.md §4.12), so the empty value is what elides: a save of an empty
// wstring writes NO field line, and a load of a body with no line leaves the
// storage empty. And the used length is in CODE UNITS, which is what the
// count rule means for this kind: a save's `L` is TWICE it.

static void default_and_count()
{
    Caption value;
    uint8_t buffer[256];

    // an empty wstring ELIDES: the whole save is the form byte, the terminator
    // and an empty id table
    const int64_t empty_size = CaptionSave( value, buffer, (int64_t) sizeof( buffer ) );
    check( empty_size > 0 );

    static const uint16_t units[3] = { 0x043C, 0x0438, 0x0440 };
    for ( int i = 0; i < 3; i++ ) { value.title[i] = (char16_t) units[i]; }
    value.title[3] = 0;
    value.title_length = 3;
    const int64_t size = CaptionSave( value, buffer, (int64_t) sizeof( buffer ) );
    check( size > 0 );
    // the id table grew by one entry (8) and its count word was already there,
    // so the field line is what is left: the reference, the kind byte, `L` and
    // SIX payload bytes for three code units
    check( size - empty_size == 8 + 1 + 1 + 1 + 6 );
    // `L` IS A BYTE LENGTH (§3), twice the code unit count, and it sits after
    // the form byte, the reference and the kind byte
    check( buffer[0] == 1 && buffer[1] == 1 && buffer[2] == 33 && buffer[3] == 6 );
    check( buffer[4] == 0x3C && buffer[5] == 0x04 ); // little-endian, low byte first

    // and it round-trips
    Caption out;
    TableReport report;
    check( CaptionLoad( out, buffer, size, &report ) );
    check( report.malformed == false && report.clamped == 0 );
    check( units_equal( out.title, out.title_length, units, 3 ) );
}

// ---------------------------------------------------------------------------
// EVERY OTHER KIND 33 SITE takes the same content rule, because §3 states it
// once for the kind and not once per site. One refusal row and one accepting
// row at each: a `type` a table reaches, a union ARM, and an ELEMENT.

static void nested_type_site()
{
    static const uint16_t good[2] = { 0x0041, 0x0042 };
    static const uint16_t bad[1]  = { 0xD83D };

    // Line rides as kind 13, its body holding the wstring field's own line.
    // Reference 1 is Caption.line and reference 2 is Line.text, so the nested
    // body's own line names the SECOND entry: the id table is the whole wire's
    // in FIRST-USE order (§3), not one table per body.
    Bytes inner;
    put_leb( inner, 2 );
    inner.push_back( 33 );
    Bytes good_units = wide_field( good, 2 );
    inner.insert( inner.end(), good_units.begin(), good_units.end() );
    put_leb( inner, 0 ); // the nested body's terminator

    Bytes wire;
    wire.push_back( 1 );
    put_leb( wire, 1 );
    wire.push_back( 13 );
    put_leb( wire, (uint64_t) inner.size() );
    wire.insert( wire.end(), inner.begin(), inner.end() );
    put_leb( wire, 0 );
    put_u64( wire, id_line );
    put_u64( wire, id_text );
    put_u64( wire, 2 );

    Caption out;
    TableReport report;
    check( CaptionLoad( out, wire.data(), (int64_t) wire.size(), &report ) );
    check( !report.malformed );
    check( units_equal( out.line.text, out.line.text_length, good, 2 ) );

    // the same nesting with an unpaired surrogate: the FIELD reads its
    // declared default, one malformed counts, and the parent reads on past L
    Caption bad_out;
    TableReport bad_report;
    Bytes bad_inner;
    put_leb( bad_inner, 2 );
    bad_inner.push_back( 33 );
    Bytes bad_payload_units = wide_field( bad, 1 );
    bad_inner.insert( bad_inner.end(), bad_payload_units.begin(), bad_payload_units.end() );
    put_leb( bad_inner, 0 );
    Bytes bad_wire;
    bad_wire.push_back( 1 );
    put_leb( bad_wire, 1 );
    bad_wire.push_back( 13 );
    put_leb( bad_wire, (uint64_t) bad_inner.size() );
    bad_wire.insert( bad_wire.end(), bad_inner.begin(), bad_inner.end() );
    put_leb( bad_wire, 0 );
    put_u64( bad_wire, id_line );
    put_u64( bad_wire, id_text );
    put_u64( bad_wire, 2 );
    check( CaptionLoad( bad_out, bad_wire.data(), (int64_t) bad_wire.size(), &bad_report ) );
    check( bad_report.malformed );
    check( bad_out.line.text_length == 0 );
    check( bad_out.line.text[0] == 0 );
}

static void arm_site()
{
    static const uint16_t good[2] = { 0x0041, 0x0042 };
    static const uint16_t bad[2]  = { 0x0041, 0xD83D };

    // a union field is `arm id reference, kind, L, payload` (§3), and an arm's
    // `L` is the wstring's BYTE length with no length of its own
    for ( int bad_row = 0; bad_row < 2; bad_row++ )
    {
        const uint16_t * units = bad_row ? bad : good;
        Bytes wire;
        wire.push_back( 1 );
        put_leb( wire, 1 ); // Caption.body
        wire.push_back( 15 );
        put_leb( wire, 2 ); // the arm reference: Body.wide
        wire.push_back( 33 );
        put_leb( wire, 4 ); // L: two code units
        for ( int i = 0; i < 2; i++ )
        {
            wire.push_back( (uint8_t) units[i] );
            wire.push_back( (uint8_t) ( units[i] >> 8 ) );
        }
        put_leb( wire, 0 );
        put_u64( wire, id_body );
        put_u64( wire, id_wide );
        put_u64( wire, 2 );

        Caption out;
        TableReport report;
        check( CaptionLoad( out, wire.data(), (int64_t) wire.size(), &report ) );
        if ( bad_row )
        {
            // ILL-FORMED TEXT AT AN ARM: the union reads None (§3, §4)
            check( report.malformed );
            check( out.body.type == BodyType::None );
        }
        else
        {
            check( !report.malformed );
            check( out.body.type == BodyType::Wide );
            check( units_equal( out.body.wide.value, out.body.wide.value_length, good, 2 ) );
        }
    }
}

static void element_site()
{
    static const uint16_t good[2] = { 0x0041, 0x0042 };
    static const uint16_t bad[1]  = { 0xDCA9 };

    // `[..3]Line` is kind 14 over element kind 13: the element kind byte, the
    // count, then each element preceded by its own L (§3)
    for ( int bad_row = 0; bad_row < 2; bad_row++ )
    {
        Bytes element;
        put_leb( element, 2 ); // Line.text is reference 2
        element.push_back( 33 );
        Bytes units = bad_row ? wide_field( bad, 1 ) : wide_field( good, 2 );
        element.insert( element.end(), units.begin(), units.end() );
        put_leb( element, 0 ); // the element body's terminator

        Bytes body;
        body.push_back( 13 ); // the element kind
        put_leb( body, 1 );   // one element
        put_leb( body, (uint64_t) element.size() );
        body.insert( body.end(), element.begin(), element.end() );

        Bytes wire;
        wire.push_back( 1 );
        put_leb( wire, 1 ); // Caption.lines
        wire.push_back( 14 );
        put_leb( wire, (uint64_t) body.size() );
        wire.insert( wire.end(), body.begin(), body.end() );
        put_leb( wire, 0 );
        put_u64( wire, id_lines );
        put_u64( wire, id_text );
        put_u64( wire, 2 );

        Caption out;
        TableReport report;
        check( CaptionLoad( out, wire.data(), (int64_t) wire.size(), &report ) );
        check( out.lines_count == 1 );
        if ( bad_row )
        {
            check( report.malformed );
            check( out.lines[0].text_length == 0 );
        }
        else
        {
            check( !report.malformed );
            check( units_equal( out.lines[0].text, out.lines[0].text_length, good, 2 ) );
        }
    }
}

// ---------------------------------------------------------------------------
// THE TEXT FORM's wide rows (§16.2, §16.5): the same text transcoded at the
// boundary, a clamp at N code units that never splits a pair, storage BUILT IN
// CODE holding an unpaired surrogate written as U+FFFD, and one holding a zero
// unit written as  . The two storage rows are built in code deliberately:
// no wire can deliver either.

static void text_form()
{
    struct Row
    {
        const char * name;
        const char * text;      // the JSON value, quoted
        const uint16_t * units; // what the read must place
        int count;
        int clamped;
        const char * written;   // what ToJson must write back, quoted
    };
    static const uint16_t t_empty[1]  = { 0 };
    static const uint16_t t_basic[1]  = { 0x0041 };
    static const uint16_t t_astral[2] = { 0xD83D, 0xDCA9 };
    static const uint16_t t_seven[7]  = { 0x0041, 0x0042, 0x0043, 0x0044, 0x0045, 0x0046, 0x0047 };
    // one past the bound clamps, and a clamp whose cut falls between a
    // surrogate pair drops the high half with it: six units, not seven
    static const uint16_t t_cut[6]    = { 0x0041, 0x0042, 0x0043, 0x0044, 0x0045, 0x0046 };

    static const Row rows[] = {
        { "wide-text-empty",       "\"\"",              t_empty,  0, 0, "\"\"" },
        { "wide-text-basic-plane", "\"A\"",             t_basic,  1, 0, "\"A\"" },
        { "wide-text-astral-pair", "\"\xf0\x9f\x92\xa9\"", t_astral, 2, 0, "\"\xf0\x9f\x92\xa9\"" },
        { "wide-text-at-the-bound", "\"ABCDEFG\"",      t_seven,  7, 0, "\"ABCDEFG\"" },
        { "wide-text-one-past-the-bound", "\"ABCDEFGH\"", t_seven, 7, 1, "\"ABCDEFG\"" },
        { "wide-text-clamp-between-a-pair", "\"ABCDEF\xf0\x9f\x92\xa9\"", t_cut, 6, 1, "\"ABCDEF\"" },
    };

    for ( const Row & row : rows )
    {
        char text[256];
        const int n = snprintf( text, sizeof( text ), "{ \"title\": %s }", row.text );
        Caption value;
        TableReport report;
        check_vector( CaptionFromJson( value, text, n, &report ), row.name );
        check_vector( !report.malformed, row.name );
        check_vector( report.clamped == row.clamped, row.name );
        check_vector( units_equal( value.title, value.title_length, row.units, row.count ), row.name );

        char out[512];
        const int64_t written = CaptionToJson( value, out, (int64_t) sizeof( out ) );
        check_vector( written > 0, row.name );
        out[written] = 0;
        check_vector( strstr( out, row.written ) != NULL, row.name );
    }

    // §16.3's two rows, BUILT IN CODE: an unpaired surrogate is not a code
    // point at all and writes one U+FFFD, and a zero unit is U+0000, which
    // JSON has an escape for, and writes  
    {
        Caption value;
        value.title[0] = (char16_t) 0x0041;
        value.title[1] = (char16_t) 0xD83D; // a high surrogate with no low half
        value.title[2] = 0;
        value.title_length = 2;
        char out[512];
        const int64_t written = CaptionToJson( value, out, (int64_t) sizeof( out ) );
        check( written > 0 );
        out[written] = 0;
        check( strstr( out, "\"A\xef\xbf\xbd\"" ) != NULL );
    }
    {
        Caption value;
        value.title[0] = (char16_t) 0x0041;
        value.title[1] = (char16_t) 0x0000; // a zero unit
        value.title[2] = (char16_t) 0x0042;
        value.title[3] = 0;
        value.title_length = 3;
        char out[512];
        const int64_t written = CaptionToJson( value, out, (int64_t) sizeof( out ) );
        check( written > 0 );
        out[written] = 0;
        check( strstr( out, "\"A\\u0000B\"" ) != NULL );
    }
}

// ---------------------------------------------------------------------------
// THE COOK (§7.2): `char16_t[N + 1]` then an `int32` used length in CODE
// UNITS. A FIXED table's cook is one region of one node, so `Stamp` is where
// the layout is asserted rather than derived.

static void cook_layout()
{
    Stamp value;
    static const uint16_t units[2] = { 0x0041, 0xFFFF };
    for ( int i = 0; i < 2; i++ ) { value.label[i] = (char16_t) units[i]; }
    value.label[2] = 0;
    value.label_length = 2;
    value.seq = 7;

    const int64_t size = StampCookMeasure( value );
    check( size > 0 );
    std::vector<uint8_t> little( (size_t) size, 0xAA );
    std::vector<uint8_t> big( (size_t) size, 0xAA );
    check( StampCook( value, little.data(), (uint64_t) size, TableByteOrder::Little ) );
    check( StampCook( value, big.data(), (uint64_t) size, TableByteOrder::Big ) );
    // the two differ, because a code unit is a two-byte SCALAR and a swap has
    // to know where every one of them begins (§7.2)
    check( memcmp( little.data(), big.data(), (size_t) size ) != 0 );

    const Stamp * mapped = StampOpen( little.data(), (uint64_t) size );
    check( mapped != NULL );
    if ( mapped != NULL )
    {
        check( mapped->label_length == 2 );
        check( (uint16_t) mapped->label[0] == 0x0041 );
        check( (uint16_t) mapped->label[1] == 0xFFFF );
        check( mapped->label[2] == 0 ); // the terminating zero unit
        check( mapped->seq == 7 );
    }
}

// ---------------------------------------------------------------------------
// THE PINNED INSTANCES (docs/SPEC-TABLES.md §3). C++ is the reference writer:
// these instances' encodings are pinned into testdata/wire/tables/<name>.bin
// and named by the conformance manifest, so the wire fuzzer's seeds carry kind
// 33 at every site it plants a mutation at (§4.2). A break here under an
// unchanged schema is stop-the-line, never a quiet re-pin —
// SCHEMA_UPDATE_WIRE_GOLDENS=1 rewrites them deliberately (make
// update-goldens).

static void pin( const char * name, const uint8_t * data, int64_t bytes )
{
    char path[256];
    snprintf( path, sizeof( path ), "testdata/wire/tables/%s.bin", name );
    if ( getenv( "SCHEMA_UPDATE_WIRE_GOLDENS" ) != NULL )
    {
        FILE * f = fopen( path, "wb" );
        if ( f == NULL ) { printf( "FAILED: cannot write %s\n", path ); failures++; return; }
        fwrite( data, 1, (size_t) bytes, f );
        fclose( f );
        return;
    }
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
    {
        printf( "FAILED: missing table wire golden %s (run: make update-goldens)\n", path );
        failures++;
        return;
    }
    static uint8_t pinned[1u << 16];
    const size_t n = fread( pinned, 1, sizeof( pinned ), f );
    fclose( f );
    if ( (int64_t) n != bytes || memcmp( pinned, data, n ) != 0 )
    {
        printf( "FAILED: table wire golden %s: %lld bytes written, %lld pinned\n",
                name, (long long) bytes, (long long) n );
        failures++;
    }
}

static void save_and_pin_caption( const char * name, const Caption & value )
{
    static uint8_t buffer[1u << 16];
    const int64_t size = CaptionSave( value, buffer, (int64_t) sizeof( buffer ) );
    check_vector( size > 0, name );
    if ( size <= 0 ) { return; }
    pin( name, buffer, size );
    // and the round trip, so a pin is a wire this reader reads back exactly
    Caption out;
    TableReport report;
    check_vector( CaptionLoad( out, buffer, size, &report ), name );
    check_vector( !report.malformed && report.clamped == 0, name );
}

static void set_units( char16_t * out, int32_t & length, const uint16_t * units, int count )
{
    for ( int i = 0; i < count; i++ ) { out[i] = (char16_t) units[i]; }
    out[count] = 0;
    length = (int32_t) count;
}

static void pinned_instances()
{
    static const uint16_t basic[3]  = { 0x043C, 0x0438, 0x0440 };
    static const uint16_t astral[4] = { 0x0041, 0xD83D, 0xDCA9, 0x0042 };
    static const uint16_t two[2]    = { 0x0041, 0x0042 };

    {
        // wide_empty: every wstring at its declared default, which is empty —
        // wide text takes no specified default (SPEC.md §4.12) — so no kind 33
        // line rides at all and the file is the elision itself
        Caption value;
        save_and_pin_caption( "wide_empty", value );
    }
    {
        // wide_title: one kind 33 field line and nothing else
        Caption value;
        set_units( value.title, value.title_length, basic, 3 );
        save_and_pin_caption( "wide_title", value );
    }
    {
        // wide_sites: kind 33 at EVERY site the wire has at once — a table's
        // own field, a type a table reaches, an element, and a union arm — so
        // one seed carries all four for the fuzzer to plant at (§4.2)
        Caption value;
        set_units( value.title, value.title_length, astral, 4 );
        set_units( value.line.text, value.line.text_length, two, 2 );
        value.lines_count = 2;
        set_units( value.lines[0].text, value.lines[0].text_length, two, 2 );
        set_units( value.lines[1].text, value.lines[1].text_length, basic, 3 );
        value.body.type = BodyType::Wide;
        set_units( value.body.wide.value, value.body.wide.value_length, two, 2 );
        save_and_pin_caption( "wide_sites", value );
    }
    {
        // wide_narrow_arm: the same union at its NARROW arm, so a respelling
        // between kind 12 and kind 33 is an ordinary kind mismatch in both
        // directions rather than UTF-8 bytes read as code units (§3)
        Caption value;
        value.body.type = BodyType::Narrow;
        memcpy( value.body.narrow.value, "ok", 2 );
        value.body.narrow.value[2] = 0;
        value.body.narrow.value_length = 2;
        save_and_pin_caption( "wide_narrow_arm", value );
    }
    {
        // wide_stamp: the FIXED root, whose cook is one region of one node
        Stamp value;
        set_units( value.label, value.label_length, two, 2 );
        value.seq = 7;
        static uint8_t buffer[1u << 12];
        const int64_t size = StampSave( value, buffer, (int64_t) sizeof( buffer ) );
        check( size > 0 );
        if ( size > 0 ) { pin( "wide_stamp", buffer, size ); }
    }
}

int main()
{
    pinned_instances();
    field_vectors();
    odd_length();
    default_and_count();
    nested_type_site();
    arm_site();
    element_site();
    text_form();
    cook_layout();
    if ( failures != 0 )
    {
        printf( "wide table gate: %d check(s) failed\n", failures );
        return 1;
    }
    printf( "wide table gate: kind 33 at every site, the content rule, the clamp, the text form and the cook\n" );
    return 0;
}
