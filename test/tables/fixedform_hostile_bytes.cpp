// THE TWO WIRE BYTES A FIXED READER REFUSES
// (docs/FIXED-FORM-ALGORITHM.md §4.5's last row; "Reference fixes pending" fix 2).
//
// A fixed record is a positional image and the read loop moves bytes. Two of
// those bytes are not integers: a `bool` and an OPTIONAL's PRESENT flag are
// `0` or `1` on the wire and nothing else. Since 2026-11-03 §4.5 says a byte
// other than `0` or `1` REFUSES `wire_damaged`: no writer emits one, and `0x02`
// is neither stored verbatim nor normalized.
//
// EVERY CASE HAS A LAWFUL TWIN. The same record with the byte at `0` or `1`
// reads, lands the byte as the language's own false or true, lands every
// neighbour from the wire over a pre-poisoned stain, and moves no counter; the
// forged record is refused by name. A reader that refused everything fails the
// twin, and one that normalised or stored the byte fails the refusal.
//
// THE LAST TWO CASES ARE THE WIDER WITNESS finding 3 asks for. `compiled_norm`
// saves with the OLD side of a versioning pair and reads with the NEW side, so
// the runtime COMPILES a plan from the old layout: a plan that is not the
// identity, and the path where the bool rule is easiest to lose. `bool_run`
// exercises the [N]bool run and the coalescer directly, with the non-bool
// bytes on BOTH sides of the run pre-poisoned, so a run that overran or a
// coalescer that folded bool into a copy would move a neighbour, and with a
// forged byte at EVERY position of the run in turn.
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
    std::memcpy( (void *) &v, p, 1 );
    return v;
}

// A LAWFUL READ MOVES NO COUNTER. The two live §4 counters stay exactly zero
// and the strict read names no refusal.
template <class Report>
void counters_are_zero( const Report & r, const char * what )
{
    check( r.unknown == 0 && r.widened == 0 && r.refuse_reason == decltype( r.refuse_reason )( 0 ), what );
}

// A FORGED BYTE IS REFUSED BY NAME: -1, refused, and the reason wire_damaged.
template <class Report, class Reason>
void refused_wire_damaged( int64_t n, const Report & r, Reason wire_damaged, const char * what )
{
    check( n == -1 && r.refused && r.refuse_reason == wire_damaged, what );
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

int64_t load_hostile( const std::vector<uint8_t> & file, hb::Hostile & back, hb::TableReport & r )
{
    hb::HostileReset( back );
    // PRE-POISON the neighbours: on a read that lands, they must land from the
    // wire, neither keeping this stain nor taking the bool op's byte.
    back.link.n = 0x5A5A5A5A;
    back.trail = 0x5A5A5A5Au;
    std::vector<hb::TableFixedEntry> plan( 256 );
    return hb::HostileFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 256, NULL, &r );
}

// a bool byte of 2, and its lawful twin of 1
void bool_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_record( body );
    {
        hb::Hostile back;
        hb::TableReport r;
        const int64_t n = load_hostile( file, back, r );
        check( n == 1 && !r.refused && !r.damaged, "bool_byte_two: the lawful twin (a bool byte of 1) reads" );
        check( byte_of( &back.flag ) == 1, "bool_byte_two: the lawful twin's bool lands as the language's own true (1)" );
        check( back.link_present && back.link.n == 11 && back.trail == 0xBBBBBBBBu,
               "bool_byte_two: the fields beside the bool land from the wire, untouched" );
        counters_are_zero( r, "bool_byte_two: the lawful twin moves no counter and names no refusal" );
    }
    file[body + 0] = 2;
    {
        hb::Hostile back;
        hb::TableReport r;
        const int64_t n = load_hostile( file, back, r );
        refused_wire_damaged( n, r, hb::TableRefuseReason::wire_damaged,
                              "bool_byte_two: a bool byte of 2 refuses wire_damaged, never lands (§4.5)" );
    }
}

