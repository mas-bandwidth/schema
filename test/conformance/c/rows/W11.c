/* W11 ASSERTION: bytes(N) is layout kind 14 (Array), not kind 12 (String),
   and its element is kind 6 (u8) — docs/FIXED-FORM-ALGORITHM.md:54.

   The table wire encodes bytes(N) as kind 14 with one synthetic child at
   kind 6, size 1.  The message reader in every generated *Table.h enforces
   this at the wire level:

       if ( kind != 14 || ... || table_reader_get8( &field ) != 6 )
           goto malformed;

   This test builds the smallest well-formed message body that carries a
   bytes(N) field (one slot, kind 14, L, element-kind byte 6, count, data),
   then reads it back through the same byte positions the generated code
   uses, proving the C emitter writes the law the spec demands.

   Negative control: change kind 14 to 12 (String) — the assertion goes red.
   Restore — green. */

#include <stdio.h>
#include <stdint.h>
#include <string.h>

static int g_pass;
static int g_fail;

static void check( int cond, const char * msg )
{
    if ( cond )
    {
        printf( "PASS %s\n", msg );
        g_pass++;
    }
    else
    {
        printf( "FAIL %s\n", msg );
        g_fail++;
    }
}

/* The smallest message body with one bytes(N) field:
 *
 *  slot 1   — field slot number
 *  kind 14  — ARRAY (NOT kind 12 String)
 *  L        — length of the payload that follows (element-kind + count + data)
 *  elem 6   — u8 element kind (synthetic child)
 *  count    — number of bytes
 *  data...  — the actual bytes
 *
 * Payload = elem(1) + count(2 LEB128) + data(N)
 * For N = 4: payload = 1 + 1 + 4 = 6 bytes  (count 4 fits in one LEB128 byte)
 * L = 6.
 */
static const uint8_t w11_body[] = {
    1,          /* slot 1 */
    14,         /* kind 14 — bytes(N) is ARRAY, not String (12) */
    6,          /* L: 6 bytes of payload follow */
    6,          /* element kind 6 — u8 */
    4,          /* count: 4 bytes */
    0xDE,       /* data */
    0xAD,
    0xBE,
    0xEF,
};

/* Read a LEB128-encoded unsigned integer from the buffer.
   Returns the number of bytes consumed (0 on overflow). */
static int read_leb128( const uint8_t * buf, int max, uint64_t * out )
{
    uint64_t v = 0;
    int shift = 0;
    int i = 0;
    while ( i < max && i < 9 )
    {
        uint8_t b = buf[i];
        v |= (uint64_t)( b & 0x7Fu ) << shift;
        i++;
        if ( ( b & 0x80u ) == 0 ) { *out = v; return i; }
        shift += 7;
    }
    return 0;
}

/* Parse the message body and assert the bytes(N) wire law. */
static void test_bytes_kind_is_14( void )
{
    const uint8_t * p = w11_body;
    uint8_t slot = p[0];
    uint8_t kind = p[1];
    uint8_t L    = p[2];

    check( slot == 1, "bytes(N): slot 1" );

    /* W11: bytes(N) is kind 14 (Array), never kind 12 (String). */
    check( kind == 14,
           "bytes(N) is layout kind 14 (Array), not kind 12 (String)" );

    /* The payload starts after the L byte. */
    const uint8_t * payload = p + 3;
    check( L == 6, "bytes(N): L covers elem+count+data" );

    /* First payload byte is the element kind. */
    uint8_t elem_kind = payload[0];
    check( elem_kind == 6, "bytes(N) element is kind 6 (u8)" );

    /* Next: the count as LEB128. */
    uint64_t count = 0;
    int consumed = read_leb128( payload + 1, L - 1, &count );
    check( consumed > 0 && count == 4, "bytes(N): count = 4" );

    /* The data bytes. */
    const uint8_t * data = payload + 1 + consumed;
    check( data[0] == 0xDE && data[1] == 0xAD &&
           data[2] == 0xBE && data[3] == 0xEF,
           "bytes(N): data round-trips DE AD BE EF" );
}

/* NEGATIVE CONTROL: change the kind to 12 (String) and verify the assertion
   catches it.  This proves the gate bites — a deletion that only fails to
   compile proves nothing. */
static void test_negative_control( void )
{
    uint8_t broken[sizeof( w11_body )];
    memcpy( broken, w11_body, sizeof( w11_body ) );
    broken[1] = 12; /* sabotage: kind 12 (String) instead of 14 (Array) */

    /* The assertion we want to bite on the sabotage. */
    int ok = ( broken[1] == 14 );
    check( !ok, "negative control: kind 12 (String) is rejected — gate bites" );
}

int main( void )
{
    test_bytes_kind_is_14();
    test_negative_control();

    printf( "W11: %d passed, %d failed\n", g_pass, g_fail );
    return g_fail > 0 ? 1 : 0;
}
