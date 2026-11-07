package ccommon

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// RuntimeHeaderName is the one header every generated C file includes: the
// macros and helpers that are the SAME TEXT in every unit this compiler
// emits, defined once per translation unit whatever the number of units in
// it. Everything a unit's shape decides (its types, its table runtime sized
// by its vocabulary) carries the unit's package instead (cqualify), so two
// units' headers meet in one translation unit with nothing redefined.
const RuntimeHeaderName = "SchemaRuntime.h"

// RuntimeVersionMacro names the shared header's version. Every generated
// header compares it against the version it was emitted with, and a
// mismatch is a hard #error: two units from compilers whose shared runtimes
// differ must never meet in one translation unit with one of them silently
// picked by an include guard.
const RuntimeVersionMacro = "SCHEMA_RUNTIME_VERSION"

// runtimeBody is the header's content below the version line. The version is
// a hash of exactly this text, so any edit here moves it.
const runtimeBody = `#include <stdint.h>
#include <string.h> /* memcpy — the interior-null scan */

/* The feature tests below are spelled the way the serialize runtime spells
   its own, tight inside the preprocessor's parentheses, so the formatter is
   kept off them. */
/* clang-format off */

/* SCHEMA_UNUSED guards the header-static functions: a translation unit that
   includes a header but calls only some of its functions would otherwise warn
   on every one it did not use, and generated headers are compiled with
   -Wall -Wextra -Werror in the CONSUMER's tree. */
#if defined(__GNUC__) || defined(__clang__)
#define SCHEMA_UNUSED __attribute__((unused))
#else
#define SCHEMA_UNUSED
#endif

/* SCHEMA_C_READ_INLINE / SCHEMA_C_WRITE_INLINE — how every generated wire
   function is spelled, beside ` + "`static SCHEMA_UNUSED`" + `: an inlining DEMAND
   (always_inline / __forceinline), the serialize family's remedy for LLVM
   refusing the header-static per-message entries into small-message call
   sites at marginal cost over the hot-callsite inline threshold. Measured
   and shipped; compilers with neither spelling get plain ` + "`static`" + `.
   Branch-weight hints are NOT the fix here — measured in this family to
   invite the machine outliner into the hot bodies. Do not add them. */
#if defined( _MSC_VER )
#define SCHEMA_C_READ_INLINE __forceinline
#define SCHEMA_C_WRITE_INLINE __forceinline
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_C_READ_INLINE inline __attribute__(( always_inline ))
#define SCHEMA_C_WRITE_INLINE inline __attribute__(( always_inline ))
#else
#define SCHEMA_C_READ_INLINE
#define SCHEMA_C_WRITE_INLINE
#endif

/* SCHEMA_C_ALIGN16 makes the C lane pair honor the table/cook layout's
   16-byte width and alignment. Left undefined on a compiler with no spelling
   for it; a header whose storage needs it refuses that compiler by name. */
#if defined(_MSC_VER)
#define SCHEMA_C_ALIGN16 __declspec(align(16))
#elif defined(__GNUC__) || defined(__clang__)
#define SCHEMA_C_ALIGN16 __attribute__((aligned(16)))
#elif defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L
#define SCHEMA_C_ALIGN16 _Alignas(16)
#endif

/* A COMPILE-TIME assertion under -std=c99, which has none of its own. C11's
   _Static_assert carries the message into the build log verbatim and is used
   wherever the compiler has it; everywhere else the negative array bound is
   the same refusal with a tag a reader can find. Every tag carries its
   unit's package, record and field, so no two of these ever meet. */
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L
#define SCHEMA_TABLE_STATIC_ASSERT( tag, cond, message ) _Static_assert( cond, message )
#elif defined( __cplusplus ) && __cplusplus >= 201103L
#define SCHEMA_TABLE_STATIC_ASSERT( tag, cond, message ) static_assert( cond, message )
#else
#define SCHEMA_TABLE_STATIC_ASSERT( tag, cond, message ) typedef char schema_table_assert_##tag[ ( cond ) ? 1 : -1 ]
#endif

/* A record's ALIGNMENT, which the cook's layout contract asserts and which the
   cook's Open compares the header's alignment word against (SPEC-TABLES §7.1,
   §20.3). C99 has no alignof either; C11's _Alignof, the two GNU spellings and
   MSVC's cover every compiler this repo builds under, and the last form is the
   portable definition of the same number. */
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 201112L
#define SCHEMA_TABLE_ALIGNOF( T ) _Alignof( T )
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_TABLE_ALIGNOF( T ) __alignof__( T )
#elif defined( _MSC_VER )
#define SCHEMA_TABLE_ALIGNOF( T ) __alignof( T )
#else
#define SCHEMA_TABLE_ALIGNOF( T ) ( (size_t) offsetof( struct { char schema_pad_; T schema_member_; }, schema_member_ ) )
#endif

/* THE FIXED PLAN'S SOURCE AND DESTINATION NEVER ALIAS: one is a record in a
   read buffer and the other is the caller's own storage. C99 has the keyword
   itself; the __cplusplus arm matters because a generated header is
   includable from C++ through its extern "C" block, and C++ has no restrict. */
#if defined( __STDC_VERSION__ ) && __STDC_VERSION__ >= 199901L && !defined( __cplusplus )
#define SCHEMA_TABLE_RESTRICT restrict
#elif defined( _MSC_VER )
#define SCHEMA_TABLE_RESTRICT __restrict
#elif defined( __GNUC__ ) || defined( __clang__ )
#define SCHEMA_TABLE_RESTRICT __restrict__
#else
#define SCHEMA_TABLE_RESTRICT
#endif

/* clang-format on */

#ifdef __cplusplus
extern "C" {
#endif

/* The bounded append pair every flag_names_* renderer shares. */
static SCHEMA_UNUSED int schema_flag_append_( char * buffer, int buffer_size, int position, const char * name )
{
    if ( position > 0 && position < buffer_size - 1 )
    {
        buffer[position++] = '|';
    }
    while ( *name && position < buffer_size - 1 )
    {
        buffer[position++] = *name++;
    }
    return position;
}

static SCHEMA_UNUSED int schema_flag_append_hex_( char * buffer, int buffer_size, int position, uint64_t bits )
{
    int shift = 60;
    position = schema_flag_append_( buffer, buffer_size, position, "0x" );
    while ( shift > 0 && ( ( bits >> shift ) & 0xf ) == 0 )
    {
        shift -= 4;
    }
    for ( ; shift >= 0; shift -= 4 )
    {
        if ( position < buffer_size - 1 )
        {
            buffer[position++] = "0123456789abcdef"[( bits >> shift ) & 0xf];
        }
    }
    return position;
}

/* string(N) refuses malformed UTF-8 on read in every build mode (SPEC §4.7).
   Rejects truncated sequences, bare continuations, overlongs,
   surrogates and code points past U+10FFFF. */
static SCHEMA_UNUSED int schema_utf8_valid_( const uint8_t * bytes, int32_t length )
{
    int32_t i = 0;
    while ( i < length )
    {
        uint8_t lead = bytes[i];
        int32_t continuations;
        uint32_t code_point;
        int32_t k;
        if ( lead < 0x80 )
        {
            i++;
            continue;
        }
        else if ( ( lead & 0xE0 ) == 0xC0 )
        {
            continuations = 1;
            code_point = lead & 0x1F;
        }
        else if ( ( lead & 0xF0 ) == 0xE0 )
        {
            continuations = 2;
            code_point = lead & 0x0F;
        }
        else if ( ( lead & 0xF8 ) == 0xF0 )
        {
            continuations = 3;
            code_point = lead & 0x07;
        }
        else
        {
            return 0;
        }
        if ( continuations >= length - i )
        {
            return 0;
        }
        for ( k = 1; k <= continuations; k++ )
        {
            if ( ( bytes[i + k] & 0xC0 ) != 0x80 )
            {
                return 0;
            }
            code_point = ( code_point << 6 ) | (uint32_t) ( bytes[i + k] & 0x3F );
        }
        if ( continuations == 1 && code_point < 0x80 )
        {
            return 0;
        }
        if ( continuations == 2 && ( code_point < 0x800 || ( code_point >= 0xD800 && code_point <= 0xDFFF ) ) )
        {
            return 0;
        }
        if ( continuations == 3 && ( code_point < 0x10000 || code_point > 0x10FFFF ) )
        {
            return 0;
        }
        i += 1 + continuations;
    }
    return 1;
}

/* string(N) carries bytes excluding 0x00: an interior null is content every
   generated reader refuses (SPEC §4.7) — generated-code validation; no
   serialize primitive performs it. The scan is word-wise: eight bytes per
   step under the zero-byte idiom, and a payload of eight bytes or more
   re-tests its final eight bytes as one whole (overlapping) word, so every
   load stays inside the payload the stream already delivered. */
static SCHEMA_UNUSED SCHEMA_C_READ_INLINE int schema_interior_null_( const uint8_t * bytes, int32_t length )
{
    uint64_t word;
    int32_t i = 0;
    if ( length >= 8 )
    {
        for ( ; i <= length - 8; i += 8 )
        {
            memcpy( &word, bytes + i, 8 );
            if ( ( ( word - 0x0101010101010101ULL ) & ~word & 0x8080808080808080ULL ) != 0 )
            {
                return 1;
            }
        }
        if ( i < length )
        {
            memcpy( &word, bytes + length - 8, 8 );
            if ( ( ( word - 0x0101010101010101ULL ) & ~word & 0x8080808080808080ULL ) != 0 )
            {
                return 1;
            }
        }
        return 0;
    }
    for ( ; i < length; i++ )
    {
        if ( bytes[i] == 0 )
        {
            return 1;
        }
    }
    return 0;
}

#ifdef __cplusplus
}
#endif
`

