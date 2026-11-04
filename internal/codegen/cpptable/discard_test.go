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
    want[0].lead = 7; want[0].rng = 100; memcpy( want[0].label, "ABCD", 4 ); want[0].label_length = 4;
    want[1].lead = 9; want[1].rng = 50;  memcpy( want[1].label, "QWER", 4 ); want[1].label_length = 4;
    const int64_t n = RecFixedSave( want, 2, data, (int64_t) sizeof( data ) );
    if ( n != RecFixedMeasure( 2 ) ) { printf( "fixed: the save wrote %lld\n", (long long) n ); return 1; }
    // record 0's rng past its max, for the read to clamp: the writer refuses
    // it (docs/SPEC.md §5.1), so the record's own store puts it on the wire
    want[0].rng = 101;
    RecFixedWriteBody( data + RecFixedMeasure( 0 ) + 8, want[0] );
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

// TestPointerNamingAnOpenNodeIsRefusedCPP is the C++ reference's gate for
// docs/SPEC-TABLES.md §3.1: no writer produces a cycle, so a pointer naming a
// node whose descent is still open is damage and the whole load is refused.
// The wire is a clean chain root -> 2 -> 3 with node 2's pointer turned back
// at node 2 itself. Red if the region load or the builder load hands back the
// cyclic graph.
func TestPointerNamingAnOpenNodeIsRefusedCPP(t *testing.T) {
	t.Parallel()
	u := loadUnitFromSource(t, discardUnit)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Graph"))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"a":5,"next":{"a":6,"next":{"a":7}}}`), &r) {
		t.Fatal("the text did not place")
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(wire, []byte{byte(ir.TableKindPointer), 3})
	if at < 0 || bytes.Count(wire, []byte{byte(ir.TableKindPointer), 3}) != 1 {
		t.Fatal("node 2's pointer at node 3 is not where this test expects it")
	}
	wire[at+1] = 2
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Graph")), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("the engine does not refuse the cycle: ok=%v err=%v %+v", ok, err, report)
	}
	body := fmt.Sprintf(`
    const uint8_t cyclic[] = { %s };
    alignas( 64 ) static uint8_t region[ 1 << 16 ];
    TableReport g; memset( &g, 0, sizeof( g ) );
    if ( GraphLoadTolerant( region, (int64_t) sizeof( region ), cyclic, (int64_t) sizeof( cyclic ), &g ) != NULL ) { printf( "cycle: the region load handed back a cyclic graph\n" ); return 1; }
    if ( !g.malformed ) { printf( "cycle: the region load did not say malformed\n" ); return 1; }
    GraphBuilder builder;
    TableReport br; memset( &br, 0, sizeof( br ) );
    if ( GraphLoadBuilderTolerant( builder, cyclic, (int64_t) sizeof( cyclic ), &br ) || !br.malformed ) { printf( "cycle: the builder load did not refuse\n" ); return 1; }
`, cppBytes(wire))
	out, err := cppHeldRun(t, u, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// cycleUnit is the cycle check's own unit: two pointer slots to make shares
// and diamonds, and a third under a guard, so a slot the storage holds under a
// FALSE guard is an edge too (docs/SPEC-TABLES.md §3.1, §4).
const cycleUnit = `package cyclecheck

table Node
{
    a     int32 = 0
    on    bool
    left  *Node
    right *Node
    if on
    {
        side *Node
    }
}
`

// cycleGraph builds a Node graph in the engine's model: nodes[0] is the root,
// and each link names (from, field, to).
type cycleLink struct {
	from  int
	field string
	to    int
}

func cycleEncode(t *testing.T, m *tabletext.Model, count int, on []int, links []cycleLink) []byte {
	t.Helper()
	def := m.Lookup("Node")
	nodes := make([]*tabletext.Instance, count)
	for i := range nodes {
		nodes[i] = m.New(def)
	}
	for _, i := range on {
		f, _ := nodes[i].FieldByKey("on")
		f.Cell.B = true
	}
	for _, l := range links {
		f, ok := nodes[l.from].FieldByKey(l.field)
		if !ok {
			t.Fatalf("Node has no field %s", l.field)
		}
		f.Cell.Node = nodes[l.to]
	}
	wire, err := tablewire.Encode(m, nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// cyclePatch turns the ONE pointer naming node index `from` into one naming
// `to`. Every test keeps its node count under 128, so an index is one byte.
func cyclePatch(t *testing.T, wire []byte, from, to byte) {
	t.Helper()
	pattern := []byte{byte(ir.TableKindPointer), from}
	if bytes.Count(wire, pattern) != 1 {
		t.Fatalf("index %d is named %d times, not once", from, bytes.Count(wire, pattern))
	}
	wire[bytes.Index(wire, pattern)+1] = to
}

// cycleGuardOff turns the root's left node's `on` (true) false on the wire,
// leaving the guarded slot written: the engine is asked which byte it is, so
// the patch is the rule's and not a guess.
func cycleGuardOff(t *testing.T, m *tabletext.Model, wire []byte) {
	t.Helper()
	for i := 0; i+2 < len(wire); i++ {
		if wire[i+1] != byte(ir.TableKindBool) || wire[i+2] != 1 {
			continue
		}
		try := append([]byte(nil), wire...)
		try[i+2] = 0
		root := m.New(m.Lookup("Node"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, root, try, &report); !ok || err != nil || report.Malformed {
			continue
		}
		left, _ := root.FieldByKey("left")
		if left.Cell.Node == nil {
			continue
		}
		on, _ := left.Cell.Node.FieldByKey("on")
		side, _ := left.Cell.Node.FieldByKey("side")
		if !on.Cell.B && side.Cell.Node != nil {
			copy(wire, try)
			return
		}
	}
	t.Fatal("no byte turns the guard off with the guarded slot kept")
}

// cycleProbe is the C++ that loads one wire through the region Load, the
// retaining Load and the builder Load, and demands the given verdict of all
// three: refused as damage, or loaded whole.
func cycleProbe(name string, wire []byte, load bool, onFail string, after ...string) string {
	want := "false"
	if load {
		want = "true"
	}
	return fmt.Sprintf(`
    {
        const uint8_t wire[] = { %s };
        const bool want = %s;
        const int64_t need = NodeLoadMeasure( wire, (int64_t) sizeof( wire ) );
        std::vector<uint8_t> region( (size_t) ( need > 0 ? need : 4096 ) + 64 );
        uint8_t * aligned = region.data();
        while ( ( (uintptr_t) aligned & 63 ) != 0 ) { aligned++; }
        TableReport g; memset( &g, 0, sizeof( g ) );
        const Node * root = NodeLoadTolerant( aligned, need > 0 ? need : 4096, wire, (int64_t) sizeof( wire ), &g );
        if ( ( root != NULL ) != want || g.malformed == want ) { printf( "%s: the region load: root=%%d malformed=%%d\n", root != NULL ? 1 : 0, (int) g.malformed ); return 1; }
        alignas( 64 ) static uint8_t retain_region[ 1 << 16 ];
        uint8_t store[ 1 << 12 ];
        TableRetain retain;
        TableRetain::Id ids[16];
        retain.bytes = store; retain.capacity = (int64_t) sizeof( store );
        retain.ids = ids; retain.id_capacity = 16;
        TableReport rr; memset( &rr, 0, sizeof( rr ) );
        const Node * kept = NodeLoadRetain( retain_region, (int64_t) sizeof( retain_region ), wire, (int64_t) sizeof( wire ), &retain, &rr );
        if ( ( kept != NULL ) != want || rr.malformed == want ) { printf( "%s: the retaining load: root=%%d malformed=%%d\n", kept != NULL ? 1 : 0, (int) rr.malformed ); return 1; }
        NodeBuilder builder;
        TableReport br; memset( &br, 0, sizeof( br ) );
        const bool built = NodeLoadBuilderTolerant( builder, wire, (int64_t) sizeof( wire ), &br );
        if ( built != want || br.malformed == want ) { printf( "%s: the builder load: ok=%%d malformed=%%d\n", (int) built, (int) br.malformed ); return 1; }
%s
    }
`, cppBytes(wire), want, onFail, onFail, onFail, strings.Join(after, "\n"))
}

// TestCyclicWireIsRefusedCPP is the C++ reference's gate for Glenn's ruling
// (2026-10-31, docs/SPEC-TABLES.md §3.1, §4): a cyclic wire is damage, on the
// region load, the retaining load and the builder load. A back edge to a mid
// ancestor, a cycle the root never reaches, and a cycle through a slot the
// storage holds under a FALSE guard each refuse; the acyclic twin of each
// loads.
func TestCyclicWireIsRefusedCPP(t *testing.T) {
	t.Parallel()
	t.Run("container slots", testCyclicContainerSlotsCPP)
	u := loadUnitFromSource(t, cycleUnit)
	m := tabletext.NewModel(u)
	var body strings.Builder

	// root(1) -> A(2) -> B(3) -> L(4); B's pointer at L turned back at A
	chain := []cycleLink{{0, "left", 1}, {1, "left", 2}, {2, "left", 3}}
	twin := cycleEncode(t, m, 4, nil, chain)
	body.WriteString(cycleProbe("mid ancestor twin", twin, true, "cycle: the acyclic twin of the mid-ancestor case was refused"))
	mid := cycleEncode(t, m, 4, nil, chain)
	cyclePatch(t, mid, 4, 2)
	body.WriteString(cycleProbe("mid ancestor", mid, false, "cycle: a back edge to a mid ancestor loaded"))

	// the same chain with the root's pointer at A nulled: A -> B -> A is a
	// cycle no walk from the root reaches
	unreached := cycleEncode(t, m, 4, nil, chain)
	cyclePatch(t, unreached, 2, 0)
	cyclePatch(t, unreached, 4, 2)
	body.WriteString(cycleProbe("unreached", unreached, false, "cycle: a cycle the root does not reach loaded"))

	// root(1) -> A(2), A.on and A.side -> L(3); the guard turned off on the
	// wire with the slot kept, and the slot turned back at A
	guarded := []cycleLink{{0, "left", 1}, {1, "side", 2}}
	off := cycleEncode(t, m, 3, []int{1}, guarded)
	cycleGuardOff(t, m, off)
	body.WriteString(cycleProbe("guard twin", off, true, "cycle: the acyclic twin of the false-guard case was refused"))
	cyclePatch(t, off, 3, 2)
	body.WriteString(cycleProbe("false guard", off, false, "cycle: a cycle through a slot under a false guard loaded"))

	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestDiamondChainLoadsLinearCPP is the cycle check's LINEAR gate: a chain of
// forty diamonds, each level's two nodes both naming the next level's two, is
// a DAG with 2^40 root-to-leaf paths. It must load whole on every read, and a
// check that re-enters a shared node, or walks paths rather than nodes, cannot
// finish it or refuses it.
func TestDiamondChainLoadsLinearCPP(t *testing.T) {
	t.Parallel()
	u := loadUnitFromSource(t, cycleUnit)
	m := tabletext.NewModel(u)
	const levels = 40
	count := 1 + 2*levels
	var links []cycleLink
	links = append(links, cycleLink{0, "left", 1}, cycleLink{0, "right", 2})
	for l := range levels - 1 {
		a, b := 1+2*l, 2+2*l
		na, nb := a+2, b+2
		links = append(links, cycleLink{a, "left", na}, cycleLink{a, "right", nb}, cycleLink{b, "left", na}, cycleLink{b, "right", nb})
	}
	wire := cycleEncode(t, m, count, nil, links)
	out, err := cppHeldRun(t, u, cycleProbe("diamond", wire, true, "diamond: the walk did not finish linear"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// A node reached through a container still participates in the cycle check.
// Its valid twin must publish usable pointers on all three load paths: saving
// the loaded graph must recover the engine's original canonical bytes.
func testCyclicContainerSlotsCPP(t *testing.T) {
	u := loadUnitFromSource(t, `package cyclecontainers
table Node
{
    left *Node
    edges map[uint32]*Node
    links []*Node
    slots [2]*Node
}
`)
	m := tabletext.NewModel(u)
	const roundTrip = `
        std::vector<uint8_t> saved( sizeof( wire ) );
        if ( NodeMeasure( root ) != (int64_t) sizeof( wire ) ||
             NodeSave( root, saved.data(), (int64_t) saved.size() ) != (int64_t) sizeof( wire ) ||
             memcmp( saved.data(), wire, sizeof( wire ) ) != 0 )
            { printf( "container: region pointers were not published correctly\n" ); return 1; }
        if ( NodeSave( kept, saved.data(), (int64_t) saved.size() ) != (int64_t) sizeof( wire ) ||
             memcmp( saved.data(), wire, sizeof( wire ) ) != 0 )
            { printf( "container: retained pointers were not published correctly\n" ); return 1; }
        if ( NodeMeasure( builder ) != (int64_t) sizeof( wire ) ||
             NodeSave( builder, saved.data(), (int64_t) saved.size() ) != (int64_t) sizeof( wire ) ||
             memcmp( saved.data(), wire, sizeof( wire ) ) != 0 )
            { printf( "container: builder pointers were not published correctly\n" ); return 1; }
`
	var body strings.Builder
	for _, tc := range []struct{ name, text string }{
		{"map", `{"edges":{"7":{"left":{}}}}`},
		{"list", `{"links":[{"left":{}}]}`},
		{"array", `{"slots":[{"left":{}},null]}`},
	} {
		inst := m.New(m.Lookup("Node"))
		var report tabletext.Report
		if !m.Read(inst, []byte(tc.text), &report) || report.Malformed {
			t.Fatalf("%s: valid container text refused: %+v", tc.name, report)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		body.WriteString(cycleProbe(tc.name+" twin", wire, true, "container: valid "+tc.name+" refused", roundTrip))
		cyclePatch(t, wire, 3, 1) // root -> container child -> root
		var damaged tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Node")), wire, &damaged); ok || err != nil || !damaged.Malformed {
			t.Fatalf("%s: engine did not refuse the cycle: ok=%v err=%v %+v", tc.name, ok, err, damaged)
		}
		body.WriteString(cycleProbe(tc.name+" cycle", wire, false, "container: cyclic "+tc.name+" loaded"))
	}
	out, err := cppHeldRun(t, u, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
