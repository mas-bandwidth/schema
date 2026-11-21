// THE VARIABLE SAVE'S WRITE CONTRACT, DEBUG BUILD (docs/SPEC.md §5.1 rows 1,
// 2 and 5): C++ is an assert leg, so a value outside its declaration is the
// caller's bug, refused by schema_assert in a debug build and by nothing in a
// release one (a `required` field and an enum outside its set also refuse in
// every build, by the Save's -1). Each case runs in a child, as the
// conformance driver's write-refusal battery does: the child sets one value
// and saves; death by SIGABRT is the assert's refusal, exit 11 the Save's -1,
// exit 10 a write. Every refused case has a twin at its edge that writes.
//
// Built without NDEBUG by make/cpp-write-contract.mk. No time, no sockets.

#include "ContractTable.h"

#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

using namespace contractdemo;

static int failures = 0;

enum
{
    kWroteExit = 10,
    kRefusedExit = 11,
};

typedef void ( *Setter )( Contract & value );

// the verdict of one save, from a child so an assert's abort is observable
static const char * Verdict( Setter set )
{
    fflush( stdout );
    const pid_t pid = fork();
    if ( pid < 0 )
    {
        return "fork failed";
    }
    if ( pid == 0 )
    {
        if ( freopen( "/dev/null", "w", stderr ) == NULL )
        {
            _exit( 2 );
        }
        static Contract value;
        set( value );
        static uint8_t buffer[4096];
        const int64_t bytes = ContractSave( &value, buffer, (int64_t) sizeof( buffer ) );
        _exit( bytes > 0 ? kWroteExit : kRefusedExit );
    }
    int status = 0;
    if ( waitpid( pid, &status, 0 ) != pid )
    {
        return "wait failed";
    }
    if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT )
    {
        return "refused";
    }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRefusedExit )
    {
        return "refused";
    }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit )
    {
        return "wrote";
    }
    return "crashed";
}

static void Expect( const char * name, Setter set, const char * want )
{
    const char * got = Verdict( set );
    const bool ok = strcmp( got, want ) == 0;
    printf( "%s %s: %s (want %s)\n", ok ? "PASS" : "FAIL", name, got, want );
    if ( !ok )
    {
        failures++;
    }
}

int main()
{
    // row 1: a ranged integer outside [min, max], as a field, an element and an arm payload
    Expect( "row1-field-high", []( Contract & v ) { v.level = 101; }, "refused" );
    Expect( "row1-field-low", []( Contract & v ) { v.level = -1; }, "refused" );
    Expect( "row1-field-edge", []( Contract & v ) { v.level = 100; }, "wrote" );
    Expect( "row1-element-high", []( Contract & v ) { v.levels_count = 2; v.levels[0] = 5; v.levels[1] = 101; }, "refused" );
    Expect( "row1-element-edge", []( Contract & v ) { v.levels_count = 2; v.levels[0] = 5; v.levels[1] = 100; }, "wrote" );
    Expect( "row1-arm-high", []( Contract & v ) { v.pick.type = PickType::Small; v.pick.small = 10; }, "refused" );
    Expect( "row1-arm-edge", []( Contract & v ) { v.pick.type = PickType::Small; v.pick.small = 9; }, "wrote" );

    // row 2: a bits(N) value past its width, as a field and an arm payload
    Expect( "row2-field-wide", []( Contract & v ) { v.mask = 8192; }, "refused" );
    Expect( "row2-field-edge", []( Contract & v ) { v.mask = 8191; }, "wrote" );
    Expect( "row2-arm-wide", []( Contract & v ) { v.pick.type = PickType::Wide; v.pick.wide = 8192; }, "refused" );
    Expect( "row2-arm-edge", []( Contract & v ) { v.pick.type = PickType::Wide; v.pick.wide = 8191; }, "wrote" );

    // row 5: an enum outside its set, as a field and an element
    Expect( "row5-field-outside", []( Contract & v ) { v.grade = (Grade) 4; }, "refused" );
    Expect( "row5-field-edge", []( Contract & v ) { v.grade = Grade::C; }, "wrote" );
    Expect( "row5-element-outside", []( Contract & v ) { v.grades_count = 1; v.grades[0] = (Grade) 4; }, "refused" );
    Expect( "row5-element-edge", []( Contract & v ) { v.grades_count = 1; v.grades[0] = Grade::C; }, "wrote" );

    if ( failures != 0 )
    {
        printf( "cpp-write-contract: %d failure(s)\n", failures );
        return 1;
    }
    printf( "cpp-write-contract: every row holds\n" );
    return 0;
}
