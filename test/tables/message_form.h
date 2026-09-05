// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), included by main.cpp.
//
// A FILE carries its own id table and a MESSAGE STREAM announces one and then
// carries BITPACKED bodies, a batch at a time. This section pins the
// announcement and the vectors the page names, holds the byte counts its
// arithmetic prints, and runs the batch surface's answers and the refusals the
// form adds to §3's verdict.
//
// It is a header included once, into the one binary that owns the wire pins,
// because a golden with two homes is one golden that can disagree with itself.

#ifndef SCHEMA_TEST_MESSAGE_FORM_H
#define SCHEMA_TEST_MESSAGE_FORM_H

#include "BackendTable.h"
#include "VocabTable.h"
#include "Vocab9Table.h"

// ---- the six values, field for field (docs/SPEC-TABLES.md §3.3) ------------
//
// The three FULL vectors carry the measurement's own values, and their byte
// counts are the page's: 106 / 273 / 104 as files, 51 / 142 / 41 as messages.
// The three DEFAULT vectors touch nothing, so every field elides and the body
// is its terminator alone: 10 / 43 / 10 as files, 3 / 10 / 3 as messages.

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
        // array element in this corpus, and is what the page's 142 counts
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
// byte and a second json/ file would be one golden with two homes. A single
// message is the BATCH OF ONE (§3.3).
template <typename T, typename Measure, typename Save, typename MeasureMessages, typename SaveMessages>
static void pin_message_vector( const char * name, const T & value,
                                Measure measure, Save save,
                                MeasureMessages measure_messages, SaveMessages save_messages,
                                int64_t file_bytes, int64_t message_bytes )
{
    static uint8_t file[1u << 16];
    static uint8_t message[1u << 16];
    const int64_t wrote_file = save( value, file, sizeof( file ) );
    CHECK( wrote_file > 0 && wrote_file == measure( value ) );
    backenddemo::TableReport report;
    const int64_t wrote_message = save_messages( &value, 1, message, sizeof( message ), &report );
    CHECK( wrote_message > 0 && wrote_message == measure_messages( &value, 1, &report ) );
    CHECK( !report.refused );
    // THE PINNED BYTE COUNTS ARE THE PAGE'S TABLE: a vector whose count moves
    // is a wire that moved (docs/SPEC-TABLES.md §3.3).
    if ( wrote_file != file_bytes || wrote_message != message_bytes )
    {
        printf( "FAIL message vector %s: %lld/%lld bytes, the page pins %lld/%lld\n",
                name, (long long) wrote_file, (long long) wrote_message,
                (long long) file_bytes, (long long) message_bytes );
        failures++;
    }
    // A BATCH IS THREE PARTS: the form byte, the count, and the bodies as one
    // bit stream padded to a byte — there is no trailer at all
    CHECK( wrote_message >= 2 && message[0] == 2 && message[1] == 0 );
    CHECK( wrote_file >= 1 && file[0] == 1 );
    pin_table_golden( name, file, wrote_file );
    char message_name[128];
    snprintf( message_name, sizeof( message_name ), "%s_message", name );
    pin_table_golden( message_name, message, wrote_message );
}

static void test_message_form_goldens()
{
    // THE ANNOUNCEMENT: 28 entries and 316 bytes, an ordinary form 1 FILE
    // (docs/SPEC-TABLES.md §3.3)
    {
        static uint8_t announcement[4096];
        const int64_t bytes = backenddemo::Announce( announcement, sizeof( announcement ) );
        CHECK( bytes == 316 );
        CHECK( announcement[0] == 1 );
        pin_table_golden( "backend_conn", announcement, bytes );
        // and a buffer one byte short is refused rather than half-written
        CHECK( backenddemo::Announce( announcement, bytes - 1 ) == -1 );
        backenddemo::TableVocabulary vocabulary;
        CHECK( backenddemo::AnnounceRead( vocabulary, announcement, bytes, NULL ) );
        CHECK( vocabulary.count == 28 );
        CHECK( vocabulary.ref_bits == 5 ); // bits_required( 0, 28 )
        CHECK( backenddemo::kTableMessageRefBitsHere == 5 );
    }

    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        pin_message_vector( "login_full", value,
                            backenddemo::LoginRequestMeasure, backenddemo::LoginRequestSave,
                            backenddemo::LoginRequestMeasureMessages, backenddemo::LoginRequestSaveMessages,
                            106, 51 );
    }
    {
        static backenddemo::LoginRequest value; // untouched: everything elides
        pin_message_vector( "login_default", value,
                            backenddemo::LoginRequestMeasure, backenddemo::LoginRequestSave,
                            backenddemo::LoginRequestMeasureMessages, backenddemo::LoginRequestSaveMessages,
                            10, 3 );
    }
    {
        static backenddemo::MatchResult value;
        build_backend_match( value );
        pin_message_vector( "match_full", value,
                            backenddemo::MatchResultMeasure, backenddemo::MatchResultSave,
                            backenddemo::MatchResultMeasureMessages, backenddemo::MatchResultSaveMessages,
                            273, 142 );
    }
    {
        static backenddemo::MatchResult value;
        pin_message_vector( "match_default", value,
                            backenddemo::MatchResultMeasure, backenddemo::MatchResultSave,
                            backenddemo::MatchResultMeasureMessages, backenddemo::MatchResultSaveMessages,
                            43, 10 );
    }
    {
        static backenddemo::StorePurchase value;
        build_backend_store( value );
        pin_message_vector( "store_full", value,
                            backenddemo::StorePurchaseMeasure, backenddemo::StorePurchaseSave,
                            backenddemo::StorePurchaseMeasureMessages, backenddemo::StorePurchaseSaveMessages,
                            104, 41 );
    }
    {
        static backenddemo::StorePurchase value;
        pin_message_vector( "store_default", value,
                            backenddemo::StorePurchaseMeasure, backenddemo::StorePurchaseSave,
                            backenddemo::StorePurchaseMeasureMessages, backenddemo::StorePurchaseSaveMessages,
                            10, 3 );
    }
}

// ---- THE BATCH (docs/SPEC-TABLES.md §3.3) ---------------------------------
//
// The three full messages as ONE batch: one form byte, one count of three, and
// the three bodies back to back with no alignment between them, which is 230
// bytes against 234 for the three sent alone. A batch is of one root, so a
// batch of three roots is what the page calls the application's discriminator
// done by hand: the body-level codec, under one count. Red if a leg aligns
// between bodies, writes a terminator the batch does not carry, sizes a batch
// as the sum of its bodies alone, or accepts a count of zero.