// RuntimeVersion is the shared header's version: FNV-1a 64 of its body.
func RuntimeVersion() uint64 {
	h := fnv.New64a()
	h.Write([]byte(runtimeBody))
	return h.Sum64()
}

// RuntimeHeader is SchemaRuntime.h, byte-identical from every unit this
// compiler emits.
func RuntimeHeader() []byte {
	var b strings.Builder
	b.WriteString("/* Code generated by the schema compiler. DO NOT EDIT.\n")
	b.WriteString("   SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	b.WriteString("   your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	b.WriteString("   AGPL-3.0, its output is not.\n")
	b.WriteString("   The C runtime every generated header shares: the same text from every unit,\n")
	b.WriteString("   defined once per translation unit, and versioned so that units from two\n")
	b.WriteString("   compilers whose runtimes differ refuse to meet (see RuntimeInclude). */\n\n")
	b.WriteString("#ifndef SCHEMA_RUNTIME_H\n#define SCHEMA_RUNTIME_H\n\n")
	fmt.Fprintf(&b, "#define %s 0x%016xULL\n\n", RuntimeVersionMacro, RuntimeVersion())
	b.WriteString(runtimeBody)
	b.WriteString("\n#endif /* SCHEMA_RUNTIME_H */\n")
	return []byte(b.String())
}

// RuntimeInclude is the include and the version check every generated header
// opens with. The include guard means the FIRST SchemaRuntime.h a translation
// unit meets defines the runtime; every header after it checks that what it
// got is what it was emitted against, and refuses the build by name if not.
func RuntimeInclude() string {
	return fmt.Sprintf("#include \"%s\"\n#if %s != 0x%016xULL\n#error \"%s: this header was generated against a different SchemaRuntime.h than the one already included in this translation unit; regenerate every schema unit in it with one compiler\"\n#endif\n",
		RuntimeHeaderName, RuntimeVersionMacro, RuntimeVersion(), RuntimeHeaderName)
}

// Align16Check refuses, by name, a compiler on which SchemaRuntime.h found no
// 16-byte alignment spelling, in a header whose storage needs one.
const Align16Check = `
#ifndef SCHEMA_C_ALIGN16
#error "table 128-bit storage requires a compiler with 16-byte alignment support"
#endif
`
