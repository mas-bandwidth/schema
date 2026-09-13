// THE TWO WIRE BYTES A FIXED READER NORMALISES
// (docs/FIXED-FORM-ALGORITHM.md §4.5's last row; "Reference fixes pending" fix 2).
//
// A fixed record is a positional image and the read loop moves bytes. Two of
// those bytes are not integers: a `bool` and an OPTIONAL's PRESENT flag are
// `0` or `1` on the wire and nothing else, and §4.5 says a reader lands them
// as `byte != 0`, normalised to the language's own true — "0x02 is not a bool
// a reader stores verbatim".
//
// THE FIX IS IN. The C++ reference lands both through kTableFixedBool, so a
// forged 0x02 becomes a bool holding 1 (true) and never 2, on the identity plan
// and on a plan compiled from another writer's layout. The first two cases below
// are ordinary checks now; each was the RED witness that waited on fix 2, and
// each names the neighbouring bytes it pre-poisons so a normalise that spilled
// would be caught. Each also holds the four §4 counters at exactly zero, because
// a forged bool is a CONTENT fact and not a damage event.
//
// THE LAST TWO CASES ARE THE WIDER WITNESS finding 3 asks for. `compiled_norm`
// saves with the OLD side of a versioning pair and reads with the NEW side, so
// the runtime COMPILES a plan from the old layout: a plan that is not the
// identity, and the path where the bool normalise is easiest to lose. `bool_run`
// exercises the new [N]bool run and the coalescer directly, with the non-bool
// bytes on BOTH sides of the run pre-poisoned, so a run that overran or a
// coalescer that folded bool into a copy would move a neighbour.
//
// HOW THIS FILE READS THE BYTE. It `memcpy`s the member's byte out and compares
// the INTEGER, so the fixture never LOADS a bool whose value would be UB. That
// is the whole of the trick, and it is how the case could sit inside the
// sanitized target and still report.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "HBTable.h"
#include "VOLD_hostile_boolTable.h"
#include "VNEW_hostile_boolTable.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
}

// The one byte of a member, read as an INTEGER. Never a load of the member.
uint8_t byte_of( const void * p )
{
    uint8_t v = 0;
    std::memcpy( &v, p, 1 );
    return v;
}

// A FORGED BOOL IS NOT DAMAGE. The four §4 counters stay exactly zero on both
// the identity plan and a compiled one; the read still returns the record.
template <class Report>
void counters_are_zero( const Report & r, const char * what )
{
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0, what );
}

// One lawful record, saved, with `at` pointing at the record BODY so a case can
// forge exactly one byte of it. The body offsets are the writer's, declared
// order: 0 flag, 1 link_present, 2..5 the payload, 6..9 trail.
std::vector<uint8_t> one_record( size_t & body_at )
{
    hb::Hostile v;
    hb::HostileReset( v );
    v.flag = true;
    v.link_present = true;
    v.link.n = 11;
    v.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> out( (size_t) hb::HostileFixedMeasure( 1 ) );
    check( hb::HostileFixedSave( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(),
           "hostile_bytes: the lawful record saves" );
    body_at = (size_t) hb::kTableFixedHeaderBytes + 4 + (size_t) hb::HostileFixedLayoutBytes + 8;
    return out;
}

// a bool byte of 2
void bool_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_record( body );
    file[ body + 0 ] = 2;

    hb::Hostile back;
    hb::HostileReset( back );
    // PRE-POISON the neighbours: the bytes beside the normalised one must land
    // from the wire, neither keeping this stain nor taking the bool op's byte.
    back.link.n = 0x5A5A5A5A;
    back.trail = 0x5A5A5A5Au;
    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "bool_byte_two: a bool byte of 2 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.flag ) == 1,
           "bool_byte_two: a bool byte of 2 lands as the language's own true (1), never verbatim (§4.5)" );
    check( back.link_present && back.link.n == 11 && back.trail == 0xBBBBBBBBu,
           "bool_byte_two: the fields beside the normalised byte land from the wire, untouched" );
    counters_are_zero( r,
           "bool_byte_two: unknown/kind_mismatch/widened/clamped stay exactly 0" );
}

// a present byte of 2
void present_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_record( body );
    file[ body + 1 ] = 2;

    hb::Hostile back;
    hb::HostileReset( back );
    back.link.n = 0x5A5A5A5A;
    back.trail = 0x5A5A5A5Au;
    hb::TableReport r;
    std::vector<hb::TableFixedEntry> plan( 256 );
    const int64_t n = hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                            plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "present_byte_two: a present byte of 2 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.link_present ) == 1,
           "present_byte_two: an optional's present byte of 2 lands as the language's own true (1) (§4.5)" );
    check( back.trail == 0xBBBBBBBBu && back.link.n == 11,
           "present_byte_two: the payload and the trail beside it land from the wire, untouched" );
    counters_are_zero( r,
           "present_byte_two: unknown/kind_mismatch/widened/clamped stay exactly 0" );
}

// ---- THE PLAN THAT IS NOT THE IDENTITY -------------------------------------
//
// VOLD_hostile_bool and VNEW_hostile_bool share one table name, so a VNEW
// reader given a VOLD record finds a hash that is not its identity and COMPILES
// a plan from the old layout (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3). Its
// bool, its present byte and its bool run must all still normalise. The body
// offsets are the pair's writer's declared order, shared by both sides:
// 0 flag, 1 link_present, 2..5 link.n, 6..9 lead, 10..17 flags, 18..21 trail,
// 22..25 extra (NEW only).
constexpr size_t kPairFlag    = 0;
constexpr size_t kPairPresent = 1;
constexpr size_t kPairFlags   = 10;

