// STORAGE THAT IS NOT TEXT REFUSES THE WRITE (docs/SPEC-TABLES.md §16.14 W0).
//
// No wire and no text can put ill-formed UTF-8 into a narrow string field, so
// storage that holds it was put there by a program. A JSON text must be UTF-8
// (RFC 8259 §8.1), and a U+FFFD in its place would make two different strings
// one text, so ToJson and ToJsonMeasure both answer -1. The well-formed value
// beside it is the control: the same field, written.
//
// `make tables-json-write-refusal` runs this against the generated walk;
// `make tables-json-write-refusal-negative-control` runs it against a copy
// whose refusal is taken out, and requires it to go red by name.

#include <cstdio>
#include <cstring>

#include "TablesTable.h"

int main()
{
    int failures = 0;

    tabledemo::ProfileConfig good;
    memcpy( (void *) good.name, "ab", 2 );
    good.name_length = 2;
    if ( tabledemo::ProfileConfigToJsonMeasure( good ) <= 0 )
    {
        printf( "FAIL json write refusal: the well-formed control did not write\n" );
        failures++;
    }

    // a lead byte with no continuation, at the end of the used length
    tabledemo::ProfileConfig cut;
    memcpy( (void *) cut.name, "ab\xc3", 3 );
    cut.name_length = 3;
    // a stray continuation byte in the middle
    tabledemo::ProfileConfig stray;
    memcpy( (void *) stray.name, "a\x80z", 3 );
    stray.name_length = 3;

    const tabledemo::ProfileConfig * bad[] = { &cut, &stray };
    for ( const tabledemo::ProfileConfig * value : bad )
    {
        char text[512];
        const int64_t measured = tabledemo::ProfileConfigToJsonMeasure( *value );
        const int64_t written = tabledemo::ProfileConfigToJson( *value, text, (int64_t) sizeof( text ) );
        if ( measured != -1 || written != -1 )
        {
            printf( "FAIL json write refusal: ill-formed UTF-8 in a narrow field was written (measure %lld, write %lld), not refused\n",
                    (long long) measured, (long long) written );
            failures++;
        }
    }

    if ( failures != 0 ) { return 1; }
    printf( "json write refusal: ill-formed narrow text refuses the write, the control writes\n" );
    return 0;
}
