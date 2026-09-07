// THE MESSAGE FORM'S CONTENT RULE FOR A *string BLOB RECORD
// (docs/SPEC-TABLES.md §3, §3.1, §3.3).
//
// §3.1 states kinds `12` and `33`'s content rule MET AT A NODE: "A TEXT blob's
// CONTENT is refused on the same terms", so a `*string` blob whose bytes are
// not well-formed UTF-8, or which carries a zero byte, is damage and not data.
// §3.3 says a form-`2` body's content rules are §3's, unchanged in what they
// reject, and that what differs is only the recovery, which a bit stream does
// not have: the damage is TERMINAL for the batch.
//
// A `*bytes` blob has no such rule, because it is bytes and never text, and
// the row below that carries the same ill-formed bytes under the reserved
// `bytes` id holds the rule to the `string` id alone.
//
// The instrument is a batch forged by hand over `blobdemo`'s `Catalog`, whose
// numbering reaches a `*string` blob through `note` and a `*bytes` blob
// through `thumb`. A blob record is its own framing wherever it appears: the
// type reference, a length at thirty-two raw bits, the ALIGN, then the bytes
// verbatim. The program is green under the emitter this repository ships and
// red under one that drops the rule, which is the whole of
// `make tables-message-form-blob-negative-control`.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>

#include "AssetsTable.h"

static int failures = 0;

static void row( bool ok, const char * name )
{
    if ( ok )
    {
        printf( "message blob: %s\n", name );
        return;
    }
    printf( "message blob FAILED: %s\n", name );
    failures++;
}

// ONE BATCH OF ONE BODY carrying a NODE TABLE of one blob record and no field
// of its own (§3.1, §3.3): the form byte, the count as `M - 1`, the node
// table's own reference, the record count at thirty-two bits, the record's
// type reference, its length at thirty-two bits, the align, the bytes, the
// body's zero reference, and the pad to the byte boundary. A record is
// numbered and placed whether or not a slot names it, so the content rule is
// reached by the record alone.
static int64_t forge( uint8_t * out, int64_t capacity, const blobdemo::TableVocabulary & vocabulary,
                      uint64_t type_slot, const uint8_t * data, uint64_t length )
{
    blobdemo::TableBitWriter w( out, capacity );
    w.put( 2, 8 );
    w.put( 0, 8 );
    w.put( blobdemo::kTableNodeTableFieldSlot, vocabulary.ref_bits );
    w.put( 1, 32 );
    w.put( type_slot, vocabulary.ref_bits );
    w.put( length, 32 );
    w.align();
    w.putbytes( data, (int64_t) length );
    w.put( 0, vocabulary.ref_bits );
    w.align();
    if ( w.overflow ) { return -1; }
    return w.bits / 8;
}

struct read_back
{
    bool loaded;
    blobdemo::TableReport report;
};

static read_back load( const blobdemo::TableVocabulary & vocabulary, const uint8_t * batch, int64_t bytes )
{
    read_back out;
    out.loaded = false;
    const int64_t need = blobdemo::CatalogLoadMeasure( vocabulary, batch, bytes, NULL );
    if ( need < 0 ) { return out; }
    uint8_t * region = (uint8_t *) malloc( need > 0 ? (size_t) need : 1 );
    const blobdemo::Catalog * roots[1] = { NULL };
    int64_t count = 1;
    blobdemo::CatalogLoadMessages( roots, &count, region, need, vocabulary, batch, bytes, &out.report );
    out.loaded = roots[0] != NULL && count == 1;
    free( region );
    return out;
}

