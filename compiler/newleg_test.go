package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestNewLegRefusesExistingTargets(t *testing.T) {
	for _, lang := range []string{"c", "cpp", "cs", "csharp", "dart", "elixir", "go", "java", "js", "javascript", "rust"} {
		_, err := NewLeg(lang)
		if err == nil || !strings.Contains(err.Error(), "already a live target") {
			t.Errorf("new-leg %s: want a live-target refusal, got %v", lang, err)
		}
	}
}

func TestNewLegRefusesInvalidNames(t *testing.T) {
	for _, lang := range []string{"", "Lua", "c++", "go-lua", "type", "package", "linux", "wasm", "checks", "compiler"} {
		_, err := NewLeg(lang)
		if err == nil {
			t.Errorf("new-leg %q: accepted", lang)
		}
	}
}

func TestNewLegLuaFiles(t *testing.T) {
	files, err := NewLeg("lua")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"compiler/target_lua.go",
		"internal/codegen/lua/lua.go",
		"internal/codegen/lua/lua_test.go",
		"make/lua.mk",
		"test/conformance/lua/driver",
		"test/conformance/lua/ci.json",
		"bench/tables/lua/leg",
		"test/lua/main.lua",
	}
	slices.Sort(want)
	got := make([]string, 0, len(files))
	for name := range files {
		got = append(got, name)
		if strings.HasSuffix(name, ".schema") {
			t.Errorf("new-leg wrote a .schema file (%s); fmt is the only command that writes one", name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("files: got %v want %v", got, want)
	}
	mk := string(files["make/lua.mk"])
	for _, needle := range []string{
		"TEST_LEGS         += test-lua",
		"CONFORMANCE_LEGS  += $(call unless_skipped,lua,build/conformance-lua)",
		"BENCH_TABLES_LEGS += generated/bench/tables/lua/.stamp",
		"GOLDENS_LEGS      += update-goldens-lua",
		"go test ./internal/codegen/lua -count=1",
	} {
		if !strings.Contains(mk, needle) {
			t.Errorf("make/lua.mk missing %q\n%s", needle, mk)
		}
	}
	target := string(files["compiler/target_lua.go"])
	for _, needle := range []string{
		"//go:build schema_leg_lua",
		`Names() []string { return []string{"lua"} }`,
		"registerBuiltin(luaTarget{}, false, false, false, false)",
	} {
		if !strings.Contains(target, needle) {
			t.Errorf("compiler/target_lua.go missing %q\n%s", needle, target)
		}
	}
	if !NewLegExec("test/conformance/lua/driver") || !NewLegExec("bench/tables/lua/leg") {
		t.Fatal("driver and leg must be executable")
	}
	if NewLegExec("make/lua.mk") {
		t.Fatal("make/lua.mk must not be marked executable")
	}
}

func TestNewLegLuaFixturePasses(t *testing.T) {
	files, err := NewLeg("lua")
	if err != nil {
		t.Fatal(err)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"internal/codegen/lua/lua.go", "internal/codegen/lua/lua_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := "module newleg_lua_fixture\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test the lua fixture: %v\n%s", err, out)
	}
}

func TestNewLegZigOmitsLuaHarness(t *testing.T) {
	files, err := NewLeg("zig")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["test/zig/main.lua"]; ok {
		t.Fatal("non-lua legs must not write a lua harness")
	}
	if _, ok := files["internal/codegen/zig/zig.go"]; !ok {
		t.Fatal("missing zig emitter")
	}
}
