// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), included by main.cpp.
//
// A FILE carries its own id table and a MESSAGE STREAM announces one and then
// carries none. This section pins the announcement and the twelve vectors the
// page names, holds the byte counts it prints, and runs the refusals the form
// adds to §3's verdict.
//
// It is a header included once, into the one binary that owns the wire pins,
// because a golden with two homes is one golden that can disagree with itself.

#ifndef SCHEMA_TEST_MESSAGE_FORM_H
#define SCHEMA_TEST_MESSAGE_FORM_H

#include "BackendTable.h"
#include "VocabTable.h"

// ---- the six values, field for field (docs/SPEC-TABLES.md §3.3) ------------
//
// The three FULL vectors carry the measurement's own values, and their byte
// counts are the page's: 106 / 273 / 104 as files, 58 / 225 / 48 as messages.
// The three DEFAULT vectors touch nothing, so every field elides and the body
// is its terminator alone.

static void build_backend_login( backenddemo::LoginRequest & value )
{
    value.player_id = 9007199254740993ull;
    for ( int32_t i = 0; i < 32; i++ ) { value.session_token[i] = (uint8_t) ( ( i * 7 + 3 ) & 0xFF ); }
    value.session_token_length = 32;
    value.client_build = 140233;
    value.region = backenddemo::Region::EU;
}

static void build_backend_match( backenddemo::MatchResult & value )
{
    value.match_id = 72340172838076673ull;
    for ( int32_t i = 0; i < 10; i++ )
    {
        value.players[i].player_id = (uint64_t) ( 1000 + i );
        value.players[i].score = 1234 + i * 77;
        // placement runs 1..10, so ONE of the ten rows sits at its declared
        // default and elides — which is the row that puts elision INSIDE an
        // array element in this corpus, and is what the page's 273 counts
        value.players[i].placement = (uint8_t) ( ( i % 10 ) + 1 );
    }
}

static void build_backend_store( backenddemo::StorePurchase & value )
{
    value.player_id = 9007199254740993ull;
    set_string( value.sku, value.sku_length, "coins.pack.large.2500" );
    value.quantity = 7;
    value.price_minor = 499;
    value.currency = backenddemo::Currency::EUR;
}

// ---- the connection, and its two directions -------------------------------

static backenddemo::TableVocabulary backend_connection()
{
    backenddemo::TableVocabulary vocabulary;
    static uint8_t announcement[4096];
    const int64_t bytes = backenddemo::Announce( announcement, sizeof( announcement ) );
    CHECK( bytes == backenddemo::AnnounceMeasure() );
    CHECK( backenddemo::AnnounceRead( vocabulary, announcement, bytes, NULL ) );
    return vocabulary;
}

// pin_message_vector writes the two forms of ONE value and holds both counts.
// The FILE form rides every surface an instance rides; the MESSAGE form rides
// the wire surface alone, because its text is the file-form vector's byte for
// byte and a second json/ file would be one golden with two homes.
template <typename T, typename Measure, typename Save, typename MeasureMessage, typename SaveMessage>
static void pin_message_vector( const char * name, const T & value,
                                Measure measure, Save save,
                                MeasureMessage measure_message, SaveMessage save_message,
                                int64_t file_bytes, int64_t message_bytes )
{
    static uint8_t file[1u << 16];
    static uint8_t message[1u << 16];
    const int64_t wrote_file = save( value, file, sizeof( file ) );
    CHECK( wrote_file > 0 && wrote_file == measure( value ) );
    const int64_t wrote_message = save_message( value, message, sizeof( message ) );
    CHECK( wrote_message > 0 && wrote_message == measure_message( value ) );
    // THE PINNED BYTE COUNTS ARE THE PAGE'S TABLES: a vector whose count moves
    // is a wire that moved (docs/SPEC-TABLES.md §3.3).
    if ( wrote_file != file_bytes || wrote_message != message_bytes )
    {
        printf( "FAIL message vector %s: %lld/%lld bytes, the page pins %lld/%lld\n",
                name, (long long) wrote_file, (long long) wrote_message,
                (long long) file_bytes, (long long) message_bytes );
        failures++;
    }
    // A MESSAGE IS TWO PARTS: the form byte and the root body, and the body's
    // terminator is the message's last byte — there is no trailer at all.
    CHECK( wrote_message >= 2 && message[0] == 2 && message[wrote_message - 1] == 0 );
    CHECK( wrote_file >= 1 && file[0] == 1 );
    pin_table_golden( name, file, wrote_file );
    char message_name[128];
    snprintf( message_name, sizeof( message_name ), "%s_message", name );
    pin_table_golden( message_name, message, wrote_message );
}