static void test_message_form_batch()
{
    static backenddemo::LoginRequest login;
    static backenddemo::MatchResult match;
    static backenddemo::StorePurchase store;
    build_backend_login( login );
    build_backend_match( match );
    build_backend_store( store );
    backenddemo::TableReport report;

    // the three ALONE: 51 + 142 + 41
    const int64_t alone = backenddemo::LoginRequestMeasureMessages( &login, 1, &report )
                        + backenddemo::MatchResultMeasureMessages( &match, 1, &report )
                        + backenddemo::StorePurchaseMeasureMessages( &store, 1, &report );
    CHECK( alone == 234 );

    // the three as ONE BATCH: measured as one bit stream, then written as one
    int64_t bits = 8;
    bits += backenddemo::LoginRequestMeasureMessageBody( bits, login );
    CHECK( bits == 396 ); // login ends at bit 404 of the batch, the form byte's eight aside
    bits += backenddemo::MatchResultMeasureMessageBody( bits, match );
    CHECK( bits == 1516 ); // match ends at 1524
    bits += backenddemo::StorePurchaseMeasureMessageBody( bits, store );
    CHECK( bits == 1832 ); // 1840 bits whole, the form byte counted as eight of them
    const int64_t batch_bytes = backenddemo::TableMessageBatchBytes( bits - 8 );
    CHECK( batch_bytes == 230 );

    static uint8_t batch[1024];
    backenddemo::TableMessageBatch writer;
    CHECK( backenddemo::TableMessageBatchBegin( writer, batch, sizeof( batch ), 3 ) );
    CHECK( backenddemo::LoginRequestSaveMessageBody( writer.w, login ) ); writer.written++;
    CHECK( backenddemo::MatchResultSaveMessageBody( writer.w, match ) ); writer.written++;
    CHECK( backenddemo::StorePurchaseSaveMessageBody( writer.w, store ) ); writer.written++;
    const int64_t wrote = backenddemo::TableMessageBatchEnd( writer );
    CHECK( wrote == 230 );
    CHECK( batch[0] == 2 && batch[1] == 2 ); // the form byte, and a count of three carried as M - 1
    pin_table_golden( "backend_round_message", batch, wrote );

    // and it reads back, body by body, against the connection's vocabulary
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    backenddemo::TableMessageBatchReader reader;
    backenddemo::TableReport read_report;
    CHECK( backenddemo::TableMessageBatchOpen( reader, vocabulary, batch, wrote, &read_report ) == 3 );
    backenddemo::LoginRequest login_read;
    backenddemo::MatchResult match_read;
    backenddemo::StorePurchase store_read;
    CHECK( backenddemo::LoginRequestLoadMessageBody( reader.r, vocabulary, &read_report, login_read ) ); reader.remaining--;
    CHECK( backenddemo::MatchResultLoadMessageBody( reader.r, vocabulary, &read_report, match_read ) ); reader.remaining--;
    CHECK( backenddemo::StorePurchaseLoadMessageBody( reader.r, vocabulary, &read_report, store_read ) ); reader.remaining--;
    CHECK( backenddemo::TableMessageBatchClose( reader ) );
    CHECK( !read_report.malformed && !read_report.refused && read_report.unknown == 0 );
    CHECK( login_read.client_build == 140233 && match_read.players[9].placement == 10 && store_read.quantity == 7 );

    // A BATCH OF ZERO IS NOT SPELLABLE: the verbs refuse it
    CHECK( backenddemo::LoginRequestMeasureMessages( &login, 0, &report ) == -1 );
    CHECK( backenddemo::LoginRequestSaveMessages( &login, 0, batch, sizeof( batch ), &report ) == -1 );
    CHECK( !backenddemo::TableMessageBatchBegin( writer, batch, sizeof( batch ), 0 ) );

    // A BATCH OF 256, which is the wire's own maximum, as one buffer
    static backenddemo::LoginRequest many[256];
    for ( int32_t i = 0; i < 256; i++ ) { backenddemo::LoginRequestReset( many[i] ); many[i].client_build = (uint32_t) ( i + 1 ); }
    static uint8_t wide[8192];
    const int64_t wide_bytes = backenddemo::LoginRequestSaveMessages( many, 256, wide, sizeof( wide ), &report );
    CHECK( wide_bytes > 0 && wide_bytes == backenddemo::LoginRequestMeasureMessages( many, 256, &report ) );
    CHECK( wide[1] == 255 ); // M - 1
    // each body is a reference, 32 bits and a terminator: 42 bits, 256 of them, plus the count
    CHECK( wide_bytes == 1 + ( 8 + 256 * 42 + 7 ) / 8 );
    pin_table_golden( "backend_batch_256_message", wide, wide_bytes );
    static backenddemo::LoginRequest back[256];
    int64_t count = 256;
    backenddemo::TableReport wide_report;
    CHECK( backenddemo::LoginRequestLoadMessages( back, &count, vocabulary, wide, wide_bytes, &wide_report ) );
    CHECK( count == 256 && back[0].client_build == 1 && back[255].client_build == 256 );
    CHECK( !wide_report.malformed && !wide_report.refused );
}

// ---- the goldens READ BACK, against the connection's announced table -------

template <typename T, typename LoadMessages, typename MeasureMessages, typename SaveMessages, typename Reset>
static void reload_message_golden( const char * name, const backenddemo::TableVocabulary & vocabulary,
                                   LoadMessages load_messages, MeasureMessages measure_messages, SaveMessages save_messages, Reset reset )
{
    char message_name[128];
    snprintf( message_name, sizeof( message_name ), "%s_message", name );
    const int64_t pinned = read_table_golden( message_name );
    if ( pinned < 0 ) { return; }
    T value;
    reset( value );
    int64_t count = 1;
    backenddemo::TableReport report;
    if ( !load_messages( &value, &count, vocabulary, golden_pinned, pinned, &report ) || report.malformed || count != 1 )
    {
        printf( "FAIL message golden %s does not load\n", message_name );
        failures++;
        return;
    }
    CHECK( !report.refused );
    CHECK( report.unknown == 0 && report.kind_mismatch == 0 );
    CHECK( report.clamped == 0 && report.duplicate == 0 );
    CHECK( measure_messages( &value, 1, &report ) == pinned );
    const int64_t wrote = save_messages( &value, 1, golden_again, sizeof( golden_again ), &report );
    if ( wrote != pinned || memcmp( golden_again, golden_pinned, (size_t) pinned ) != 0 )
    {
        printf( "FAIL message golden %s re-saves differently: %lld out, %lld pinned\n",
                message_name, (long long) wrote, (long long) pinned );
        failures++;
    }
}

// THE TWO FORMS ARE TWO ENCODINGS OF ONE VALUE, and the pin is a ROUND TRIP
// (§3.3): loading the file form and saving the message form reproduces the
// message's pinned bytes, and the reverse reproduces the file's.
template <typename T, typename Load, typename Measure, typename Save, typename LoadMessages, typename MeasureMessages, typename SaveMessages, typename Reset>
static void cross_message_golden( const char * name, const backenddemo::TableVocabulary & vocabulary,
                                  Load load, Measure measure, Save save,
                                  LoadMessages load_messages, MeasureMessages measure_messages, SaveMessages save_messages, Reset reset )
{
    static uint8_t file[1u << 16];
    static uint8_t message[1u << 16];
    char message_name[128];
    snprintf( message_name, sizeof( message_name ), "%s_message", name );
    const int64_t file_bytes = read_table_golden( name );
    if ( file_bytes < 0 ) { return; }
    memcpy( file, golden_pinned, (size_t) file_bytes );
    const int64_t message_bytes = read_table_golden( message_name );
    if ( message_bytes < 0 ) { return; }
    memcpy( message, golden_pinned, (size_t) message_bytes );
    backenddemo::TableReport report;
    // FILE in, MESSAGE out
    {
        T value;
        reset( value );
        CHECK( load( value, file, file_bytes, &report ) && !report.malformed );
        CHECK( measure_messages( &value, 1, &report ) == message_bytes );
        CHECK( save_messages( &value, 1, golden_again, sizeof( golden_again ), &report ) == message_bytes );
        if ( memcmp( golden_again, message, (size_t) message_bytes ) != 0 )
        {
            printf( "FAIL %s: the file form loaded and saved as a message is not the pinned message\n", name );
            failures++;
        }
    }
    // MESSAGE in, FILE out
    {
        T value;
        reset( value );
        int64_t count = 1;
        CHECK( load_messages( &value, &count, vocabulary, message, message_bytes, &report ) && !report.malformed && count == 1 );
        CHECK( measure( value ) == file_bytes );
        CHECK( save( value, golden_again, sizeof( golden_again ) ) == file_bytes );
        if ( memcmp( golden_again, file, (size_t) file_bytes ) != 0 )
        {
            printf( "FAIL %s: the message form loaded and saved as a file is not the pinned file\n", name );
            failures++;
        }
    }
}

