// The NEGATIVE CONTROL for an over-long string in JSON (docs/SPEC-TABLES.md
// §16.2, §4).
//
// A string longer than its field used to clamp to a code-point prefix and
// count `clamped`. The strict read REFUSES the whole value instead
// (length_over_bound), so the prefix is never handed to a caller and the one thing
// left to guard is the verdict. A walker that forgot the refusal would hand
// back a silently shortened string, and only a test that reads the verdict on
// an over-long input can see that.
//
// THE CONTROL IS RUN BOTH WAYS, because a binary that only ever meets the
// sabotage cannot show it has a blade. It has three exits:
//
//   0  the read ACCEPTED the over-long string and stored the code-point
//      prefix                         -> the sabotaged arm requires this
//   2  the read REFUSED with length_over_bound and the field at its default
//                                     -> the honest arm requires this
//   1  anything else                  -> neither arm may pass on it
//
// A refusal for another reason, an accepted read that stored something other
// than the prefix, or a refusal that kept bytes are all exit 1: none of them is
// the one defect this control names.

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
    if ( tabledemo::RootConfigFromJson( value, text, (int64_t) strlen( text ), &report ) )
    {
        // the sabotage removes the refusal and leaves the scan alone, so what
        // it stores is still the prefix; other bytes are a different defect
        if ( value.version_note_length != 15 || strcmp( value.version_note, "123456789012\xe2\x9c\x93" ) != 0 )
        {
            printf( "FAIL over-long string negative control: the read accepted the text but stored\n"
                    "      %d bytes that are not the code-point prefix, a different defect\n",
                    value.version_note_length );
            return 1;
        }
        printf( "over-long string negative control: the read accepted a string past its bound\n"
                "      and handed back a shortened value: red, as required\n" );
        return 0;
    }

    if ( report.refuse_reason != tabledemo::length_over_bound )
    {
        printf( "FAIL over-long string negative control: the read refused for reason %d, not\n"
                "      length_over_bound, so the bound was never what stopped it\n",
                (int) report.refuse_reason );
        return 1;
    }

    if ( value.version_note_length != 0 || value.version_note[0] != 0 )
    {
        printf( "FAIL over-long string negative control: the read refused but kept %d bytes\n",
                value.version_note_length );
        return 1;
    }

    printf( "over-long string negative control: the read refused with length_over_bound and the\n"
            "      field at its default: the honest verdict\n" );
    return 2;
}
