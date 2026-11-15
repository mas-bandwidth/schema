/* Constructor storage and the C++ packet oracle, including reused storage. */
#include <stdio.h>
#include <string.h>
#include "DefaultsWire.h"

static int failed;
static const uint8_t name_bytes[] = { 0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80 };
static const uint8_t token_bytes[] = { 0x5c, 0x6e, 0x5c, 0x74 };

static int check( int condition, const char * name )
{
    if ( !condition ) { fprintf( stderr, "FAILED: %s\n", name ); failed = 1; }
    return condition;
}

static packetdefaults_sample_t expected_sample( void )
{
    packetdefaults_sample_t value;
    memset( &value, 0, sizeof( value ) );
    memcpy( value.name, name_bytes, sizeof( name_bytes ) );
    value.name_length = 6;
    memcpy( value.token, token_bytes, sizeof( token_bytes ) );
    value.token_length = 4;
    value.caps = 5;
    return value;
}

static packetdefaults_sample_t short_sample( void )
{
    packetdefaults_sample_t value;
    memset( &value, 0, sizeof( value ) );
    value.name[0] = 'A'; value.name_length = 1;
    value.token[1] = 0xff; value.token_length = 2;
    value.caps = 2;
    return value;
}

static packetdefaults_sample_t dirty_sample( void )
{
    packetdefaults_sample_t value = expected_sample();
    memcpy( value.name, "dirty!", 6 );
    memset( value.token, 0xa1, sizeof( value.token ) );
    value.caps = 7;
    memcpy( value.empty_name, "old", 3 ); value.empty_name_length = 3;
    memset( value.empty_token, 0xb1, sizeof( value.empty_token ) ); value.empty_token_length = 2;
    value.empty_caps = 7;
    return value;
}

/* Compare fields, not padding or inactive union storage. */
static int equal_sample( const packetdefaults_sample_t * a, const packetdefaults_sample_t * b )
{
    return a->name_length == b->name_length && memcmp( a->name, b->name, sizeof( a->name ) ) == 0 &&
           a->token_length == b->token_length && memcmp( a->token, b->token, sizeof( a->token ) ) == 0 &&
           a->caps == b->caps && a->empty_caps == b->empty_caps &&
           a->empty_name_length == b->empty_name_length && memcmp( a->empty_name, b->empty_name, sizeof( a->empty_name ) ) == 0 &&
           a->empty_token_length == b->empty_token_length && memcmp( a->empty_token, b->empty_token, sizeof( a->empty_token ) ) == 0;
}

static int equal_batch( const packetdefaults_batch_t * a, const packetdefaults_batch_t * b )
{
    int i;
    if ( a->counted_count != b->counted_count || !equal_sample( &a->head, &b->head ) ) return 0;
    for ( i = 0; i < 2; i++ ) if ( !equal_sample( &a->items[i], &b->items[i] ) ) return 0;
    for ( i = 0; i < 3; i++ ) if ( !equal_sample( &a->counted[i], &b->counted[i] ) ) return 0;
    return 1;
}

static int equal_zero_count( const packetdefaults_zero_count_t * a, const packetdefaults_zero_count_t * b )
{
    return a->items_count == b->items_count && equal_sample( &a->items[0], &b->items[0] ) &&
           equal_sample( &a->items[1], &b->items[1] );
}

static int equal_conditional( const packetdefaults_conditional_t * a, const packetdefaults_conditional_t * b )
{
    return a->enabled == b->enabled && equal_sample( &a->value, &b->value );
}

static int equal_choice( const packetdefaults_choice_t * a, const packetdefaults_choice_t * b )
{
    if ( a->type != b->type ) return 0;
    if ( a->type == PACKETDEFAULTS_CHOICE_TYPE_SAMPLE ) return equal_sample( &a->as.sample, &b->as.sample );
    if ( a->type == PACKETDEFAULTS_CHOICE_TYPE_CONDITIONAL ) return equal_conditional( &a->as.conditional, &b->as.conditional );
    return a->type == PACKETDEFAULTS_CHOICE_TYPE_NONE;
}

typedef int ( * Writer )( serialize_write_stream_t *, const void * );
typedef int ( * Reader )( serialize_read_stream_t *, void * );
typedef int ( * Equal )( const void *, const void * );

