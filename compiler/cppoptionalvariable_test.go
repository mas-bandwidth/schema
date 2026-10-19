package compiler

import (
	"testing"
)

// TestCPPOptionalVariableClosureTable holds docs/SPEC-TABLES.md §2.3's rule for
// `?T` over a variable closure in the C++ backend: every walk that follows
// pointer edges gates on the presence companion, so an absent value owns no
// node. The expected answers below are the ones
// internal/codegen/gotable/optionalvariable_test.go's
// TestGoOptionalVariableClosureTable pins for the same schema text and the same
// input bytes (cases "scalar", "lock in a list and a union arm", and "lock in a
// map value"), transcribed into C++ probes:
//
//   - absent, present with its text set, and present with its text empty each
//     round-trip the binary wire and the JSON text;
//   - an absent value leaves no node in the measured size: the absent wire is
//     pinned byte for byte (absentGolden), and its measure and cook measure
//     are the same whether or not the absent value still holds a pointer;
//   - a present value then cleared and saved writes no stale node, through
//     save, measure, the retaining save, the cooked form, Lock's pack and the
//     JSON writer.
func TestCPPOptionalVariableClosureTable(t *testing.T) {
	t.Parallel()
	t.Run("scalar", func(t *testing.T) {
		t.Parallel()
		files, err := New().Generate(unitFromSource(t, optionalVariableCppSchema), "cpp", Options{})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		runCppTableProbe(t, files, optionalVariableCppProbe)
	})
	// a unit with containers takes the container pack, and a union arm is a
	// value the pack descends: both reset a cleared ?T at Lock (§2.3)
	t.Run("lock in a list and a union arm", func(t *testing.T) {
		t.Parallel()
		files, err := New().Generate(unitFromSource(t, optionalVariableNestedCppSchema), "cpp", Options{})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		runCppTableProbe(t, files, optionalVariableNestedCppProbe)
	})
	// a map value is a container element the pack places first; the clear
	// then walks the entry and resets the absent ?T inside its value (§2.3)
	t.Run("lock in a map value", func(t *testing.T) {
		t.Parallel()
		files, err := New().Generate(unitFromSource(t, optionalVariableMapCppSchema), "cpp", Options{})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		runCppTableProbe(t, files, optionalVariableMapCppProbe)
	})
}

// optionalVariableCppSchema is optionalVariableSchema from
// internal/codegen/gotable/optionalvariable_test.go, case "scalar".
const optionalVariableCppSchema = `package probe

table Inner
{
    text *string
    n int32
}

table Outer
{
    inner ?Inner
    tail int32
}
`

// optionalVariableCppProbe transcribes TestOptionalVariableClosure in
// internal/codegen/gotable/optionalvariable_test.go (case "scalar").
const optionalVariableCppProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
#include <cstdlib>

using namespace probe;

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)

// absentGolden is the pinned wire from
// internal/codegen/gotable/optionalvariable_test.go's absentGolden: Outer{tail:
// 5} with inner absent. No node payload rides: an absent value owns no node.
static const uint8_t absentGolden[] = {
    0x01, 0x01, 0x04, 0x05, 0x00, 0x00, 0x00, 0x00,
    0x23, 0xd7, 0x98, 0x07, 0xef, 0x56, 0x4c, 0xd9,
    0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00
};

static uint8_t buffer[ 1 << 14 ];
static uint8_t twin[ 1 << 14 ];
static uint8_t rbuf[ 1 << 14 ];
alignas( 64 ) static uint8_t region[ 1 << 16 ];

static bool holds( const uint8_t * data, int64_t bytes, const char * needle )
{
    const int64_t n = (int64_t) std::strlen( needle );
    if ( n == 0 || bytes < n ) { return false; }
    for ( int64_t i = 0; i + n <= bytes; i++ )
    {
        if ( std::memcmp( data + i, needle, (size_t) n ) == 0 ) { return true; }
    }
    return false;
}

static int64_t save_builder( OuterBuilder & b )
{
    const int64_t n = OuterMeasure( b );
    if ( n < 0 ) { return -1; }
    if ( OuterSave( b, buffer, (int64_t) sizeof( buffer ) ) != n ) { return -2; }
    return n;
}

