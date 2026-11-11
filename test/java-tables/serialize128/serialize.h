// The fixed-form dump takes exactly one thing from serialize.h: the 128-bit
// storage type the generated fixed-table headers name for a uint128/int128
// field. It calls no serialize stream, bitpacker or byte routine -- the fixed
// form is self-contained -- so this header supplies only that type and lets
// the Java leg rebuild the reference byte oracle without a C++ serialize
// checkout. Every byte in the corpus is still written by the generated C++
// reference itself.
#pragma once

#include <cstdint>

namespace serialize
{
    using int128_t = __int128;
    using uint128_t = unsigned __int128;
}
