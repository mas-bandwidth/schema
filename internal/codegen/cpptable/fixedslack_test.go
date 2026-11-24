package cpptable

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; test/tables/FixedSlack.schema's header is the recipe): a load's
// result never depends on what the value held before. Load file A (every
// counted array at its bound, every slot 77) into a value, then file B (every
// count 1) into THE SAME value: every slot past every count holds what a fresh
// value holds there. Before the slack pass this leg left the writer's dead
// zeros in those slots (cells[1].id 0, not the declared 7).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cppFixedSlackBody = `
    static uint8_t file_a[1 << 16];
    static uint8_t file_b[1 << 16];
    static TableFixedEntry plan[4096];
    int failed = 0;
    // every slot past every count of v holds what the fresh value holds there
    auto past_counts_hold_defaults = [&failed]( const SlackRoot & v, const SlackRoot & fresh, const char * when )
    {
        for ( int i = v.cells_count; i < 3; ++i )
        {
            if ( v.cells[i].id != fresh.cells[i].id || v.cells[i].id != 7 )
            { printf( "FAIL: %s: cells[%d].id = %d, want the fresh value's 7\n", when, i, (int) v.cells[i].id ); failed = 1; }
            if ( v.cells[i].subs_count != fresh.cells[i].subs_count )
            { printf( "FAIL: %s: cells[%d].subs_count = %d, want 0\n", when, i, (int) v.cells[i].subs_count ); failed = 1; }
            for ( int j = 0; j < 2; ++j )
            {
                if ( v.cells[i].subs[j].v != fresh.cells[i].subs[j].v || v.cells[i].subs[j].v != 3 )
                { printf( "FAIL: %s: cells[%d].subs[%d].v = %d, want the fresh value's 3\n", when, i, j, (int) v.cells[i].subs[j].v ); failed = 1; }
            }
        }
        for ( int i = 0; i < v.cells_count && i < 3; ++i )
        {
            for ( int j = v.cells[i].subs_count; j < 2; ++j )
            {
                if ( v.cells[i].subs[j].v != fresh.cells[i].subs[j].v || v.cells[i].subs[j].v != 3 )
                { printf( "FAIL: %s: cells[%d].subs[%d].v = %d, want the fresh value's 3\n", when, i, j, (int) v.cells[i].subs[j].v ); failed = 1; }
            }
        }
        for ( int i = v.nums_count; i < 4; ++i )
        {
            if ( v.nums[i] != fresh.nums[i] )
            { printf( "FAIL: %s: nums[%d] = %d, want the fresh value's %d\n", when, i, (int) v.nums[i], (int) fresh.nums[i] ); failed = 1; }
        }
    };
    TableReport r;

    // 1. file A: every counted array at its bound, every slot off its default
    SlackRoot a;
    a.cells_count = 3;
    for ( int i = 0; i < 3; ++i )
    {
        a.cells[i].id = 77;
        a.cells[i].subs_count = 2;
        for ( int j = 0; j < 2; ++j ) { a.cells[i].subs[j].v = 77; }
    }
    a.nums_count = 4;
    for ( int i = 0; i < 4; ++i ) { a.nums[i] = 77; }
    const int64_t bytes_a = SlackRootFixedSave( &a, 1, file_a, (int64_t) sizeof( file_a ) );
    if ( bytes_a <= 0 ) { printf( "FAIL: file A did not save\n" ); return 1; }

    // 2. file B: every count 1, every live value 77
    SlackRoot b;
    b.cells_count = 1;
    b.cells[0].id = 77;
    b.cells[0].subs_count = 1;
    b.cells[0].subs[0].v = 77;
    b.nums_count = 1;
    b.nums[0] = 77;
    const int64_t bytes_b = SlackRootFixedSave( &b, 1, file_b, (int64_t) sizeof( file_b ) );
    if ( bytes_b <= 0 ) { printf( "FAIL: file B did not save\n" ); return 1; }

    const SlackRoot fresh = SlackRoot();

    // 3. load A into one value, then B into THE SAME value
    SlackRoot v;
    if ( SlackRootFixedLoad( &v, 1, file_a, bytes_a, plan, 4096, NULL, &r ) != 1 || r.refused ) { printf( "FAIL: file A did not load\n" ); return 1; }
    if ( v.cells_count != 3 || v.cells[2].id != 77 || v.cells[2].subs[1].v != 77 || v.nums[3] != 77 ) { printf( "FAIL: file A did not land every slot\n" ); return 1; }
    if ( SlackRootFixedLoad( &v, 1, file_b, bytes_b, plan, 4096, NULL, &r ) != 1 || r.refused ) { printf( "FAIL: file B did not load over A\n" ); return 1; }

    // 4. the live values are B's, and every slot past every count is fresh
    if ( v.cells_count != 1 || v.cells[0].id != 77 || v.cells[0].subs_count != 1 || v.cells[0].subs[0].v != 77 ) { printf( "FAIL: B's live cell\n" ); failed = 1; }
    if ( v.nums_count != 1 || v.nums[0] != 77 ) { printf( "FAIL: B's live num\n" ); failed = 1; }
    past_counts_hold_defaults( v, fresh, "lawful load of B over A" );

    // a fresh value loaded from B holds the same
    {
        SlackRoot once;
        if ( SlackRootFixedLoad( &once, 1, file_b, bytes_b, plan, 4096, NULL, &r ) != 1 ) { printf( "FAIL: file B did not load into a fresh value\n" ); failed = 1; }
        past_counts_hold_defaults( once, fresh, "lawful load of B into a fresh value" );
    }

    // the REFUSED load still resets to the declared defaults
    if ( SlackRootFixedLoad( &v, 1, file_a, bytes_a, plan, 4096, NULL, &r ) != 1 ) { printf( "FAIL: file A did not load again\n" ); failed = 1; }
    file_b[1] = 1; // a reserved header byte: the whole load is refused
    if ( SlackRootFixedLoad( &v, 1, file_b, bytes_b, plan, 4096, NULL, &r ) != -1 || !( r.refused || r.damaged ) ) { printf( "FAIL: a damaged B did not refuse (refused=%d damaged=%d)\n", (int) r.refused, (int) r.damaged ); failed = 1; }
    if ( v.cells_count != 0 || v.nums_count != 0 ) { printf( "FAIL: the refused load left a count off 0\n" ); failed = 1; }
    past_counts_hold_defaults( v, fresh, "refused load of B over A" );
    if ( failed ) { return 1; }
