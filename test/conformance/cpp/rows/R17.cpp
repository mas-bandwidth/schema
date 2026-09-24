// ROW cpp/R17 — the conformance leg's guard for the roadmap cell of that id
// (docs/roadmap.sexp, the row group "normalization: Bool and present-byte
// normalization"; audit schema#898 item R17), the law at
// docs/FIXED-FORM-ALGORITHM.md:949:
//
//     A `bool` or a present byte that is not `0` or `1` is normalised to the
//     language's own true and **counts nothing** (bill §12.12).
//
// with the op that keeps it at docs/FIXED-FORM-ALGORITHM.md:337 — a bool or a
// PRESENT FLAG "lands as `byte != 0`, normalised to the language's own true.
// `0x02` is not a bool a reader stores verbatim (fix 2)". The cpp port's own
// witness for the same law on a COMPILED plan and on a bool run is
// test/tables/fixedform_hostile_bytes.cpp (run by `make tables-fixedform`);
// the conformance leg's corpus carries no form-3 file at all, so this file is
// the leg's own assertion, built from the leg's own tabledemo unit the same
// way the driver reaches that code: the generated PackTable.cpp on the
// CONFORMANCE_INCLUDES path, the root the driver's codec table itself names
// (test/conformance/cpp/main.cpp, CODEC( "tabledemo", tabledemo, PackConfig )).
//
// THE PRODUCTION PATH, whole: tabledemo::PackConfigFixedLoad (the generated
// fixed-form reader) -> TableFixedRun (the one read loop every plan walks)
// -> TableFixedApply, case kTableFixedBool, `d[i] = (uint8_t)( s[i] != 0 )` —
// which lands 1 and moves no counter — -> PackConfigFixedClamp (the bounds
// pass; nothing here is out of range, so it moves nothing either). A port
// that landed the byte verbatim, or that counted the normalise as a clamp,
// fails this file by name.
//
// THE VECTOR. No corpus data exists for this cell, so the vector is one
// lawful PackConfig record saved through the generated writer itself, then
// ONE body byte overwritten per case. The offsets derive from the writer's
// own declared order in the generated PackTable.h — PackConfigFixedWriteBody
// lays the ships at 73 + i*98; ShipEntryFixedWriteBody puts gunner_present at
// +64 and the gunner payload at +65; GunnerSettingsFixedWriteBody puts
// reaction at +0..3 and tracking at +4:
//
//     the record body : kTableFixedHeaderBytes + 4 + PackConfigFixedLayoutBytes + 8
//     gunner_present  : body + 73 + 1*98 + 64      (ships[1], the present byte)
//     tracking        : body + 73 + 1*98 + 65 + 4  (ships[1].gunner, the bool)
//
// Each case first checks the byte it is about to forge held the writer's own
// 1, so a layout that moved fails the case by name instead of quietly
// forging a neighbour. The destination is poisoned before every load — the
// identity plan prefills nothing, so a field the plan does not land keeps its
// stain — and the forged member's byte is memcpy'd out and compared as an
// integer, never loaded as a bool: a reader that landed 0x02 verbatim would
// put 2 into a C++ bool, every load of which is undefined behaviour, and the
// test REPORTS that bug instead of being the crash.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "PackTable.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    std::printf( "%s: %s\n", ok ? "ok" : "FAIL", what );
    if ( !ok ) { failures++; }
}

// the member's byte, read as an integer and never as a bool load
uint8_t byte_of( const void * p )
{
    uint8_t v = 0;
    std::memcpy( &v, p, 1 );
    return v;
}

// the writer's declared order, the derivation the header comment spells out
constexpr int64_t kShipsAt    = 73;  // PackConfig body: ships at 73 + i*98
constexpr int64_t kShipBytes  = 98;
constexpr int64_t kPresentAt  = 64;  // ShipEntry body: gunner_present
constexpr int64_t kPayloadAt  = 65;  // ShipEntry body: the gunner payload
constexpr int64_t kTrackingAt = 4;   // GunnerSettings body: tracking

int64_t ship1_present_at( int64_t body )
{
    return body + kShipsAt + 1 * kShipBytes + kPresentAt;
}