static void test_message_form_goldens()
{
    // THE ANNOUNCEMENT: twenty-nine entries and 252 bytes, and it is an
    // ordinary form 1 FILE (docs/SPEC-TABLES.md §3.3).
    {
        static uint8_t announcement[4096];
        const int64_t bytes = backenddemo::Announce( announcement, sizeof( announcement ) );
        CHECK( bytes == 252 );
        CHECK( announcement[0] == 1 );
        pin_table_golden( "backend_conn", announcement, bytes );
        // and a buffer one byte short is refused rather than half-written
        CHECK( backenddemo::Announce( announcement, bytes - 1 ) == -1 );
    }

    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        pin_message_vector( "login_full", value,
                            backenddemo::LoginRequestMeasure, backenddemo::LoginRequestSave,
                            backenddemo::LoginRequestMeasureMessage, backenddemo::LoginRequestSaveMessage,
                            106, 58 );
    }
    {
        static backenddemo::LoginRequest value; // untouched: everything elides
        pin_message_vector( "login_default", value,
                            backenddemo::LoginRequestMeasure, backenddemo::LoginRequestSave,
                            backenddemo::LoginRequestMeasureMessage, backenddemo::LoginRequestSaveMessage,
                            10, 2 );
    }
    {
        static backenddemo::MatchResult value;
        build_backend_match( value );
        pin_message_vector( "match_full", value,
                            backenddemo::MatchResultMeasure, backenddemo::MatchResultSave,
                            backenddemo::MatchResultMeasureMessage, backenddemo::MatchResultSaveMessage,
                            273, 225 );
    }
    {
        static backenddemo::MatchResult value;
        pin_message_vector( "match_default", value,
                            backenddemo::MatchResultMeasure, backenddemo::MatchResultSave,
                            backenddemo::MatchResultMeasureMessage, backenddemo::MatchResultSaveMessage,
                            43, 27 );
    }
    {
        static backenddemo::StorePurchase value;
        build_backend_store( value );
        pin_message_vector( "store_full", value,
                            backenddemo::StorePurchaseMeasure, backenddemo::StorePurchaseSave,
                            backenddemo::StorePurchaseMeasureMessage, backenddemo::StorePurchaseSaveMessage,
                            104, 48 );
    }
    {
        static backenddemo::StorePurchase value;
        pin_message_vector( "store_default", value,
                            backenddemo::StorePurchaseMeasure, backenddemo::StorePurchaseSave,
                            backenddemo::StorePurchaseMeasureMessage, backenddemo::StorePurchaseSaveMessage,
                            10, 2 );
    }
}

// ---- the goldens READ BACK, against the connection's announced table -------

template <typename T, typename LoadMessage, typename SaveMessage, typename Reset>
static void reload_message_golden( const char * name, const backenddemo::TableVocabulary & vocabulary,
                                   LoadMessage load_message, SaveMessage save_message, Reset reset )
{
    char message_name[128];
    snprintf( message_name, sizeof( message_name ), "%s_message", name );
    const int64_t pinned = read_table_golden( message_name );
    if ( pinned < 0 ) { return; }
    T value;
    reset( value );
    backenddemo::TableReport report;
    if ( !load_message( value, vocabulary, golden_pinned, pinned, &report ) || report.malformed )
    {
        printf( "FAIL message golden %s does not load\n", message_name );
        failures++;
        return;
    }
    CHECK( !report.refused );
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 );
    CHECK( report.clamped == 0 && report.duplicate == 0 );
    const int64_t wrote = save_message( value, golden_again, sizeof( golden_again ) );
    if ( wrote != pinned || memcmp( golden_again, golden_pinned, (size_t) pinned ) != 0 )
    {
        printf( "FAIL message golden %s re-saves differently: %lld out, %lld pinned\n",
                message_name, (long long) wrote, (long long) pinned );
        failures++;
    }
}

