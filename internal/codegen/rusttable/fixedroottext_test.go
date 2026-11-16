package rusttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A fixed read's value has the same text and cooked images as every other
// reader of that value (docs/SPEC-TABLES.md §3.4, §7.6, §16).
func TestRustFixedRootTextAndCook(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Rust conformance driver")
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("make", "build/conformance-rust")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Rust driver: %v\n%s", err, out)
	}
	cases := []struct{ name, table string }{
		{"root_full", "RootConfig"},
		{"keyed_config", "KeyedConfig"},
	}
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.txt")
	var rows bytes.Buffer
	for _, tc := range cases {
		fmt.Fprintf(&rows, "instance %s tabledemo %s testdata/conformance/tables/wire/%s.bin\n", tc.name, tc.table, tc.name)
		fmt.Fprintf(&rows, "cook-write %s\n", tc.name)
	}
	if err := os.WriteFile(manifest, rows.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, surface := range []string{"json-write", "json-read", "cook-write"} {
		outdir := filepath.Join(dir, surface)
		if err := os.Mkdir(outdir, 0o700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(filepath.Join(root, "build/conformance-rust"), manifest, surface, outdir)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Rust %s: %v\n%s", surface, err, out)
		}
		for _, tc := range cases {
			switch surface {
			case "json-write":
				fixedRootBytesEqual(t, filepath.Join(outdir, tc.name+".json"), filepath.Join(root, "testdata/conformance/tables/json", tc.name+".json"))
			case "json-read":
				fixedRootBytesEqual(t, filepath.Join(outdir, tc.name), filepath.Join(root, "testdata/conformance/tables/wire", tc.name+".bin"))
			case "cook-write":
				for _, suffix := range []string{"", "-be"} {
					name := tc.name + suffix
					fixedRootBytesEqual(t, filepath.Join(outdir, name), filepath.Join(root, "testdata/conformance/tables/cook-write", name+".cook"))
				}
			}
		}
	}
}

func fixedRootBytesEqual(t *testing.T, gotPath, wantPath string) {
	t.Helper()
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from committed %s: got %d bytes, want %d", gotPath, wantPath, len(got), len(want))
	}
}
