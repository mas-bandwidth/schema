package ctable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCVariableTestsStrictApi closes the STRICT-100 gap for the C
// variable-table tests (docs/plans/strict-only.md): the compiler's C
// variable-table tests and the generated variable read surface use only
// the one strict read. The plain names return the whole value or refuse
// the whole load; there is no retired partial-read entry point and no
// retired report field in either place.
//
// The scoped test files are the compiler tests that drive a variable
// (region, builder, retaining or graph) C read: tablescwire (graph,
// identity, lists, maps and widening wires), tablesccook (builder load
// into a cook), tablescretain (the retaining region read),
// tablescpointerarm (region and retaining reads), tablescwireblobshared
// (region and builder reads) and tablescstrictload (region and builder
// reads). Fixed-form and message-form tests are other cells and are not
// this gate's scope.
var vtStrictScopedFiles = []string{
	"tablescwire_test.go",
	"tablesccook_test.go",
	"tablescretain_test.go",
	"tablescpointerarm_test.go",
	"tablescwireblobshared_test.go",
	"tablescstrictload_test.go",
}

// vtStrictRetired are the retired spellings: a partial-read entry point
// suffix and the report fields a strict report no longer carries.
var vtStrictRetired = []string{
	"_tolerant",
	".clamped",
	".kind_mismatch",
	".malformed",
	".duplicate",
	"SCHEMA_TABLE_REFUSE_CLAMPED",
}

func vtStrictFindings(where, text string) []string {
	var out []string
	for _, bad := range vtStrictRetired {
		if strings.Contains(text, bad) {
			out = append(out, fmt.Sprintf("%s retains retired strict-only spelling %q", where, bad))
		}
	}
	return out
}

const vtStrictSchema = `package probe

fixed table Inner
{
    n int32 = 0
}

table Graph
{
    a     int32 = 0
    score int32 = 0 | min = 0, max = 10
    inner Inner
    next  *Graph
}
`

const vtStrictWriterSchema = `package probe

fixed table Inner
{
    n int32 = 0
}

table Graph
{
    a     int32 = 0
    score int32 = 0
    inner Inner
    next  *Graph
}
`

func vtStrictWire(t *testing.T, src, text string) []byte {
	t.Helper()
	u := unitFrom(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Graph"))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("the text did not place: %+v", r)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestCVariableTestsStrictApi(t *testing.T) {
	t.Parallel()

	// The generated variable surface carries the plain names and none of
	// the retired spellings.
	files := generate(t, vtStrictSchema)
	var header strings.Builder
	for name, data := range files {
		for _, f := range vtStrictFindings("generated "+name, string(data)) {
			t.Error(f)
		}
		if strings.HasSuffix(name, "Table.h") {
			header.Write(data)
		}
	}
	for _, want := range []string{"graph_load(", "graph_load_builder(", "graph_load_retain(", "graph_from_json("} {
		if !strings.Contains(header.String(), want) {
			t.Errorf("generated variable header is missing the plain read %q", want)
		}
	}

	// The scoped compiler C variable-table tests use only that surface.
	for _, name := range vtStrictScopedFiles {
		path := filepath.Join("..", "..", "..", "compiler", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range vtStrictFindings(path, string(data)) {
			t.Error(f)
		}
	}

	// The strict read itself, run in C: a clean wire reads whole through
	// the region and builder names; damage, an out-of-range value and a
	// truncated wire each refuse whole and land nothing.
	slowtest.Gate(t, "the C compiler (cc)")
	reader := unitFrom(t, vtStrictSchema)
	clean := vtStrictWire(t, vtStrictSchema, `{"a":5,"score":7,"inner":{"n":3},"next":{"a":6}}`)
	damaged := bytes.Clone(clean)
	at := bytes.Index(damaged, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatal("Inner's body is not where this test expects it")
	}
	damaged[at] = 37
	over := vtStrictWire(t, vtStrictWriterSchema, `{"a":5,"score":99,"inner":{"n":3}}`)
	body := fmt.Sprintf(`
    static const uint8_t clean[] = { %s };
    static const uint8_t damaged[] = { %s };
    static const uint8_t over[] = { %s };
    static uint8_t region[8192];
    TableReport r; GraphBuilder b; const Graph * root; Graph * broot;
    memset( &r, 0, sizeof( r ) );
    root = graph_load( region, sizeof( region ), clean, (int64_t) sizeof( clean ), &r );
    if ( root == NULL || root->a != 5 || root->score != 7 || root->inner.n != 3 ) { printf( "clean region read did not land the whole value\n" ); return 1; }
    if ( !graph_builder_init( &b ) ) { return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( !graph_load_builder( &b, clean, (int64_t) sizeof( clean ), &r ) ) { printf( "clean builder read refused\n" ); return 1; }
    broot = graph_builder_root( &b );
    if ( broot == NULL || broot->a != 5 ) { printf( "clean builder read did not land the whole value\n" ); return 1; }
    graph_builder_shutdown( &b );
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, sizeof( region ), damaged, (int64_t) sizeof( damaged ), &r ) != NULL || !r.refused ) { printf( "damaged region read was not refused whole\n" ); return 1; }
    if ( !graph_builder_init( &b ) ) { return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( graph_load_builder( &b, damaged, (int64_t) sizeof( damaged ), &r ) ) { printf( "damaged builder read was not refused whole\n" ); return 1; }
    broot = graph_builder_root( &b );
    if ( broot != NULL && broot->a != 0 ) { printf( "damaged builder read landed a partial value\n" ); return 1; }
    graph_builder_shutdown( &b );
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, sizeof( region ), over, (int64_t) sizeof( over ), &r ) != NULL || !r.refused ) { printf( "out-of-range region read was not refused whole\n" ); return 1; }
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, sizeof( region ), clean, (int64_t) sizeof( clean ) - 3, &r ) != NULL || !r.refused ) { printf( "truncated region read was not refused whole\n" ); return 1; }
`, cBytes(clean), cBytes(damaged), cBytes(over))
	if out, err := cHeldRun(t, reader, body); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestCVariableTestsStrictApiNegativeControl is the gate's reversed
// witness: it plants a retired spelling in the real artifacts the gate
// reads — the actual bytes of a scoped compiler test file and the actual
// generated header — and requires the gate's own check to flag each plant.
// A check that cannot see a plant in the real artifact proves nothing
// about the unplanted one.
func TestCVariableTestsStrictApiNegativeControl(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "compiler", "tablescwire_test.go")
	real, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := vtStrictFindings(path, string(real)); len(got) != 0 {
		t.Fatalf("the unplanted real artifact already fails the gate: %v", got)
	}
	planted := string(real) + "\n// plant: root_load_tolerant and report.clamped\n"
	got := vtStrictFindings(path, planted)
	if len(got) != 2 {
		t.Fatalf("the gate did not flag both plants in the real test file: %v", got)
	}

	files := generate(t, vtStrictSchema)
	var name string
	for n := range files {
		if strings.HasSuffix(n, "Table.h") {
			name = n
			break
		}
	}
	if name == "" {
		t.Fatal("generator produced no Table.h for the variable schema")
	}
	if got := vtStrictFindings("generated "+name, string(files[name])); len(got) != 0 {
		t.Fatalf("the unplanted generated artifact already fails the gate: %v", got)
	}
	sabotaged := string(files[name]) + "\nstatic const void * plant = (const void *) graph_load_tolerant;\n"
	if got := vtStrictFindings("generated "+name, sabotaged); len(got) != 1 {
		t.Fatalf("the gate did not flag the plant in the real generated header: %v", got)
	}
}
