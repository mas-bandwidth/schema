// libFuzzer entry point for the C++ tables port's generated readers.
//
// One translation unit is compiled per schema in test/cpp-fuzz/schemas.txt.
// The Makefile generates a roots.inc beside the generated headers naming every
// root and whether it is the variable class (LoadMeasure + Load into a region,
// FromJson into a Builder) or the fixed class (Load and FromJson into a value).
// Every root is driven through all three reader kinds the card names: load,
// the strict cooked Open, and the text/JSON parser.
//
// The oracle is the card's: no crash, no hang, no sanitizer/assert finding, and
// a refusal is a clean typed error. A reader that fails MUST say why: Load
// sets report.malformed or report.refused, FromJson sets report.malformed,
// and Open names the TableRefuseReason it refused. A failure with a silent
// report aborts the fuzzer, as does a success that carries a refusal verdict.
// Nothing here asserts that malformed input is accepted or refused a
// particular way -- the sanitizers plus the typed-result checks are the gate.
//
// The counters prove reader coverage: after fuzz_all, every declared root must
// have been driven through Load and FromJson, and every root carrying Open
// must have been driven through Open. gen_roots.sh emits the counts.

#include <stdint.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <vector>

#ifndef FUZZ_ROOTS_INC
#error "FUZZ_ROOTS_INC must name the generated roots.inc"
#endif

#define FUZZ_FAIL( WHAT )                                                      \
    do {                                                                       \
        fprintf( stderr, "FUZZ ORACLE: %s\n", ( WHAT ) );                      \
        fflush( stderr );                                                      \
        abort();                                                               \
    } while ( 0 )

static size_t fuzz_load_calls = 0;
static size_t fuzz_json_calls = 0;
static size_t fuzz_open_calls = 0;

// A strict read returns false with ONE typed reason (docs/SPEC-TABLES.md
// §6.5, §11): refuse_reason is set even when the malformed/refused flags are
// not, so the reason itself is the typed refusal. Checking only the two flags
// turned every clamped or kind-mismatch refusal into a false oracle failure.
#define FUZZ_CHECK_REPORT( REPORT, KIND )                                      \
    do {                                                                       \
        if ( !( REPORT ).malformed && !( REPORT ).refused &&                   \
             ( REPORT ).refuse_reason == ok ) {                                \
            FUZZ_FAIL( KIND " failed without a typed refusal or malformed report" ); \
        }                                                                      \
    } while ( 0 )

#define FUZZ_CHECK_SUCCESS( REPORT, KIND )                                     \
    do {                                                                       \
        if ( ( REPORT ).malformed || ( REPORT ).refused ||                     \
             ( REPORT ).refuse_reason != ok ) {                                \
            FUZZ_FAIL( KIND " succeeded and carried a typed failure" );        \
        }                                                                      \
    } while ( 0 )

// A wire Load KEEPS a partial result when framing damage stops it (the read
// report's malformed flag), so a non-NULL root carrying malformed is the
// contract. A non-NULL root carrying a REFUSAL is not: refusal means the file
// was not read at all.
#define FUZZ_CHECK_LOAD_SUCCESS( REPORT )                                      \
    do {                                                                       \
        if ( ( REPORT ).refused || ( REPORT ).refuse_reason != ok ) {          \
            FUZZ_FAIL( "Load returned a result and claimed a refusal" );       \
        }                                                                      \
    } while ( 0 )

#define FUZZ_FIXED_COMMON(R)                                                   \
    do {                                                                       \
        TableReport report = TableReport();                                    \
        R value;                                                               \
        R##Reset(value);                                                       \
        const bool loaded = R##Load(value, data, (int64_t) size, &report);     \
        fuzz_load_calls++;                                                     \
        if ( loaded ) {                                                        \
            FUZZ_CHECK_LOAD_SUCCESS(report);                                \
            (void) R##Measure(value);                                          \
        } else {                                                               \
            FUZZ_CHECK_REPORT(report, "Load");                                 \
        }                                                                      \
        R##Reset(value);                                                       \
        TableReport json_report = TableReport();                               \
        const bool parsed = R##FromJson(value, (const char *) data,           \
                                         (int64_t) size, &json_report);        \
        fuzz_json_calls++;                                                     \
        if ( parsed ) {                                                        \
            FUZZ_CHECK_SUCCESS(json_report, "FromJson");                       \
        } else {                                                               \
            FUZZ_CHECK_REPORT(json_report, "FromJson");                        \
        }                                                                      \
    } while ( 0 )

