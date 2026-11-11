package squirrel_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// TestSquirrelLoaderForgetsFailedModule pins the loader's cache against a
// module that never loaded: a missing file and a script that throws both fail
// every require of that name, while two files that require each other still
// load once each and meet the table being filled.
func TestSquirrelLoaderForgetsFailedModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := squirrel.Runtime()
	if _, ok := files[squirrel.RuntimePrefix+"module.nut"]; !ok {
		t.Fatalf("the loader %smodule.nut is not in the runtime; emitted %v", squirrel.RuntimePrefix, keysOf(files))
	}
	files["HalfWay.nut"] = []byte(`local _M = vargv[0];
_M.Half <- 1;
throw "half way";
`)
	files["CycleA.nut"] = []byte(`local _M = vargv[0];
local _require = vargv[1];
_M.Name <- "A";
_M.Value <- 7;
local B = _require("CycleB");
_M.FromB <- B.Value;
`)
	files["CycleB.nut"] = []byte(`local _M = vargv[0];
local _require = vargv[1];
_M.Name <- "B";
_M.Value <- 9;
local A = _require("CycleA");
_M.FromA <- A.Value;
`)
	files["main.nut"] = []byte(`local schema = dofile("schema_module.nut", true);
local missing = 0;
for (local i = 0; i < 2; i++) {
  try { schema.require("Absent"); } catch (e) { missing++; }
}
print("missing=" + missing + "\n");
local throwing = 0;
for (local i = 0; i < 2; i++) {
  try { schema.require("HalfWay"); } catch (e) { throwing++; }
}
print("throwing=" + throwing + "\n");
local A = schema.require("CycleA");
local B = schema.require("CycleB");
print("cycle=" + A.Value + "," + A.FromB + "," + A.Name + "," + B.Name + "\n");
`)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	for _, want := range []string{
		"missing=2\n",
		"throwing=2\n",
		"cycle=7,9,A,B\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
