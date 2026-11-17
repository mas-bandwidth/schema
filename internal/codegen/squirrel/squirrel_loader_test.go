package squirrel_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// TestSquirrelLoaderForgetsFailedModule pins the loader's failure rule
// (docs/PORT-SQUIRREL.md, LOADING IS CLOSED): a module that fails to load — its
// file is absent, or its script throws — is not cached, so every later require
// of the same name fails the same way rather than answering an empty or
// half-filled table. The loader still caches before running the script, so a
// cycle meets the table it is still filling and two modules that require each
// other load.
func TestSquirrelLoaderForgetsFailedModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string][]byte{
		"schema_module.nut": squirrel.Runtime()["schema_module.nut"],
		"Halfway.nut": []byte(`local _M = vargv[0];
_M.Before <- 1;
throw "halfway";
_M.After <- 2;
`),
		"CycleA.nut": []byte(`local _M = vargv[0];
local _require = vargv[1];
local _B = _require("CycleB");
_M.Name <- "A";
_M.Other <- _B;
`),
		"CycleB.nut": []byte(`local _M = vargv[0];
local _require = vargv[1];
local _A = _require("CycleA");
_M.Name <- "B";
_M.Other <- _A;
`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `local schema = dofile("schema_module.nut", true);
local verdict = function(f) {
  try { f(); return "loaded"; }
  catch (e) { return "threw"; }
};
print("missing1=" + verdict(function() { schema.require("Absent"); }) + "\n");
print("missing2=" + verdict(function() { schema.require("Absent"); }) + "\n");
print("throw1=" + verdict(function() { schema.require("Halfway"); }) + "\n");
print("throw2=" + verdict(function() { schema.require("Halfway"); }) + "\n");
local A = schema.require("CycleA");
local B = schema.require("CycleB");
print("cycle=" + (A.Other == B) + "," + (B.Other == A) + "\n");
`
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	for _, want := range []string{
		"missing1=threw\n",
		"missing2=threw\n",
		"throw1=threw\n",
		"throw2=threw\n",
		"cycle=true,true\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
