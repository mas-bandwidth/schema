// THE MESSAGE FORM'S CONTENT RULE AND BOUND FOR string(N)
// (docs/SPEC-TABLES.md §3.3, §4).
//
// §3's one content rule holds over a form-`2` body unchanged in what it
// rejects: a kind `12` payload that is not well-formed UTF-8, or that carries
// a zero byte among its bytes, is DAMAGE and not data. What differs is only
// the recovery, which a bit stream does not have, so the damage is TERMINAL
// for the batch. And A PAYLOAD PAST THE BOUND IS NOT DAMAGE: it is out of
// range, and the strict read refuses the whole batch with that reason rather
// than keeping a prefix. A payload exactly AT the bound, a multi-byte code
// point ending on it included, lands whole.
//
// The instrument is a batch forged by hand over `backenddemo`'s
// `StorePurchase.sku`, a `string(32)` whose length rides at
// bits_required( 0, 32 ) = 6 bits, so a payload of 34 bytes is spellable and
// the bound is reachable. The program is green under the emitter this
// repository ships and red under one that drops either rule, which is the
// whole of `make tables-message-form-text-negative-control`.

#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>

#include "BackendTable.h"

static int failures = 0;

static void row( bool ok, const char * name )
{
    if ( ok )
    {
        printf( "message text: %s\n", name );
        return;
    }
    printf( "message text FAILED: %s\n", name );
    failures++;
}

// ONE BATCH OF ONE BODY carrying `sku` and nothing else (§3.3): the form byte,
// the count as `M - 1`, the field's reference, the length, the ALIGN that buys
// a memcpy, the bytes, the body's zero reference, and the pad to the byte
// boundary.
static int64_t forge( uint8_t * out, int64_t capacity, const backenddemo::TableVocabulary & vocabulary,
                      int64_t slot, int64_t length_bits, const uint8_t * text, uint64_t length )
{
    backenddemo::TableBitWriter w( out, capacity );
    w.put( 2, 8 );
    w.put( 0, 8 );
    w.put( (uint64_t) slot, vocabulary.ref_bits );
    w.put( length, length_bits );
    w.align();
    w.putbytes( text, (int64_t) length );
    w.put( 0, vocabulary.ref_bits );
    w.align();
    if ( w.overflow ) { return -1; }
    return w.bits / 8;
}

struct read_back
{
    bool loaded;
    backenddemo::TableReport report;
    backenddemo::StorePurchase value;
};

static read_back load( const backenddemo::TableVocabulary & vocabulary, const uint8_t * batch, int64_t bytes )
{
    read_back out;
    int64_t count = 1;
    out.loaded = backenddemo::StorePurchaseLoadMessages( &out.value, &count, vocabulary, batch, bytes, &out.report );
    if ( out.loaded && count != 1 ) { out.loaded = false; }
    return out;
}