int64_t ship1_tracking_at( int64_t body )
{
    return body + kShipsAt + 1 * kShipBytes + kPayloadAt + kTrackingAt;
}

const char kShipName[3][9]  = { "ship-000", "ship-001", "ship-002" };
const char kCallSign[3][6]  = { "GUN-A", "GUN-B", "GUN-C" };

// one lawful pack: every ship carries a present gunner whose tracking bool is
// true, and the one reserve carries a present gunner whose tracking bool is
// FALSE — so the read also sees a lawful 0 land 0 beside the forged byte
std::vector<uint8_t> one_lawful_pack( int64_t & body_at, const char * case_name )
{
    tabledemo::PackConfig v;
    tabledemo::PackConfigReset( v );
    v.version = 9;
    v.global.tick_rate = 120;
    v.global.difficulty = tabledemo::Difficulty::Hard;
    v.global.build_note_length = 6;
    std::memcpy( v.global.build_note, "lawful", 6 );
    v.global.spawn_delays[0] = 0.25f;
    v.global.spawn_delays[1] = 0.5f;
    v.global.spawn_delays[2] = 0.75f;
    for ( int i = 0; i < 3; ++i )
    {
        tabledemo::ShipEntry & s = v.ships.slots[i];
        s.display_name_length = 8;
        std::memcpy( s.display_name, kShipName[i], 8 );
        s.health = 100.5f + (float) i;
        s.mass = 1.5f + (float) i;
        s.hardpoints_count = 2;
        s.hardpoints[0] = 2 + i;
        s.hardpoints[1] = 4 + i;
        s.gunner_present = true;
        s.gunner.reaction = 0.25f + 0.5f * (float) i;
        s.gunner.tracking = true;
        s.gunner.callsign_length = 5;
        std::memcpy( s.gunner.callsign, kCallSign[i], 5 );
    }
    v.thresholds.slots[0] = 111;
    v.thresholds.slots[1] = 222;
    v.thresholds.slots[2] = 333;
    v.reserves_count = 1;
    v.reserves[0].display_name_length = 8;
    std::memcpy( v.reserves[0].display_name, "reserve0", 8 );
    v.reserves[0].health = 50.5f;
    v.reserves[0].mass = 5.5f;
    v.reserves[0].hardpoints_count = 2;
    v.reserves[0].hardpoints[0] = 1;
    v.reserves[0].hardpoints[1] = 2;
    v.reserves[0].gunner_present = true;
    v.reserves[0].gunner.reaction = 0.75f;
    v.reserves[0].gunner.tracking = false;
    v.reserves[0].gunner.callsign_length = 5;
    std::memcpy( v.reserves[0].gunner.callsign, "GUN-R", 5 );

    std::vector<uint8_t> out( (size_t) tabledemo::PackConfigFixedMeasure( 1 ) );
    char what[128];
    std::snprintf( what, sizeof( what ), "%s: the lawful record saves through the generated writer", case_name );
    check( tabledemo::PackConfigFixedSave( &v, 1, out.data(), (int64_t) out.size() ) == (int64_t) out.size(), what );
    body_at = (int64_t) tabledemo::kTableFixedHeaderBytes + 4 + (int64_t) tabledemo::PackConfigFixedLayoutBytes + 8;
    return out;
}

