// THE ARRAY ROOT IN THE C TEXT FORM (docs/SPEC-TABLES.md §16.10).
//
// The oracle is the Go backend on this base: internal/codegen/gotable/
// stringarrays_test.go, TestGoTableStringArraysAndJSONArrayRoot — its
// array-root rows write exactly the one field's array (not an object keyed by
// that field), read that array back equal, and refuse an object in its place.
package ctable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cArrayRootSchema = `package probe

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

const cArrayRootProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>

static int failures = 0;
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); failures++; } } while (0)

/* JSON writers return counted bytes, without a trailing NUL. */
static void compact( char * text, int64_t length )
{
    char * at = text;
    char * p;
    for ( p = text; p < text + length; p++ )
    {
        if ( *p == ' ' || *p == '\n' || *p == '\r' || *p == '\t' ) { continue; }
        *at++ = *p;
    }
    *at = 0;
}

int main( void )
{
    GoalListBuilder b;
    Goal * one;
    Goal * two;
    int64_t n;
    char text[ 1024 ];
    GoalListBuilder r;
    TableReport report;
    const char * array_text = "[{\"x\":1},{\"x\":2}]";
    GoalListBuilder bad;
    TableReport bad_report;
    const char * object_text = "{\"goals\":[{\"x\":1}]}";
    HolderBuilder h;
    Goal * hg;
    int64_t hn;
    char htext[ 1024 ];
    HolderBuilder hr;
    TableReport hr_report;
    const char * holder_text = "{\"g\":[{\"x\":7}]}";
    HolderBuilder hm;
    TableReport hm_report;
    const char * nested_object = "{\"g\":{\"goals\":[{\"x\":7}]}}";

    CHECK( goal_list_table_type()->json_array );
    CHECK( !goal_table_type()->json_array );

    CHECK( goal_list_builder_init( &b ) );
    one = goal_list_goals_add( &b.main, &goal_list_builder_root( &b )->goals );
    CHECK( one != NULL );
    one->x = 1;
    two = goal_list_goals_add( &b.main, &goal_list_builder_root( &b )->goals );
    CHECK( two != NULL );
    two->x = 2;
    CHECK( goal_list_builder_lock( &b ) );

    n = goal_list_to_json_measure( (const GoalList *)(const void *)b.region );
    CHECK( n > 0 && n < (int64_t) sizeof( text ) );
    if ( n <= 0 || n >= (int64_t) sizeof( text ) ) { return 1; }
    memset( text, '!', sizeof( text ) );
    CHECK( goal_list_to_json( (const GoalList *)(const void *)b.region, text, (int64_t) sizeof( text ) ) == n );
    compact( text, n );
    CHECK( strcmp( text, "[{\"x\":1},{\"x\":2}]" ) == 0 );
    goal_list_builder_shutdown( &b );

    CHECK( goal_list_builder_init( &r ) );
    CHECK( goal_list_from_json( &r, array_text, (int64_t) strlen( array_text ), &report ) );
    CHECK( !report.refused && report.kind_mismatch == 0 );
    CHECK( goal_list_builder_root( &r )->goals.count == 2 );
    CHECK( goal_list_builder_lock( &r ) );
    CHECK( goal_list_goals_at( &( (const GoalList *)(const void *)r.region )->goals, 0 )->x == 1 );
    CHECK( goal_list_goals_at( &( (const GoalList *)(const void *)r.region )->goals, 1 )->x == 2 );
    goal_list_builder_shutdown( &r );

    CHECK( goal_list_builder_init( &bad ) );
    CHECK( !goal_list_from_json( &bad, object_text, (int64_t) strlen( object_text ), &bad_report ) );
    CHECK( bad_report.refused );
    goal_list_builder_shutdown( &bad );

    CHECK( holder_builder_init( &h ) );
    hg = goal_list_goals_add( &h.main, &holder_builder_root( &h )->g.goals );
    CHECK( hg != NULL );
    hg->x = 7;
    CHECK( holder_builder_lock( &h ) );
    hn = holder_to_json_measure( (const Holder *)(const void *)h.region );
    CHECK( hn > 0 && hn < (int64_t) sizeof( htext ) );
    if ( hn <= 0 || hn >= (int64_t) sizeof( htext ) ) { return 1; }
    memset( htext, '!', sizeof( htext ) );
    CHECK( holder_to_json( (const Holder *)(const void *)h.region, htext, (int64_t) sizeof( htext ) ) == hn );
    compact( htext, hn );
    CHECK( strcmp( htext, "{\"g\":[{\"x\":7}]}" ) == 0 );
    holder_builder_shutdown( &h );

    CHECK( holder_builder_init( &hr ) );
    CHECK( holder_from_json( &hr, holder_text, (int64_t) strlen( holder_text ), &hr_report ) );
    CHECK( hr_report.kind_mismatch == 0 );
    CHECK( holder_builder_root( &hr )->g.goals.count == 1 );
    CHECK( holder_builder_lock( &hr ) );
    CHECK( goal_list_goals_at( &( (const Holder *)(const void *)hr.region )->g.goals, 0 )->x == 7 );
    holder_builder_shutdown( &hr );

    CHECK( holder_builder_init( &hm ) );
    CHECK( holder_from_json( &hm, nested_object, (int64_t) strlen( nested_object ), &hm_report ) );
    CHECK( hm_report.kind_mismatch == 1 );
    holder_builder_shutdown( &hm );

    return failures != 0;
}
`

func TestCJSONArrayRoot(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler on PATH")
	}
	u := unitFrom(t, cArrayRootSchema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(cArrayRootProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".c") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
}
