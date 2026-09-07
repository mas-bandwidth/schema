package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedOutputIsConfined(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "out")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", filepath.Join(parent, "absolute"), `..\escape`} {
		if err := writeGenerated(dir, map[string][]byte{name: []byte("new")}, false); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	outside := filepath.Join(parent, "existing")
	if err := os.WriteFile(outside, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeGenerated(dir, map[string][]byte{"link": []byte("new")}, false); err == nil {
		t.Fatal("followed escaping link")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "original" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
	if err := writeGenerated(dir, map[string][]byte{"ok.h": []byte("ok")}, false); err != nil {
		t.Fatal(err)
	}
}