static void test_message_form_reload()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    // THE BUILD VERSION KEYS THE TABLE and gates nothing: it is what the
    // announcement carries under the reserved id, and what a refusal names.
    CHECK( vocabulary.announced );
    CHECK( vocabulary.build_version == backenddemo::BuildVersion );
    CHECK( vocabulary.table.count == 29 );

    reload_message_golden<backenddemo::LoginRequest>( "login_full", vocabulary,
        backenddemo::LoginRequestLoadMessage, backenddemo::LoginRequestSaveMessage, backenddemo::LoginRequestReset );
    reload_message_golden<backenddemo::LoginRequest>( "login_default", vocabulary,
        backenddemo::LoginRequestLoadMessage, backenddemo::LoginRequestSaveMessage, backenddemo::LoginRequestReset );
    reload_message_golden<backenddemo::MatchResult>( "match_full", vocabulary,
        backenddemo::MatchResultLoadMessage, backenddemo::MatchResultSaveMessage, backenddemo::MatchResultReset );
    reload_message_golden<backenddemo::MatchResult>( "match_default", vocabulary,
        backenddemo::MatchResultLoadMessage, backenddemo::MatchResultSaveMessage, backenddemo::MatchResultReset );
    reload_message_golden<backenddemo::StorePurchase>( "store_full", vocabulary,
        backenddemo::StorePurchaseLoadMessage, backenddemo::StorePurchaseSaveMessage, backenddemo::StorePurchaseReset );
    reload_message_golden<backenddemo::StorePurchase>( "store_default", vocabulary,
        backenddemo::StorePurchaseLoadMessage, backenddemo::StorePurchaseSaveMessage, backenddemo::StorePurchaseReset );

    // AND THE VALUE SURVIVES: the message form carries the same value the file
    // form does, and the two differ in their reference bytes alone.
    {
        static backenddemo::LoginRequest wrote;
        build_backend_login( wrote );
        static uint8_t message[256];
        const int64_t bytes = backenddemo::LoginRequestSaveMessage( wrote, message, sizeof( message ) );
        backenddemo::LoginRequest read;
        backenddemo::TableReport report;
        CHECK( backenddemo::LoginRequestLoadMessage( read, vocabulary, message, bytes, &report ) );
        CHECK( read.player_id == wrote.player_id );
        CHECK( read.client_build == wrote.client_build );
        CHECK( read.region == wrote.region );
        CHECK( memcmp( read.session_token, wrote.session_token, 32 ) == 0 );
    }
}

// ---- A SLOT AT OR PAST 128 (docs/SPEC-TABLES.md §3.3) ----------------------
//
// The vocabdemo unit's vocabulary passes 127 ids, so a message over its LAST
// table names ids on both sides of the one-byte boundary. Red if a leg spells
// a reference non-minimally, or sizes a message as though every reference were
// one byte.

