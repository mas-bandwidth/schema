package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRefusalSavedWireScoredByHarness(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join("..", "..", "..", "testdata", "conformance", "table-write-bounds")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pin := filepath.Join(dir, "private-expected.bin")
	expected := read(filepath.Join(fixture, "expected", "torture_var_wref_resized.bin"))
	write(pin, expected)
	line := "write-refusal resize writebounds TortureVarWref input.bin opt_arr count 3"
	manifestPath := filepath.Join(dir, "manifest")
	write(manifestPath, []byte(line+" wrote "+pin+"\n"))
	m, err := ReadManifest(manifestPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	derivedPath := filepath.Join(dir, "driver-manifest")
	if err := deriveManifest(m, derivedPath); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(read(derivedPath))); got != line+" save-wire" {
		t.Fatalf("driver sees %q, want the neutral save-wire flag only", got)
	}
	cases, err := expectations(m, "write-refusal", nil, dir)
	if err != nil || len(cases) != 1 {
		t.Fatalf("expectations: %v, %v", cases, err)
	}
	write(filepath.Join(dir, "resize"), []byte("wrote\n"))
	answer := filepath.Join(dir, "resize.wire")
	for _, tc := range []struct {
		name string
		wire []byte
		pass bool
	}{
		{"missing", nil, false},
		{"unmodified lawful input", read(filepath.Join(fixture, "torture_var_wref.bin")), false},
		{"resized", expected, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wire != nil {
				write(answer, tc.wire)
			}
			r := &result{total: 1}
			scoreAnswer("substitute", "write-refusal", dir, cases[0], Instance{}, r)
			if (r.pass == 1) != tc.pass || (len(r.failures) == 0) != tc.pass {
				t.Fatalf("wrote with %s output: pass=%d, failures=%v", tc.name, r.pass, r.failures)
			}
		})
	}
}
