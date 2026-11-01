/* THE C AND C++ PORTS RUN THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md): one
 * case per process.
 *
 * The case binary answers ONE manifest line: its carrier, schema, table and raw
 * input file are its arguments, and its answer is written to stdout for the
 * Go driver (tools/hostilecorpusexec), which owns the manifest, the oracle,
 * the time budget, the crash isolation and the carried findings:
 *
 *   accept text\n<the port's text writer's bytes>   a decoded value
 *   accept bytes\n<the port's packet writer's bytes> a decoded packet
 *   accept -\n                                      an announcement read
 *   refuse <reason>\n                               a named refusal, or damaged
 *   skip <why>\n                                    the port has no such API
 *   unbound <what>\n                                the runner names no binding
 *   error <what>\n                                  the port could not answer
 *
 * This file is what the C and C++ ports share: the binding row each unit
 * exports as hc_bindings_<unit>, and the answer functions the case binary
 * (main.c) defines. Each port's binding macros are its own: hostile_c.h here,
 * test/cpp-hostile/hostile.hpp for C++. */

#ifndef SCHEMA_C_HOSTILE_H
#define SCHEMA_C_HOSTILE_H

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct hc_binding_t
{
    const char * schema;
    const char * table;
    /* NULL is a carrier this port has no API for: the case answers skip */
    void ( *wire )( const uint8_t * data, int64_t size );
    void ( *wire_retain )( const uint8_t * data, int64_t size );
    void ( *text )( const uint8_t * data, int64_t size );
    void ( *message )( const uint8_t * data, int64_t size, int announced );
    void ( *announce )( const uint8_t * data, int64_t size, const char * carrier );
    void ( *packet )( const uint8_t * data, int64_t size );
    const char * wire_retain_skip; /* why wire_retain is NULL, when it is */
} hc_binding_t;

void hc_accept_text( const char * text, int64_t size );
void hc_accept_bytes( const uint8_t * bytes, int64_t size );
void hc_accept_none( void );
void hc_refuse( const char * reason );
void hc_error( const char * what );
/* a refused read's reason: every C unit spells the same numbers (the
   SCHEMA_<PACKAGE>_TABLE_REFUSAL_REASON enum and the message reasons) */
const char * hc_reason_name( int reason );
/* with HC_TRACE set in the environment, the report a verdict was read off is
   printed to stderr, for working a FIX-CARD by hand */
void hc_trace( int refused, int malformed, int reason, int landed );
/* the post-accept surface (rule 4): a save that answers -1 is a clean
   refusal; only a crash, a sanitizer report or a hang is a failure */
void hc_exercise_save( int64_t size, int64_t ( *save )( const void *, uint8_t *, int64_t ), const void * value );

#ifdef __cplusplus
}
#endif

#endif /* SCHEMA_C_HOSTILE_H */
