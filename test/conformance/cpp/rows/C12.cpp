// C12: a bool byte other than 0 or 1 — the C++ conformance guard for schema matrix cell cpp/C12.
//
// Law (docs/FIXED-FORM-ALGORITHM.md:453, §4.5 fix 2, 2026-11-03): a `bool` or a
// PRESENT FLAG byte of 0 or 1 lands as false or true; any other byte refuses the
// whole load `wire_damaged`. 0x02 is neither stored verbatim nor normalised. The
// row once asserted normalisation to 1; docs/rulings.json strict-only-read
// retired that, and test/conformance/go/rows/C12_test.go holds the same law.
//
// This test constructs a valid fixed-form wire file for WeaponConfig (from
// TablesTable.h, part of the conformance includes), forges the `homing` bool
// byte, loads it through the generated C++ fixed-form reader, and asserts 0 and
// 1 land while 0x02 and 0xFF refuse and land nothing.
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

// The law moved (docs/FIXED-FORM-ALGORITHM.md:453 and §4.5 fix 2, 2026-11-03;
// docs/rulings.json strict-only-read): a bool byte of 0 or 1 lands as false or
// true, and ANY OTHER BYTE refuses the whole load `wire_damaged`, checked over
// every record before any lands. 0x02 is neither stored verbatim nor
// normalised. This row agrees with test/conformance/go/rows/C12_test.go.
void refused_bool_case( uint8_t forged, const char * refuses, const char * nothing_lands )
{
    size_t body = 0;
    std::vector<uint8_t> file = one_weapon_record( body );

    file[ body + kHomingSrcOffset ] = forged;

    tabledemo::WeaponConfig back;
    tabledemo::WeaponConfigReset( back );

    // Pre-poison fields so a load that left them as they were would be caught.
    back.damage = 999.0f;
    back.penetration = 0x5A5A5A5A;

    tabledemo::TableReport r;
    std::vector<tabledemo::TableFixedEntry> plan( 256 );
    const int64_t n = tabledemo::WeaponConfigFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                        plan.data(), 256, NULL, &r );

    // strict-only-read: was "n == 1 && !refused && !damaged, the bool normalised
    // to 1"; a bool byte other than 0 or 1 now refuses the whole load.
    check( n < 0 && ( r.refused || r.damaged ) && r.refuse_reason == tabledemo::wire_damaged, refuses );
    // a refused load lands nothing: every row is left at its declared defaults
    // (the generated FixedLoad's R4 rule), never the wire's values or the poison
    tabledemo::WeaponConfig fresh;
    tabledemo::WeaponConfigReset( fresh );
    check( back.damage == fresh.damage && back.penetration == fresh.penetration
               && back.speed == fresh.speed && byte_of( &back.homing ) == byte_of( &fresh.homing ), nothing_lands );
}

// A FORGED BOOL BYTE OF 2: the whole load refuses wire_damaged, nothing lands.
void bool_byte_two_case()
{
    refused_bool_case( 2,
        "bool_byte_two: a bool byte of 2 refuses the whole load wire_damaged (C12)",
        "bool_byte_two: a refused load lands nothing" );
}

// A BOOL BYTE OF 1: the lawful true lands as 1, the clean read.
void bool_byte_one_case()
{
    size_t body = 0;
    std::vector<uint8_t> file = one_weapon_record( body );
    file[ body + kHomingSrcOffset ] = 1;

    tabledemo::WeaponConfig back;
    tabledemo::WeaponConfigReset( back );

    tabledemo::TableReport r;
    std::vector<tabledemo::TableFixedEntry> plan( 256 );
    const int64_t n = tabledemo::WeaponConfigFixedLoad( &back, 1, file.data(), (int64_t) file.size(),
                                                        plan.data(), 256, NULL, &r );

    check( n == 1 && !r.refused && !r.damaged && r.unknown == 0 && r.widened == 0,
           "bool_byte_one: a bool byte of 1 reads clean" );
    check( byte_of( &back.homing ) == 1,
           "bool_byte_one: a bool byte of 1 lands as true (1) (C12)" );
    check( back.damage == 42.0f && back.penetration == 5,
           "bool_byte_one: the fields beside the bool land from the wire" );
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

    check( n == 1 && !r.refused && !r.damaged,
           "bool_byte_zero: a bool byte of 0 is a CONTENT fact, so the record still reads" );
    check( byte_of( &back.homing ) == 0,
           "bool_byte_zero: a bool byte of 0 lands as false (0) (C12)" );
}

// A BOOL BYTE OF 0xFF: the maximal byte — refuses the whole load like 0x02.
void bool_byte_ff_case()
{
    refused_bool_case( 0xFF,
        "bool_byte_ff: a bool byte of 0xFF refuses the whole load wire_damaged (C12)",
        "bool_byte_ff: a refused load lands nothing" );
}

} // namespace

int main()
{
    bool_byte_two_case();
    bool_byte_one_case();
    bool_byte_zero_case();
    bool_byte_ff_case();

    if ( failures > 0 )
    {
        std::printf( "C12 cpp bool byte not 0 or 1: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "C12 cpp bool byte not 0 or 1: refused wire_damaged, 0 and 1 land\n" );
    return 0;
}
