package jstable

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsDiscardReader has a fixed root and a variable root, each carrying a nested
// body between two scalars, so damage inside the nested body leaves the walk
// able to read on past its L: the case where a reader that kept what it
// decoded would hand back a value.
const jsDiscardReader = `package discardjs

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

// jsDiscardWriter is the reader's unit one build later: Graph gained a field
// ahead of the damage, so a retaining read has an unknown field in its store
// by the time it meets the damage, and a discard that forgets the store shows.
// Root is a plain table here even though the reader declares it fixed: a fixed
// root's writer emits form 3 (§3.4), and this gate damages the variable form's
// nested body, which is the form the reader's variable loader reads.
const jsDiscardWriter = `package discardjs

fixed table Inner
{
    n int32 = 0
}

table Root
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
}

table Graph
{
    a     int32 = 0
    x     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

// jsDiscardWire encodes the text under the writer's unit and then damages the
// nested body: Inner's one field, kind 4 holding 3, gets the kind 37 no reader
// can skip. The engine is asked first, under the writer's unit, so the vector
// is damaged by the rule and not by assumption. It answers the clean wire too.
func jsDiscardWire(t *testing.T, writer *ir.Unit, root, text string) (clean, damaged []byte) {
	t.Helper()
	m := tabletext.NewModel(writer)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	clean = bytes.Clone(wire)
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	rm := tabletext.NewModel(writer)
	var report tabletext.Report
	if ok, err := tablewire.Decode(rm, rm.New(rm.Lookup(root)), wire, &report); ok || !tablewire.ReadRefusal(err) {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return clean, wire
}

// jsDiscardRun writes the unit's variable module and runs probe beside it
// under the leg's own node; probe imports the module as `m`.
func jsDiscardRun(t *testing.T, u *ir.Unit, files map[string][]byte, probe string) {
	t.Helper()
	slowtest.Gate(t, "node (it runs the generated module)")
	gen, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	module := runtimeHome(u) + "Variable.js"
	data, ok := gen[module]
	if !ok {
		t.Fatalf("no variable module %s generated", module)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.js"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := `import { readFileSync } from "node:fs";
import * as m from "./module.js";
let failures = 0;
function fail(why) { console.log("FAIL: " + why); failures++; }
` + probe + `
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(jsNode(t), "probe.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestDamageDiscardsTheWholeLoadJavaScript is the JavaScript leg's gate for
// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): a read that meets
// damage hands back nothing to use and its report refuses the read.
//
//   - nested: damage inside a nested body, with whole fields before and after
//     it, makes both roots' Load answer null, the retaining LoadRetain answer
//     null with its store empty (an unknown field it had kept before the
//     damage is gone), and FromJson answer null on text that does not read.
//     The same wires undamaged load, so a reader that refused everything would
//     be red here too.
//   - framing: every `framing` row of the conformance manifest whose verdict is
//     refused (docs/LAYERS/1-framing.md) and whose bytes carry the variable
//     form, through Load and LoadRetain of the row's root, answers null with
//     the store empty. A row whose first byte is another form is the form
//     gate's, not this one's.
//
// The message batch's half is TestMessageFormBatchJavaScript/damage: a damaged
// batch delivers no body. Red if any read hands back what it decoded.
func TestDamageDiscardsTheWholeLoadJavaScript(t *testing.T) {
	slowtest.Gate(t, "node (whole read discard)")
	t.Parallel()

	t.Run("nested", func(t *testing.T) {
		t.Parallel()
		reader := loadSchemaString(t, "discard.schema", jsDiscardReader)
		writer := loadSchemaString(t, "discard.schema", jsDiscardWriter)
		fixedClean, fixedDamaged := jsDiscardWire(t, writer, "Root", `{"a":5,"inner":{"n":3},"b":9}`)
		graphClean, graphDamaged := jsDiscardWire(t, writer, "Graph", `{"a":5,"x":7,"inner":{"n":3},"b":9}`)
		jsDiscardRun(t, reader, map[string][]byte{
			"fixed.bin": fixedClean, "fixed-damaged.bin": fixedDamaged,
			"graph.bin": graphClean, "graph-damaged.bin": graphDamaged,
		}, `
const wire = (name) => new Uint8Array(readFileSync(name));
for (const root of ["Root", "Graph"]) {
  const base = root === "Root" ? "fixed" : "graph";
  const good = new m.TableVariableReport();
  const gv = m[root + "Load"](wire(base + ".bin"), good);
  if (gv === null || good.err() !== null || gv.a !== 5 || gv.b !== 9 || gv.inner.n !== 3) {
    fail(root + ": the clean wire did not load: " + JSON.stringify(good));
  }
  const r = new m.TableVariableReport();
  const v = m[root + "Load"](wire(base + "-damaged.bin"), r);
  if (v !== null) { fail(root + ": Load handed back a value on damage: a=" + v.a + " b=" + v.b); }
  if (r.err() === null) { fail(root + ": Load's report does not refuse the read"); }
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 12), new Array(64));
  const rr = new m.TableVariableReport();
  const rv = m[root + "LoadRetain"](wire(base + "-damaged.bin"), retain, rr);
  if (rv !== null) { fail(root + ": LoadRetain handed back a value on damage"); }
  if (rr.err() === null || retain.used !== 0 || retain.count !== 0) {
    fail(root + ": LoadRetain kept a store on damage: refused=" + (rr.err() !== null) + " used=" + retain.used + " count=" + retain.count);
  }
  const tr = new m.TableVariableReport();
  const tv = m[root + "FromJson"]('{"a":5,"inner":{"n":3},"b":9', tr);
  if (tv !== null) { fail(root + ": FromJson handed back a value on text that does not read"); }
  if (tr.err() === null) { fail(root + ": FromJson's report does not refuse the read"); }
}
// the retaining read keeps the unknown field on the clean wire, so an empty
// store after the damaged read is the discard and not a store never filled
const kept = new m.TableVariableRetain(new Uint8Array(1 << 12), new Array(64));
const kr = new m.TableVariableReport();
if (m.GraphLoadRetain(wire("graph.bin"), kept, kr) === null || kept.count === 0 || kr.err() !== null) {
  fail("Graph: the clean wire's retaining read kept nothing: count=" + kept.count + " " + JSON.stringify(kr));
}
`)
	})

	t.Run("framing", func(t *testing.T) {
		t.Parallel()
		repo, err := filepath.Abs("../../..")
		if err != nil {
			t.Fatal(err)
		}
		units := map[string]string{
			"tblr1":     "test/tables/R1.schema",
			"graphdemo": "tables/pointers",
			"listdemo":  "tables/lists",
		}
		type row struct {
			Case string `json:"case"`
			Root string `json:"root"`
			Wire string `json:"wire"`
		}
		rows := map[string][]row{}
		f, err := os.Open(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 6 || fs[0] != "framing" || fs[5] != "refused" {
				continue
			}
			if _, ok := units[fs[2]]; !ok {
				t.Fatalf("framing row %s names unit %s, which this gate does not load: add it", fs[1], fs[2])
			}
			// This gate walks the VARIABLE form: a row whose first byte names
			// another form is the form gate's, and a fixed-class root answers
			// its defaults there rather than discarding (§3).
			wirePath := filepath.Join(repo, fs[4])
			first, err := os.ReadFile(wirePath)
			if err != nil {
				t.Fatal(err)
			}
			if len(first) != 0 && first[0] != 1 {
				continue
			}
			rows[fs[2]] = append(rows[fs[2]], row{fs[1], fs[3], wirePath})
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatal("the manifest holds no refused framing row through the variable form")
		}
		names := make([]string, 0, len(rows))
		for n := range rows {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			list := rows[name]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				u := loadUnitForMeasure(t, repo, units[name])
				if u.Package != name {
					t.Fatalf("%s loads package %s", units[name], u.Package)
				}
				js, err := json.Marshal(list)
				if err != nil {
					t.Fatal(err)
				}
				jsDiscardRun(t, u, nil, fmt.Sprintf(`
for (const row of %s) {
  const wire = new Uint8Array(readFileSync(row.wire));
  const r = new m.TableVariableReport();
  const v = m[row.root + "Load"](wire, r);
  if (v !== null) { fail(row.case + ": Load handed back a value on a refused wire"); }
  if (r.err() === null) { fail(row.case + ": Load's report does not refuse the read"); }
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const rr = new m.TableVariableReport();
  const rv = m[row.root + "LoadRetain"](wire, retain, rr);
  if (rv !== null) { fail(row.case + ": LoadRetain handed back a value on a refused wire"); }
  if (rr.err() === null || retain.used !== 0 || retain.count !== 0) {
    fail(row.case + ": LoadRetain kept a store on a refused wire: refused=" + (rr.err() !== null) + " used=" + retain.used + " count=" + retain.count);
  }
}
`, js))
			})
		}
	})
}

// TestCyclicWireIsDamageJavaScript is the JavaScript leg's gate for A CYCLIC
// WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4; the owner, 2026-10-31): the
// manifest's cycle rows (a node naming itself, a node naming the root, a cycle
// in the middle of a chain) load as nothing through Load and LoadRetain with
// the report refusing the read, and the two acyclic rows beside them load: a
// chain, and a diamond chain of forty levels whose 2^40 paths a walk that
// re-entered shared nodes would never finish.
func TestCyclicWireIsDamageJavaScript(t *testing.T) {
	t.Parallel()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{ // case -> it must load
		"node_cycle_self":    false,
		"node_cycle_to_root": false,
		"node_cycle_mid":     false,
		"node_chain_two":     true,
		"node_diamond_chain": true,
	}
	type row struct {
		Case string `json:"case"`
		Root string `json:"root"`
		Wire string `json:"wire"`
		Load bool   `json:"load"`
	}
	var rows []row
	f, err := os.Open(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 6 || fs[0] != "framing" {
			continue
		}
		load, ok := cases[fs[1]]
		if !ok {
			continue
		}
		if fs[2] != "graphdemo" {
			t.Fatalf("%s names unit %s, not graphdemo", fs[1], fs[2])
		}
		// THE VERDICT IS THE WHOLE CONTRACT (docs/rulings.json
		// refusal-verdict-only): a cycle row is refused, and why is this
		// leg's own diagnostic.
		if want := "refused"; !load && fs[5] != want {
			t.Fatalf("%s: the manifest's verdict is %s, not %s", fs[1], fs[5], want)
		}
		rows = append(rows, row{fs[1], fs[3], filepath.Join(repo, fs[4]), load})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(cases) {
		t.Fatalf("the manifest holds %d of the %d cycle rows", len(rows), len(cases))
	}
	js, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitForMeasure(t, repo, "tables/pointers")
	// THE OVERWRITTEN SELF-EDGE: the root body names `next` twice, first as
	// the root itself and then as null, and the node record's own self-edge
	// is nulled. Its verdict is the Go engine's (a repeated field id is
	// damage under strict-only, docs/plans/strict-only.md decision 5), so the
	// probe below owes exactly what the oracle answers.
	var selfWire string
	for _, r := range rows {
		if r.Case == "node_cycle_self" {
			selfWire = r.Wire
		}
	}
	base, err := os.ReadFile(selfWire)
	if err != nil {
		t.Fatal(err)
	}
	over := make([]byte, 0, len(base)+3)
	over = append(over, base[:7]...)
	over = append(over, 3, 17, 1, 3, 17, 0)
	over = append(over, base[10:]...)
	for i := 13; i+2 < len(over); i++ {
		if over[i] == 3 && over[i+1] == 17 && over[i+2] == 2 {
			over[i+2] = 0
			break
		}
	}
	gm := tabletext.NewModel(u)
	var grep tabletext.Report
	overLoads, err := tablewire.Decode(gm, gm.New(gm.Lookup("ListNode")), over, &grep)
	if err != nil && !tablewire.ReadRefusal(err) {
		t.Fatalf("overwritten_self_cycle: the oracle failed: %v", err)
	}
	overLoads = overLoads && err == nil
	jsDiscardRun(t, u, map[string][]byte{"overwritten_self_cycle.bin": over}, fmt.Sprintf(`
for (const row of %s) {
  const wire = new Uint8Array(readFileSync(row.wire));
  const r = new m.TableVariableReport();
  const v = m[row.root + "Load"](wire, r);
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const rr = new m.TableVariableReport();
  const rv = m[row.root + "LoadRetain"](wire, retain, rr);
  if (row.load) {
    if (v === null || r.err() !== null) { fail(row.case + ": an acyclic wire did not load: " + JSON.stringify(r)); }
    if (rv === null || rr.err() !== null) { fail(row.case + ": an acyclic wire did not load retained: " + JSON.stringify(rr)); }
    continue;
  }
  if (v !== null || r.err() === null) { fail(row.case + ": a cyclic wire loaded (reason=" + r.err() + ")"); }
  if (rv !== null || rr.err() === null || retain.used !== 0 || retain.count !== 0) {
    fail(row.case + ": a cyclic wire loaded retained (reason=" + rr.err() + " used=" + retain.used + ")");
  }
}
// Unreached cycle: zero the root's next pointer (byte 9: 2 -> 0) in node_cycle_mid
// so records 2 and 3 form a cycle the root never reaches (§3.1).
{
  const midRow = (%s).find(x => x.case === "node_cycle_mid");
  const unreached = new Uint8Array(readFileSync(midRow.wire));
  unreached[9] = 0;
  const r = new m.TableVariableReport();
  const v = m.ListNodeLoad(unreached, r);
  if (v !== null || r.err() === null) { fail("unreached_cycle: a cyclic wire loaded (reason=" + r.err() + ")"); }
}
// Overwritten self-edge in the same body (built and judged by the Go engine
// above): this leg owes the oracle's verdict, and a read that returns holds
// next === null, the final stored slot (§3.1).
{
  const over = new Uint8Array(readFileSync("overwritten_self_cycle.bin"));
  const r = new m.TableVariableReport();
  const v = m.ListNodeLoad(over, r);
  if (%t) {
    if (v === null || r.err() !== null || v.next !== null) {
      fail("overwritten_self_cycle: refused or next !== null: " + JSON.stringify(r));
    }
  } else if (v !== null || r.err() === null) {
    fail("overwritten_self_cycle: a repeated field id loaded (the oracle refuses it)");
  }
}
`, js, js, overLoads))

	// List, map, and false-guard pointer-slot cycles (§2.8, §2.9, §3.1).
	cu := loadSchemaString(t, "cycle.schema", `package cyclecheck
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
`)
	cm := tabletext.NewModel(cu)
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
	jsDiscardRun(t, cu, map[string][]byte{
		"list-twin.bin": listTwin, "list-cycle.bin": listCycle,
		"map-twin.bin": mapTwin, "map-cycle.bin": mapCycle,
		"guard-twin.bin": guardTwin, "guard-cycle.bin": guardCycle,
	}, `
for (const [name, wantLoad] of [
  ["list-twin.bin", true], ["list-cycle.bin", false],
  ["map-twin.bin", true], ["map-cycle.bin", false],
  ["guard-twin.bin", true], ["guard-cycle.bin", false],
]) {
  const wire = new Uint8Array(readFileSync(name));
  const r = new m.TableVariableReport();
  const v = m.NodeLoad(wire, r);
  if (wantLoad) {
    if (v === null || r.err() !== null) { fail(name + ": did not load: " + JSON.stringify(r)); }
  } else {
    if (v !== null || r.err() === null) { fail(name + ": a cyclic wire loaded (reason=" + r.err() + ")"); }
  }
}
`)
}
