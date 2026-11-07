package compiler

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestNewLegRefusesExistingTargets(t *testing.T) {
	for _, lang := range []string{"c", "cpp", "cs", "csharp", "dart", "elixir", "go", "java", "js", "javascript", "lua", "rust"} {
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

func TestNewLegZigFiles(t *testing.T) {
	files, err := NewLeg("zig")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"compiler/target_zig.go",
		"internal/codegen/zig/zig.go",
		"internal/codegen/zig/zig_test.go",
		"make/zig.mk",
		"test/conformance/zig/driver",
		"test/conformance/zig/ci.json",
		"bench/tables/zig/leg",
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
	mk := string(files["make/zig.mk"])
	for _, needle := range []string{
		"TEST_LEGS         += test-zig",
		"CONFORMANCE_LEGS  += $(call unless_skipped,zig,build/conformance-zig)",
		"BENCH_TABLES_LEGS += generated/bench/tables/zig/.stamp",
		"GOLDENS_LEGS      += update-goldens-zig",
		"go test ./internal/codegen/zig -count=1",
	} {
		if !strings.Contains(mk, needle) {
			t.Errorf("make/zig.mk missing %q\n%s", needle, mk)
		}
	}
	target := string(files["compiler/target_zig.go"])
	for _, needle := range []string{
		"//go:build schema_leg_zig",
		`Names() []string { return []string{"zig"} }`,
		"registerBuiltin(zigTarget{}, false, false, false, false)",
	} {
		if !strings.Contains(target, needle) {
			t.Errorf("compiler/target_zig.go missing %q\n%s", needle, target)
		}
	}
	if !NewLegExec("test/conformance/zig/driver") || !NewLegExec("bench/tables/zig/leg") {
		t.Fatal("driver and leg must be executable")
	}
	if NewLegExec("make/zig.mk") {
		t.Fatal("make/zig.mk must not be marked executable")
	}
}

func TestNewLegZigFixturePasses(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated zig leg fixture)")
	files, err := NewLeg("zig")
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
	for _, name := range []string{"internal/codegen/zig/zig.go", "internal/codegen/zig/zig_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := "module newleg_zig_fixture\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test the zig fixture: %v\n%s", err, out)
	}
}

func TestNewLegBenchScaffoldIsUnavailable(t *testing.T) {
	files, err := NewLeg("zig")
	if err != nil {
		t.Fatal(err)
	}
	leg := filepath.Join(t.TempDir(), "leg")
	if err := os.WriteFile(leg, files["bench/tables/zig/leg"], 0o755); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"build", "run"} {
		cmd := exec.Command(leg, action)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("%s exited 0, want unavailable exit 2", action)
		}
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok || exit.ExitCode() != 2 {
			t.Fatalf("%s: err=%v, want exit 2", action, err)
		}
		if len(out) != 0 {
			t.Fatalf("%s emitted output for an unavailable scaffold: %q", action, out)
		}
	}
}

// TestNewLegGooseRecipe: goose's scaffold is .goose files commented with `//`
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT), not the default `#`, which is no
// Goose comment. Under the schema_leg_goose tag goose is a live target and
// new-leg refuses it by name; the recipe is held either way.
func TestNewLegGooseRecipe(t *testing.T) {
	t.Parallel()
	ext, comment := newLegRecipe("goose")
	if ext != "goose" || comment != "//" {
		t.Fatalf("newLegRecipe(goose) = %q, %q; want goose, //", ext, comment)
	}
	files, err := NewLeg("goose")
	if err != nil {
		if !strings.Contains(err.Error(), "goose is already a live target") {
			t.Fatal(err)
		}
		return
	}
	emitter := string(files["internal/codegen/goose/goose.go"])
	for _, want := range []string{`const ext = "goose"`, `"// Code generated by the schema compiler`} {
		if !strings.Contains(emitter, want) {
			t.Errorf("scaffolded goose.go missing %q", want)
		}
	}
	if strings.Contains(emitter, `"# Code generated`) {
		t.Error("scaffolded goose.go comments with #, which is no Goose comment")
	}
	if _, ok := files["test/goose/main.lua"]; ok {
		t.Error("the goose leg must not write the lua harness")
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