static int64_t save_root( const Outer * v )
{
    if ( v == NULL ) { return -1; }
    const int64_t n = OuterMeasure( v );
    if ( n < 0 ) { return -1; }
    if ( OuterSave( v, buffer, (int64_t) sizeof( buffer ) ) != n ) { return -2; }
    return n;
}

// json_is compares the JSON text with the compact spelling the Go oracle's
// compactText pins, ignoring the writer's indentation (no value in this probe
// carries whitespace, so stripping it is exact).
static bool json_is( const Outer * v, const char * want )
{
    const int64_t n = OuterToJsonMeasure( v );
    if ( n < 0 ) { return false; }
    char * text = (char *) std::malloc( (size_t) n + 1 );
    if ( text == NULL ) { return false; }
    const int64_t got = OuterToJson( v, text, n );
    char compact[ 1 << 12 ];
    int64_t at = 0;
    for ( int64_t i = 0; i < got; i++ )
    {
        const char c = text[i];
        if ( c == ' ' || c == '\n' || c == '\t' || c == '\r' ) { continue; }
        if ( at < (int64_t) sizeof( compact ) ) { compact[at++] = c; }
    }
    const int64_t wanted = (int64_t) std::strlen( want );
    const bool same = at == wanted && std::memcmp( compact, want, (size_t) wanted ) == 0;
    if ( !same ) { std::fprintf( stderr, "json got %.*s want %s\n", (int) at, compact, want ); }
    std::free( text );
    return same;
}

