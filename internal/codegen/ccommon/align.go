// Package ccommon holds portability spellings shared by C's two emitters.
package ccommon

// Align16 makes the C lane pair honor the table/cook layout's 16-byte width
// and alignment, including when a packet type supplies table storage.
const Align16 = `
#ifndef SCHEMA_C_ALIGN16
#if defined(_MSC_VER)
#define SCHEMA_C_ALIGN16 __declspec(align(16))
#elif defined(__GNUC__) || defined(__clang__)
#define SCHEMA_C_ALIGN16 __attribute__((aligned(16)))
#elif defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L
#define SCHEMA_C_ALIGN16 _Alignas(16)
#else
#error "table 128-bit storage requires a compiler with 16-byte alignment support"
#endif
#endif
`
