package javatable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const javaCycleSchema = `package cyclecheck

table Node
{
    a      int32 = 0
    on     bool
    left   *Node
    right  *Node
    items  []*Node
    lookup map[int32]*Node
    if on
    {
        side *Node
    }
}
`

const javaCycleWriterSchema = `package cyclecheck

table Node
{
    a      int32 = 0
    on     bool
    left   *Node
    right  *Node
    items  []*Node
    lookup map[int32]*Node
    if on
    {
        side *Node
    }
    extra  int32 = 0
}
`

type javaCycleLink struct {
	from  int
	field string
	to    int
}

func javaCycleModel(t *testing.T, src string) (*ir.Unit, *tabletext.Model) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	return u, tabletext.NewModel(u)
}

func javaCycleEncode(t *testing.T, m *tabletext.Model, count int, on []int, extra int64, links []javaCycleLink) []byte {
	t.Helper()
	nodes := make([]*tabletext.Instance, count)
	for i := range nodes {
		nodes[i] = m.New(m.Lookup("Node"))
		if extra != 0 {
			if f, ok := nodes[i].FieldByKey("extra"); ok {
				f.Cell.I = extra
			}
		}
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
		switch l.field {
		case "items":
			f.Elems = append(f.Elems, tabletext.Cell{Node: nodes[l.to]})
			f.Count = len(f.Elems)
		case "lookup":
			entry := m.NewMapEntry(f.Def)
			kf, _ := entry.FieldByKey("key")
			kf.Cell.I = int64(len(f.Entries) + 1)
			vf, _ := entry.FieldByKey("value")
			vf.Cell.Node = nodes[l.to]
			f.Entries = append(f.Entries, tabletext.Cell{Tab: entry})
		default:
			f.Cell.Node = nodes[l.to]
		}
	}
	wire, err := tablewire.Encode(m, nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func javaCyclePatch(t *testing.T, wire []byte, from, to byte) {
	t.Helper()
	pattern := []byte{byte(ir.TableKindPointer), from}
	if n := bytes.Count(wire, pattern); n != 1 {
		t.Fatalf("index %d is named %d times, not once", from, n)
	}
	wire[bytes.Index(wire, pattern)+1] = to
}

func javaCycleGuardOff(t *testing.T, m *tabletext.Model, wire []byte) {
	t.Helper()
	for i := 0; i+2 < len(wire); i++ {
		if wire[i+1] != byte(ir.TableKindBool) || wire[i+2] != 1 {
			continue
		}
		try := append([]byte(nil), wire...)
		try[i+2] = 0
		root := m.New(m.Lookup("Node"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, root, try, &report); !ok || err != nil {
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

// javaCycleOverwritten builds a wire where the root body writes `left = 1`
// (self-edge) followed by `left = 0` (null). Last-wins is retired: the same
// field id twice in one body refuses the whole load (docs/plans/strict-only.md,
// framing R5), whatever the final slot would have held.
func javaCycleOverwritten(t *testing.T, m *tabletext.Model) []byte {
	t.Helper()
	wire := javaCycleEncode(t, m, 2, nil, 0, []javaCycleLink{{0, "left", 1}, {0, "right", 1}})
	pattern := []byte{byte(ir.TableKindPointer), 2}
	first := bytes.Index(wire, pattern)
	if first < 1 {
		t.Fatal("missing first pointer entry")
	}
	leftIdx := wire[first-1]
	second := bytes.LastIndex(wire, pattern)
	if second <= first {
		t.Fatal("missing second pointer entry")
	}
	wire[first+1] = 1
	wire[second-1] = leftIdx
	wire[second+1] = 0
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Node")), wire, &r); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("engine read a repeated pointer field: ok=%v err=%v %+v", ok, err, r)
	}
	return wire
}

func TestJavaCyclicWireIsDamage(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java cycle gate)")
	t.Parallel()
	u, m := javaCycleModel(t, javaCycleSchema)
	_, mw := javaCycleModel(t, javaCycleWriterSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	chain := []javaCycleLink{{0, "left", 1}, {1, "left", 2}, {2, "left", 3}}
	twin := javaCycleEncode(t, mw, 4, nil, 77, chain)
	mid := javaCycleEncode(t, mw, 4, nil, 77, chain)
	javaCyclePatch(t, mid, 4, 2)
	self := javaCycleEncode(t, mw, 4, nil, 77, chain)
	javaCyclePatch(t, self, 4, 3)
	toRoot := javaCycleEncode(t, mw, 4, nil, 77, chain)
	javaCyclePatch(t, toRoot, 4, 1)
	unreached := javaCycleEncode(t, mw, 4, nil, 77, chain)
	javaCyclePatch(t, unreached, 2, 0)
	javaCyclePatch(t, unreached, 4, 2)

	guarded := []javaCycleLink{{0, "left", 1}, {1, "side", 2}}
	guardTwin := javaCycleEncode(t, mw, 3, []int{1}, 77, guarded)
	javaCycleGuardOff(t, mw, guardTwin)
	guardCycle := append([]byte(nil), guardTwin...)
	javaCyclePatch(t, guardCycle, 3, 2)

	// List pointer-slot cycle: root(1).left -> A(2), A.left -> B(3), B.items[0] -> L(4);
	// patching B.items[0] (4) back to A (2) creates a cycle directly through []*Node.
	listLinks := []javaCycleLink{{0, "left", 1}, {1, "left", 2}, {2, "items", 3}}
	listTwin := javaCycleEncode(t, mw, 4, nil, 77, listLinks)
	listCycle := javaCycleEncode(t, mw, 4, nil, 77, listLinks)
	listPat := []byte{byte(ir.TableKindPointer), 1, 4}
	if n := bytes.Count(listCycle, listPat); n != 1 {
		t.Fatalf("list pointer pattern matched %d times, want 1", n)
	}
	listCycle[bytes.Index(listCycle, listPat)+2] = 2

	// Map pointer-slot cycle: root(1).left -> A(2), A.left -> B(3), B.lookup[1] -> L(4);
	// patching B.lookup[1] (4) back to A (2) creates a cycle directly through map[int32]*Node.
	mapLinks := []javaCycleLink{{0, "left", 1}, {1, "left", 2}, {2, "lookup", 3}}
	mapTwin := javaCycleEncode(t, mw, 4, nil, 77, mapLinks)
	mapCycle := javaCycleEncode(t, mw, 4, nil, 77, mapLinks)
	javaCyclePatch(t, mapCycle, 4, 2)

	overwritten := javaCycleOverwritten(t, m)

	const levels = 40
	dlinks := []javaCycleLink{{0, "left", 1}, {0, "right", 2}}
	for l := range levels - 1 {
		a, b := 1+2*l, 2+2*l
		dlinks = append(dlinks,
			javaCycleLink{a, "left", a + 2},
			javaCycleLink{a, "right", b + 2},
			javaCycleLink{b, "left", a + 2},
			javaCycleLink{b, "right", b + 2},
		)
	}
	diamond := javaCycleEncode(t, m, 1+2*levels, nil, 0, dlinks)

	type row struct {
		name string
		wire []byte
		ok   bool
	}
	rows := []row{
		{"twin", twin, true},
		{"mid", mid, false},
		{"self", self, false},
		{"to_root", toRoot, false},
		{"unreached", unreached, false},
		{"guard_twin", guardTwin, true},
		{"guard_cycle", guardCycle, false},
		{"list_twin", listTwin, true},
		{"list_cycle", listCycle, false},
		{"map_twin", mapTwin, true},
		{"map_cycle", mapCycle, false},
		{"overwritten", overwritten, false},
		{"diamond", diamond, true},
	}

	var checks strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&checks, "        check(%q, hex(%q), %v);\n", r.name, hex.EncodeToString(r.wire), r.ok)
	}
	driver := strings.ReplaceAll(javaCycleDriverTemplate, "// CHECKS", checks.String())

	witnesses := []struct {
		name, file, old, new, want string
	}{
		{"intact", "", "", "", ""},
		{"no_cycle_check", "NodeVariable.java", "if (TableVariable.cyclic(nt))", "if (false && TableVariable.cyclic(nt))", "FAILED: mid (load)"},
		{"no_list_cycle", "NodeVariable.java", "for (Long idx : v.itemsIdx) {", "for (Long idx : java.util.Collections.<Long>emptyList()) {", "FAILED: list_cycle (load)"},
		{"no_map_cycle", "NodeVariable.java", "for (NodeLookupEntryVariable.Value elem : v.lookup) {", "for (NodeLookupEntryVariable.Value elem : java.util.Collections.<NodeLookupEntryVariable.Value>emptyList()) {", "FAILED: map_cycle (load)"},
		{"no_retain_clear", "NodeRetain.java", "if (TableVariable.cyclic(nt)) {", "if (TableVariable.cyclic(nt)) { if (true) { report.damaged = true; return null; }", "FAILED: mid (retain not cleared)"},
		{"closed_as_open", "TableVariable.java", "if (state[t] == 1) {", "if (state[t] != 0) {", "FAILED: diamond (load)"},
	}

	for _, w := range witnesses {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			cp := map[string][]byte{}
			maps.Copy(cp, files)
			if w.file != "" {
				src := string(cp[w.file])
				if !strings.Contains(src, w.old) {
					t.Fatalf("%s: missing target in %s", w.name, w.file)
				}
				cp[w.file] = []byte(strings.ReplaceAll(src, w.old, w.new))
			}
			dir := compileCookUnit(t, javac, cp, "cyclecheck", map[string]string{"CycleDriver": driver})
			out, err := exec.Command(javaBin, "-ea", "-cp", dir, "CycleDriver").CombinedOutput()
			if w.file == "" {
				if err != nil {
					t.Fatalf("intact failed: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), w.want) {
				t.Fatalf("%s: expected %q, got err=%v\n%s", w.name, w.want, err, out)
			}
		})
	}
}

const javaCycleDriverTemplate = `import cyclecheck.NodeRetain;
import cyclecheck.NodeVariable;
import cyclecheck.TableRetain;
import cyclecheck.TableVariable;

public final class CycleDriver {
    static byte[] hex(String s) {
        byte[] b = new byte[s.length() / 2];
        for (int i = 0; i < b.length; i++) {
            b[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        }
        return b;
    }

    static void fail(String msg) {
        System.out.println("FAILED: " + msg);
        System.exit(1);
    }

    static void check(String name, byte[] wire, boolean wantOk) {
        TableVariable.Report r = new TableVariable.Report();
        NodeVariable.Value v = NodeVariable.load(wire, r);
        if (wantOk) {
            if (v == null || r.damaged || r.refused) {
                fail(name + " (load)");
            }
        } else {
            if (v != null || !(r.damaged || r.refused)) {
                fail(name + " (load)");
            }
        }

        TableRetain retain = new TableRetain(1 << 16, 1 << 10);
        TableVariable.Report rr = new TableVariable.Report();
        NodeVariable.Value rv = NodeRetain.loadRetain(wire, retain, rr);
        if (wantOk) {
            if (rv == null || rr.damaged || rr.refused) {
                fail(name + " (retain)");
            }
        } else {
            if (rv != null || !(rr.damaged || rr.refused)) {
                fail(name + " (retain)");
            }
            if (retain.used != 0 || retain.idUsed != 0) {
                fail(name + " (retain not cleared)");
            }
        }
    }

    public static void main(String[] args) {
// CHECKS
        System.out.println("ok");
    }
}
`