static void test_message_form_wide_vocabulary()
{
    vocabdemo::TableVocabulary vocabulary;
    static uint8_t announcement[8192];
    const int64_t announced = vocabdemo::Announce( announcement, sizeof( announcement ) );
    CHECK( announced == vocabdemo::AnnounceMeasure() );
    CHECK( vocabdemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
    CHECK( vocabulary.table.count > 127 );
    pin_table_golden( "vocab_conn", announcement, announced );

    static vocabdemo::Wide09 value;
    value.field_09_00 = 1;
    value.field_09_01 = 2;
    value.field_09_02 = 3;
    value.field_09_03 = 4;
    value.field_09_04 = 5;
    value.field_09_05 = 6;
    value.field_09_06 = 7;
    value.field_09_07 = 8;
    value.field_09_08 = 9;
    value.field_09_09 = 10;
    value.field_09_10 = 11;
    value.field_09_11 = 12;
    value.field_09_12 = 13;

    static uint8_t message[4096];
    const int64_t bytes = vocabdemo::Wide09SaveMessage( value, message, sizeof( message ) );
    CHECK( bytes > 0 && bytes == vocabdemo::Wide09MeasureMessage( value ) );
    pin_table_golden( "vocab_wide_message", message, bytes );
    // and its FILE form beside it, so the pair rides the manifest's message
    // line and the two forms can be compared under resolution
    static uint8_t file[4096];
    const int64_t file_bytes = vocabdemo::Wide09Save( value, file, sizeof( file ) );
    CHECK( file_bytes > 0 && file_bytes == vocabdemo::Wide09Measure( value ) );
    pin_table_golden( "vocab_wide", file, file_bytes );

    // THE ENCODING PAYS FOR WHAT IT SPELLS, and minimally: this body carries
    // thirteen field headers, some in one-byte slots and some in two-byte
    // ones, so a leg that sized every reference at one byte is short and one
    // that spelled a low slot non-minimally is long.
    int64_t one_byte = 0, two_byte = 0;
    for ( int64_t slot = 1; slot <= vocabulary.table.count; slot++ )
    {
        ( slot < 128 ? one_byte : two_byte )++;
    }
    CHECK( one_byte == 127 && two_byte == vocabulary.table.count - 127 );

    vocabdemo::Wide09 read;
    vocabdemo::TableReport report;
    CHECK( vocabdemo::Wide09LoadMessage( read, vocabulary, message, bytes, &report ) );
    CHECK( !report.refused && !report.malformed && report.unknown == 0 );
    CHECK( read.field_09_00 == 1 && read.field_09_12 == 13 );
    static uint8_t again[4096];
    CHECK( vocabdemo::Wide09SaveMessage( read, again, sizeof( again ) ) == bytes );
    CHECK( memcmp( again, message, (size_t) bytes ) == 0 );

    // and a message over the FIRST table names its ids in one-byte slots
    // alone, which is the other half of the boundary
    static vocabdemo::Wide00 low;
    low.field_00_00 = 5;
    static uint8_t low_message[256];
    const int64_t low_bytes = vocabdemo::Wide00SaveMessage( low, low_message, sizeof( low_message ) );
    static uint8_t low_file[256];
    pin_table_golden( "vocab_low", low_file, vocabdemo::Wide00Save( low, low_file, sizeof( low_file ) ) );
    // the form byte, one one-byte reference, its kind byte, a u32 and the
    // body's terminator: eight bytes, and every reference in it spelled in one
    CHECK( low_bytes == 8 );
    pin_table_golden( "vocab_low_message", low_message, low_bytes );
}

// ---- A POINTERED MESSAGE (docs/SPEC-TABLES.md §3.1, §3.3) ------------------
//
// The node table is a FIELD of the root body under the reserved node-table id,
// not part of the trailer, so it is inside what a form 2 message carries. Its
// records name their type ids through the connection's table like every other
// reference. Red if the node-table id or a type id is missing from the
// announcement, or if the numbering differs from the file form's.

static void test_message_form_pointered()
{
    graphdemo::TableVocabulary vocabulary;
    static uint8_t announcement[8192];
    const int64_t announced = graphdemo::Announce( announcement, sizeof( announcement ) );
    CHECK( graphdemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
    pin_table_golden( "graph_conn", announcement, announced );

    graphdemo::SceneBuilder builder;
    build_graph_tree( builder );
    CHECK( builder.Lock() );
    static uint8_t message[16384];
    const int64_t bytes = graphdemo::SceneSaveMessage( builder.AsConst(), message, sizeof( message ) );
    CHECK( bytes > 0 && bytes == graphdemo::SceneMeasureMessage( builder.AsConst() ) );
    CHECK( message[0] == 2 && message[bytes - 1] == 0 );
    pin_table_golden( "graph_tree_message", message, bytes );

    int64_t attribution = 0;
    const int64_t need = graphdemo::SceneLoadMeasure( vocabulary, message, bytes, &attribution );
    CHECK( need > 0 );
    static uint8_t region[65536];
    graphdemo::TableReport report;
    const graphdemo::Scene * root = graphdemo::SceneLoadMessage( region, need, vocabulary, message, bytes, &report );
    CHECK( root != NULL );
    CHECK( !report.refused && !report.malformed && report.unknown == 0 );
    if ( root != NULL )
    {
        CHECK( strcmp( root->name, "tree" ) == 0 );
        CHECK( root->version == 2 );
        const graphdemo::ListNode * head = graphdemo::ListNodeAt( root->head );
        CHECK( head != NULL && head->value == 1 );
        const graphdemo::TreeNode * top = graphdemo::TreeNodeAt( root->tree );
        CHECK( top != NULL && strcmp( top->label, "top" ) == 0 );
        // AND THE MESSAGE RE-SAVES to the same bytes, so the numbering the
        // load rebuilt is the numbering the save derived
        static uint8_t again[16384];
        const int64_t rewrote = graphdemo::SceneSaveMessage( root, again, sizeof( again ) );
        CHECK( rewrote == bytes && memcmp( again, message, (size_t) bytes ) == 0 );
    }
}

// ---- PER-DIRECTION INDEPENDENCE (docs/SPEC-TABLES.md §3.3) -----------------
//
// A peer holds TWO tables for a connection, the one it writes with and the one
// it reads with, and neither is the other's. Two peers at different units is
// the ordinary case, and each decodes the other's messages against the table
// THAT peer announced. Red if a leg resolves a message against its own table.

static void test_message_form_two_peers()
{
    // peer A speaks backenddemo, peer B speaks vocabdemo
    const backenddemo::TableVocabulary from_a = backend_connection();
    vocabdemo::TableVocabulary from_b;
    static uint8_t b_announcement[8192];
    const int64_t b_announced = vocabdemo::Announce( b_announcement, sizeof( b_announcement ) );
    CHECK( vocabdemo::AnnounceRead( from_b, b_announcement, b_announced, NULL ) );

    // THE TWO TABLES ARE DIFFERENT, which is what makes the pair a control
    CHECK( from_a.table.count != from_b.table.count );
    CHECK( from_a.build_version != from_b.build_version );

    static backenddemo::StorePurchase a_value;
    build_backend_store( a_value );
    static uint8_t a_message[1024];
    const int64_t a_bytes = backenddemo::StorePurchaseSaveMessage( a_value, a_message, sizeof( a_message ) );
    pin_table_golden( "peer_a_message", a_message, a_bytes );
    static uint8_t a_file[1024];
    pin_table_golden( "peer_a", a_file, backenddemo::StorePurchaseSave( a_value, a_file, sizeof( a_file ) ) );

    static vocabdemo::Wide00 b_value;
    b_value.field_00_00 = 11;
    b_value.field_00_12 = 22;
    static uint8_t b_message[1024];
    const int64_t b_bytes = vocabdemo::Wide00SaveMessage( b_value, b_message, sizeof( b_message ) );
    pin_table_golden( "peer_b_message", b_message, b_bytes );
    static uint8_t b_file[1024];
    pin_table_golden( "peer_b", b_file, vocabdemo::Wide00Save( b_value, b_file, sizeof( b_file ) ) );

    // each side decodes against the table THAT peer announced
    backenddemo::StorePurchase a_read;
    backenddemo::TableReport a_report;
    CHECK( backenddemo::StorePurchaseLoadMessage( a_read, from_a, a_message, a_bytes, &a_report ) );
    CHECK( a_read.price_minor == 499 && a_read.quantity == 7 );
    vocabdemo::Wide00 b_read;
    vocabdemo::TableReport b_report;
    CHECK( vocabdemo::Wide00LoadMessage( b_read, from_b, b_message, b_bytes, &b_report ) );
    CHECK( b_read.field_00_00 == 11 && b_read.field_00_12 == 22 );
}

// ---- THE REFUSALS THIS FORM ADDS (docs/SPEC-TABLES.md §3.3, §11) -----------

static void test_message_form_refusals()
{
    static uint8_t announcement[4096];
    const int64_t announced = backenddemo::Announce( announcement, sizeof( announcement ) );

    // A MESSAGE WITH NO TABLE FOR THE CONNECTION is refused BY NAME: nothing
    // is decoded, the reader says it holds no table, and malformed does not
    // fire. It does not fall back to the file form and does not guess a table.
    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t message[256];
        const int64_t bytes = backenddemo::LoginRequestSaveMessage( value, message, sizeof( message ) );
        backenddemo::TableVocabulary empty;
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        CHECK( !backenddemo::LoginRequestLoadMessage( out, empty, message, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary );
        CHECK( !report.malformed );
        CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
        CHECK( out.player_id == 0 ); // nothing was decoded: the declared default stands
        pin_table_golden( "message_no_vocabulary", message, bytes );
    }

    // A FORM 2 WIRE WHERE A FILE WAS EXPECTED is refused by name, because a
    // message stored on its own is not readable: its table is somewhere else.
    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t message[256];
        const int64_t bytes = backenddemo::LoginRequestSaveMessage( value, message, sizeof( message ) );
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        CHECK( !backenddemo::LoginRequestLoad( out, message, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::message_form_as_file );
        CHECK( !report.malformed );
        CHECK( report.unknown == 0 && report.kind_mismatch == 0 );
    }

    // AND A FILE WHERE A MESSAGE WAS EXPECTED is a form byte this reader does
    // not carry, which is the verdict the form byte already needed.
    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t file[256];
        const int64_t bytes = backenddemo::LoginRequestSave( value, file, sizeof( file ) );
        backenddemo::TableVocabulary vocabulary = backend_connection();
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        CHECK( !backenddemo::LoginRequestLoadMessage( out, vocabulary, file, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::newer_form && !report.malformed );
    }

    // A SECOND ANNOUNCEMENT ON A CONNECTION IS REFUSED BY NAME. It does not
    // replace the table, it does not amend it, and it changes nothing.
    {
        backenddemo::TableVocabulary vocabulary;
        CHECK( backenddemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
        const int64_t was = vocabulary.table.count;
        const uint64_t was_version = vocabulary.build_version;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, announcement, announced, &report ) );
        CHECK( report.refused && report.reason == backenddemo::second_announcement );
        CHECK( !report.malformed );
        CHECK( vocabulary.announced && vocabulary.table.count == was && vocabulary.build_version == was_version );
        pin_table_golden( "message_second_announcement", announcement, announced );
    }

    // A TABLE PAST A BOUND IS REFUSED BEFORE ANYTHING IS ALLOCATED: the count
    // is read, compared and refused without touching an entry.
    {
        backenddemo::TableVocabulary vocabulary;
        vocabulary.max_entries = 28; // one below what this unit announces
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, announcement, announced, &report ) );
        CHECK( report.refused && report.reason == backenddemo::vocabulary_too_large );
        CHECK( !report.malformed && !vocabulary.announced );
        CHECK( vocabulary.table.entries == NULL && vocabulary.table.count == 0 );
        // and exactly at the bound it is read
        backenddemo::TableVocabulary exact;
        exact.max_entries = 29;
        CHECK( backenddemo::AnnounceRead( exact, announcement, announced, NULL ) );
    }

    // A REFUSED ANNOUNCEMENT SETS NO TABLE, so every message on that
    // connection is refused for want of one until the connection ends.
    {
        backenddemo::TableVocabulary vocabulary;
        vocabulary.max_entries = 4;
        CHECK( !backenddemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t message[256];
        const int64_t bytes = backenddemo::LoginRequestSaveMessage( value, message, sizeof( message ) );
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        CHECK( !backenddemo::LoginRequestLoadMessage( out, vocabulary, message, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary );
    }
}

// ---- THE ANNOUNCEMENT'S ONE STRICT CHECK, AND ITS TOLERANCE ---------------
//
// The reserved build-version field present, exactly once, under kind 9, eight
// bytes wide — and everything else in its body an ordinary field under §4's
// tolerance, so an unknown one is skipped and counted and the announcement can
// GAIN a field in a later minor without a lockstep redeploy.

// forge_announcement rebuilds an announcement with a body of the caller's own
// making over the SAME trailer, which is what lets a row change one fact.
static int64_t forge_announcement( uint8_t * out, int64_t capacity, const uint8_t * body, int64_t body_bytes )
{
    static uint8_t announcement[4096];
    const int64_t announced = backenddemo::Announce( announcement, sizeof( announcement ) );
    const int64_t trailer = 29 * 8 + 8;
    const int64_t total = 1 + body_bytes + trailer;
    if ( total > capacity ) { return -1; }
    out[0] = 1;
    memcpy( out + 1, body, (size_t) body_bytes );
    memcpy( out + 1 + body_bytes, announcement + announced - trailer, (size_t) trailer );
    return total;
}

static void test_message_form_announcement_check()
{
    static uint8_t forged[4096];

    // ABSENT: the body is its terminator alone
    {
        const uint8_t body[1] = { 0 };
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, 1 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_no_build_version", forged, bytes );
    }
    // PRESENT TWICE
    {
        const uint8_t body[21] = { 1, 9, 1,0,0,0,0,0,0,0, 1, 9, 2,0,0,0,0,0,0,0, 0 };
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, 21 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_build_version_twice", forged, bytes );
    }
    // UNDER A KIND OTHER THAN 9: kind 8, a u32
    {
        const uint8_t body[7] = { 1, 8, 1,0,0,0, 0 };
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, 7 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_build_version_kind", forged, bytes );
    }
    // AT A WIDTH THAT IS NOT EIGHT: kind 9 with four bytes left in the body
    {
        const uint8_t body[6] = { 1, 9, 1,0,0,0 };
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, 6 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && !vocabulary.announced );
        pin_table_golden( "announce_build_version_width", forged, bytes );
    }
    // AND THE TOLERANT ROW: an UNKNOWN field beside the reserved one sets the
    // table and counts one unknown. This is the whole reason the announcement
    // is a table body rather than a fixed header.
    {
        // slot 2 is an ordinary vocabulary id, under kind 8 with a u32 payload
        const uint8_t body[17] = { 1, 9, 0x11,0x22,0x33,0x44,0x55,0x66,0x77,0x88, 2, 8, 9,0,0,0, 0 };
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, 17 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( vocabulary.announced );
        CHECK( vocabulary.build_version == 0x8877665544332211ull );
        CHECK( report.unknown == 1 );
        CHECK( !report.refused && !report.malformed );
        pin_table_golden( "announce_unknown_field", forged, bytes );
    }
}

