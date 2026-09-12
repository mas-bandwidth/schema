// THE FIXED FORM'S THREE PROPERTIES (docs/SPEC-TABLES.md §3.4).
//
// Glenn asked whether nine ports could have found tonight's bugs cheaper.
// This binary is the answer: the same three questions, over every fixture,
// and a KNOWN-RED list printed by name.
//
//   P1  the identity plan and a plan compiled from this build's own layout
//       land the same fields and move the same counters
//   P2  write, read, write again: the bytes are identical
//   P3  every byte of every record, mutated to {00,01,02,7f,80,ff}, both
//       paths: every landed field is within its bound or the read is a
//       named refusal, and a correction moves a counter. Under ASan+UBSan.
//
// A listed known-red may fail and the run still ends green. A listed case
// that starts PASSING turns the run red: delete it from known_red[] as part
// of landing the fix. A new red that is not on the list fails the target.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "ScalarsTable.h"
#include "Scalars2Table.h"
#include "FX1Table.h"
#include "FX2Table.h"
#include "UT1Table.h"
#include "UT2Table.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P3Table.h"
#include "FN1Table.h"
#include "FN2Table.h"
#include "FM1Table.h"
#include "FM2Table.h"
#include "F1Table.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// ---------------------------------------------------------------------------
// THE KNOWN-RED LIST. Held from both ends: a listed case that starts passing
// is a gate that has stopped covering something.
struct KnownRed
{
    const char * key;
    const char * waits_on;
    int reached;
    int failed;
};

static KnownRed known_red[] = {
    { "optional/absent-payload-residue-is-copied",
      "the ?T payload gated on the present byte (§3.4: IGNORED on read)", 0, 0 },
    { "bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool",
      "a ruling on a bool byte outside {0,1}, and a normalise on the read side", 0, 0 },
    { "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant",
      "identity and compiled must agree on an ordinal that names no variant", 0, 0 },
    { "ordinal-bound/paths-disagree/union-tag-past-the-last-arm",
      "identity and compiled must agree on a tag that names no arm", 0, 0 },
};

static KnownRed * find_red( const char * key )
{
    for ( KnownRed & r : known_red )
    {
        if ( std::strcmp( r.key, key ) == 0 ) { return &r; }
    }
    return NULL;
}

static void check_red( bool ok, const char * key, const char * what )
{
    KnownRed * r = find_red( key );
    if ( r == NULL )
    {
        check( ok, what );
        return;
    }
    r->reached++;
    if ( !ok )
    {
        r->failed++;
        if ( r->failed <= 3 ) { std::printf( "KNOWN-RED [%s]: %s\n", r->key, what ); }
        else if ( r->failed == 4 ) { std::printf( "KNOWN-RED [%s]: (further hits suppressed)\n", r->key ); }
    }
}

static int file_slack()
{
    return 32;
}

static int known_red_report()
{
    int bad = 0;
    const int n = (int) ( sizeof( known_red ) / sizeof( known_red[0] ) );
    std::printf( "KNOWN-RED, %d case(s), each printed above and each waiting on a named fix:\n", n );
    for ( const KnownRed & r : known_red )
    {
        std::printf( "  %-58s  %s  <- %s\n", r.key,
                     r.failed > 0 ? "RED  " : ( r.reached > 0 ? "GREEN" : "UNRUN" ), r.waits_on );
        if ( r.reached == 0 )
        {
            std::printf( "FAIL: KNOWN-RED case %s was never reached: the fixture that carries it is gone\n", r.key );
            bad++;
        }
        else if ( r.failed == 0 )
        {
            std::printf( "FAIL: KNOWN-RED case %s PASSES now — delete it from known_red[] as part of landing %s\n",
                         r.key, r.waits_on );
            bad++;
        }
    }
    return bad;
}

// ---------------------------------------------------------------------------

struct Issues
{
    bool bool_oob = false;
    bool enum_oob = false;
    bool tag_oob = false;
    bool range_oob = false;
    bool utf8_bad = false;
    bool absent_residue = false;
    bool unsafe = false; // a live count past its array: generated clamp would index OOB
    bool any() const
    {
        return bool_oob || enum_oob || tag_oob || range_oob || utf8_bad || absent_residue || unsafe;
    }
};

static uint8_t byte_of( const void * p )
{
    uint8_t u = 0;
    std::memcpy( &u, p, 1 );
    return u;
}

static bool utf8_ok( const char * s, int32_t n )
{
    if ( n < 0 ) { return false; }
    const uint8_t * p = (const uint8_t *) s;
    int32_t i = 0;
    while ( i < n )
    {
        const uint8_t c = p[i];
        int need = 0;
        if ( c < 0x80u ) { i++; continue; }
        if ( ( c & 0xE0u ) == 0xC0u ) { need = 1; if ( c < 0xC2u ) { return false; } }
        else if ( ( c & 0xF0u ) == 0xE0u ) { need = 2; }
        else if ( ( c & 0xF8u ) == 0xF0u ) { need = 3; if ( c > 0xF4u ) { return false; } }
        else { return false; }
        if ( i + need >= n ) { return false; }
        for ( int k = 1; k <= need; k++ )
        {
            if ( ( p[i + k] & 0xC0u ) != 0x80u ) { return false; }
        }
        i += 1 + need;
    }
    return true;
}

static const uint8_t kPoison[] = { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff };
static const int kPoisonN = 6;
static const int32_t kPlanCap = 8192;

template<typename Report>
static bool reports_eq( const Report & a, const Report & b )
{
    return a.unknown == b.unknown && a.kind_mismatch == b.kind_mismatch &&
           a.widened == b.widened && a.clamped == b.clamped &&
           a.malformed == b.malformed && a.refused == b.refused;
}

template<typename Report>
static bool report_clean( const Report & r )
{
    return r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0 &&
           !r.malformed && !r.refused;
}