int main()
{
    // ABSENT, FRESH: the baseline every absent measure is pinned against.
    OuterBuilder fresh;
    Outer * fv = fresh.GetRoot();
    CHECK( fv != NULL );
    fv->tail = 5;
    const int64_t absent = save_builder( fresh );
    CHECK( absent == (int64_t) sizeof( absentGolden ) );
    CHECK( std::memcmp( buffer, absentGolden, sizeof( absentGolden ) ) == 0 );

    const int64_t absentRegion = OuterLoadMeasure( buffer, absent );
    CHECK( absentRegion > 0 );
    const int64_t absentCook = OuterCookMeasure( fresh );
    CHECK( absentCook > 0 );
    CHECK( fresh.Lock() );
    const int64_t absentPack = fresh.RegionBytes();
    CHECK( absentPack > 0 );
    CHECK( fresh.AsConst() != NULL && !fresh.AsConst()->inner_present );
    CHECK( json_is( fresh.AsConst(), "{\"tail\":5}" ) );

    // The JSON reader leaves T absent, at its defaults, on null (§16.2).
    {
        OuterBuilder nb;
        TableReport report;
        const char * text = "{\"inner\":null,\"tail\":5}";
        CHECK( OuterFromJson( nb, text, (int64_t) std::strlen( text ), &report ) );
        CHECK( !report.malformed && !report.refused && report.unknown == 0 );
        CHECK( nb.GetRoot()->inner_present == false && nb.GetRoot()->inner.text.value == 0 );
        const int64_t n = save_builder( nb );
        CHECK( n == absent && std::memcmp( buffer, absentGolden, sizeof( absentGolden ) ) == 0 );
    }

    // PRESENT, TEXT SET.
    OuterBuilder b;
    Outer * v = b.GetRoot();
    CHECK( v != NULL );
    v->tail = 5;
    v->inner_present = true;
    char * hi = TableStringEmplace( b.main, v->inner.text, "hi", 2 );
    CHECK( hi != NULL && v->inner.text.value != 0 );
    const int64_t present = save_builder( b );
    CHECK( present > absent );
    CHECK( holds( buffer, present, "hi" ) );

    // PRESENT, TEXT EMPTY: the pointer is set and names an empty string.
    {
        OuterBuilder e;
        TableReport report;
        const char * text = "{\"inner\":{\"text\":\"\"},\"tail\":5}";
        CHECK( OuterFromJson( e, text, (int64_t) std::strlen( text ), &report ) );
        CHECK( !report.malformed && !report.refused );
        CHECK( e.GetRoot()->inner_present && e.GetRoot()->inner.text.value != 0 );
        const int64_t n = save_builder( e );
        CHECK( n > absent );
        CHECK( OuterLoadMeasure( buffer, n ) > 0 );
    }

    // PRESENT, ALL DEFAULT: rides even so (§2.3), and owns no node.
    {
        OuterBuilder d;
        Outer * dv = d.GetRoot();
        CHECK( dv != NULL );
        dv->tail = 5;
        dv->inner_present = true;
        CHECK( save_builder( d ) > 0 );
    }

    // A PRESENT VALUE THEN CLEARED writes no stale node: the presence bool is
    // cleared and inner.text still names the "hi" node.
    v->inner_present = false;
    CHECK( v->inner.text.value != 0 );
    CHECK( OuterMeasure( b ) == absent );
    CHECK( save_builder( b ) == absent );
    CHECK( std::memcmp( buffer, absentGolden, sizeof( absentGolden ) ) == 0 );
    CHECK( !holds( buffer, absent, "hi" ) );
    CHECK( OuterCookMeasure( b ) == absentCook );

    // the cooked writer itself, on the cleared builder value: it opens absent,
    // and the reference the cleared T still holds is written null (§7).
    {
        alignas( 64 ) uint8_t cooked[ 1 << 14 ];
        std::memset( cooked, 0, sizeof( cooked ) );
        CHECK( OuterCook( b, cooked, sizeof( cooked ), TableByteOrder::Little ) );
        const Outer * croot = OuterOpen( cooked, (uint64_t) OuterCookMeasure( b ) );
        CHECK( croot != NULL && !croot->inner_present && croot->inner.text.value == 0 && croot->tail == 5 );
    }

    // The JSON writer on the cleared builder value, from a loaded region.
    CHECK( OuterLoadMeasure( buffer, absent ) == absentRegion );
    const Outer * cleared = OuterLoad( region, (int64_t) sizeof( region ), buffer, absent, NULL );
    CHECK( cleared != NULL && !cleared->inner_present && cleared->inner.text.value == 0 );
    CHECK( json_is( cleared, "{\"tail\":5}" ) );

    // Lock's pack writes the absent T at its defaults, every pointer null,
    // never the stale arena reference the cleared storage still held.
    CHECK( b.Lock() );
    CHECK( b.AsConst() != NULL && !b.AsConst()->inner_present );
    CHECK( b.AsConst()->inner.text.value == 0 );
    CHECK( (int64_t) b.RegionBytes() == absentPack );
    CHECK( save_root( b.AsConst() ) == absent );
    CHECK( std::memcmp( buffer, absentGolden, sizeof( absentGolden ) ) == 0 );

    // RETAIN-UNKNOWN (§6.6) gives back the same bytes for the absent wire.
    {
        TableRetain retain;
        TableReport report;
        const Outer * kept = OuterLoadRetain( region, (int64_t) sizeof( region ), buffer, absent, &retain, &report );
        CHECK( kept != NULL && !report.malformed && !report.refused );
        const int64_t n = OuterSaveRetain( kept, &retain, rbuf, (int64_t) sizeof( rbuf ), &report );
        CHECK( n == absent && std::memcmp( rbuf, absentGolden, sizeof( absentGolden ) ) == 0 );
    }

    // The present wire round-trips through Load and the retaining read.
    {
        OuterBuilder p;
        Outer * pv = p.GetRoot();
        CHECK( pv != NULL );
        pv->tail = 5;
        pv->inner_present = true;
        CHECK( TableStringEmplace( p.main, pv->inner.text, "hi", 2 ) != NULL );
        const int64_t n = save_builder( p );
        CHECK( n > absent );
        std::memcpy( twin, buffer, (size_t) n );
        const int64_t need = OuterLoadMeasure( buffer, n );
        CHECK( need > 0 );
        const Outer * back = OuterLoad( region, (int64_t) sizeof( region ), buffer, n, NULL );
        CHECK( back != NULL && back->inner_present && back->inner.text.value != 0 && back->tail == 5 );
        CHECK( save_root( back ) == n && std::memcmp( buffer, twin, (size_t) n ) == 0 );
        TableRetain retain;
        TableReport report;
        const Outer * kept = OuterLoadRetain( region, (int64_t) sizeof( region ), buffer, n, &retain, &report );
        CHECK( kept != NULL && !report.malformed && !report.refused );
        const int64_t rn = OuterSaveRetain( kept, &retain, rbuf, (int64_t) sizeof( rbuf ), &report );
        CHECK( rn == n && std::memcmp( rbuf, twin, (size_t) n ) == 0 );
    }

    return 0;
}
`

// optionalVariableNestedCppSchema is optionalVariableNestedSchema from
// internal/codegen/gotable/optionalvariable_test.go, case "lock in a list and a
// union arm".
const optionalVariableNestedCppSchema = `package probe

