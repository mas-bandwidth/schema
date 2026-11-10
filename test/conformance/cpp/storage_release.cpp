// The storage-bound refusal in a release build (docs/SPEC.md §5.1): a
// wstring(8) whose used length is 9 indexes past the storage it describes, and
// a build that compiled every assert out still refuses it before the first
// byte. This translation unit is compiled -DNDEBUG, so a refusal here cannot
// be the debug assert.

#include <cstdint>
#include <cstdio>

#include "StorageTable.h"

int main()
{
    storagerelease::Root value{};
    uint8_t buffer[4096];
    storagerelease::TableReport report{};

    value.wide_length = 9; // the declared bound is 8
    value.wide[8] = u'A';

    if ( storagerelease::RootMeasureMessages( &value, 1, &report ) != -1 )
    {
        std::printf( "FAIL storage release: RootMeasureMessages wrote a wstring past its storage\n" );
        return 1;
    }

    report = {};
    if ( storagerelease::RootSaveMessages( &value, 1, buffer, (int64_t) sizeof buffer, &report ) != -1 )
    {
        std::printf( "FAIL storage release: RootSaveMessages wrote a wstring past its storage\n" );
        return 1;
    }

    std::printf( "storage release gate: a wstring past its storage is refused with NDEBUG\n" );
    return 0;
}
