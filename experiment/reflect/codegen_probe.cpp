// codegen_probe.cpp — EXPERIMENT (issue #105). The code-identity harness.
//
// Eight symbols, four pairs. Each pair is the SAME operation on the SAME type
// reached two ways: through the compiler-emitted per-type function, and through
// the one generic function over the reflection surface. Both sides are
// __attribute__((noinline)) so each gets its own symbol to disassemble, and
// both sides call their inner function exactly once (BENCH-STANDARD §3.2 — the
// call sites are the same on both sides, so the only difference measured is how
// the field walk was expressed).
//
// Build at the flags bench/run.sh publishes for the C++ leg:
//   -O3 -DNDEBUG -DSERIALIZE_RELEASE -std=c++17 -Wall -Wextra -Werror
//   -ffp-contract=off -fno-rtti

#include "WireWire.h"
#include "ObjectsWire.h"

#include "schema_reflect.h"
#include "WireReflect.h"
#include "ObjectsReflect.h"

#define PROBE __attribute__(( noinline )) extern "C"

// ---- TestData ------------------------------------------------------------

PROBE bool probe_emitted_write_testdata( serialize::WriteStream & stream, const example::TestData & value )
{
    return example::WriteTestData( stream, value );
}

PROBE bool probe_generic_write_testdata( serialize::WriteStream & stream, const example::TestData & value )
{
    return schema_reflect::Write( stream, value );
}

PROBE bool probe_emitted_read_testdata( serialize::ReadStream & stream, example::TestData & value )
{
    return example::ReadTestData( stream, value );
}

PROBE bool probe_generic_read_testdata( serialize::ReadStream & stream, example::TestData & value )
{
    return schema_reflect::Read( stream, value );
}

// ---- ShipData_Deep -------------------------------------------------------

PROBE bool probe_emitted_write_ship( serialize::WriteStream & stream, const example::ShipData_Deep & value )
{
    return example::WriteShipData_Deep( stream, value );
}

PROBE bool probe_generic_write_ship( serialize::WriteStream & stream, const example::ShipData_Deep & value )
{
    return schema_reflect::Write( stream, value );
}

PROBE bool probe_emitted_read_ship( serialize::ReadStream & stream, example::ShipData_Deep & value )
{
    return example::ReadShipData_Deep( stream, value );
}

PROBE bool probe_generic_read_ship( serialize::ReadStream & stream, example::ShipData_Deep & value )
{
    return schema_reflect::Read( stream, value );
}

// ---- controls (no reflection) --------------------------------------------

#include "control_probe.h"

PROBE bool probe_control_verbatim_write_testdata( serialize::WriteStream & stream, const example::TestData & value )
{
    return control::WriteTestData_Verbatim( stream, value );
}

PROBE bool probe_control_perfield_write_testdata( serialize::WriteStream & stream, const example::TestData & value )
{
    return control::WriteTestData_PerField( stream, value );
}
