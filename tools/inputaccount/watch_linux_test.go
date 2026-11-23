//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// The joined check succeeds only when a real Make invocation passes, opens
// the genuine inputs, and leaves exactly the extra file unread.
func TestCheckExtraOnePass(t *testing.T) {
	const dir = "testdata/wire/packet-defaults"
	const extra = dir + "/zz-inputaccount-canary.bin"
	const real = dir + "/sample.bin"
	for _, tc := range []struct {
		name   string
		recipe string
		code   int
		want   string
	}{
		{"only the extra is unread", "cat " + real, 0, "the expanded inventory went red"},
		{"the genuine input is unread", "true", 1, "UNREAD " + real},
		{"the extra is read", "cat " + real + " " + extra, 1, "leaving only " + extra + " unread"},
		{"the reader fails", "cat " + real + "; exit 1", 1, "leg c: make packet-defaults-c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			write := func(name, data string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(real, "sample")
			write("test/packet-defaults/c_main.c", "")
			write("Makefile", ".PHONY: packet-defaults-c\n"+
				"packet-defaults-c:\n\t@test -f "+extra+"\n\t@echo ran >> count\n\t@"+tc.recipe+"\n")
			var stdout, stderr bytes.Buffer
			code := run([]string{"-family", "packet-defaults", "-leg", "c", "-check-extra"}, &stdout, &stderr)
			if code != tc.code || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit = %d, want %d and %q; log:\n%s", code, tc.code, tc.want, &stderr)
			}
			count, err := os.ReadFile("count")
			if err != nil || string(count) != "ran\n" {
				t.Fatalf("Make invocations = %q, %v; want one", count, err)
			}
			if _, err := os.Stat(extra); !os.IsNotExist(err) {
				t.Fatalf("extra input cleanup: got %v, want no matching file", err)
			}
		})
	}
}
