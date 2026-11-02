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
`, cppBytes(fixed), cppBytes(variable))
	out, err := cppHeldRun(t, u, body)
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
func cycleProbe(name string, wire []byte, load bool, onFail string) string {
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
    }
`, cppBytes(wire), want, onFail, onFail, onFail)
}

// TestCyclicWireIsRefusedCPP is the C++ reference's gate for Glenn's ruling
// (2026-10-31, docs/SPEC-TABLES.md §3.1, §4): a cyclic wire is damage, on the
// region load, the retaining load and the builder load. A back edge to a mid
// ancestor, a cycle the root never reaches, and a cycle through a slot the
// storage holds under a FALSE guard each refuse; the acyclic twin of each
// loads.
func TestCyclicWireIsRefusedCPP(t *testing.T) {
	t.Parallel()
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
	for l := 0; l < levels-1; l++ {
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
