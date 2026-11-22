//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The watcher records the files opened in a watched directory, and only
// those: the accounting judges a leg by what it opened.
func TestWatchRecordsOpens(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, n := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join("in", n), nil, 0o644); err != nil {
			if err := os.MkdirAll("in", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join("in", n), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	w, err := watch([]string{"in"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("in", "a.bin"))
	if err != nil {
		t.Fatal(err)
	}
	_ = b
	opened, err := w.stop()
	if err != nil {
		t.Fatal(err)
	}
	if !opened["in/a.bin"] || opened["in/b.bin"] || len(opened) != 1 {
		t.Errorf("opened = %v, want only in/a.bin", opened)
	}
}

// The canaries plant and restore: an extra input appears and goes, a wrong
// input is flipped and put back byte for byte.
func TestPlantRestores(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("in", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("in/a.bits", []byte("12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("in/b.bin", []byte{0x10, 0x20}, 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := []string{"in/a.bits", "in/b.bin"}
	p, undo, err := plant("extra", inputs)
	if err != nil || p != "in/zz-inputaccount-canary.bits" {
		t.Fatalf("extra planted %q, %v", p, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	undo()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("extra canary not removed: %v", err)
	}
	p, undo, err = plant("wrong", inputs)
	if err != nil || p != "in/b.bin" {
		t.Fatalf("wrong planted %q, %v", p, err)
	}
	if b, _ := os.ReadFile(p); b[0] != 0x11 {
		t.Errorf("wrong canary did not flip the first byte: %x", b)
	}
	undo()
	if b, _ := os.ReadFile(p); b[0] != 0x10 || b[1] != 0x20 {
		t.Errorf("wrong canary not restored: %x", b)
	}
	if _, _, err := plant("other", inputs); err == nil {
		t.Error("an unknown canary was accepted")
	}
}