static void test_message_form_reload()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    // THE BUILD VERSION KEYS THE VOCABULARY and gates nothing: it is what the
    // announcement carries under the reserved id, and what a refusal names.
    CHECK( vocabulary.announced );
    CHECK( vocabulary.build_version == backenddemo::BuildVersion );
    CHECK( vocabulary.count == 28 );

    reload_message_golden<backenddemo::LoginRequest>( "login_full", vocabulary,
        backenddemo::LoginRequestLoadMessages, backenddemo::LoginRequestMeasureMessages, backenddemo::LoginRequestSaveMessages, backenddemo::LoginRequestReset );
    reload_message_golden<backenddemo::LoginRequest>( "login_default", vocabulary,
        backenddemo::LoginRequestLoadMessages, backenddemo::LoginRequestMeasureMessages, backenddemo::LoginRequestSaveMessages, backenddemo::LoginRequestReset );
    reload_message_golden<backenddemo::MatchResult>( "match_full", vocabulary,
        backenddemo::MatchResultLoadMessages, backenddemo::MatchResultMeasureMessages, backenddemo::MatchResultSaveMessages, backenddemo::MatchResultReset );
    reload_message_golden<backenddemo::MatchResult>( "match_default", vocabulary,
        backenddemo::MatchResultLoadMessages, backenddemo::MatchResultMeasureMessages, backenddemo::MatchResultSaveMessages, backenddemo::MatchResultReset );
    reload_message_golden<backenddemo::StorePurchase>( "store_full", vocabulary,
        backenddemo::StorePurchaseLoadMessages, backenddemo::StorePurchaseMeasureMessages, backenddemo::StorePurchaseSaveMessages, backenddemo::StorePurchaseReset );
    reload_message_golden<backenddemo::StorePurchase>( "store_default", vocabulary,
        backenddemo::StorePurchaseLoadMessages, backenddemo::StorePurchaseMeasureMessages, backenddemo::StorePurchaseSaveMessages, backenddemo::StorePurchaseReset );

    cross_message_golden<backenddemo::LoginRequest>( "login_full", vocabulary,
        backenddemo::LoginRequestLoad, backenddemo::LoginRequestMeasure, backenddemo::LoginRequestSave,
        backenddemo::LoginRequestLoadMessages, backenddemo::LoginRequestMeasureMessages, backenddemo::LoginRequestSaveMessages, backenddemo::LoginRequestReset );
    cross_message_golden<backenddemo::LoginRequest>( "login_default", vocabulary,
        backenddemo::LoginRequestLoad, backenddemo::LoginRequestMeasure, backenddemo::LoginRequestSave,
        backenddemo::LoginRequestLoadMessages, backenddemo::LoginRequestMeasureMessages, backenddemo::LoginRequestSaveMessages, backenddemo::LoginRequestReset );
    cross_message_golden<backenddemo::MatchResult>( "match_full", vocabulary,
        backenddemo::MatchResultLoad, backenddemo::MatchResultMeasure, backenddemo::MatchResultSave,
        backenddemo::MatchResultLoadMessages, backenddemo::MatchResultMeasureMessages, backenddemo::MatchResultSaveMessages, backenddemo::MatchResultReset );
    cross_message_golden<backenddemo::MatchResult>( "match_default", vocabulary,
        backenddemo::MatchResultLoad, backenddemo::MatchResultMeasure, backenddemo::MatchResultSave,
        backenddemo::MatchResultLoadMessages, backenddemo::MatchResultMeasureMessages, backenddemo::MatchResultSaveMessages, backenddemo::MatchResultReset );
    cross_message_golden<backenddemo::StorePurchase>( "store_full", vocabulary,
        backenddemo::StorePurchaseLoad, backenddemo::StorePurchaseMeasure, backenddemo::StorePurchaseSave,
        backenddemo::StorePurchaseLoadMessages, backenddemo::StorePurchaseMeasureMessages, backenddemo::StorePurchaseSaveMessages, backenddemo::StorePurchaseReset );
    cross_message_golden<backenddemo::StorePurchase>( "store_default", vocabulary,
        backenddemo::StorePurchaseLoad, backenddemo::StorePurchaseMeasure, backenddemo::StorePurchaseSave,
        backenddemo::StorePurchaseLoadMessages, backenddemo::StorePurchaseMeasureMessages, backenddemo::StorePurchaseSaveMessages, backenddemo::StorePurchaseReset );

    // AND THE VALUE SURVIVES: the message form carries the same value the file
    // form does, at a third fewer bytes.
    {
        static backenddemo::LoginRequest wrote;
        build_backend_login( wrote );
        static uint8_t message[256];
        backenddemo::TableReport report;
        const int64_t bytes = backenddemo::LoginRequestSaveMessages( &wrote, 1, message, sizeof( message ), &report );
        backenddemo::LoginRequest read;
        int64_t count = 1;
        CHECK( backenddemo::LoginRequestLoadMessages( &read, &count, vocabulary, message, bytes, &report ) );
        CHECK( count == 1 );
        CHECK( read.player_id == wrote.player_id );
        CHECK( read.client_build == wrote.client_build );
        CHECK( read.region == wrote.region );
        CHECK( memcmp( read.session_token, wrote.session_token, 32 ) == 0 );
    }
}

// ---- A WIDE VOCABULARY (docs/SPEC-TABLES.md §3.3) --------------------------
//
// vocabdemo's vocabulary passes 128 entries so a reference is 8 bits, and
// vocab9demo's passes 256 so a reference is 9 bits. A body over each unit's
// LAST table names entries at the far end of the range and one over its FIRST
// at the near end. Red if a leg fixes the reference width, or sizes a batch as
// though a reference were a byte.

