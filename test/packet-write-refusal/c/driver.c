/* C packet Write refusal surface (docs/SPEC.md §5.1, write-refusal-packet).
   Every shared case starts from the type's default value and changes ONE
   field; it runs in a child, so a debug assert is observed as the refusal
   (an abort) rather than ending the case list. A "wrote" twin must be
   written, read back and compare equal. The driver fails on any case in
   cases.txt it has no setter for, and on any setter cases.txt does not name.
   Cases are loaded before any child starts: the child shares the open file
   description, and an abort rewinds a buffered read. */
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "RefusalPacketWire.h"

typedef packetwriterefusal_packet_write_refusal_t value_t;

static void set_text( value_t * v, int length, char fill )
{
    int i;
    for ( i = 0; i < length && i < 8; i++ ) { v->text[i] = fill; }
    v->text_length = length;
}

static void set_scalar_high( value_t * v ) { v->scalar = 11; }
static void set_scalar_low( value_t * v ) { v->scalar = -1; }
static void set_scalar_at_max( value_t * v ) { v->scalar = 10; }
static void set_scalar_at_min( value_t * v ) { v->scalar = 0; }
static void set_bits_wide( value_t * v ) { v->narrow = 200; }
static void set_bits_at_max( value_t * v ) { v->narrow = 127; }
static void set_enum_high( value_t * v ) { v->grade = (packetwriterefusal_grade_t) 4; }
static void set_enum_at_max( value_t * v ) { v->grade = PACKETWRITEREFUSAL_GRADE_GOLD; }
static void set_union_tag_high( value_t * v ) { v->shot.type = (packetwriterefusal_shot_type_t) 3; }
static void set_union_tag_at_max( value_t * v ) { v->shot.type = PACKETWRITEREFUSAL_SHOT_TYPE_MAX; }
static void set_count_high( value_t * v ) { v->window_count = 9; }
static void set_count_low( value_t * v ) { v->window_count = 1; }
static void set_count_at_max( value_t * v ) { v->window_count = 8; }
static void set_count_at_min( value_t * v ) { v->window_count = 2; }
static void set_text_long( value_t * v ) { set_text( v, 8, 'a' ); v->text[8] = 'a'; v->text_length = 9; }
static void set_text_illformed( value_t * v ) { v->text[0] = (char) 0xff; v->text_length = 1; }
static void set_text_zero( value_t * v ) { v->text[0] = 0; v->text_length = 1; }
static void set_text_at_max( value_t * v ) { set_text( v, 8, 'a' ); }
static void set_const_wrong( value_t * v ) { v->fixed_constant = 6; }
static void set_const_at( value_t * v ) { v->fixed_constant = 5; }
static void set_reserved_wrong( value_t * v ) { v->reserved_zero = 1; }
static void set_reserved_at( value_t * v ) { v->reserved_zero = 0; }

static const struct
{
    const char * name;
    void ( *set )( value_t * );
} setters[] = {
    { "scalar_high", set_scalar_high }, { "scalar_low", set_scalar_low },
    { "scalar_at_max", set_scalar_at_max }, { "scalar_at_min", set_scalar_at_min },
    { "bits_wide", set_bits_wide }, { "bits_at_max", set_bits_at_max },
    { "enum_high", set_enum_high }, { "enum_at_max", set_enum_at_max },
    { "union_tag_high", set_union_tag_high }, { "union_tag_at_max", set_union_tag_at_max },
    { "count_high", set_count_high }, { "count_low", set_count_low },
    { "count_at_max", set_count_at_max }, { "count_at_min", set_count_at_min },
    { "text_long", set_text_long }, { "text_illformed", set_text_illformed },
    { "text_zero", set_text_zero }, { "text_at_max", set_text_at_max },
    { "const_wrong", set_const_wrong }, { "const_at", set_const_at },
    { "reserved_wrong", set_reserved_wrong }, { "reserved_at", set_reserved_at },
};
#define SETTER_COUNT ( (int) ( sizeof( setters ) / sizeof( setters[0] ) ) )

static char * trim( char * text )
{
    char * end;
    while ( *text == ' ' || *text == '\t' ) { text++; }
    end = text + strlen( text );
    while ( end > text && ( end[-1] == ' ' || end[-1] == '\t' || end[-1] == '\r' || end[-1] == '\n' ) ) { *--end = 0; }
    return text;
}

