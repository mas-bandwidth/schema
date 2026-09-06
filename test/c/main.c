/*
    The C corpus test — the fifth target's half of the cross-language gate.

    Every golden here is the SAME file the C++, C#, Go and Rust tests are held
    to. That is the whole point: a target that only checks itself proves
    nothing about the property this project exists to provide.
*/

#include <math.h> /* the gate-7 discrimination tripwires (fmaf, floor) */
#include <stdio.h>
#include <string.h>

#include "ArmDefaultsWire.h"
#include "ClausesWire.h"
#include "DegenerateWire.h"
#include "JoinsWire.h"
#include "TypesWire.h"
#include "EnumsWire.h"
#include "WireWire.h"

static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

/* golden_wire byte-compares written wire against the C++-pinned golden. */
static void golden_wire( const char * name, const unsigned char * data, int bytes )
{
    char path[512];
    static unsigned char expected[8192];
    FILE * file;
    size_t n;
    int i;

    sprintf( path, "../../testdata/wire/%s.bin", name );
    file = fopen( path, "rb" );
    if ( !file )
    {
        printf( "FAILED: cannot open wire golden %s\n", path );
        failed = 1;
        return;
    }
    n = fread( expected, 1, sizeof( expected ), file );
    fclose( file );

    if ( (int) n != bytes )
    {
        printf( "FAILED: wire golden %s — C wrote %d bytes, golden is %d\n", name, bytes, (int) n );
        failed = 1;
        return;
    }
    for ( i = 0; i < bytes; i++ )
    {
        if ( data[i] != expected[i] )
        {
            printf( "FAILED: wire golden %s — first difference at byte %d: C=%02x golden=%02x\n",
                    name, i, data[i], expected[i] );
            failed = 1;
            return;
        }
    }
}

static void fill_test_data( TestData * in )
{
    int i;
    memset( in, 0, sizeof( *in ) );
    in->a = -100;
    in->b = 100;
    in->c = 149;
    in->d = 0x11;
    in->e = 0x22;
    in->f = 0x33;
    in->g = 1;
    in->items_count = 3;
    in->items[0] = 0;
    in->items[1] = 128;
    in->items[2] = 255;
    in->float_value = 3.1415926f;
    in->compressed_float_value = 2.5f;
    in->double_value = 1.0 / 3.0;
    in->int8_value = -128;
    in->int16_value = -32768;
    in->uint8_value = 255;
    in->uint16_value = 65535;
    in->uint32_value = 4294967295u;
    in->uint64_value = 18446744073709551615ULL;
    in->int64_full = (-9223372036854775807LL - 1);
    in->int64_range = -999999999999LL;
    for ( i = 0; i < (int) sizeof( in->fixed_bytes ); i++ )
    {
        in->fixed_bytes[i] = (uint8_t) ( i * 3 );
    }
    memcpy( in->text, "the quick brown fox", 19 );
    in->text_length = 19;
}


static void test_default_arm_initialization( void )
{
    /* Independent oracle: First=1 in 2 bits, count=0 in 2, marker=5 in 3.
       The logical packet is one byte; the allocations provide read slack. */
    uint64_t oracle_words[2] = { 0, 0 };
    uint64_t write_words[2] = { 0, 0 };
    unsigned char * oracle = (unsigned char *) oracle_words;
    unsigned char * buffer = (unsigned char *) write_words;
    DefaultChoice input, output;
    serialize_write_stream_t ws;
    serialize_read_stream_t rs;
    int attempt, i;
    oracle[0] = 0x51;

    memset( &input, 0, sizeof( input ) );
    input.type = DEFAULT_CHOICE_TYPE_FIRST;
    input.as.first = new_default_arm();
    check( input.as.first.entries_count == 0, "DefaultArm constructor count is zero" );
    for ( i = 0; i < 2; i++ )
        check( input.as.first.entries[i].retries == -1 && input.as.first.entries[i].preferred == WEAPON_RAILGUN,
               "DefaultArm constructor initializes both backing entries" );
    input.as.first.marker = 5;
    serialize_write_stream_init( &ws, buffer, sizeof( write_words ) );
    if ( !write_default_choice( &ws, &input ) )
    {
        check( 0, "write constructed DefaultChoice" );
        return;
    }
    check( serialize_write_bits_processed( &ws ) == 7, "DefaultChoice writes the independent 7-bit oracle" );
    serialize_write_flush( &ws );
    check( serialize_write_bytes_processed( &ws ) == 1 && buffer[0] == 0x51,
           "DefaultChoice writes the independent 0x51 oracle" );

    memset( &output, 0, sizeof( output ) );
    output.type = DEFAULT_CHOICE_TYPE_SECOND;
    for ( attempt = 0; attempt < 2; attempt++ )
    {
        /* The second pass leaves the decoded First tag in place. Both reads
           must construct afresh; an unchanged-tag shortcut retains poison. */
        output.as.first.entries_count = 2;
        output.as.first.marker = 7;
        for ( i = 0; i < 2; i++ )
        {
            output.as.first.entries[i].retries = 123;
            output.as.first.entries[i].preferred = WEAPON_LASER;
        }
        serialize_read_stream_init( &rs, oracle, 1 );
        if ( !read_default_choice( &rs, &output ) )
        {
            check( 0, "read independent DefaultChoice oracle" );
            return;
        }
        const int consumed_ok = serialize_read_bits_processed( &rs ) == 7;
        const int selected_ok = output.type == DEFAULT_CHOICE_TYPE_FIRST && output.as.first.entries_count == 0 && output.as.first.marker == 5;
        check( consumed_ok, "DefaultChoice consumes exactly 7 bits" );
        check( selected_ok, "DefaultChoice selects First and reads count zero, marker five" );
        /* The control's backing-default marker requires a successful oracle decode. */
        if ( !consumed_ok || !selected_ok )
            return;
        for ( i = 0; i < 2; i++ )
            check( output.as.first.entries[i].retries == -1 && output.as.first.entries[i].preferred == WEAPON_RAILGUN,
                   "DefaultChoice reconstructs both backing entries, including repeated tags" );
    }
}

