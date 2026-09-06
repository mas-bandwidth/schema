// The NEGATIVE CONTROL for the clamp's PREFIX rule (docs/SPEC-TABLES.md §16.2).
//
// A string longer than the field clamps at a code point boundary, and what
// stays is a PREFIX of the text. A scan that keeps placing after one code point
// fails to fit stores a string the input never spelled — a later, shorter code
// point slides into the room the long one left — while the `clamped` count
// comes back right, which is exactly why a suite that only reads counters could
// never see it.
//
// This binary is built against a walker whose scan resumes after a code point
// that did not fit, and it PASSES only when the stored bytes come back wrong.

#include <cstdio>
#include <cstring>

#include "TablesTable.h"

int main()
{
    tabledemo::RootConfig value; // version_note is string(16)
    tabledemo::TableReport report;
    // twelve ASCII bytes, a three-byte code point that fits at 15, one that
    // does not fit at 18, and a single byte that would fit at 16
    const char * text = "{ \"version_note\": \"123456789012\xe2\x9c\x93\xe2\x9c\x93X\" }";
    if ( !tabledemo::RootConfigFromJson( value, text, (int64_t) strlen( text ), &report ) )
    {
        printf( "clamp prefix negative control: the sabotaged walker would not read at all — red\n" );
        return 0;
    }

    // the CLAMP must still be counted: the sabotage moves the bytes and not the
    // ledger, and a control that passed for the wrong reason would prove nothing
    if ( report.clamped != 1 )
    {
        printf( "clamp prefix negative control: the clamp went uncounted as well — red, but for the wrong reason\n" );
        return 0;
    }

    if ( value.version_note_length != 15 || strcmp( value.version_note, "123456789012\xe2\x9c\x93" ) != 0 )
    {
        printf( "clamp prefix negative control: the scan resumed past the code point that did not fit,\n"
                "      storing %d bytes the text never spelled in that order — red, as required\n",
                value.version_note_length );
        return 0;
    }

    printf( "FAIL clamp prefix negative control: a walker whose scan resumes after an\n"
            "      over-long code point still stored the prefix. The suite cannot see a\n"
            "      clamp that is not a prefix.\n" );
    return 1;
}
