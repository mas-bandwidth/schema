/* The storage-bound refusal in a release build (docs/SPEC.md §5.1): a
   wstring(8) whose used length is 9 indexes past the storage it describes, and
   a build that compiled every assert out still refuses it before the first
   byte. This translation unit is compiled -DNDEBUG, so a refusal here cannot
   be the debug assert. */

#include <stdio.h>
#include <string.h>

#include "StorageTable.h"

int main( void )
{
    storagerelease_root_t value;
    uint8_t buffer[4096];
    storagerelease_table_report_t report;

    memset( &value, 0, sizeof value );
    value.wide_length = 9; /* the declared bound is 8 */
    value.wide[8] = 0x41;

    memset( &report, 0, sizeof report );
    if ( storagerelease_root_measure_messages( &value, 1, &report ) != -1 )
    {
        printf( "FAIL storage release: MeasureMessages wrote a wstring past its storage\n" );
        return 1;
    }

    memset( &report, 0, sizeof report );
    if ( storagerelease_root_save_messages( &value, 1, buffer, (int64_t) sizeof buffer, &report ) != -1 )
    {
        printf( "FAIL storage release: SaveMessages wrote a wstring past its storage\n" );
        return 1;
    }

    printf( "storage release gate: a wstring past its storage is refused with NDEBUG\n" );
    return 0;
}
