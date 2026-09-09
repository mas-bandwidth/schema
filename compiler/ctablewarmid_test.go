// THE WARM SCALAR-LEAF MEASURE IN C, COUNTED AT BOTH BRANCHES
// (docs/SPEC-TABLES.md §3).
//
// A plain scalar leaf's measure has two ways to count the same body. The COLD
// loop walks the riding fields, interning each field id through
// table_writer_id_at and adding the kind and payload bytes. The WARM branch
// counts the riding references — one byte each — with their payloads and
// advances the measuring writer once, and it may only run when EVERY field id
// of the leaf is already interned in this writer's vocabulary, the unit's
// vocabulary is small enough that a reference is one byte, and the whole body
// fits the remaining capacity.
//
// THE TWO BRANCHES MUST PRODUCE THE SAME NUMBER, and no byte the emitter
// writes says which one ran — that is the point of the change and also the
// reason a byte pin cannot hold it. So this fixture COUNTS THE ENTRIES. The
// generated header is compiled with a counter bumped at the head of each
// branch, and each case names which branch every leaf body must take:
//
//   - ALL FIELDS INTERNED takes the warm branch. Root's `second` Leaf is
//     measured after `first` has interned a, b and c, and rows[1..] after
//     rows[0] has interned p and q;
//   - A FIELD NOT YET INTERNED takes the cold loop, whatever else is warm.
//     The same file with Leaf's c and Row's q left at their defaults in every
//     body — an elided field interns nothing — leaves slot[c] and slot[q]
//     empty, and all five bodies must fall through to the cold walk.
//
// Both shapes ride here on purpose: Leaf is a scalar TABLE FIELD, so it is a
// default-probe target and its measure opens on `!w->check_default`; Row is
// only ever an ARRAY ELEMENT, which no probe reaches, so after #810 its
// measure opens on the buffer alone. The warm branch hangs off both.
//
// The instrumentation is injected into the generated text inside this test and
// never touches a tracked file; the injection is checked to have applied, so a
// counter that stopped matching the emitter fails loudly instead of reporting
// zero.
package compiler

import (
	"fmt"
	"strings"
	"testing"
)

const cWarmIdSchema = `package warmid

// a scalar leaf reached as a scalar table field: a default probe target
table Leaf
{
    a int32
    b float32
    c uint8
}

// a scalar leaf reached only as an array element: no probe can set check_default
table Row
{
    p int32
    q int32
}

table Root
{
    first  Leaf
    second Leaf
    rows   [..8]Row
    n      int32
}
`

// The head of each branch of emitWireScalarLeafMeasure, in
// internal/codegen/ctable/wire.go.
const (
	cWarmIdWarmHead = "            int64_t body_bytes = 1;\n"
	cWarmIdColdHead = "        int64_t payload_bytes = 1; /* the zero reference ending this body */\n"
)

// cWarmIdInstrument bumps a counter at the head of each measure branch. It
// answers the patched files and the number of leaf bodies the emitter wrote a
// warm branch for, so a header that grew no warm branch at all cannot be read
// as a leaf that declined to take one.
func cWarmIdInstrument(t *testing.T, files map[string][]byte) (map[string][]byte, int) {
	t.Helper()
	out := map[string][]byte{}
	warmSites, coldSites := 0, 0
	for name, data := range files {
		text := string(data)
		if strings.HasSuffix(name, ".h") {
			warmSites += strings.Count(text, cWarmIdWarmHead)
			coldSites += strings.Count(text, cWarmIdColdHead)
			text = strings.ReplaceAll(text, cWarmIdWarmHead, cWarmIdWarmHead+"            schema_warm_entries++;\n")
			text = strings.ReplaceAll(text, cWarmIdColdHead, cWarmIdColdHead+"        schema_cold_entries++;\n")
			text = "extern long schema_warm_entries;\nextern long schema_cold_entries;\n" + text
		}
		out[name] = []byte(text)
	}
	if warmSites == 0 {
		t.Fatal("the generated C carries no warm scalar-leaf branch: this fixture would count nothing")
	}
	if coldSites == 0 {
		t.Fatal("the generated C carries no cold scalar-leaf walk: this fixture would count nothing")
	}
	return out, warmSites
}

