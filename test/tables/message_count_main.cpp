// A WIDE COUNT, AS A NEGATIVE CONTROL (docs/SPEC-TABLES.md §3.3).
//
// An array's count is read WIDE off the wire and bounded while it is still
// wide. A reader that narrows it to int32 first turns a count at or above 2^31
// negative, which passes a signed test against the reader's own bound
// untouched and lands a NEGATIVE count in the caller's storage. Nothing about
// the body is ill-formed, so no refusal fires and no counter moves: the only
// instrument is a vector that carries such a count and a program that reads
// the count back.
//
// The vector is `few` announced over [2, 2^32 - 1] with a ranged uint32
// element whose min equals its max, which rides no bits at all, under a count
// of 2^31 + 1. Six bytes of wire. Green under the emitter this repository
// ships, where the count clamps to the declared five, and red under one that
// narrows before it clamps, which is the whole of the control in
// `make tables-message-form-count-negative-control`.

#include <cstdio>
#include <cstring>
#include <cstdint>
#include <vector>

#include "BasesTable.h"

// the vocabulary FIELD's own bytes inside an announcement, which is a form 1
// file whose body is the build version at reference 1 under kind 9 and the
// vocabulary at reference 2 under kind 14 over element kind 6
static int64_t read_leb( const uint8_t * a, int64_t & at )
{
    uint64_t v = 0;
    int shift = 0;
    for ( ;; )
    {
        const uint8_t by = a[at++];
        v |= uint64_t( by & 0x7F ) << shift;
        if ( ( by & 0x80 ) == 0 ) { return (int64_t) v; }
        shift += 7;
    }
}

static void write_leb( std::vector<uint8_t> & out, uint64_t v )
{
    do
    {
        uint8_t by = (uint8_t) ( v & 0x7F );
        v >>= 7;
        out.push_back( (uint8_t) ( v != 0 ? by | 0x80 : by ) );
    } while ( v != 0 );
}

int main()
{
    std::vector<uint8_t> announcement( (size_t) basesdemo::AnnounceMeasure() );
    if ( basesdemo::Announce( announcement.data(), (int64_t) announcement.size() ) != (int64_t) announcement.size() )
    {
        printf( "wide count control: the announcement did not write\n" );
        return 2;
    }
    // step past the form byte, the build version field and the vocabulary
    // field's header to the vocabulary's own bytes
    int64_t at = 1 + 10 + 2;
    read_leb( announcement.data(), at ); // the field's L
    at += 1;                             // the element kind
    const int64_t words_bytes = read_leb( announcement.data(), at );
    const uint8_t * words = announcement.data() + at;

    // the same vocabulary with `few`'s shape replaced: min 2, max 2^32 - 1,
    // element kind 8 ranged at ZERO bits over a base of zero
    const uint8_t wide[10] = { 0x02, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F, 0x08, 0x01, 0x00, 0x00 };
    std::vector<uint8_t> forged_words;
    bool replaced = false;
    for ( int64_t cursor = 0; cursor < words_bytes; )
    {
        const int64_t begin = cursor;
        basesdemo::TableMessageEntry entry;
        if ( !basesdemo::TableMessageEntryRead( words, words_bytes, cursor, entry ) )
        {
            printf( "wide count control: this unit's own vocabulary does not parse\n" );
            return 2;
        }
        if ( entry.kind == 14 && entry.elem_kind == 8 && entry.min == 2 && entry.max == 5 )
        {
            forged_words.insert( forged_words.end(), words + begin, words + begin + 9 ); // the id and the kind stay
            forged_words.insert( forged_words.end(), wide, wide + 10 );
            replaced = true;
            continue;
        }
        forged_words.insert( forged_words.end(), words + begin, words + cursor );
    }
    if ( !replaced )
    {
        printf( "wide count control: this unit announces no [2..5] array of uint32\n" );
        return 2;
    }

    std::vector<uint8_t> inner;
    inner.push_back( 6 ); // the element kind
    write_leb( inner, (uint64_t) forged_words.size() );
    inner.insert( inner.end(), forged_words.begin(), forged_words.end() );
    std::vector<uint8_t> forged;
    forged.push_back( 1 ); // the form byte
    const uint8_t version_field[10] = { 1, 9, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88 };
    forged.insert( forged.end(), version_field, version_field + 10 );
    forged.push_back( 2 );
    forged.push_back( 14 );
    write_leb( forged, (uint64_t) inner.size() );
    forged.insert( forged.end(), inner.begin(), inner.end() );
    forged.push_back( 0 ); // the body's terminator
    const uint64_t ids[2] = { basesdemo::kTableBuildVersionFieldId, basesdemo::kTableMessageVocabularyFieldId };
    for ( int i = 0; i < 2; i++ ) { for ( int b = 0; b < 8; b++ ) { forged.push_back( (uint8_t) ( ids[i] >> ( 8 * b ) ) ); } }
    forged.push_back( 2 );
    for ( int b = 1; b < 8; b++ ) { forged.push_back( 0 ); }

    std::vector<basesdemo::TableMessageEntry> entries( (size_t) basesdemo::kTableMessageEntriesHere );
    basesdemo::TableVocabulary vocabulary( entries.data(), basesdemo::kTableMessageEntriesHere );
    if ( !basesdemo::AnnounceRead( vocabulary, forged.data(), (int64_t) forged.size(), NULL ) )
    {
        printf( "wide count control: the forged announcement was refused\n" );
        return 2;
    }
    int64_t few_slot = 0;
    for ( int64_t slot = 1; slot <= vocabulary.count; slot++ )
    {
        const basesdemo::TableMessageEntry e = basesdemo::TableVocabularyEntryAt( vocabulary, slot );
        if ( e.kind == 14 && e.elem_kind == 8 && e.min == 2 && e.max == (int64_t) 0xFFFFFFFFull ) { few_slot = slot; }
    }
    if ( few_slot == 0 )
    {
        printf( "wide count control: the forged vocabulary carries no wide array\n" );
        return 2;
    }

    uint8_t message[64];
    basesdemo::TableBitWriter w( message, sizeof( message ) );
    w.put( 2, 8 ); w.put( 0, 8 ); // the form byte and a count of one
    w.put( (uint64_t) few_slot, vocabulary.ref_bits );
    w.put( 0x7FFFFFFFull, 32 );   // the count rides as its offset from the minimum: 2^31 + 1 elements
    w.put( 0, vocabulary.ref_bits );
    w.align();

    basesdemo::Bases read;
    int64_t count = 1;
    basesdemo::TableReport report;
    if ( !basesdemo::BasesLoadMessages( &read, &count, vocabulary, message, w.bits / 8, &report ) )
    {
        printf( "wide count control: the vector did not load\n" );
        return 1;
    }
    if ( report.malformed || report.clamped != 1 )
    {
        printf( "wide count control: the vector read malformed=%d clamped=%d\n", (int) report.malformed, report.clamped );
        return 1;
    }
    if ( read.few_count != 5 )
    {
        printf( "wide count control: a count of 2^31 + 1 landed as %d, not the declared five\n", read.few_count );
        return 1;
    }
    printf( "wide count control: a count of 2^31 + 1 clamps to five and counts one clamped\n" );
    return 0;
}
