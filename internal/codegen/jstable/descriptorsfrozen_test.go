package jstable

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaScriptDescriptorsAreConstants holds docs/PORTING.md M13 on the
// JavaScript variable form: the reflection and codec descriptors are constants
// once the module has loaded. The variable codecs and unions are linked at load
// (TableVariableLink fills each table reference, the by-id and by-key lookups
// and the quoted text keys) and then frozen whole; the exported
// <Name>TableType, <Package>UnitRegistry and TableRetainKnown are frozen as
// they are made.
//
// The probe imports the generated module with one line appended that exports
// the module-private codec and union tables, then walks every object, array
// and Map reachable from them and from the exported descriptors and requires
// each to be frozen, and a write into a field descriptor to throw (an ES module
// is strict). The arms probe unit is the corpus: maps of each key kind, unions
// with table and direct arms, optionals, enum-keyed arrays and pointers, so
// every descriptor shape the link fills is reached.
//
// THE REVERSED WITNESS removes the two freeze calls after the link and requires
// the walk to find a mutable descriptor, so this test cannot pass over a module
// that leaves the codec graph open.
func TestJavaScriptDescriptorsAreConstants(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it imports the generated module)")
	node := jsNode(t)
	work := t.TempDir()

	probe := unitFrom(t, mapsArmsProbeSrc)
	files, err := generateVariableFiles(probe, nil)
	if err != nil {
		t.Fatalf("the variable form was refused: %v", err)
	}
	home := runtimeHome(probe) + "Variable.js"
	module, ok := files[home]
	if !ok {
		t.Fatal("no variable module was generated")
	}
	const freezeLines = "TableVariableFreeze(TableVariableCodecs);\nTableVariableFreeze(TableVariableUnions);\n"
	if bytes.Count(module, []byte(freezeLines)) != 1 {
		t.Fatal("the module does not freeze the codec and union tables after the link exactly once: the witness below would plant nothing")
	}
	const expose = "\nexport const TableProbeCodecs = TableVariableCodecs;\nexport const TableProbeUnions = TableVariableUnions;\n"

	run := func(name string, data []byte) string {
		t.Helper()
		dir := filepath.Join(work, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, home)
		if err := os.WriteFile(path, append(append([]byte{}, data...), expose...), 0o600); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(dir, "probe.mjs")
		if err := os.WriteFile(script, []byte(descriptorsFrozenDriver), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, script, path)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the descriptor probe did not run: %v\n%s", name, err, out)
		}
		t.Logf("%s probe output:\n%s", name, out)
		return string(out)
	}

	out := run("frozen", module)
	if !strings.Contains(out, "FROZEN OK") {
		t.Errorf("a descriptor is mutable after module load:\n%s", out)
	}
	if !strings.Contains(out, "WRITE THROWS") {
		t.Errorf("a write into a field descriptor did not throw:\n%s", out)
	}

	open := run("open", bytes.Replace(module, []byte(freezeLines), nil, 1))
	if strings.Contains(open, "FROZEN OK") {
		t.Error("REVERSED WITNESS: with the freeze removed the walk still found every descriptor frozen, so this test does not hold M13")
	}
}

// descriptorsFrozenDriver walks every descriptor reachable from the module's
// codec and union tables and its exported reflection, and reports the first
// one that is not frozen.
const descriptorsFrozenDriver = `
import { pathToFileURL } from "node:url";

const m = await import(pathToFileURL(process.argv[2]).href);
const roots = { codecs: m.TableProbeCodecs, unions: m.TableProbeUnions, known: m.TableRetainKnown };
for (const k of Object.keys(m)) {
  if (k.endsWith("TableType") || k.endsWith("UnitRegistry")) { roots[k] = m[k]; }
}
const seen = new Set();
let open = null, count = 0;
function walk(x, path) {
  if (open !== null || x === null || typeof x !== "object" || ArrayBuffer.isView(x) || seen.has(x)) { return; }
  seen.add(x);
  count++;
  if (!Object.isFrozen(x)) { open = path; return; }
  if (x instanceof Map) {
    for (const [k, v] of x) { walk(v, path + ".get(" + String(k) + ")"); }
    return;
  }
  for (const k of Object.keys(x)) { walk(x[k], path + "." + k); }
}
for (const [name, root] of Object.entries(roots)) { walk(root, name); }
console.log(open === null ? "FROZEN OK " + count + " objects" : "OPEN " + open);
try {
  m.TableProbeCodecs.Probe.fields[0].id = 0;
  console.log("WRITE ACCEPTED");
} catch (e) {
  console.log("WRITE THROWS " + e.constructor.name);
}
`
