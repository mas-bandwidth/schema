#pragma once

#include "Vectors.h"

// Your own type. It derives from the generated basis struct, so the layout is
// the schema's and the operators are yours.
struct GameVector2 : starlight::Vector2
{
    GameVector2 & operator+=( const GameVector2 & o ) { x += o.x; y += o.y; return *this; }
};