#define FUZZ_FIXED_OPEN(R)                                                     \
    do {                                                                       \
        FUZZ_FIXED_COMMON(R);                                                  \
        const TableRefuseReason untouched = ok;            \
        TableRefuseReason why = untouched;                                     \
        const R * root = R##Open(data, size, &why);                            \
        fuzz_open_calls++;                                                     \
        if ( root == NULL ) {                                                  \
            if ( why == untouched ) {                                          \
                FUZZ_FAIL("Open refused without naming a reason");             \
            }                                                                  \
        } else if ( why != untouched ) {                                       \
            FUZZ_FAIL("Open succeeded and wrote a refusal reason");            \
        }                                                                      \
    } while ( 0 )

#define FUZZ_FIXED(R) FUZZ_FIXED_COMMON(R)

#define FUZZ_VAR_COMMON(R)                                                     \
    do {                                                                       \
        const TableRefuseReason untouched = ok;            \
        TableRefuseReason measure_reason = untouched;                          \
        const int64_t need = R##LoadMeasure(data, (int64_t) size, NULL,        \
                                            &measure_reason);                  \
        fuzz_load_calls++;                                                     \
        if ( need > 0 ) {                                                      \
            if ( measure_reason != untouched ) {                               \
                FUZZ_FAIL("LoadMeasure succeeded and named a refusal");        \
            }                                                                  \
            if ( need <= (int64_t) ( 64u * 1024u * 1024u ) ) {                 \
                std::vector<uint8_t> region((size_t) need);                    \
                TableReport report = TableReport();                            \
                const R * root = R##Load(region.data(), need, data,            \
                                         (int64_t) size, &report);             \
                if ( root != NULL ) {                                          \
                    FUZZ_CHECK_LOAD_SUCCESS(report);                        \
                    (void) R##Measure(root);                                   \
                } else {                                                       \
                    FUZZ_CHECK_REPORT(report, "Load");                         \
                }                                                              \
            }                                                                  \
        }                                                                      \
        R##Builder builder;                                                    \
        TableReport json_report = TableReport();                               \
        const bool parsed = R##FromJson(builder, (const char *) data,          \
                                        (int64_t) size, &json_report);         \
        fuzz_json_calls++;                                                     \
        if ( parsed ) {                                                        \
            FUZZ_CHECK_SUCCESS(json_report, "FromJson");                       \
            if ( builder.Lock() ) { (void) R##Measure(builder); }              \
        } else {                                                               \
            FUZZ_CHECK_REPORT(json_report, "FromJson");                        \
        }                                                                      \
    } while ( 0 )

#define FUZZ_VAR_OPEN(R)                                                       \
    do {                                                                       \
        FUZZ_VAR_COMMON(R);                                                    \
        const TableRefuseReason untouched = ok;            \
        TableRefuseReason why = untouched;                                     \
        const R * root = R##Open(data, size, &why);                            \
        fuzz_open_calls++;                                                     \
        if ( root == NULL ) {                                                  \
            if ( why == untouched ) {                                          \
                FUZZ_FAIL("Open refused without naming a reason");             \
            }                                                                  \
        } else if ( why != untouched ) {                                       \
            FUZZ_FAIL("Open succeeded and wrote a refusal reason");            \
        }                                                                      \
    } while ( 0 )

#define FUZZ_VAR(R) FUZZ_VAR_COMMON(R)

#include FUZZ_ROOTS_INC

extern "C" int LLVMFuzzerTestOneInput( const uint8_t * data, size_t size )
{
    if ( size == 0 ) { return 0; }
    fuzz_all( data, size );
    if ( fuzz_load_calls < FUZZ_ROOT_COUNT || fuzz_json_calls < FUZZ_ROOT_COUNT ) {
        FUZZ_FAIL("a reader kind did not reach every root");
    }
    if ( fuzz_open_calls < FUZZ_OPEN_ROOTS ) {
        FUZZ_FAIL("the strict cooked reader did not reach every root that has an Open");
    }
    return 0;
}
