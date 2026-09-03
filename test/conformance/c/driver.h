/* THE C CONFORMANCE DRIVER's shared surface (test/conformance/README.md).
 *
 * C HAS NO NAMESPACES, so the seven units this driver reads cannot meet in one
 * translation unit: tblv1's Cfg and tblv2's Cfg declare the same struct name,
 * and every unit's <Base>Table.h defines the same TableReport, TableWriter and
 * TableReader. Each unit therefore gets a translation unit of its own, which
 * includes only its own headers and hands the surfaces back through the ERASED
 * shapes below. main.c includes no generated header at all.
 *
 * That is what a real C consumer of two schema units does, and the generated
 * externals are spelled schema_<package>_<type>_<what>_ precisely so those
 * translation units link together. */

#ifndef SCHEMA_CONFORMANCE_DRIVER_H
#define SCHEMA_CONFORMANCE_DRIVER_H

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

/* every helper here is used by SOME translation unit of this driver and not by
   all of them, which is the shape a shared header has. */
#if defined(__GNUC__) || defined(__clang__)
#define SCHEMA_CONFORMANCE_UNUSED __attribute__((unused))
#else
#define SCHEMA_CONFORMANCE_UNUSED
#endif

/* Each unit declares its OWN TableReport — the generated surface is not
 * namespaced (§6.1) — so the driver carries one report shape of its own and
 * each row copies into it. Five counters is the whole of §4's report. */
typedef struct ConformanceReport
{
    int unknown, kind_mismatch, clamped, duplicate;
    int malformed;
} ConformanceReport;

/* One row per (unit, root) the corpus names. Every row is the SAME six
 * function pointers, so a surface is one loop over the table and not a switch
 * per root. */
typedef struct ConformanceCodec
{
    const char * unit;
    const char * root;
    int ( *load )( void * value, const uint8_t * bytes, int64_t size, ConformanceReport * report );
    int64_t ( *measure )( const void * value );
    int64_t ( *save )( const void * value, uint8_t * buffer, int64_t capacity );
    int ( *from_json )( void * value, const char * text, int64_t bytes, ConformanceReport * report );
    int64_t ( *to_json )( const void * value, char * buffer, int64_t capacity );
    void * ( *storage )( void ); /* one static instance per root, reset by load */
} ConformanceCodec;

/* A GROWING TEXT, for the two dumps. The harness compares bytes, so nothing
 * here formats anything a reader has to parse back. */
typedef struct ConformanceText
{
    char * data;
    size_t length;
    size_t capacity;
} ConformanceText;

SCHEMA_CONFORMANCE_UNUSED static void conformance_text_raw( ConformanceText * out, const char * data, size_t bytes )
{
    if ( out->length + bytes + 1 > out->capacity )
    {
        size_t want = ( out->capacity ? out->capacity : 4096 );
        while ( want < out->length + bytes + 1 ) { want *= 2; }
        out->data = (char *) realloc( out->data, want );
        if ( out->data == NULL ) { abort(); }
        out->capacity = want;
    }
    memcpy( out->data + out->length, data, bytes );
    out->length += bytes;
    out->data[out->length] = 0;
}

SCHEMA_CONFORMANCE_UNUSED static void conformance_text_add( ConformanceText * out, const char * s )
{
    conformance_text_raw( out, s, strlen( s ) );
}

/* the per-unit entry points, each defined in that unit's own translation unit */
const ConformanceCodec * conformance_codecs_tabledemo( int * count );
const ConformanceCodec * conformance_codecs_tblv1( int * count );
const ConformanceCodec * conformance_codecs_tblv2( int * count );
const ConformanceCodec * conformance_codecs_tblp1( int * count );
const ConformanceCodec * conformance_codecs_tblp3( int * count );

/* the BLOCK unit (blockdemo): open one image at the extent and pointer the
 * caller claims, and read every row out of the descriptors */
int conformance_block_open( const char * name, const uint8_t * data, size_t bytes, int64_t extent, int pointer );
int conformance_block_dump( const char * name, const uint8_t * data, size_t bytes, ConformanceText * out );

/* the COOK unit (graphdemo): the canonical node dump, and the forgery
 * battery's one question.
 *
 * conformance_quiet silences the walk's own refusal messages. A driver run
 * wants them — a walk that refused is a case that failed. The FUZZER does not:
 * a forged file whose walk refuses is the CORRECT outcome, tens of thousands of
 * times, and the noise would bury the one message that matters. It is a flag
 * rather than a redirect because the sanitizers write to stderr too. */
extern int conformance_quiet;
int conformance_cook_dump( const char * root, const uint8_t * data, size_t bytes, ConformanceText * out );
int conformance_cook_open( const char * root, const uint8_t * data, size_t bytes, int64_t extent, int pointer );

/* THE BUFFER A CALLER HOLDS. `extent` is the length the caller CLAIMS, which
 * may run past the bytes the file carries or short of them; `pointer` is where
 * its base sits — 0 an aligned base, 1..63 that many bytes past one, and a
 * negative pointer means no buffer at all. A driver allocates EXACTLY the
 * claim, copies what fits and zeroes the rest, so a reader that walks past what
 * it was given walks into a sanitizer's redzone rather than into a neighbour. */
typedef struct ConformanceBuffer
{
    uint8_t * allocation;
    uint8_t * base;
    int64_t bytes;
} ConformanceBuffer;

SCHEMA_CONFORMANCE_UNUSED static int conformance_buffer_create( ConformanceBuffer * buffer, const uint8_t * data, size_t bytes,
                                      int64_t extent, int pointer )
{
    size_t copy;
    buffer->allocation = NULL;
    buffer->base = NULL;
    buffer->bytes = extent < 0 ? (int64_t) bytes : extent;
    if ( pointer < 0 ) { buffer->bytes = 0; return 1; } /* no buffer at all */
    buffer->allocation = (uint8_t *) malloc( (size_t) ( buffer->bytes > 0 ? buffer->bytes : 1 ) + 64 + 64 );
    if ( buffer->allocation == NULL ) { return 0; }
    buffer->base = (uint8_t *) ( ( (uintptr_t) buffer->allocation + 63 ) & ~(uintptr_t) 63 );
    buffer->base += pointer;
    memset( buffer->base, 0, (size_t) buffer->bytes );
    copy = bytes < (size_t) buffer->bytes ? bytes : (size_t) buffer->bytes;
    memcpy( buffer->base, data, copy );
    return 1;
}

SCHEMA_CONFORMANCE_UNUSED static void conformance_buffer_destroy( ConformanceBuffer * buffer )
{
    free( buffer->allocation );
    buffer->allocation = NULL;
    buffer->base = NULL;
}

#endif /* SCHEMA_CONFORMANCE_DRIVER_H */