static int run_case( int setter )
{
    uint32_t encoded[64] = {0};
    value_t value, decoded;
    serialize_write_stream_t writer;
    serialize_read_stream_t reader;
    int written;

    value = packetwriterefusal_new_packet_write_refusal();
    setters[setter].set( &value );

    serialize_write_stream_init( &writer, (unsigned char *) encoded, (int) sizeof( encoded ) );
    if ( !packetwriterefusal_write_packet_write_refusal( &writer, &value ) ) { return 10; }
    serialize_write_flush( &writer );
    written = serialize_write_bytes_processed( &writer );

    memset( &decoded, 0, sizeof( decoded ) );
    serialize_read_stream_init( &reader, (const unsigned char *) encoded, written );
    if ( !packetwriterefusal_read_packet_write_refusal( &reader, &decoded ) ) { return 11; }
    if ( decoded.scalar != value.scalar || decoded.narrow != value.narrow || decoded.grade != value.grade
         || decoded.window_count != value.window_count || decoded.text_length != value.text_length
         || memcmp( decoded.text, value.text, (size_t) value.text_length ) != 0 || decoded.shot.type != value.shot.type
         || decoded.fixed_constant != value.fixed_constant || decoded.reserved_zero != value.reserved_zero ) { return 12; }
    /* the read ends at the byte boundary the final-padding check consumes */
    if ( serialize_read_bits_processed( &reader ) != (serialize_int64_t) ( ( serialize_write_bits_processed( &writer ) + 7 ) / 8 ) * 8 ) { return 13; }
    return 0;
}

struct loaded_case
{
    char name[64];
    char verdict[32];
    int setter;
};

int main( int argc, char ** argv )
{
    FILE * cases;
    char line[512];
    struct loaded_case loaded[64];
    int named[SETTER_COUNT];
    int count = 0;
    int failed = 0;
    int refused = 0;
    int wrote = 0;
    int i, j;
    memset( named, 0, sizeof( named ) );
    if ( argc != 2 ) { fprintf( stderr, "usage: %s cases.txt\n", argv[0] ); return 2; }
    cases = fopen( argv[1], "r" );
    if ( cases == NULL ) { perror( argv[1] ); return 2; }
    while ( fgets( line, sizeof( line ), cases ) != NULL )
    {
        char * name;
        char * verdict;
        char * first = trim( line );
        int setter = -1;
        if ( *first == 0 || *first == '#' ) { continue; }
        name = strtok( first, "|" );
        verdict = strtok( NULL, "|" );
        if ( name == NULL || verdict == NULL ) { fprintf( stderr, "FAILED: packet-write-refusal: invalid case row\n" ); failed = 1; break; }
        name = trim( name ); verdict = trim( verdict );
        for ( j = 0; j < SETTER_COUNT; j++ ) { if ( strcmp( setters[j].name, name ) == 0 ) { setter = j; } }
        if ( setter < 0 ) { fprintf( stderr, "FAILED: packet-write-refusal %s: neither run nor named not representable in C\n", name ); failed = 1; continue; }
        if ( count >= (int) ( sizeof( loaded ) / sizeof( loaded[0] ) ) || strlen( name ) >= sizeof( loaded[0].name ) || strlen( verdict ) >= sizeof( loaded[0].verdict ) )
        {
            fprintf( stderr, "FAILED: packet-write-refusal: invalid case row\n" );
            failed = 1;
            break;
        }
        strcpy( loaded[count].name, name );
        strcpy( loaded[count].verdict, verdict );
        loaded[count].setter = setter;
        named[setter] = 1;
        count++;
    }
    if ( ferror( cases ) ) { perror( "read cases" ); failed = 1; }
    fclose( cases );
    for ( j = 0; j < SETTER_COUNT; j++ )
    {
        if ( !named[j] ) { fprintf( stderr, "FAILED: packet-write-refusal %s: driver case missing from cases.txt\n", setters[j].name ); failed = 1; }
    }
    if ( failed ) { return 1; }

    for ( i = 0; i < count; i++ )
    {
        pid_t child;
        int wait_status;

        child = fork();
        if ( child < 0 ) { perror( "fork" ); return 1; }
        if ( child == 0 )
        {
            (void) freopen( "/dev/null", "w", stderr );
            _exit( run_case( loaded[i].setter ) );
        }
        if ( waitpid( child, &wait_status, 0 ) < 0 ) { perror( "waitpid" ); return 1; }
        if ( strcmp( loaded[i].verdict, "refused" ) == 0 )
        {
            if ( !WIFSIGNALED( wait_status ) || WTERMSIG( wait_status ) != SIGABRT )
            {
                fprintf( stderr, "FAILED: packet-write-refusal %s: expected debug assert, child status %d\n", loaded[i].name, wait_status );
                failed = 1;
            }
            refused++;
        }
        else if ( strcmp( loaded[i].verdict, "wrote" ) == 0 )
        {
            if ( !WIFEXITED( wait_status ) || WEXITSTATUS( wait_status ) != 0 )
            {
                fprintf( stderr, "FAILED: packet-write-refusal %s: expected a written, read-back twin, child status %d\n", loaded[i].name, wait_status );
                failed = 1;
            }
            wrote++;
        }
        else { fprintf( stderr, "FAILED: packet-write-refusal %s: invalid verdict\n", loaded[i].name ); failed = 1; }
    }
    if ( failed ) { return 1; }
    printf( "packet-write-refusal C: %d cases, %d refused, %d written\n", count, refused, wrote );
    return 0;
}
