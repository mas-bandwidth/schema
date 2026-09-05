// THE VOCABULARY `was` CONTROL (docs/SPEC-TABLES.md §5). R1's config, read
// under R2. Built against the shipped R2 every renamed name lands: the enum
// value, the union arm, the keyed slot and the nested type's field. Built
// against an R2 whose four `was` attributes were stripped, each is a name
// this reader cannot find: `unknown` counts five (the value, the array element
// holding the same value, the slot, the arm and the field), the value and the
// union read None, the slot is dropped, and the field holds its declared
// default.
// The Makefile compiles this file twice and requires the two answers to
// differ exactly that way.
#include "R2Table.h"
#include <cstdio>
#include <cstring>

int main()
{
    FILE * f = fopen( "testdata/wire/tables/r1_cfg.bin", "rb" );
    if ( f == NULL ) { printf( "missing golden\n" ); return 2; }
    static uint8_t wire[4096];
    const int64_t n = (int64_t) fread( wire, 1, sizeof( wire ), f );
    fclose( f );
    tblr2::Cfg cfg;
    tblr2::TableReport report;
    if ( !tblr2::CfgLoad( cfg, wire, n, &report ) ) { printf( "load refused\n" ); return 2; }
    const char * grade = cfg.grade == tblr2::Grade::Argent ? "Argent" : cfg.grade == tblr2::Grade::None ? "None" : "other";
    const char * effect = cfg.effect.type == tblr2::EffectType::Shield ? "shield" : cfg.effect.type == tblr2::EffectType::None ? "None" : "other";
    const float charge = cfg.effect.type == tblr2::EffectType::Shield ? cfg.effect.shield.charge : 0.0f;
    printf( "unknown=%d kind_mismatch=%d malformed=%d grade=%s effect=%s charge=%g mult=%g tally_argent=%d\n",
            (int) report.unknown, (int) report.kind_mismatch, (int) report.malformed,
            grade, effect, charge, cfg.buff.mult, cfg.tally[tblr2::Grade::Argent] );
    return 0;
}
