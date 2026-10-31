package cpptable

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// discardUnit has a fixed root and a variable root, each carrying a nested
// body between two scalars, so damage inside the nested body leaves the walk
// able to read on past its L: the case where a reader that kept what it
// decoded would hand back a value.
const discardUnit = `package discard

fixed table Inner
{
    n int32 = 0
}

fixed table Root
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
}

table Graph
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

// discardWire encodes the text as the root and then damages the nested body:
// Inner's one field, kind 4 holding 3, gets the kind 37 no reader can skip.
// The engine is asked first, so the vector is damaged by the rule and not by
// assumption.
func discardWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return wire
}

func cppBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

// TestDamageDiscardsTheWholeLoadCPP is the C++ reference's gate for DAMAGE
// DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): damage inside a nested
// body, with a whole field before it and after it, makes LoadTolerant answer
// false with the value at its declared defaults, and makes the region Load
// hand back no root. Red if either read hands back a field it decoded.
func TestDamageDiscardsTheWholeLoadCPP(t *testing.T) {
	t.Parallel()
	u := loadUnitFromSource(t, discardUnit)
	fixed := discardWire(t, u, "Root", `{"a":5,"inner":{"n":3},"b":9}`)
	variable := discardWire(t, u, "Graph", `{"a":5,"inner":{"n":3},"b":9}`)
	body := fmt.Sprintf(`
    const uint8_t fixed[] = { %s };
    Root v; RootReset( v );
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( RootLoadTolerant( v, fixed, (int64_t) sizeof( fixed ), &r ) ) { printf( "fixed: LoadTolerant answered true on damage\n" ); return 1; }
    if ( !r.malformed ) { printf( "fixed: the report does not say malformed\n" ); return 1; }
    if ( v.a != 0 || v.b != 0 || v.inner.n != 0 ) { printf( "fixed: a damaged load handed back a=%%d b=%%d n=%%d\n", (int) v.a, (int) v.b, (int) v.inner.n ); return 1; }
    Root strict; RootReset( strict );
    TableReport s; memset( &s, 0, sizeof( s ) );
    if ( RootLoad( strict, fixed, (int64_t) sizeof( fixed ), &s ) || !s.malformed ) { printf( "fixed: the strict Load did not refuse\n" ); return 1; }

    const uint8_t variable[] = { %s };
    const int64_t need = GraphLoadMeasure( variable, (int64_t) sizeof( variable ) );
    std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
    uint8_t * aligned = region.data();
    while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
    TableReport g; memset( &g, 0, sizeof( g ) );
    const Graph * root = GraphLoadTolerant( aligned, need > 0 ? need : 4096, variable, (int64_t) sizeof( variable ), &g );
    if ( root != NULL ) { printf( "variable: LoadTolerant handed back a root on damage: a=%%d b=%%d\n", (int) root->a, (int) root->b ); return 1; }
    if ( need >= 0 && !g.malformed ) { printf( "variable: the report does not say malformed\n" ); return 1; }
`, cppBytes(fixed), cppBytes(variable))
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
