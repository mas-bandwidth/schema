package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runFail(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		t.Fatalf("schema %s: exited 0, want a refusal\n%s", strings.Join(args, " "), out)
	}
	status := 1
	if ee, ok := err.(*exec.ExitError); ok {
		status = ee.ExitCode()
	}
	return string(out), status
}

func schemaTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("# schema\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"compiler", "make", filepath.Join("internal", "codegen")} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNewLegMissingLanguageIsARefusal(t *testing.T) {
	bin := buildCLI(t)
	out, status := runFail(t, bin, "new-leg")
	if status != 1 {
		t.Fatalf("exit %d, want 1\n%s", status, out)
	}
	if !strings.Contains(out, "new-leg needs the language name: schema new-leg lua") {
		t.Fatalf("missing-language refusal: %q", out)
	}
}

func TestNewLegExistingLanguageIsARefusal(t *testing.T) {
	bin := buildCLI(t)
	out, status := runFail(t, bin, "new-leg", "go")
	if status != 1 {
		t.Fatalf("exit %d, want 1\n%s", status, out)
	}
	if !strings.Contains(out, "already a live target") {
		t.Fatalf("live-target refusal: %q", out)
	}
}

func TestNewLegLuaWritesSkeletonAndFixturePasses(t *testing.T) {
	bin := buildCLI(t)
	root := schemaTree(t)
	out := run(t, bin, "new-leg", "lua", "--root", root, "--verbose")
	for _, rel := range []string{
		"compiler/target_lua.go",
		"internal/codegen/lua/lua.go",
		"internal/codegen/lua/lua_test.go",
		"make/lua.mk",
		"test/conformance/lua/driver",
		"test/conformance/lua/ci.json",
		"bench/tables/lua/leg",
		"test/lua/main.lua",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
		if !strings.Contains(out, "wrote "+path) {
			t.Errorf("verbose did not name %s:\n%s", path, out)
		}
	}
	driver := filepath.Join(root, "test", "conformance", "lua", "driver")
	info, err := os.Stat(driver)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("driver is not executable: %v", info.Mode())
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	repo, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"lua.go", "lua_test.go"} {
		data, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "lua", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := "module newleg_lua_fixture\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => " + repo + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if got, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, got)
	}

	_, status := runFail(t, bin, "new-leg", "lua", "--root", root)
	if status != 1 {
		t.Fatalf("overwrite exit %d, want 1", status)
	}
}

func TestHelpListsNewLeg(t *testing.T) {
	bin := buildCLI(t)
	out, _ := exec.Command(bin, "help").CombinedOutput()
	if !strings.Contains(string(out), "schema new-leg") {
		t.Fatalf("help does not list new-leg:\n%s", out)
	}
}
