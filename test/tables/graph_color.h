// graph_color.h — the pointer corpus's native-mapped type (SPEC §4.2, Native
// type mapping), the cross-file case: Color is declared in Parts.schema and
// used from Graph.schema, so the storage there spells ::ColorMath and every
// generated call on it — the codecs AND the cooked form's Open walk — goes
// through a derived-to-base conversion. Derivation adds behavior, never
// layout, which is what keeps the storage relocatable.

#pragma once

#include "Parts.h"

#include <type_traits>

struct ColorMath : public graphdemo::Color
{
    ColorMath() { r = 0; g = 0; b = 0; }
    ColorMath( uint8_t red, uint8_t green, uint8_t blue ) { r = red; g = green; b = blue; }

    uint32_t packed() const { return uint32_t( r ) << 16 | uint32_t( g ) << 8 | uint32_t( b ); }
};

static_assert( sizeof( ColorMath ) == sizeof( graphdemo::Color ), "derivation must not change layout" );
static_assert( std::is_trivially_copyable<ColorMath>::value, "native storage stays relocatable" );
static_assert( std::is_standard_layout<ColorMath>::value, "native storage stays standard-layout" );
