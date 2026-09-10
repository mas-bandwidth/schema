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
int64_t fixed_ut1_write( uint8_t * buffer, int64_t capacity );
int64_t fixed_ut1_bytes( void );
int64_t fixed_ut2_write( uint8_t * buffer, int64_t capacity );
int64_t fixed_ut2_bytes( void );

/* ...and each reads the other's, through the ONE plan-driven path. */
void fixed_fx1_read_own( const uint8_t * data, int64_t bytes );
void fixed_fx1_slack( void );
void fixed_fx1_read_fx2( const uint8_t * data, int64_t bytes );
void fixed_fx2_read_fx1( const uint8_t * data, int64_t bytes );
void fixed_fx2_bytes_row_control( const uint8_t * data, int64_t bytes );
void fixed_fx2_plan_cache( const uint8_t * fx1, int64_t fx1_bytes );
void fixed_v2_read_v1( const uint8_t * data, int64_t bytes );

/* TWO LANES, BECAUSE THEY ARE TWO FACTS: the guard's ordinal and the text op's
   own flavour (docs/SPEC-TABLES.md §3.4). The control puts them back in one
   lane and watches the same record read wrong. */
void fixed_ut1_read_own( const uint8_t * data, int64_t bytes );
void fixed_ut1_shared_lane_control( const uint8_t * data, int64_t bytes );
void fixed_ut1_read_ut2( const uint8_t * data, int64_t bytes );
void fixed_ut2_read_ut1( const uint8_t * data, int64_t bytes );

/* THE BOUNDS THE READ LOOP DOES NOT HOLD (docs/SPEC-TABLES.md §3.4): a ranged
   scalar's declared min and max, and an ORDINAL's set. Straight-line in the
   generated decode, after the copy, and the control is the loop run alone. */
int64_t fixed_fx1_write_out_of_range( uint8_t * buffer, int64_t capacity );
void fixed_fx1_bounds( const uint8_t * data, int64_t bytes );
void fixed_fx2_bounds( const uint8_t * data, int64_t bytes );
void fixed_ut1_bounds( void );
void fixed_v1_bounds( void );
void fixed_v1_absent_optional( void );
void fixed_fx1_text_content( void );

/* THE BYTE-FLIP FUZZ's reader (docs/SPEC-TABLES.md §3.4, "held by test"): it
   makes no claim about the values, only that the read answers one of the three
   ways the form allows and never leaves the buffer doing it. Every offset this
   reader uses is arithmetic over sizes a STRANGER wrote down, so the claim is
   one only a sanitizer can hold and the run is under one. */
void fixed_fx2_probe( const uint8_t * data, int64_t bytes );

#endif /* SCHEMA_TEST_C_FIXEDFORM_H */
