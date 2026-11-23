#include "BackendTable.h"
#include "BackendBlock.h"
#define SCHEMA_CONFORMANCE_PACKAGE backenddemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER BACKENDDEMO
#include "unit.h"
SCHEMA_CONFORMANCE_CODEC( LoginRequest, login_request )
SCHEMA_CONFORMANCE_CODEC( MatchResult, match_result )
SCHEMA_CONFORMANCE_CODEC( StorePurchase, store_purchase )
static const ConformanceCodec codecs[] = {
    SCHEMA_CONFORMANCE_ROW( backenddemo, LoginRequest ),
    SCHEMA_CONFORMANCE_ROW( backenddemo, MatchResult ),
    SCHEMA_CONFORMANCE_ROW( backenddemo, StorePurchase )
};
/* Sets the one field a write-contract row names (driver.h). */
int conformance_write_refusal_set_backenddemo( const char * root, void * value, const char * path,
                                               const char * kind, const char * text )
{
    const long long n = strtoll( text, NULL, 10 );
    if ( strcmp( root, "StorePurchase" ) == 0 )
    {
        backenddemo_store_purchase_t * v = (backenddemo_store_purchase_t *) value;
        if ( strcmp( path, "quantity" ) == 0 && strcmp( kind, "int" ) == 0 ) { v->quantity = (uint8_t) n; return 1; }
        if ( strcmp( path, "currency" ) == 0 && strcmp( kind, "ordinal" ) == 0 ) { v->currency = (backenddemo_currency_t) n; return 1; }
        if ( strcmp( path, "sku" ) == 0 ) { return conformance_write_refusal_text( v->sku, (int32_t) sizeof( v->sku ) - 1, &v->sku_length, kind, text ); }
    }
    if ( strcmp( root, "LoginRequest" ) == 0 )
    {
        backenddemo_login_request_t * v = (backenddemo_login_request_t *) value;
        if ( strcmp( path, "session_token" ) == 0 ) { return conformance_write_refusal_bytes( v->session_token, (int32_t) sizeof( v->session_token ), &v->session_token_length, kind, text ); }
    }
    if ( strcmp( root, "MatchResult" ) == 0 )
    {
        backenddemo_match_result_t * v = (backenddemo_match_result_t *) value;
        if ( strcmp( path, "players[0].score" ) == 0 && strcmp( kind, "int" ) == 0 ) { v->players[0].score = (int32_t) n; return 1; }
        if ( strcmp( path, "players" ) == 0 && strcmp( kind, "elems" ) == 0 ) { return conformance_write_refusal_elems( text, (long long) ( sizeof( v->players ) / sizeof( v->players[0] ) ) ); }
    }
    return 0;
}

/* THE BLOCK BUILDER over the row's value (docs/SPEC-TABLES.md §19.1): the
   storage is created, begin is handed the value's counts and settles the
   layout, the projection's fields are the producer's to write and are written
   from the value, and block_bytes closes it. `wrote` (a non-negative *wrote)
   when the block is built. The C builder takes the counts of out-of-line arrays
   and nothing else: a used length or a range is not an argument of begin. */
static int backend_block( const char * root, const void * value, int64_t * wrote )
{
    const backenddemo_table_block_allocator_t allocator = backenddemo_table_block_default_allocator();
    backenddemo_table_block_refusal_t refusal;
    memset( &refusal, 0, sizeof( refusal ) );
    if ( strcmp( root, "StorePurchase" ) == 0 )
    {
        const backenddemo_store_purchase_t * v = (const backenddemo_store_purchase_t *) value;
        backenddemo_store_purchase_block_storage_t storage;
        backenddemo_store_purchase_block_t block;
        backenddemo_store_purchase_counts_t counts; /* StorePurchase declares no out-of-line array */
        memset( &counts, 0, sizeof( counts ) );
        if ( !backenddemo_store_purchase_block_storage_create( &storage, &allocator ) ) { fprintf( stderr, "driver: block storage_create refused\n" ); exit( 2 ); }
        *wrote = -1;
        if ( backenddemo_store_purchase_block_begin( &block, &storage, &counts, &refusal ) )
        {
            block.projection->player_id = v->player_id;
            memcpy( block.projection->sku, v->sku, sizeof( block.projection->sku ) );
            block.projection->sku_length = v->sku_length;
            block.projection->quantity = v->quantity;
            block.projection->price_minor = v->price_minor;
            block.projection->currency = v->currency;
            *wrote = backenddemo_store_purchase_block_bytes( &block );
        }
        backenddemo_store_purchase_block_storage_destroy( &storage );
        return 1;
    }
    if ( strcmp( root, "MatchResult" ) == 0 )
    {
        const backenddemo_match_result_t * v = (const backenddemo_match_result_t *) value;
        backenddemo_match_result_block_storage_t storage;
        backenddemo_match_result_block_t block;
        backenddemo_match_result_counts_t counts; /* players is a [10] array held in the projection */
        size_t i;
        memset( &counts, 0, sizeof( counts ) );
        if ( !backenddemo_match_result_block_storage_create( &storage, &allocator ) ) { fprintf( stderr, "driver: block storage_create refused\n" ); exit( 2 ); }
        *wrote = -1;
        if ( backenddemo_match_result_block_begin( &block, &storage, &counts, &refusal ) )
        {
            block.projection->match_id = v->match_id;
            for ( i = 0; i < sizeof( v->players ) / sizeof( v->players[0] ); i++ ) { block.projection->players[i] = v->players[i]; }
            *wrote = backenddemo_match_result_block_bytes( &block );
        }
        backenddemo_match_result_block_storage_destroy( &storage );
        return 1;
    }
    return 0;
}

