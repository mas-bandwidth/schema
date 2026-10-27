package cqualify

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ccommon"
)

// unit is a hand-written stand-in for an emitter's output: one of every
// file-scope declaration shape the C emitters produce, and every place a
// name may appear WITHOUT being a declared name's use.
const unitHeader = `/* type Vec3 — a comment naming Vec3 and write_vec3 stays as written */
#ifndef SCHEMA_EXAMPLE_TYPES_H
#define SCHEMA_EXAMPLE_TYPES_H
#include "SchemaRuntime.h"
#define MAX_OBJECTS (10000)
#define PROTOCOL_ID 0x1ULL
#define SCHEMA_TABLE_REFUSE_RUNTIME
#ifndef schema_assert
#define schema_assert assert
#endif
#define SQUARE( Vec3 ) ( ( Vec3 ) * ( Vec3 ) )
typedef uint8_t ShipType;
#define SHIP_TYPE_BOMBER 1
typedef struct Vec3 {
    double x;
    double write_vec3; /* a member that shares a function's spelling */
} Vec3;
typedef struct TableReport { int32_t unknown; } TableReport;
enum SCHEMA_TABLE_REFUSAL_REASON { SCHEMA_TABLE_NO_REFUSAL, SCHEMA_TABLE_NEWER_FORM };
enum { kTableAnnounce = 3, TableByteOrder_Big = 1 };
typedef void * ( * TableAllocFn )( void * context, size_t bytes );
static const char * const vec3_names[2] = { "Vec3", "write_vec3" };
static SCHEMA_UNUSED int write_vec3( serialize_write_stream_t * stream, const Vec3 * value )
{
    (void) value->write_vec3;
    return SCHEMA_UNUSED_HELPER( stream ) && MAX_OBJECTS > 0 && sizeof( Vec3 ) > 0;
}
static SCHEMA_UNUSED int schema_example_vec3_save_( const Vec3 * value );
SCHEMA_TABLE_STATIC_ASSERT( example_vec3_size, sizeof( Vec3 ) == 16, "Vec3" );
#endif
`

func qualifyOne(t *testing.T, src string) string {
	t.Helper()
	out, err := Qualify(map[string][]byte{"Types.h": []byte(src), ccommon.RuntimeHeaderName: ccommon.RuntimeHeader()}, "example")
	if err != nil {
		t.Fatal(err)
	}
	if string(out[ccommon.RuntimeHeaderName]) != string(ccommon.RuntimeHeader()) {
		t.Error("the shared runtime header must pass through untouched")
	}
	return string(out["Types.h"])
}

// TestQualifySpellings pins the one rule set (SPEC §6.3): a type takes
// <package>_<snake>_t, a function or object <package>_<name>, a macro or
// constant <PACKAGE>_<NAME>, a reserved SCHEMA_/schema_ name the package
// after its prefix — and a name already the package's stays as it is.
func TestQualifySpellings(t *testing.T) {
	got := qualifyOne(t, unitHeader)
	for _, want := range []string{
		"typedef struct example_vec3_t {",
		"} example_vec3_t;",
		"typedef uint8_t example_ship_type_t;",
		"#define EXAMPLE_SHIP_TYPE_BOMBER 1",
		"#define EXAMPLE_MAX_OBJECTS (10000)",
		"#define EXAMPLE_PROTOCOL_ID 0x1ULL",
		"#define SCHEMA_EXAMPLE_TABLE_REFUSE_RUNTIME",
		"typedef struct example_table_report_t { int32_t unknown; } example_table_report_t;",
		"enum SCHEMA_EXAMPLE_TABLE_REFUSAL_REASON { SCHEMA_EXAMPLE_TABLE_NO_REFUSAL, SCHEMA_EXAMPLE_TABLE_NEWER_FORM };",
		"enum { EXAMPLE_K_TABLE_ANNOUNCE = 3, EXAMPLE_TABLE_BYTE_ORDER_BIG = 1 };",
		"typedef void * ( * example_table_alloc_fn_t )( void * context, size_t bytes );",
		"static const char * const example_vec3_names[2] = { \"Vec3\", \"write_vec3\" };",
		"static SCHEMA_UNUSED int example_write_vec3( serialize_write_stream_t * stream, const example_vec3_t * value )",
		"EXAMPLE_MAX_OBJECTS > 0 && sizeof( example_vec3_t ) > 0",
		// already the package's: untouched
		"#ifndef SCHEMA_EXAMPLE_TYPES_H",
		"static SCHEMA_UNUSED int schema_example_vec3_save_( const example_vec3_t * value );",
		// the shared runtime's names: untouched
		"#include \"SchemaRuntime.h\"",
		"SCHEMA_TABLE_STATIC_ASSERT( example_vec3_size, sizeof( example_vec3_t ) == 16, \"Vec3\" );",
		// a caller-overridable hook: untouched
		"#ifndef schema_assert\n#define schema_assert assert",
		// comments, strings, members and a macro's own parameters: untouched
		"/* type Vec3 — a comment naming Vec3 and write_vec3 stays as written */",
		"double write_vec3; /* a member that shares a function's spelling */",
		"(void) value->write_vec3;",
		"#define EXAMPLE_SQUARE( Vec3 ) ( ( Vec3 ) * ( Vec3 ) )",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SCHEMA_EXAMPLE_EXAMPLE") || strings.Contains(got, "example_example_") {
		t.Errorf("a name was qualified twice:\n%s", got)
	}
}

