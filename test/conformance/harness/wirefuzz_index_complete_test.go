package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE VECTOR INDEX AND THE VECTOR DIRECTORY AGREE, BOTH WAYS. A *.bin that no
// INDEX.txt line names is a pinned red nothing runs (56 of 128 were found so on
// 2026-11-22), and a line whose file is missing is a vector that cannot run.
// readWireVectors refuses either, naming the file.
func TestWireVectorIndexAndDirectoryAgree(t *testing.T) {
	write := func(t *testing.T, dir, name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	line := func(name, file string) string { return fmt.Sprintf("%s tabledemo ProfileConfig %s\n", name, file) }

	t.Run("complete", func(t *testing.T) {
		dir := t.TempDir()
		a := write(t, dir, "a.bin", "x")
		idx := write(t, dir, "INDEX.txt", line("a", a))
		vecs, err := readWireVectors(idx)
		if err != nil || len(vecs) != 1 {
			t.Fatalf("a complete index must read: %v %d", err, len(vecs))
		}
	})
	t.Run("planted outside the index", func(t *testing.T) {
		dir := t.TempDir()
		a := write(t, dir, "a.bin", "x")
		write(t, dir, "planted.bin", "x")
		idx := write(t, dir, "INDEX.txt", line("a", a))
		_, err := readWireVectors(idx)
		if err == nil || !strings.Contains(err.Error(), "planted.bin") {
			t.Fatalf("a vector on disk outside the index must be refused by name, got %v", err)
		}
	})
	t.Run("entry without a file", func(t *testing.T) {
		dir := t.TempDir()
		a := write(t, dir, "a.bin", "x")
		idx := write(t, dir, "INDEX.txt", line("a", a)+line("ghost", filepath.Join(dir, "ghost.bin")))
		_, err := readWireVectors(idx)
		if err == nil || !strings.Contains(err.Error(), "ghost") {
			t.Fatalf("an entry with no file must be refused by name, got %v", err)
		}
	})
	t.Run("the repository index", func(t *testing.T) {
		root, err := treeRoot()
		if err != nil {
			t.Fatal(err)
		}
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(filepath.Dir(root)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(wd) })
		vecs, err := readWireVectors(defaultVectors)
		if err != nil {
			t.Fatal(err)
		}
		if len(vecs) == 0 {
			t.Fatal("the repository index lists no vectors")
		}
	})
}
