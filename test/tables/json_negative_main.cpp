// The NEGATIVE CONTROL for the text form (SPEC-TABLES.md §16.5).
//
// A green round-trip suite is worth nothing until the suite has been shown
// capable of going red. This binary is built against a DELIBERATELY SABOTAGED
// walker — the field-offset arithmetic shifted by one field width, applied by
// the Makefile to a throwaway copy of the generated header — and it PASSES
// only when the round trip fails.
//
// Attachment is the smallest table with two fields (slot at offset 0, power
// at offset 4, both four bytes), so the sabotage swaps exactly two fields and
// reads nothing outside the struct.

#include <cstdio>
#include <cstring>

#include "TablesTable.h"

int main()
{
    tabledemo::Attachment value;
    value.slot = 5;
    value.power = 2.5f;

    int64_t size = tabledemo::AttachmentToJsonMeasure( value );
    if ( size <= 0 )
    {
        printf( "json negative control: the sabotaged writer refused outright — red, as required\n" );
        return 0;
    }
    char text[512];
    if ( size > (int64_t) sizeof( text ) )
    {
        printf( "FAIL json negative control: the text does not fit the fixture buffer\n" );
        return 1;
    }
    if ( tabledemo::AttachmentToJson( value, text, size ) != size )
    {
        printf( "json negative control: the sabotaged writer disagreed with its own measure — red\n" );
        return 0;
    }

    tabledemo::Attachment back;
    tabledemo::TableReport report;
    if ( !tabledemo::AttachmentFromJson( back, text, size, &report ) )
    {
        printf( "json negative control: the sabotaged text would not read back — red\n" );
        return 0;
    }

    uint8_t a[256], b[256];
    int64_t na = tabledemo::AttachmentSave( value, a, sizeof( a ) );
    int64_t nb = tabledemo::AttachmentSave( back, b, sizeof( b ) );
    if ( na != nb || na <= 0 || memcmp( a, b, (size_t) na ) != 0 )
    {
        printf( "json negative control: the round trip changed the wire — red, as required\n" );
        return 0;
    }

    printf( "FAIL json negative control: a walker with its offsets sabotaged still round-tripped\n"
            "      Attachment byte-identically. The round-trip suite cannot see an offset bug.\n" );
    return 1;
}