// TestQualifyIsOneMapAcrossFiles: a name declared in one file of the unit is
// respelled in every file that uses it, and a use in a .c file reaches the
// declaration in its header.
func TestQualifyIsOneMapAcrossFiles(t *testing.T) {
	out, err := Qualify(map[string][]byte{
		"Types.h":     []byte("typedef struct Vec3 { int x; } Vec3;\n"),
		"TypesWire.h": []byte("#include \"Types.h\"\nstatic int write_vec3( const Vec3 * v ) { return v->x; }\n"),
		"View.c":      []byte("#include \"TypesWire.h\"\nint vec3_count( void ) { Vec3 v = { 1 }; return write_vec3( &v ); }\n"),
	}, "example")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out["View.c"]); !strings.Contains(got, "int example_vec3_count( void ) { example_vec3_t v = { 1 }; return example_write_vec3( &v ); }") {
		t.Errorf("the .c file must take the header's spellings:\n%s", got)
	}
}

// TestQualifyRefusesACollision: two names that would spell the same symbol
// after qualification are refused by name, never emitted twice.
func TestQualifyRefusesACollision(t *testing.T) {
	_, err := Qualify(map[string][]byte{
		"Types.h": []byte("typedef struct MaxHp { int x; } MaxHp;\ntypedef struct max_hp_t { int y; } max_hp_t;\n"),
	}, "example")
	if err == nil || !strings.Contains(err.Error(), "example_max_hp_t") {
		t.Fatalf("a collision after qualification must be refused by name, got %v", err)
	}
}

// TestTwoUnitsShareOnlyTheRuntime: two packages' maps over the same
// declarations have no qualified name in common — the property that lets
// their headers share a translation unit.
func TestTwoUnitsShareOnlyTheRuntime(t *testing.T) {
	files := map[string][]byte{"Types.h": []byte(unitHeader)}
	a, err := Names(files, "tblw1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Names(files, "tblw2")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, q := range a {
		seen[q] = true
	}
	for n, q := range b {
		if seen[q] {
			t.Errorf("%s spells %s in both packages", n, q)
		}
	}
	if len(a) == 0 || len(a) != len(b) {
		t.Errorf("both packages must qualify the same declarations: %d and %d", len(a), len(b))
	}
}

// TestRuntimeHeaderVersionTracksItsText: the version every header checks is
// the shared header's own content, so any edit to the runtime moves it, and
// the include every header opens with names the same number.
func TestRuntimeHeaderVersionTracksItsText(t *testing.T) {
	h := string(ccommon.RuntimeHeader())
	inc := ccommon.RuntimeInclude()
	for _, want := range []string{"#define SCHEMA_RUNTIME_VERSION 0x", "#ifndef SCHEMA_RUNTIME_H"} {
		if !strings.Contains(h, want) {
			t.Errorf("SchemaRuntime.h must carry %q", want)
		}
	}
	line := h[strings.Index(h, "#define SCHEMA_RUNTIME_VERSION "):]
	version := strings.Fields(line)[2]
	if !strings.Contains(inc, "#if SCHEMA_RUNTIME_VERSION != "+version) || !strings.Contains(inc, "#error") {
		t.Errorf("the include must check the header's own version %s with a hard #error:\n%s", version, inc)
	}
}
