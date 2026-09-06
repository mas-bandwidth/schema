// THE MESSAGE FORM'S COST ROWS (docs/SPEC-TABLES.md §3.3), measured rather
// than derived: the bytes each form spends on the three backend messages and
// on the batch of three, and the read and write time the bitpacked body takes
// against the BYTE-FRAMED body over the same values.
//
// THE BYTE BODY THIS MEASURES AGAINST IS THE FILE FORM'S. §3.3 replaced the
// byte-framed message body in place, so it is not in the tree to time; the
// file form's body is byte-framed and §3.3 states it is the same body, "§3's
// byte-framed body is the FILE's body and does not move", differing only in
// where the id table sits. That difference is one table per buffer and it is
// named in the report rather than folded into the ratio.
//
// The loop is the same values, the same buffers and the same body count on
// both sides, and the reported figure is the BEST of several rounds, because
// a best-of rejects scheduler noise on a machine that is doing other work.

#include <stdio.h>
#include <string.h>
#include <time.h>

#include "BackendTable.h"

static int failures = 0;
#define CHECK( x ) do { if ( !( x ) ) { printf( "FAILED %s:%d: %s\n", __FILE__, __LINE__, #x ); failures++; } } while ( 0 )

using namespace backenddemo;

static void build_login( LoginRequest & value )
{
    value.player_id = 9007199254740993ull;
    for ( int32_t i = 0; i < 32; i++ ) { value.session_token[i] = (uint8_t) ( ( i * 7 + 3 ) & 0xFF ); }
    value.session_token_length = 32;
    value.client_build = 140233;
    value.region = Region::EU;
}

static void build_match( MatchResult & value )
{
    value.match_id = 72340172838076673ull;
    for ( int32_t i = 0; i < 10; i++ )
    {
        value.players[i].player_id = (uint64_t) ( 1000 + i );
        value.players[i].score = 1234 + i * 77;
        value.players[i].placement = (uint8_t) ( ( i % 10 ) + 1 );
    }
}

static void build_store( StorePurchase & value )
{
    value.player_id = 9007199254740993ull;
    const char * sku = "coins.pack.large.2500";
    const int32_t n = (int32_t) strlen( sku );
    memcpy( value.sku, sku, (size_t) n );
    value.sku[n] = 0;
    value.sku_length = n;
    value.quantity = 7;
    value.price_minor = 499;
    value.currency = Currency::EUR;
}

static double now_seconds()
{
    struct timespec ts;
    clock_gettime( CLOCK_MONOTONIC, &ts );
    return (double) ts.tv_sec + (double) ts.tv_nsec * 1e-9;
}

static const int kRounds = 7;
static const int kIterations = 100000;

static uint8_t file_buffer[1 << 16];
static uint8_t msg_buffer[1 << 16];

// TIME ONE VERB, best of kRounds, seconds per iteration.
#define TIME( best, body ) do { \
    best = 1e30; \
    for ( int round = 0; round < kRounds; round++ ) \
    { \
        const double t0 = now_seconds(); \
        for ( int it = 0; it < kIterations; it++ ) { body; } \
        const double dt = ( now_seconds() - t0 ) / (double) kIterations; \
        if ( dt < best ) { best = dt; } \
    } \
} while ( 0 )

static void print_row( const char * name, int64_t file_bytes, int64_t message_bytes,
    double write_file, double write_message, double read_file, double read_message )
{
    printf( "| `%s` | %lld | %lld | %.2fx | %.2fx |\n",
        name, (long long) file_bytes, (long long) message_bytes,
        write_message / write_file, read_message / read_file );
    printf( "      (write %.1f ns file, %.1f ns message; read %.1f ns file, %.1f ns message)\n",
        write_file * 1e9, write_message * 1e9, read_file * 1e9, read_message * 1e9 );
}