static void test_message_form_wide_vocabulary()
{
    // vocabdemo: 130 field names, the tail, and ten table ids: 144 entries, 8 bits
    {
        vocabdemo::TableVocabulary vocabulary;
        static uint8_t announcement[16384];
        const int64_t announced = vocabdemo::Announce( announcement, sizeof( announcement ) );
        CHECK( announced == vocabdemo::AnnounceMeasure() );
        CHECK( vocabdemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
        CHECK( vocabulary.count == 144 && vocabulary.ref_bits == 8 );
        CHECK( vocabdemo::kTableMessageRefBitsHere == 8 );
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
        vocabdemo::TableReport report;
        const int64_t bytes = vocabdemo::Wide09SaveMessages( &value, 1, message, sizeof( message ), &report );
        CHECK( bytes > 0 && bytes == vocabdemo::Wide09MeasureMessages( &value, 1, &report ) );
        // thirteen fields, each an 8-bit reference and 32 bits, and the
        // terminator: 8 + 13 * 40 + 8 bits, padded, behind the form byte
        CHECK( bytes == 1 + ( 8 + 13 * 40 + 8 + 7 ) / 8 );
        pin_table_golden( "vocab_wide_message", message, bytes );
        static uint8_t file[4096];
        const int64_t file_bytes = vocabdemo::Wide09Save( value, file, sizeof( file ) );
        CHECK( file_bytes > 0 && file_bytes == vocabdemo::Wide09Measure( value ) );
        pin_table_golden( "vocab_wide", file, file_bytes );

        vocabdemo::Wide09 read;
        int64_t count = 1;
        CHECK( vocabdemo::Wide09LoadMessages( &read, &count, vocabulary, message, bytes, &report ) );
        CHECK( !report.refused && !report.malformed && report.unknown == 0 );
        CHECK( read.field_09_00 == 1 && read.field_09_12 == 13 );
        static uint8_t again[4096];
        CHECK( vocabdemo::Wide09SaveMessages( &read, 1, again, sizeof( again ), &report ) == bytes );
        CHECK( memcmp( again, message, (size_t) bytes ) == 0 );

        static vocabdemo::Wide00 low;
        low.field_00_00 = 5;
        static uint8_t low_message[256];
        const int64_t low_bytes = vocabdemo::Wide00SaveMessages( &low, 1, low_message, sizeof( low_message ), &report );
        static uint8_t low_file[256];
        pin_table_golden( "vocab_low", low_file, vocabdemo::Wide00Save( low, low_file, sizeof( low_file ) ) );
        // the form byte, the count, one 8-bit reference, a u32 and the
        // terminator: 8 + 8 + 32 + 8 bits, padded
        CHECK( low_bytes == 1 + ( 8 + 8 + 32 + 8 + 7 ) / 8 );
        pin_table_golden( "vocab_low_message", low_message, low_bytes );
    }
    // vocab9demo: 260 field names, the tail, and twenty table ids: 284 entries, 9 bits
    {
        vocab9demo::TableVocabulary vocabulary;
        static uint8_t announcement[16384];
        const int64_t announced = vocab9demo::Announce( announcement, sizeof( announcement ) );
        CHECK( announced == vocab9demo::AnnounceMeasure() );
        CHECK( vocab9demo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
        CHECK( vocabulary.count == 284 && vocabulary.ref_bits == 9 );
        CHECK( vocab9demo::kTableMessageRefBitsHere == 9 );
        pin_table_golden( "vocab9_conn", announcement, announced );

        static vocab9demo::Wide19 value;
        value.field_19_00 = 1;
        value.field_19_12 = 13;
        static uint8_t message[4096];
        vocab9demo::TableReport report;
        const int64_t bytes = vocab9demo::Wide19SaveMessages( &value, 1, message, sizeof( message ), &report );
        CHECK( bytes > 0 && bytes == vocab9demo::Wide19MeasureMessages( &value, 1, &report ) );
        // two fields at 9 + 32 bits each and a 9-bit terminator, behind the count
        CHECK( bytes == 1 + ( 8 + 2 * 41 + 9 + 7 ) / 8 );
        pin_table_golden( "vocab9_wide_message", message, bytes );
        static uint8_t file[4096];
        pin_table_golden( "vocab9_wide", file, vocab9demo::Wide19Save( value, file, sizeof( file ) ) );
        vocab9demo::Wide19 read;
        int64_t count = 1;
        CHECK( vocab9demo::Wide19LoadMessages( &read, &count, vocabulary, message, bytes, &report ) );
        CHECK( !report.malformed && read.field_19_00 == 1 && read.field_19_12 == 13 );

        static vocab9demo::Wide00 low;
        low.field_00_00 = 5;
        static uint8_t low_message[256];
        const int64_t low_bytes = vocab9demo::Wide00SaveMessages( &low, 1, low_message, sizeof( low_message ), &report );
        CHECK( low_bytes == 1 + ( 8 + 9 + 32 + 9 + 7 ) / 8 );
        pin_table_golden( "vocab9_low_message", low_message, low_bytes );
        static uint8_t low_file[256];
        pin_table_golden( "vocab9_low", low_file, vocab9demo::Wide00Save( low, low_file, sizeof( low_file ) ) );
    }
}

// ---- A POINTERED BATCH (docs/SPEC-TABLES.md §3.1, §3.3) --------------------
//
// The node table is the FIRST field of each root body, its count is thirty-two
// raw bits, its table records carry NO length and end at their own zero
// reference, and its indices are bits_required(0, node count) wide. A batch of
// pointered roots is measured once and loaded into ONE region. Beside it a
// root reaching no node, which carries no node-table reference at all.

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
    const graphdemo::Scene * roots[1] = { builder.AsConst() };
    static uint8_t message[16384];
    graphdemo::TableReport report;
    const int64_t bytes = graphdemo::SceneSaveMessages( roots, 1, message, sizeof( message ), &report );
    CHECK( bytes > 0 && bytes == graphdemo::SceneMeasureMessages( roots, 1, &report ) );
    CHECK( message[0] == 2 && message[1] == 0 );
    pin_table_golden( "graph_tree_message", message, bytes );

    // THE NODE TABLE IS THE FIRST FIELD: the body's first reference names it
    {
        graphdemo::TableBitReader r( message + 1, bytes - 1 );
        uint64_t count = 0, ref = 0;
        CHECK( r.get( count, 8 ) && count == 0 );
        CHECK( r.get( ref, vocabulary.ref_bits ) );
        CHECK( graphdemo::TableVocabularyEntryAt( vocabulary, ref ).id == graphdemo::kTableNodeTableFieldId );
        uint64_t nodes = 0;
        CHECK( r.get( nodes, 32 ) && nodes == 6 ); // two list nodes, three tree nodes, the settings
    }

    int64_t attribution = 0;
    const int64_t need = graphdemo::SceneLoadMeasure( vocabulary, message, bytes, &attribution );
    CHECK( need > 0 && attribution == 7 * (int64_t) sizeof( graphdemo::TableNodeDirEntry ) );
    static uint8_t region[65536];
    const graphdemo::Scene * loaded[1] = { NULL };
    int64_t count = 1;
    CHECK( graphdemo::SceneLoadMessages( loaded, &count, region, need, vocabulary, message, bytes, &report ) );
    CHECK( count == 1 && loaded[0] != NULL );
    CHECK( !report.refused && !report.malformed && report.unknown == 0 );
    if ( loaded[0] != NULL )
    {
        const graphdemo::Scene * root = loaded[0];
        CHECK( strcmp( root->name, "tree" ) == 0 );
        CHECK( root->version == 2 );
        const graphdemo::ListNode * head = graphdemo::ListNodeAt( root->head );
        CHECK( head != NULL && head->value == 1 );
        const graphdemo::ListNode * second = head != NULL ? graphdemo::ListNodeAt( head->next ) : NULL;
        CHECK( second != NULL && second->value == 2 );
        const graphdemo::TreeNode * top = graphdemo::TreeNodeAt( root->tree );
        CHECK( top != NULL && strcmp( top->label, "top" ) == 0 );
        const graphdemo::Settings * settings = graphdemo::SettingsAt( root->settings );
        CHECK( settings != NULL && settings->quality == 3 );
        // AND THE MESSAGE RE-SAVES to the same bytes, so the numbering the
        // load rebuilt is the numbering the save derived
        static uint8_t again[16384];
        const int64_t rewrote = graphdemo::SceneSaveMessages( loaded, 1, again, sizeof( again ), &report );
        CHECK( rewrote == bytes && memcmp( again, message, (size_t) bytes ) == 0 );
    }

    // A BATCH OF THREE POINTERED ROOTS TAKES ONE REGION: measured once,
    // allocated once, each body under its own numbering
    {
        const graphdemo::Scene * three[3] = { builder.AsConst(), builder.AsConst(), builder.AsConst() };
        static uint8_t batch[65536];
        const int64_t batch_bytes = graphdemo::SceneSaveMessages( three, 3, batch, sizeof( batch ), &report );
        CHECK( batch_bytes > 0 && batch_bytes == graphdemo::SceneMeasureMessages( three, 3, &report ) );
        CHECK( batch_bytes < 3 * bytes ); // three bodies share one form byte and one count
        pin_table_golden( "graph_batch_message", batch, batch_bytes );
        int64_t batch_attribution = 0;
        const int64_t batch_need = graphdemo::SceneLoadMeasure( vocabulary, batch, batch_bytes, &batch_attribution );
        CHECK( batch_need == 3 * need && batch_attribution == 3 * attribution );
        static uint8_t batch_region[65536 * 3];
        const graphdemo::Scene * out[3] = { NULL, NULL, NULL };
        int64_t out_count = 3;
        CHECK( graphdemo::SceneLoadMessages( out, &out_count, batch_region, batch_need, vocabulary, batch, batch_bytes, &report ) );
        CHECK( out_count == 3 && out[0] != NULL && out[1] != NULL && out[2] != NULL );
        CHECK( out[2] != NULL && strcmp( out[2]->name, "tree" ) == 0 );
        const graphdemo::ListNode * batch_head = out[2] != NULL ? graphdemo::ListNodeAt( out[2]->head ) : NULL;
        CHECK( batch_head != NULL && batch_head->value == 1 );
    }

    // A ROOT THAT REACHES NO NODE ELIDES THE FIELD: no reserved reference at all
    {
        graphdemo::SceneBuilder empty;
        graphdemo::Scene * root = empty.GetRoot();
        set_string( root->name, root->name_length, "empty" );
        CHECK( empty.Lock() );
        const graphdemo::Scene * one[1] = { empty.AsConst() };
        static uint8_t small[256];
        const int64_t small_bytes = graphdemo::SceneSaveMessages( one, 1, small, sizeof( small ), &report );
        CHECK( small_bytes > 0 );
        pin_table_golden( "graph_empty_message", small, small_bytes );
        graphdemo::TableBitReader r( small + 1, small_bytes - 1 );
        uint64_t small_count = 0, ref = 0;
        CHECK( r.get( small_count, 8 ) && r.get( ref, vocabulary.ref_bits ) );
        CHECK( graphdemo::TableVocabularyEntryAt( vocabulary, ref ).id != graphdemo::kTableNodeTableFieldId );
        int64_t attribution_empty = 0;
        const int64_t need_empty = graphdemo::SceneLoadMeasure( vocabulary, small, small_bytes, &attribution_empty );
        CHECK( need_empty > 0 && attribution_empty == (int64_t) sizeof( graphdemo::TableNodeDirEntry ) );
        static uint8_t region_empty[4096];
        const graphdemo::Scene * loaded_empty[1] = { NULL };
        int64_t count_empty = 1;
        CHECK( graphdemo::SceneLoadMessages( loaded_empty, &count_empty, region_empty, need_empty, vocabulary, small, small_bytes, &report ) );
        CHECK( loaded_empty[0] != NULL && strcmp( loaded_empty[0]->name, "empty" ) == 0 && graphdemo::ListNodeAt( loaded_empty[0]->head ) == NULL );
    }
}

// ---- PER-DIRECTION INDEPENDENCE (docs/SPEC-TABLES.md §3.3) -----------------
//
// A peer holds TWO vocabularies for a connection, the one it writes with and
// the one it reads with, and neither is the other's. Two peers at different
// units is the ordinary case, and each decodes the other's messages against
// the vocabulary THAT peer announced. Red if a leg resolves against its own.

static void test_message_form_two_peers()
{
    // peer A speaks backenddemo, peer B speaks vocabdemo
    const backenddemo::TableVocabulary from_a = backend_connection();
    vocabdemo::TableVocabulary from_b;
    static uint8_t b_announcement[8192];
    const int64_t b_announced = vocabdemo::Announce( b_announcement, sizeof( b_announcement ) );
    CHECK( vocabdemo::AnnounceRead( from_b, b_announcement, b_announced, NULL ) );

    // THE TWO VOCABULARIES ARE DIFFERENT, which is what makes the pair a control
    CHECK( from_a.count != from_b.count );
    CHECK( from_a.ref_bits != from_b.ref_bits );
    CHECK( from_a.build_version != from_b.build_version );

    static backenddemo::StorePurchase a_value;
    build_backend_store( a_value );
    static uint8_t a_message[1024];
    backenddemo::TableReport a_report;
    const int64_t a_bytes = backenddemo::StorePurchaseSaveMessages( &a_value, 1, a_message, sizeof( a_message ), &a_report );
    pin_table_golden( "peer_a_message", a_message, a_bytes );
    static uint8_t a_file[1024];
    pin_table_golden( "peer_a", a_file, backenddemo::StorePurchaseSave( a_value, a_file, sizeof( a_file ) ) );

    static vocabdemo::Wide00 b_value;
    b_value.field_00_00 = 11;
    b_value.field_00_12 = 22;
    static uint8_t b_message[1024];
    vocabdemo::TableReport b_report;
    const int64_t b_bytes = vocabdemo::Wide00SaveMessages( &b_value, 1, b_message, sizeof( b_message ), &b_report );
    pin_table_golden( "peer_b_message", b_message, b_bytes );
    static uint8_t b_file[1024];
    pin_table_golden( "peer_b", b_file, vocabdemo::Wide00Save( b_value, b_file, sizeof( b_file ) ) );

    // each side decodes against the vocabulary THAT peer announced
    backenddemo::StorePurchase a_read;
    int64_t a_count = 1;
    CHECK( backenddemo::StorePurchaseLoadMessages( &a_read, &a_count, from_a, a_message, a_bytes, &a_report ) );
    CHECK( a_read.price_minor == 499 && a_read.quantity == 7 );
    vocabdemo::Wide00 b_read;
    int64_t b_count = 1;
    CHECK( vocabdemo::Wide00LoadMessages( &b_read, &b_count, from_b, b_message, b_bytes, &b_report ) );
    CHECK( b_read.field_00_00 == 11 && b_read.field_00_12 == 22 );
}

// ---- THE REFUSALS THIS FORM ADDS (docs/SPEC-TABLES.md §3.3, §11) -----------

static void test_message_form_refusals()
{
    static uint8_t announcement[4096];
    const int64_t announced = backenddemo::Announce( announcement, sizeof( announcement ) );

    // A MESSAGE WITH NO VOCABULARY FOR THE CONNECTION is refused BY NAME:
    // nothing is decoded, the reader says it holds no vocabulary, and
    // malformed does not fire. It does not fall back to the file form and
    // does not guess a vocabulary.
    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t message[256];
        backenddemo::TableReport report;
        const int64_t bytes = backenddemo::LoginRequestSaveMessages( &value, 1, message, sizeof( message ), &report );
        backenddemo::TableVocabulary empty;
        backenddemo::LoginRequest out;
        backenddemo::LoginRequestReset( out );
        int64_t count = 1;
        CHECK( !backenddemo::LoginRequestLoadMessages( &out, &count, empty, message, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary );
        CHECK( !report.malformed );
        CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
        CHECK( out.player_id == 0 && count == 0 ); // nothing was decoded: the declared default stands
        pin_table_golden( "message_no_vocabulary", message, bytes );
    }

    // A FORM 2 WIRE WHERE A FILE WAS EXPECTED is refused by name, because a
    // batch stored on its own is not readable: its vocabulary is somewhere else.
    {
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t message[256];
        backenddemo::TableReport report;
        const int64_t bytes = backenddemo::LoginRequestSaveMessages( &value, 1, message, sizeof( message ), &report );
        backenddemo::LoginRequest out;
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
        int64_t count = 1;
        backenddemo::TableReport report;
        CHECK( !backenddemo::LoginRequestLoadMessages( &out, &count, vocabulary, file, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::newer_form && !report.malformed );
    }

    // A SECOND ANNOUNCEMENT ON A CONNECTION IS REFUSED BY NAME. It does not
    // replace the vocabulary, it does not amend it, and it changes nothing;
    // closing the connection is the application's act.
    {
        backenddemo::TableVocabulary vocabulary;
        CHECK( backenddemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
        const int64_t was = vocabulary.count;
        const uint64_t was_version = vocabulary.build_version;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, announcement, announced, &report ) );
        CHECK( report.refused && report.reason == backenddemo::second_announcement );
        CHECK( !report.malformed );
        CHECK( vocabulary.announced && vocabulary.count == was && vocabulary.build_version == was_version );
        pin_table_golden( "message_second_announcement", announcement, announced );
        // and a DIFFERENT second announcement changes nothing either
        static uint8_t other[8192];
        const int64_t other_bytes = vocabdemo::Announce( other, sizeof( other ) );
        backenddemo::TableReport other_report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, other, other_bytes, &other_report ) );
        CHECK( other_report.refused && other_report.reason == backenddemo::second_announcement );
        CHECK( vocabulary.count == was && vocabulary.build_version == was_version );
    }

    // A VOCABULARY PAST A BOUND IS REFUSED BEFORE ANYTHING IS ALLOCATED, and
    // the bound is TWO numbers: the entry count, and the vocabulary's bytes
    // read off the field's own L before an entry is touched.
    {
        backenddemo::TableVocabulary vocabulary;
        vocabulary.max_entries = 27; // one below what this unit announces
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, announcement, announced, &report ) );
        CHECK( report.refused && report.reason == backenddemo::vocabulary_too_large );
        CHECK( !report.malformed && !vocabulary.announced );
        CHECK( vocabulary.vocabulary == NULL && vocabulary.count == 0 );
        // and exactly at the bound it is read
        backenddemo::TableVocabulary exact;
        exact.max_entries = 28;
        CHECK( backenddemo::AnnounceRead( exact, announcement, announced, NULL ) );
        // the BYTE bound: the unit's vocabulary is 273 bytes of entries
        backenddemo::TableVocabulary bytes_bound;
        bytes_bound.max_bytes = 272;
        backenddemo::TableReport bytes_report;
        CHECK( !backenddemo::AnnounceRead( bytes_bound, announcement, announced, &bytes_report ) );
        CHECK( bytes_report.refused && bytes_report.reason == backenddemo::vocabulary_too_large && !bytes_bound.announced );
        backenddemo::TableVocabulary bytes_exact;
        bytes_exact.max_bytes = 273;
        CHECK( backenddemo::AnnounceRead( bytes_exact, announcement, announced, NULL ) );
        CHECK( bytes_exact.vocabulary_bytes == 273 );
    }

    // A REFUSED ANNOUNCEMENT SETS NO VOCABULARY, so every message on that
    // connection is refused for want of one until the connection ends.
    {
        backenddemo::TableVocabulary vocabulary;
        vocabulary.max_entries = 4;
        CHECK( !backenddemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
        static backenddemo::LoginRequest value;
        build_backend_login( value );
        static uint8_t message[256];
        backenddemo::TableReport report;
        const int64_t bytes = backenddemo::LoginRequestSaveMessages( &value, 1, message, sizeof( message ), &report );
        backenddemo::LoginRequest out;
        int64_t count = 1;
        CHECK( !backenddemo::LoginRequestLoadMessages( &out, &count, vocabulary, message, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary );
    }
}

// ---- THE BATCH'S FIVE ANSWERS (docs/SPEC-TABLES.md §3.3) --------------------

static void test_message_form_five_answers()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();

    // M ABOVE 256 ON THE WRITE SIDE: both verbs refuse by name, nothing written
    {
        static backenddemo::LoginRequest many[257];
        for ( int32_t i = 0; i < 257; i++ ) { backenddemo::LoginRequestReset( many[i] ); }
        backenddemo::TableReport measure_report;
        CHECK( backenddemo::LoginRequestMeasureMessages( many, 257, &measure_report ) == -1 );
        CHECK( measure_report.refused && measure_report.reason == backenddemo::batch_too_large && !measure_report.malformed );
        static uint8_t buffer[8192];
        memset( buffer, 0xAB, sizeof( buffer ) );
        backenddemo::TableReport save_report;
        CHECK( backenddemo::LoginRequestSaveMessages( many, 257, buffer, sizeof( buffer ), &save_report ) == -1 );
        CHECK( save_report.refused && save_report.reason == backenddemo::batch_too_large && !save_report.malformed );
        bool untouched = true;
        for ( size_t i = 0; i < sizeof( buffer ); i++ ) { if ( buffer[i] != 0xAB ) { untouched = false; break; } }
        CHECK( untouched ); // nothing was written: no consecutive batches on the caller's behalf
    }

    // M ABOVE THE CALLER'S CAPACITY ON THE READ SIDE: refused by name from the
    // count, nothing decoded, no counter moved, and the returned count is the
    // wire's M so the caller knows what to call again with
    {
        static backenddemo::LoginRequest many[256];
        for ( int32_t i = 0; i < 256; i++ ) { backenddemo::LoginRequestReset( many[i] ); many[i].client_build = 7; }
        static uint8_t wide[8192];
        backenddemo::TableReport report;
        const int64_t wide_bytes = backenddemo::LoginRequestSaveMessages( many, 256, wide, sizeof( wide ), &report );
        static backenddemo::LoginRequest eight[8];
        for ( int32_t i = 0; i < 8; i++ ) { backenddemo::LoginRequestReset( eight[i] ); }
        int64_t count = 8;
        backenddemo::TableReport small;
        CHECK( !backenddemo::LoginRequestLoadMessages( eight, &count, vocabulary, wide, wide_bytes, &small ) );
        CHECK( small.refused && small.reason == backenddemo::batch_too_large );
        CHECK( !small.malformed && small.unknown == 0 && small.kind_mismatch == 0 && small.clamped == 0 );
        CHECK( count == 256 ); // the wire's M, not the caller's capacity
        CHECK( eight[0].client_build == 0 ); // nothing decoded
        // and the recovery is one call with capacity at or above it
        static backenddemo::LoginRequest room[256];
        int64_t again = 256;
        backenddemo::TableReport ok;
        CHECK( backenddemo::LoginRequestLoadMessages( room, &again, vocabulary, wide, wide_bytes, &ok ) );
        CHECK( again == 256 && room[255].client_build == 7 );
    }

    // DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1: a three-body batch
    // damaged inside the second returns one
    {
        static backenddemo::LoginRequest three[3];
        for ( int32_t i = 0; i < 3; i++ ) { backenddemo::LoginRequestReset( three[i] ); three[i].client_build = (uint32_t) ( 100 + i ); }
        static uint8_t batch[256];
        backenddemo::TableReport report;
        const int64_t bytes = backenddemo::LoginRequestSaveMessages( three, 3, batch, sizeof( batch ), &report );
        CHECK( bytes == 1 + ( 8 + 3 * 42 + 7 ) / 8 );
        // body 1 spans bits 8..49 of the stream; body 2's reference begins at
        // bit 50. Plant a reference of 31, the largest five bits spell and
        // three past E, in place of body 2's field reference.
        static uint8_t damaged[256];
        memcpy( damaged, batch, (size_t) bytes );
        for ( int64_t bit = 50; bit < 55; bit++ ) { damaged[ 1 + bit / 8 ] = (uint8_t) ( damaged[ 1 + bit / 8 ] | ( 1 << ( bit % 8 ) ) ); }
        pin_table_golden( "backend_batch_damaged_second_message", damaged, bytes );
        static backenddemo::LoginRequest out[3];
        for ( int32_t i = 0; i < 3; i++ ) { backenddemo::LoginRequestReset( out[i] ); out[i].client_build = 999; }
        int64_t count = 3;
        backenddemo::TableReport damage;
        CHECK( !backenddemo::LoginRequestLoadMessages( out, &count, vocabulary, damaged, bytes, &damage ) );
        CHECK( damage.malformed && !damage.refused );
        CHECK( count == 1 ); // k - 1
        CHECK( out[0].client_build == 100 ); // the first body stands
        CHECK( out[2].client_build == 999 ); // the third was never read
    }

    // AND A POINTERED BATCH TAKES ONE REGION: held by test_message_form_pointered
}

