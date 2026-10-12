package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestScaffoldWritesTheFiveFilesInForm: every template renders for a new
// leg, the Go comes out gofmt'd (render runs go/format and fails on Go that
// does not parse), and the fixture unit is already in `schema fmt` form, so
// the skeleton lands in the shape the tree's gates hold it to.
func TestScaffoldWritesTheFiveFilesInForm(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := Scaffold(root, "lua", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(files) {
		t.Fatalf("wrote %d files, want %d: %v", len(written), len(files), written)
	}
	fixture := filepath.Join(root, "test", "lua", "Fixture.schema")
	src, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := compiler.Format(fixture, src)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(src) {
		t.Errorf("the fixture is not in schema fmt form:\n%s\nwant\n%s", src, canonical)
	}
	backend, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "lua", "lua.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`const Ext = ".lua"`, `const comment = "--"`} {
		if !strings.Contains(string(backend), want) {
			t.Errorf("lua.go does not carry %s", want)
		}
	}
	mk, err := os.ReadFile(filepath.Join(root, "make", "lua.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mk), "TEST_LEGS += test-lua") {
		t.Errorf("make/lua.mk does not register its leg:\n%s", mk)
	}
}

// TestScaffoldRefusesAndWritesNothing: a name that is no leg name, a name a
// target already answers to (an alias included), and a tree where one file
// already exists are each refused by name, and a refusal writes no file.
func TestScaffoldRefusesAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ lang, want string }{
		{"Lua", "leg name"},
		{"lua-5", "leg name"},
		{"func", "leg name"},
		{"go", "already a target"},
		{"csharp", "already a target"},
	} {
		if _, err := Scaffold(root, c.lang, "", ""); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Scaffold(%q) = %v, want a refusal naming %q", c.lang, err, c.want)
		}
	}
	if _, err := Scaffold(root, "lua", "lua", ""); err == nil || !strings.Contains(err.Error(), "--ext") {
		t.Errorf("an extension with no dot was not refused: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "make"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "make", "lua.mk"), []byte("# a port in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(root, "lua", "", ""); err == nil || !strings.Contains(err.Error(), "make/lua.mk already exists") {
		t.Errorf("an existing make/lua.mk was not refused by name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "compiler", "target_lua.go")); err == nil {
		t.Error("a refused scaffold wrote compiler/target_lua.go")
	}
	if _, err := Scaffold(t.TempDir(), "lua", "", ""); err == nil || !strings.Contains(err.Error(), "not a schema checkout") {
		t.Errorf("a directory with no go.mod was not refused: %v", err)
	}
}