table Inner
{
    text *string
}

table Holder
{
    inner ?Inner
    n int32
}

union Arm
{
    holder Holder
}

table Outer
{
    items []Holder
    arm Arm
    tail int32
}
`

// optionalVariableNestedCppProbe transcribes TestOptionalVariableClosureNested
// in internal/codegen/gotable/optionalvariable_test.go.
const optionalVariableNestedCppProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>

using namespace probe;

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)

static uint8_t absentBuf[ 1 << 14 ];
static uint8_t scratch[ 1 << 14 ];

static bool holds( const uint8_t * data, int64_t bytes, const char * needle )
{
    const int64_t n = (int64_t) std::strlen( needle );
    if ( n == 0 || bytes < n ) { return false; }
    for ( int64_t i = 0; i + n <= bytes; i++ )
    {
        if ( std::memcmp( data + i, needle, (size_t) n ) == 0 ) { return true; }
    }
    return false;
}

static int64_t save_builder( OuterBuilder & b, uint8_t * dst, int64_t cap )
{
    const int64_t n = OuterMeasure( b );
    if ( n < 0 ) { return -1; }
    if ( OuterSave( b, dst, cap ) != n ) { return -2; }
    return n;
}

static int64_t save_root( const Outer * v, uint8_t * dst, int64_t cap )
{
    if ( v == NULL ) { return -1; }
    const int64_t n = OuterMeasure( v );
    if ( n < 0 ) { return -1; }
    if ( OuterSave( v, dst, cap ) != n ) { return -2; }
    return n;
}

int main()
{
    // the absent baseline: one list element and the arm, each ?Inner absent
    OuterBuilder a;
    Outer * av = a.GetRoot();
    CHECK( av != NULL );
    av->tail = 5;
    CHECK( OuterItemsAdd( a.main, av->items ) != NULL );
    av->arm.type = ArmType::Holder;
    const int64_t absent = save_builder( a, absentBuf, (int64_t) sizeof( absentBuf ) );
    CHECK( absent > 0 );
    CHECK( a.Lock() );
    const int64_t absentPack = a.RegionBytes();
    CHECK( absentPack > 0 );

    // present, then cleared: each Inner still names its text node
    OuterBuilder b;
    Outer * v = b.GetRoot();
    CHECK( v != NULL );
    v->tail = 5;
    Holder * item = OuterItemsAdd( b.main, v->items );
    CHECK( item != NULL );
    item->inner_present = true;
    CHECK( TableStringEmplace( b.main, item->inner.text, "aa", 2 ) != NULL );
    v->arm.type = ArmType::Holder;
    Holder * arm = &v->arm.holder;
    arm->inner_present = true;
    CHECK( TableStringEmplace( b.main, arm->inner.text, "bb", 2 ) != NULL );
    const int64_t present = save_builder( b, scratch, (int64_t) sizeof( scratch ) );
    CHECK( present > absent );
    CHECK( holds( scratch, present, "aa" ) && holds( scratch, present, "bb" ) );

    item->inner_present = false;
    arm->inner_present = false;
    CHECK( item->inner.text.value != 0 && arm->inner.text.value != 0 );
    const int64_t cleared = save_builder( b, scratch, (int64_t) sizeof( scratch ) );
    CHECK( cleared == absent && std::memcmp( scratch, absentBuf, (size_t) absent ) == 0 );
    CHECK( b.Lock() );
    CHECK( (int64_t) b.RegionBytes() == absentPack );
    const Outer * c = b.AsConst();
    CHECK( c != NULL );
    int seen = 0;
    for ( const Holder & h : c->items )
    {
        seen++;
        CHECK( !h.inner_present && h.inner.text.value == 0 );
    }
    CHECK( seen == 1 );
    CHECK( c->arm.type == ArmType::Holder && !c->arm.holder.inner_present && c->arm.holder.inner.text.value == 0 );
    const int64_t locked = save_root( c, scratch, (int64_t) sizeof( scratch ) );
    CHECK( locked == absent && std::memcmp( scratch, absentBuf, (size_t) absent ) == 0 );
    return 0;
}
`