// a present byte of 2, and its lawful twin of 1
void present_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_record( body );
    {
        hb::Hostile back;
        hb::TableReport r;
        const int64_t n = load_hostile( file, back, r );
        check( n == 1 && !r.refused && !r.damaged, "present_byte_two: the lawful twin (a present byte of 1) reads" );
        check( byte_of( &back.link_present ) == 1,
               "present_byte_two: the lawful twin's present byte lands as the language's own true (1)" );
        check( back.trail == 0xBBBBBBBBu && back.link.n == 11,
               "present_byte_two: the payload and the trail beside it land from the wire, untouched" );
        counters_are_zero( r, "present_byte_two: the lawful twin moves no counter and names no refusal" );
    }
    file[body + 1] = 2;
    {
        hb::Hostile back;
        hb::TableReport r;
        const int64_t n = load_hostile( file, back, r );
        refused_wire_damaged( n, r, hb::TableRefuseReason::wire_damaged,
                              "present_byte_two: an optional's present byte of 2 refuses wire_damaged (§4.5)" );
    }
}

// ---- THE PLAN THAT IS NOT THE IDENTITY -------------------------------------
//
// VOLD_hostile_bool and VNEW_hostile_bool share one table name, so a VNEW
// reader given a VOLD record finds a hash that is not its identity and COMPILES
// a plan from the old layout (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3). Its
// bool, its present byte and its bool run must all still be held to 0 or 1.
// The body offsets are the pair's writer's declared order, shared by both
// sides: 0 flag, 1 link_present, 2..5 link.n, 6..9 lead, 10..17 flags,
// 18..21 trail, 22..25 extra (NEW only).
constexpr size_t kPairFlag = 0;
constexpr size_t kPairPresent = 1;
constexpr size_t kPairFlags = 10;

// The lawful run is NOT the writer's own alternation, so a reader that kept
// the saved image instead of landing the wire would be caught.
const uint8_t kLawful[8] = { 1, 1, 0, 1, 0, 0, 1, 0 };
// The forged bytes, one per position of the run in turn.
const uint8_t kForged[8] = { 2, 0xFF, 3, 0x80, 4, 2, 0x7F, 2 };

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

int64_t load_pair( const std::vector<uint8_t> & file, vnew_hostile_bool::Hbb & back, vnew_hostile_bool::TableReport & r )
{
    vnew_hostile_bool::HbbReset( back );
    back.lead = 0x5A5A5A5Au;  // pre-poison the neighbour before the run
    back.trail = 0x5A5A5A5Au; // and the one after it
    std::vector<vnew_hostile_bool::TableFixedEntry> plan( 256 );
    return vnew_hostile_bool::HbbFixedLoad( &back, 1, file.data(), (int64_t) file.size(), plan.data(), 256, NULL, &r );
}

bool run_lands( const vnew_hostile_bool::Hbb & back )
{
    for ( size_t i = 0; i < 8; ++i )
    {
        if ( byte_of( &back.flags[i] ) != kLawful[i] ) { return false; }
    }
    return true;
}

// Every forged position of the run, one at a time, over `file`'s lawful run.
bool every_run_forgery_refuses( const std::vector<uint8_t> & file, size_t body )
{
    bool all = true;
    for ( size_t i = 0; i < 8; ++i )
    {
        std::vector<uint8_t> forged = file;
        forged[body + kPairFlags + i] = kForged[i];
        vnew_hostile_bool::Hbb back;
        vnew_hostile_bool::TableReport r;
        const int64_t n = load_pair( forged, back, r );
        if ( !( n == -1 && r.refused && r.refuse_reason == vnew_hostile_bool::TableRefuseReason::wire_damaged ) ) { all = false; }
    }
    return all;
}