#define CODEC( Type, name ) \
    static int emit_##name( serialize_write_stream_t * s, const void * v ) { return packetdefaults_write_##name( s, (const Type *) v ); } \
    static int parse_##name( serialize_read_stream_t * s, void * v ) { return packetdefaults_read_##name( s, (Type *) v ); } \
    static int compare_##name( const void * a, const void * b ) { return equal_##name( (const Type *) a, (const Type *) b ); }
CODEC( packetdefaults_sample_t, sample )
CODEC( packetdefaults_batch_t, batch )
CODEC( packetdefaults_zero_count_t, zero_count )
CODEC( packetdefaults_conditional_t, conditional )
CODEC( packetdefaults_choice_t, choice )

static void read_check( const char * name, const void * wire, int bytes, int bits,
                        void * value, const void * want, Reader read, Equal equal )
{
    serialize_read_stream_t stream;
    serialize_read_stream_init( &stream, wire, bytes );
    if ( !check( read( &stream, value ), name ) ) return;
    check( serialize_read_bits_processed( &stream ) == bits, "packet-default read consumed bits" );
    check( equal( value, want ), name );
}

static void golden( const char * directory, const char * name, const void * value,
                    void * initial, const void * want, Writer write, Reader read, Equal equal )
{
    uint64_t written[512] = { 0 }, expected[512] = { 0 };
    serialize_write_stream_t stream;
    char path[1024];
    FILE * file;
    int bits = -1;
    size_t bytes;
    serialize_write_stream_init( &stream, written, sizeof( written ) );
    if ( !check( write( &stream, value ), name ) ) return;
    serialize_write_flush( &stream );
    snprintf( path, sizeof( path ), "%s/%s.bin", directory, name );
    file = fopen( path, "rb" );
    if ( !check( file != NULL, path ) ) return;
    bytes = fread( expected, 1, sizeof( expected ), file );
    check( !ferror( file ), "read golden bytes" );
    fclose( file );
    if ( !check( bytes <= sizeof( expected ) - 8, "golden has padded allocation" ) ) return;
    snprintf( path, sizeof( path ), "%s/%s.bits", directory, name );
    file = fopen( path, "rb" );
    if ( !check( file != NULL, path ) ) return;
    check( fscanf( file, "%d", &bits ) == 1, "read golden bit count" );
    fclose( file );
    check( serialize_write_bytes_processed( &stream ) == (int) bytes &&
           memcmp( written, expected, bytes ) == 0, name );
    check( serialize_write_bits_processed( &stream ) == bits, "packet-default write golden bits" );
    /* Decode the pinned C++ bytes, never the C writer's output. */
    read_check( name, expected, (int) bytes, bits, initial, want, read, equal );
}

static void choice_roundtrip( const char * name, const packetdefaults_choice_t * value, packetdefaults_choice_t * reused, const packetdefaults_choice_t * want )
{
    uint64_t buffer[512] = { 0 };
    serialize_write_stream_t stream;
    serialize_write_stream_init( &stream, buffer, sizeof( buffer ) );
    if ( !check( packetdefaults_write_choice( &stream, value ), name ) ) return;
    serialize_write_flush( &stream );
    read_check( name, buffer, serialize_write_bytes_processed( &stream ), serialize_write_bits_processed( &stream ),
                reused, want, parse_choice, compare_choice );
}

