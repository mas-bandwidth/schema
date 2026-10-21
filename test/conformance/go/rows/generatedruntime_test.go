package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generatedRuntimeSource is a generated unit's runtime as the rows read it: the
// unit's own per-package file and, because a default-generated Go unit uses
// the shared runtime (docs/SPEC-TABLES.md §8.4), the package it aliases,
// tables/goruntime/goruntime.go. The fixed-form runtime the rows inspect lives
// there now; the unit keeps only what it does not share.
func generatedRuntimeSource(t *testing.T, unitPath string) string {
	t.Helper()
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("cannot read generated code: %v", err)
	}
	shared, err := os.ReadFile(filepath.Join(findRepoRoot(t), "tables", "goruntime", "goruntime.go"))
	if err != nil {
		t.Fatalf("cannot read the shared Go runtime: %v", err)
	}
	return string(unit) + "\n" + string(shared)
}

// runtimeHas reports whether src carries text in either spelling of the
// runtime's names: a standalone unit's tableFixed... or the shared package's
// exported TableFixed....
func runtimeHas(src, text string) bool {
	return strings.Contains(src, text) || strings.Contains(src, strings.ReplaceAll(text, "tableFixed", "TableFixed"))
}