int main()
{
    std::vector<uint8_t> announcement( (size_t) blobdemo::AnnounceMeasure() );
    if ( blobdemo::Announce( announcement.data(), (int64_t) announcement.size() ) != (int64_t) announcement.size() )
    {
        printf( "message blob: the announcement did not write\n" );
        return 2;
    }
    std::vector<blobdemo::TableMessageEntry> entries( (size_t) blobdemo::kTableMessageEntriesHere );
    blobdemo::TableVocabulary vocabulary( entries.data(), blobdemo::kTableMessageEntriesHere );
    if ( !blobdemo::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), NULL ) )
    {
        printf( "message blob: this unit's own announcement was refused\n" );
        return 2;
    }

    // THE TWO RESERVED BLOB IDS ride in the announcement's tail whether or not
    // a root names them (§3.1), so the slots come off the vocabulary rather
    // than out of a number this program spells
    uint64_t string_slot = 0, bytes_slot = 0;
    for ( int64_t at = 1; at <= vocabulary.count; at++ )
    {
        const blobdemo::TableMessageEntry e = blobdemo::TableVocabularyEntryAt( vocabulary, at );
        if ( e.id == blobdemo::kTableStringTypeId ) { string_slot = (uint64_t) at; }
        if ( e.id == blobdemo::kTableBytesTypeId ) { bytes_slot = (uint64_t) at; }
    }
    if ( string_slot == 0 || bytes_slot == 0 )
    {
        printf( "message blob: this unit announces neither reserved blob id\n" );
        return 2;
    }

    uint8_t batch[128];

    // A WELL-FORMED BLOB IS ORDINARY, and the row is here first so a gate that
    // went red by refusing everything is not one that holds the rule
    {
        const uint8_t data[8] = { 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h' };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, string_slot, data, 8 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.loaded && !got.report.malformed && !got.report.refused && got.report.unknown == 0,
             "a well-formed *string blob on a message body loads with a silent report" );
    }

    // A TRUNCATED SEQUENCE is a payload that is not text at any length (§3)
    {
        const uint8_t data[5] = { 'p', 'a', 'c', 'k', 0xC3 };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, string_slot, data, 5 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.report.malformed && !got.report.refused,
             "a truncated UTF-8 sequence in a *string blob record is damage, and the batch ends there" );
    }

    // A ZERO BYTE AMONG THE BYTES is damage on the same rule (§3, §3.1)
    {
        const uint8_t data[5] = { 'p', 'a', 0x00, 'c', 'k' };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, string_slot, data, 5 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.report.malformed && !got.report.refused,
             "a zero byte among a *string blob's bytes is damage, and the batch ends there" );
    }

    // AN OVERLONG ENCODING is ill-formed even though its bytes are a legal
    // shape: 0xC0 0x80 spells U+0000 in two bytes
    {
        const uint8_t data[4] = { 'p', 0xC0, 0x80, 'k' };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, string_slot, data, 4 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.report.malformed && !got.report.refused,
             "an overlong encoding in a *string blob record is damage, and the batch ends there" );
    }

    // A LONE LEAD BYTE UTF-8 NEVER SPELLS, which is the byte the pinned vector
    // message_blob_ill_formed_text carries
    {
        const uint8_t data[8] = { 'a', 'b', 'c', 0xFF, 'e', 'f', 'g', 'h' };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, string_slot, data, 8 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.report.malformed && !got.report.refused,
             "a lead byte UTF-8 never spells is damage, and the batch ends there" );
    }

    // AND A *bytes BLOB HAS NO SUCH RULE, because it is bytes and never text
    // (§3.1): the same bytes under the reserved `bytes` id load clean, which
    // holds the content rule to the `string` id alone
    {
        const uint8_t data[5] = { 'p', 'a', 0x00, 'c', 0xFF };
        const int64_t bytes = forge( batch, sizeof( batch ), vocabulary, bytes_slot, data, 5 );
        const read_back got = load( vocabulary, batch, bytes );
        row( got.loaded && !got.report.malformed && !got.report.refused,
             "the same bytes under the reserved bytes id load with a silent report" );
    }

    if ( failures != 0 )
    {
        printf( "message blob: %d row(s) red\n", failures );
        return 1;
    }
    printf( "message blob: a *string blob record on a message body carries kind 12's content rule\n" );
    return 0;
}
