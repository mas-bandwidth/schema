/* c/SignedFixedPointValidData — signed fixed-point fields: valid-data write/read
 * acceptance (audit schema#898, matrix schema#876, roadmap signed-fixed-point/c/valid-data).
 *
 * THE LAW (docs/FIXED-FORM-ALGORITHM.md §3.4, record, fixed(I,F)):
 *   A signed fixed-point field fixed(I,F) stores an I-bit signed integer whose
 *   value is interpreted as that integer divided by 2^F. The wire writes the raw
 *   storage bits; the reader decodes and clamps to the declared range. Valid data
 *   within range must round-trip byte-for-byte: save then load recovers every
 *   stored bit exactly.
 *
 * THE PRODUCTION PATH THIS TEST DRIVES: the scalardemo unit's SimState fixed table
 * (tables/scalars/Scalars.schema), whose signed fixed-point fields are:
 *   tilt     fixed(4,4)    int8_t   Q4.4   range [-8, 7]
 *   angle    fixed(16,16)  int32_t  Q16.16 range [-180, 180]
 *   position fixed(48,16)  int64_t  Q48.16 range [-30000, 30000]
 *   reach    fixed(112,16) int128   Q112.16 range [-1000000, 1000000]
 *   ticks    fixed(32,0)   int32_t  Q32.0  range [0, 1000000]
 *   scale    fixed(16,16)  int32_t  Q16.16 range [-8, 8], default 1.0
 *   samples  [3]fixed(16,16) int32_t Q16.16 range [-8, 8]
 * The save/load functions are sim_state_save() and sim_state_load()
 * (build/tables-generated-c/scalars/ScalarsTable.h).
 *
 * Standalone: depends on no other rows/ file, edits no shared file.
 * Run (flags per make/c.mk's build/conformance-c, C_CONFORMANCE_INCLUDES):
 *   cc -std=c11 -Wall <the -I flags> test/conformance/c/rows/SignedFixedPointValidData.c \
 *      build/tables-generated-c/scalars/ScalarsTable.c serialize.c/serialize.c \
 *      -o build/rows-c-SignedFixedPointValidData -lm \
 *   && ./build/rows-c-SignedFixedPointValidData
 */

#include <stddef.h>
#include <stdio.h>
#include <string.h>

#include "ScalarsTable.h"

static int failures;

static void check(int ok, const char *what)
{
    printf("%s: %s\n", ok ? "PASS" : "FAIL", what);
    if (!ok) { failures = 1; }
}

int main(void)
{
    SimState original, loaded;
    TableReport report;
    uint8_t buffer[4096];
    int64_t n;

    /* Build a SimState with VALID (non-default, non-boundary) signed fixed-point
       values. Each value is the raw fixed-point encoding: value * 2^F. */

    sim_state_reset(&original);

    /* tilt: fixed(4,4), Q4.4. Raw value 3.5 = 3.5 * 16 = 56.
       Within range [-128, 112] (i.e. -8.0 to 7.0). */
    original.tilt = 56; /* 3.5 in Q4.4 */

    /* angle: fixed(16,16), Q16.16. Raw value 45.5 = 45.5 * 65536 = 2981888.
       Within range [-180*65536, 180*65536]. */
    original.angle = 2981888; /* 45.5 in Q16.16 */

    /* position: fixed(48,16), Q48.16. Raw value 1234.75 = 1234.75 * 65536 = 80920576.
       Within range [-30000*65536, 30000*65536]. */
    original.position = 80920576; /* 1234.75 in Q48.16 */

    /* reach: fixed(112,16), Q112.16. Raw value 50000.25 = 50000.25 * 65536.
       hi = 0 (positive, small enough for 64 bits), lo = 3276816384. */
    original.reach = serialize_int128_make(0ull, 3276816384ull); /* 50000.25 in Q112.16 */

    /* ticks: fixed(32,0), Q32.0 (plain integer). Value 500000.
       Within range [0, 1000000]. */
    original.ticks = 500000;

    /* scale: fixed(16,16), Q16.16. Default is 1.0 = 65536.
       Use 2.5 = 163840 instead. Within range [-524288, 524288]. */
    original.scale = 163840; /* 2.5 in Q16.16 */

    /* samples[3]: fixed(16,16), Q16.16. Range [-8, 8].
       1.5 = 98304, 2.25 = 147456, -3.75 = -245760 */
    original.samples[0] = 98304;   /* 1.5 */
    original.samples[1] = 147456;  /* 2.25 */
    original.samples[2] = -245760; /* -3.75 */

    /* Also set the nested Pose's signed fixed-point fields:
       pose.x: fixed(48,16) = 0.5 default = 32768, use 500.5 = 327999488
       pose.y: fixed(48,16) = 0 default, use 100.25 = 6569984 */
    original.pose.x = 327999488; /* 500.5 in Q48.16 */
    original.pose.y = 6569984;   /* 100.25 in Q48.16 */

    /* ---- SAVE ---- */
    n = sim_state_measure(&original);
    check(n > 0, "c/SignedFixedPointValidData: measure returns positive size");
    if (n <= 0 || n > (int64_t)sizeof(buffer)) { return 1; }

    n = sim_state_save(&original, buffer, n);
    check(n > 0, "c/SignedFixedPointValidData: save succeeds");
    if (n <= 0) { return 1; }

    /* ---- LOAD ---- */
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    check(sim_state_load(&loaded, buffer, n, &report) == 1,
          "c/SignedFixedPointValidData: load succeeds with valid data");
    check(report.malformed == 0 && report.refused == 0,
          "c/SignedFixedPointValidData: no malformed or refused on valid data");

    /* ---- VERIFY every signed fixed-point field round-trips ---- */

    check(loaded.tilt == 56,
          "c/SignedFixedPointValidData: tilt round-trips (3.5 in Q4.4)");

    check(loaded.angle == 2981888,
          "c/SignedFixedPointValidData: angle round-trips (45.5 in Q16.16)");

    check(loaded.position == 80920576,
          "c/SignedFixedPointValidData: position round-trips (1234.75 in Q48.16)");

    check(serialize_int128_equal(loaded.reach, serialize_int128_make(0ull, 3276816384ull)),
          "c/SignedFixedPointValidData: reach round-trips (50000.25 in Q112.16)");

    check(loaded.ticks == 500000,
          "c/SignedFixedPointValidData: ticks round-trips (500000 in Q32.0)");

    check(loaded.scale == 163840,
          "c/SignedFixedPointValidData: scale round-trips (2.5 in Q16.16, overriding default 1.0)");

    check(loaded.samples[0] == 98304 && loaded.samples[1] == 147456 && loaded.samples[2] == -245760,
          "c/SignedFixedPointValidData: samples[3] round-trip ([1.5, 2.25, -3.75] in Q16.16)");

    /* Nested Pose signed fixed-point fields */
    check(loaded.pose.x == 327999488,
          "c/SignedFixedPointValidData: pose.x round-trips (500.5 in Q48.16)");
    check(loaded.pose.y == 6569984,
          "c/SignedFixedPointValidData: pose.y round-trips (100.25 in Q48.16)");

    printf("%s\n", failures ? "c/SignedFixedPointValidData: RED" : "c/SignedFixedPointValidData: GREEN");
    return failures;
}
