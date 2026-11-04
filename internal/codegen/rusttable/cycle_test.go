package rusttable

import (
	"bytes"
	"fmt"
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

// TestCyclicWireIsDamageRust holds docs/SPEC-TABLES.md §3.1's ruling on the
// Rust leg: a cyclic wire is damage, and every read refuses the whole load
// (Glenn, 2026-10-31). The framing vectors node_cycle_self, node_cycle_to_root
// and node_cycle_mid, plus an unreached cycle, are read through graphdemo's
// ListNode load and load_retain, and each must report malformed and hand back
// None with both retention stores empty; an overwritten self-edge in the same
// body loads clean; node_chain_two and node_diamond_chain (Scene, 82 shared
// nodes and 2^40 paths) must load clean, the diamond inside a linear-pass
// deadline; and pointer cycles through list elements, map values, and false-
// guard slots are refused while their acyclic twins load clean.
func TestCyclicWireIsDamageRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	out, err := runRustCycleProbe(t, nil)
	if err != nil {
		t.Fatalf("the cycle probe failed: %v\n%s", err, out)
	}
	runRustCycleSlotKindsProbe(t)
}

// TestCyclicWireIsDamageRustNegativeControl is the gate's reversed witness:
// disabling the cycle refusal in the generated codec must turn the cyclic rows
// red while leaving the acyclic rows green.
func TestCyclicWireIsDamageRustNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	const sig = "if report.cycle_on_save { report.malformed = true; return None; }"
	out, err := runRustCycleProbe(t, func(src string) string {
		return strings.ReplaceAll(src, sig, "if false && report.cycle_on_save { report.malformed = true; return None; } // SABOTAGED")
	})
	if err == nil {
		t.Fatalf("the probe stayed green with cycle refusal disabled: the gate is blind\n%s", out)
	}
	for _, row := range []string{"node_cycle_self", "node_cycle_to_root", "node_cycle_mid", "unreached_cycle"} {
		if !strings.Contains(out, "FAIL: "+row) {
			t.Errorf("the reversed witness did not fail on %s:\n%s", row, out)
		}
	}
	for _, row := range []string{"node_chain_two", "node_diamond_chain", "overwritten_self_cycle"} {
		if strings.Contains(out, "FAIL: "+row) {
			t.Errorf("the reversed witness failed on the acyclic %s too:\n%s", row, out)
		}
	}
}