// the destination, stained first: the identity plan prefills nothing, so every
// value byte must land from the wire or keep its stain. The bools are poisoned
// with legal values only — an entry that does not run leaves false standing.
void poison( tabledemo::PackConfig & back )
{
    tabledemo::PackConfigReset( back );
    back.version = 0xDEADBEEFu;
    back.global.tick_rate = 0x5A5A5A5Au;
    back.global.difficulty = tabledemo::Difficulty::None;
    back.global.build_note_length = -5;
    std::memset( back.global.build_note, 0xA5, sizeof( back.global.build_note ) );
    back.global.spawn_delays[0] = -1.5f;
    back.global.spawn_delays[1] = -2.5f;
    back.global.spawn_delays[2] = -3.5f;
    for ( int i = 0; i < 3; ++i )
    {
        tabledemo::ShipEntry & s = back.ships.slots[i];
        s.display_name_length = -5;
        std::memset( s.display_name, 0xA5, sizeof( s.display_name ) );
        s.health = -9.5f;
        s.mass = -8.5f;
        s.hardpoints_count = -5;
        s.hardpoints[0] = s.hardpoints[1] = s.hardpoints[2] = s.hardpoints[3] = 0x5A5A5A5A;
        s.gunner_present = false;
        s.gunner.reaction = -7.5f;
        s.gunner.tracking = false;
        s.gunner.callsign_length = -5;
        std::memset( s.gunner.callsign, 0xA5, sizeof( s.gunner.callsign ) );
    }
    back.thresholds.slots[0] = back.thresholds.slots[1] = back.thresholds.slots[2] = -777;
    back.reserves_count = -5;
    for ( int i = 0; i < 3; ++i )
    {
        tabledemo::ShipEntry & s = back.reserves[i];
        s.display_name_length = -5;
        std::memset( s.display_name, 0xA5, sizeof( s.display_name ) );
        s.health = -9.5f;
        s.mass = -8.5f;
        s.hardpoints_count = -5;
        s.hardpoints[0] = s.hardpoints[1] = s.hardpoints[2] = s.hardpoints[3] = 0x5A5A5A5A;
        s.gunner_present = false;
        s.gunner.reaction = -7.5f;
        s.gunner.tracking = false;
        s.gunner.callsign_length = -5;
        std::memset( s.gunner.callsign, 0xA5, sizeof( s.gunner.callsign ) );
    }
}

// the load: the production reader, one record, the caller's plan scratch
int64_t load_pack( const std::vector<uint8_t> & file, tabledemo::PackConfig & back, tabledemo::TableReport & r )
{
    std::vector<tabledemo::TableFixedEntry> plan( 256 );
    poison( back );
    return tabledemo::PackConfigFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                           plan.data(), 256, NULL, &r );
}

// every field beside the normalised byte, from the wire: the header's scalars,
// all three ships (their presents and their untouched bools included), the
// thresholds, and the reserve — whose tracking bool is a lawful FALSE that
// must land 0 — plus the reserve slack's zero prefill landing false
bool neighbours_land( const tabledemo::PackConfig & back )
{
    bool near = true;
    near = near && back.version == 9;
    near = near && back.global.tick_rate == 120;
    near = near && back.global.difficulty == tabledemo::Difficulty::Hard;
    near = near && back.global.build_note_length == 6
        && std::memcmp( back.global.build_note, "lawful", 6 ) == 0;
    near = near && back.global.spawn_delays[0] == 0.25f
        && back.global.spawn_delays[1] == 0.5f
        && back.global.spawn_delays[2] == 0.75f;
    for ( int i = 0; i < 3; ++i )
    {
        const tabledemo::ShipEntry & s = back.ships.slots[i];
        near = near && s.display_name_length == 8
            && std::memcmp( s.display_name, kShipName[i], 8 ) == 0;
        near = near && s.health == 100.5f + (float) i && s.mass == 1.5f + (float) i;
        near = near && s.hardpoints_count == 2
            && s.hardpoints[0] == 2 + i && s.hardpoints[1] == 4 + i;
        near = near && byte_of( &s.gunner_present ) == 1;
        near = near && s.gunner.reaction == 0.25f + 0.5f * (float) i;
        near = near && s.gunner.callsign_length == 5
            && std::memcmp( s.gunner.callsign, kCallSign[i], 5 ) == 0;
        if ( i != 1 ) { near = near && byte_of( &s.gunner.tracking ) == 1; }
    }
    near = near && back.thresholds.slots[0] == 111
        && back.thresholds.slots[1] == 222
        && back.thresholds.slots[2] == 333;
    near = near && back.reserves_count == 1;
    near = near && back.reserves[0].display_name_length == 8
        && std::memcmp( back.reserves[0].display_name, "reserve0", 8 ) == 0;
    near = near && back.reserves[0].health == 50.5f && back.reserves[0].mass == 5.5f;
    near = near && back.reserves[0].hardpoints_count == 2
        && back.reserves[0].hardpoints[0] == 1 && back.reserves[0].hardpoints[1] == 2;
    near = near && byte_of( &back.reserves[0].gunner_present ) == 1;
    near = near && back.reserves[0].gunner.reaction == 0.75f;
    near = near && byte_of( &back.reserves[0].gunner.tracking ) == 0; // the lawful false
    near = near && back.reserves[0].gunner.callsign_length == 5
        && std::memcmp( back.reserves[0].gunner.callsign, "GUN-R", 5 ) == 0;
    near = near && byte_of( &back.reserves[1].gunner_present ) == 0; // slack's zero prefill
    near = near && byte_of( &back.reserves[2].gunner_present ) == 0;
    return near;
}

