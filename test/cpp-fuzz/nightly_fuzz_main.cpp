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
// a refusal is a clean typed error. Nothing here asserts that malformed input
// is accepted or refused a particular way -- the sanitizers are the gate.

#include <stdint.h>
#include <stddef.h>
#include <vector>

#ifndef FUZZ_ROOTS_INC
#error "FUZZ_ROOTS_INC must name the generated roots.inc"
#endif

#define FUZZ_FIXED_COMMON(R)                                                   \
    do {                                                                       \
        TableReport report = TableReport();                                    \
        R value;                                                               \
        R##Reset(value);                                                       \
        if ( R##Load(value, data, (int64_t) size, &report) ) {                 \
            (void) R##Measure(value);                                          \
        }                                                                      \
        R##Reset(value);                                                       \
        (void) R##FromJson(value, (const char *) data, (int64_t) size, &report); \
    } while ( 0 )

#define FUZZ_FIXED_OPEN(R)                                                     \
    do {                                                                       \
        FUZZ_FIXED_COMMON(R);                                                  \
        (void) R##Open(data, size);                                            \
    } while ( 0 )

#define FUZZ_FIXED(R) FUZZ_FIXED_COMMON(R)

#define FUZZ_VAR_COMMON(R)                                                     \
    do {                                                                       \
        TableReport report = TableReport();                                    \
        const int64_t need = R##LoadMeasure(data, (int64_t) size);             \
        if ( need > 0 && need <= (int64_t) ( 64u * 1024u * 1024u ) ) {         \
            std::vector<uint8_t> region((size_t) need);                        \
            const R * root = R##Load(region.data(), need, data,                \
                                     (int64_t) size, &report);                 \
            if ( root != NULL ) { (void) R##Measure(root); }                   \
        }                                                                      \
        R##Builder builder;                                                    \
        if ( R##FromJson(builder, (const char *) data, (int64_t) size, &report) ) { \
            if ( builder.Lock() ) { (void) R##Measure(builder); }              \
        }                                                                      \
    } while ( 0 )

#define FUZZ_VAR_OPEN(R)                                                       \
    do {                                                                       \
        FUZZ_VAR_COMMON(R);                                                    \
        (void) R##Open(data, size);                                            \
    } while ( 0 )

#define FUZZ_VAR(R) FUZZ_VAR_COMMON(R)

#include FUZZ_ROOTS_INC

extern "C" int LLVMFuzzerTestOneInput( const uint8_t * data, size_t size )
{
    if ( size == 0 ) { return 0; }
    fuzz_all( data, size );
    return 0;
}
