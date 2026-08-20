// probe_main.cpp — EXPERIMENT (issue #105). Keeps the eight probe symbols alive
// in a LINKED binary so their call targets resolve to real symbols (an
// unlinked .o shows every external call as a placeholder branch to offset 0,
// which the verdict pass cannot classify).
#include <cstdio>
#include <cstdint>
#include "Wire.h"
#include "Objects.h"
#include "serialize.h"

extern "C" bool probe_control_verbatim_write_testdata( serialize::WriteStream &, const example::TestData & );
extern "C" bool probe_control_perfield_write_testdata( serialize::WriteStream &, const example::TestData & );
extern "C" bool probe_emitted_write_testdata( serialize::WriteStream &, const example::TestData & );
extern "C" bool probe_generic_write_testdata( serialize::WriteStream &, const example::TestData & );
extern "C" bool probe_emitted_read_testdata( serialize::ReadStream &, example::TestData & );
extern "C" bool probe_generic_read_testdata( serialize::ReadStream &, example::TestData & );
extern "C" bool probe_emitted_write_ship( serialize::WriteStream &, const example::ShipData_Deep & );
extern "C" bool probe_generic_write_ship( serialize::WriteStream &, const example::ShipData_Deep & );
extern "C" bool probe_emitted_read_ship( serialize::ReadStream &, example::ShipData_Deep & );
extern "C" bool probe_generic_read_ship( serialize::ReadStream &, example::ShipData_Deep & );

int main()
{
    static uint8_t buffer[4096];
    example::TestData td{};
    example::ShipData_Deep sd{};
    int n = 0;
    { serialize::WriteStream s( buffer, sizeof( buffer ) ); n += probe_emitted_write_testdata( s, td ); }
    { serialize::WriteStream s( buffer, sizeof( buffer ) ); n += probe_generic_write_testdata( s, td ); }
    { serialize::ReadStream  s( buffer, sizeof( buffer ) ); n += probe_emitted_read_testdata( s, td ); }
    { serialize::ReadStream  s( buffer, sizeof( buffer ) ); n += probe_generic_read_testdata( s, td ); }
    { serialize::WriteStream s( buffer, sizeof( buffer ) ); n += probe_emitted_write_ship( s, sd ); }
    { serialize::WriteStream s( buffer, sizeof( buffer ) ); n += probe_generic_write_ship( s, sd ); }
    { serialize::ReadStream  s( buffer, sizeof( buffer ) ); n += probe_emitted_read_ship( s, sd ); }
    { serialize::ReadStream  s( buffer, sizeof( buffer ) ); n += probe_generic_read_ship( s, sd ); }
    { serialize::WriteStream s( buffer, sizeof( buffer ) ); n += probe_control_verbatim_write_testdata( s, td ); }
    { serialize::WriteStream s( buffer, sizeof( buffer ) ); n += probe_control_perfield_write_testdata( s, td ); }
    printf( "%d\n", n );
    return 0;
}
