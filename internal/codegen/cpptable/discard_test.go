package cpptable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
    f     float32 = 0
    next  *Graph
}
`

// discardWriterUnit is discardUnit's Graph as a newer writer declares it, with
// one field the reader cannot name, so a retaining read has a record to keep.
const discardWriterUnit = `package discard

fixed table Inner
{
    n int32 = 0
}

table Graph
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
    f     float32 = 0
    extra int32 = 0
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
// false with the value at its declared defaults, the strict Load refuse, the
// region Load and the retaining Load hand back no root (the retention store
// empty), and the builder load answer false. Red if any read hands back what
// it decoded.
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

    alignas( 64 ) static uint8_t retain_region[ 1 << 16 ];
    uint8_t store[ 1 << 12 ];
    TableRetain retain;
    TableRetain::Id ids[16];
    retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
    retain.ids = ids; retain.id_capacity = 16;
    TableReport rr; memset( &rr, 0, sizeof( rr ) );
    if ( GraphLoadRetain( retain_region, (int64_t) sizeof( retain_region ), variable, (int64_t) sizeof( variable ), &retain, &rr ) != NULL )
        { printf( "retain: LoadRetain handed back a root on damage\n" ); return 1; }
    if ( !rr.malformed || retain.used != 0 || retain.count != 0 ) { printf( "retain: malformed=%%d used=%%lld count=%%lld\n", (int) rr.malformed, (long long) retain.used, (long long) retain.count ); return 1; }

    GraphBuilder builder;
    TableReport br; memset( &br, 0, sizeof( br ) );
    if ( GraphLoadBuilderTolerant( builder, variable, (int64_t) sizeof( variable ), &br ) ) { printf( "builder: LoadBuilderTolerant answered true on damage\n" ); return 1; }
    if ( !br.malformed ) { printf( "builder: the report does not say malformed\n" ); return 1; }
    const Graph * groot = builder.GetRoot();
    if ( groot == NULL || groot->a != 0 || groot->b != 0 || groot->inner.n != 0 ) { printf( "builder: a damaged load left a usable root\n" ); return 1; }
    GraphBuilder strict_builder;
    TableReport sr; memset( &sr, 0, sizeof( sr ) );
    if ( GraphLoadBuilder( strict_builder, variable, (int64_t) sizeof( variable ), &sr ) ) { printf( "builder: the strict load accepted damage\n" ); return 1; }
    const Graph * sroot = strict_builder.GetRoot();
    if ( sroot == NULL || sroot->a != 0 || sroot->b != 0 ) { printf( "builder: the strict load left a usable root\n" ); return 1; }