// ---- DAMAGE IS TERMINAL FOR THE BATCH (docs/SPEC-TABLES.md §3.3) -----------
//
// Damage planted inside the SECOND body of three, and inside a NESTED body of
// the second. Red if a leg reads the third body, or discards the first.

static void test_message_form_damage_terminal()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    static backenddemo::MatchResult three[3];
    for ( int32_t i = 0; i < 3; i++ ) { backenddemo::MatchResultReset( three[i] ); three[i].match_id = (uint64_t) ( 10 + i ); three[i].players[0].score = 5; }
    static uint8_t batch[1024];
    backenddemo::TableReport report;
    const int64_t bytes = backenddemo::MatchResultSaveMessages( three, 3, batch, sizeof( batch ), &report );
    CHECK( bytes > 0 );
    // each body: match_id (5 + 64), players (5, no count), ten rows of which
    // the first carries score (5 + 17) and a terminator (5) and nine are
    // terminators alone (5 each), then the body's terminator (5): 151 bits
    const int64_t body_bits = 5 + 64 + 5 + ( 5 + 17 + 5 ) + 9 * 5 + 5;
    CHECK( bytes == 1 + ( 8 + 3 * body_bits + 7 ) / 8 );

    // inside the SECOND body: its match_id reference becomes 31, three past E
    {
        static uint8_t damaged[1024];
        memcpy( damaged, batch, (size_t) bytes );
        const int64_t at = 8 + body_bits;
        for ( int64_t bit = at; bit < at + 5; bit++ ) { damaged[ 1 + bit / 8 ] = (uint8_t) ( damaged[ 1 + bit / 8 ] | ( 1 << ( bit % 8 ) ) ); }
        pin_table_golden( "match_batch_damaged_second_message", damaged, bytes );
        static backenddemo::MatchResult out[3];
        for ( int32_t i = 0; i < 3; i++ ) { backenddemo::MatchResultReset( out[i] ); out[i].match_id = 999; }
        int64_t count = 3;
        backenddemo::TableReport damage;
        CHECK( !backenddemo::MatchResultLoadMessages( out, &count, vocabulary, damaged, bytes, &damage ) );
        CHECK( damage.malformed && !damage.refused && damage.unknown == 0 );
        CHECK( count == 1 && out[0].match_id == 10 && out[2].match_id == 999 );
    }
    // inside a NESTED body of the second: the first PlayerRow's score reference
    {
        static uint8_t damaged[1024];
        memcpy( damaged, batch, (size_t) bytes );
        const int64_t at = 8 + body_bits + 5 + 64 + 5; // past match_id and the players reference
        for ( int64_t bit = at; bit < at + 5; bit++ ) { damaged[ 1 + bit / 8 ] = (uint8_t) ( damaged[ 1 + bit / 8 ] | ( 1 << ( bit % 8 ) ) ); }
        pin_table_golden( "match_batch_damaged_nested_message", damaged, bytes );
        static backenddemo::MatchResult out[3];
        for ( int32_t i = 0; i < 3; i++ ) { backenddemo::MatchResultReset( out[i] ); out[i].match_id = 999; }
        int64_t count = 3;
        backenddemo::TableReport damage;
        CHECK( !backenddemo::MatchResultLoadMessages( out, &count, vocabulary, damaged, bytes, &damage ) );
        CHECK( damage.malformed && !damage.refused && damage.unknown == 0 );
        CHECK( count == 1 && out[0].match_id == 10 && out[2].match_id == 999 );
        CHECK( out[1].match_id == 11 ); // the fields decoded before the damage stand, and the COUNT says it is not a body
    }
}

