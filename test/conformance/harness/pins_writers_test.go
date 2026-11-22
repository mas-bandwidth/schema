package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestFramingWritersAreAllSelected holds the rewrite-and-diff of the framing
// pins (Makefile, PINS_FRAMING_RUN, run by `make pins-framing` in CI and by
// `make update-goldens`) to every test that can write one. A Test function
// whose body reads FRAMING_WRITE writes a pin; if the Makefile's selection does
// not match its name, that pin is checked by nothing that regenerates it, and a
// hand-written file there lands green. The selection is a regexp over test
// names, so a new writer named outside it fails here, at the commit that adds it.
func TestFramingWritersAreAllSelected(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^PINS_FRAMING_RUN := (.+)$`).FindSubmatch(mk)
	if m == nil {
		t.Fatal("Makefile has no PINS_FRAMING_RUN line")
	}
	selected, err := regexp.Compile(string(m[1]))
	if err != nil {
		t.Fatalf("PINS_FRAMING_RUN is not a regexp: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "test/conformance/harness/*_test.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no harness test files found: %v", err)
	}
	writers := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
			if !strings.Contains(body, `"FRAMING_`+`WRITE"`) {
				continue
			}
			writers++
			if !selected.MatchString(fn.Name.Name) {
				t.Errorf("%s (%s) writes a pin under FRAMING_WRITE=1 but Makefile PINS_FRAMING_RUN %q does not select it: add it to the selection", fn.Name.Name, filepath.Base(f), m[1])
			}
		}
	}
	// the writers this was written against; a count that falls to nothing means
	// the scan stopped finding them, not that there are none
	if writers < 20 {
		t.Errorf("found %d FRAMING_WRITE writers, want at least 20: the scan has stopped seeing them", writers)
	}
}