// the five counters of the §4 report: a normalised byte moves none of them
bool counts_nothing( const tabledemo::TableReport & r )
{
    return r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0
        && r.clamped == 0 && r.duplicate == 0;
}

// ---- the three cases --------------------------------------------------------

void bool_byte_case( uint8_t forged, const char * case_name )
{
    int64_t body = 0;
    std::vector<uint8_t> file = one_lawful_pack( body, case_name );
    char what[160];

    std::snprintf( what, sizeof( what ),
                   "%s: the byte at the derived offset held the writer's own 1 before the forge", case_name );
    check( file[(size_t) ship1_tracking_at( body )] == 1, what );
    file[(size_t) ship1_tracking_at( body )] = forged;

    tabledemo::PackConfig back;
    tabledemo::TableReport r;
    const int64_t n = load_pack( file, back, r );

    std::snprintf( what, sizeof( what ),
                   "%s: a bool byte of 0x%02x is a content fact, so the record still reads", case_name, (unsigned) forged );
    check( n == 1 && !r.refused && !r.malformed, what );

    std::snprintf( what, sizeof( what ),
                   "%s: a bool byte of 0x%02x lands as the language's own true (1), never verbatim (ALG §4.5:949)",
                   case_name, (unsigned) forged );
    check( byte_of( &back.ships.slots[1].gunner.tracking ) == 1, what );

    std::snprintf( what, sizeof( what ),
                   "%s: every field beside the normalised byte lands from the wire, untouched", case_name );
    check( neighbours_land( back ), what );

    std::snprintf( what, sizeof( what ),
                   "%s: unknown/kind_mismatch/widened/clamped/duplicate stay exactly 0 — the byte counts nothing",
                   case_name );
    check( counts_nothing( r ), what );
}

void present_byte_case()
{
    const char * case_name = "present";
    int64_t body = 0;
    std::vector<uint8_t> file = one_lawful_pack( body, case_name );

    check( file[(size_t) ship1_present_at( body )] == 1,
           "present: the byte at the derived offset held the writer's own 1 before the forge" );
    file[(size_t) ship1_present_at( body )] = 0x02;

    tabledemo::PackConfig back;
    tabledemo::TableReport r;
    const int64_t n = load_pack( file, back, r );

    check( n == 1 && !r.refused && !r.malformed,
           "present: a present byte of 0x02 is a content fact, so the record still reads" );
    check( byte_of( &back.ships.slots[1].gunner_present ) == 1,
           "present: an optional's present byte of 0x02 lands as the language's own true (1), never verbatim (ALG §4.5:949)" );
    check( neighbours_land( back ),
           "present: every field beside the normalised byte lands from the wire, untouched — payload included" );
    check( counts_nothing( r ),
           "present: unknown/kind_mismatch/widened/clamped/duplicate stay exactly 0 — the byte counts nothing" );
}

} // namespace

int main()
{
    bool_byte_case( 0x02, "bool-02" );
    bool_byte_case( 0xff, "bool-ff" );
    present_byte_case();
    if ( failures > 0 )
    {
        std::printf( "cpp/R17: %d FAILED — a bool or present byte not 0/1 must normalise and count nothing\n", failures );
        return 1;
    }
    std::printf( "cpp/R17: the bool and the present byte normalised to the language's own true and counted nothing\n" );
    return 0;
}