func runRustCycleProbe(t *testing.T, edit func(string) string) (string, error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	u := vcLoadUnit(t, root, "tables/pointers")
	framingDir := filepath.Join(root, "testdata", "wire", "tables", "framing")
	readFraming := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(framingDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	selfWire := readFraming("node_cycle_self")
	toRootWire := readFraming("node_cycle_to_root")
	midWire := readFraming("node_cycle_mid")
	chainTwoWire := readFraming("node_chain_two")
	diamondWire := readFraming("node_diamond_chain")

	unreachedWire := bytes.Clone(midWire)
	unreachedWire[9] = 0
	overwrittenWire := make([]byte, len(selfWire)+3)
	copy(overwrittenWire[:7], selfWire[:7])
	copy(overwrittenWire[7:13], []byte{3, 17, 1, 3, 17, 0})
	copy(overwrittenWire[13:], selfWire[10:])
	for i := 13; i+2 < len(overwrittenWire); i++ {
		if overwrittenWire[i] == 3 && overwrittenWire[i+1] == 17 && overwrittenWire[i+2] == 2 {
			overwrittenWire[i+2] = 0
			break
		}
	}

	body := fmt.Sprintf(`
    const SELF_WIRE: [u8; %d] = [%s];
    const TO_ROOT_WIRE: [u8; %d] = [%s];
    const MID_WIRE: [u8; %d] = [%s];
    const UNREACHED_WIRE: [u8; %d] = [%s];
    const OVERWRITTEN_WIRE: [u8; %d] = [%s];
    const CHAIN_TWO_WIRE: [u8; %d] = [%s];
    const DIAMOND_WIRE: [u8; %d] = [%s];

    let mut failed = false;
    let mut check = |ok: bool, what: &str| {
        if !ok {
            println!("FAIL: {what}");
            failed = true;
        }
    };

    for (row, wire) in [
        ("node_cycle_self", &SELF_WIRE[..]),
        ("node_cycle_to_root", &TO_ROOT_WIRE[..]),
        ("node_cycle_mid", &MID_WIRE[..]),
        ("unreached_cycle", &UNREACHED_WIRE[..]),
    ] {
        let mut r = TableReport::default();
        let v = list_node_load(wire, &mut r);
        check(r.malformed && r.cycle_on_save && v.is_none(), &format!("{row}: load did not refuse cycle as damage"));
        let v_after = list_node_load(&CHAIN_TWO_WIRE, &mut r);
        check(
            r.malformed && r.cycle_on_save && v_after.as_ref().is_some_and(|n| n.next.is_some()),
            &format!("{row}: reused report after cycle refusal poisoned subsequent valid CHAIN_TWO_WIRE load"),
        );
        let mut r_clean = TableReport { cycle_on_save: true, ..TableReport::default() };
        let v_clean = list_node_load(&CHAIN_TWO_WIRE, &mut r_clean);
        check(
            !r_clean.malformed && r_clean.cycle_on_save && v_clean.as_ref().is_some_and(|n| n.next.is_some()),
            &format!("{row}: prior cycle_on_save with malformed=false poisoned subsequent valid CHAIN_TWO_WIRE load"),
        );

        let mut rr = TableReport::default();
        let mut store = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(1024) };
        let rv = list_node_load_retain(wire, &mut store, &mut rr);
        check(rr.malformed && rr.cycle_on_save && rv.is_none(), &format!("{row}: retaining load did not refuse cycle as damage"));
        check(store.bytes.is_empty() && store.ids.is_empty(), &format!("{row}: retaining store was not cleared on cycle refusal"));
        let rv_after = list_node_load_retain(&CHAIN_TWO_WIRE, &mut store, &mut rr);
        check(
            rr.malformed && rr.cycle_on_save && rv_after.as_ref().is_some_and(|n| n.next.is_some()),
            &format!("{row}: reused report after retaining cycle refusal poisoned subsequent valid CHAIN_TWO_WIRE load"),
        );
    }

    {
        let mut r = TableReport::default();
        let v = list_node_load(&OVERWRITTEN_WIRE, &mut r);
        check(!r.malformed && v.as_ref().is_some_and(|n| n.next.is_none()), "overwritten_self_cycle: overwritten self-edge should load clean");
    }

    {
        let mut r = TableReport::default();
        let v = list_node_load(&CHAIN_TWO_WIRE, &mut r);
        check(!r.malformed && v.as_ref().is_some_and(|n| n.next.is_some()), "node_chain_two: plain load failed");
        let mut rr = TableReport::default();
        let mut store = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(1024) };
        let rv = list_node_load_retain(&CHAIN_TWO_WIRE, &mut store, &mut rr);
        check(!rr.malformed && rv.as_ref().is_some_and(|n| n.next.is_some()), "node_chain_two: retaining load failed");
    }

    {
        let start = std::time::Instant::now();
        let mut r = TableReport::default();
        let v = scene_load(&DIAMOND_WIRE, &mut r);
        check(!r.malformed && v.is_some(), "node_diamond_chain: diamond chain reported damage");
        check(start.elapsed().as_millis() < 10_000, "node_diamond_chain: exceeded 10s linear deadline");
    }

    if failed {
        std::process::exit(1);
    }
    println!("CYCLE ok");
`,
		len(selfWire), rustBytes(selfWire),
		len(toRootWire), rustBytes(toRootWire),
		len(midWire), rustBytes(midWire),
		len(unreachedWire), rustBytes(unreachedWire),
		len(overwrittenWire), rustBytes(overwrittenWire),
		len(chainTwoWire), rustBytes(chainTwoWire),
		len(diamondWire), rustBytes(diamondWire),
	)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		patched := 0
		for name, data := range files {
			if strings.HasSuffix(name, "_table.rs") {
				s := string(data)
				ns := edit(s)
				if ns != s {
					files[name] = []byte(ns)
					patched++
				}
			}
		}
		if patched == 0 {
			t.Fatal("the sabotage matched nothing in generated _table.rs files")
		}
	}
	return runRustGeneratedFilesProbe(t, files, body)
}

