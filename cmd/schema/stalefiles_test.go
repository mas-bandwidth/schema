package main

import (
	"os"
	"path/filepath"
	"testing"
)

const fixedPosUnit = "package pos\n\nfixed table Pos {\n  x int32\n  y int32\n}\n"
const varPosUnit = "package pos\n\ntable Pos {\n  x int32\n  y int32\n}\n"
const otherUnit = "package otherpkg\n\ntable Other {\n  z int32\n}\n"

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestGenerateGoRemovesStaleSurfaceFiles pins SPEC-TABLES.md §8.8's stale
// removal: a default generate --lang go (no --surface) removes a .go file the
// compiler itself wrote on an earlier run when this run no longer emits it,
// while a hand-written file and another unit's generated file in the same
// directory stay, and --surface returns nothing.
func TestGenerateGoRemovesStaleSurfaceFiles(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	fixedDir := filepath.Join(work, "fixed")
	otherDir := filepath.Join(work, "other")
	writeTestFile(t, filepath.Join(fixedDir, "Pos.schema"), fixedPosUnit)
	writeTestFile(t, filepath.Join(otherDir, "Other.schema"), otherUnit)

	out := filepath.Join(work, "out")

	// first generate writes PosBlock.go (a fixed table emits the block form),
	// then a hand-written file and another unit's generated file join it.
	run(t, bin, "generate", "--lang", "go", "--out", out, fixedDir)
	if !fileExists(filepath.Join(out, "PosBlock.go")) {
		t.Fatalf("first generate must write PosBlock.go")
	}
	writeTestFile(t, filepath.Join(out, "x.go"), "package x\n// hand-written\n")
	run(t, bin, "generate", "--lang", "go", "--out", out, otherDir)
	if !fileExists(filepath.Join(out, "OtherTable.go")) {
		t.Fatalf("other unit must write OtherTable.go")
	}

	// re-spell Pos as a variable table and generate into the same out: the
	// stale PosBlock.go is removed, the hand-written and other-unit files stay.
	writeTestFile(t, filepath.Join(fixedDir, "Pos.schema"), varPosUnit)
	run(t, bin, "generate", "--lang", "go", "--out", out, fixedDir)

	if fileExists(filepath.Join(out, "PosBlock.go")) {
		t.Fatalf("stale PosBlock.go must be removed after re-spelling Pos as a variable table")
	}
	if !fileExists(filepath.Join(out, "x.go")) {
		t.Fatalf("hand-written x.go must stay")
	}
	if !fileExists(filepath.Join(out, "OtherTable.go")) {
		t.Fatalf("another unit's generated file must stay")
	}

	// with --surface, nothing is removed: a stale PosBlock.go survives.
	surfaceOut := filepath.Join(work, "surface-out")
	writeTestFile(t, filepath.Join(fixedDir, "Pos.schema"), fixedPosUnit)
	run(t, bin, "generate", "--lang", "go", "--out", surfaceOut, fixedDir)
	if !fileExists(filepath.Join(surfaceOut, "PosBlock.go")) {
		t.Fatalf("preparation must write PosBlock.go")
	}
	writeTestFile(t, filepath.Join(fixedDir, "Pos.schema"), varPosUnit)
	run(t, bin, "generate", "--lang", "go", "--surface", "wire", "--out", surfaceOut, fixedDir)
	if !fileExists(filepath.Join(surfaceOut, "PosBlock.go")) {
		t.Fatalf("with --surface nothing is removed; stale PosBlock.go must survive")
	}
}