// ---- THE RESERVED BUILD-VERSION ID IN A BODY (§3.1, §3.3) ------------------
//
// A reserved id in any body but the one whose transport it is, is MALFORMED.
// One row plants it in a message body and one plants it in a NESTED body, and
// neither may count anything but malformed.

static void test_message_form_reserved_id_in_a_body()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    // slot 1 IS the reserved build-version id, so a message naming slot 1 is a
    // body carrying a reserved id
    {
        const uint8_t message[12] = { 2, 1, 9, 1,0,0,0,0,0,0,0, 0 };
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        backenddemo::LoginRequestLoadMessage( out, vocabulary, message, sizeof( message ), &report );
        CHECK( report.malformed );
        CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
        CHECK( !report.refused );
        pin_table_golden( "message_reserved_id_root", message, sizeof( message ) );
    }
    // and inside a NESTED body: MatchResult's players array holds PlayerRow
    // bodies, and one of them names slot 1
    {
        // players (slot 7) under kind 14, its L, then the array body: the
        // element kind, the count, one element's L and its body — and that
        // body names slot 1, the reserved build-version id
        const uint8_t message[19] = { 2, 7, 14, 14, 13, 1, 11, 1, 9, 1,0,0,0,0,0,0,0, 0, 0 };
        backenddemo::MatchResult out;
        backenddemo::TableReport report;
        backenddemo::MatchResultLoadMessage( out, vocabulary, message, sizeof( message ), &report );
        CHECK( report.malformed );
        CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
        CHECK( !report.refused );
        pin_table_golden( "message_reserved_id_nested", message, sizeof( message ) );
    }
}

