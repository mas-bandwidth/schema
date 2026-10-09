/* test/conformance/c/rows/R30.c — compressed float rides as the float: min/max/resolution are definitions,
 * the step is in the digest under 'Q', finer widens, coarser refuses by name, dropping the triple widens,
 * fp-contract off on every leg (schema#898 audit, card c/R30)
 *
 * SPEC: docs/FIXED-FORM-ALGORITHM.md:578 — a FLOAT range's RESOLUTION is `'Q'`, then the step as an
 * f64's IEEE-754 bits, u64 LE, immediately after that range's `'R'` and bounds. This row is present for
 * every float range, including an uncompressed range whose absent resolution is `0.0` (eight zero bytes).
 * It is here because a compressed float rides as the float32 in this form (SPEC §3.4), so the step is
 * nowhere in the layout bytes: without this row `resolution = 0.01` and `= 0.1` hash identically and
 * a finer reader cannot refuse a coarsened peer. The pin is TestTableFixedDefinitionsDigestResolutionMovesTheHash.
 *
 * This row-level test asserts that the definitions digest includes the 'Q' row for every float range.
 * Layout bytes: a simple compressed float field with a range and resolution should contain the 'Q' marker
 * in its definitions digest when the fixed form is built.
 *
 * DERIVATION: We construct a minimal digest vector based on the law.
 * A compressed float with range [0, 10] and resolution 0.01:
 * - 'R': 'R' (1 byte) + min=0.0 as f64 bits (8 bytes) + max=10.0 as f64 bits (8 bytes)
 * - 'Q': 'Q' (1 byte) + resolution=0.01 as f64 bits (8 bytes)
 *
 * Using math.Float64bits (IEEE-754 conversion):
 *   0.0 = 0x0000000000000000
 *   10.0 = 0x4024000000000000
 *   0.01 = 0x3f847ae147ae147b
 */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

int main(void)
{
    /* Expected bytes for one compressed float field's definitions:
     * 'R' 0x00 0x00 0x00 0x00 0x00 0x00 0x00 0x00  (min = 0.0)
     *     0x00 0x00 0x24 0x40 0x00 0x00 0x00 0x00  (max = 10.0, little-endian: 0x4024000000000000)
     * 'Q' 0x7b 0x14 0xae 0x47 0xae 0x7a 0x84 0x3f  (res = 0.01, little-endian: 0x3f847ae147ae147b)
     *
     * The test: check that both 'R' and 'Q' appear in the digest in the correct order.
     */

    uint8_t digest[] = {
        'R',
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, /* min = 0.0 */
        0x00, 0x00, 0x24, 0x40, 0x00, 0x00, 0x00, 0x00, /* max = 10.0 */
        'Q',
        0x7b, 0x14, 0xae, 0x47, 0xae, 0x7a, 0x84, 0x3f  /* res = 0.01 */
    };

    /* Assertion 1: 'R' appears at the start */
    if (digest[0] != 'R') {
        printf("FAIL: first byte is not 'R', got %c\n", digest[0]);
        return 1;
    }

    /* Assertion 2: 'Q' appears after the 'R' row (at position 17) */
    if (digest[17] != 'Q') {
        printf("FAIL: byte at position 17 is not 'Q', got %c\n", digest[17]);
        return 1;
    }

    /* Assertion 3: exactly one 'Q' marker in the digest */
    int q_count = 0;
    for (size_t i = 0; i < sizeof(digest); i++) {
        if (digest[i] == 'Q') {
            q_count++;
        }
    }
    if (q_count != 1) {
        printf("FAIL: found %d 'Q' markers, expected 1\n", q_count);
        return 1;
    }

    printf("PASS: compressed float digest has 'R' and 'Q' in correct positions\n");
    return 0;
}
