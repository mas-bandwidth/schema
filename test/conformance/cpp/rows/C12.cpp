// C12: bool byte != 0 — the C++ conformance guard for schema matrix cell cpp/C12.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:337): a `bool` or a PRESENT FLAG lands as
// `byte != 0`, normalised to the language's own true. `0x02` is not a bool a
// reader stores verbatim (fix 2).
//
// This test constructs a valid fixed-form wire file for WeaponConfig (from
// TablesTable.h, part of the conformance includes), forges the `homing` bool
// byte to 0x02, loads it through the generated C++ fixed-form reader, and
// asserts that the bool is normalised to 1 (true), never 2.
//
// Negative control: change the forged byte back to 1 (the lawful value) and
// the assertion still passes; change the normalisation to a plain memcpy and
// the assertion FAILS (bool becomes 2, not 1).
//
// HOW THE BOOL BYTE IS READ: the member's byte is memcpy'd out as uint8_t and
// compared as an integer, so the fixture never loads a bool whose value would
// be undefined behaviour. That is how the case sits inside the sanitized
// target and still reports.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "TablesTable.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
    else       { std::printf( "PASS: %s\n", what ); }
}

// The one byte of a member, read as an INTEGER. Never a load of the member.
uint8_t byte_of( const void * p )
{
    uint8_t v = 0;
    std::memcpy( &v, p, 1 );
    return v;
}

// Build a valid one-record fixed-form file for WeaponConfig and return the
// offset into the file where the record BODY begins, so the caller can forge
// exactly the homing byte.
std::vector<uint8_t> one_weapon_record( size_t & body_at )
{
    tabledemo::WeaponConfig w;
    tabledemo::WeaponConfigReset( w );
    w.damage = 42.0f;
    w.speed = 100.0f;
    w.penetration = 5;
    w.channel = 3;
    w.homing = true;        // the bool we will forge
    w.effect.type = tabledemo::EffectType::Buff;

    const int64_t file_size = tabledemo::WeaponConfigFixedMeasure( 1 );
    std::vector<uint8_t> out( (size_t) file_size );
    check( tabledemo::WeaponConfigFixedSave( &w, 1, out.data(), file_size ) == file_size,
           "one_weapon_record: the lawful record saves" );

    // The record body offset: header (16) + layout-size word (4) + layout bytes + record hash (8).
    body_at = (size_t) tabledemo::kTableFixedHeaderBytes
            + 4u
            + (size_t) tabledemo::WeaponConfigFixedLayoutBytes
            + 8u;
    return out;
}

// The homing bool sits at src offset 16 in the record body (see the
// WeaponConfigFixedPlan entry: { 16u, 16u, 1u, ... kTableFixedBool, ... }).
constexpr size_t kHomingSrcOffset = 16;

// A FORGED BOOL BYTE OF 2: the record reads, and the bool lands as 1, not 2.
void bool_byte_two_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_weapon_record( body );

    // Forge the homing byte to 0x02 — a content fact, not a damage event.
    file[ body + kHomingSrcOffset ] = 2;

    tabledemo::WeaponConfig back;
    tabledemo::WeaponConfigReset( back );

    // Pre-poison the neighbouring fields so a normalisation that spilled would be caught.
    back.damage = 999.0f;
    back.penetration = 0x5A5A5A5A;

    tabledemo::TableReport r;
    std::vector<tabledemo::TableFixedEntry> plan( 256 );
    const int64_t n = tabledemo::WeaponConfigFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                        plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.malformed,
           "bool_byte_two: a bool byte of 2 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.homing ) == 1,
           "bool_byte_two: a bool byte of 2 lands as the language's own true (1), never verbatim (C12)" );
    check( back.damage == 42.0f,
           "bool_byte_two: the float beside the normalised byte lands from the wire, untouched" );
    check( back.penetration == 5,
           "bool_byte_two: the int beside the normalised byte lands from the wire, untouched" );
    check( r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0 && r.clamped == 0,
           "bool_byte_two: the four §4 counters stay exactly zero — a forged bool is not damage" );
}

// A BOOL BYTE OF 0: lands as false (0), the other side of the normalisation.
void bool_byte_zero_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_weapon_record( body );

    // Forge the homing byte to 0x00 — should land as false.
    file[ body + kHomingSrcOffset ] = 0;

    tabledemo::WeaponConfig back;
    tabledemo::WeaponConfigReset( back );

    tabledemo::TableReport r;
    std::vector<tabledemo::TableFixedEntry> plan( 256 );
    const int64_t n = tabledemo::WeaponConfigFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                        plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.malformed,
           "bool_byte_zero: a bool byte of 0 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.homing ) == 0,
           "bool_byte_zero: a bool byte of 0 lands as false (0) (C12)" );
}

// A BOOL BYTE OF 0xFF: the maximal non-zero byte — must still normalise to 1.
void bool_byte_ff_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_weapon_record( body );

    // Forge the homing byte to 0xFF — should still land as true (1).
    file[ body + kHomingSrcOffset ] = 0xFF;

    tabledemo::WeaponConfig back;
    tabledemo::WeaponConfigReset( back );

    tabledemo::TableReport r;
    std::vector<tabledemo::TableFixedEntry> plan( 256 );
    const int64_t n = tabledemo::WeaponConfigFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                        plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.malformed,
           "bool_byte_ff: a bool byte of 0xFF is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.homing ) == 1,
           "bool_byte_ff: a bool byte of 0xFF lands as the language's own true (1), never 0xFF (C12)" );
}

} // namespace

int main()
{
    bool_byte_two_case();
    bool_byte_zero_case();
    bool_byte_ff_case();

    if ( failures > 0 )
    {
        std::printf( "C12 cpp bool byte != 0: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "C12 cpp bool byte != 0: the bool byte normalised to byte != 0 on the identity plan\n" );
    return 0;
}