`, cppBytes(fixed), cppBytes(variable))
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestRefusedLoadsHandBackNothingCPP holds the REFUSALS that are not framing
// damage to the same rule (docs/SPEC-TABLES.md §3.3, §4, §6.5): a non-finite
// float refuses the read, and the builder load then leaves its root at the
// declared defaults and the retaining load leaves its store empty. The wire is
// a newer writer's, so the retaining read has an unknown field to keep before
// the refusal lands. Red if either hands back what it read.
func TestRefusedLoadsHandBackNothingCPP(t *testing.T) {
	t.Parallel()
	writer := loadUnitFromSource(t, discardWriterUnit)
	m := tabletext.NewModel(writer)
	inst := m.New(m.Lookup("Graph"))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"a":5,"inner":{"n":3},"b":9,"f":1.5,"extra":7}`), &r) {
		t.Fatal("the text did not place")
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	onePointFive := []byte{0x00, 0x00, 0xC0, 0x3F}
	at := bytes.Index(wire, onePointFive)
	if at < 0 || bytes.Count(wire, onePointFive) != 1 {
		t.Fatal("the float field is not where this test expects it")
	}
	wire[at+3] = 0x7F // 0x7FC00000: a NaN
	reader := loadUnitFromSource(t, discardUnit)
	rm := tabletext.NewModel(reader)
	var report tabletext.Report
	if _, err := tablewire.Decode(rm, rm.New(rm.Lookup("Graph")), wire, &report); err == nil {
		t.Fatal("the engine does not refuse the NaN")
	}
	body := fmt.Sprintf(`
    const uint8_t nan_wire[] = { %s };
    GraphBuilder builder;
    TableReport br; memset( &br, 0, sizeof( br ) );
    if ( GraphLoadBuilderTolerant( builder, nan_wire, (int64_t) sizeof( nan_wire ), &br ) ) { printf( "nan: the builder load accepted a NaN\n" ); return 1; }
    const Graph * groot = builder.GetRoot();
    if ( groot == NULL || groot->a != 0 || groot->b != 0 || groot->inner.n != 0 ) { printf( "nan: the builder load left a usable root\n" ); return 1; }

    alignas( 64 ) static uint8_t region[ 1 << 16 ];
    uint8_t store[ 1 << 12 ];
    TableRetain retain;
    TableRetain::Id ids[16];
    retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
    retain.ids = ids; retain.id_capacity = 16;
    TableReport rr; memset( &rr, 0, sizeof( rr ) );
    if ( GraphLoadRetain( region, (int64_t) sizeof( region ), nan_wire, (int64_t) sizeof( nan_wire ), &retain, &rr ) != NULL ) { printf( "nan: LoadRetain handed back a root\n" ); return 1; }
    if ( retain.used != 0 || retain.count != 0 ) { printf( "nan: the retention store kept used=%%lld count=%%lld\n", (long long) retain.used, (long long) retain.count ); return 1; }
`, cppBytes(wire))
	out, err := cppHeldRun(t, reader, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestFixedRefusalHandsBackNothingCPP holds the fixed form's tolerant read to
// the same rule (docs/SPEC-TABLES.md §4, §6.5): a batch refused at record 1,
// by a layout hash it cannot name or by ill-formed text, leaves record 0 at
// its declared defaults and the report's clamped and widened where they were
// before the call, though record 0 decoded and clamped first.
func TestFixedRefusalHandsBackNothingCPP(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "fxdiscard.schema")
	if err := os.WriteFile(path, []byte(`package fxdiscard

fixed table Rec
{
    lead uint32
    rng int32 | min = 0, max = 100
    label string(8)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `
    static uint8_t data[1 << 12];
    static uint8_t work[1 << 12];
    static TableFixedEntry plan[4096];
    Rec want[2];
    RecReset( want[0] ); RecReset( want[1] );
    want[0].lead = 7; want[0].rng = 101; memcpy( want[0].label, "ABCD", 4 ); want[0].label_length = 4;
    want[1].lead = 9; want[1].rng = 50;  memcpy( want[1].label, "QWER", 4 ); want[1].label_length = 4;
    const int64_t n = RecFixedSave( want, 2, data, (int64_t) sizeof( data ) );
    if ( n != RecFixedMeasure( 2 ) ) { printf( "fixed: the save wrote %lld\n", (long long) n ); return 1; }
    const int64_t record_bytes = RecFixedMeasure( 2 ) - RecFixedMeasure( 1 );
    Rec fresh; RecReset( fresh );

    // record 1's layout hash names nothing: no_layout
    memcpy( work, data, (size_t) n );
    work[n - record_bytes] ^= 0x01;
    {
        Rec got[2];
        TableReport r; memset( &r, 0, sizeof( r ) );
        const int64_t k = RecFixedLoadTolerant( got, 2, work, n, plan, 4096, NULL, &r );
        if ( k != -1 || !r.refused || r.reason != TableMessageReason::no_layout ) { printf( "fixed: a hash mismatch at record 1 was not refused: %lld\n", (long long) k ); return 1; }
        if ( got[0].lead != fresh.lead || got[0].rng != fresh.rng || got[0].label_length != fresh.label_length ) { printf( "fixed: no_layout left record 0 usable: lead=%u rng=%d\n", got[0].lead, got[0].rng ); return 1; }
        if ( r.clamped != 0 || r.widened != 0 ) { printf( "fixed: no_layout left clamped=%d widened=%d\n", r.clamped, r.widened ); return 1; }
    }

    // record 1's label is ill-formed UTF-8: text_ill_formed
    memcpy( work, data, (size_t) n );
    {
        int64_t at = -1;
        for ( int64_t i = n - record_bytes; i + 4 <= n; ++i ) { if ( memcmp( work + i, "QWER", 4 ) == 0 ) { at = i; break; } }
        if ( at < 0 ) { printf( "fixed: the label is not locatable\n" ); return 1; }
        work[at + 1] = 0xFF;
    }
    {
        Rec got[2];
        TableReport r; memset( &r, 0, sizeof( r ) );
        const int64_t k = RecFixedLoadTolerant( got, 2, work, n, plan, 4096, NULL, &r );
        if ( k != -1 || !r.refused || r.reason != TableMessageReason::text_ill_formed ) { printf( "fixed: ill-formed text at record 1 was not refused: %lld\n", (long long) k ); return 1; }
        if ( got[0].lead != fresh.lead || got[0].rng != fresh.rng || got[0].label_length != fresh.label_length ) { printf( "fixed: text_ill_formed left record 0 usable: lead=%u rng=%d\n", got[0].lead, got[0].rng ); return 1; }
        if ( r.clamped != 0 || r.widened != 0 ) { printf( "fixed: text_ill_formed left clamped=%d widened=%d\n", r.clamped, r.widened ); return 1; }
    }
`
	out, err := cppRunCapability(t, path, "fxdiscard", body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