// ---- THE PAD, AND WHAT FOLLOWS IT (docs/SPEC-TABLES.md §3.3) ---------------

static void test_message_form_pad()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    static backenddemo::LoginRequest value;
    build_backend_login( value );
    static uint8_t message[256];
    backenddemo::TableReport report;
    const int64_t bytes = backenddemo::LoginRequestSaveMessages( &value, 1, message, sizeof( message ), &report );
    CHECK( bytes == 51 ); // 404 bits: the last byte carries four bits of body and four of pad

    // a pad bit that is not zero
    {
        static uint8_t bad_pad[256];
        memcpy( bad_pad, message, (size_t) bytes );
        bad_pad[bytes - 1] = (uint8_t) ( bad_pad[bytes - 1] | 0x80 );
        pin_table_golden( "login_full_bad_pad_message", bad_pad, bytes );
        backenddemo::LoginRequest out;
        int64_t count = 1;
        backenddemo::TableReport pad;
        CHECK( !backenddemo::LoginRequestLoadMessages( &out, &count, vocabulary, bad_pad, bytes, &pad ) );
        CHECK( pad.malformed && !pad.refused );
        CHECK( out.client_build == 140233 ); // the body decoded before the pad stands, and the count says so
        CHECK( count == 1 );
    }
    // a byte after the pad
    {
        static uint8_t trailing[256];
        memcpy( trailing, message, (size_t) bytes );
        trailing[bytes] = 0;
        pin_table_golden( "login_full_trailing_byte_message", trailing, bytes + 1 );
        backenddemo::LoginRequest out;
        int64_t count = 1;
        backenddemo::TableReport extra;
        CHECK( !backenddemo::LoginRequestLoadMessages( &out, &count, vocabulary, trailing, bytes + 1, &extra ) );
        CHECK( extra.malformed && !extra.refused );
    }
}

// ---- THE MASK'S WIDTH (docs/SPEC-TABLES.md §3.3, §4) -----------------------
//
// A `flags` field of three variants: three bits on the message wire, eight
// bytes in a file. A file carrying a bit above W, saved as a message, drops it,
// which is the §4 round-trip row the form moves.

static void test_message_form_mask_width()
{
    tabledemo::TableVocabulary vocabulary;
    static uint8_t announcement[8192];
    const int64_t announced = tabledemo::Announce( announcement, sizeof( announcement ) );
    CHECK( tabledemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );

    tabledemo::LoadoutConfig plain;
    tabledemo::LoadoutConfigReset( plain );
    tabledemo::LoadoutConfig masked;
    tabledemo::LoadoutConfigReset( masked );
    masked.perks = 5; // Shielded and Turbo: two of the three bits
    backenddemo::TableReport unused;
    (void) unused;
    tabledemo::TableReport report;
    // THREE BITS ON THE MESSAGE WIRE: the reference and W bits
    const int64_t plain_bits = tabledemo::LoadoutConfigMeasureMessageBody( 8, plain );
    const int64_t masked_bits = tabledemo::LoadoutConfigMeasureMessageBody( 8, masked );
    CHECK( masked_bits - plain_bits == tabledemo::kTableMessageRefBitsHere + 3 );
    // EIGHT BYTES IN A FILE: the header and a raw u64
    const int64_t plain_file = tabledemo::LoadoutConfigMeasure( plain );
    const int64_t masked_file = tabledemo::LoadoutConfigMeasure( masked );
    CHECK( masked_file - plain_file == 1 + 1 + 8 + 8 ); // the header, the raw u64, and the trailer entry the new id costs

    // A BIT ABOVE W survives a FILE round trip and not a MESSAGE one
    tabledemo::LoadoutConfig appended;
    tabledemo::LoadoutConfigReset( appended );
    appended.perks = ( 1u << 3 ) | 1u; // a fourth bit no variant names, beside Shielded
    static uint8_t file[4096];
    const int64_t file_bytes = tabledemo::LoadoutConfigSave( appended, file, sizeof( file ) );
    CHECK( file_bytes > 0 );
    pin_table_golden( "loadout_appended_bit", file, file_bytes );
    tabledemo::LoadoutConfig from_file;
    tabledemo::LoadoutConfigReset( from_file );
    CHECK( tabledemo::LoadoutConfigLoad( from_file, file, file_bytes, &report ) && !report.malformed );
    CHECK( from_file.perks == ( ( 1u << 3 ) | 1u ) ); // the file kept the appended bit
    static uint8_t message[4096];
    const int64_t message_bytes = tabledemo::LoadoutConfigSaveMessages( &from_file, 1, message, sizeof( message ), &report );
    CHECK( message_bytes > 0 );
    pin_table_golden( "loadout_appended_bit_message", message, message_bytes );
    tabledemo::LoadoutConfig from_message;
    tabledemo::LoadoutConfigReset( from_message );
    int64_t count = 1;
    CHECK( tabledemo::LoadoutConfigLoadMessages( &from_message, &count, vocabulary, message, message_bytes, &report ) && !report.malformed );
    CHECK( from_message.perks == 1u ); // the message DROPPED the bit above W, by width
}