// ---- A REFERENCE PAST THE TABLE (§3, §3.3) --------------------------------
//
// In the ROOT body there is no parent to read on into, so the body stops
// there, malformed counts once, AND THE FIELDS DECODED BEFORE IT STAND. The
// entry count ITSELF is the last legal slot and must resolve.

static void test_message_form_reference_bound()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    {
        // player_id (slot 2) whole, then a reference ONE PAST the entry count
        const uint8_t message[16] = { 2, 2, 9, 5,0,0,0,0,0,0,0, 30, 8, 1,0,0 };
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        backenddemo::LoginRequestLoadMessage( out, vocabulary, message, sizeof( message ), &report );
        CHECK( report.malformed );
        CHECK( out.player_id == 5 ); // the field decoded BEFORE the bad reference stands
        pin_table_golden( "message_reference_past_table", message, sizeof( message ) );
    }
    {
        // the entry count itself, 29, is the LAST LEGAL SLOT and must resolve:
        // it names StorePurchase's own id, which is no field of LoginRequest,
        // so it is §4's ordinary unknown and never a resolve-time failure
        const uint8_t message[7] = { 2, 29, 8, 1,0,0,0 };
        uint8_t whole[8];
        memcpy( whole, message, 7 );
        whole[7] = 0;
        backenddemo::LoginRequest out;
        backenddemo::TableReport report;
        CHECK( backenddemo::LoginRequestLoadMessage( out, vocabulary, whole, 8, &report ) );
        CHECK( !report.malformed && !report.refused );
        CHECK( report.unknown == 1 );
        pin_table_golden( "message_reference_last_slot", whole, sizeof( whole ) );
    }
}

#endif // SCHEMA_TEST_MESSAGE_FORM_H
