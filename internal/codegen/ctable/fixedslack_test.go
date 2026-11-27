package ctable

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; test/tables/FixedSlack.schema's header is the recipe): a load's
// result never depends on what the value held before. Load file A (every
// counted array at its bound, every slot 77) into a value, then file B (every
// count 1) into THE SAME value: every slot past every count holds what a fresh
// value holds there. Before the slack pass this leg left the writer's dead
// zeros in those slots (cells[1].id 0, not the declared 7).

import (
	"strings"
	"testing"
)

const cFixedSlackProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>

static int failed = 0;
#define EXPECT( cond, what ) do { if ( !( cond ) ) { fprintf( stderr, "FAIL: %s\n", what ); failed = 1; } } while ( 0 )

static uint8_t file_a[1 << 16];
static uint8_t file_b[1 << 16];
static TableFixedEntry plan[4096];

/* every slot past every count of v holds what the fresh value holds there */
static void past_counts_hold_defaults( const SlackRoot * v, const SlackRoot * fresh, const char * when )
{
    int i, j;
    for ( i = v->cells_count; i < 3; ++i )
    {
        if ( v->cells[i].id != fresh->cells[i].id || v->cells[i].id != 7 )
        { fprintf( stderr, "FAIL: %s: cells[%d].id = %d, want the fresh value's 7\n", when, i, (int) v->cells[i].id ); failed = 1; }
        if ( v->cells[i].subs_count != fresh->cells[i].subs_count )
        { fprintf( stderr, "FAIL: %s: cells[%d].subs_count = %d, want 0\n", when, i, (int) v->cells[i].subs_count ); failed = 1; }
        for ( j = 0; j < 2; ++j )
        {
            if ( v->cells[i].subs[j].v != fresh->cells[i].subs[j].v || v->cells[i].subs[j].v != 3 )
            { fprintf( stderr, "FAIL: %s: cells[%d].subs[%d].v = %d, want the fresh value's 3\n", when, i, j, (int) v->cells[i].subs[j].v ); failed = 1; }
        }
    }
    for ( i = 0; i < v->cells_count && i < 3; ++i )
    {
        for ( j = v->cells[i].subs_count; j < 2; ++j )
        {
            if ( v->cells[i].subs[j].v != fresh->cells[i].subs[j].v || v->cells[i].subs[j].v != 3 )
            { fprintf( stderr, "FAIL: %s: cells[%d].subs[%d].v = %d, want the fresh value's 3\n", when, i, j, (int) v->cells[i].subs[j].v ); failed = 1; }
        }
    }
    for ( i = v->nums_count; i < 4; ++i )
    {
        if ( v->nums[i] != fresh->nums[i] )
        { fprintf( stderr, "FAIL: %s: nums[%d] = %d, want the fresh value's %d\n", when, i, (int) v->nums[i], (int) fresh->nums[i] ); failed = 1; }
    }
}

