package cpptable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestCompileLineageComesFromTheLock(t *testing.T) {
	dir := t.TempDir()
	src := "package compilelock\n\nfixed table Row\n{\n    a int32 | min = 0, max = 100\n    b int32\n}\n"
	path := filepath.Join(dir, "Lock.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, path)
	if _, _, err := lockfile.Update(u, []string{path}); err != nil {
		t.Fatal(err)
	}
	got := lockfile.Lineage(mustOpen(t, path), "Row")
	if len(got) != 1 || len(got[0].Digest) == 0 {
		t.Fatalf("the lock records the range digest: %+v", got)
	}

	u = loadUnit(t, path)
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	hdr, ok := out["LockTable.h"]
	if !ok {
		for name := range out {
			t.Log(name)
		}
		t.Fatal("Generate emits LockTable.h")
	}
	text := string(hdr)
	want := fmt.Sprintf("0x%016xull", got[0].Wire)
	if !strings.Contains(text, want) {
		t.Fatalf("COMPILE lays the lock's wire hash down as static data; want %s in the header", want)
	}
	if !strings.Contains(text, "RowFixedLineage[]") {
		t.Fatal("COMPILE emits the lineage array from the lock")
	}
	if !strings.Contains(text, "constexpr int32_t RowFixedFloor = 0;") {
		t.Fatal("COMPILE emits the floor from lockfile.Floor")
	}
}

func TestCompileFromLockIgnoresFixturePeerConvention(t *testing.T) {
	dir := t.TempDir()
	oldSrc := "package vold_gate\n\nfixed table Row\n{\n    a int32\n}\n"
	newSrc := "package vnew_gate\n\nfixed table Row\n{\n    a int32\n    b int32\n}\n"
	oldPath := filepath.Join(dir, "VOLD_gate.schema")
	newPath := filepath.Join(dir, "VNEW_gate.schema")
	if err := os.WriteFile(oldPath, []byte(oldSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(newSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, newPath)
	if _, _, err := lockfile.Update(u, []string{newPath}); err != nil {
		t.Fatal(err)
	}
	u = loadUnit(t, newPath)
	if loadUnitLock(u) == nil {
		t.Fatal("VNEW_gate has a lock")
	}
	if n := len(fixturePeerPaths(newPath)); n == 0 {
		t.Fatal("the convention names VOLD_gate as a sibling; that is the trap")
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	hdr, ok := out["VNEW_gateTable.h"]
	if !ok {
		for name := range out {
			t.Log(name)
		}
		t.Fatal("Generate emits VNEW_gateTable.h")
	}
	text := string(hdr)
	if strings.Contains(text, "vold_gate") {
		t.Fatal("a locked unit must not append a fixture-peer entry past the identity")
	}
	if !strings.Contains(text, "RowFixedLineage[]") {
		t.Fatal("COMPILE still emits the lineage from the lock")
	}
	n := strings.Count(text, "ull, //")
	if n != 1 {
		t.Fatalf("lock-only lineage is one identity entry, got %d notes in:\n%s", n, text)
	}
	if !strings.Contains(text, "// identity") {
		t.Fatal("the one entry is the identity")
	}
}

func loadUnit(t *testing.T, path string) *ir.Unit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ast, perrs := parser.Parse(path, data)
	if len(perrs) > 0 || ast == nil {
		t.Fatalf("parse: %v", perrs)
	}
	name := filepath.Base(path)
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  path,
		Name:  name,
		Base:  strings.TrimSuffix(name, ".schema"),
		Bytes: data,
		AST:   ast,
	}})
	if len(cerrs) > 0 || u == nil {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func mustOpen(t *testing.T, schemaPath string) *lockfile.Unit {
	t.Helper()
	lock, ok, err := lockfile.Open([]string{schemaPath})
	if err != nil || !ok || lock == nil {
		t.Fatalf("Open: ok=%v err=%v", ok, err)
	}
	return lock
}