// optionalVariableMapCppSchema is optionalVariableMapSchema from
// internal/codegen/gotable/optionalvariable_test.go, case "lock in a map value".
const optionalVariableMapCppSchema = `package probe

table Inner
{
    text *string
}

table Holder
{
    inner ?Inner
    n int32
}

table Outer
{
    byKey map[int32]Holder
    tail int32
}
`

// optionalVariableMapCppProbe transcribes TestOptionalVariableClosureMapValue
// in internal/codegen/gotable/optionalvariable_test.go.
const optionalVariableMapCppProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>

using namespace probe;

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)

static uint8_t absentBuf[ 1 << 14 ];
static uint8_t scratch[ 1 << 14 ];

static bool holds( const uint8_t * data, int64_t bytes, const char * needle )
{
    const int64_t n = (int64_t) std::strlen( needle );
    if ( n == 0 || bytes < n ) { return false; }
    for ( int64_t i = 0; i + n <= bytes; i++ )
    {
        if ( std::memcmp( data + i, needle, (size_t) n ) == 0 ) { return true; }
    }
    return false;
}

static int64_t save_builder( OuterBuilder & b, uint8_t * dst, int64_t cap )
{
    const int64_t n = OuterMeasure( b );
    if ( n < 0 ) { return -1; }
    if ( OuterSave( b, dst, cap ) != n ) { return -2; }
    return n;
}

static int64_t save_root( const Outer * v, uint8_t * dst, int64_t cap )
{
    if ( v == NULL ) { return -1; }
    const int64_t n = OuterMeasure( v );
    if ( n < 0 ) { return -1; }
    if ( OuterSave( v, dst, cap ) != n ) { return -2; }
    return n;
}

int main()
{
    // the absent baseline: one entry whose value holds an absent ?Inner
    OuterBuilder a;
    Outer * av = a.GetRoot();
    CHECK( av != NULL );
    av->tail = 5;
    CHECK( OuterByKeyInsert( a.main, av->byKey, 7 ) != NULL );
    const int64_t absent = save_builder( a, absentBuf, (int64_t) sizeof( absentBuf ) );
    CHECK( absent > 0 );
    CHECK( a.Lock() );
    const int64_t absentPack = a.RegionBytes();
    CHECK( absentPack > 0 );

    // present, then cleared: the value's Inner still names its text node
    OuterBuilder b;
    Outer * v = b.GetRoot();
    CHECK( v != NULL );
    v->tail = 5;
    Holder * h = OuterByKeyInsert( b.main, v->byKey, 7 );
    CHECK( h != NULL );
    h->inner_present = true;
    CHECK( TableStringEmplace( b.main, h->inner.text, "cc", 2 ) != NULL );
    const int64_t present = save_builder( b, scratch, (int64_t) sizeof( scratch ) );
    CHECK( present > absent && holds( scratch, present, "cc" ) );

    h->inner_present = false;
    CHECK( h->inner.text.value != 0 );
    const int64_t cleared = save_builder( b, scratch, (int64_t) sizeof( scratch ) );
    CHECK( cleared == absent && std::memcmp( scratch, absentBuf, (size_t) absent ) == 0 );
    CHECK( b.Lock() );
    CHECK( (int64_t) b.RegionBytes() == absentPack );
    const Outer * c = b.AsConst();
    CHECK( c != NULL );
    int seen = 0;
    for ( auto kv : c->byKey )
    {
        seen++;
        CHECK( kv.key == 7 && kv.value != NULL && !kv.value->inner_present && kv.value->inner.text.value == 0 );
    }
    CHECK( seen == 1 );
    const int64_t locked = save_root( c, scratch, (int64_t) sizeof( scratch ) );
    CHECK( locked == absent && std::memcmp( scratch, absentBuf, (size_t) absent ) == 0 );
    return 0;
}
`
