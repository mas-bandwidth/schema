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
const jsDiscardWriter = `package discardjs

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
    x     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

// jsDiscardWire encodes the text under the writer's unit and then damages the
// nested body: Inner's one field, kind 4 holding 3, gets the kind 37 no reader
// can skip. The engine is asked first, under the READER's unit, so the vector
// is damaged by the rule and not by assumption. It answers the clean wire too.
func jsDiscardWire(t *testing.T, writer, reader *ir.Unit, root, text string) (clean, damaged []byte) {
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
	rm := tabletext.NewModel(reader)
	var report tabletext.Report
	if ok, err := tablewire.Decode(rm, rm.New(rm.Lookup(root)), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return clean, wire
}

// jsDiscardRun writes the unit's variable module and runs probe beside it
// under the leg's own node; probe imports the module as `m`.
func jsDiscardRun(t *testing.T, u *ir.Unit, files map[string][]byte, probe string) {
	t.Helper()
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
// damage hands back nothing to use and its report says malformed.
//
//   - nested: damage inside a nested body, with whole fields before and after
//     it, makes the fixed root's and the variable root's Load answer null, the
//     retaining LoadRetain answer null with its store empty (an unknown field
//     it had kept before the damage is gone), and FromJson answer null on text
//     that does not read. The same wires undamaged load, so a reader that
//     refused everything would be red here too.
//   - framing: every `framing` row of the conformance manifest whose verdict is
//     malformed (docs/LAYERS/1-framing.md), through Load and LoadRetain of the
//     row's root, answers null with the store empty.
//
// The message batch's half is TestMessageFormBatchJavaScript/damage: a damaged
// batch delivers no body. Red if any read hands back what it decoded.
func TestDamageDiscardsTheWholeLoadJavaScript(t *testing.T) {
	t.Parallel()

	t.Run("nested", func(t *testing.T) {
		t.Parallel()
		reader := loadSchemaString(t, "discard.schema", jsDiscardReader)
		writer := loadSchemaString(t, "discard.schema", jsDiscardWriter)
		fixedClean, fixedDamaged := jsDiscardWire(t, writer, reader, "Root", `{"a":5,"inner":{"n":3},"b":9}`)
		graphClean, graphDamaged := jsDiscardWire(t, writer, reader, "Graph", `{"a":5,"x":7,"inner":{"n":3},"b":9}`)
		jsDiscardRun(t, reader, map[string][]byte{
			"fixed.bin": fixedClean, "fixed-damaged.bin": fixedDamaged,
			"graph.bin": graphClean, "graph-damaged.bin": graphDamaged,
		}, `
const wire = (name) => new Uint8Array(readFileSync(name));
for (const root of ["Root", "Graph"]) {
  const base = root === "Root" ? "fixed" : "graph";
  const good = new m.TableVariableReport();
  const gv = m[root + "Load"](wire(base + ".bin"), good);
  if (gv === null || good.malformed || gv.a !== 5 || gv.b !== 9 || gv.inner.n !== 3) {
    fail(root + ": the clean wire did not load: " + JSON.stringify(good));
  }
  const r = new m.TableVariableReport();
  const v = m[root + "Load"](wire(base + "-damaged.bin"), r);
  if (v !== null) { fail(root + ": Load handed back a value on damage: a=" + v.a + " b=" + v.b); }
  if (!r.malformed) { fail(root + ": Load's report does not say malformed"); }
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 12), new Array(64));
  const rr = new m.TableVariableReport();
  const rv = m[root + "LoadRetain"](wire(base + "-damaged.bin"), retain, rr);
  if (rv !== null) { fail(root + ": LoadRetain handed back a value on damage"); }
  if (!rr.malformed || retain.used !== 0 || retain.count !== 0) {
    fail(root + ": LoadRetain kept a store on damage: malformed=" + rr.malformed + " used=" + retain.used + " count=" + retain.count);
  }
  const tr = new m.TableVariableReport();
  const tv = m[root + "FromJson"]('{"a":5,"inner":{"n":3},"b":9', tr);
  if (tv !== null) { fail(root + ": FromJson handed back a value on text that does not read"); }
  if (!tr.malformed) { fail(root + ": FromJson's report does not say malformed"); }
}
// the retaining read keeps the unknown field on the clean wire, so an empty
// store after the damaged read is the discard and not a store never filled
const kept = new m.TableVariableRetain(new Uint8Array(1 << 12), new Array(64));
const kr = new m.TableVariableReport();
if (m.GraphLoadRetain(wire("graph.bin"), kept, kr) === null || kept.count === 0) {
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
			if len(fs) < 6 || fs[0] != "framing" || fs[5] != "malformed" {
				continue
			}
			if _, ok := units[fs[2]]; !ok {
				t.Fatalf("framing row %s names unit %s, which this gate does not load: add it", fs[1], fs[2])
			}
			rows[fs[2]] = append(rows[fs[2]], row{fs[1], fs[3], filepath.Join(repo, fs[4])})
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatal("the manifest holds no malformed framing row")
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
  if (v !== null) { fail(row.case + ": Load handed back a value on damage"); }
  if (!r.malformed) { fail(row.case + ": Load's report does not say malformed"); }
  const retain = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
  const rr = new m.TableVariableReport();
  const rv = m[row.root + "LoadRetain"](wire, retain, rr);
  if (rv !== null) { fail(row.case + ": LoadRetain handed back a value on damage"); }
  if (!rr.malformed || retain.used !== 0 || retain.count !== 0) {
    fail(row.case + ": LoadRetain kept a store on damage: malformed=" + rr.malformed + " used=" + retain.used + " count=" + retain.count);
  }
}
`, js))
			})
		}
	})
}
