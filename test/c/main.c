/*
    The C corpus test — the fifth target's half of the cross-language gate.

    Every golden here is the SAME file the C++, C#, Go and Rust tests are held
    to. That is the whole point: a target that only checks itself proves
    nothing about the property this project exists to provide.
*/

#include <math.h> /* the gate-7 discrimination tripwires (fmaf, floor) */
#include <stdio.h>
#include <string.h>

#include "DegenerateWire.h"
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


int main( void )
{
    static unsigned char buffer[8192 + 8];      /* + 8: read buffer allocations extend 8 bytes past the data (serialize.c loads 64-bit windows) */
    serialize_write_stream_t w;
    serialize_read_stream_t r;

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

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "OK\n" );
    return 0;
}