const uint8_t kStorm[8] = { 0, 2, 0xFF, 1, 3, 0x80, 0, 4 };

std::vector<uint8_t> one_old_pair_record( size_t & body_at )
{
    vold_hostile_bool::Hbb v;
    vold_hostile_bool::HbbReset( v );
    v.flag = true;
    v.link_present = true;
    v.link.n = 11;
    v.lead = 0xAAAAAAAAu;
    for ( int i = 0; i < 8; ++i ) { v.flags[i] = ( i % 2 ) != 0; }
    v.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> out( (size_t) vold_hostile_bool::HbbFixedMeasure( 1 ) );
    check( vold_hostile_bool::HbbFixedSave( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(),
           "compiled_norm: the OLD pair record saves" );
    body_at = (size_t) vold_hostile_bool::kTableFixedHeaderBytes + 4 + (size_t) vold_hostile_bool::HbbFixedLayoutBytes + 8;
    return out;
}

std::vector<uint8_t> one_new_pair_record( size_t & body_at )
{
    vnew_hostile_bool::Hbb v;
    vnew_hostile_bool::HbbReset( v );
    v.flag = true;
    v.link_present = true;
    v.link.n = 11;
    v.lead = 0xAAAAAAAAu;
    for ( int i = 0; i < 8; ++i ) { v.flags[i] = ( i % 2 ) != 0; }
    v.trail = 0xBBBBBBBBu;
    std::vector<uint8_t> out( (size_t) vnew_hostile_bool::HbbFixedMeasure( 1 ) );
    check( vnew_hostile_bool::HbbFixedSave( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(),
           "bool_run: the NEW pair record saves" );
    body_at = (size_t) vnew_hostile_bool::kTableFixedHeaderBytes + 4 + (size_t) vnew_hostile_bool::HbbFixedLayoutBytes + 8;
    return out;
}

// A COMPILED PLAN, from the OLD writer's layout. Every forged byte normalises;
// the counters do not move; the field the old writer does not name defaults.
void compiled_plan_normalises_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_old_pair_record( body );
    file[ body + kPairFlag ] = 2;
    file[ body + kPairPresent ] = 2;
    for ( size_t i = 0; i < 8; ++i ) { file[ body + kPairFlags + i ] = kStorm[i]; }

    vnew_hostile_bool::Hbb back;
    vnew_hostile_bool::HbbReset( back );
    back.lead = 0x5A5A5A5Au;   // pre-poison the neighbour before the run
    back.trail = 0x5A5A5A5Au;  // and the one after it
    vnew_hostile_bool::TableReport r;
    std::vector<vnew_hostile_bool::TableFixedEntry> plan( 256 );
    const int64_t n = vnew_hostile_bool::HbbFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                       plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed,
           "compiled_norm: an OLD record read through a plan compiled from its layout still reads" );
    check( byte_of( &back.flag ) == 1,
           "compiled_norm: a forged bool lands as true (1) on the COMPILED plan, never verbatim" );
    check( byte_of( &back.link_present ) == 1,
           "compiled_norm: a forged present byte lands as true (1) on the COMPILED plan" );
    bool run_ok = true;
    for ( size_t i = 0; i < 8; ++i ) { if ( byte_of( &back.flags[i] ) > 1 ) { run_ok = false; } }
    check( run_ok, "compiled_norm: every byte of the compiled plan's bool run lands as 0 or 1" );
    check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
           "compiled_norm: the non-bool bytes beside the run land from the wire, untouched" );
    check( back.link.n == 11, "compiled_norm: the payload beside the present byte lands" );
    check( back.extra == 9,
           "compiled_norm: the field the OLD writer never had takes its declared default" );
    counters_are_zero( r,
           "compiled_norm: unknown/kind_mismatch/widened/clamped stay exactly 0" );
}

// THE BOOL-RUN AND COALESCER PATH, on the identity plan. `flags` is ONE
// normalised run between two non-bool fields; both sentinels are stained first,
// so a run that overran or a coalescer that merged bool into a copy would leave
// the stain and be caught.
void bool_run_sentinels_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_new_pair_record( body );
    for ( size_t i = 0; i < 8; ++i ) { file[ body + kPairFlags + i ] = kStorm[i]; }

    vnew_hostile_bool::Hbb back;
    vnew_hostile_bool::HbbReset( back );
    back.lead = 0x5A5A5A5Au;
    back.trail = 0x5A5A5A5Au;
    vnew_hostile_bool::TableReport r;
    std::vector<vnew_hostile_bool::TableFixedEntry> plan( 256 );
    const int64_t n = vnew_hostile_bool::HbbFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                       plan.data(), 256, NULL, &r );
    check( n == 1 && !r.refused && !r.malformed, "bool_run: the record reads" );
    bool run_ok = true;
    for ( size_t i = 0; i < 8; ++i ) { if ( byte_of( &back.flags[i] ) > 1 ) { run_ok = false; } }
    check( run_ok, "bool_run: the normalised [N]bool run lands every byte as 0 or 1" );
    check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
           "bool_run: the non-bool sentinels ADJACENT to the run land from the wire, untouched" );
    counters_are_zero( r,
           "bool_run: unknown/kind_mismatch/widened/clamped stay exactly 0" );
}

} // namespace

int main()
{
    bool_byte_two_case();
    present_byte_two_case();
    compiled_plan_normalises_case();
    bool_run_sentinels_case();
    if ( failures > 0 )
    {
        std::printf( "fixed form hostile bytes (C++): %d FAILED\n", failures );
        return 1;
    }
    std::printf( "fixed form hostile bytes (C++): the bool and the present byte normalised on the identity and compiled plans, and the bool run guarded\n" );
    return 0;
}