// ONE ROW over a FIXED root: the four timings and the two byte counts. The
// verbs take an ARRAY of values, so a batch of one is the value itself.
#define ROW( label, T, value ) do { \
    TableReport report; \
    int64_t fb = T##Save( value, file_buffer, sizeof( file_buffer ) ); \
    int64_t mb = T##SaveMessages( &value, 1, msg_buffer, sizeof( msg_buffer ), &report ); \
    CHECK( fb > 0 && mb > 0 ); \
    double wf, wm, rf, rm; \
    TIME( wf, T##Save( value, file_buffer, sizeof( file_buffer ) ) ); \
    TIME( wm, T##SaveMessages( &value, 1, msg_buffer, sizeof( msg_buffer ), &report ) ); \
    static T scratch; \
    int64_t count = 1; \
    TIME( rf, T##Load( scratch, file_buffer, fb, &report ) ); \
    TIME( rm, ( count = 1, T##LoadMessages( &scratch, &count, vocabulary, msg_buffer, mb, &report ) ) ); \
    CHECK( count == 1 && !report.malformed && !report.refused ); \
    print_row( label, fb, mb, wf, wm, rf, rm ); \
} while ( 0 )

int main()
{
    static uint8_t announcement[1 << 14];
    const int64_t announced = Announce( announcement, sizeof( announcement ) );
    CHECK( announced == AnnounceMeasure() );
    TableVocabulary vocabulary;
    CHECK( AnnounceRead( vocabulary, announcement, announced, NULL ) );

    static LoginRequest login;   LoginRequestReset( login );   build_login( login );
    static MatchResult match;    MatchResultReset( match );    build_match( match );
    static StorePurchase store;  StorePurchaseReset( store );  build_store( store );

    static Envelope three[3];
    for ( int i = 0; i < 3; i++ ) { EnvelopeReset( three[i] ); }
    three[0].payload.type = PayloadType::Login;
    build_login( three[0].payload.login );
    three[1].payload.type = PayloadType::Result;
    build_match( three[1].payload.result );
    three[2].payload.type = PayloadType::Purchase;
    build_store( three[2].payload.purchase );

    printf( "\n| instance | file form | bitpacked body | write factor | read factor |\n" );
    printf( "|---|---:|---:|---:|---:|\n" );

    ROW( "LoginRequest, full", LoginRequest, login );
    ROW( "MatchResult, full", MatchResult, match );
    ROW( "StorePurchase, full", StorePurchase, store );

    // THE BATCH: three envelopes in ONE buffer against the same three saved as
    // three separate FILES, which is what a byte-framed peer sends for three.
    {
        TableReport report;
        int64_t file_total = 0;
        for ( int i = 0; i < 3; i++ ) { file_total += EnvelopeSave( three[i], file_buffer, sizeof( file_buffer ) ); }
        const int64_t mb = EnvelopeSaveMessages( three, 3, msg_buffer, sizeof( msg_buffer ), &report );
        CHECK( mb > 0 );

        double wf, wm, rf, rm;
        TIME( wf, { for ( int i = 0; i < 3; i++ ) { EnvelopeSave( three[i], file_buffer, sizeof( file_buffer ) ); } } );
        TIME( wm, EnvelopeSaveMessages( three, 3, msg_buffer, sizeof( msg_buffer ), &report ) );

        // the file leg reads three separate files back, which is the same
        // three bodies and the same fields the batch carries
        static uint8_t files[3][1 << 14];
        int64_t sizes[3];
        for ( int i = 0; i < 3; i++ ) { sizes[i] = EnvelopeSave( three[i], files[i], sizeof( files[i] ) ); }
        static Envelope scratch[3];
        int64_t count = 3;
        TIME( rf, { for ( int i = 0; i < 3; i++ ) { EnvelopeLoad( scratch[i], files[i], sizes[i], &report ); } } );
        TIME( rm, ( count = 3, EnvelopeLoadMessages( scratch, &count, vocabulary, msg_buffer, mb, &report ) ) );
        CHECK( count == 3 && !report.malformed && !report.refused );
        print_row( "the three, one batch", file_total, mb, wf, wm, rf, rm );
    }

    printf( "\nannouncement: %lld bytes, %lld entries\n", (long long) announced, (long long) vocabulary.count );
    printf( "%d rounds of %d iterations, best of rounds\n", kRounds, kIterations );

    if ( failures != 0 ) { printf( "\n%d check(s) failed\n", failures ); return 1; }
    return 0;
}