static void run( const char * directory )
{
    int i, attempt;
    packetdefaults_sample_t expected = expected_sample(), sample = packetdefaults_new_sample(), zero, initial, want;
    packetdefaults_batch_t batch = packetdefaults_new_batch(), batch_initial, batch_want;
    packetdefaults_zero_count_t zero_count = packetdefaults_new_zero_count(), zero_initial, zero_want;
    packetdefaults_conditional_t conditional = packetdefaults_new_conditional(), conditional_initial, conditional_want;
    packetdefaults_choice_t choice, choice_initial, choice_want;
    packetdefaults_empty_only_t empty = packetdefaults_new_empty_only();
    packetdefaults_prefix_t prefix = packetdefaults_new_prefix();
    packetdefaults_wide_mask_t wide = packetdefaults_new_wide_mask();
    const uint8_t prefix_name[6] = { 0xc3, 0xa9 };
    const uint8_t prefix_token[5] = { 0x5c, 0x6e };
    const uint8_t zeros[4] = { 0 };
    memset( &zero, 0, sizeof( zero ) );
    /* Stable marker for the constructor-byte sabotage. */
    check( equal_sample( &sample, &expected ), "packet-default constructor bytes" );
    check( !equal_sample( &zero, &sample ), "zero initialization differs from declared defaults" );
    check( empty.name_length == 0 && empty.token_length == 0 && empty.caps == 0 &&
           memcmp( empty.name, zeros, sizeof( empty.name ) ) == 0 &&
           memcmp( empty.token, zeros, sizeof( empty.token ) ) == 0, "explicit-empty constructor" );
    check( prefix.name_length == 2 && prefix.token_length == 2 &&
           memcmp( prefix.name, prefix_name, sizeof( prefix_name ) ) == 0 &&
           memcmp( prefix.token, prefix_token, sizeof( prefix_token ) ) == 0, "short literal backing tails and terminator" );
    check( wide.high == ( UINT64_C(1) << 63 ) && wide.all == UINT64_MAX, "unsigned high and full flags masks" );
    {
        uint64_t buffer[4] = { 0 };
        uint64_t oracle[4] = { 0 };
        const uint8_t bytes[16] = { 0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff };
        serialize_write_stream_t stream;
        serialize_read_stream_t reader;
        packetdefaults_wide_mask_t decoded;
        memset( &decoded, 0, sizeof( decoded ) );
        memcpy( oracle, bytes, sizeof( bytes ) );
        serialize_write_stream_init( &stream, buffer, sizeof( buffer ) );
        check( packetdefaults_write_wide_mask( &stream, &wide ), "wide flags write" );
        check( serialize_write_bits_processed( &stream ) == 128, "wide flags have no elision" );
        serialize_write_flush( &stream );
        check( memcmp( buffer, bytes, sizeof( bytes ) ) == 0, "wide flags literal wire" );
        serialize_read_stream_init( &reader, oracle, sizeof( bytes ) );
        check( packetdefaults_read_wide_mask( &reader, &decoded ) && serialize_read_bits_processed( &reader ) == 128 &&
               decoded.high == wide.high && decoded.all == wide.all, "wide flags literal read" );
    }
    initial = zero;
    {
        uint64_t buffer[4] = { 0 }, oracle[4] = { 0 };
        /* 5 | (1<<32)<<3 | 2<<36 = 0x2800000005, exactly 38 bits. */
        const uint8_t bytes[5] = { 0x05, 0, 0, 0, 0x28 };
        packetdefaults_split_mask_t split, decoded;
        serialize_write_stream_t writer;
        serialize_read_stream_t reader;
        memset( &split, 0, sizeof( split ) );
        memset( &decoded, 0, sizeof( decoded ) );
        split.lead = 5; split.mask = UINT64_C(1) << 32; split.tail = 2;
        memcpy( oracle, bytes, sizeof( bytes ) );
        serialize_write_stream_init( &writer, buffer, sizeof( buffer ) );
        check( packetdefaults_write_split_mask( &writer, &split ), "33-bit flags write" );
        check( serialize_write_bits_processed( &writer ) == 38, "33-bit flags spend exactly 33 bits" );
        serialize_write_flush( &writer );
        check( memcmp( buffer, bytes, sizeof( bytes ) ) == 0, "33-bit flags literal wire" );
        serialize_read_stream_init( &reader, oracle, sizeof( bytes ) );
        check( packetdefaults_read_split_mask( &reader, &decoded ) && serialize_read_bits_processed( &reader ) == 38 &&
               decoded.lead == 5 && decoded.mask == split.mask && decoded.tail == 2, "33-bit flags literal read" );
    }
    golden( directory, "sample-defaults", &sample, &initial, &expected, emit_sample, parse_sample, compare_sample );
    check( equal_sample( &batch.head, &expected ), "nested defaults" );
    for ( i = 0; i < 2; i++ ) check( equal_sample( &batch.items[i], &expected ), "fixed backing defaults" );
    for ( i = 0; i < 3; i++ ) check( equal_sample( &batch.counted[i], &expected ), "counted backing defaults" );
    check( batch.counted_count == 1, "nonzero born count" );
    batch_initial = batch;
    batch_initial.counted[1] = dirty_sample(); batch_initial.counted[2] = short_sample();
    batch_want = batch_initial;
    golden( directory, "batch-defaults", &batch, &batch_initial, &batch_want, emit_batch, parse_batch, compare_batch );
    check( zero_count.items_count == 0 && equal_sample( &zero_count.items[0], &expected ) &&
           equal_sample( &zero_count.items[1], &expected ), "zero-count backing defaults" );
    zero_initial.items[0] = dirty_sample(); zero_initial.items[1] = short_sample(); zero_initial.items_count = 2;
    zero_want = zero_initial; zero_want.items_count = 0;
    golden( directory, "zero-count", &zero_count, &zero_initial, &zero_want, emit_zero_count, parse_zero_count, compare_zero_count );
    check( conditional.enabled && equal_sample( &conditional.value, &expected ), "conditional defaults" );
    memset( &conditional_initial, 0, sizeof( conditional_initial ) );
    conditional_want = conditional;
    golden( directory, "conditional-on", &conditional, &conditional_initial, &conditional_want, emit_conditional, parse_conditional, compare_conditional );
    conditional.enabled = 0;
    conditional_initial.enabled = 1; conditional_initial.value = dirty_sample();
    memset( &conditional_want, 0, sizeof( conditional_want ) );
    golden( directory, "conditional-off", &conditional, &conditional_initial, &conditional_want, emit_conditional, parse_conditional, compare_conditional );
    memset( &choice, 0, sizeof( choice ) );
    check( choice.type == PACKETDEFAULTS_CHOICE_TYPE_NONE, "union zero tag is None" );
    choice.type = PACKETDEFAULTS_CHOICE_TYPE_SAMPLE; choice.as.sample = sample;
    choice_initial.type = PACKETDEFAULTS_CHOICE_TYPE_CONDITIONAL;
    choice_initial.as.conditional.enabled = 1; choice_initial.as.conditional.value = dirty_sample();
    choice_want = choice;
    golden( directory, "choice-sample", &choice, &choice_initial, &choice_want, emit_choice, parse_choice, compare_choice );
    initial = dirty_sample(); want = initial;
    want.name[0] = 'A'; want.name[1] = 0; want.name_length = 1;
    want.token[0] = 0; want.token[1] = 0xff; want.token_length = 2; want.caps = 2;
    want.empty_name[0] = 0; want.empty_name_length = 0; want.empty_token_length = 0; want.empty_caps = 0;
    sample = short_sample();
    golden( directory, "sample-short", &sample, &initial, &want, emit_sample, parse_sample, compare_sample );
    initial = dirty_sample(); want = initial;
    want.name[0] = 0; want.name_length = 0; want.token_length = 0; want.caps = 0;
    want.empty_name[0] = 0; want.empty_name_length = 0; want.empty_token_length = 0; want.empty_caps = 0;
    golden( directory, "sample-empty", &zero, &initial, &want, emit_sample, parse_sample, compare_sample );
    for ( i = 0; i < 2; i++ )
    {
        choice.type = PACKETDEFAULTS_CHOICE_TYPE_SAMPLE; choice.as.sample = i ? zero : short_sample();
        choice_want.type = PACKETDEFAULTS_CHOICE_TYPE_SAMPLE; choice_want.as.sample = expected;
        choice_want.as.sample.name_length = i ? 0 : 1;
        choice_want.as.sample.name[0] = i ? 0 : 'A';
        if ( !i ) choice_want.as.sample.name[1] = 0;
        choice_want.as.sample.token_length = i ? 0 : 2;
        if ( !i ) { choice_want.as.sample.token[0] = 0; choice_want.as.sample.token[1] = 0xff; }
        choice_want.as.sample.caps = i ? 0 : 2;
        choice_initial.type = PACKETDEFAULTS_CHOICE_TYPE_CONDITIONAL;
        for ( attempt = 0; attempt < 2; attempt++ )
        {
            choice_initial.as.sample = dirty_sample();
            choice_roundtrip( "union selection restores declared tails", &choice, &choice_initial, &choice_want );
        }
    }
    choice.type = PACKETDEFAULTS_CHOICE_TYPE_CONDITIONAL; choice.as.conditional = conditional;
    choice_want.type = PACKETDEFAULTS_CHOICE_TYPE_CONDITIONAL;
    memset( &choice_want.as.conditional, 0, sizeof( choice_want.as.conditional ) );
    choice_initial.type = PACKETDEFAULTS_CHOICE_TYPE_SAMPLE;
    for ( attempt = 0; attempt < 2; attempt++ )
    {
        choice_initial.as.conditional.enabled = 1; choice_initial.as.conditional.value = dirty_sample();
        choice_roundtrip( "union false branch clears defaults", &choice, &choice_initial, &choice_want );
    }
}

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { fprintf( stderr, "usage: %s <golden-directory>\n", argv[0] ); return 2; }
    run( argv[1] );
    if ( failed ) return 1;
    puts( "packet defaults C: constructors, eight C++ goldens and reused storage OK" );
    return 0;
}