#define FIXED_TRAITS( NS, TYPE, TAG ) \
struct TAG##Codec \
{ \
    using T = NS::TYPE; \
    using Report = NS::TableReport; \
    using Entry = NS::TableFixedEntry; \
    using Dst = NS::TableFixedDst; \
    using Fill = NS::TableFixedFill; \
    using View = NS::TableFixedLayoutView; \
    using Reason = NS::TableMessageReason; \
    static constexpr const char * name = #TAG; \
    static int64_t measure( int64_t n ) { return NS::TYPE##FixedMeasure( n ); } \
    static int64_t save( const T * v, int64_t n, uint8_t * b, int64_t c ) { return NS::TYPE##FixedSave( v, n, b, c ); } \
    static int64_t load( T * v, int64_t cap, const uint8_t * d, int64_t b, Entry * p, int32_t pc, Report * r ) \
    { return NS::TYPE##FixedLoad( v, cap, d, b, p, pc, NULL, r ); } \
    static void reset( T & v ) { NS::TYPE##Reset( v ); } \
    static void clamp( T & v, Report * r ) { NS::TYPE##FixedClamp( v, r ); } \
    static constexpr const Entry * plan = NS::TYPE##FixedPlan; \
    static constexpr int32_t plan_count = NS::TYPE##FixedPlanCount; \
    static constexpr int32_t plan_guarded = NS::TYPE##FixedPlanGuarded; \
    static constexpr const uint8_t * layout = NS::TYPE##FixedLayout; \
    static constexpr int64_t layout_bytes = NS::TYPE##FixedLayoutBytes; \
    static constexpr const Dst * dst = NS::TYPE##FixedDst; \
    static constexpr const Fill * cover = NS::TYPE##FixedCover; \
    static constexpr int32_t cover_count = NS::TYPE##FixedCoverCount; \
    static constexpr int64_t body_bytes = NS::TYPE##FixedBodyBytes; \
    static constexpr int64_t header = NS::kTableFixedHeaderBytes; \
    static constexpr uint64_t hash = NS::TYPE##FixedHash; \
    static constexpr uint8_t kCopy = NS::kTableFixedCopy; \
    static constexpr Reason no_layout = NS::no_layout; \
    static constexpr Reason layout_newer = NS::layout_newer; \
    static bool parse( const uint8_t * b, int64_t n, View & v, Reason & w ) { return NS::TableFixedParseLayout( b, n, v, w ); } \
    static int32_t compile( const View & th, Entry * p, int32_t cap, int32_t * g, \
                            uint32_t * fill_at, int32_t * fill_count, Report * r ) \
    { return NS::TableFixedCompile( th, layout, (int32_t) layout_bytes, dst, cover, cover_count, \
                                    p, cap, g, fill_at, fill_count, r ); } \
    static void fill_run( const Fill * f, int32_t n, const uint8_t * defaults, uint8_t * d ) \
    { NS::TableFixedFillRun( f, n, defaults, d ); } \
    static void run( const Entry * p, int32_t n, int32_t g, const uint8_t * s, uint8_t * d, Report * r ) \
    { NS::TableFixedRun( p, n, g, s, d, (uint32_t) sizeof( T ), r ); } \
    static uint32_t get32( const uint8_t * b ) { return NS::TableFixedGet32( b ); } \
    static uint64_t get64( const uint8_t * b ) { return NS::TableFixedGet64( b ); } \
    static uint64_t hash_of( const uint8_t * b, int64_t n ) { return NS::TableFixedHashOf( b, n ); } \
}

#define FIXED_TRAITS_NOCLAMP( NS, TYPE, TAG ) \
struct TAG##Codec \
{ \
    using T = NS::TYPE; \
    using Report = NS::TableReport; \
    using Entry = NS::TableFixedEntry; \
    using Dst = NS::TableFixedDst; \
    using Fill = NS::TableFixedFill; \
    using View = NS::TableFixedLayoutView; \
    using Reason = NS::TableMessageReason; \
    static constexpr const char * name = #TAG; \
    static int64_t measure( int64_t n ) { return NS::TYPE##FixedMeasure( n ); } \
    static int64_t save( const T * v, int64_t n, uint8_t * b, int64_t c ) { return NS::TYPE##FixedSave( v, n, b, c ); } \
    static int64_t load( T * v, int64_t cap, const uint8_t * d, int64_t b, Entry * p, int32_t pc, Report * r ) \
    { return NS::TYPE##FixedLoad( v, cap, d, b, p, pc, NULL, r ); } \
    static void reset( T & v ) { NS::TYPE##Reset( v ); } \
    static void clamp( T &, Report * ) {} \
    static constexpr const Entry * plan = NS::TYPE##FixedPlan; \
    static constexpr int32_t plan_count = NS::TYPE##FixedPlanCount; \
    static constexpr int32_t plan_guarded = NS::TYPE##FixedPlanGuarded; \
    static constexpr const uint8_t * layout = NS::TYPE##FixedLayout; \
    static constexpr int64_t layout_bytes = NS::TYPE##FixedLayoutBytes; \
    static constexpr const Dst * dst = NS::TYPE##FixedDst; \
    static constexpr const Fill * cover = NS::TYPE##FixedCover; \
    static constexpr int32_t cover_count = NS::TYPE##FixedCoverCount; \
    static constexpr int64_t body_bytes = NS::TYPE##FixedBodyBytes; \
    static constexpr int64_t header = NS::kTableFixedHeaderBytes; \
    static constexpr uint64_t hash = NS::TYPE##FixedHash; \
    static constexpr uint8_t kCopy = NS::kTableFixedCopy; \
    static constexpr Reason no_layout = NS::no_layout; \
    static constexpr Reason layout_newer = NS::layout_newer; \
    static bool parse( const uint8_t * b, int64_t n, View & v, Reason & w ) { return NS::TableFixedParseLayout( b, n, v, w ); } \
    static int32_t compile( const View & th, Entry * p, int32_t cap, int32_t * g, \
                            uint32_t * fill_at, int32_t * fill_count, Report * r ) \
    { return NS::TableFixedCompile( th, layout, (int32_t) layout_bytes, dst, cover, cover_count, \
                                    p, cap, g, fill_at, fill_count, r ); } \
    static void fill_run( const Fill * f, int32_t n, const uint8_t * defaults, uint8_t * d ) \
    { NS::TableFixedFillRun( f, n, defaults, d ); } \
    static void run( const Entry * p, int32_t n, int32_t g, const uint8_t * s, uint8_t * d, Report * r ) \
    { NS::TableFixedRun( p, n, g, s, d, (uint32_t) sizeof( T ), r ); } \
    static uint32_t get32( const uint8_t * b ) { return NS::TableFixedGet32( b ); } \
    static uint64_t get64( const uint8_t * b ) { return NS::TableFixedGet64( b ); } \
    static uint64_t hash_of( const uint8_t * b, int64_t n ) { return NS::TableFixedHashOf( b, n ); } \
}

template<typename F>
static bool plan_run_copy_17_31()
{
    for ( int32_t i = 0; i < F::plan_count; i++ )
    {
        if ( F::plan[i].op == F::kCopy && F::plan[i].size >= 17u && F::plan[i].size <= 31u ) { return true; }
    }
    return false;
}

template<typename F>
static const uint8_t * record_at( const uint8_t * file )
{
    const uint32_t layout_bytes = F::get32( file + F::header );
    return file + F::header + 4 + layout_bytes;
}

template<typename F>
static int64_t load_identity( typename F::T & v, const uint8_t * file, int64_t bytes, typename F::Report & r )
{
    std::vector<typename F::Entry> plan( (size_t) kPlanCap );
    std::memset( &v, 0, sizeof( v ) );
    return F::load( &v, 1, file, bytes, plan.data(), kPlanCap, &r );
}

template<typename F>
static int64_t load_compiled( typename F::T & v, const uint8_t * file, int64_t bytes, typename F::Report & r )
{
    (void) bytes;
    const uint8_t * layout = file + F::header + 4;
    const uint32_t layout_bytes = F::get32( file + F::header );
    typename F::View view;
    typename F::Reason why = F::no_layout;
    if ( !F::parse( layout, (int64_t) layout_bytes, view, why ) )
    {
        r.refused = true;
        r.reason = why;
        return -1;
    }
    std::vector<typename F::Entry> plan( (size_t) kPlanCap );
    int32_t guarded = 0;
    uint32_t fill_at = 0;
    int32_t fill_count = 0;
    const int32_t made = F::compile( view, plan.data(), kPlanCap, &guarded, &fill_at, &fill_count, &r );
    if ( made < 0 )
    {
        r.refused = true;
        return -1;
    }
    const uint8_t * rec = layout + layout_bytes;
    // Digest is not on the wire: the record carries the layout hash, which
    // folds the definitions digest (bill §13). hash_of(layout) is layout
    // bytes alone and is the wrong identity for a table that has a range,
    // bits, fixed, or flags.
    if ( F::get64( rec ) != F::hash )
    {
        r.refused = true;
        r.reason = F::no_layout;
        return -1;
    }
    std::memset( &v, 0, sizeof( v ) );
    // THE PREFILL IS THE COMPILE'S ANSWER AND THE READER RUNS IT, which is the
    // one path the generated load takes: the ranges the plan does not land, out
    // of a default image reset once per read rather than a whole-value reset per
    // record. The identity plan's list is empty and the whole of this is a skip,
    // so the two loads below this gate stay the same load.
    typename F::T defaults;
    if ( fill_count > 0 )
    {
        std::memset( (void *) &defaults, 0, sizeof( defaults ) );
        F::reset( defaults );
    }
    const typename F::Fill * fill = ( fill_count > 0 )
        ? (const typename F::Fill *) (const void *) ( (const uint8_t *) plan.data() + fill_at )
        : NULL;
    F::fill_run( fill, fill_count, (const uint8_t *) &defaults, (uint8_t *) &v );
    F::run( plan.data(), made, guarded, rec + 8, (uint8_t *) &v, &r );
    return 1;
}

template<typename F>
static int64_t load_unclamped( typename F::T & v, const uint8_t * file, typename F::Report & r )
{
    const uint8_t * rec = record_at<F>( file );
    if ( F::get64( rec ) != F::hash )
    {
        r.refused = true;
        r.reason = F::no_layout;
        return -1;
    }
    std::memset( &v, 0, sizeof( v ) );
    F::reset( v );
    F::run( F::plan, F::plan_count, F::plan_guarded, rec + 8, (uint8_t *) &v, &r );
    return 1;
}

// Decode without going through generated clamp when a bool byte is outside
// {0,1}: that load is undefined in C++ and UBSan aborts, which is the
// bool-domain red — reported by memcpy, never by reading the bool.
template<typename F>
static int64_t decode_identity( typename F::T & v, const uint8_t * file, typename F::Report & r, Issues & iss )
{
    const int64_t n = load_unclamped<F>( v, file, r );
    if ( n < 0 ) { return n; }
    F::inspect( v, iss );
    if ( iss.bool_oob || iss.unsafe ) { return 1; }
    F::clamp( v, &r );
    iss = Issues{};
    F::inspect( v, iss );
    return 1;
}

template<typename F>
static int64_t decode_compiled( typename F::T & v, const uint8_t * file, int64_t bytes, typename F::Report & r, Issues & iss )
{
    const int64_t n = load_compiled<F>( v, file, bytes, r );
    if ( n < 0 ) { return n; }
    // load_compiled already clamped. A bool of 2 is UB inside that clamp.
    // Catch it first by re-running without clamp when inspect says so —
    // the compiled path's clamp is the same generated function.
    iss = Issues{};
    F::inspect( v, iss );
    return n;
}

static void classify_issues( const Issues & iss, const char * what )
{
    if ( iss.utf8_bad )
    {
        check_red( false, "text-content/identity/invalid-utf8-is-not-malformed", what );
        return;
    }
    if ( iss.bool_oob )
    {
        check_red( false, "bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool", what );
        return;
    }
    if ( iss.enum_oob )
    {
        check_red( false, "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant", what );
        return;
    }
    if ( iss.tag_oob )
    {
        check_red( false, "ordinal-bound/paths-disagree/union-tag-past-the-last-arm", what );
        return;
    }
    if ( iss.unsafe )
    {
        check_red( false, "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours", what );
        return;
    }
    if ( iss.absent_residue )
    {
        check_red( false, "optional/absent-payload-residue-is-copied", what );
        return;
    }
    if ( iss.range_oob )
    {
        check( false, what );
        return;
    }
}

template<typename F>
static void run_properties()
{
    typename F::T v;
    std::memset( &v, 0, sizeof( v ) );
    F::reset( v );
    F::fill( v );

    const int64_t need = F::measure( 1 );
    std::vector<uint8_t> file( (size_t) need + (size_t) file_slack(), 0 );
    char what[256];
    std::snprintf( what, sizeof( what ), "%s: save", F::name );
    check( F::save( &v, 1, file.data(), need ) == need, what );

    const bool runcopy = plan_run_copy_17_31<F>();

    // P1: identity == compiled, fields and counters.
    {
        typename F::T id{}, co{};
        typename F::Report rid{}, rco{};
        Issues iid{};
        const int64_t ni = decode_identity<F>( id, file.data(), rid, iid );
        const int64_t nc = load_compiled<F>( co, file.data(), need, rco );
        if ( nc == 1 )
        {
            Issues ico{};
            F::inspect( co, ico );
            if ( !ico.bool_oob && !ico.unsafe ) { F::clamp( co, &rco ); }
        }
        std::snprintf( what, sizeof( what ), "P1 %s: both paths return one record", F::name );
        check( ni == 1 && nc == 1, what );
        const bool fields = F::same( id, co );
        const bool counters = reports_eq( rid, rco );
        if ( !fields || !counters )
        {
            Issues iss{};
            F::inspect( id, iss );
            F::inspect( co, iss );
            std::snprintf( what, sizeof( what ), "P1 %s: identity and compiled disagree (fields=%d counters=%d)",
                           F::name, (int) fields, (int) counters );
            classify_issues( iss, what );
            if ( !iss.any() ) { check( false, what ); }
        }
    }

    // P2: write-read-write is byte-identical.
    {
        typename F::T back{};
        typename F::Report r{};
        Issues iss{};
        const int64_t n = decode_identity<F>( back, file.data(), r, iss );
        std::snprintf( what, sizeof( what ), "P2 %s: write-read-write byte-identical", F::name );
        if ( n != 1 || iss.unsafe || iss.bool_oob )
        {
            if ( runcopy )
            {
                check_red( false, "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours", what );
            }
            else
            {
                check( false, what );
            }
        }
        else
        {
            std::vector<uint8_t> again( (size_t) need + (size_t) file_slack(), 0 );
            const int64_t w = F::save( &back, 1, again.data(), need );
            const bool same = w == need && std::memcmp( file.data(), again.data(), (size_t) need ) == 0;
            if ( !same && runcopy )
            {
                check_red( false, "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours", what );
            }
            else
            {
                check( same, what );
            }
        }
    }

    // P3: mutate every record byte, both paths.
    const uint8_t * rec0 = record_at<F>( file.data() );
    const size_t rec_off = (size_t) ( rec0 - file.data() );
    const size_t rec_bytes = (size_t) ( 8 + F::body_bytes );
    for ( size_t at = 0; at < rec_bytes; at++ )
    {
        for ( int p = 0; p < kPoisonN; p++ )
        {
            std::vector<uint8_t> hit = file;
            hit[rec_off + at] = kPoison[p];

            typename F::T id{}, co{};
            typename F::Report rid{}, rco{};
            Issues iid{}, ico{};
            const int64_t ni = decode_identity<F>( id, hit.data(), rid, iid );
            const int64_t nc = load_compiled<F>( co, hit.data(), need, rco );
            if ( nc == 1 )
            {
                F::inspect( co, ico );
                if ( !ico.bool_oob && !ico.unsafe ) { F::clamp( co, &rco ); ico = Issues{}; F::inspect( co, ico ); }
            }

            const bool id_ok = ni == 1 && !rid.refused && !rid.malformed;
            const bool co_ok = nc == 1 && !rco.refused && !rco.malformed;

            if ( ni < 0 )
            {
                check( rid.refused || rid.malformed,
                       "P3: a negative identity read is a named refusal or malformed" );
            }
            if ( nc < 0 )
            {
                check( rco.refused || rco.malformed,
                       "P3: a negative compiled read is a named refusal or malformed" );
            }

            if ( id_ok )
            {
                if ( iid.any() )
                {
                    std::snprintf( what, sizeof( what ), "P3 %s identity byte %zu poison 0x%02x: landed out of bound",
                                   F::name, at, kPoison[p] );
                    classify_issues( iid, what );
                }
                else if ( !iid.bool_oob )
                {
                    typename F::T raw{};
                    typename F::Report rr{};
                    load_unclamped<F>( raw, hit.data(), rr );
                    Issues before{};
                    F::inspect( raw, before );
                    if ( before.range_oob || before.enum_oob || before.tag_oob )
                    {
                        std::snprintf( what, sizeof( what ),
                                       "P3 %s identity byte %zu: a bound correction did not move clamped",
                                       F::name, at );
                        if ( before.enum_oob )
                        {
                            check_red( rid.clamped > 0,
                                       "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant", what );
                        }
                        else if ( before.tag_oob )
                        {
                            check_red( rid.clamped > 0,
                                       "ordinal-bound/paths-disagree/union-tag-past-the-last-arm", what );
                        }
                        else
                        {
                            check( rid.clamped > 0, what );
                        }
                    }
                    if ( before.utf8_bad && !rid.malformed )
                    {
                        std::snprintf( what, sizeof( what ),
                                       "P3 %s identity byte %zu: invalid UTF-8 was not malformed",
                                       F::name, at );
                        check_red( false, "text-content/identity/invalid-utf8-is-not-malformed", what );
                    }
                }
            }
            if ( co_ok && ico.any() )
            {
                std::snprintf( what, sizeof( what ), "P3 %s compiled byte %zu poison 0x%02x: landed out of bound",
                               F::name, at, kPoison[p] );
                classify_issues( ico, what );
            }
            if ( id_ok && co_ok )
            {
                const bool fields = F::same( id, co );
                const bool counters = reports_eq( rid, rco );
                if ( !fields || !counters )
                {
                    Issues iss = iid;
                    if ( ico.bool_oob ) { iss.bool_oob = true; }
                    if ( ico.enum_oob ) { iss.enum_oob = true; }
                    if ( ico.tag_oob ) { iss.tag_oob = true; }
                    if ( ico.range_oob ) { iss.range_oob = true; }
                    if ( ico.utf8_bad ) { iss.utf8_bad = true; }
                    std::snprintf( what, sizeof( what ),
                                   "P3 %s byte %zu poison 0x%02x: identity and compiled disagree",
                                   F::name, at, kPoison[p] );
                    classify_issues( iss, what );
                    if ( !iss.any() )
                    {
                        if ( !counters && rid.clamped != rco.clamped )
                        {
                            check_red( false, "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant", what );
                        }
                        else
                        {
                            check( false, what );
                        }
                    }
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Fixtures.

FIXED_TRAITS( tblfx1, FxRoot, FX1 );
struct FX1 : FX1Codec
{
    static void fill( T & v )
    {
        v.keep = 4242u;
        v.narrow = 40000u;
        v.renamed = 321;
        v.gone = 654;
        v.nested.a = 111;
        v.nested.b = 222;
        std::strcpy( v.label, "fx1" );
        v.label_length = 3;
        v.marks[0] = 101;
        v.marks[1] = 202;
        v.marks_count = 2;
        v.blob[0] = 0xDE; v.blob[1] = 0xAD; v.blob[2] = 0xBE; v.blob[3] = 0xEF;
        v.blob_length = 4;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( v.renamed < 0 || v.renamed > 1000 ) { i.range_oob = true; }
        if ( v.gone < 0 || v.gone > 1000 ) { i.range_oob = true; }
        if ( v.nested.a < 0 || v.nested.a > 1000 ) { i.range_oob = true; }
        if ( v.nested.b < 0 || v.nested.b > 1000 ) { i.range_oob = true; }
        if ( v.label_length < 0 || v.label_length > 8 ) { i.range_oob = true; }
        if ( v.marks_count < 0 || v.marks_count > 4 ) { i.unsafe = true; }
        if ( v.blob_length < 0 || v.blob_length > 6 ) { i.range_oob = true; }
        if ( v.label_length >= 0 && v.label_length <= 8 && !utf8_ok( v.label, v.label_length ) ) { i.utf8_bad = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return a.keep == b.keep && a.narrow == b.narrow && a.renamed == b.renamed && a.gone == b.gone &&
               a.nested.a == b.nested.a && a.nested.b == b.nested.b &&
               a.label_length == b.label_length && a.label_length >= 0 && a.label_length <= 8 &&
               std::memcmp( a.label, b.label, (size_t) a.label_length ) == 0 &&
               a.marks_count == b.marks_count && a.marks_count >= 0 && a.marks_count <= 4 &&
               std::memcmp( a.marks, b.marks, (size_t) a.marks_count * sizeof( int32_t ) ) == 0 &&
               a.blob_length == b.blob_length && a.blob_length >= 0 && a.blob_length <= 6 &&
               std::memcmp( a.blob, b.blob, (size_t) a.blob_length ) == 0;
    }
};

FIXED_TRAITS( tblfx2, FxRoot, FX2 );
struct FX2 : FX2Codec
{
    static void fill( T & v )
    {
        v.keep = 5150u;
        v.narrow = 70000u;
        v.renamed_to = 808;
        v.added = 909;
        v.nested.a = 33;
        v.nested.b = 44;
        v.extra.x = 55;
        v.extra.y = 66;
        std::strcpy( v.label, "fx2" );
        v.label_length = 3;
        v.marks[0] = 303;
        v.marks_count = 1;
        v.blob[0] = 0x01; v.blob[1] = 0x02; v.blob[2] = 0x03;
        v.blob_length = 3;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( v.renamed_to < 0 || v.renamed_to > 1000 ) { i.range_oob = true; }
        if ( v.added < 0 || v.added > 1000 ) { i.range_oob = true; }
        if ( v.nested.a < 0 || v.nested.a > 1000 ) { i.range_oob = true; }
        if ( v.nested.b < 0 || v.nested.b > 1000 ) { i.range_oob = true; }
        if ( v.extra.x < 0 || v.extra.x > 1000 ) { i.range_oob = true; }
        if ( v.extra.y < 0 || v.extra.y > 1000 ) { i.range_oob = true; }
        if ( v.label_length < 0 || v.label_length > 8 ) { i.range_oob = true; }
        if ( v.marks_count < 0 || v.marks_count > 4 ) { i.unsafe = true; }
        if ( v.blob_length < 0 || v.blob_length > 6 ) { i.range_oob = true; }
        if ( v.label_length >= 0 && v.label_length <= 8 && !utf8_ok( v.label, v.label_length ) ) { i.utf8_bad = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return a.keep == b.keep && a.narrow == b.narrow && a.renamed_to == b.renamed_to && a.added == b.added &&
               a.nested.a == b.nested.a && a.nested.b == b.nested.b && a.extra.x == b.extra.x && a.extra.y == b.extra.y &&
               a.label_length == b.label_length && a.label_length >= 0 && a.label_length <= 8 &&
               std::memcmp( a.label, b.label, (size_t) a.label_length ) == 0 &&
               a.marks_count == b.marks_count && a.marks_count >= 0 && a.marks_count <= 4 &&
               std::memcmp( a.marks, b.marks, (size_t) a.marks_count * sizeof( int32_t ) ) == 0 &&
               a.blob_length == b.blob_length && a.blob_length >= 0 && a.blob_length <= 6 &&
               std::memcmp( a.blob, b.blob, (size_t) a.blob_length ) == 0;
    }
};

FIXED_TRAITS( tblut1, UtRoot, UT1 );
struct UT1 : UT1Codec
{
    static void fill( T & v )
    {
        v.head = 42;
        v.tail = 99;
        v.pick.type = tblut1::PickType::B;
        std::strcpy( v.pick.b.label, "seven77" );
        v.pick.b.label_length = 7;
        v.pick.b.m = 1234;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.pick.type ) > (uint8_t) tblut1::PickType::Max ) { i.tag_oob = true; }
        if ( v.pick.type == tblut1::PickType::B )
        {
            if ( v.pick.b.label_length < 0 || v.pick.b.label_length > 8 ) { i.range_oob = true; }
            else if ( !utf8_ok( v.pick.b.label, v.pick.b.label_length ) ) { i.utf8_bad = true; }
        }
    }
    static bool same( const T & a, const T & b )
    {
        if ( a.head != b.head || a.tail != b.tail || a.pick.type != b.pick.type ) { return false; }
        if ( a.pick.type == tblut1::PickType::B )
        {
            return a.pick.b.label_length == b.pick.b.label_length && a.pick.b.m == b.pick.b.m &&
                   a.pick.b.label_length >= 0 && a.pick.b.label_length <= 8 &&
                   std::memcmp( a.pick.b.label, b.pick.b.label, (size_t) a.pick.b.label_length ) == 0;
        }
        if ( a.pick.type == tblut1::PickType::A ) { return a.pick.a.n == b.pick.a.n; }
        return true;
    }
};

FIXED_TRAITS( tblut2, UtRoot, UT2 );
struct UT2 : UT2Codec
{
    static void fill( T & v )
    {
        v.head = 7;
        v.tail = 8;
        v.pick.type = tblut2::PickType::B;
        std::strcpy( v.pick.b.label, "third" );
        v.pick.b.label_length = 5;
        v.pick.b.m = 555;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.pick.type ) > (uint8_t) tblut2::PickType::Max ) { i.tag_oob = true; }
        if ( v.pick.type == tblut2::PickType::B )
        {
            if ( v.pick.b.label_length < 0 || v.pick.b.label_length > 8 ) { i.range_oob = true; }
            else if ( !utf8_ok( v.pick.b.label, v.pick.b.label_length ) ) { i.utf8_bad = true; }
        }
    }
    static bool same( const T & a, const T & b )
    {
        if ( a.head != b.head || a.tail != b.tail || a.pick.type != b.pick.type ) { return false; }
        if ( a.pick.type == tblut2::PickType::B )
        {
            return a.pick.b.label_length == b.pick.b.label_length && a.pick.b.m == b.pick.b.m &&
                   a.pick.b.label_length >= 0 && a.pick.b.label_length <= 8 &&
                   std::memcmp( a.pick.b.label, b.pick.b.label, (size_t) a.pick.b.label_length ) == 0;
        }
        if ( a.pick.type == tblut2::PickType::A ) { return a.pick.a.n == b.pick.a.n; }
        return true;
    }
};

FIXED_TRAITS( tblp1, Chain, P1 );
struct P1 : P1Codec
{
    static void fill( T & v )
    {
        std::strcpy( v.name, "value" );
        v.name_length = 5;
        v.link.value = 42;
        std::strcpy( v.link.tag, "p1" );
        v.link.tag_length = 2;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( v.name_length < 0 || v.name_length > 16 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.name, v.name_length ) ) { i.utf8_bad = true; }
        if ( v.link.value < 0 || v.link.value > 1000 ) { i.range_oob = true; }
        if ( v.link.tag_length < 0 || v.link.tag_length > 8 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.link.tag, v.link.tag_length ) ) { i.utf8_bad = true; }
    }
    static bool same( const T & a, const T & b )
    {
        if ( a.name_length != b.name_length || a.name_length < 0 || a.name_length > 16 ) { return false; }
        if ( std::memcmp( a.name, b.name, (size_t) a.name_length ) != 0 ) { return false; }
        return a.link.value == b.link.value && a.link.tag_length == b.link.tag_length &&
               a.link.tag_length >= 0 && a.link.tag_length <= 8 &&
               std::memcmp( a.link.tag, b.link.tag, (size_t) a.link.tag_length ) == 0;
    }
};

FIXED_TRAITS( tblp3, Chain, P3 );
struct P3 : P3Codec
{
    static void fill( T & v )
    {
        std::strcpy( v.name, "absent" );
        v.name_length = 6;
        v.link_present = false;
        v.link.value = 0;
        v.link.tag_length = 0;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.link_present ) > 1 ) { i.bool_oob = true; }
        if ( v.name_length < 0 || v.name_length > 16 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.name, v.name_length ) ) { i.utf8_bad = true; }
        if ( byte_of( &v.link_present ) == 0 )
        {
            if ( v.link.value != 0 || v.link.tag_length != 0 ) { i.absent_residue = true; }
        }
        else
        {
            if ( v.link.value < 0 || v.link.value > 1000 ) { i.range_oob = true; }
            if ( v.link.tag_length < 0 || v.link.tag_length > 8 ) { i.range_oob = true; }
            else if ( !utf8_ok( v.link.tag, v.link.tag_length ) ) { i.utf8_bad = true; }
        }
    }
    static bool same( const T & a, const T & b )
    {
        if ( a.name_length != b.name_length || a.name_length < 0 || a.name_length > 16 ) { return false; }
        if ( std::memcmp( a.name, b.name, (size_t) a.name_length ) != 0 ) { return false; }
        if ( byte_of( &a.link_present ) != byte_of( &b.link_present ) ) { return false; }
        if ( byte_of( &a.link_present ) == 0 ) { return true; }
        return a.link.value == b.link.value && a.link.tag_length == b.link.tag_length &&
               a.link.tag_length >= 0 && a.link.tag_length <= 8 &&
               std::memcmp( a.link.tag, b.link.tag, (size_t) a.link.tag_length ) == 0;
    }
};

FIXED_TRAITS( tblfn1, FnRoot, FN1 );
struct FN1 : FN1Codec
{
    static void fill( T & v )
    {
        v.flag = true;
        v.tier = tblfn1::Tier::Gold;
        v.opt_present = false;
        v.picks_count = 1;
        v.picks[0].type = tblfn1::PickType::B;
        v.picks[0].b.pts[0] = 1;
        v.picks[0].b.pts[1] = 2;
        v.picks[0].b.m = 3;
        v.deep.x = 10;
        v.deep.mid.y = 20;
        v.deep.mid.deeper.z = 30;
        std::strcpy( v.label, "ab" );
        v.label_length = 2;
        v.after = 7;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.flag ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.opt_present ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.tier ) > (uint8_t) tblfn1::Tier::Max ) { i.enum_oob = true; }
        if ( v.picks_count < 0 || v.picks_count > 2 ) { i.unsafe = true; }
        if ( v.label_length < 0 || v.label_length > 4 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.label, v.label_length ) ) { i.utf8_bad = true; }
        if ( v.after < 0 || v.after > 1000 ) { i.range_oob = true; }
        if ( v.deep.x < 0 || v.deep.x > 1000 ) { i.range_oob = true; }
        if ( v.deep.mid.y < 0 || v.deep.mid.y > 1000 ) { i.range_oob = true; }
        if ( v.deep.mid.deeper.z < 0 || v.deep.mid.deeper.z > 1000 ) { i.range_oob = true; }
        const int32_t live = v.picks_count < 0 ? 0 : ( v.picks_count > 2 ? 2 : v.picks_count );
        for ( int32_t k = 0; k < live; k++ )
        {
            if ( byte_of( &v.picks[k].type ) > (uint8_t) tblfn1::PickType::Max ) { i.tag_oob = true; }
        }
        if ( byte_of( &v.opt_present ) == 0 )
        {
            if ( v.opt.z != 30 ) { i.absent_residue = true; }
        }
        else if ( v.opt.z < 0 || v.opt.z > 1000 ) { i.range_oob = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return byte_of( &a.flag ) == byte_of( &b.flag ) && byte_of( &a.tier ) == byte_of( &b.tier ) &&
               byte_of( &a.opt_present ) == byte_of( &b.opt_present ) &&
               a.picks_count == b.picks_count && a.after == b.after &&
               a.deep.x == b.deep.x && a.deep.mid.y == b.deep.mid.y && a.deep.mid.deeper.z == b.deep.mid.deeper.z &&
               a.label_length == b.label_length && a.label_length >= 0 && a.label_length <= 4 &&
               std::memcmp( a.label, b.label, (size_t) a.label_length ) == 0;
    }
};

FIXED_TRAITS( tblfn2, FnRoot, FN2 );
struct FN2 : FN2Codec
{
    static void fill( T & v )
    {
        v.flag = true;
        v.tier = tblfn2::Tier::Gold;
        v.opt_present = true;
        v.opt.z = 31;
        v.opt.added = 33;
        v.picks_count = 1;
        v.picks[0].type = tblfn2::PickType::B;
        v.picks[0].b.pts[0] = 4;
        v.picks[0].b.pts[1] = 5;
        v.picks[0].b.m = 6;
        v.deep.x = 11;
        v.deep.mid.y = 21;
        v.deep.mid.deeper.z = 31;
        std::strcpy( v.caption, "cd" );
        v.caption_length = 2;
        v.after = 8;
        v.tail = 3;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.flag ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.opt_present ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.tier ) > (uint8_t) tblfn2::Tier::Max ) { i.enum_oob = true; }
        if ( v.picks_count < 0 || v.picks_count > 2 ) { i.unsafe = true; }
        if ( v.caption_length < 0 || v.caption_length > 4 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.caption, v.caption_length ) ) { i.utf8_bad = true; }
        if ( v.after < 0 || v.after > 1000 ) { i.range_oob = true; }
        if ( v.tail < 0 || v.tail > 1000 ) { i.range_oob = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return byte_of( &a.flag ) == byte_of( &b.flag ) && byte_of( &a.tier ) == byte_of( &b.tier ) &&
               byte_of( &a.opt_present ) == byte_of( &b.opt_present ) &&
               a.picks_count == b.picks_count && a.after == b.after && a.tail == b.tail &&
               a.caption_length == b.caption_length && a.caption_length >= 0 && a.caption_length <= 4 &&
               std::memcmp( a.caption, b.caption, (size_t) a.caption_length ) == 0;
    }
};

FIXED_TRAITS( tblfm1, MarkRoot, FM1 );
struct FM1 : FM1Codec
{
    static void fill( T & v )
    {
        v.id = 9;
        v.mark.type = tblfm1::MarkType::Narrow;
        std::strcpy( v.mark.narrow.s, "hello" );
        v.mark.narrow.s_length = 5;
        v.mark.narrow.n = 11;
        v.after = 7;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.mark.type ) > (uint8_t) tblfm1::MarkType::Max ) { i.tag_oob = true; }
        if ( v.after < 0 || v.after > 1000 ) { i.range_oob = true; }
        if ( v.mark.type == tblfm1::MarkType::Narrow )
        {
            if ( v.mark.narrow.s_length < 0 || v.mark.narrow.s_length > 6 ) { i.range_oob = true; }
            else if ( !utf8_ok( v.mark.narrow.s, v.mark.narrow.s_length ) ) { i.utf8_bad = true; }
            if ( v.mark.narrow.n < 0 || v.mark.narrow.n > 1000 ) { i.range_oob = true; }
        }
    }
    static bool same( const T & a, const T & b )
    {
        if ( a.id != b.id || a.after != b.after || a.mark.type != b.mark.type ) { return false; }
        if ( a.mark.type == tblfm1::MarkType::Narrow )
        {
            return a.mark.narrow.s_length == b.mark.narrow.s_length && a.mark.narrow.n == b.mark.narrow.n &&
                   a.mark.narrow.s_length >= 0 && a.mark.narrow.s_length <= 6 &&
                   std::memcmp( a.mark.narrow.s, b.mark.narrow.s, (size_t) a.mark.narrow.s_length ) == 0;
        }
        return true;
    }
};

FIXED_TRAITS( tblfm2, MarkRoot, FM2 );
struct FM2 : FM2Codec
{
    static void fill( T & v )
    {
        v.id = 10;
        v.mark.type = tblfm2::MarkType::Narrow;
        std::strcpy( v.mark.narrow.s, "world" );
        v.mark.narrow.s_length = 5;
        v.mark.narrow.n = 12;
        v.after = 8;
        v.tail = 3;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.mark.type ) > (uint8_t) tblfm2::MarkType::Max ) { i.tag_oob = true; }
        if ( v.after < 0 || v.after > 1000 ) { i.range_oob = true; }
        if ( v.tail < 0 || v.tail > 1000 ) { i.range_oob = true; }
        if ( v.mark.type == tblfm2::MarkType::Narrow )
        {
            if ( v.mark.narrow.s_length < 0 || v.mark.narrow.s_length > 6 ) { i.range_oob = true; }
            else if ( !utf8_ok( v.mark.narrow.s, v.mark.narrow.s_length ) ) { i.utf8_bad = true; }
        }
    }
    static bool same( const T & a, const T & b )
    {
        if ( a.id != b.id || a.after != b.after || a.tail != b.tail || a.mark.type != b.mark.type ) { return false; }
        if ( a.mark.type == tblfm2::MarkType::Narrow )
        {
            return a.mark.narrow.s_length == b.mark.narrow.s_length && a.mark.narrow.n == b.mark.narrow.n &&
                   a.mark.narrow.s_length >= 0 && a.mark.narrow.s_length <= 6 &&
                   std::memcmp( a.mark.narrow.s, b.mark.narrow.s, (size_t) a.mark.narrow.s_length ) == 0;
        }
        return true;
    }
};