// A COMPILED PLAN, from the OLD writer's layout. The lawful twin lands every
// byte and defaults the field the old writer does not name; every forged byte
// refuses.
void compiled_plan_refuses_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_old_pair_record( body );
    for ( size_t i = 0; i < 8; ++i ) { file[body + kPairFlags + i] = kLawful[i]; }
    {
        vnew_hostile_bool::Hbb back;
        vnew_hostile_bool::TableReport r;
        const int64_t n = load_pair( file, back, r );
        check( n == 1 && !r.refused && !r.damaged,
               "compiled_norm: an OLD lawful record read through a plan compiled from its layout reads" );
        check( byte_of( &back.flag ) == 1 && byte_of( &back.link_present ) == 1,
               "compiled_norm: the bool and the present byte land as true (1) on the COMPILED plan" );
        check( run_lands( back ), "compiled_norm: every byte of the compiled plan's bool run lands from the wire" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "compiled_norm: the non-bool bytes beside the run land from the wire, untouched" );
        check( back.link.n == 11, "compiled_norm: the payload beside the present byte lands" );
        check( back.extra == 9, "compiled_norm: the field the OLD writer never had takes its declared default" );
        counters_are_zero( r, "compiled_norm: the lawful twin moves no counter and names no refusal" );
    }
    {
        std::vector<uint8_t> forged = file;
        forged[body + kPairFlag] = 2;
        vnew_hostile_bool::Hbb back;
        vnew_hostile_bool::TableReport r;
        refused_wire_damaged( load_pair( forged, back, r ), r, vnew_hostile_bool::TableRefuseReason::wire_damaged,
                              "compiled_norm: a forged bool refuses wire_damaged on the COMPILED plan" );
    }
    {
        std::vector<uint8_t> forged = file;
        forged[body + kPairPresent] = 2;
        vnew_hostile_bool::Hbb back;
        vnew_hostile_bool::TableReport r;
        refused_wire_damaged( load_pair( forged, back, r ), r, vnew_hostile_bool::TableRefuseReason::wire_damaged,
                              "compiled_norm: a forged present byte refuses wire_damaged on the COMPILED plan" );
    }
    check( every_run_forgery_refuses( file, body ),
           "compiled_norm: a forged byte at every position of the compiled plan's bool run refuses wire_damaged" );
}

// THE BOOL-RUN AND COALESCER PATH, on the identity plan. `flags` is ONE run
// between two non-bool fields; both sentinels are stained first, so a run that
// overran or a coalescer that merged bool into a copy would leave the stain and
// be caught, and a coalescer that dropped the run from the bool scan would let
// a forged position land.
void bool_run_sentinels_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_new_pair_record( body );
    for ( size_t i = 0; i < 8; ++i ) { file[body + kPairFlags + i] = kLawful[i]; }
    {
        vnew_hostile_bool::Hbb back;
        vnew_hostile_bool::TableReport r;
        const int64_t n = load_pair( file, back, r );
        check( n == 1 && !r.refused && !r.damaged, "bool_run: the lawful record reads" );
        check( run_lands( back ), "bool_run: the [N]bool run lands every byte from the wire" );
        check( back.lead == 0xAAAAAAAAu && back.trail == 0xBBBBBBBBu,
               "bool_run: the non-bool sentinels ADJACENT to the run land from the wire, untouched" );
        counters_are_zero( r, "bool_run: the lawful record moves no counter and names no refusal" );
    }
    check( every_run_forgery_refuses( file, body ),
           "bool_run: a forged byte at every position of the identity plan's bool run refuses wire_damaged" );
}

} // namespace

int main()
{
    bool_byte_two_case();
    present_byte_two_case();
    compiled_plan_refuses_case();
    bool_run_sentinels_case();
    if ( failures > 0 )
    {
        std::printf( "fixed form hostile bytes (C++): %d FAILED\n", failures );
        return 1;
    }
    std::printf( "fixed form hostile bytes (C++): a bool, a present byte and every byte of a bool run refuse wire_damaged past 0 or 1 on the identity and compiled plans, and every lawful twin lands\n" );
    return 0;
}
