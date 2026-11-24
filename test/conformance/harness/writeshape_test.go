package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteShapeRowsAreHeldToTheirRules pins how ReadManifest holds a
// `write-refusal-shape` row (docs/SPEC-CONFORMANCE.md DW-TABLE-SHAPE): kind
// shape rides that surface alone, names a token of the closed set, and is
// refused; the whole-value path `.` takes kind shape alone. A lawful row of
// each passes beside the refused ones.
func TestWriteShapeRowsAreHeldToTheirRules(t *testing.T) {
	t.Parallel()
	const unit = "unit u testdata/u\n"
	for _, c := range []struct {
		name, line, err string
	}{
		{"lawful shape row", "write-refusal-shape a u R w.bin f shape int refused", ""},
		{"lawful twin", "write-refusal-shape a u R w.bin f int 7 wrote", ""},
		{"lawful whole value", "write-refusal-shape a u R w.bin . shape null refused", ""},
		{"shape off its surface", "write-refusal a u R w.bin f shape int refused", "rides only the write-refusal-shape surface"},
		{"token outside the set", "write-refusal-shape a u R w.bin f shape integer refused", "is not a shape token"},
		{"shape written", "write-refusal-shape a u R w.bin f shape int wrote", "a shape row is refused"},
		{"whole value of another kind", "write-refusal-shape a u R w.bin . int 7 refused", "takes kind shape alone"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "manifest.txt")
			if err := os.WriteFile(path, []byte(unit+c.line+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := ReadManifest(path, dir)
			switch {
			case c.err == "" && err != nil && strings.Contains(err.Error(), "shape"):
				t.Fatalf("a lawful row was refused: %v", err)
			case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
				t.Fatalf("ReadManifest answered %v, want an error naming %q", err, c.err)
			}
		})
	}
}

// TestShapeOwedNamesEveryRefuseEveryBuildLeg pins which legs owe the shape
// surface: the eight whose writers refuse in every build owe every case, so
// an absence there is a failure; an assert leg, the reference leg and any
// other surface owe nothing here.
func TestShapeOwedNamesEveryRefuseEveryBuildLeg(t *testing.T) {
	t.Parallel()
	for _, lang := range []string{"go", "js", "elixir", "lua", "luau", "squirrel", "moo", "goose"} {
		if shapeOwed(lang, "write-refusal-shape", 1, "x") == "" {
			t.Errorf("%s does not owe the shape surface", lang)
		}
		if shapeOwed(lang, "write-refusal-shape", 0, "x") != "" {
			t.Errorf("%s owes the shape surface on a corpus with no shape rows", lang)
		}
		if shapeOwed(lang, "write-refusal", 1, "x") != "" {
			t.Errorf("%s owes write-refusal through the shape rule", lang)
		}
	}
	for _, lang := range []string{"cpp", "c", "cs", "rust", "java", "dart", "lobster"} {
		if why := shapeOwed(lang, "write-refusal-shape", 1, "x"); why != "" {
			t.Errorf("%s, an assert or reference leg, owes the shape surface: %s", lang, why)
		}
	}
}
