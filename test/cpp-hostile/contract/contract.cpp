// THE VARIABLE SAVE'S WRITE CONTRACT, DEBUG BUILD (docs/SPEC.md §5.1 rows 1,
// 2 and 5), ROW 10 ON EVERY WRITER (a ranged float off its range, on the
// variable Save, the FixedSave and the fixed table's SaveMessages), and ROW 3
// ONE RECORD DOWN (a child's count below its own minimum, on the FixedSave,
// the SaveMessages and the FixedLoad, whose bounds pass refuses it in every
// build): C++ is an assert leg, so a value outside its declaration is the
// caller's bug, refused by schema_assert in a debug build and by nothing in a
// release one (a `required` field and an enum outside its set also refuse in
// every build, by the Save's -1). Each case runs in a child, as the
// conformance driver's write-refusal battery does: the child sets one value
// and saves; death by SIGABRT is the assert's refusal, exit 11 the Save's -1,
// exit 10 a write. Every refused case has a twin at its edge that writes.
//
// Built without NDEBUG by make/cpp-write-contract.mk. No time, no sockets.

#include "ContractTable.h"

#include <math.h>
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

// one write, whole: the bytes it wrote, or a refusal's zero or negative
typedef int64_t ( *Writer )( float ratio );

static const char * VerdictOf( Setter set, Writer write, float ratio );

// the verdict of one save, from a child so an assert's abort is observable
static const char * Verdict( Setter set )
{
    return VerdictOf( set, NULL, 0.0f );
}

// one call whole, in the child: above zero is a write (or a read), zero or
// below a refusal
typedef int64_t ( *Action )();

static const char * Collect( pid_t pid, const char * yes )
{
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
        return yes;
    }
    return "crashed";
}

static const char * VerdictOf( Setter set, Writer write, float ratio )
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
        if ( write != NULL )
        {
            _exit( write( ratio ) > 0 ? kWroteExit : kRefusedExit );
        }
        static Contract value;
        set( value );
        static uint8_t buffer[4096];
        const int64_t bytes = ContractSave( &value, buffer, (int64_t) sizeof( buffer ) );
        _exit( bytes > 0 ? kWroteExit : kRefusedExit );
    }
    return Collect( pid, "wrote" );
}

// the verdict of one action, from a child; `yes` names its success
static const char * ActionVerdict( Action act, const char * yes )
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
        _exit( act() > 0 ? kWroteExit : kRefusedExit );
    }
    return Collect( pid, yes );
}

static void ExpectAction( const char * name, Action act, const char * yes, const char * want )
{
    const char * got = ActionVerdict( act, yes );
    const bool ok = strcmp( got, want ) == 0;
    printf( "%s %s: %s (want %s)\n", ok ? "PASS" : "FAIL", name, got, want );
    if ( !ok )
    {
        failures++;
    }
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

static void ExpectWrite( const char * name, Writer write, float ratio, const char * want )
{
    const char * got = VerdictOf( NULL, write, ratio );
    const bool ok = strcmp( got, want ) == 0;
    printf( "%s %s: %s (want %s)\n", ok ? "PASS" : "FAIL", name, got, want );
    if ( !ok )
    {
        failures++;
    }
}

static uint8_t write_buffer[4096];

static int64_t SaveVariable( float ratio )
{
    static Contract value;
    value.ratio = ratio;
    return ContractSave( &value, write_buffer, (int64_t) sizeof( write_buffer ) );
}

static int64_t SaveFixed( float ratio )
{
    static FixedContract value;
    value.ratio = ratio;
    return FixedContractFixedSave( &value, 1, write_buffer, (int64_t) sizeof( write_buffer ) );
}

static int64_t SaveFixedMessage( float ratio )
{
    static FixedContract value;
    value.ratio = ratio;
    TableReport report;
    return FixedContractSaveMessages( &value, 1, write_buffer, (int64_t) sizeof( write_buffer ), &report );
}

// ROW 3 ONE RECORD DOWN (F-184): one child in the nest, holding `vals` used
// bytes, a 5 and then zeros, so a count of 2 and of 3 differ in the count
// alone, and a count of 1 leaves the slack zero as the form requires.
template <typename T>
static void FillNest( T & value, int vals )
{
    value = T();
    value.nest_count = 1;
    value.nest[0].vals_count = vals;
    for ( int i = 0; i < vals; ++i )
    {
        value.nest[0].vals[i] = i == 0 ? 5 : 0;
    }
}

// the images a FixedLoad reads: the child at its minimum, and the same bytes
// with the child's stored count one below it and nothing else wrong (its
// slack is zero). No writer of this build makes the second (its write
// asserts), so it is the lawful image with the one byte where a count of 2
// and a count of 3 differ set to 1.
static uint8_t edge_image[4096];
static uint8_t below_image[4096];
static int64_t image_size = 0;

static bool BelowMinImage( int64_t ( *save )( int vals, uint8_t * out, int64_t capacity ) )
{
    static uint8_t three[4096];
    image_size = save( 2, edge_image, (int64_t) sizeof( edge_image ) );
    if ( image_size <= 0 || save( 3, three, (int64_t) sizeof( three ) ) != image_size )
    {
        return false;
    }
    int64_t at = -1;
    for ( int64_t i = 0; i < image_size; ++i )
    {
        if ( edge_image[i] != three[i] )
        {
            if ( at >= 0 )
            {
                return false;
            }
            at = i;
        }
    }
    if ( at < 0 || edge_image[at] != 2 || three[at] != 3 )
    {
        return false;
    }
    memcpy( below_image, edge_image, (size_t) image_size );
    below_image[at] = 1;
    return true;
}

#define NEST_CASES( T )                                                                                       \
    template <int VALS>                                                                                       \
    static int64_t T##FixedWrite()                                                                            \
    {                                                                                                         \
        static T value;                                                                                       \
        FillNest( value, VALS );                                                                              \
        return T##FixedSave( &value, 1, write_buffer, (int64_t) sizeof( write_buffer ) );                     \
    }                                                                                                         \
    template <int VALS>                                                                                       \
    static int64_t T##MessageWrite()                                                                          \
    {                                                                                                         \
        static T value;                                                                                       \
        FillNest( value, VALS );                                                                              \
        TableReport report;                                                                                   \
        return T##SaveMessages( &value, 1, write_buffer, (int64_t) sizeof( write_buffer ), &report );         \
    }                                                                                                         \
    static int64_t T##Image( int vals, uint8_t * out, int64_t capacity )                                      \
    {                                                                                                         \
        static T value;                                                                                       \
        FillNest( value, vals );                                                                              \
        return T##FixedSave( &value, 1, out, capacity );                                                      \
    }                                                                                                         \
    template <bool BELOW>                                                                                     \
    static int64_t T##Load()                                                                                  \
    {                                                                                                         \
        static T value;                                                                                       \
        static TableFixedEntry plan[1024];                                                                    \
        TableReport report;                                                                                   \
        const int64_t n = T##FixedLoad( &value, 1, BELOW ? below_image : edge_image, image_size, plan, 1024, \
                                        NULL, &report );                                                      \
        return n == 1 && !report.refused ? 1 : 0;                                                             \
    }

