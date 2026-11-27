// CELL cpp/BoolValuesValidData — "Boolean values: valid-data write/read acceptance"
// (docs/roadmap.sexp task bool-values/cpp/valid-data).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:453, §4.5 fix 2, 2026-11-03): a `bool`
// or a present flag byte of 0 or 1 lands as false or true; any other byte
// refuses the whole load `wire_damaged` (docs/rulings.json strict-only-read;
// the row once asserted normalisation to true, which that law retired).
// docs/FIXED-FORM-ALGORITHM.md:168 — `bool` is `0` or `1` on write.
// docs/FIXED-FORM-ALGORITHM.md:939 — a bool counts nothing (bill §12.12).
//
// WHAT THIS TEST DOES: writes a WeaponConfig record with `homing = true`,
// reads it back through WeaponConfigFixedLoad, and asserts the bool
// round-trips.  Writes `homing = false` and asserts it round-trips too.
// Then forges the bool byte on the wire to 0x02 (not 0 or 1), reads it
// back, and asserts the whole load refuses wire_damaged and lands nothing.
//
// PRODUCTION PATH: WeaponConfigFixedSave (§3.4 positional write) /
// WeaponConfigFixedLoad (§5.3 LOAD then plan run) in
// build/tables-generated/examples/TablesTable.h, the same header the leg's
// runner includes.  The production caller is tabledemo::WeaponConfigFixedLoad
// at TablesTable.h:12445, which runs WeaponConfigFixedPlan —
// PlanEntries[kTableFixedBool] at TablesTable.h:12354.
//
// THE NEGATIVE CONTROL: make the forged byte 0x01 and the refusal
// assertions go RED (a lawful true reads).

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "TablesTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) failures++;
}

int main()
{
    // ---- true writes 1, reads true ----------------------------------------
    {
        tabledemo::WeaponConfig w;
        tabledemo::WeaponConfigReset( w );
        w.damage = 21.0f; w.speed = 500.0f; w.penetration = 1;
        w.channel = 0; w.homing = true;
        w.effect.type = tabledemo::EffectType::None;

        std::vector<uint8_t> wire( (size_t) tabledemo::WeaponConfigFixedMeasure( 1 ) );
        check( tabledemo::WeaponConfigFixedSave( &w, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
               "BoolValuesValidData: save homing=true" );

        tabledemo::WeaponConfig r;
        tabledemo::WeaponConfigReset( r );
        tabledemo::TableReport report{};
        std::vector<tabledemo::TableFixedEntry> plan( 4096 );
        int64_t n = tabledemo::WeaponConfigFixedLoad( &r, 1, wire.data(),
            (int64_t) wire.size(), plan.data(), (int32_t) plan.size(), NULL, &report );
        check( n == 1, "BoolValuesValidData: load homing=true" );
        check( !report.refused && !report.damaged, "BoolValuesValidData: true is not a refusal" );
        check( r.homing == true, "BoolValuesValidData: homing=true round-trips" );
    }

    // ---- false writes 0, reads false --------------------------------------
    {
        tabledemo::WeaponConfig w;
        tabledemo::WeaponConfigReset( w );
        w.damage = 21.0f; w.speed = 500.0f; w.penetration = 1;
        w.channel = 0; w.homing = false;
        w.effect.type = tabledemo::EffectType::None;

        std::vector<uint8_t> wire( (size_t) tabledemo::WeaponConfigFixedMeasure( 1 ) );
        check( tabledemo::WeaponConfigFixedSave( &w, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
               "BoolValuesValidData: save homing=false" );

        tabledemo::WeaponConfig r;
        tabledemo::WeaponConfigReset( r );
        tabledemo::TableReport report{};
        std::vector<tabledemo::TableFixedEntry> plan( 4096 );
        int64_t n = tabledemo::WeaponConfigFixedLoad( &r, 1, wire.data(),
            (int64_t) wire.size(), plan.data(), (int32_t) plan.size(), NULL, &report );
        check( n == 1, "BoolValuesValidData: load homing=false" );
        check( !report.refused && !report.damaged, "BoolValuesValidData: false is not a refusal" );
        check( r.homing == false, "BoolValuesValidData: homing=false round-trips" );
    }

    // ---- forge 0x02 on the wire — the whole load refuses, nothing lands ----
    {
        tabledemo::WeaponConfig w;
        tabledemo::WeaponConfigReset( w );
        w.damage = 21.0f; w.speed = 500.0f; w.penetration = 1;
        w.channel = 0; w.homing = false;  // writer puts 0
        w.effect.type = tabledemo::EffectType::None;

        std::vector<uint8_t> wire( (size_t) tabledemo::WeaponConfigFixedMeasure( 1 ) );
        check( tabledemo::WeaponConfigFixedSave( &w, 1, wire.data(), (int64_t) wire.size() )
               == (int64_t) wire.size(),
               "BoolValuesValidData: save homing=false for forge" );

        // THE BODY is past the header + 4-byte layout length + layout + 8-byte record hash.
        // homing is at body offset 16 (TablesTable.h:12082: TableFixedPut8(b+16, ...)).
        uint8_t * body = wire.data() + tabledemo::kTableFixedHeaderBytes + 4
                         + tabledemo::WeaponConfigFixedLayoutBytes + 8;
        body[16] = 0x02;  // forge: not 0, not 1 — the law's boundary case

        tabledemo::WeaponConfig r;
        tabledemo::WeaponConfigReset( r );
        r.damage = 999.0f;  // poison: a refused load leaves the declared defaults, not this
        tabledemo::TableReport report{};
        std::vector<tabledemo::TableFixedEntry> plan( 4096 );
        int64_t n = tabledemo::WeaponConfigFixedLoad( &r, 1, wire.data(),
            (int64_t) wire.size(), plan.data(), (int32_t) plan.size(), NULL, &report );
        // strict-only-read: was "n == 1, not a refusal, normalised to true, no
        // counter"; a bool byte other than 0 or 1 now refuses the whole load.
        check( n < 0, "BoolValuesValidData: forged 0x02 refuses the load" );
        check( ( report.refused || report.damaged ) && report.refuse_reason == tabledemo::wire_damaged,
               "BoolValuesValidData: forged 0x02 refuses wire_damaged (FIXED-FORM-ALGORITHM.md:453)" );
        tabledemo::WeaponConfig fresh;
        tabledemo::WeaponConfigReset( fresh );
        check( r.damage == fresh.damage && r.speed == fresh.speed && r.homing == fresh.homing,
               "BoolValuesValidData: a refused load lands nothing (the row holds its declared defaults, R4)" );
    }

    if ( failures != 0 ) { std::printf( "BoolValuesValidData: %d assertion(s) failed\n", failures ); return 1; }
    std::printf( "BoolValuesValidData: Boolean values: valid-data write/read acceptance - green\n" );
    return 0;
}