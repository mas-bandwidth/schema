// A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4). Glenn's ruling,
// 2026-10-31: "Why the fuck would I want to read a cycle" and "It's perhaps
// the MOST OBVIOUS attack somebody would do on this protocol!"
//
// No writer produces a cycle (the numbering walk refuses one at save and at
// Lock), so a load whose graph holds one refuses the WHOLE load, on the
// strict, tolerant, retaining and region reads alike. This file is the
// engine's gate:
//
//  1. THE PINNED FORGED CYCLE (schema#535's one flipped bit) is damage, and
//     the instance is handed back at its declared defaults.
//  2. EVERY STORED SLOT COUNTS: a cycle through a pointer under a false
//     guard, and a cycle the root never reaches, both refuse; the acyclic twin
//     of each loads.
//  3. THE CHECK IS LINEAR: a diamond chain of 81 nodes and 2^40 paths loads,
//     inside a deadline, because each node is entered once and a pointer to a
//     CLOSED node is a share, never re-entered.
package tablewire_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const selfCycleVector = "../../testdata/wire/tables/fuzz-vectors/pointer_self_cycle.bin"

func pointerModel(t *testing.T) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/pointers"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

func TestForgedPointerCycleIsDamage(t *testing.T) {
	t.Parallel()
	wire, err := os.ReadFile(selfCycleVector)
	if err != nil {
		t.Fatal(err)
	}
	m := pointerModel(t)
	inst := m.New(m.Lookup("Scene"))
	var r tabletext.Report
	ok, derr := tablewire.Decode(m, inst, wire, &r)
	if derr != nil {
		t.Fatalf("the load errored rather than reporting damage: %v", derr)
	}
	if ok || !r.Malformed {
		t.Fatalf("a forged cycle loaded: ok=%v %+v", ok, r)
	}
	head, _ := inst.FieldByKey("head")
	if head.Cell.Node != nil {
		t.Fatal("damage discards the whole load, and the refused instance still holds a node")
	}
}

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

func cycleModel(t *testing.T) *tabletext.Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(path, []byte(cycleUnit), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

type cycleLink struct {
	from  int
	field string
	to    int
}

// cycleEncode writes an acyclic Node graph, nodes[0] the root, through the
// engine's writer; a test then turns one index on the wire.
func cycleEncode(t *testing.T, m *tabletext.Model, count int, on []int, links []cycleLink) []byte {
	t.Helper()
	nodes := make([]*tabletext.Instance, count)
	for i := range nodes {
		nodes[i] = m.New(m.Lookup("Node"))
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
// `to`; every graph here stays under 128 nodes, so an index is one byte.
func cyclePatch(t *testing.T, wire []byte, from, to byte) {
	t.Helper()
	pattern := []byte{byte(ir.TableKindPointer), from}
	if n := bytes.Count(wire, pattern); n != 1 {
		t.Fatalf("index %d is named %d times, not once", from, n)
	}
	wire[bytes.Index(wire, pattern)+1] = to
}

// cycleGuardOff clears the one `on = true` byte on the wire, so the guarded
// slot rides under a FALSE guard; the engine confirms the slot is still
// stored.
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

// cycleRead loads one wire on the tolerant and region reads and
// demands one verdict of all of them.
func cycleRead(t *testing.T, m *tabletext.Model, name string, wire []byte, load bool) {
	t.Helper()
	reads := map[string]func(*tabletext.Instance, *tabletext.Report) (bool, error){
		"tolerant": func(inst *tabletext.Instance, r *tabletext.Report) (bool, error) {
			return tablewire.Decode(m, inst, wire, r)
		},
		"region": func(inst *tabletext.Instance, r *tabletext.Report) (bool, error) {
			return tablewire.DecodeRegion(m, inst, wire, r)
		},
	}
	for how, read := range reads {
		var r tabletext.Report
		ok, err := read(m.New(m.Lookup("Node")), &r)
		if err != nil {
			t.Fatalf("%s (%s): %v", name, how, err)
		}
		if load && (!ok || r.Malformed) {
			t.Errorf("%s (%s): an acyclic graph was refused: %+v", name, how, r)
		}
		if !load && (ok || !r.Malformed) {
			t.Errorf("%s (%s): a cyclic wire loaded: %+v", name, how, r)
		}
	}
}

func TestCyclicWireIsDamage(t *testing.T) {
	t.Parallel()
	m := cycleModel(t)

	// root(1) -> A(2) -> B(3) -> L(4); B's pointer at L turned back at A
	chain := []cycleLink{{0, "left", 1}, {1, "left", 2}, {2, "left", 3}}
	cycleRead(t, m, "mid ancestor twin", cycleEncode(t, m, 4, nil, chain), true)
	mid := cycleEncode(t, m, 4, nil, chain)
	cyclePatch(t, mid, 4, 2)
	cycleRead(t, m, "mid ancestor", mid, false)
	self := cycleEncode(t, m, 4, nil, chain)
	cyclePatch(t, self, 4, 3)
	cycleRead(t, m, "self", self, false)
	root := cycleEncode(t, m, 4, nil, chain)
	cyclePatch(t, root, 4, 1)
	cycleRead(t, m, "to root", root, false)

	// the root's pointer at A nulled: A -> B -> A is a cycle the root never
	// reaches, and it is still a cyclic wire
	unreached := cycleEncode(t, m, 4, nil, chain)
	cyclePatch(t, unreached, 2, 0)
	cyclePatch(t, unreached, 4, 2)
	cycleRead(t, m, "unreached", unreached, false)

	// root(1) -> A(2), A.side -> L(3) under a guard the wire turns false: the
	// load still STORES the slot, so the slot is an edge
	guarded := []cycleLink{{0, "left", 1}, {1, "side", 2}}
	off := cycleEncode(t, m, 3, []int{1}, guarded)
	cycleGuardOff(t, m, off)
	cycleRead(t, m, "false guard twin", off, true)
	cyclePatch(t, off, 3, 2)
	cycleRead(t, m, "false guard", off, false)
}

// TestDiamondChainLoadsLinear is the check's linear gate: forty diamonds, each
// level's two nodes both naming the next level's two, are 81 nodes and 2^40
// root-to-leaf paths. The load must finish, and finish whole, in a deadline a
// walk that re-enters a shared node cannot meet.
func TestDiamondChainLoadsLinear(t *testing.T) {
	t.Parallel()
	m := cycleModel(t)
	const levels = 40
	links := []cycleLink{{0, "left", 1}, {0, "right", 2}}
	for l := 0; l < levels-1; l++ {
		a, b := 1+2*l, 2+2*l
		links = append(links, cycleLink{a, "left", a + 2}, cycleLink{a, "right", b + 2}, cycleLink{b, "left", a + 2}, cycleLink{b, "right", b + 2})
	}
	wire := cycleEncode(t, m, 1+2*levels, nil, links)
	done := make(chan error, 1)
	go func() {
		var r tabletext.Report
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Node")), wire, &r)
		if err == nil && (!ok || r.Malformed) {
			err = errDiamondRefused
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("diamond: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("diamond: the walk did not finish linear")
	}
}

type diamondError string

func (e diamondError) Error() string { return string(e) }

const errDiamondRefused = diamondError("a legal DAG was refused")
