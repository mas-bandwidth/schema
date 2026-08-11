// vec_math.h — the corpus's native-mapped math type (SPEC §4.2, Native type
// mapping). The pattern under test: a hand math type DERIVES from the
// generated basis struct (data members generated, operations hand-written),
// and cpp_native = VecMath / cpp_include = "vec_math.h" on the Vec3
// declaration makes every generated C++ storage site speak ::VecMath.
// The static_asserts pin what makes this sound: derivation adds behavior,
// never layout — storage stays relocatable.

#pragma once

#include "Types.h"

#include <type_traits>

struct VecMath : public example::Vec3
{
    VecMath()
    {
        x = 0.0;
        y = 0.0;
        z = 0.0;
    }

    VecMath( double _x, double _y, double _z )
    {
        x = _x;
        y = _y;
        z = _z;
    }

    VecMath operator + ( const VecMath & other ) const
    {
        return VecMath( x + other.x, y + other.y, z + other.z );
    }

    double length_squared() const
    {
        return x * x + y * y + z * z;
    }
};

static_assert( sizeof( VecMath ) == sizeof( example::Vec3 ), "derivation must not change layout" );
static_assert( std::is_trivially_copyable<VecMath>::value, "native math type must stay relocatable" );
static_assert( std::is_standard_layout<VecMath>::value, "native math type must stay standard-layout" );
