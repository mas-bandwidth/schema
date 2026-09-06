// The NEGATIVE CONTROL for the clamp's PREFIX rule (docs/SPEC-TABLES.md §16.2).
//
// A string longer than the field clamps at a code point boundary, and what
// stays is a PREFIX of the text. A scan that keeps placing after one code point
// fails to fit stores a string the input never spelled, because a later and
// shorter code point slides into the room the long one left, while the
// `clamped` count comes back right. That is exactly why a suite which only
// reads counters could never see it.
//
// THE CONTROL NAMES ONE DEFECT AND ESTABLISHES THAT ONE. It runs twice: against
// a walker whose scan resumes after a code point that did not fit, where it must
// SEE the wrong bytes, and against the honest walker, where it must find nothing
// to see. So there are exactly three outcomes it may report, and only one of
// them is success:
//
//   the read SUCCEEDED, the clamp counted 1, and the stored bytes are wrong
//                                        -> red as required, the defect stands
//   the read succeeded, the clamp counted 1, the stored bytes are the prefix
//                                        -> the honest walker, or a sabotage
//                                           the suite cannot see
//   anything else                        -> the defect was never reached
//
// A READ THAT FAILED AND A CLAMP COUNT THAT MOVED ARE THE THIRD OUTCOME, not
// the first. Both were success here, so a sabotage that broke the read outright,
// or moved the ledger as well as the bytes, printed the word red and returned
// zero over a run that observed no prefix at all. A control has to fail on the
// read's VERDICT and on the CLAMPED COUNT as surely as on the bytes, or the
// branch it passes through is a branch that proves nothing.

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
        printf( "FAIL clamp prefix negative control: the walker would not read this text at\n"
                "      all, so the clamp was never reached and the stored bytes say nothing.\n"
                "      A failed read does not establish the defect this control names.\n" );
        return 1;
    }

    // the CLAMP must still be counted: the defect moves the bytes and not the
    // ledger, and a run whose ledger also moved has not met the defect either
    if ( report.clamped != 1 )
    {
        printf( "FAIL clamp prefix negative control: the read counted %d clamps where the\n"
                "      text spells exactly one, so this run is not the one-clamp read the\n"
                "      control reads bytes out of. The defect it names is a clamp that is\n"
                "      not a prefix, and no clamp was observed.\n",
                (int) report.clamped );
        return 1;
    }

    if ( value.version_note_length != 15 || strcmp( value.version_note, "123456789012\xe2\x9c\x93" ) != 0 )
    {
        printf( "clamp prefix negative control: the scan resumed past the code point that did not fit,\n"
                "      storing %d bytes the text never spelled in that order: red, as required\n",
                value.version_note_length );
        return 0;
    }

    printf( "FAIL clamp prefix negative control: a walker whose scan resumes after an\n"
            "      over-long code point still stored the prefix. The suite cannot see a\n"
            "      clamp that is not a prefix.\n" );
    return 1;
}