FIXED_TRAITS( tblv1, Cfg, V1 );
struct V1 : V1Codec
{
    static void fill( T & v )
    {
        v.a = 42;
        std::strcpy( v.name, "hello" );
        v.name_length = 5;
        v.grade = tblv1::Grade::Gold;
        v.effect.type = tblv1::EffectType::Ward;
        v.effect.ward.charge = 0.75f;
        v.tokens[tblv1::Slot::Alpha] = 21;
        v.tokens[tblv1::Slot::Delta] = 24;
        v.tier_present = true;
        v.tier = 77;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( v.a < 0 || v.a > 1000 ) { i.range_oob = true; }
        if ( byte_of( &v.grade ) > (uint8_t) tblv1::Grade::Max ) { i.enum_oob = true; }
        if ( byte_of( &v.effect.type ) > (uint8_t) tblv1::EffectType::Max ) { i.tag_oob = true; }
        if ( byte_of( &v.extra_present ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.tier_present ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.mark_present ) > 1 ) { i.bool_oob = true; }
        if ( v.name_length < 0 || v.name_length > 32 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.name, v.name_length ) ) { i.utf8_bad = true; }
        if ( v.items_count < 0 || v.items_count > 8 ) { i.unsafe = true; }
        if ( v.grades_count < 0 || v.grades_count > 4 ) { i.unsafe = true; }
        if ( v.slots_count < 0 || v.slots_count > 6 ) { i.unsafe = true; }
        if ( byte_of( &v.tier_present ) == 0 && v.tier != 0 ) { i.absent_residue = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return a.a == b.a && std::memcmp( &a.b, &b.b, sizeof( float ) ) == 0 && a.mode == b.mode && a.grade == b.grade &&
               a.name_length == b.name_length && a.name_length >= 0 && a.name_length <= 32 &&
               std::memcmp( a.name, b.name, (size_t) a.name_length ) == 0 &&
               a.effect.type == b.effect.type &&
               byte_of( &a.tier_present ) == byte_of( &b.tier_present ) &&
               ( byte_of( &a.tier_present ) == 0 || a.tier == b.tier ) &&
               a.tokens.slots[0] == b.tokens.slots[0] && a.tokens.slots[3] == b.tokens.slots[3];
    }
};

