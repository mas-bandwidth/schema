package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// framingDir is where the hand-built framing vectors live (docs/LAYERS/1-framing.md).
const framingDir = "testdata/wire/tables/framing"

// TestFramingVectorBytesArePinned holds every vector's file to its byte script:
// the script in framing_vectors_test.go is the readable form and the file is
// what every leg reads, and the two may not drift. FRAMING_WRITE=1 writes the
// files from the scripts, which is how a new vector lands.
func TestFramingVectorBytesArePinned(t *testing.T) {
	_, _, u := corpus(t)
	write := os.Getenv("FRAMING_WRITE") == "1"
	seen := map[string]bool{}
	for _, v := range framingVectors {
		if seen[v.name] {
			t.Errorf("%s: two vectors share the name", v.name)
		}
		seen[v.name] = true
		want, err := v.bytes(u)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		path := filepath.Join(framingDir, v.name+".bin")
		if write {
			if err := os.MkdirAll(framingDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v — run with FRAMING_WRITE=1 to write it", v.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the file is not the script's bytes — run with FRAMING_WRITE=1 to rewrite it", v.name)
		}
	}
}

// TestFramingRowsNameEveryVector holds the manifest and the scripts to one
// list: every vector not held out has a `framing` row, every row names a
// vector, and a held-out vector has none.
func TestFramingRowsNameEveryVector(t *testing.T) {
	m, _, _ := corpus(t)
	rows := map[string]FramingCase{}
	for _, fc := range m.Framings {
		rows[fc.Name] = fc
	}
	for _, v := range framingVectors {
		row, ok := rows[v.name]
		switch {
		case v.held && ok:
			t.Errorf("%s: held out on a finding (docs/LAYERS/1-framing.md) and still a manifest row", v.name)
		case !v.held && !ok:
			t.Errorf("%s: the manifest has no framing row for it", v.name)
		case ok:
			if row.Wire != filepath.ToSlash(filepath.Join(framingDir, v.name+".bin")) {
				t.Errorf("%s: the row reads %s, the vector is %s/%s.bin", v.name, row.Wire, framingDir, v.name)
			}
			if row.Unit != v.unit || row.Root != v.root {
				t.Errorf("%s: the row reads %s.%s, the vector is %s.%s", v.name, row.Unit, row.Root, v.unit, v.root)
			}
			delete(rows, v.name)
		}
	}
	for name := range rows {
		t.Errorf("%s: a framing row with no vector script", name)
	}
}

// TestFramingVectorsReadThroughTheEngine is THE ORACLE CHECK
// (docs/LAYERS/1-framing.md): the Go engine, which every leg is fuzzed
// against, answers every framing vector as the rule text says — the report on
// the manifest row and the value in the row's file. A difference here is an
// engine defect or a finding, never a vector to re-pin from the engine.
func TestFramingVectorsReadThroughTheEngine(t *testing.T) {
	m, _, u := corpus(t)
	for _, fc := range m.Framings {
		unit, err := u.get(fc.Unit)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(fc.Wire)
		if err != nil {
			t.Fatal(err)
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(fc.Root)
		if def == nil {
			t.Fatalf("%s: the unit declares no root %q", fc.Name, fc.Root)
		}
		inst := model.New(def)
		var report tabletext.Report
		_, err = tablewire.Decode(model, inst, data, &report)
		if err != nil && !tablewire.Refused(err) {
			t.Errorf("%s: the engine errored: %v", fc.Name, err)
			continue
		}
		got := Counts{
			Unknown:      report.Unknown,
			KindMismatch: report.KindMismatch,
			Widened:      report.Widened,
			Clamped:      report.Clamped,
			Duplicate:    report.Duplicate,
			Malformed:    report.Malformed,
			Refused:      tablewire.Refused(err),
		}
		want := fc.Counts
		if fc.Refused {
			want = Counts{Refused: true}
		}
		if got != want {
			t.Errorf("%s: the engine reads %s and the rule says %s", fc.Name, got, want)
		}
		if fc.Value == "" || fc.Refused {
			continue
		}
		text, err := model.Write(inst)
		if err != nil {
			t.Errorf("%s: the engine's text writer refused the value: %v", fc.Name, err)
			continue
		}
		if !bytes.HasSuffix(text, []byte("\n")) {
			// the text ends with one newline, which is part of the form rather
			// than a file convention (§16.1); the engine's writer leaves it to
			// the file it lands in, exactly as `harness generate` does
			text = append(text, '\n')
		}
		wantText, err := os.ReadFile(fc.Value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(text, wantText) {
			t.Errorf("%s: the engine's value is not the rule's\n--- engine\n%s--- rule\n%s", fc.Name, text, wantText)
		}
	}
}
