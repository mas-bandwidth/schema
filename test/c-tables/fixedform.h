/* THE FIXED FORM'S VERSIONING CONFORMANCE, C leg (docs/SPEC-TABLES.md §3.4).
   The C++ reference is test/tables/fixedform_main.cpp and this holds the same
   invariant: a fixed record is positional BY PLAN, and the positions are the
   WRITER's LAYOUT and never the reader's own declaration.

   ONE UNIT PER TRANSLATION UNIT. C has no namespace, so the reference's
   tblfx1::FxRoot beside tblfx2::FxRoot has no C spelling: two generations of
   one schema cannot share a TU (SPEC §6.1, and internal/codegen/ctable's
   package comment states it). So each generation gets a .c of its own, this
   header is the whole surface between them, and NOT ONE GENERATED TYPE
   APPEARS IN IT. It is the same shape the C conformance driver already uses
   for two generations of one schema.

   EACH SIDE MAKES ITS OWN CHECKS, because each side is the only one that can
   name what it expects. They report through fixed_check, which main.c owns. */

#ifndef SCHEMA_TEST_C_FIXEDFORM_H
#define SCHEMA_TEST_C_FIXEDFORM_H

#include <stdint.h>

/* one assertion, counted; `what` is the sentence a failure prints */
void fixed_check( int ok, const char * what );

/* Each generation writes ONE record with every field off its default, so
   nothing below can pass by accident. */
int64_t fixed_fx1_write( uint8_t * buffer, int64_t capacity );
int64_t fixed_fx1_bytes( void );
int64_t fixed_fx2_write( uint8_t * buffer, int64_t capacity );
int64_t fixed_fx2_bytes( void );
int64_t fixed_v1_write( uint8_t * buffer, int64_t capacity );
int64_t fixed_v1_bytes( void );

/* ...and each reads the other's, through the ONE plan-driven path. */
void fixed_fx1_read_own( const uint8_t * data, int64_t bytes );
void fixed_fx1_read_fx2( const uint8_t * data, int64_t bytes );
void fixed_fx2_read_fx1( const uint8_t * data, int64_t bytes );
void fixed_v2_read_v1( const uint8_t * data, int64_t bytes );

/* THE BYTE-FLIP FUZZ's reader (docs/SPEC-TABLES.md §3.4, "held by test"): it
   makes no claim about the values, only that the read answers one of the three
   ways the form allows and never leaves the buffer doing it. Every offset this
   reader uses is arithmetic over sizes a STRANGER wrote down, so the claim is
   one only a sanitizer can hold and the run is under one. */
void fixed_fx2_probe( const uint8_t * data, int64_t bytes );

#endif /* SCHEMA_TEST_C_FIXEDFORM_H */
