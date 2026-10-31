package ctable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4; Glenn, 2026-10-31:
// "if it fails to read, it's broken and everything in it today is suspect.
// discard."), on every C read.
//
// TestDamageDiscardsTheWholeLoadC is the C leg's gate, the twin of the C++
// reference's TestDamageDiscardsTheWholeLoadCPP: damage inside a nested body,
// with a whole field before it and after it, so a reader that kept what it
// decoded would have something to hand back. Every public load answers no
// usable value: the value read and the strict read answer 0 with the value at
// its declared defaults, the region read and the retaining read hand back no
// root (the retention store empty), the builder read answers 0 with the root
// at its defaults, the text reads answer 0 with the value at its defaults, and
// a damaged message batch answers count 0 with every record reset.
//
// TestDamageDiscardsTheWholeLoadCNegativeControl is the gate's own proof:
// each read's discard is removed in turn from the emitted source, and the
// probe must go red on exactly the rows that discard owns.

const discardProbeSchema = `package probe

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

// discardProbeWire encodes the text as the root and damages the nested body:
// Inner's one field, kind 4 holding 3, gets kind 37, which no reader can skip.
// The engine is asked first, so the vector is damaged by the rule and not by
// assumption.
func discardProbeWire(t *testing.T, u *ir.Unit, root string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"a":5,"inner":{"n":3},"b":9}`), &r) {
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

func cBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

const discardProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>

static int failures;
#define FAIL( row, why ) do { printf( "FAIL: %s: %s\n", row, why ); failures++; } while ( 0 )

static const uint8_t fixed_wire[] = { FIXED_WIRE };
static const uint8_t graph_wire[] = { GRAPH_WIRE };
static const char fixed_text[] = "{\"a\":5,\"inner\":{\"n\":3},\"b\":";
static const char graph_text[] = "{\"a\":5,\"inner\":{\"n\":3},\"b\":";

static int root_is_reset( const Root * v ) { return v->a == 0 && v->b == 0 && v->inner.n == 0; }
static int graph_is_reset( const Graph * v ) { return v->a == 0 && v->b == 0 && v->inner.n == 0; }

static void value_rows( void )
{
    Root v; TableReport r;
    root_reset( &v ); memset( &r, 0, sizeof( r ) );
    if ( root_load_tolerant( &v, fixed_wire, (int64_t) sizeof( fixed_wire ), &r ) ) { FAIL( "value", "load_tolerant answered 1 on damage" ); }
    else if ( !r.malformed ) { FAIL( "value", "the report does not say malformed" ); }
    else if ( !root_is_reset( &v ) ) { printf( "value: a=%d b=%d n=%d\n", (int) v.a, (int) v.b, (int) v.inner.n ); FAIL( "value", "a damaged load handed back what it decoded" ); }
    root_reset( &v );
    if ( root_load( &v, fixed_wire, (int64_t) sizeof( fixed_wire ), &r ) || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED || !root_is_reset( &v ) )
    { FAIL( "strict", "the strict load did not refuse the damage whole" ); }
}

static void region_rows( void )
{
    static uint8_t store[1 << 12]; static TableRetainId ids[16];
    int64_t need = graph_load_measure( graph_wire, (int64_t) sizeof( graph_wire ) );
    uint8_t * region = (uint8_t *) malloc( (size_t) ( need > 0 ? need : 1 ) );
    TableReport r; TableRetain retain; const Graph * root;
    if ( need <= 0 || region == NULL ) { FAIL( "region", "load_measure" ); free( region ); return; }
    memset( &r, 0, sizeof( r ) );
    root = graph_load_tolerant( region, need, graph_wire, (int64_t) sizeof( graph_wire ), &r );
    if ( root != NULL ) { printf( "region: a=%d b=%d\n", (int) root->a, (int) root->b ); FAIL( "region", "load_tolerant handed back a root on damage" ); }
    else if ( !r.malformed ) { FAIL( "region", "the report does not say malformed" ); }
    memset( &r, 0, sizeof( r ) );
    if ( graph_load( region, need, graph_wire, (int64_t) sizeof( graph_wire ), &r ) != NULL || r.refuse_reason != SCHEMA_TABLE_REFUSE_WIRE_DAMAGED )
    { FAIL( "region strict", "the strict region load did not refuse the damage" ); }
    memset( &retain, 0, sizeof( retain ) ); retain.bytes = store; retain.capacity = sizeof( store ); retain.ids = ids; retain.id_capacity = 16;
    memset( &r, 0, sizeof( r ) );
    if ( graph_load_retain( region, need, graph_wire, (int64_t) sizeof( graph_wire ), &retain, &r ) != NULL )
    { FAIL( "retain", "load_retain handed back a root on damage" ); }
    else if ( !r.malformed || retain.used != 0 || retain.count != 0 ) { FAIL( "retain", "the report is not malformed or the retention store is not empty" ); }
    free( region );
}

static void builder_rows( void )
{
    GraphBuilder b; TableReport r; Graph * root;
    if ( !graph_builder_init( &b ) ) { FAIL( "builder", "builder_init" ); return; }
    memset( &r, 0, sizeof( r ) );
    if ( graph_load_builder_tolerant( &b, graph_wire, (int64_t) sizeof( graph_wire ), &r ) ) { FAIL( "builder", "load_builder_tolerant answered 1 on damage" ); }
    else if ( !r.malformed ) { FAIL( "builder", "the report does not say malformed" ); }
    else if ( ( root = graph_builder_root( &b ) ) != NULL && !graph_is_reset( root ) )
    { printf( "builder: a=%d b=%d\n", (int) root->a, (int) root->b ); FAIL( "builder", "the builder's root kept what the damaged load decoded" ); }
    graph_builder_shutdown( &b );
}

static void text_rows( void )
{
    Root v; GraphBuilder b; TableReport r; Graph * root;
    root_reset( &v ); memset( &r, 0, sizeof( r ) );
    if ( root_from_json_tolerant( &v, fixed_text, (int64_t) strlen( fixed_text ), &r ) ) { FAIL( "text", "from_json_tolerant answered 1 on a cut text" ); }
    else if ( !root_is_reset( &v ) ) { printf( "text: a=%d n=%d\n", (int) v.a, (int) v.inner.n ); FAIL( "text", "a damaged text read handed back what it decoded" ); }
    if ( !graph_builder_init( &b ) ) { FAIL( "graph text", "builder_init" ); return; }
    memset( &r, 0, sizeof( r ) );
    if ( graph_from_json_tolerant( &b, graph_text, (int64_t) strlen( graph_text ), &r ) ) { FAIL( "graph text", "from_json_tolerant answered 1 on a cut text" ); }
    else if ( ( root = graph_builder_root( &b ) ) != NULL && !graph_is_reset( root ) )
    { printf( "graph text: a=%d n=%d\n", (int) root->a, (int) root->inner.n ); FAIL( "graph text", "a damaged text read left what it decoded in the root" ); }
    graph_builder_shutdown( &b );
}

static void message_rows( void )
{
    TableMessageEntry entries[kTableMessageEntriesHere]; TableVocabulary voc = table_vocabulary( entries, kTableMessageEntriesHere );
    Root values[2]; TableReport r; uint8_t wire[256]; int64_t n, count = 2;
    memset( &r, 0, sizeof( r ) );
    if ( !announce_read( &voc, kTableAnnounce, kTableAnnounceBytes, &r ) ) { FAIL( "message", "announce_read" ); return; }
    root_reset( &values[0] ); values[0].a = 5; values[0].inner.n = 3; values[0].b = 9;
    values[1] = values[0];
    n = root_save_messages( values, 2, wire, sizeof( wire ), &r );
    if ( n < 2 ) { FAIL( "message", "save_messages" ); return; }
    memset( &r, 0, sizeof( r ) );
    if ( root_load_messages_tolerant( values, &count, &voc, wire, n - 1, &r ) || count != 0 || !root_is_reset( &values[0] ) || !root_is_reset( &values[1] ) )
    { FAIL( "message", "a damaged batch was not discarded whole" ); }
}

int main( void )
{
    value_rows();
    region_rows();
    builder_rows();
    text_rows();
    message_rows();
    return failures != 0;
}
`

// discardSabotage is one read's discard removed from the emitted source (each
// pair a from and its replacement), and the rows the probe must then fail:
// those rows, every one of them, and no other.
type discardSabotage struct {
	name  string
	pairs [][2]string
	rows  []string
}

var discardSabotages = []discardSabotage{
	{"value read", [][2]string{{"if ( !whole || report->malformed ) { probe_root_reset( value ); return 0; }", "if ( !whole ) { return 0; }"}},
		[]string{"value"}},
	{"region read", [][2]string{{"if(!probe_graph_load_graph(&reader,(Graph *)(void *)region,&nodes,directory,&sink) || report->malformed) {", "if(!probe_graph_load_graph(&reader,(Graph *)(void *)region,&nodes,directory,&sink)) {"}},
		[]string{"region"}},
	{"retaining read", [][2]string{{"retention)||report->malformed){", "retention)){"}},
		[]string{"retain"}},
	{"builder read", [][2]string{{"if(!ok || report->malformed) { probe_graph_reset(root); return 0; }", "if(!ok) { return 0; }"}},
		[]string{"builder"}},
	{"text read", [][2]string{{"if ( value != NULL ) { probe_root_reset( value ); } /* damage discards the whole load (§4) */", "/* SABOTAGED */"}},
		[]string{"text"}},
	{"graph text read", [][2]string{{"if ( root != NULL ) { probe_graph_reset( root ); } /* damage discards the whole load (§4) */", "/* SABOTAGED */"}},
		[]string{"graph text"}},
}

// discardProbeRun builds the probe over the generated unit, with sabotage (if
// any) applied to the emitted source, and answers the FAIL lines.
func discardProbeRun(t *testing.T, sabotage *discardSabotage) ([]string, string) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	u := unitFrom(t, discardProbeSchema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if sabotage != nil {
		for _, pair := range sabotage.pairs {
			patched := false
			for name, data := range files {
				if src := string(data); strings.Contains(src, pair[0]) {
					files[name] = []byte(strings.ReplaceAll(src, pair[0], pair[1]))
					patched = true
				}
			}
			if !patched {
				t.Fatalf("the %s sabotage patches nothing: no emitted file carries %q", sabotage.name, pair[0])
			}
		}
	}
	source := strings.Replace(discardProbe, "FIXED_WIRE", cBytes(discardProbeWire(t, u, "Root")), 1)
	source = strings.Replace(source, "GRAPH_WIRE", cBytes(discardProbeWire(t, u, "Graph")), 1)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	opt := "-O2"
	if sabotage != nil {
		opt = "-O0" // a control is run once and thrown away (make/c.mk TABLES_CFLAGS_CONTROL)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", opt, "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	output, _ := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	var fails []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if rest, ok := strings.CutPrefix(line, "FAIL: "); ok {
			fails = append(fails, rest)
		}
	}
	if strings.Contains(string(output), "ERROR: AddressSanitizer") || strings.Contains(string(output), "runtime error:") {
		t.Fatalf("the probe tripped a sanitizer:\n%s", output)
	}
	return fails, string(output)
}

func TestDamageDiscardsTheWholeLoadC(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	if fails, out := discardProbeRun(t, nil); len(fails) != 0 {
		t.Fatalf("a C read handed back a value after damage:\n%s", out)
	}
}

func TestDamageDiscardsTheWholeLoadCNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	for i := range discardSabotages {
		c := &discardSabotages[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fails, out := discardProbeRun(t, c)
			if len(fails) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the probe green:\n%s", c.name, out)
			}
			for _, line := range fails {
				row, _, _ := strings.Cut(line, ": ")
				owned := false
				for _, want := range c.rows {
					owned = owned || row == want
				}
				if !owned {
					t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage turned row %q red, which it does not own:\n%s", c.name, row, out)
				}
			}
			for _, want := range c.rows {
				hit := false
				for _, line := range fails {
					row, _, _ := strings.Cut(line, ": ")
					hit = hit || row == want
				}
				if !hit {
					t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left row %q green:\n%s", c.name, want, out)
				}
			}
		})
	}
}