// ---- THE ANNOUNCEMENT'S TWO STRICT CHECKS, AND ITS TOLERANCE ---------------
//
// The build version present, exactly once, under kind 9, eight bytes wide, and
// the vocabulary present, exactly once, under kind 14 over element kind 6 —
// and everything else in its body an ordinary field under §4's tolerance, so
// an unknown one is skipped and counted and the announcement can GAIN a field
// in a later minor without a lockstep redeploy.

// forge_announcement builds an announcement from a body of the caller's own
// making over a trailer of the caller's ids, which is what lets a row change
// one fact. The unit's own vocabulary bytes are what a well-formed body carries.
static int64_t forge_announcement( uint8_t * out, int64_t capacity, const uint8_t * body, int64_t body_bytes,
                                   const uint64_t * ids, int64_t id_count )
{
    const int64_t trailer = id_count * 8 + 8;
    const int64_t total = 1 + body_bytes + trailer;
    if ( total > capacity ) { return -1; }
    out[0] = 1;
    memcpy( out + 1, body, (size_t) body_bytes );
    int64_t at = 1 + body_bytes;
    for ( int64_t i = 0; i < id_count; i++ )
    {
        for ( int b = 0; b < 8; b++ ) { out[at++] = (uint8_t) ( ids[i] >> ( 8 * b ) ); }
    }
    for ( int b = 0; b < 8; b++ ) { out[at++] = (uint8_t) ( (uint64_t) id_count >> ( 8 * b ) ); }
    return total;
}

// the unit's own vocabulary field, byte for byte, out of its announcement
static int64_t backend_vocabulary_field( uint8_t * out, int64_t capacity )
{
    static uint8_t announcement[4096];
    const int64_t announced = backenddemo::Announce( announcement, sizeof( announcement ) );
    backenddemo::TableVocabulary vocabulary;
    CHECK( backenddemo::AnnounceRead( vocabulary, announcement, announced, NULL ) );
    // reference 2, kind 14, L, element kind 6, the byte count, the bytes
    const int64_t n = vocabulary.vocabulary_bytes;
    int64_t at = 0;
    out[at++] = 2;
    out[at++] = 14;
    const int64_t inner = 1 + 2 + n; // element kind, a two-byte LEB128 count, the bytes
    CHECK( n >= 128 && n < 16384 );
    out[at++] = (uint8_t) ( 0x80 | ( inner & 0x7F ) );
    out[at++] = (uint8_t) ( inner >> 7 );
    out[at++] = 6;
    out[at++] = (uint8_t) ( 0x80 | ( n & 0x7F ) );
    out[at++] = (uint8_t) ( n >> 7 );
    CHECK( at + n <= capacity );
    memcpy( out + at, vocabulary.vocabulary, (size_t) n );
    return at + n;
}

static void test_message_form_announcement_check()
{
    static uint8_t forged[8192];
    static uint8_t body[8192];
    static uint8_t vocabulary_field[4096];
    const int64_t field_bytes = backend_vocabulary_field( vocabulary_field, sizeof( vocabulary_field ) );
    const uint64_t ids[3] = { backenddemo::kTableBuildVersionFieldId, backenddemo::kTableMessageVocabularyFieldId, 0x1122334455667788ull };
    const uint8_t version_field[10] = { 1, 9, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88 };

    struct Row { const char * name; int64_t bytes; };
    // each row builds a body, expects a refusal, and pins the wire
    // BUILD VERSION ABSENT: the vocabulary alone
    {
        int64_t at = 0;
        memcpy( body + at, vocabulary_field, (size_t) field_bytes ); at += field_bytes;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_no_build_version", forged, bytes );
    }
    // BUILD VERSION TWICE
    {
        int64_t at = 0;
        memcpy( body + at, version_field, 10 ); at += 10;
        memcpy( body + at, version_field, 10 ); at += 10;
        memcpy( body + at, vocabulary_field, (size_t) field_bytes ); at += field_bytes;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_build_version_twice", forged, bytes );
    }
    // BUILD VERSION UNDER A KIND OTHER THAN 9: kind 8, a u32
    {
        int64_t at = 0;
        const uint8_t wrong[6] = { 1, 8, 1, 0, 0, 0 };
        memcpy( body + at, wrong, 6 ); at += 6;
        memcpy( body + at, vocabulary_field, (size_t) field_bytes ); at += field_bytes;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_build_version_kind", forged, bytes );
    }
    // BUILD VERSION AT A WIDTH THAT IS NOT EIGHT: kind 9 with four bytes left
    {
        const uint8_t narrow[6] = { 1, 9, 1, 0, 0, 0 };
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), narrow, 6, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && !vocabulary.announced );
        pin_table_golden( "announce_build_version_width", forged, bytes );
    }
    // VOCABULARY ABSENT: the build version alone
    {
        int64_t at = 0;
        memcpy( body + at, version_field, 10 ); at += 10;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_no_vocabulary", forged, bytes );
    }
    // VOCABULARY TWICE
    {
        int64_t at = 0;
        memcpy( body + at, version_field, 10 ); at += 10;
        memcpy( body + at, vocabulary_field, (size_t) field_bytes ); at += field_bytes;
        memcpy( body + at, vocabulary_field, (size_t) field_bytes ); at += field_bytes;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_vocabulary_twice", forged, bytes );
    }
    // VOCABULARY UNDER A WRONG ELEMENT KIND: kind 14 over element kind 8
    {
        int64_t at = 0;
        memcpy( body + at, version_field, 10 ); at += 10;
        const uint8_t wrong[8] = { 2, 14, 6, 8, 1, 0, 0, 0 }; // an array of one u32
        memcpy( body + at, wrong, 8 ); at += 8;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( report.refused && report.reason == backenddemo::no_vocabulary && !vocabulary.announced );
        pin_table_golden( "announce_vocabulary_element_kind", forged, bytes );
    }
    // AND THE TOLERANT ROW: an UNKNOWN field beside both sets the vocabulary
    // and counts one unknown. This is the whole reason the announcement is a
    // table body rather than a fixed header.
    {
        int64_t at = 0;
        memcpy( body + at, version_field, 10 ); at += 10;
        const uint8_t unknown[6] = { 3, 8, 9, 0, 0, 0 }; // slot 3 is an ordinary id, under kind 8 with a u32 payload
        memcpy( body + at, unknown, 6 ); at += 6;
        memcpy( body + at, vocabulary_field, (size_t) field_bytes ); at += field_bytes;
        body[at++] = 0;
        const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 3 );
        backenddemo::TableVocabulary vocabulary;
        backenddemo::TableReport report;
        CHECK( backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
        CHECK( vocabulary.announced );
        CHECK( vocabulary.build_version == 0x8877665544332211ull );
        CHECK( vocabulary.count == 28 );
        CHECK( report.unknown == 1 );
        CHECK( !report.refused && !report.malformed );
        pin_table_golden( "announce_unknown_field", forged, bytes );
    }
}

// ---- THE THREE RESERVED IDS WHERE THEY DO NOT BELONG (§3.1, §3.3, §5) ------
//
// A reserved id in a FILE body and in a nested file body counts malformed and
// nothing else; a reserved id where an entry's id belongs in an ANNOUNCEMENT's
// vocabulary refuses the announcement as malformed and sets no vocabulary. A
// message body carries references and never an id, so it has no row here.