func cWarmIdDriver() string {
	return `#include <stdio.h>
#include <string.h>
#include <stdint.h>

long schema_warm_entries = 0;
long schema_cold_entries = 0;

#include "ProbeTable.h"

static uint8_t buffer[ 8192 ];
static uint8_t twin[ 8192 ];
static Root value;
static Root back;

/* interned = 1: every leaf field rides, so the first body of each shape
   interns every id that shape names and the bodies after it find them all.
   interned = 0: ONE field of each shape -- Leaf's c, Row's q -- stays at its
   default in every body. An elided field interns nothing, so slot[c] and
   slot[q] are never filled and no body of either shape can go warm, however
   many bodies the file carries. */
static void build( Root * v, int interned )
{
    int32_t i;

    root_reset( v );

    v->first.a = 1;
    v->first.b = 2.5f;
    v->first.c = interned ? 3 : 0;   /* 0 is c's default: the field elides */

    v->second.a = 4;
    v->second.b = 5.5f;
    v->second.c = interned ? 6 : 0;

    v->rows_count = 3;
    for ( i = 0; i < v->rows_count; i++ )
    {
        v->rows[ i ].p = 7 + i;
        v->rows[ i ].q = interned ? 20 + i : 0;  /* 0 is q's default */
    }

    v->n = 13;
}

/* one measure, with the counters zeroed around it */
static int64_t counted_measure( const Root * v, long * warm, long * cold )
{
    int64_t measured;
    schema_warm_entries = 0;
    schema_cold_entries = 0;
    measured = root_measure( v );
    *warm = schema_warm_entries;
    *cold = schema_cold_entries;
    return measured;
}

static int run( int interned )
{
    long warm = 0, cold = 0;
    int64_t measured, wrote, again;
    TableReport report;

    build( &value, interned );
    measured = counted_measure( &value, &warm, &cold );
    printf( "interned=%d measure=%lld warm=%ld cold=%ld\n",
            interned, (long long) measured, warm, cold );

    /* whichever branch counted the body, the file has to agree with it */
    wrote = root_save( &value, buffer, (int64_t) sizeof( buffer ) );
    if ( wrote < 0 || wrote != measured )
    {
        printf( "MEASURE/SAVE DISAGREE save %lld measure %lld\n",
                (long long) wrote, (long long) measured );
        return 1;
    }
    memset( &report, 0, sizeof( report ) );
    if ( !root_load( &back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED\n" );
        return 1;
    }
    if ( back.second.a != 4 || back.second.b != 5.5f || back.rows_count != 3 ||
         back.rows[ 2 ].p != 9 || back.n != 13 ||
         back.first.c != value.first.c || back.rows[ 2 ].q != value.rows[ 2 ].q )
    {
        printf( "VALUES MOVED\n" );
        return 1;
    }
    again = root_save( &back, twin, (int64_t) sizeof( twin ) );
    if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
    {
        printf( "ROUND TRIP MOVED\n" );
        return 1;
    }

    /* AND THE SHORT BUFFER: the warm branch's room guard must hand a body one
       byte short of its own maximum back to the cold walk, which is the path
       that raises overflow. A save into a buffer that cannot hold the file
       must answer -1, not a short file. */
    if ( root_save( &value, buffer, wrote - 1 ) != -1 )
    {
        printf( "SHORT BUFFER ACCEPTED\n" );
        return 1;
    }
    return 0;
}

int main( void )
{
    if ( run( 1 ) != 0 ) { return 1; }
    if ( run( 0 ) != 0 ) { return 1; }
    printf( "OK\n" );
    return 0;
}
`
}

// TestCTableWarmIdBranches holds the warm scalar-leaf measure to the two
// conditions it claims: every field id interned takes it, and one id missing
// sends the same body back to the cold walk.
func TestCTableWarmIdBranches(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, cWarmIdSchema), "c", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	header := string(files["ProbeTable.h"])
	// the two openings this fixture rides on, one per probe reachability
	for _, want := range []string{
		"    if ( w->buffer == NULL && !w->check_default )\n",
		"    if ( w->buffer == NULL )\n",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the generated C no longer opens a scalar leaf measure with %q: this fixture would not cover both shapes", strings.TrimSpace(want))
		}
	}
	patched, warmSites := cWarmIdInstrument(t, files)
	out := runCRefOrdinal(t, patched, cWarmIdDriver(), "cwarmid",
		"the counted measure refused its own value")

	want := []string{
		// Leaf: `first` interns a, b, c cold, `second` finds all three warm.
		// Row: rows[0] interns p and q cold, rows[1] and rows[2] go warm.
		"interned=1 measure=" + cWarmIdMeasure(out, 1) + " warm=3 cold=2",
		// Leaf's c and Row's q elide in every body, so slot[c] and slot[q]
		// are never filled and all five bodies fall through to the cold walk.
		"interned=0 measure=" + cWarmIdMeasure(out, 0) + " warm=0 cold=5",
	}
	for _, line := range want {
		if !strings.Contains(out, line+"\n") {
			t.Fatalf("THE MEASURE TOOK THE WRONG BRANCH\n  want: %s\n   got:\n%s", line, out)
		}
	}
	if !strings.Contains(out, "OK\n") {
		t.Fatalf("the driver did not finish:\n%s", out)
	}
	t.Logf("%d warm branches emitted; counts: %s", warmSites, strings.TrimSpace(out))
}

// cWarmIdMeasure lifts the measured length the driver printed for one case.
// The LENGTH is not what this test pins — compiler/ctablerefordinal_test.go
// and the goldens hold the bytes — the BRANCH COUNTS beside it are.
func cWarmIdMeasure(out string, interned int) string {
	prefix := fmt.Sprintf("interned=%d measure=", interned)
	for line := range strings.SplitSeq(out, "\n") {
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		if measure, _, found := strings.Cut(rest, " "); found {
			return measure
		}
	}
	return "?"
}
