package squirreltable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

func TestSquirrelTableRuntimeRefusesUnpinnedVm(t *testing.T) {
	t.Parallel()
	if _, err := sqrun.Interpreter(); err != nil {
		sqrun.Require(t)
	}
	schemaDir := t.TempDir()
	schemaPath := filepath.Join(schemaDir, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte("package probe\ntable Root { value float64 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	typeFiles, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tableFiles, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(typeFiles)+len(tableFiles))
	for name, data := range typeFiles {
		files[name] = data
	}
	for name, data := range tableFiles {
		files[name] = data
	}
	for name, data := range squirrel.Runtime() {
		files[name] = data
	}
	runtimeFiles := squirreltable.Runtime()
	for name, data := range runtimeFiles {
		files[name] = data
	}

	runModule := func(t *testing.T, descriptor []byte) string {
		t.Helper()
		dir := t.TempDir()
		for name, data := range files {
			if name == "schema_table_descriptor.nut" {
				data = descriptor
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		main := `local schema = dofile("schema_module.nut", true);
local T = schema.require("ProbeTables");
local v = T.Descriptors.Root.make();
v.Value = 16777217.0;
local wire = schema.require("schema_table_save").save(T, "Root", v);
local report = {};
local got = schema.require("schema_table_load").load(T, "Root", wire, report);
if (got == null || got.Value != 16777217.0) throw "float64 table round trip failed";
print(format("roundtrip=%.0f bytes=%d\n", got.Value, wire.len()));
`
		if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
	}
	if got := runModule(t, runtimeFiles["schema_table_descriptor.nut"]); !strings.HasPrefix(got, "roundtrip=16777217 bytes=") {
		t.Fatalf("pinned module round trip = %q", got)
	}

	for _, probe := range []struct {
		name, source, replacement string
	}{
		{"integer", "_intsize_ == 8", "_intsize_ == 4"},
		{"float", "_floatsize_ == 8", "_floatsize_ == 4"},
		{"character", "_charsize_ == 1", "_charsize_ == 2"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Parallel()
			source := string(runtimeFiles["schema_table_descriptor.nut"])
			badRuntime := strings.Replace(source, probe.source, probe.replacement, 1)
			dir := t.TempDir()
			for name, data := range files {
				if name == "schema_table_descriptor.nut" {
					data = []byte(badRuntime)
				}
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			main := `local tableExports = {};
local modules = {};
local require;
require = function(name) {
	if (name in modules) return modules[name];
  local exports = {};
  modules[name] <- exports;
  local fn = loadfile(name + ".nut", true);
  fn.call(::getroottable(), exports, require);
  return exports;
};
local refusal = null;
try { loadfile("ProbeTables.nut", true).call(::getroottable(), tableExports, require); }
catch (e) { refusal = e; }
print("refused=" + (refusal == null ? "none" : refusal) + "\n");
local n = 0;
foreach (k, v in tableExports) n++;
print("exports=" + n + "\n");
`
			if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil)))
			if !strings.Contains(out, "refused=squirrel_table_unpinned_vm") || !strings.Contains(out, "exports=0") {
				t.Fatalf("runtime did not refuse by name: %q (source replacement %q -> %q)", out, probe.source, probe.replacement)
			}
		})
	}
}