int main()
{
    std::vector<uint8_t> announcement( (size_t) backenddemo::AnnounceMeasure() );
    if ( backenddemo::Announce( announcement.data(), (int64_t) announcement.size() ) != (int64_t) announcement.size() )
    {
        printf( "message text: the announcement did not write\n" );
        return 2;
    }
    std::vector<backenddemo::TableMessageEntry> entries( (size_t) backenddemo::kTableMessageEntriesHere );
    backenddemo::TableVocabulary vocabulary( entries.data(), backenddemo::kTableMessageEntriesHere );
    if ( !backenddemo::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), NULL ) )
    {
        printf( "message text: this unit's own announcement was refused\n" );
        return 2;
    }

    // `sku` is the unit's ONLY kind 12 entry, so the vocabulary names it
    // without the test hardcoding a slot the announcement's order decides
    int64_t slot = 0, bound = 0;
    for ( int64_t at = 1; at <= vocabulary.count; at++ )
    {
        const backenddemo::TableMessageEntry e = backenddemo::TableVocabularyEntryAt( vocabulary, at );
        if ( e.kind != 12 ) { continue; }
        if ( slot != 0 )
        {
            printf( "message text: this unit announces more than one kind 12 entry\n" );
            return 2;
        }
        slot = at;
        bound = e.max;
    }
    if ( slot == 0 || bound != 32 )
    {
        printf( "message text: this unit announces no string(32) at kind 12\n" );
        return 2;
    }
    const int64_t length_bits = backenddemo::TableBitsRequired( 0, bound );

    uint8_t batch[128];

    // ILL-FORMED TEXT IS DAMAGE HERE TOO, AND IT IS TERMINAL (§3.3): a
    // TRUNCATED SEQUENCE is a payload that is not text at any length
    {
        const uint8_t text[5] = { 'p', 'a', 'c', 'k', 0xC3 };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, slot, length_bits, text, 5 );
        const read_back got = load( vocabulary, batch, bytes );
        row( !got.loaded && got.report.damaged && !got.report.refused,
             "a truncated UTF-8 sequence on a message body is damage, and the batch ends there" );
    }

    // A ZERO BYTE AMONG THE `L` BYTES is damage on the same rule (§3)
    {
        const uint8_t text[5] = { 'p', 'a', 0x00, 'c', 'k' };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, slot, length_bits, text, 5 );
        const read_back got = load( vocabulary, batch, bytes );
        row( !got.loaded && got.report.damaged && !got.report.refused,
             "a zero byte among a message body's text is damage, and the batch ends there" );
    }

    // AN OVERLONG ENCODING is ill-formed even though its bytes are a legal
    // shape: 0xC0 0x80 spells U+0000 in two bytes
    {
        const uint8_t text[4] = { 'p', 0xC0, 0x80, 'k' };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, slot, length_bits, text, 4 );
        const read_back got = load( vocabulary, batch, bytes );
        row( !got.loaded && got.report.damaged && !got.report.refused,
             "an overlong encoding on a message body is damage, and the batch ends there" );
    }

    // A PAYLOAD PAST THE BOUND REFUSES (§4): thirty ASCII bytes and one
    // four-byte code point are thirty-four bytes against thirty-two, and no
    // prefix of them lands, at a code point boundary or anywhere else
    {
        uint8_t text[34];
        memset( (void *) text, 'a', 30 );
        text[30] = 0xF0; text[31] = 0x9F; text[32] = 0x98; text[33] = 0x80; // U+1F600
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, slot, length_bits, text, 34 );
        const read_back got = load( vocabulary, batch, bytes );
        row( !got.loaded && !got.report.damaged && got.report.refuse_reason == backenddemo::out_of_range &&
                 got.value.sku_length == 0,
             "a payload past the bound whose last code point straddles it refuses as out_of_range" );
    }

    // and the same where the bound sits on a boundary: thirty-four ASCII bytes
    {
        uint8_t text[34];
        memset( (void *) text, 'b', 34 );
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, slot, length_bits, text, 34 );
        const read_back got = load( vocabulary, batch, bytes );
        row( !got.loaded && !got.report.damaged && got.report.refuse_reason == backenddemo::out_of_range &&
                 got.value.sku_length == 0,
             "a payload past the bound on a code point boundary refuses as out_of_range" );
    }

    // AND A PAYLOAD AT THE BOUND LANDS WHOLE, code point or not: the last
    // four bytes are one code point ending exactly at thirty-two
    {
        uint8_t text[32];
        memset( (void *) text, 'c', 28 );
        text[28] = 0xF0; text[29] = 0x9F; text[30] = 0x98; text[31] = 0x80; // U+1F600
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, slot, length_bits, text, 32 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.loaded && !got.report.damaged && got.report.refuse_reason == backenddemo::ok && got.value.sku_length == 32 &&
                 memcmp( got.value.sku, text, 32 ) == 0,
             "a payload at the bound lands whole and refuses nothing" );
    }

    if ( failures != 0 )
    {
        printf( "message text: %d row(s) red\n", failures );
        return 1;
    }
    printf( "message text: the message form carries string(N)'s content rule and its bound\n" );
    return 0;
}