int main( void )
{
    static unsigned char buffer[8192 + 8];      /* + 8: read buffer allocations extend 8 bytes past the data (serialize.c loads 64-bit windows) */
    serialize_write_stream_t w;
    serialize_read_stream_t r;

    test_default_arm_initialization();

    /* ---- ShipCreate: the bool-gated flags branch, both ways ---- */
    {
        ShipCreate in, out;
        memset( &in, 0, sizeof( in ) );
        in.ship_type = SHIP_TYPE_BOMBER;
        in.position.x = 1000; in.position.y = -2000; in.position.z = 3000;
        in.has_flags = 1;
        in.flags = SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING;
        in.team = TEAM_BLUE;
        in.health = 750;
        in.thrust = 55;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ship_create( &w, &in ), "write ShipCreate" );
        serialize_write_flush( &w );
        golden_wire( "shipcreate_flags", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_ship_create( &r, &out ), "read ShipCreate" );
        check( out.ship_type == SHIP_TYPE_BOMBER && out.team == TEAM_BLUE, "ShipCreate enums round-trip" );
        check( out.health == 750 && out.thrust == 55, "ShipCreate ranged ints round-trip" );
        check( out.flags == ( SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING ), "ShipCreate flags round-trip" );

        /* the untaken branch must read as zero (SPEC §5) */
        in.has_flags = 0;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ship_create( &w, &in ), "write ShipCreate no flags" );
        serialize_write_flush( &w );
        memset( &out, 0, sizeof( out ) );
        out.flags = 0xFF; /* dirty — the read must clear it */
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_ship_create( &r, &out ), "read ShipCreate no flags" );
        check( !out.has_flags && out.flags == 0, "untaken branch reads as zero (SPEC §5)" );
    }

    /* ---- RigidBody: the back-reference example, both branch sides ---- */
    {
        RigidBody in, out;
        memset( &in, 0, sizeof( in ) );
        in.position.x = 1.5; in.position.y = -2.5; in.position.z = 3.25;
        in.orientation.x = 0.1; in.orientation.y = 0.2; in.orientation.z = 0.3; in.orientation.w = 0.9;
        in.at_rest = 0;
        in.linear_velocity.x = 10.0; in.linear_velocity.y = 20.0; in.linear_velocity.z = -3.0;
        in.angular_velocity.x = 0.25; in.angular_velocity.y = 0.5; in.angular_velocity.z = 0.75;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_rigid_body( &w, &in ), "write RigidBody moving" );
        serialize_write_flush( &w );
        golden_wire( "rigidbody_moving", buffer, serialize_write_bytes_processed( &w ) );

        in.at_rest = 1;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_rigid_body( &w, &in ), "write RigidBody at rest" );
        serialize_write_flush( &w );
        golden_wire( "rigidbody_at_rest", buffer, serialize_write_bytes_processed( &w ) );

        /* the at-rest read must ZERO both velocities (SPEC §5) */
        memset( &out, 0, sizeof( out ) );
        out.linear_velocity.x = 99.0;
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_rigid_body( &r, &out ), "read RigidBody at rest" );
        check( out.at_rest, "at_rest reads true" );
        check( out.linear_velocity.x == 0.0 && out.linear_velocity.y == 0.0 && out.linear_velocity.z == 0.0 &&
               out.angular_velocity.x == 0.0 && out.angular_velocity.y == 0.0 && out.angular_velocity.z == 0.0,
               "velocities read as zero under the taken at-rest branch (SPEC §5)" );
    }

    /* ---- Chat: the string framing ---- */
    {
        Chat in, out;
        memset( &in, 0, sizeof( in ) );
        memcpy( in.text, "wire parity", 11 );
        in.text_length = 11;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_chat( &w, &in ), "write Chat" );
        serialize_write_flush( &w );
        golden_wire( "chat", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_chat( &r, &out ), "read Chat" );
        check( out.text_length == 11 && memcmp( out.text, "wire parity", 11 ) == 0, "Chat round-trips" );
    }

    /* ---- Chat: an interior null is content the read REFUSES (SPEC §4.7) ----
       The compliance half: the C reader owes the same refusal the C++, C#, Go
       and Rust readers have always emitted. A conforming writer cannot produce
       the vector (the write side asserts), so the hostile stream is doctored
       after the write: the wire is 9 length bits riding bytes 0-1, align
       padding to byte 2, then the text bytes — buffer[2 + k] is text[k]. The
       vectors pin every branch of the word-wise scan: a null inside a full
       word, a null in the final byte of an eight-multiple payload, a null the
       overlapping tail word judges, and a null on the short-string per-byte
       path. */
    {
        Chat in, out;

        memset( &in, 0, sizeof( in ) );
        memcpy( in.text, "interior null defense s", 23 ); /* 2 words + 7-byte tail */
        in.text_length = 23;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_chat( &w, &in ), "write Chat for doctoring" );
        serialize_write_flush( &w );

        buffer[2 + 11] = 0; /* inside the second full scan word */
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( !read_chat( &r, &out ), "an interior null is refused (SPEC §4.7)" );

        buffer[2 + 11] = 'x';
        buffer[2 + 22] = 0; /* the FINAL payload byte — the overlapping tail word judges it */
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( !read_chat( &r, &out ), "a null in the overlap-tail word is refused" );

        memcpy( in.text, "sixteen bytes ok", 16 ); /* two exact words, no tail */
        in.text_length = 16;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_chat( &w, &in ), "write Chat eight-multiple" );
        serialize_write_flush( &w );
        buffer[2 + 15] = 0; /* last byte of an eight-multiple payload */
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( !read_chat( &r, &out ), "a null in the last full-word byte is refused" );

        memcpy( in.text, "hello", 5 ); /* below one word: the per-byte path */
        in.text_length = 5;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_chat( &w, &in ), "write short Chat" );
        serialize_write_flush( &w );
        buffer[2 + 2] = 0;
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( !read_chat( &r, &out ), "a short string's interior null is refused" );
    }

    /* ---- Chat: the accept neighbors — 0x00 nowhere in [0, length) passes at
       every word-scan boundary: empty, one byte, one exact word, a word plus
       a one-byte tail, two exact words (SPEC §4.7) ---- */
    {
        static const int32_t boundary_lengths[] = { 0, 1, 8, 9, 16 };
        Chat in, out;
        int li, i;
        for ( li = 0; li < (int) ( sizeof( boundary_lengths ) / sizeof( boundary_lengths[0] ) ); li++ )
        {
            int32_t length = boundary_lengths[li];
            memset( &in, 0, sizeof( in ) );
            for ( i = 0; i < length; i++ )
            {
                in.text[i] = (char) ( 'a' + ( i % 26 ) );
            }
            in.text_length = length;
            serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
            check( write_chat( &w, &in ), "write Chat boundary length" );
            serialize_write_flush( &w );

            memset( &out, 0xEF, sizeof( out ) ); /* dirty — the reader must supply the terminator */
            serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
            check( read_chat( &r, &out ), "read Chat boundary length" );
            check( out.text_length == length && memcmp( out.text, in.text, (size_t) length ) == 0,
                   "Chat boundary length round-trips" );
            check( out.text[length] == 0, "the reader supplies the terminator" );
        }
    }

    /* ---- ProbeBits: the full-range uint32/uint64 paths ---- */
    {
        ProbeBits in, out;
        memset( &in, 0, sizeof( in ) );
        in.small = 0x1FF;
        in.boundary = 0x1FFFFFFFFULL;
        in.wide = 0xFEDCBA9876543210ULL;
        in.sensor = 4294967295u;
        in.nonce = 18446744073709551615ULL;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_bits( &w, &in ), "write ProbeBits" );
        serialize_write_flush( &w );
        golden_wire( "probebits", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_probe_bits( &r, &out ), "read ProbeBits" );
        check( out.small == in.small && out.boundary == in.boundary && out.wide == in.wide &&
               out.sensor == in.sensor && out.nonce == in.nonce,
               "ProbeBits round-trips — 9/33/64-bit and full-range paths" );
    }

    /* ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
       round trip, the None arm, an array of unions, and the refusal
       negative controls ---- */
    {
        ProbeCollider in, out;
        int bytes;
        memset( &in, 0, sizeof( in ) );
        check( in.shape.type == PROBE_SHAPE_TYPE_NONE, "zero IS the empty union" );
        check( PROBE_SHAPE_MAX_BITS == 2 + 16, "MAX_BITS is tag + the largest arm" );

        in.armor = 7;
        in.shape.type = PROBE_SHAPE_TYPE_SLAB;
        in.shape.as.slab.width = 42;
        in.shape.as.slab.height = 9;
        /* in.backup stays None — the empty arm costs the tag bits only */
        in.extras_count = 1;
        in.extras[0].type = PROBE_SHAPE_TYPE_RING;
        in.extras[0].as.ring.radius = 777;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_collider( &w, &in ), "write ProbeCollider" );
        serialize_write_flush( &w );
        bytes = serialize_write_bytes_processed( &w );
        golden_wire( "probecollider", buffer, bytes );

        memset( &out, 0, sizeof( out ) );
        out.backup.type = PROBE_SHAPE_TYPE_RING; /* dirty — the read must restore None */
        serialize_read_stream_init( &r, buffer, bytes );
        check( read_probe_collider( &r, &out ), "read ProbeCollider" );
        check( out.armor == 7 && out.shape.type == PROBE_SHAPE_TYPE_SLAB &&
               out.shape.as.slab.width == 42 && out.shape.as.slab.height == 9,
               "the selected arm round-trips" );
        check( out.backup.type == PROBE_SHAPE_TYPE_NONE, "the None arm reads back empty" );
        check( out.extras_count == 1 && out.extras[0].type == PROBE_SHAPE_TYPE_RING &&
               out.extras[0].as.ring.radius == 777, "the union array round-trips" );

        /* NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
           [0, 2]; forcing both bits makes 3 and the read must refuse */
        buffer[1] |= 0x03;
        serialize_read_stream_init( &r, buffer, bytes );
        check( !read_probe_collider( &r, &out ), "an out-of-range union tag is refused (SPEC §4.8)" );
        buffer[1] = (unsigned char) ( buffer[1] & ~0x03 ) | 0x02; /* restore tag = 2 */

        /* NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at
           bit offset 10 with range [0, 100]; all seven bits decode 127 */
        buffer[1] |= 0xFC;
        buffer[2] |= 0x01;
        serialize_read_stream_init( &r, buffer, bytes );
        check( !read_probe_collider( &r, &out ), "a corrupt union arm payload is refused (SPEC §4.8)" );

        /* the write side validates the tag BEFORE it rides */
        in.shape.type = (ProbeShapeType) 3;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( !write_probe_collider( &w, &in ), "an out-of-set union tag writes nothing (SPEC §4.8)" );
    }

    /* ---- TestData: signed narrows, full-range ints, align, fixed bytes, string ---- */
    {
        TestData in, out;
        fill_test_data( &in );

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_test_data( &w, &in ), "write TestData" );
        serialize_write_flush( &w );
        golden_wire( "testdata", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_test_data( &r, &out ), "read TestData" );
        check( out.a == in.a && out.b == in.b && out.c == in.c, "TestData ranged ints" );
        check( out.uint32_value == in.uint32_value && out.uint64_value == in.uint64_value,
               "TestData full-range unsigned" );
        check( out.int64_full == in.int64_full && out.int64_range == in.int64_range,
               "TestData int64 paths" );
        check( out.text_length == 19 && memcmp( out.text, "the quick brown fox", 19 ) == 0,
               "TestData string" );
        check( memcmp( out.fixed_bytes, in.fixed_bytes, sizeof( in.fixed_bytes ) ) == 0,
               "TestData fixed bytes" );
    }

    /* ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
       0.005 quantizes to 1 under the float32 two-rounding law (fused: 0,
       double: 0); -4.8585 over the non-zero-min range quantizes to 142
       (double: 141). The same pinned instance as the C++ leg, against the
       same golden. */
    {
        CompressedProbe in, out;
        memset( &in, 0, sizeof( in ) );
        in.boundary = 0.005f;
        in.offset = -4.8585f;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_compressed_probe( &w, &in ), "write CompressedProbe" );
        serialize_write_flush( &w );
        golden_wire( "compressed_probe", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_compressed_probe( &r, &out ), "read CompressedProbe" );
        check( out.boundary == 1.0f / 1000.0f * 10.0f, "boundary reconstructs integer 1" );
        check( out.offset == 142.0f / 10000.0f * 10.0f - 5.0f, "offset reconstructs integer 142" );

        /* the discrimination property itself is asserted, so the vectors
           cannot quietly stop discriminating (fma() is exact by definition
           on every platform) */
        {
            volatile float boundary_n = ( 0.005f - 0.0f ) / 10.0f;
            volatile float boundary_scaled = boundary_n * 1000.0f;
            volatile float offset_n = ( -4.8585f - -5.0f ) / 10.0f;
            volatile float offset_scaled = offset_n * 10000.0f;
            check( (unsigned) floor( boundary_scaled + 0.5f ) == 1,
                   "the law: float32, two roundings" );
            check( (unsigned) floor( fmaf( boundary_n, 1000.0f, 0.5f ) ) == 0,
                   "a fused build diverges on the boundary vector" );
            check( (unsigned) floor( (double) boundary_n * 1000.0 + 0.5 ) == 0,
                   "a double build diverges on the boundary vector" );
            check( (unsigned) floor( offset_scaled + 0.5f ) == 142,
                   "the law on the non-zero-min vector" );
            check( (unsigned) floor( (double) offset_n * 10000.0 + 0.5 ) == 141,
                   "a double build diverges on the non-zero-min vector" );
        }
    }

    /* ---- the string UTF-8 contract's validator can FAIL (SPEC §4.7) ----
       string(N) payloads are well-formed UTF-8 by contract, writer-trusted,
       debug-asserted through schema_utf8_valid_. The enforcement predicate
       is proven able to reject each malformation class. */
    {
        check( schema_utf8_valid_( (const serialize_uint8_t *) "plain ascii", 11 ), "ascii is well-formed" );
        check( schema_utf8_valid_( (const serialize_uint8_t *) "h\xC3\xA9llo", 6 ), "2-byte sequence" );
        check( schema_utf8_valid_( (const serialize_uint8_t *) "\xF0\x9F\x9A\x80", 4 ), "4-byte astral sequence" );
        check( !schema_utf8_valid_( (const serialize_uint8_t *) "\xFF", 1 ), "no such lead byte" );
        check( !schema_utf8_valid_( (const serialize_uint8_t *) "\x80", 1 ), "bare continuation" );
        check( !schema_utf8_valid_( (const serialize_uint8_t *) "ok\xC3", 3 ), "truncated sequence" );
        check( !schema_utf8_valid_( (const serialize_uint8_t *) "\xC0\xAF", 2 ), "overlong slash" );
        check( !schema_utf8_valid_( (const serialize_uint8_t *) "\xED\xA0\x80", 3 ), "encoded surrogate" );
        check( !schema_utf8_valid_( (const serialize_uint8_t *) "\xF4\x90\x80\x80", 4 ), "above U+10FFFF" );
    }

    /* ---- InputPacket: the counted array ---- */
    {
        InputPacket packet;
        memset( &packet, 0, sizeof( packet ) );
        packet.synchronize_sequence = 7;
        packet.current_frame = 123456789;
        packet.start_frame = 123456780;
        packet.inputs_count = 2;
        packet.inputs[0].throttle = 0.5f;
        packet.inputs[0].fire = 1;
        packet.inputs[1].stick_x = -0.25f;
        packet.inputs[1].boost = 1;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_input_packet( &w, &packet ), "write InputPacket" );
        serialize_write_flush( &w );
        golden_wire( "inputpacket", buffer, serialize_write_bytes_processed( &w ) );
    }

    /* ---- SPEC §5: BOTH untaken sides zero on read ----
       The backend used to zero only the then-fields in the else arm, so taking
       the then arm left the else-fields holding whatever the caller's memory
       had. Nothing failed: the wire was correct and the storage was stale. */
    {
        ProbeSample in, out;

        memset( &in, 0, sizeof( in ) );
        in.active = 1;
        in.orientation = 45.0f;
        in.raw_delta = 7;
        in.big_delta = 9;
        in.weapon = WEAPON_RAILGUN;
        in.has_target = 1;
        in.target_id = 4242;
        in.samples_count = 1;   /* [1..8]: the minimum is 1, not 0 */
        in.samples[0] = 99;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_sample( &w, &in ), "write ProbeSample active" );
        serialize_write_flush( &w );

        /* dirty every member the ACTIVE arm does not write */
        memset( &out, 0xEF, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_probe_sample( &r, &out ), "read ProbeSample active" );
        check( out.active, "active reads true" );
        check( out.weapon == WEAPON_RAILGUN, "the taken arm decodes" );
        check( out.idle_ticks == 0,
               "the UNTAKEN else field is zeroed when the then arm is taken (SPEC §5)" );

        /* and the mirror: taking the else arm zeroes the then fields */
        in.active = 0;
        in.idle_ticks = 1234;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_probe_sample( &w, &in ), "write ProbeSample idle" );
        serialize_write_flush( &w );

        memset( &out, 0xEF, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_probe_sample( &r, &out ), "read ProbeSample idle" );
        check( !out.active && out.idle_ticks == 1234, "the else arm decodes" );
        check( out.weapon == 0 && out.target_id == 0,
               "the UNTAKEN then fields are zeroed when the else arm is taken (SPEC §5)" );
    }

    /* ---- flag_name / flag_names: per-bit names and the set renderer ---- */
    {
        char buffer[SHIP_FLAGS_NAMES_MAX];
        check( strcmp( flag_name_ship_flags( 0 ), "FiringLaser" ) == 0, "flag_name names bit 0" );
        check( strcmp( flag_name_ship_flags( 9 ), "???" ) == 0, "flag_name is out-of-range safe" );
        check( strcmp( flag_names_ship_flags( 0, buffer, sizeof( buffer ) ), "0" ) == 0, "flag_names renders the empty set as 0" );
        check( strcmp( flag_names_ship_flags( SHIP_FLAGS_FIRING_LASER | SHIP_FLAGS_BRAKING, buffer, sizeof( buffer ) ), "FiringLaser|Braking" ) == 0, "flag_names renders the set bits" );
        check( strcmp( flag_names_ship_flags( SHIP_FLAGS_AIMING | ( 1ULL << 63 ), buffer, sizeof( buffer ) ), "Aiming|0x8000000000000000" ) == 0, "flag_names renders unknown high bits as hex" );
    }

    /* ---- Degenerate.schema: the degenerate arrangements (issue #203) ----

       Twelve shapes written back to back into ONE stream against the one
       C++-pinned golden, in the C++ test's order. A fixed scalar array whose
       elements an emitter places TWICE is invisible to a same-language round
       trip; only the byte compare against another language's bytes names it. */
    {
        Vec2 vec2; SpanF64 span_f64; SpanU64 span_u64; SpanI64 span_i64;
        SpanOne span_one; SpanChunk span_chunk; SpanTail span_tail;
        SpanTwice span_twice; Trio trio; TrioSole trio_sole;
        TrioFirst trio_first; TrioStraddle straddle;
        Vec2 r_vec2; SpanF64 r_span_f64; SpanU64 r_span_u64; SpanI64 r_span_i64;
        SpanOne r_span_one; SpanChunk r_span_chunk; SpanTail r_span_tail;
        SpanTwice r_span_twice; Trio r_trio; TrioSole r_trio_sole;
        TrioFirst r_trio_first; TrioStraddle r_straddle;

        memset( &vec2, 0, sizeof( vec2 ) );
        vec2.x = 1.5; vec2.y = -2.25;

        memset( &span_f64, 0, sizeof( span_f64 ) );
        span_f64.values[0] = 3.5; span_f64.values[1] = -4.75;

        memset( &span_u64, 0, sizeof( span_u64 ) );
        span_u64.values[0] = 0xDEADBEEFCAFEBABEULL; span_u64.values[1] = 1;

        memset( &span_i64, 0, sizeof( span_i64 ) );
        span_i64.values[0] = -1234567890123LL; span_i64.values[1] = 42;

        memset( &span_one, 0, sizeof( span_one ) );
        span_one.values[0] = 0x0123456789ABCDEFULL;

        memset( &span_chunk, 0, sizeof( span_chunk ) );
        span_chunk.values[0] = 0x1111; span_chunk.values[1] = 0x2222;
        span_chunk.values[2] = 0x3333; span_chunk.values[3] = 0x4444;

        memset( &span_tail, 0, sizeof( span_tail ) );
        span_tail.values[0] = 6.125; span_tail.values[1] = -7.0;
        span_tail.tail = 0xFEEDFACEu;

        memset( &span_twice, 0, sizeof( span_twice ) );
        span_twice.a[0] = 8.5; span_twice.a[1] = 9.5;
        span_twice.b[0] = -10.5; span_twice.b[1] = -11.5;

        memset( &trio, 0, sizeof( trio ) );
        trio.a = 0xABCDE; trio.b = 0x12345; trio.c = 0xFFFFF;

        memset( &trio_sole, 0, sizeof( trio_sole ) );
        trio_sole.inner.a = 1; trio_sole.inner.b = 2; trio_sole.inner.c = 3;

        memset( &trio_first, 0, sizeof( trio_first ) );
        trio_first.inner.a = 0xAAAAA; trio_first.inner.b = 0x55555;
        trio_first.inner.c = 0xF0F0F; trio_first.trailer = 0xBEEF;

        memset( &straddle, 0, sizeof( straddle ) );
        straddle.pad0 = 0x0011223344556677ULL;
        straddle.pad1 = 0x8899AABBCCDDEEFFULL;
        straddle.pad2 = 0xFFFFFFFFFFFFFFFFULL;
        straddle.pad3 = 0;
        straddle.pad4 = 0x123456789ABCDEF0ULL;
        straddle.pad5 = 0xABCDEFu;
        straddle.inner.a = 0x11111; straddle.inner.b = 0x22222; straddle.inner.c = 0x33333;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_vec2( &w, &vec2 ), "write Vec2" );
        check( write_span_f64( &w, &span_f64 ), "write SpanF64" );
        check( write_span_u64( &w, &span_u64 ), "write SpanU64" );
        check( write_span_i64( &w, &span_i64 ), "write SpanI64" );
        check( write_span_one( &w, &span_one ), "write SpanOne" );
        check( write_span_chunk( &w, &span_chunk ), "write SpanChunk" );
        check( write_span_tail( &w, &span_tail ), "write SpanTail" );
        check( write_span_twice( &w, &span_twice ), "write SpanTwice" );
        check( write_trio( &w, &trio ), "write Trio" );
        check( write_trio_sole( &w, &trio_sole ), "write TrioSole" );
        check( write_trio_first( &w, &trio_first ), "write TrioFirst" );
        check( write_trio_straddle( &w, &straddle ), "write TrioStraddle" );
        check( serialize_write_bits_processed( &w ) == 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
               "the twelve degenerate shapes ride their declared widths and nothing more" );
        serialize_write_flush( &w );
        golden_wire( "degenerate", buffer, serialize_write_bytes_processed( &w ) );

        /* dirty every read target: a read that skips a field must be caught,
           and gcc's -Wmaybe-uninitialized wants the definite store */
        memset( &r_vec2, 0xEF, sizeof( r_vec2 ) );
        memset( &r_span_f64, 0xEF, sizeof( r_span_f64 ) );
        memset( &r_span_u64, 0xEF, sizeof( r_span_u64 ) );
        memset( &r_span_i64, 0xEF, sizeof( r_span_i64 ) );
        memset( &r_span_one, 0xEF, sizeof( r_span_one ) );
        memset( &r_span_chunk, 0xEF, sizeof( r_span_chunk ) );
        memset( &r_span_tail, 0xEF, sizeof( r_span_tail ) );
        memset( &r_span_twice, 0xEF, sizeof( r_span_twice ) );
        memset( &r_trio, 0xEF, sizeof( r_trio ) );
        memset( &r_trio_sole, 0xEF, sizeof( r_trio_sole ) );
        memset( &r_trio_first, 0xEF, sizeof( r_trio_first ) );
        memset( &r_straddle, 0xEF, sizeof( r_straddle ) );

        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_vec2( &r, &r_vec2 ), "read Vec2" );
        check( read_span_f64( &r, &r_span_f64 ), "read SpanF64" );
        check( read_span_u64( &r, &r_span_u64 ), "read SpanU64" );
        check( read_span_i64( &r, &r_span_i64 ), "read SpanI64" );
        check( read_span_one( &r, &r_span_one ), "read SpanOne" );
        check( read_span_chunk( &r, &r_span_chunk ), "read SpanChunk" );
        check( read_span_tail( &r, &r_span_tail ), "read SpanTail" );
        check( read_span_twice( &r, &r_span_twice ), "read SpanTwice" );
        check( read_trio( &r, &r_trio ), "read Trio" );
        check( read_trio_sole( &r, &r_trio_sole ), "read TrioSole" );
        check( read_trio_first( &r, &r_trio_first ), "read TrioFirst" );
        check( read_trio_straddle( &r, &r_straddle ), "read TrioStraddle" );

        check( r_vec2.x == 1.5 && r_vec2.y == -2.25, "Vec2 round-trips" );
        check( r_span_f64.values[0] == 3.5 && r_span_f64.values[1] == -4.75, "SpanF64 round-trips" );
        check( r_span_u64.values[0] == 0xDEADBEEFCAFEBABEULL && r_span_u64.values[1] == 1, "SpanU64 round-trips" );
        check( r_span_i64.values[0] == -1234567890123LL && r_span_i64.values[1] == 42, "SpanI64 round-trips" );
        check( r_span_one.values[0] == 0x0123456789ABCDEFULL, "SpanOne round-trips" );
        check( r_span_chunk.values[0] == 0x1111 && r_span_chunk.values[3] == 0x4444, "SpanChunk round-trips" );
        check( r_span_tail.values[0] == 6.125 && r_span_tail.values[1] == -7.0 && r_span_tail.tail == 0xFEEDFACEu,
               "SpanTail round-trips" );
        check( r_span_twice.a[0] == 8.5 && r_span_twice.b[1] == -11.5, "SpanTwice round-trips" );
        check( r_trio.a == 0xABCDE && r_trio.b == 0x12345 && r_trio.c == 0xFFFFF, "Trio round-trips" );
        check( r_trio_sole.inner.a == 1 && r_trio_sole.inner.c == 3, "TrioSole round-trips" );
        check( r_trio_first.inner.a == 0xAAAAA && r_trio_first.trailer == 0xBEEF, "TrioFirst round-trips" );
        check( r_straddle.pad0 == 0x0011223344556677ULL && r_straddle.pad4 == 0x123456789ABCDEF0ULL,
               "TrioStraddle pads round-trip" );
        check( r_straddle.pad5 == 0xABCDEFu && r_straddle.inner.a == 0x11111 && r_straddle.inner.c == 0x33333,
               "TrioStraddle's nested fields round-trip across the boundary" );
    }

    /* ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----

       Degenerate.schema is every-type-a-whole-number-of-bytes by
       construction, so no clause boundary in it lands mid-byte. These two
       units are chosen so they do. Each shape is written to its OWN stream
       and flushed, and the golden is those concatenated — the shapes are not
       byte-aligned, so a shared stream would not equal the concatenation
       every emitter can produce. */
    {
        unsigned char arrangement[1024];
        unsigned char shape[64];
        unsigned char slice[64];
        int stream_len = 0;
        int read_off = 0;
        int c, i, f, o, in, sl;

        /* write one shape into its own stream, hold it to its declared width,
           and append the flushed bytes */
#define EMIT( bits, call )                                                    \
        do                                                                    \
        {                                                                     \
            serialize_write_stream_t ws;                                      \
            int n;                                                            \
            serialize_write_stream_init( &ws, shape, (int) sizeof( shape ) ); \
            check( call, "write " #call );                                    \
            check( serialize_write_bits_processed( &ws ) == ( bits ),         \
                   #call " rides its declared width" );                       \
            serialize_write_flush( &ws );                                     \
            n = serialize_write_bytes_processed( &ws );                       \
            check( n == ( ( bits ) + 7 ) / 8, #call " byte width" );          \
            memcpy( arrangement + stream_len, shape, (size_t) n );            \
            stream_len += n;                                                  \
        } while ( 0 )

#define CONSUME( bits, call )                                                 \
        do                                                                    \
        {                                                                     \
            serialize_read_stream_t rs;                                       \
            int n = ( ( bits ) + 7 ) / 8;                                     \
            memset( slice, 0, sizeof( slice ) );                              \
            memcpy( slice, arrangement + read_off, (size_t) n );              \
            serialize_read_stream_init( &rs, slice, n );                      \
            check( call, "read " #call );                                     \
            read_off += n;                                                    \
        } while ( 0 )

        /* ---- Clauses.schema ---- */
        {
            static const int w13_counts[] = { 0, 1, 3, 4, 5, 7, 12 };
            static const int w17_counts[] = { 0, 1, 2, 3, 4, 9 };
            static const int w26_counts[] = { 0, 1, 2, 3, 6 };
            static const int w1_counts[] = { 0, 1, 3, 4, 5, 20 };
            static const int tri_counts[] = { 0, 1, 3, 4, 5, 10 };
            static const int strs_bits[] = { 27, 155, 75 };
            int k;

            W13 w13; W17 w17; W26 w26; W1 w1; W52 w52; W50 w50; F13 f13;
            ArrTri3 tri; ArrEleven eleven; HoldsEmptyUnion hu; Strs strs;
            ArrNested nested; Sole sole;

            for ( k = 0; k < 7; k++ )
            {
                c = w13_counts[k];
                memset( &w13, 0, sizeof( w13 ) );
                w13.items_count = c;
                for ( i = 0; i < c; i++ ) w13.items[i] = (uint16_t) ( 8191 - i * 733 );
                EMIT( 4 + 13 * c, write_w13( &ws, &w13 ) );
            }
            for ( k = 0; k < 6; k++ )
            {
                c = w17_counts[k];
                memset( &w17, 0, sizeof( w17 ) );
                w17.items_count = c;
                for ( i = 0; i < c; i++ ) w17.items[i] = (uint32_t) ( 131071 - i * 11117 );
                EMIT( 4 + 17 * c, write_w17( &ws, &w17 ) );
            }
            for ( k = 0; k < 5; k++ )
            {
                c = w26_counts[k];
                memset( &w26, 0, sizeof( w26 ) );
                w26.items_count = c;
                for ( i = 0; i < c; i++ ) w26.items[i] = (uint32_t) ( 67108863 - i * 5555555 );
                EMIT( 3 + 26 * c, write_w26( &ws, &w26 ) );
            }
            for ( k = 0; k < 6; k++ )
            {
                c = w1_counts[k];
                memset( &w1, 0, sizeof( w1 ) );
                w1.items_count = c;
                for ( i = 0; i < c; i++ ) w1.items[i] = (uint8_t) ( i % 2 );
                EMIT( 5 + c, write_w1( &ws, &w1 ) );
            }
            for ( c = 0; c <= 3; c++ )
            {
                memset( &w52, 0, sizeof( w52 ) );
                w52.items_count = c;
                for ( i = 0; i < c; i++ ) w52.items[i] = 4503599627370495ULL - (uint64_t) i * 123456789ULL;
                EMIT( 2 + 52 * c, write_w52( &ws, &w52 ) );
            }
            for ( c = 0; c <= 3; c++ )
            {
                memset( &w50, 0, sizeof( w50 ) );
                w50.items_count = c;
                for ( i = 0; i < c; i++ ) w50.items[i] = 1125899906842623ULL - (uint64_t) i * 987654321ULL;
                EMIT( 2 + 50 * c, write_w50( &ws, &w50 ) );
            }
            memset( &f13, 0, sizeof( f13 ) );
            for ( i = 0; i < 7; i++ ) f13.items[i] = (uint16_t) ( 8191 - i * 911 );
            EMIT( 91, write_f13( &ws, &f13 ) );

            for ( k = 0; k < 6; k++ )
            {
                c = tri_counts[k];
                memset( &tri, 0, sizeof( tri ) );
                tri.items_count = c;
                for ( i = 0; i < c; i++ ) { tri.items[i].a = (uint32_t) ( i % 2 ); tri.items[i].b = (uint32_t) ( i % 4 ); }
                EMIT( 4 + 3 * c, write_arr_tri3( &ws, &tri ) );
            }

            memset( &eleven, 0, sizeof( eleven ) );
            for ( i = 0; i < 9; i++ ) { eleven.items[i].a = (uint32_t) ( i % 8 ); eleven.items[i].b = (uint32_t) ( 255 - i * 17 ); }
            EMIT( 99, write_arr_eleven( &ws, &eleven ) );

            /* lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag */
            for ( k = 0; k <= 2; k++ )
            {
                memset( &hu, 0, sizeof( hu ) );
                hu.lead = 21;
                hu.tail = 99;
                hu.u.type = (EmptyUnionType) k;
                EMIT( 14, write_holds_empty_union( &ws, &hu ) );
            }

            /* lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
               b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The
               5-bit lead is what puts the align at a non-zero offset. */
            for ( k = 0; k <= 2; k++ )
            {
                memset( &strs, 0, sizeof( strs ) );
                strs.lead = 21;
                strs.tail = 5;
                if ( k == 1 )
                {
                    memcpy( strs.s, "abcdefgh", 8 );
                    strs.s_length = 8;
                    for ( i = 0; i < 8; i++ ) strs.b[i] = (uint8_t) ( 0xF0 + i );
                    strs.b_length = 8;
                }
                else if ( k == 2 )
                {
                    memcpy( strs.s, "xyz", 3 );
                    strs.s_length = 3;
                    for ( i = 0; i < 3; i++ ) strs.b[i] = (uint8_t) ( i + 1 );
                    strs.b_length = 3;
                }
                EMIT( strs_bits[k], write_strs( &ws, &strs ) );
            }

            for ( c = 0; c <= 4; c++ )
            {
                memset( &nested, 0, sizeof( nested ) );
                nested.lead = 21;
                nested.tail = 5;
                nested.items_count = c;
                for ( i = 0; i < c; i++ ) { nested.items[i].a = (uint32_t) ( i % 8 ); nested.items[i].b = (uint32_t) ( 200 - i * 7 ); }
                EMIT( 11 + 11 * c, write_arr_nested( &ws, &nested ) );
            }

            memset( &sole, 0, sizeof( sole ) );
            sole.only = 5555;
            EMIT( 13, write_sole( &ws, &sole ) );

            golden_wire( "clauses", arrangement, stream_len );

            /* Read each shape back out of its own slice. A clause that decodes
               a different number of elements than the writer encoded shows up
               here even where the byte compare above happens to pass. */
            for ( k = 0; k < 7; k++ )
            {
                c = w13_counts[k];
                memset( &w13, 0xEF, sizeof( w13 ) );
                CONSUME( 4 + 13 * c, read_w13( &rs, &w13 ) );
                check( w13.items_count == c, "W13 count round-trips" );
                for ( i = 0; i < c; i++ ) check( w13.items[i] == (uint16_t) ( 8191 - i * 733 ), "W13 element round-trips" );
            }
            for ( k = 0; k < 6; k++ )
            {
                c = w17_counts[k];
                memset( &w17, 0xEF, sizeof( w17 ) );
                CONSUME( 4 + 17 * c, read_w17( &rs, &w17 ) );
                check( w17.items_count == c, "W17 count round-trips" );
                for ( i = 0; i < c; i++ ) check( w17.items[i] == (uint32_t) ( 131071 - i * 11117 ), "W17 element round-trips" );
            }
            for ( k = 0; k < 5; k++ )
            {
                c = w26_counts[k];
                memset( &w26, 0xEF, sizeof( w26 ) );
                CONSUME( 3 + 26 * c, read_w26( &rs, &w26 ) );
                check( w26.items_count == c, "W26 count round-trips" );
                for ( i = 0; i < c; i++ ) check( w26.items[i] == (uint32_t) ( 67108863 - i * 5555555 ), "W26 element round-trips" );
            }
            for ( k = 0; k < 6; k++ )
            {
                c = w1_counts[k];
                memset( &w1, 0xEF, sizeof( w1 ) );
                CONSUME( 5 + c, read_w1( &rs, &w1 ) );
                check( w1.items_count == c, "W1 count round-trips" );
                for ( i = 0; i < c; i++ ) check( w1.items[i] == (uint8_t) ( i % 2 ), "W1 element round-trips" );
            }
            for ( c = 0; c <= 3; c++ )
            {
                memset( &w52, 0xEF, sizeof( w52 ) );
                CONSUME( 2 + 52 * c, read_w52( &rs, &w52 ) );
                check( w52.items_count == c, "W52 count round-trips" );
                for ( i = 0; i < c; i++ ) check( w52.items[i] == 4503599627370495ULL - (uint64_t) i * 123456789ULL, "W52 element round-trips" );
            }
            for ( c = 0; c <= 3; c++ )
            {
                memset( &w50, 0xEF, sizeof( w50 ) );
                CONSUME( 2 + 50 * c, read_w50( &rs, &w50 ) );
                check( w50.items_count == c, "W50 count round-trips" );
                for ( i = 0; i < c; i++ ) check( w50.items[i] == 1125899906842623ULL - (uint64_t) i * 987654321ULL, "W50 element round-trips" );
            }
            memset( &f13, 0xEF, sizeof( f13 ) );
            CONSUME( 91, read_f13( &rs, &f13 ) );
            for ( i = 0; i < 7; i++ ) check( f13.items[i] == (uint16_t) ( 8191 - i * 911 ), "F13 element round-trips" );

            for ( k = 0; k < 6; k++ )
            {
                c = tri_counts[k];
                memset( &tri, 0xEF, sizeof( tri ) );
                CONSUME( 4 + 3 * c, read_arr_tri3( &rs, &tri ) );
                check( tri.items_count == c, "ArrTri3 count round-trips" );
                for ( i = 0; i < c; i++ )
                    check( tri.items[i].a == (uint32_t) ( i % 2 ) && tri.items[i].b == (uint32_t) ( i % 4 ), "ArrTri3 element round-trips" );
            }

            memset( &eleven, 0xEF, sizeof( eleven ) );
            CONSUME( 99, read_arr_eleven( &rs, &eleven ) );
            for ( i = 0; i < 9; i++ )
                check( eleven.items[i].a == (uint32_t) ( i % 8 ) && eleven.items[i].b == (uint32_t) ( 255 - i * 17 ), "ArrEleven element round-trips" );

            for ( k = 0; k <= 2; k++ )
            {
                memset( &hu, 0xEF, sizeof( hu ) );
                CONSUME( 14, read_holds_empty_union( &rs, &hu ) );
                check( hu.lead == 21 && hu.tail == 99 && hu.u.type == (EmptyUnionType) k, "HoldsEmptyUnion round-trips" );
            }

            for ( k = 0; k <= 2; k++ )
            {
                memset( &strs, 0xEF, sizeof( strs ) );
                CONSUME( strs_bits[k], read_strs( &rs, &strs ) );
                check( strs.lead == 21 && strs.tail == 5, "Strs lead and tail round-trip" );
                if ( k == 0 ) check( strs.s_length == 0 && strs.b_length == 0, "Strs empty round-trips" );
                if ( k == 1 ) check( strs.s_length == 8 && strcmp( strs.s, "abcdefgh" ) == 0 && strs.b_length == 8 && strs.b[7] == 0xF7, "Strs full round-trips" );
                if ( k == 2 ) check( strs.s_length == 3 && strcmp( strs.s, "xyz" ) == 0 && strs.b_length == 3 && strs.b[2] == 3, "Strs partial round-trips" );
            }

            for ( c = 0; c <= 4; c++ )
            {
                memset( &nested, 0xEF, sizeof( nested ) );
                CONSUME( 11 + 11 * c, read_arr_nested( &rs, &nested ) );
                check( nested.items_count == c && nested.lead == 21 && nested.tail == 5, "ArrNested round-trips" );
                for ( i = 0; i < c; i++ )
                    check( nested.items[i].a == (uint32_t) ( i % 8 ) && nested.items[i].b == (uint32_t) ( 200 - i * 7 ), "ArrNested element round-trips" );
            }

            memset( &sole, 0xEF, sizeof( sole ) );
            CONSUME( 13, read_sole( &rs, &sole ) );
            check( sole.only == 5555, "Sole round-trips" );
            check( read_off == stream_len, "the clauses reads consume the whole golden" );
        }

        /* ---- Joins.schema ----

           Every branch is written on BOTH arms, so no path is pinned by
           omission. The expected value after a round trip is not the value
           written: the untaken side reads back as zero (SPEC §5). */
        stream_len = 0;
        read_off = 0;
        {
            ArmsAgree agree; ArmsDisagree disagree; ArmEmpty arm_empty;
            ArmAlign align_str, align_empty; ArmsNested an; ArmArray aa;
            HoldsUneven hun; ArrUneven au; RegainAfterAlign ra;
            static const int uneven_item_bits[] = { 0, 5, 44, 49 };
            static const int uneven_bits[] = { 18, 21, 55 };
            int k, after_align;

            for ( f = 0; f <= 1; f++ )
            {
                /* the arms agree on WIDTH but not on value, so a join that
                   keeps the wrong arm is a value mismatch, not just a width one */
                memset( &agree, 0, sizeof( agree ) );
                agree.lead = 21; agree.flag = f; agree.a = 1234; agree.b = 1500; agree.tail = 99;
                EMIT( 24, write_arms_agree( &ws, &agree ) );

                memset( &disagree, 0, sizeof( disagree ) );
                disagree.lead = 21; disagree.flag = f; disagree.a = 1234; disagree.b = 5; disagree.tail = 99;
                EMIT( f ? 24 : 16, write_arms_disagree( &ws, &disagree ) );

                memset( &arm_empty, 0, sizeof( arm_empty ) );
                arm_empty.lead = 21; arm_empty.flag = f; arm_empty.a = 456789; arm_empty.tail = 99;
                EMIT( f ? 32 : 13, write_arm_empty( &ws, &arm_empty ) );

                memset( &align_str, 0, sizeof( align_str ) );
                align_str.lead = 21; align_str.flag = f; memcpy( align_str.s, "abcd", 4 );
                align_str.s_length = 4; align_str.b = 1000; align_str.tail = 99;
                EMIT( f ? 55 : 23, write_arm_align( &ws, &align_str ) );

                memset( &align_empty, 0, sizeof( align_empty ) );
                align_empty.lead = 21; align_empty.flag = f; align_empty.b = 1000; align_empty.tail = 99;
                EMIT( 23, write_arm_align( &ws, &align_empty ) );
            }

            for ( o = 0; o <= 1; o++ )
            {
                for ( in = 0; in <= 1; in++ )
                {
                    memset( &an, 0, sizeof( an ) );
                    an.lead = 5; an.outer = o; an.inner = in;
                    an.x = 500000000; an.y = 17; an.z = 4000; an.tail = 33;
                    EMIT( o ? ( in ? 40 : 16 ) : 23, write_arms_nested( &ws, &an ) );
                }
            }

            for ( f = 0; f <= 1; f++ )
            {
                for ( c = 0; c <= 3; c++ )
                {
                    memset( &aa, 0, sizeof( aa ) );
                    aa.lead = 21; aa.flag = f; aa.items_count = c; aa.b = 300; aa.tail = 99;
                    for ( i = 0; i < c; i++ ) aa.items[i] = (uint16_t) ( 8191 - i * 777 );
                    EMIT( f ? 15 + 13 * c : 22, write_arm_array( &ws, &aa ) );
                }
            }

            /* lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits */
            for ( k = 0; k <= 2; k++ )
            {
                memset( &hun, 0, sizeof( hun ) );
                hun.lead = 21; hun.tail = 1500;
                hun.u.type = (UnevenType) k;
                if ( k == 1 ) hun.u.as.narrow.n = 5;
                if ( k == 2 ) hun.u.as.wide.w = 123456789012ULL;
                EMIT( uneven_bits[k], write_holds_uneven( &ws, &hun ) );
            }

            /* alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37) */
            for ( c = 0; c <= 3; c++ )
            {
                memset( &au, 0, sizeof( au ) );
                au.lead = 21; au.tail = 5; au.items_count = c;
                for ( i = 0; i < c; i++ )
                {
                    if ( i % 2 == 0 )
                    {
                        au.items[i].type = UNEVEN_TYPE_NARROW;
                        au.items[i].as.narrow.n = (uint32_t) ( i % 8 );
                    }
                    else
                    {
                        au.items[i].type = UNEVEN_TYPE_WIDE;
                        au.items[i].as.wide.w = 99887766554ULL + (uint64_t) i;
                    }
                }
                EMIT( 10 + uneven_item_bits[c], write_arr_uneven( &ws, &au ) );
            }

            /* lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s,
               then a 32 + 29 + 19 + 4 static run after the align regains it */
            for ( c = 0; c <= 3; c++ )
            {
                for ( sl = 0; sl <= 4; sl += 4 )
                {
                    memset( &ra, 0, sizeof( ra ) );
                    ra.lead = 21; ra.items_count = c; ra.s_length = sl;
                    if ( sl ) memcpy( ra.s, "wxyz", 4 );
                    for ( i = 0; i < c; i++ ) ra.items[i] = (uint16_t) ( 8191 - i * 999 );
                    ra.p = 0xDEADBEEFu; ra.q = ( 1u << 29 ) - 7; ra.r = ( 1u << 19 ) - 3; ra.tail = 9;
                    after_align = ( ( 5 + 2 + 13 * c + 3 ) + 7 ) / 8 * 8;
                    EMIT( after_align + 8 * sl + 84, write_regain_after_align( &ws, &ra ) );
                }
            }

            golden_wire( "joins", arrangement, stream_len );

            for ( f = 0; f <= 1; f++ )
            {
                memset( &agree, 0xEF, sizeof( agree ) );
                CONSUME( 24, read_arms_agree( &rs, &agree ) );
                check( agree.lead == 21 && agree.flag == f && agree.tail == 99, "ArmsAgree round-trips" );
                check( f ? ( agree.a == 1234 && agree.b == 0 ) : ( agree.b == 1500 && agree.a == 0 ),
                       "ArmsAgree's untaken side reads as zero (SPEC §5)" );

                memset( &disagree, 0xEF, sizeof( disagree ) );
                CONSUME( f ? 24 : 16, read_arms_disagree( &rs, &disagree ) );
                check( disagree.lead == 21 && disagree.tail == 99, "ArmsDisagree round-trips" );
                check( f ? ( disagree.a == 1234 && disagree.b == 0 ) : ( disagree.b == 5 && disagree.a == 0 ),
                       "ArmsDisagree's untaken side reads as zero" );

                memset( &arm_empty, 0xEF, sizeof( arm_empty ) );
                CONSUME( f ? 32 : 13, read_arm_empty( &rs, &arm_empty ) );
                check( arm_empty.lead == 21 && arm_empty.tail == 99, "ArmEmpty round-trips" );
                check( arm_empty.a == ( f ? 456789u : 0u ), "ArmEmpty's absent arm reads as zero" );

                memset( &align_str, 0xEF, sizeof( align_str ) );
                CONSUME( f ? 55 : 23, read_arm_align( &rs, &align_str ) );
                check( align_str.lead == 21 && align_str.tail == 99, "ArmAlign round-trips" );
                check( f ? ( align_str.s_length == 4 && strcmp( align_str.s, "abcd" ) == 0 && align_str.b == 0 )
                         : ( align_str.b == 1000 && align_str.s_length == 0 ),
                       "ArmAlign's untaken side reads as zero" );

                memset( &align_empty, 0xEF, sizeof( align_empty ) );
                CONSUME( 23, read_arm_align( &rs, &align_empty ) );
                check( align_empty.lead == 21 && align_empty.tail == 99, "ArmAlign with an empty string round-trips" );
                check( f ? ( align_empty.s_length == 0 && align_empty.b == 0 ) : ( align_empty.b == 1000 ),
                       "ArmAlign's empty string round-trips" );
            }

            for ( o = 0; o <= 1; o++ )
            {
                for ( in = 0; in <= 1; in++ )
                {
                    memset( &an, 0xEF, sizeof( an ) );
                    CONSUME( o ? ( in ? 40 : 16 ) : 23, read_arms_nested( &rs, &an ) );
                    check( an.lead == 5 && an.tail == 33 && an.outer == o, "ArmsNested round-trips" );
                    if ( o )
                    {
                        check( an.inner == in && an.z == 0, "ArmsNested's outer arm round-trips" );
                        check( in ? ( an.x == 500000000u && an.y == 0 ) : ( an.y == 17 && an.x == 0 ),
                               "ArmsNested's inner arm round-trips" );
                    }
                    else
                    {
                        check( an.z == 4000 && an.x == 0 && an.y == 0, "ArmsNested's else arm round-trips" );
                    }
                }
            }

            for ( f = 0; f <= 1; f++ )
            {
                for ( c = 0; c <= 3; c++ )
                {
                    memset( &aa, 0xEF, sizeof( aa ) );
                    CONSUME( f ? 15 + 13 * c : 22, read_arm_array( &rs, &aa ) );
                    check( aa.lead == 21 && aa.tail == 99, "ArmArray round-trips" );
                    if ( f )
                    {
                        check( aa.items_count == c && aa.b == 0, "ArmArray's array arm round-trips" );
                        for ( i = 0; i < c; i++ ) check( aa.items[i] == (uint16_t) ( 8191 - i * 777 ), "ArmArray element round-trips" );
                    }
                    else
                    {
                        check( aa.b == 300 && aa.items_count == 0, "ArmArray's scalar arm round-trips" );
                    }
                }
            }

            for ( k = 0; k <= 2; k++ )
            {
                memset( &hun, 0xEF, sizeof( hun ) );
                CONSUME( uneven_bits[k], read_holds_uneven( &rs, &hun ) );
                check( hun.lead == 21 && hun.tail == 1500 && hun.u.type == (UnevenType) k, "HoldsUneven round-trips" );
                if ( k == 1 ) check( hun.u.as.narrow.n == 5, "HoldsUneven's narrow arm round-trips" );
                if ( k == 2 ) check( hun.u.as.wide.w == 123456789012ULL, "HoldsUneven's wide arm round-trips" );
            }

            for ( c = 0; c <= 3; c++ )
            {
                memset( &au, 0xEF, sizeof( au ) );
                CONSUME( 10 + uneven_item_bits[c], read_arr_uneven( &rs, &au ) );
                check( au.items_count == c && au.lead == 21 && au.tail == 5, "ArrUneven round-trips" );
                for ( i = 0; i < c; i++ )
                {
                    if ( i % 2 == 0 )
                        check( au.items[i].type == UNEVEN_TYPE_NARROW && au.items[i].as.narrow.n == (uint32_t) ( i % 8 ), "ArrUneven narrow element round-trips" );
                    else
                        check( au.items[i].type == UNEVEN_TYPE_WIDE && au.items[i].as.wide.w == 99887766554ULL + (uint64_t) i, "ArrUneven wide element round-trips" );
                }
            }

            for ( c = 0; c <= 3; c++ )
            {
                for ( sl = 0; sl <= 4; sl += 4 )
                {
                    memset( &ra, 0xEF, sizeof( ra ) );
                    after_align = ( ( 5 + 2 + 13 * c + 3 ) + 7 ) / 8 * 8;
                    CONSUME( after_align + 8 * sl + 84, read_regain_after_align( &rs, &ra ) );
                    check( ra.lead == 21 && ra.items_count == c && ra.s_length == sl, "RegainAfterAlign round-trips" );
                    check( ra.p == 0xDEADBEEFu && ra.q == ( 1u << 29 ) - 7 && ra.r == ( 1u << 19 ) - 3 && ra.tail == 9,
                           "RegainAfterAlign's static run after the align round-trips" );
                    for ( i = 0; i < c; i++ ) check( ra.items[i] == (uint16_t) ( 8191 - i * 999 ), "RegainAfterAlign element round-trips" );
                }
            }
            check( read_off == stream_len, "the joins reads consume the whole golden" );
        }

#undef EMIT
#undef CONSUME
    }

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "OK\n" );
    return 0;
}