SCHEMA_CONFORMANCE_ANNOUNCE_1122

/* READ INTO THE CALLER'S VALUES (README, `read-into`), the message form: the
   connection's announcement is read, then load_messages into `count` values
   with ONE report through the lawful batch, the damaged batch, the text of
   every value, and the lawful batch again */
static int backend_read_into( const char * root, const char * form, const uint8_t * lawful, int64_t lawful_bytes,
                              const uint8_t * damaged, int64_t damaged_bytes, int64_t count,
                              const uint8_t * announcement, int64_t announcement_bytes, ConformanceText * answer )
{
    enum { vocab_cap = BACKENDDEMO_K_TABLE_MESSAGE_ENTRIES_HERE > 4096 ? BACKENDDEMO_K_TABLE_MESSAGE_ENTRIES_HERE : 4096 };
    static backenddemo_table_message_entry_t entries[vocab_cap];
    backenddemo_table_vocabulary_t vocabulary = backenddemo_table_vocabulary( entries, vocab_cap );
    backenddemo_table_report_t report, announced;
    backenddemo_store_purchase_t * values;
    int64_t i;
    int pass;
    if ( strcmp( root, "StorePurchase" ) != 0 || strcmp( form, "message" ) != 0 ) { return 1; }
    memset( &announced, 0, sizeof( announced ) );
    if ( !backenddemo_announce_read( &vocabulary, announcement, announcement_bytes, &announced ) )
    {
        fprintf( stderr, "driver: read-into: the connection's announcement did not read\n" );
        return 2;
    }
    values = (backenddemo_store_purchase_t *) calloc( (size_t) count, sizeof( backenddemo_store_purchase_t ) );
    if ( values == NULL ) { fprintf( stderr, "driver: read-into: out of memory\n" ); return 2; }
    for ( i = 0; i < count; i++ ) { backenddemo_store_purchase_reset( values + i ); }
    memset( &report, 0, sizeof( report ) );
    for ( pass = 0; pass < 3; pass++ )
    {
        const uint8_t * in = pass == 1 ? damaged : lawful;
        const int64_t bytes = pass == 1 ? damaged_bytes : lawful_bytes;
        int64_t loaded = count;
        int ok;
        if ( pass == 2 )
        {
            for ( i = 0; i < count; i++ )
            {
                const int64_t size = backenddemo_store_purchase_to_json_measure( values + i );
                char * text = (char *) malloc( (size_t) ( size > 0 ? size : 0 ) + 1 );
                if ( text == NULL || size < 0 || backenddemo_store_purchase_to_json( values + i, text, size + 1 ) != size )
                {
                    fprintf( stderr, "driver: read-into: to_json refused value %lld\n", (long long) i );
                    free( text );
                    free( values );
                    return 2;
                }
                conformance_text_raw( answer, text, (size_t) size );
                free( text );
            }
        }
        ok = backenddemo_store_purchase_load_messages( values, &loaded, &vocabulary, in, bytes, &report );
        conformance_text_add( answer, ok && !report.refused && report.refuse_reason == SCHEMA_BACKENDDEMO_TABLE_REFUSE_OK && report.damage_reason == 0 ? "read\n" : "refused\n" );
    }
    free( values );
    return 0;
}

static const ConformanceUnit1122 unit1122 = { "backenddemo", schema_conformance_announce_measure_, schema_conformance_announce_write_,
                                              schema_conformance_announce_read_, NULL, backend_read_into };
const ConformanceUnit1122 * conformance_unit1122_backenddemo( void ) { return &unit1122; }

/* The message form's SaveMessages, a batch of one, against the connection's
   announced vocabulary: the announcement is read into the vocabulary the
   `message` surface reads with, and one that does not read is no case. */
int conformance_write_refusal_write_backenddemo( int surface, const char * root, const void * value, const uint8_t * announcement,
                                                 int64_t announcement_bytes, uint8_t * buffer, int64_t capacity, int64_t * wrote )
{
    static backenddemo_table_message_entry_t entries[BACKENDDEMO_K_TABLE_MESSAGE_ENTRIES_HERE];
    backenddemo_table_vocabulary_t vocabulary = backenddemo_table_vocabulary( entries, BACKENDDEMO_K_TABLE_MESSAGE_ENTRIES_HERE );
    backenddemo_table_report_t report;
    memset( &report, 0, sizeof( report ) );
    if ( surface == CONFORMANCE_WRITE_BLOCK ) { return backend_block( root, value, wrote ); }
    if ( surface != CONFORMANCE_WRITE_MESSAGE ) { return 0; }
    if ( !backenddemo_announce_read( &vocabulary, announcement, announcement_bytes, &report ) ) { return 0; }
    if ( strcmp( root, "StorePurchase" ) == 0 ) { *wrote = backenddemo_store_purchase_save_messages( (const backenddemo_store_purchase_t *) value, 1, buffer, capacity, &report ); return 1; }
    if ( strcmp( root, "LoginRequest" ) == 0 ) { *wrote = backenddemo_login_request_save_messages( (const backenddemo_login_request_t *) value, 1, buffer, capacity, &report ); return 1; }
    if ( strcmp( root, "MatchResult" ) == 0 ) { *wrote = backenddemo_match_result_save_messages( (const backenddemo_match_result_t *) value, 1, buffer, capacity, &report ); return 1; }
    return 0;
}

const ConformanceCodec * conformance_codecs_backenddemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