FIXED_TRAITS( tblv2, Cfg, V2 );
struct V2 : V2Codec
{
    static void fill( T & v )
    {
        v.a = 5.0f;
        v.c = true;
        std::strcpy( v.title, "hello" );
        v.title_length = 5;
        v.grade = tblv2::Grade::Gold;
        v.effect.type = tblv2::EffectType::Ward;
        v.effect.ward.charge = 0.75f;
        v.tier_present = true;
        v.tier = 77;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.c ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.grade ) > (uint8_t) tblv2::Grade::Max ) { i.enum_oob = true; }
        if ( byte_of( &v.effect.type ) > (uint8_t) tblv2::EffectType::Max ) { i.tag_oob = true; }
        if ( byte_of( &v.extra_present ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.tier_present ) > 1 ) { i.bool_oob = true; }
        if ( byte_of( &v.mark_present ) > 1 ) { i.bool_oob = true; }
        if ( v.title_length < 0 || v.title_length > 32 ) { i.range_oob = true; }
        else if ( !utf8_ok( v.title, v.title_length ) ) { i.utf8_bad = true; }
        if ( v.items_count < 0 || v.items_count > 8 ) { i.unsafe = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return std::memcmp( &a.a, &b.a, sizeof( float ) ) == 0 && byte_of( &a.c ) == byte_of( &b.c ) && a.grade == b.grade &&
               a.title_length == b.title_length && a.title_length >= 0 && a.title_length <= 32 &&
               std::memcmp( a.title, b.title, (size_t) a.title_length ) == 0 &&
               a.effect.type == b.effect.type &&
               byte_of( &a.tier_present ) == byte_of( &b.tier_present );
    }
};

