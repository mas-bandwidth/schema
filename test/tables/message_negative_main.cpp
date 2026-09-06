// THE MESSAGE FORM's SLOT, AS A NEGATIVE CONTROL (docs/SPEC-TABLES.md §3.3).
//
// A form-2 writer names an id through a SLOT of the unit's vocabulary, and
// that slot is a compile-time constant the emitter computes once. Nothing
// about a message body is self-describing enough to catch a slot that moved:
// the bytes are still a legal body, the reader still resolves every reference,
// and what it decodes is another field. So the instrument is the pinned wire,
// and this program is what points at it.
//
// It reads the corpus's own `login_full` message against the unit's own
// announcement, writes it back, and requires the bytes to be the golden's.
// Green under the emitter this repository ships, and red under one whose slots
// have moved, which is the whole of the control in `make
// tables-message-form-emitter-negative-control`.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>

#include "BackendTable.h"

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    uint8_t chunk[4096];
    size_t n = 0;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 ) { out.insert( out.end(), chunk, chunk + n ); }
    fclose( f );
    return true;
}

int main( int argc, char ** argv )
{
    const char * wire = argc > 1 ? argv[1] : "testdata/wire/tables/login_full_message.bin";
    std::vector<uint8_t> golden;
    if ( !slurp( wire, golden ) ) { printf( "message slot control: cannot read %s\n", wire ); return 2; }

    // the CONNECTION's table, announced from this unit's own compile-time
    // constant: the vocabulary is a pure function of the build version
    std::vector<uint8_t> announcement( (size_t) backenddemo::AnnounceMeasure() );
    if ( backenddemo::Announce( announcement.data(), (int64_t) announcement.size() ) != (int64_t) announcement.size() )
    {
        printf( "message slot control: the announcement did not write\n" );
        return 2;
    }
    std::vector<backenddemo::TableMessageEntry> entries( (size_t) backenddemo::kTableMessageEntriesHere );
    backenddemo::TableVocabulary vocabulary( entries.data(), backenddemo::kTableMessageEntriesHere );
    if ( !backenddemo::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), NULL ) )
    {
        printf( "message slot control: this unit's own announcement was refused\n" );
        return 2;
    }

    backenddemo::LoginRequest value;
    backenddemo::TableReport report;
    int64_t count = 1;
    if ( !backenddemo::LoginRequestLoadMessages( &value, &count, vocabulary, golden.data(), (int64_t) golden.size(), &report ) || count != 1 )
    {
        printf( "message slot control: the golden did not load\n" );
        return 1;
    }
    if ( report.unknown != 0 || report.kind_mismatch != 0 || report.malformed )
    {
        printf( "message slot control: the golden read %d unknown, %d kind_mismatch, malformed=%d\n",
                report.unknown, report.kind_mismatch, (int) report.malformed );
        return 1;
    }
    const int64_t size = backenddemo::LoginRequestMeasureMessages( &value, 1, &report );
    std::vector<uint8_t> again( size > 0 ? (size_t) size : 1 );
    if ( backenddemo::LoginRequestSaveMessages( &value, 1, again.data(), size, &report ) != size )
    {
        printf( "message slot control: the save refused\n" );
        return 1;
    }
    if ( size != (int64_t) golden.size() || memcmp( again.data(), golden.data(), golden.size() ) != 0 )
    {
        printf( "message slot control: the message re-saves %lld bytes against the golden's %lld, and they differ\n",
                (long long) size, (long long) golden.size() );
        return 1;
    }
    printf( "message slot control: the golden round trips\n" );
    return 0;
}