int main( void )
{
    SlackRoot a, b, v, fresh;
    TableReport r;
    int64_t bytes_a, bytes_b;
    int i, j;

    /* 1. file A: every counted array at its bound, every slot off its default */
    slack_root_reset( &a );
    a.cells_count = 3;
    for ( i = 0; i < 3; ++i )
    {
        a.cells[i].id = 77;
        a.cells[i].subs_count = 2;
        for ( j = 0; j < 2; ++j ) { a.cells[i].subs[j].v = 77; }
    }
    a.nums_count = 4;
    for ( i = 0; i < 4; ++i ) { a.nums[i] = 77; }
    bytes_a = slack_root_fixed_save( &a, 1, file_a, (int64_t) sizeof( file_a ) );
    EXPECT( bytes_a > 0, "file A saves" );

    /* 2. file B: every count 1, every live value 77 */
    slack_root_reset( &b );
    b.cells_count = 1;
    b.cells[0].id = 77;
    b.cells[0].subs_count = 1;
    b.cells[0].subs[0].v = 77;
    b.nums_count = 1;
    b.nums[0] = 77;
    bytes_b = slack_root_fixed_save( &b, 1, file_b, (int64_t) sizeof( file_b ) );
    EXPECT( bytes_b > 0, "file B saves" );

    slack_root_reset( &fresh );

    /* 3. load A into one value, then B into THE SAME value */
    slack_root_reset( &v );
    EXPECT( slack_root_fixed_load( &v, 1, file_a, bytes_a, plan, 4096, NULL, &r ) == 1 && !r.refused, "file A loads" );
    EXPECT( v.cells_count == 3 && v.cells[2].id == 77 && v.cells[2].subs[1].v == 77 && v.nums[3] == 77, "file A lands every slot" );
    EXPECT( slack_root_fixed_load( &v, 1, file_b, bytes_b, plan, 4096, NULL, &r ) == 1 && !r.refused, "file B loads over A" );

    /* 4. the live values are B's, and every slot past every count is fresh */
    EXPECT( v.cells_count == 1 && v.cells[0].id == 77 && v.cells[0].subs_count == 1 && v.cells[0].subs[0].v == 77, "B's live cell" );
    EXPECT( v.nums_count == 1 && v.nums[0] == 77, "B's live num" );
    past_counts_hold_defaults( &v, &fresh, "lawful load of B over A" );

    /* a fresh value loaded from B holds the same */
    {
        SlackRoot once;
        slack_root_reset( &once );
        EXPECT( slack_root_fixed_load( &once, 1, file_b, bytes_b, plan, 4096, NULL, &r ) == 1, "file B loads into a fresh value" );
        past_counts_hold_defaults( &once, &fresh, "lawful load of B into a fresh value" );
    }

    /* the REFUSED load still resets to the declared defaults */
    EXPECT( slack_root_fixed_load( &v, 1, file_a, bytes_a, plan, 4096, NULL, &r ) == 1, "file A loads again" );
    file_b[1] = 1; /* a reserved header byte: the whole load is refused */
    EXPECT( slack_root_fixed_load( &v, 1, file_b, bytes_b, plan, 4096, NULL, &r ) == -1 && r.refused, "a damaged B refuses" );
    EXPECT( v.cells_count == 0 && v.nums_count == 0, "the refused load leaves every count at 0" );
    past_counts_hold_defaults( &v, &fresh, "refused load of B over A" );

    return failed;
}
`

// TestCFixedSlackPastCountHoldsDefaults runs the recipe through the generated
// C fixed load (FixedSlack.schema).
func TestCFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	runCHostileResidual(t, cReadSchema(t, "FixedSlack"), cFixedSlackProbe)
}

// TestCFixedSlackEmitted pins the emitter without a toolchain: FixedSlack's
// element defaults are not zero, so its load carries the slack pass; a unit
// whose counted arrays all reset to zero bytes carries none (§2.2's zero-cost
// rule: the generated code is what it was before the pass existed).
func TestCFixedSlackEmitted(t *testing.T) {
	t.Parallel()
	src := string(joinGenerated(generate(t, cReadSchema(t, "FixedSlack"))))
	for _, want := range []string{
		"static SCHEMA_UNUSED void schema_tblslack_slack_cell_fixed_slack_( SlackCell * value )",
		"static SCHEMA_UNUSED void schema_tblslack_slack_root_fixed_slack_( SlackRoot * value )",
		"slack_sub_reset( &value->subs[dead] );",
		"slack_cell_reset( &value->cells[dead] );",
		"schema_tblslack_slack_cell_fixed_slack_( &value->cells[i] );",
		"schema_tblslack_slack_root_fixed_slack_( values + k );",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("FixedSlack's C does not carry the slack pass: missing %q", want)
		}
	}
	zero := `package tblzero

fixed table ZeroSub
{
    v int16
    f float32
}

fixed table ZeroCell
{
    id   int32
    subs [..2]ZeroSub
    tags [3]ZeroSub
}

fixed table ZeroRoot
{
    cells [..3]ZeroCell
    nums  [..4]int32
}
`
	if got := string(joinGenerated(generate(t, zero))); strings.Contains(got, "fixed_slack") {
		t.Errorf("a unit whose counted arrays all reset to zero bytes carries a slack pass")
	}
}

// joinGenerated is every generated file's text in one string, for a pin that
// does not care which file a symbol lands in.
func joinGenerated(files map[string][]byte) []byte {
	var out []byte
	for _, data := range files {
		out = append(out, data...)
	}
	return out
}
