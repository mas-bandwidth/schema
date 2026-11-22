package ctable

// TestCCountedMinimumBornBytesMatchTheEngine holds two R10 rules on the C leg
// against the independent engine's bytes (internal/tablewire), the same
// bytes in every language, on the table wire and in the message form:
//
//   - A counted [A..B] array of TABLE or UNION elements is at its BORN state
//     when the live count is A and every element writes nothing (a table
//     element whose body is the terminator alone, a union element that is
//     None); the writer elides it exactly then (docs/SPEC-TABLES.md §2.3,
//     §4.6), as the Go oracle's emitCountedBirthRides does. One over the
//     minimum, and one element set, ride.
//   - Selecting a counted [A..B] union ARM establishes it at count A: a reset
//     value whose tag is then set to the arm holds A default elements.
//
// A FUNCTIONAL TEST: it compiles and runs C. No clock, no socket, no network.

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cCountedMinElemsSchema = `package countedmin

fixed table Elem
{
    n uint32
}

union Pick
{
    a uint32
    b int32
}

fixed table Born
{
    es [2..4]Elem
}

fixed table BornU
{
    us [2..4]Pick
}

union MinPick
{
    few [2..4]uint32
    other int32
}

fixed table Holder
{
    pick MinPick
}
`

// one case: the root, the engine's text for the value, and the C lines that
// build the same value in `v` from a reset one
type cCountedMinCase struct {
	id, table, text, build string
}

var cCountedMinCases = []cCountedMinCase{
	{"table_born", "Born", `{"es":[{},{}]}`, ``},
	{"table_three", "Born", `{"es":[{},{},{}]}`, `v.es_count = 3;`},
	{"table_one_set", "Born", `{"es":[{},{"n":7}]}`, `v.es[1].n = 7;`},
	{"union_born", "BornU", `{"us":[{},{}]}`, ``},
	{"union_three", "BornU", `{"us":[{},{},{}]}`, `v.us_count = 3;`},
	{"union_one_set", "BornU", `{"us":[{},{"a":7}]}`, `v.us[1].type = PICK_TYPE_A; v.us[1].as.a = 7;`},
	{"arm_select", "Holder", `{"pick":{"few":[0,0]}}`, `v.pick.type = MIN_PICK_TYPE_FEW;`},
}

func cCountedMinProbe() string {
	var b strings.Builder
	b.WriteString(`#include <stdio.h>
#include <string.h>
#include "HEADER"
SHAPE
static void hexout( const char * form, const char * id, const uint8_t * p, int64_t n )
{
    int64_t i;
    printf( "%s %s ", form, id );
    if ( n <= 0 ) { printf( "refused\n" ); return; }
    for ( i = 0; i < n; ++i ) { printf( "%02x", (unsigned) p[i] ); }
    printf( "\n" );
}

int main( void )
{
    static uint8_t out[4096];
    TableReport r;
`)
	for _, c := range cCountedMinCases {
		s := cSnake(c.table)
		fmt.Fprintf(&b, `    {
        %[2]s v;
        %[3]s_reset( &v );
        %[4]s
        hexout( "wire", "%[1]s", out, %[3]s_save( &v, out, (int64_t) sizeof( out ) ) );
        memset( &r, 0, sizeof( r ) );
        hexout( "message", "%[1]s", out, %[3]s_save_messages( &v, 1, out, (int64_t) sizeof( out ), &r ) );
    }
`, c.id, c.table, s, c.build)
	}
	b.WriteString("    return 0;\n}\n")
	return b.String()
}

// cCountedMinEngine is the engine's bytes for each case and form.
func cCountedMinEngine(t *testing.T) map[string]string {
	t.Helper()
	m := tabletext.NewModel(unitFrom(t, cCountedMinElemsSchema))
	want := map[string]string{}
	for _, c := range cCountedMinCases {
		v := m.New(m.Lookup(c.table))
		var rep tabletext.Report
		if !m.Read(v, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine did not read %s: %+v", c.id, c.text, rep)
		}
		wire, err := tablewire.EncodeVariable(m, v) // the table wire; Encode picks the fixed form for a fixed table
		if err != nil {
			t.Fatalf("%s: the engine did not encode: %v", c.id, err)
		}
		msg, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v})
		if err != nil {
			t.Fatalf("%s: the engine did not encode the message: %v", c.id, err)
		}
		want["wire "+c.id] = hex.EncodeToString(wire)
		want["message "+c.id] = hex.EncodeToString(msg)
	}
	return want
}

func TestCCountedMinimumBornBytesMatchTheEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "functional test: invokes the C compiler (cc) and runs compiled C probes")
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	want := cCountedMinEngine(t)
	// a debug build: the write asserts stay in, so a selected arm left at
	// count 0 stops the probe rather than writing
	probe := cArmMinBuild(t, cCountedMinElemsSchema, "", cCountedMinProbe(), "born")
	out, err := testproc.CommandContext(t, ctx, probe).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("an unexpected probe line: %q", line)
		}
		got[parts[0]+" "+parts[1]] = parts[2]
	}
	for _, c := range cCountedMinCases {
		for _, form := range []string{"wire", "message"} {
			key := form + " " + c.id
			if got[key] != want[key] {
				t.Errorf("%s: the C bytes differ from the engine's\n got %s\nwant %s", key, got[key], want[key])
			}
		}
	}
}