NEST_CASES( LooseNest )
NEST_CASES( TightNest )

static void ExpectNest( const char * name, Action below_fixed, Action edge_fixed, Action below_message,
                        Action edge_message, int64_t ( *image )( int, uint8_t *, int64_t ), Action load_below,
                        Action load_edge )
{
    char label[128];
    snprintf( label, sizeof( label ), "row3-%s-fixed-below", name );
    ExpectAction( label, below_fixed, "wrote", "refused" );
    snprintf( label, sizeof( label ), "row3-%s-fixed-edge", name );
    ExpectAction( label, edge_fixed, "wrote", "wrote" );
    snprintf( label, sizeof( label ), "row3-%s-message-below", name );
    ExpectAction( label, below_message, "wrote", "refused" );
    snprintf( label, sizeof( label ), "row3-%s-message-edge", name );
    ExpectAction( label, edge_message, "wrote", "wrote" );
    if ( !BelowMinImage( image ) )
    {
        printf( "FAIL row3-%s-image: the count-2 and count-3 images do not differ in one count byte\n", name );
        failures++;
        return;
    }
    snprintf( label, sizeof( label ), "row3-%s-load-below", name );
    ExpectAction( label, load_below, "read", "refused" );
    snprintf( label, sizeof( label ), "row3-%s-load-edge", name );
    ExpectAction( label, load_edge, "read", "read" );
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

    // row 10: a ranged float off its range, on every writer. The float32 one
    // step past the top end is past it (the ends are compared at float32), and
    // the end itself, off the 0.01 grid as every float32 is, writes.
    const float past = nextafterf( 0.1f, 1.0f );
    ExpectWrite( "row10-variable-past", SaveVariable, past, "refused" );
    ExpectWrite( "row10-variable-below", SaveVariable, -0.5f, "refused" );
    ExpectWrite( "row10-variable-edge", SaveVariable, 0.1f, "wrote" );
    ExpectWrite( "row10-fixed-past", SaveFixed, past, "refused" );
    ExpectWrite( "row10-fixed-below", SaveFixed, -0.5f, "refused" );
    ExpectWrite( "row10-fixed-edge", SaveFixed, 0.1f, "wrote" );
    ExpectWrite( "row10-fixed-message-past", SaveFixedMessage, past, "refused" );
    ExpectWrite( "row10-fixed-message-edge", SaveFixedMessage, 0.1f, "wrote" );

    // row 3 one record down (F-184): a child's count below its own minimum,
    // inside a counted array with no minimum and one with its own, on the
    // FixedSave, the SaveMessages and the FixedLoad; each beside its child at
    // the minimum
    ExpectNest( "nested-loose", LooseNestFixedWrite<1>, LooseNestFixedWrite<2>, LooseNestMessageWrite<1>,
                LooseNestMessageWrite<2>, LooseNestImage, LooseNestLoad<true>, LooseNestLoad<false> );
    ExpectNest( "nested-tight", TightNestFixedWrite<1>, TightNestFixedWrite<2>, TightNestMessageWrite<1>,
                TightNestMessageWrite<2>, TightNestImage, TightNestLoad<true>, TightNestLoad<false> );

    if ( failures != 0 )
    {
        printf( "cpp-write-contract: %d failure(s)\n", failures );
        return 1;
    }
    printf( "cpp-write-contract: every row holds\n" );
    return 0;
}