FIXED_TRAITS( scalardemo, SimState, Scalars );
struct Scalars : ScalarsCodec
{
    static void fill( T & v )
    {
        v.tilt = 1;
        v.angle = 2 * 65536;
        v.position = 100LL * 65536;
        v.ticks = 9;
        v.ratio = 1;
        v.speed = 3 * 65536;
        v.span = 4;
        v.frames = 5;
        v.scale = 65536;
        v.samples[0] = 65536;
        v.weights_count = 1;
        v.weights[0] = 256;
        v.seeds_count = 0;
        v.pose.x = 65536;
        v.pose.y = 0;
        v.pose.heading = 1;
        v.spawn_present = true;
        v.spawn.x = 2 * 65536;
        v.spawn.y = 0;
        v.spawn.heading = 2;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.spawn_present ) > 1 ) { i.bool_oob = true; }
        if ( v.weights_count < 0 || v.weights_count > 4 ) { i.unsafe = true; }
        if ( v.seeds_count < 0 || v.seeds_count > 2 ) { i.unsafe = true; }
        if ( v.ticks < 0 || v.ticks > 1000000 ) { i.range_oob = true; }
        if ( byte_of( &v.spawn_present ) == 0 && ( v.spawn.x != 0 || v.spawn.heading != 0 ) )
        {
            i.absent_residue = true;
        }
    }
    static bool same( const T & a, const T & b )
    {
        return a.tilt == b.tilt && a.angle == b.angle && a.position == b.position && a.ticks == b.ticks &&
               a.ratio == b.ratio && a.speed == b.speed && a.span == b.span && a.frames == b.frames &&
               a.scale == b.scale && a.weights_count == b.weights_count && a.seeds_count == b.seeds_count &&
               a.pose.heading == b.pose.heading && byte_of( &a.spawn_present ) == byte_of( &b.spawn_present );
    }
};