`

// TestCppFixedSlackPastCountHoldsDefaults runs the recipe through the
// generated C++ fixed load (FixedSlack.schema).
func TestCppFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	schema := filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema")
	out, err := cppRunUnitFlags(t, schema, "tblslack", cppFixedSlackBody, nil, []string{"-Wall", "-Wextra", "-Wshadow", "-Werror"})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestCppFixedSlackEmitted pins the emitter without a toolchain: FixedSlack's
// element defaults are not zero, so its load carries the slack pass; a unit
// whose counted arrays all reset to zero bytes carries none (§2.2's zero-cost
// rule: the generated code is what it was before the pass existed).
func TestCppFixedSlackEmitted(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	src := cppFixedSlackJoined(generateCPPAll(t, string(data)))
	for _, want := range []string{
		"TABLE_FIXED_INLINE void SlackCellFixedSlack( SlackCell & value )",
		"TABLE_FIXED_INLINE void SlackRootFixedSlack( SlackRoot & value )",
		"for ( int64_t i = (int64_t) value.subs_count; i >= 0 && i < 2; ++i )",
		"SlackSubReset( value.subs[i] );",
		"for ( int64_t i = (int64_t) value.cells_count; i >= 0 && i < 3; ++i )",
		"SlackCellReset( value.cells[i] );",
		"SlackCellFixedSlack( value.cells[i] );",
		"SlackRootFixedSlack( values[k] );",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("FixedSlack's C++ does not carry the slack pass: missing %q", want)
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
	if got := cppFixedSlackJoined(generateCPPAll(t, zero)); strings.Contains(got, "FixedSlack") {
		t.Errorf("a unit whose counted arrays all reset to zero bytes carries a slack pass")
	}
}

// cppFixedSlackJoined is every generated file's text in one string, for a pin
// that does not care which file a symbol lands in.
func cppFixedSlackJoined(files map[string][]byte) string {
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}
