package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsRefusedFormProbeSchema has one fixed-class root and one variable-class
// root, so the same form byte is read through each root's variable-form loader
// (docs/SPEC-TABLES.md §3, §4, §6.5).
const jsRefusedFormProbeSchema = `package refusedform

fixed table FixedRoot
{
    a int32 = 0
    b string(8) = ""
}

table VarRoot
{
    a    int32 = 0
    next *VarRoot
}
`

// TestJsRefusedFormByteLoadsNothing holds the JavaScript variable-form loader
// to the strict read (docs/SPEC-TABLES.md §3, §4, §6.5): a form byte this
// reader does not carry refuses the whole read, and the caller receives null
// for every root, fixed-class and variable-class alike. A clean file the form
// carries still loads.
func TestJsRefusedFormByteLoadsNothing(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated module)")

	t.Parallel()
	node := jsNode(t)
	u := loadSchemaString(t, "refusedform.schema", jsRefusedFormProbeSchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatalf("generateVariableFiles: %v", err)
	}
	moduleName := runtimeHome(u) + "Variable.js"
	module, ok := files[moduleName]
	if !ok {
		t.Fatalf("no variable module %s generated", moduleName)
	}
	dir := t.TempDir()
	modulePath := filepath.Join(dir, moduleName)
	if err := os.WriteFile(modulePath, module, 0o600); err != nil {
		t.Fatal(err)
	}

	const probe = `
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[2]).href);
const roots = ["FixedRoot", "VarRoot"];
const forms = [0, 2, 3, 9, 255];
let failures = 0;
function fail(why) { console.log("FAIL: " + why); failures++; }
for (const root of roots) {
  const text = new m.TableVariableReport();
  const value = m[root + "FromJson"]("{}", text);
  const clean = value === null ? null : m[root + "Save"](value);
  if (clean === null) { fail(root + ": no clean wire"); continue; }
  const cleanReport = new m.TableVariableReport();
  const loaded = m[root + "Load"](clean, cleanReport);
  if (loaded === null || cleanReport.err() !== null) {
    fail(root + ": the clean wire did not load: " + JSON.stringify(cleanReport));
  }
  for (const form of forms) {
    const bytes = Uint8Array.from(clean);
    bytes[0] = form;
    const report = new m.TableVariableReport();
    const refusedValue = m[root + "Load"](bytes, report);
    if (refusedValue !== null) {
      fail(root + " form " + form + ": Load returned a value on a refused form byte");
    }
    if (report.err() === null || report.refused !== 1) {
      fail(root + " form " + form + ": the report does not refuse the read: " + JSON.stringify(report));
    }
  }
}
if (failures !== 0) { process.exit(1); }
console.log("ok");
`
	script := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, modulePath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the refused-form probe failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