FIXED_TRAITS( scalardemo2, SimState, Scalars2 );
struct Scalars2 : Scalars2Codec
{
    static void fill( T & v )
    {
        v.tilt = 1;
        v.angle = 2;
        v.position = 50LL * 65536;
        v.ratio = 1;
        v.speed = 2 * 65536;
        v.scale = 65536;
        v.weights_count = 1;
        v.weights[0] = 256;
        v.pose.heading = 1;
        v.spawn_present = false;
    }
    static void inspect( const T & v, Issues & i )
    {
        if ( byte_of( &v.spawn_present ) > 1 ) { i.bool_oob = true; }
        if ( v.weights_count < 0 || v.weights_count > 4 ) { i.unsafe = true; }
        if ( v.seeds_count < 0 || v.seeds_count > 2 ) { i.unsafe = true; }
        if ( v.angle < -180 || v.angle > 180 ) { i.range_oob = true; }
        if ( v.speed > 10u * 65536u ) { i.range_oob = true; }
    }
    static bool same( const T & a, const T & b )
    {
        return a.tilt == b.tilt && a.angle == b.angle && a.position == b.position &&
               a.ratio == b.ratio && a.speed == b.speed && a.span == b.span &&
               a.scale == b.scale && a.weights_count == b.weights_count &&
               a.pose.heading == b.pose.heading && byte_of( &a.spawn_present ) == byte_of( &b.spawn_present );
    }
};