static void test_message_form_reserved_ids()
{
    const uint64_t reserved[3] = { 0xFFFFFFFFFFFFFFFFull, 0xFFFFFFFFFFFFFFFEull, 0xFFFFFFFFFFFFFFFDull };
    const char * names[3] = { "node_table", "build_version", "vocabulary" };
    for ( int i = 0; i < 3; i++ )
    {
        // in a FILE body: slot 1 is the reserved id, under kind 9 with eight bytes
        {
            uint8_t file[32];
            int64_t at = 0;
            file[at++] = 1; // the form byte
            file[at++] = 1; file[at++] = 9; for ( int b = 0; b < 8; b++ ) { file[at++] = 0; }
            file[at++] = 0; // the terminator
            for ( int b = 0; b < 8; b++ ) { file[at++] = (uint8_t) ( reserved[i] >> ( 8 * b ) ); }
            file[at++] = 1; for ( int b = 1; b < 8; b++ ) { file[at++] = 0; } // one entry
            backenddemo::LoginRequest out;
            backenddemo::TableReport report;
            backenddemo::LoginRequestLoad( out, file, at, &report );
            if ( i == 0 )
            {
                // THE ROOT BODY IS THE NODE TABLE'S OWN TRANSPORT (§3.1): a fixed
                // reader meeting one there is the table-gained-a-pointer edit, and
                // it reads it as the ordinary unknown it is
                CHECK( !report.malformed && report.unknown == 1 );
            }
            else
            {
                CHECK( report.malformed );
                CHECK( report.unknown == 0 );
            }
            CHECK( report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
            CHECK( !report.refused );
            char name[128];
            snprintf( name, sizeof( name ), "file_reserved_%s_root", names[i] );
            pin_table_golden( name, file, at );
        }
        // in a NESTED file body: MatchResult's players array holds PlayerRow
        // bodies, and one of them names the reserved id (slot 1); players is
        // slot 2
        {
            uint8_t file[48];
            int64_t at = 0;
            file[at++] = 1;
            file[at++] = 2; file[at++] = 14; file[at++] = 14; // players, kind 14, L
            file[at++] = 13; file[at++] = 1; // element kind 13, one element
            file[at++] = 11; // the element's L
            file[at++] = 1; file[at++] = 9; for ( int b = 0; b < 8; b++ ) { file[at++] = 0; }
            file[at++] = 0; // the element's terminator
            file[at++] = 0; // the root's terminator
            for ( int b = 0; b < 8; b++ ) { file[at++] = (uint8_t) ( reserved[i] >> ( 8 * b ) ); }
            const uint64_t players = field_id( "players" ); // the battery's own fnv1a64, not the generated one
            for ( int b = 0; b < 8; b++ ) { file[at++] = (uint8_t) ( players >> ( 8 * b ) ); }
            file[at++] = 2; for ( int b = 1; b < 8; b++ ) { file[at++] = 0; }
            backenddemo::MatchResult out;
            backenddemo::TableReport report;
            backenddemo::MatchResultLoad( out, file, at, &report );
            CHECK( report.malformed );
            CHECK( report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0 );
            CHECK( !report.refused );
            char name[128];
            snprintf( name, sizeof( name ), "file_reserved_%s_nested", names[i] );
            pin_table_golden( name, file, at );
        }
    }
    // in an ANNOUNCEMENT's vocabulary: the entry planted at kind 0
    {
        static uint8_t forged[8192];
        static uint8_t body[8192];
        static uint8_t vocabulary_field[4096];
        const int64_t field_bytes = backend_vocabulary_field( vocabulary_field, sizeof( vocabulary_field ) );
        const uint64_t ids[2] = { backenddemo::kTableBuildVersionFieldId, backenddemo::kTableMessageVocabularyFieldId };
        const uint8_t version_field[10] = { 1, 9, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88 };
        const uint64_t planted[3] = { 0xFFFFFFFFFFFFFFFEull, 0xFFFFFFFFFFFFFFFDull, 0xFFFFFFFFFFFFFFFFull };
        const char * planted_names[3] = { "build_version", "vocabulary", "second_node_table" };
        for ( int i = 0; i < 3; i++ )
        {
            int64_t at = 0;
            memcpy( body + at, version_field, 10 ); at += 10;
            // the unit's vocabulary field with ONE MORE entry appended: the
            // planted id at kind 0, nine bytes, so the field's two lengths grow
            // by nine
            memcpy( body + at, vocabulary_field, (size_t) field_bytes );
            const int64_t n = ( (int64_t) ( body[at + 5] & 0x7F ) ) | ( (int64_t) body[at + 6] << 7 );
            const int64_t inner = 1 + 2 + n + 9;
            body[at + 2] = (uint8_t) ( 0x80 | ( inner & 0x7F ) );
            body[at + 3] = (uint8_t) ( inner >> 7 );
            body[at + 5] = (uint8_t) ( 0x80 | ( ( n + 9 ) & 0x7F ) );
            body[at + 6] = (uint8_t) ( ( n + 9 ) >> 7 );
            at += field_bytes;
            for ( int b = 0; b < 8; b++ ) { body[at++] = (uint8_t) ( planted[i] >> ( 8 * b ) ); }
            body[at++] = 0; // kind 0
            body[at++] = 0; // the terminator
            const int64_t bytes = forge_announcement( forged, sizeof( forged ), body, at, ids, 2 );
            backenddemo::TableVocabulary vocabulary;
            backenddemo::TableReport report;
            CHECK( !backenddemo::AnnounceRead( vocabulary, forged, bytes, &report ) );
            CHECK( report.malformed && !report.refused && !vocabulary.announced );
            CHECK( report.unknown == 0 );
            char name[128];
            snprintf( name, sizeof( name ), "announce_reserved_%s", planted_names[i] );
            pin_table_golden( name, forged, bytes );
        }
    }
}

// ---- A REFERENCE AT AND ABOVE THE ENTRY COUNT (§3.3) ------------------------
//
// E is 28 and a reference is five bits, so 29, 30 and 31 are spellable and
// damage. The entry count ITSELF is the last legal slot and must resolve. The
// fields decoded before a bad reference stand, and nothing after it is read.

static void test_message_form_reference_bound()
{
    const backenddemo::TableVocabulary vocabulary = backend_connection();
    static backenddemo::LoginRequest value;
    backenddemo::LoginRequestReset( value );
    value.player_id = 5;
    static uint8_t message[64];
    backenddemo::TableReport report;
    const int64_t bytes = backenddemo::LoginRequestSaveMessages( &value, 1, message, sizeof( message ), &report );
    // player_id: reference (5) and u64 (64), then the terminator at bit 77 of
    // the stream: 8 + 5 + 64 = 77
    CHECK( bytes == 1 + ( 8 + 5 + 64 + 5 + 7 ) / 8 );
    const uint64_t past[3] = { 29, 30, 31 };
    for ( int i = 0; i < 3; i++ )
    {
        static uint8_t damaged[64];
        memcpy( damaged, message, (size_t) bytes );
        for ( int b = 0; b < 5; b++ )
        {
            const int64_t bit = 77 + b;
            if ( ( past[i] >> b ) & 1 ) { damaged[ 1 + bit / 8 ] = (uint8_t) ( damaged[ 1 + bit / 8 ] | ( 1 << ( bit % 8 ) ) ); }
            else { damaged[ 1 + bit / 8 ] = (uint8_t) ( damaged[ 1 + bit / 8 ] & ~( 1 << ( bit % 8 ) ) ); }
        }
        backenddemo::LoginRequest out;
        backenddemo::LoginRequestReset( out );
        int64_t count = 1;
        backenddemo::TableReport damage;
        CHECK( !backenddemo::LoginRequestLoadMessages( &out, &count, vocabulary, damaged, bytes, &damage ) );
        CHECK( damage.malformed && !damage.refused && damage.unknown == 0 );
        CHECK( out.player_id == 5 ); // the field decoded BEFORE the bad reference stands
        CHECK( count == 0 );
        if ( i == 2 ) { pin_table_golden( "message_reference_past_table", damaged, bytes ); }
    }
    // the entry count itself, 28, is the LAST LEGAL SLOT and must resolve: it
    // names StorePurchase's own type id, a kind-0 entry no field of
    // LoginRequest carries, so it is §4's ordinary unknown
    {
        static uint8_t last[64];
        memcpy( last, message, (size_t) bytes );
        for ( int b = 0; b < 5; b++ )
        {
            const int64_t bit = 77 + b;
            if ( ( 28u >> b ) & 1 ) { last[ 1 + bit / 8 ] = (uint8_t) ( last[ 1 + bit / 8 ] | ( 1 << ( bit % 8 ) ) ); }
            else { last[ 1 + bit / 8 ] = (uint8_t) ( last[ 1 + bit / 8 ] & ~( 1 << ( bit % 8 ) ) ); }
        }
        // then a terminator at bit 82, and the pad from 87 stays zero
        const int64_t last_bytes = 1 + ( 82 + 5 + 7 ) / 8;
        for ( int64_t bit = 82; bit < last_bytes * 8 - 8; bit++ ) { last[ 1 + bit / 8 ] = (uint8_t) ( last[ 1 + bit / 8 ] & ~( 1 << ( bit % 8 ) ) ); }
        backenddemo::LoginRequest out;
        backenddemo::LoginRequestReset( out );
        int64_t count = 1;
        backenddemo::TableReport ok;
        CHECK( backenddemo::LoginRequestLoadMessages( &out, &count, vocabulary, last, last_bytes, &ok ) );
        CHECK( !ok.malformed && !ok.refused );
        CHECK( ok.unknown == 1 && out.player_id == 5 && count == 1 );
        pin_table_golden( "message_reference_last_slot", last, last_bytes );
    }
}

#endif // SCHEMA_TEST_MESSAGE_FORM_H
