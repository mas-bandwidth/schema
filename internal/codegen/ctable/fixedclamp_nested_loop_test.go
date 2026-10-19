package ctable

// fixedclamp_nested_loop — a clamp body's element loops NEST when an array arm
// rides inside an array of unions, exactly as the write body's do. Each level
// takes its own counter (fixedLoopVar: `i`, then `i1`, ...), so the inner loop
// never shadows the outer `i` (the C leg builds with -Wshadow) and the element
// it clamps is the outer element the outer counter names, never `[i]` twice.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const fixedClampNestedSchema = `package probe

enum Grade
{
    First
    Second
}

union Choice
{
    grades [3]Grade
    many [..2]Grade
    signal
}

fixed table Root
{
    pending [2]Choice
    history [..2]Choice
}
`

// fixedClampBody is the text of one type's emitted clamp body function.
func fixedClampBody(t *testing.T, header, snake string) string {
	t.Helper()
	start := strings.Index(header, "_"+snake+"_fixed_clamp_body_( Root * value")
	if start < 0 {
		t.Fatalf("no %s_fixed_clamp_body in the header:\n%s", snake, header)
	}
	end := strings.Index(header[start:], "\n}\n")
	if end < 0 {
		t.Fatal("the clamp body does not close")
	}
	return header[start : start+end]
}

func TestFixedClampNestedLoopsTakeTheirOwnCounter(t *testing.T) {
	t.Parallel()

	body := fixedClampBody(t, cT08Header(t, fixedClampNestedSchema), "root")
	for _, want := range []string{
		"int64_t i;",
		"int64_t i1;",
		"for ( i1 = 0; i1 < 3; ++i1 )",
		"value->pending[i].as.grades[i1]",
		"i1 < (int64_t) value->pending[i].as.many.value_count",
		"value->pending[i].as.many.value[i1]",
		"value->history[i].as.grades[i1]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the nested clamp loop lacks %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"grades[i]", "many.value[i]", "int64_t i2;"} {
		if strings.Contains(body, bad) {
			t.Errorf("the nested clamp loop carries %q:\n%s", bad, body)
		}
	}
}

func TestFixedClampNestedLoopsTouchOnlyTheirOwnElement(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (the leg's own toolchain)")

	// pending[0] holds a grades arm whose elements are all in range; pending[1]
	// holds the empty signal arm, with a stale 7 left in the union's storage
	// where a grades element would sit. Nothing may be clamped: the signal arm
	// has no payload. A shadowed counter fails -Wshadow, and past it the inner
	// loop of pending[0] indexes pending[1] and clamps that stale byte.
	cT08Probe(t, fixedClampNestedSchema, "Root", `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

int main(void)
{
    Root v;
    int32_t clamped = 0, damaged = 0;
    memset( &v, 0, sizeof( v ) );
    v.pending[0].type = CHOICE_TYPE_GRADES;
    v.pending[0].as.grades[0] = GRADE_SECOND;
    v.pending[0].as.grades[1] = GRADE_SECOND;
    v.pending[0].as.grades[2] = GRADE_SECOND;
    v.pending[1].type = CHOICE_TYPE_SIGNAL;
    v.pending[1].as.grades[1] = 7;
    schema_probe_root_fixed_clamp_body_( &v, &clamped, &damaged );
    if ( clamped != 0 || damaged != 0 ) { printf( "clamped=%d damaged=%d, want 0 and 0\n", (int) clamped, (int) damaged ); return 1; }
    if ( v.pending[0].as.grades[1] != GRADE_SECOND ) { printf( "an in-range element moved\n" ); return 1; }
    if ( v.pending[1].as.grades[1] != 7 ) { printf( "the signal arm's storage was clamped through another element's loop\n" ); return 1; }
    return 0;
}
`)
}
