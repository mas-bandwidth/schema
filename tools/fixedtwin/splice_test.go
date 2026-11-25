package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixed form's arrays live in <Base>TableData.h, apart from the code
// (internal/codegen/ctable dataHeader). The twin splices that file in where
// the header includes it, and a data header that is named and missing is an
// error: an array the twin cannot read must never compare as an empty one.
func TestReadHeaderSplicesTheDataHeader(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("FixedTableTable.h", "int before;\n#include \"FixedTableTableData.h\" /* the arrays */\nint after;\n")
	write("FixedTableTableData.h", "static const int plan[] = { 1, 2 };\n")
	got, err := readHeader(filepath.Join(dir, "FixedTableTable.h"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "int before;\nstatic const int plan[] = { 1, 2 };\nint after;\n"; got != want {
		t.Fatalf("spliced header:\n%q\nwant\n%q", got, want)
	}

	if err := os.Remove(filepath.Join(dir, "FixedTableTableData.h")); err != nil {
		t.Fatal(err)
	}
	if _, err := readHeader(filepath.Join(dir, "FixedTableTable.h")); err == nil || !strings.Contains(err.Error(), "FixedTableTableData.h") {
		t.Fatalf("a missing data header read as %v, want an error naming it", err)
	}
}
