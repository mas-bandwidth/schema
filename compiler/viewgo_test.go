package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestGoUnitViewCorpus(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain, once per corpus unit")
	printer, err := os.ReadFile("../test/tables/view_go.gotext")
	if err != nil {
		t.Fatal(err)
	}
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(makefile))
	if len(entries) == 0 {
		t.Fatal("Makefile declares no VIEW_CORPUS entries")
	}
	for _, entry := range entries {
		path, pkg, ok := strings.Cut(entry, ":")
		if !ok {
			t.Fatalf("VIEW_CORPUS entry %q is not dir:package", entry)
		}
		t.Run(path, func(t *testing.T) {
			c := New()
			source := "../tables/" + path
			if path == "wide" {
				source = "../examples-wide"
			}
			paths, err := filepath.Glob(source + "/*.schema")
			if err != nil {
				t.Fatal(err)
			}
			u, err := c.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			if u.Package != pkg {
				t.Fatalf("VIEW_CORPUS package %s, generated %s", pkg, u.Package)
			}
			files, err := c.Generate(u, "go", Options{})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := filepath.Abs("../../serialize.go")
			if err != nil {
				t.Fatal(err)
			}
			files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
			files["view_test.go"] = []byte(strings.ReplaceAll(string(printer), "VIEW_PACKAGE", u.Package))
			files["listing.want"] = []byte(viewlisting.Listing(u))
			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", ".")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated view: %v\n%s", err, out)
			}
		})
	}
}