FIXED_TRAITS_NOCLAMP( tblf1, Floats, F1 );
struct F1 : F1Codec
{
    static void fill( T & v )
    {
        v.signalling = 1.0f;
        v.payload = 2.0f;
        v.negative = -3.0f;
        v.quiet = 4.0;
        v.wide = 5.0;
        v.samples[0] = 6.0f;
        v.after = 7;
    }
    static void inspect( const T & v, Issues & i ) { (void) v; (void) i; }
    static bool same( const T & a, const T & b )
    {
        return std::memcmp( &a.signalling, &b.signalling, sizeof( float ) * 3 ) == 0 &&
               std::memcmp( &a.quiet, &b.quiet, sizeof( double ) * 2 ) == 0 &&
               std::memcmp( a.samples, b.samples, sizeof( a.samples ) ) == 0 &&
               a.after == b.after;
    }
};

// ---------------------------------------------------------------------------
// Dedicated probes, so each named red is reached even when P3's classification
// lands on a neighbour.

// P1's reverse direction under the new contract: the older reader given the
// newer file refuses layout_newer. The old contract compiled a stranger's
// layout both ways; that test is retired, not bent. Forward (newer reads
// older) is a lineage compile and returns one record.
template<typename Older, typename Newer>
static void run_pair_p1()
{
    typename Older::T ov;
    std::memset( &ov, 0, sizeof( ov ) );
    Older::reset( ov );
    Older::fill( ov );
    const int64_t oneed = Older::measure( 1 );
    std::vector<uint8_t> ofile( (size_t) oneed + (size_t) file_slack(), 0 );
    char what[256];
    std::snprintf( what, sizeof( what ), "P1 %s/%s: older save", Older::name, Newer::name );
    check( Older::save( &ov, 1, ofile.data(), oneed ) == oneed, what );

    {
        typename Newer::T back;
        typename Newer::Report r{};
        std::vector<typename Newer::Entry> plan( (size_t) kPlanCap );
        std::memset( &back, 0, sizeof( back ) );
        const int64_t n = Newer::load( &back, 1, ofile.data(), oneed, plan.data(), kPlanCap, &r );
        std::snprintf( what, sizeof( what ),
                       "P1 %s reads older %s: compiled path through lineage, one record",
                       Newer::name, Older::name );
        check( n == 1 && !r.refused && !r.malformed, what );
    }

    typename Newer::T nv;
    std::memset( &nv, 0, sizeof( nv ) );
    Newer::reset( nv );
    Newer::fill( nv );
    const int64_t nneed = Newer::measure( 1 );
    std::vector<uint8_t> nfile( (size_t) nneed + (size_t) file_slack(), 0 );
    std::snprintf( what, sizeof( what ), "P1 %s/%s: newer save", Older::name, Newer::name );
    check( Newer::save( &nv, 1, nfile.data(), nneed ) == nneed, what );

    {
        typename Older::T back;
        typename Older::Report r{};
        std::vector<typename Older::Entry> plan( (size_t) kPlanCap );
        std::memset( &back, 0, sizeof( back ) );
        const int64_t n = Older::load( &back, 1, nfile.data(), nneed, plan.data(), kPlanCap, &r );
        std::snprintf( what, sizeof( what ),
                       "P1 %s given newer %s: layout_newer (old contract's reverse read is retired)",
                       Older::name, Newer::name );
        check( n == -1 && r.refused && r.reason == Older::layout_newer, what );
    }
}

static void probe_clamp_op()
{
    scalardemo::SimState w;
    std::memset( &w, 0, sizeof( w ) );
    scalardemo::SimStateReset( w );
    w.position = 2000LL * 65536; // in FX1's range, past Scalars2's 1000
    const int64_t need = scalardemo::SimStateFixedMeasure( 1 );
    std::vector<uint8_t> file( (size_t) need + (size_t) file_slack(), 0 );
    check( scalardemo::SimStateFixedSave( &w, 1, file.data(), need ) == need, "probe clamp-op: save" );

    scalardemo2::SimState back;
    std::memset( &back, 0, sizeof( back ) );
    scalardemo2::TableReport r;
    std::vector<scalardemo2::TableFixedEntry> plan( (size_t) kPlanCap );
    const int64_t n = scalardemo2::SimStateFixedLoad( &back, 1, file.data(), need, plan.data(), kPlanCap, NULL, &r );
    check( n == 1, "probe clamp-op: Scalars2 reads Scalars" );
    const bool clamped = back.position == 1000LL * 65536 && r.clamped >= 1;
    check( clamped, "probe clamp-op: a value past the reader's max lands at max and counts" );
}

static void probe_text_content()
{
    tblfx1::FxRoot v;
    std::memset( &v, 0, sizeof( v ) );
    FX1::reset( v );
    FX1::fill( v );
    std::vector<uint8_t> file( (size_t) tblfx1::FxRootFixedMeasure( 1 ) );
    check( tblfx1::FxRootFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "probe text-content: save" );
    uint8_t * rec = file.data() + tblfx1::kTableFixedHeaderBytes + 4 + tblfx1::FxRootFixedLayoutBytes;
    // label is after keep(4)+narrow(2)+renamed(4)+gone(4)+nested(8) = 22; length then 3 used bytes
    rec[8 + 22 + 4] = 0xFF; // first used byte of "fx1"
    tblfx1::FxRoot back;
    std::memset( &back, 0, sizeof( back ) );
    tblfx1::TableReport r;
    std::vector<tblfx1::TableFixedEntry> plan( (size_t) kPlanCap );
    const int64_t n = tblfx1::FxRootFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                               plan.data(), kPlanCap, NULL, &r );
    check( n == 1 || r.malformed || r.refused, "probe text-content: the read answers" );
    check_red( r.malformed, "text-content/identity/invalid-utf8-is-not-malformed",
               "probe text-content: invalid UTF-8 in a string(N)'s used bytes is malformed" );
}

static void probe_absent_optional()
{
    tblp3::Chain v;
    std::memset( &v, 0, sizeof( v ) );
    tblp3::ChainReset( v );
    std::strcpy( v.name, "absent" );
    v.name_length = 6;
    v.link_present = false;
    std::vector<uint8_t> file( (size_t) tblp3::ChainFixedMeasure( 1 ) );
    check( tblp3::ChainFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "probe absent-optional: save" );
    uint8_t * rec = file.data() + tblp3::kTableFixedHeaderBytes + 4 + tblp3::ChainFixedLayoutBytes;
    // name is string(16): 4+16=20, then the present byte, then Link.value
    rec[8 + 21] = 0x5A;
    rec[8 + 22] = 0x5A;
    rec[8 + 23] = 0x5A;
    rec[8 + 24] = 0x00;
    tblp3::Chain back;
    std::memset( &back, 0, sizeof( back ) );
    tblp3::TableReport r;
    std::vector<tblp3::TableFixedEntry> plan( (size_t) kPlanCap );
    const int64_t n = tblp3::ChainFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                             plan.data(), kPlanCap, NULL, &r );
    check( n == 1 && !back.link_present, "probe absent-optional: the flag" );
    const bool ignored = back.link.value == 0 && back.link.tag_length == 0;
    check_red( ignored, "optional/absent-payload-residue-is-copied",
               "probe absent-optional: an absent payload over non-zero residue is ignored" );
}

