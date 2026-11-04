#include "BackendTable.h"
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
    if ( strcmp( root, "MatchResult" ) == 0 )
    {
        backenddemo_match_result_t * v = (backenddemo_match_result_t *) value;
        if ( strcmp( path, "players[0].score" ) == 0 && strcmp( kind, "int" ) == 0 ) { v->players[0].score = (int32_t) n; return 1; }
    }
    return 0;
}

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
    if ( surface != CONFORMANCE_WRITE_MESSAGE ) { return 0; }
    if ( !backenddemo_announce_read( &vocabulary, announcement, announcement_bytes, &report ) ) { return 0; }
    if ( strcmp( root, "StorePurchase" ) == 0 ) { *wrote = backenddemo_store_purchase_save_messages( (const backenddemo_store_purchase_t *) value, 1, buffer, capacity, &report ); return 1; }
    if ( strcmp( root, "MatchResult" ) == 0 ) { *wrote = backenddemo_match_result_save_messages( (const backenddemo_match_result_t *) value, 1, buffer, capacity, &report ); return 1; }
    return 0;
}

const ConformanceCodec * conformance_codecs_backenddemo( int * count )
{
    *count = (int)(sizeof(codecs)/sizeof(codecs[0])); return codecs;
}
