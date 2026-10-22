// THE ARRAY ROOT IN THE C++ TEXT FORM (docs/SPEC-TABLES.md §16.10).
//
// The oracle is the Go backend on this base: internal/codegen/gotable/
// stringarrays_test.go, TestGoTableStringArraysAndJSONArrayRoot — its
// array-root rows write exactly the one field's array (not an object keyed by
// that field), read that array back equal, and refuse an object in its place.
// The element here is a table rather than string(8) because the C++ text-list
// carrier ([]string(M)) is a LAND-AFTER card; the array-root rule itself is
// the same text-form fact and moves no wire byte (ir.JsonArray).
package compiler

import (
	"os/exec"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppArrayRootSchema = `package probe

table Goal
{
    x int32
}

table GoalList | json = "array"
{
    goals []Goal
}

table Holder
{
    g GoalList
}
`

// cppArrayRootProbe pins the answers the Go oracle pins for the array root:
// the text is the bare array, an object at the root is malformed, and the
// same array is the text of an array-root table held by value.
const cppArrayRootProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
using namespace probe;

static int failures = 0;
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); failures++; } } while (0)

// the walker may space its output; these values and keys carry none, so
// dropping ASCII whitespace gives the compact text the Go oracle asserts.
static void compact( char * text )
{
    char * at = text;
    for ( char * p = text; *p != 0; p++ )
    {
        if ( *p == ' ' || *p == '\n' || *p == '\r' || *p == '\t' ) { continue; }
        *at++ = *p;
    }
    *at = 0;
}

int main()
{
    CHECK( GoalListTableType()->json_array );
    CHECK( !GoalTableType()->json_array );

    GoalListBuilder b;
    GoalList * root = b.GetRoot();
    CHECK( root != NULL );
    Goal * one = GoalListGoalsAdd( b.main, root->goals );
    CHECK( one != NULL );
    one->x = 1;
    Goal * two = GoalListGoalsAdd( b.main, root->goals );
    CHECK( two != NULL );
    two->x = 2;
    CHECK( b.Lock() );

    // THE TEXT IS THE BARE ARRAY (Go: stringarrays_test.go, the array root row,
    // compact(t, gtext) == ["a","b"] with this unit's elements)
    const int64_t n = GoalListToJsonMeasure( b.AsConst() );
    CHECK( n > 0 );
    char text[ 1024 ];
    CHECK( GoalListToJson( b.AsConst(), text, (int64_t) sizeof( text ) ) == n );
    compact( text );
    CHECK( strcmp( text, "[{\"x\":1},{\"x\":2}]" ) == 0 );

    // READ THE ARRAY BACK EQUAL (Go: the same row's GoalListFromJsonTolerant)
    GoalListBuilder r;
    TableReport report;
    const char * array_text = "[{\"x\":1},{\"x\":2}]";
    CHECK( GoalListFromJson( r, array_text, (int64_t) strlen( array_text ), &report ) );
    CHECK( !report.malformed && !report.refused && report.kind_mismatch == 0 );
    CHECK( r.GetRoot()->goals.count == 2 );
    CHECK( r.Lock() );
    CHECK( r.AsConst()->goals[ 0 ].x == 1 && r.AsConst()->goals[ 1 ].x == 2 );

    // AN OBJECT AT THE ARRAY ROOT IS REFUSED (Go: json_root_not_array;
    // C++ has no Reason string, so the malformed verdict is the same answer)
    GoalListBuilder bad;
    TableReport bad_report;
    const char * object_text = "{\"goals\":[{\"x\":1}]}";
    CHECK( !GoalListFromJson( bad, object_text, (int64_t) strlen( object_text ), &bad_report ) );
    CHECK( bad_report.malformed );

    // THE ARRAY ROOT NESTED BY VALUE (Go: Holder {"g":["x","y"]})
    HolderBuilder h;
    Goal * hg = GoalListGoalsAdd( h.main, h.GetRoot()->g.goals );
    CHECK( hg != NULL );
    hg->x = 7;
    CHECK( h.Lock() );
    const int64_t hn = HolderToJsonMeasure( h.AsConst() );
    CHECK( hn > 0 );
    char htext[ 1024 ];
    CHECK( HolderToJson( h.AsConst(), htext, (int64_t) sizeof( htext ) ) == hn );
    compact( htext );
    CHECK( strcmp( htext, "{\"g\":[{\"x\":7}]}" ) == 0 );

    HolderBuilder hr;
    TableReport hr_report;
    const char * holder_text = "{\"g\":[{\"x\":7}]}";
    CHECK( HolderFromJson( hr, holder_text, (int64_t) strlen( holder_text ), &hr_report ) );
    CHECK( !hr_report.malformed && hr_report.kind_mismatch == 0 );
    CHECK( hr.GetRoot()->g.goals.count == 1 );
    CHECK( hr.Lock() );
    CHECK( hr.AsConst()->g.goals[ 0 ].x == 7 );

    // an OBJECT where the nested array root is is the ordinary kind mismatch
    HolderBuilder hm;
    TableReport hm_report;
    const char * nested_object = "{\"g\":{\"goals\":[{\"x\":7}]}}";
    CHECK( HolderFromJsonTolerant( hm, nested_object, (int64_t) strlen( nested_object ), &hm_report ) );
    CHECK( hm_report.kind_mismatch == 1 );

    return failures != 0;
}
`

func TestCPPJSONArrayRoot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	if _, err := exec.LookPath("c++"); err != nil {
		t.Skip("the array-root text form is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppArrayRootSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, cppArrayRootProbe)
}
