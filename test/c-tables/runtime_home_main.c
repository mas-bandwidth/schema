/* one translation unit, one <Base>Table.h, nothing else — docs/PORTING.md:1389 (J2).
   On the C leg the shared runtime is not a FILE: it is emitted into every
   <Base>Table.h behind SCHEMA_<PACKAGE>_TABLE_PRIMITIVES (ctable.go:267-269), so
   EVERY header must stand alone. table_float_to_bits is defined in that block and
   nowhere else, so a header that lost the runtime cannot compile this. */
#ifndef RUNTIME_HOME_HEADER
#error "RUNTIME_HOME_HEADER must name the one <Base>Table.h this translation unit includes"
#endif
#include RUNTIME_HOME_HEADER
int main( void ) { return table_float_to_bits( 1.0f ) == 0x3f800000u ? 0 : 1; }
