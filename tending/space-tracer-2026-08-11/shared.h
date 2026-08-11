// shared.h — one set of deterministic test values used by BOTH writers, so the
// two sides cannot drift. No dependence on either serialize library.
#pragma once
#include <stdint.h>

struct TestVals { uint16_t seq; int a, b, c; };
static const TestVals TESTS[] = { {0,0,0,0}, {0xFFFF,1000,1000,1000}, {0x1234,1,513,999} };

static const int BLOCK_LENS[] = { 0, 1, 13, 2000 };

struct SyncVals { uint64_t frame; uint16_t seq; };
static const SyncVals SYNCS[] = { {0,0}, {0xFFFFFFFFFFFFFFFFULL,0xFFFF}, {0x0123456789ABCDEFULL,0x8001} };

struct TsVals { double ts; int rtt, jitter; };
static const TsVals TSS[] = { {1.0,0,0}, {0.5,123,456}, {-0.25,2000000000,7} };

struct UcVals { int size; uint64_t hash; };
static const UcVals UCS[] = { {0,0}, {13,0xDEADBEEFCAFEF00DULL}, {300,1} };

static const uint64_t HANDLES[] = { 0, 0xFFFFFFFFFFFFFFFFULL, 0x00000001DEADBEEFULL };

static inline void fill_pattern( uint8_t * p, int n, uint64_t seed )
{
    uint64_t x = seed ? seed : 1;
    for ( int i = 0; i < n; i++ )
    {
        x = x * 6364136223846793005ULL + 1442695040888963407ULL;
        p[i] = (uint8_t) ( x >> 56 );
    }
}