func runRustGeneratedFilesProbe(t *testing.T, files map[string][]byte, body string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" || name == TableEnumsModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs") ||
			strings.HasSuffix(name, "_table.rs") || strings.HasSuffix(name, "_variable.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables, unused_mut)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString("\nfn main() {\n")
	main.WriteString(body)
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	if out, err := exec.Command("rustc", "--edition=2024", "-O", filepath.Join(dir, "main.rs"), "-o", bin).CombinedOutput(); err != nil {
		return string(out), err
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

const rustCycleSlotKindsSchema = `package cyclecheck
table Root
{
    head *Node
}
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

func runRustCycleSlotKindsProbe(t *testing.T) {
	t.Helper()
	u := unitFrom(t, rustCycleSlotKindsSchema)
	cm := tabletext.NewModel(u)
	makeGraph := func(kind string) ([]byte, []byte) {
		nodes := make([]*tabletext.Instance, 4)
		for i := range nodes {
			nodes[i] = cm.New(cm.Lookup("Node"))
		}
		f0, _ := nodes[0].FieldByKey("left")
		f0.Cell.Node = nodes[1]
		f1, _ := nodes[1].FieldByKey("left")
		f1.Cell.Node = nodes[2]
		switch kind {
		case "list":
			f2, _ := nodes[2].FieldByKey("items")
			f2.Elems = []tabletext.Cell{{Node: nodes[3]}}
			f2.Count = 1
		case "map":
			f2, _ := nodes[2].FieldByKey("lookup")
			ent := cm.NewMapEntry(f2.Def)
			ent.Fields[0].Cell.I = 1
			ent.Fields[1].Cell.Node = nodes[3]
			f2.Entries = []tabletext.Cell{{Tab: ent}}
		case "guard":
			on, _ := nodes[2].FieldByKey("on")
			on.Cell.B = true
			f2, _ := nodes[2].FieldByKey("side")
			f2.Cell.Node = nodes[3]
		}
		twin, err := tablewire.Encode(cm, nodes[0])
		if err != nil {
			t.Fatal(err)
		}
		if kind == "guard" {
			for i := 0; i+2 < len(twin); i++ {
				if twin[i+1] == byte(ir.TableKindBool) && twin[i+2] == 1 {
					try := bytes.Clone(twin)
					try[i+2] = 0
					inst := cm.New(cm.Lookup("Node"))
					var rep tabletext.Report
					if ok, err := tablewire.Decode(cm, inst, try, &rep); ok && err == nil {
						twin = try
						break
					}
				}
			}
		}
		cyc := bytes.Clone(twin)
		if kind == "list" {
			pat := []byte{byte(ir.TableKindPointer), 1, 4}
			if bytes.Count(cyc, pat) != 1 {
				t.Fatalf("list pattern count != 1")
			}
			cyc[bytes.Index(cyc, pat)+2] = 2
		} else {
			pat := []byte{byte(ir.TableKindPointer), 4, 0}
			if bytes.Count(cyc, pat) != 1 {
				t.Fatalf("%s pattern count != 1", kind)
			}
			cyc[bytes.Index(cyc, pat)+1] = 2
		}
		return twin, cyc
	}
	listTwin, listCycle := makeGraph("list")
	mapTwin, mapCycle := makeGraph("map")
	guardTwin, guardCycle := makeGraph("guard")
	body := fmt.Sprintf(`
    const LIST_TWIN: [u8; %d] = [%s];
    const LIST_CYCLE: [u8; %d] = [%s];
    const MAP_TWIN: [u8; %d] = [%s];
    const MAP_CYCLE: [u8; %d] = [%s];
    const GUARD_TWIN: [u8; %d] = [%s];
    const GUARD_CYCLE: [u8; %d] = [%s];

    for (name, wire, want_load) in [
        ("list-twin", &LIST_TWIN[..], true),
        ("list-cycle", &LIST_CYCLE[..], false),
        ("map-twin", &MAP_TWIN[..], true),
        ("map-cycle", &MAP_CYCLE[..], false),
        ("guard-twin", &GUARD_TWIN[..], true),
        ("guard-cycle", &GUARD_CYCLE[..], false),
    ] {
        let mut r = TableReport::default();
        let v = node_load(wire, &mut r);
        if want_load {
            if r.malformed || v.as_ref().is_none_or(|n| n.left.is_none()) {
                eprintln!("FAIL: {name} did not load (malformed={})", r.malformed);
                std::process::exit(1);
            }
        } else if !r.malformed || v.is_some() {
            eprintln!("FAIL: {name} loaded (malformed={})", r.malformed);
            std::process::exit(1);
        }
    }
`,
		len(listTwin), rustBytes(listTwin),
		len(listCycle), rustBytes(listCycle),
		len(mapTwin), rustBytes(mapTwin),
		len(mapCycle), rustBytes(mapCycle),
		len(guardTwin), rustBytes(guardTwin),
		len(guardCycle), rustBytes(guardCycle),
	)
	out, err := runRustDiscardProbe(t, u, body)
	if err != nil {
		t.Fatalf("slot kinds cycle probe failed: %v\n%s", err, out)
	}
}