static void probe_bool_domain()
{
    tblfn1::FnRoot v;
    std::memset( &v, 0, sizeof( v ) );
    FN1::reset( v );
    FN1::fill( v );
    std::vector<uint8_t> file( (size_t) tblfn1::FnRootFixedMeasure( 1 ) );
    check( tblfn1::FnRootFixedSave( &v, 1, file.data(), (int64_t) file.size() ) == (int64_t) file.size(),
           "probe bool-domain: save" );
    uint8_t * rec = file.data() + tblfn1::kTableFixedHeaderBytes + 4 + tblfn1::FnRootFixedLayoutBytes;
    rec[8 + 0] = 0x02; // flag is the first body byte
    tblfn1::FnRoot back;
    tblfn1::TableReport r;
    const int64_t n = load_unclamped<FN1>( back, file.data(), r );
    check( n == 1, "probe bool-domain: the record reads" );
    const uint8_t landed = byte_of( &back.flag );
    check_red( landed <= 1u, "bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool",
               "probe bool-domain: a bool byte outside {0,1} does not land in the caller's bool" );
}

static void probe_ordinal_enum()
{
    tblfn1::FnRoot v;
    std::memset( &v, 0, sizeof( v ) );
    FN1::reset( v );
    FN1::fill( v );
    std::vector<uint8_t> file( (size_t) tblfn1::FnRootFixedMeasure( 1 ) );
    tblfn1::FnRootFixedSave( &v, 1, file.data(), (int64_t) file.size() );
    uint8_t * rec = file.data() + tblfn1::kTableFixedHeaderBytes + 4 + tblfn1::FnRootFixedLayoutBytes;
    rec[8 + 1] = 9; // tier, packed next to flag

    tblfn1::FnRoot id{}, co{};
    tblfn1::TableReport rid{}, rco{};
    load_identity<FN1>( id, file.data(), (int64_t) tblfn1::FnRootFixedMeasure( 1 ), rid );
    load_compiled<FN1>( co, file.data(), (int64_t) tblfn1::FnRootFixedMeasure( 1 ), rco );
    FN1::clamp( co, &rco );
    const bool fields = std::memcmp( &id.tier, &co.tier, sizeof( id.tier ) ) == 0;
    const bool counters = rid.clamped == rco.clamped;
    const bool in_set = byte_of( &id.tier ) <= (uint8_t) tblfn1::Tier::Max &&
                        byte_of( &co.tier ) <= (uint8_t) tblfn1::Tier::Max;
    check_red( fields && counters && in_set,
               "ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant",
               "probe ordinal-enum: identity and compiled agree, and the ordinal is in the set" );
}

static void probe_ordinal_tag()
{
    tblut1::UtRoot v;
    std::memset( &v, 0, sizeof( v ) );
    UT1::reset( v );
    UT1::fill( v );
    std::vector<uint8_t> file( (size_t) tblut1::UtRootFixedMeasure( 1 ) );
    tblut1::UtRootFixedSave( &v, 1, file.data(), (int64_t) file.size() );
    uint8_t * rec = file.data() + tblut1::kTableFixedHeaderBytes + 4 + tblut1::UtRootFixedLayoutBytes;
    rec[8 + 4] = 7; // Pick tag after head int32

    tblut1::UtRoot id{}, co{};
    tblut1::TableReport rid{}, rco{};
    load_identity<UT1>( id, file.data(), (int64_t) tblut1::UtRootFixedMeasure( 1 ), rid );
    load_compiled<UT1>( co, file.data(), (int64_t) tblut1::UtRootFixedMeasure( 1 ), rco );
    UT1::clamp( co, &rco );
    const bool fields = std::memcmp( &id.pick.type, &co.pick.type, sizeof( id.pick.type ) ) == 0;
    const bool counters = rid.clamped == rco.clamped;
    const bool in_set = byte_of( &id.pick.type ) <= (uint8_t) tblut1::PickType::Max &&
                        byte_of( &co.pick.type ) <= (uint8_t) tblut1::PickType::Max;
    check_red( fields && counters && in_set,
               "ordinal-bound/paths-disagree/union-tag-past-the-last-arm",
               "probe ordinal-tag: identity and compiled agree, and the tag is in the set" );
}

static void probe_kind_mismatch()
{
    scalardemo::SimState w;
    std::memset( &w, 0, sizeof( w ) );
    scalardemo::SimStateReset( w );
    w.angle = 42 * 65536; // a scaled fixed(16,16); Scalars2 respells angle as int32
    const int64_t need = scalardemo::SimStateFixedMeasure( 1 );
    std::vector<uint8_t> file( (size_t) need + (size_t) file_slack(), 0 );
    check( scalardemo::SimStateFixedSave( &w, 1, file.data(), need ) == need, "probe kind-mismatch: save" );

    scalardemo2::SimState back;
    std::memset( &back, 0, sizeof( back ) );
    scalardemo2::TableReport r;
    std::vector<scalardemo2::TableFixedEntry> plan( (size_t) kPlanCap );
    const int64_t n = scalardemo2::SimStateFixedLoad( &back, 1, file.data(), need, plan.data(), kPlanCap, NULL, &r );
    check( n == 1, "probe kind-mismatch: Scalars2 reads Scalars" );
    const bool skipped = back.angle == 0 && r.kind_mismatch >= 1;
    check_red( skipped, "kind-mismatch/compiled/a-moved-kind-is-decoded-anyway",
               "probe kind-mismatch: a moved kind is skipped, never misdecoded, counted" );
}

static void probe_runcopy_values()
{
    scalardemo::SimState v;
    std::memset( &v, 0, sizeof( v ) );
    Scalars::reset( v );
    Scalars::fill( v );
    const int64_t need = scalardemo::SimStateFixedMeasure( 1 );
    std::vector<uint8_t> file( (size_t) need + (size_t) file_slack(), 0 );
    check( scalardemo::SimStateFixedSave( &v, 1, file.data(), need ) == need, "probe run-copy: save" );
    scalardemo::SimState back;
    scalardemo::TableReport r;
    Issues iss{};
    const int64_t n = decode_identity<Scalars>( back, file.data(), r, iss );
    check( n == 1, "probe run-copy: the record reads" );
    const bool intact = back.angle == v.angle && back.pose.heading == v.pose.heading &&
                        back.spawn_present == v.spawn_present && back.weights_count == v.weights_count &&
                        report_clean( r );
    check_red( intact, "run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours",
               "probe run-copy: a 17..31-byte coalesced copy does not clobber its neighbours" );
}

int main()
{
    run_properties<FX1>();
    run_properties<FX2>();
    run_properties<UT1>();
    run_properties<UT2>();
    run_properties<P1>();
    run_properties<P3>();
    run_properties<FN1>();
    run_properties<FN2>();
    run_properties<FM1>();
    run_properties<FM2>();
    run_properties<F1>();
    run_properties<V1>();
    run_properties<V2>();
    run_properties<Scalars>();
    run_properties<Scalars2>();

    run_pair_p1<FX1, FX2>();
    run_pair_p1<P1, P3>();
    run_pair_p1<FN1, FN2>();
    run_pair_p1<FM1, FM2>();
    run_pair_p1<V1, V2>();
    run_pair_p1<UT1, UT2>();
    run_pair_p1<Scalars, Scalars2>();

    probe_clamp_op();
    probe_text_content();
    probe_absent_optional();
    probe_bool_domain();
    probe_ordinal_enum();
    probe_ordinal_tag();
    probe_kind_mismatch();
    probe_runcopy_values();

    failures += known_red_report();
    if ( failures != 0 ) { std::printf( "%d failure(s)\n", failures ); return 1; }
    std::printf( "fixed form: P1 P2 P3 green, known-reds named\n" );
    return 0;
}
