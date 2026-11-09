package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// EVERY INPUT THAT READS IS A REWRITE ROW (docs/SPEC-TABLES.md §22.7): each
// report row whose pinned verdict reads and each framing row whose hand verdict
// reads is read by every leg and written back, so a row added to either list
// without its rewrite row would be an input no leg is held to writing.
func TestEveryReadingRowIsARewriteRow(t *testing.T) {
	m, pinned, _ := corpus(t)
	rows := map[string]RewriteCase{}
	for _, rw := range m.Rewrites {
		rows[rw.Name] = rw
	}
	need := func(name, unit, root, wire, save string) {
		rw, ok := rows[name]
		if !ok {
			t.Errorf("%s reads and has no rewrite row: add `rewrite %s %s %s %s`", name, name, unit, root, wire)
			return
		}
		if rw.Unit != unit || rw.Root != root || rw.Wire != wire {
			t.Errorf("rewrite %s reads %s %s %s, and its source row reads %s %s %s", name, rw.Unit, rw.Root, rw.Wire, unit, root, wire)
		}
		if save != "" {
			wantSave, err := os.ReadFile(save)
			if err != nil {
				t.Errorf("%s: missing save-wire %s: %v", name, save, err)
				return
			}
			wantRewrite, err := os.ReadFile(rw.Canonical)
			if err != nil {
				t.Errorf("%s: missing rewrite %s: %v", name, rw.Canonical, err)
				return
			}
			if !bytes.Equal(wantSave, wantRewrite) {
				t.Errorf("%s: save-wire (%d B) and rewrite (%d B) disagree", name, len(wantSave), len(wantRewrite))
			}
		}
	}
	for _, r := range m.Reports {
		if pinned[r.Name] == "read" {
			need(r.Name, r.Unit, r.Root, r.Wire, r.Save)
		}
	}
	for _, fc := range m.Framings {
		if !fc.Refused {
			need(fc.Name, fc.Unit, fc.Root, fc.Wire, fc.Save)
		}
	}
}

// THE CANONICAL BYTES ARE THE ORACLE'S, AND A FIXED POINT: each committed
// rewrite/<case>.bin is what the oracle writes for its input today, and what it
// writes again for itself. A canonical file edited by hand, or an oracle whose
// writer moved, is red here before any leg is asked.
func TestRewriteCanonicalBytesAreTheOraclesFixedPoint(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.Rewrites) == 0 {
		t.Fatal("the manifest names no rewrite row")
	}
	for _, rw := range m.Rewrites {
		input, err := os.ReadFile(rw.Wire)
		if err != nil {
			t.Fatal(err)
		}
		pinned, err := os.ReadFile(rw.Canonical)
		if err != nil {
			t.Fatalf("%s: %v — run: make conformance-generate", rw.Name, err)
		}
		got, err := rewriteOnce(u, rw.Unit, rw.Root, input)
		if err != nil {
			t.Errorf("%s: %v", rw.Name, err)
			continue
		}
		if !bytes.Equal(got, pinned) {
			t.Errorf("%s: the oracle writes %d bytes, and %d are pinned", rw.Name, len(got), len(pinned))
			continue
		}
		again, err := rewriteOnce(u, rw.Unit, rw.Root, pinned)
		if err != nil || !bytes.Equal(again, pinned) {
			t.Errorf("%s: the canonical bytes are not a fixed point (%v)", rw.Name, err)
		}
	}
}

// A REWRITE ROW THAT CHANGES NOTHING TESTS NOTHING about canonicalization, so
// the hand-built rows must be inputs that are NOT already canonical, while a
// repeated field is refused under the strict-only read.
func TestHandBuiltRewriteInputsAreNotCanonical(t *testing.T) {
	m, _, u := corpus(t)
	fleetWire, err := os.ReadFile("testdata/wire/tables/w1_fleet.bin")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rewrite_fields_reversed", "rewrite_ids_reversed"} {
		var rw *RewriteCase
		for i := range m.Rewrites {
			if m.Rewrites[i].Name == name {
				rw = &m.Rewrites[i]
			}
		}
		if rw == nil {
			t.Errorf("the manifest names no rewrite row %s", name)
			continue
		}
		input, err := os.ReadFile(rw.Wire)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := os.ReadFile(rw.Canonical)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(input, canonical) {
			t.Errorf("%s: the input is already canonical, so the row holds no writer to anything", name)
		}
		if !bytes.Equal(canonical, fleetWire) {
			t.Errorf("%s: canonical bytes do not match w1_fleet.bin", name)
		}
	}
	// Under strict-only (§3), a field written twice is refused on read rather
	// than rewritten.
	dupWire, err := os.ReadFile("testdata/wire/tables/rewrite_field_twice.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rewriteOnce(u, "tblw1", "Fleet", dupWire); err == nil {
		t.Errorf("rewrite_field_twice: expected strict read to refuse duplicate field id")
	}
}

// THE BUILDER'S SORT (docs/SPEC-TABLES.md §22.2.1): the edge_map_keys value as
// a hand-authored text whose map keys arrive in reverse / UTF-16 order must
// pack to the exact canonical bytes of edge_map_keys.bin, and formatting
// edge_map_keys.bin must produce the canonical edge_map_keys.json.
func TestRewriteUnsortedMapKeysPackToCanonicalBytes(t *testing.T) {
	_, _, u := corpus(t)
	unit, err := u.get("mapdemo")
	if err != nil {
		t.Fatal(err)
	}
	unsortedText, err := os.ReadFile("testdata/conformance/tables/rewrite/edge_map_keys_unsorted.json")
	if err != nil {
		t.Fatal(err)
	}
	canonicalText, err := os.ReadFile("testdata/conformance/tables/rewrite/edge_map_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(unsortedText, canonicalText) {
		t.Fatal("edge_map_keys_unsorted.json is already in canonical order")
	}
	canonicalWire, err := os.ReadFile("testdata/wire/tables/edge_map_keys.bin")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	inst := model.New(model.Lookup("Fleet"))
	var rep tabletext.Report
	if !strictReadJSON(model, inst, unsortedText, &rep) {
		t.Fatalf("edge_map_keys_unsorted.json failed strict read: %+v", rep)
	}
	packed, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatalf("Encode(edge_map_keys_unsorted): %v", err)
	}
	if !bytes.Equal(packed, canonicalWire) {
		t.Errorf("edge_map_keys_unsorted packed to %d bytes, want %d bytes of edge_map_keys.bin", len(packed), len(canonicalWire))
	}
	formatted, err := model.Write(inst)
	if err != nil {
		t.Fatalf("Write(edge_map_keys_unsorted): %v", err)
	}
	if !bytes.Equal(oneText(formatted), canonicalText) {
		t.Errorf("formatted JSON does not match canonical edge_map_keys.json")
	}
	scratch := t.TempDir()
	one := filepath.Join(scratch, "edge_map_keys_unsorted")
	if err := os.MkdirAll(one, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(one, "Fleet.json"), unsortedText, 0o600); err != nil {
		t.Fatal(err)
	}
	back, _, packRep, err := u.c.Pack(unit, "Fleet", one)
	if err != nil || !packRep.Silent() || !bytes.Equal(back, canonicalWire) {
		t.Errorf("compiler.Pack(edge_map_keys_unsorted): err=%v rep=%+v match=%t", err, packRep, bytes.Equal(back, canonicalWire))
	}
}

// TestRewriteDerivedManifestAndGenerateRoundTrip verifies that
// deriveRewriteManifest produces a valid manifest of instance rows, that
// generateRewrites reproduces the committed canonical bytes in a scratch tree,
// and that scoreAnswer holds a leg's wire output to those exact bytes.
func TestRewriteDerivedManifestAndGenerateRoundTrip(t *testing.T) {
	m, _, u := corpus(t)
	tmp := t.TempDir()
	derivedPath := filepath.Join(tmp, "manifest-rewrite.txt")
	if err := deriveRewriteManifest(m, derivedPath); err != nil {
		t.Fatal(err)
	}
	derived, err := ReadManifest(derivedPath, filepath.Join(tmp, "json"))
	if err != nil {
		t.Fatalf("ReadManifest(derived): %v", err)
	}
	if len(derived.Instances) != len(m.Rewrites) {
		t.Fatalf("derived instances = %d, want %d", len(derived.Instances), len(m.Rewrites))
	}
	if err := generateRewrites(u, m, filepath.Join(tmp, "json")); err != nil {
		t.Fatalf("generateRewrites: %v", err)
	}
	outDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rw := range m.Rewrites {
		want, err := os.ReadFile(rw.Canonical)
		if err != nil {
			t.Fatal(err)
		}
		gen, err := os.ReadFile(filepath.Join(tmp, "rewrite", rw.Name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gen, want) {
			t.Errorf("%s: generated bytes differ from committed canonical bytes", rw.Name)
		}
		if err := os.WriteFile(filepath.Join(outDir, rw.Name), want, 0o600); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("go", "wire", outDir, expectation{name: rw.Name, want: want}, Instance{Name: rw.Name}, r)
		if r.pass != 1 {
			t.Errorf("%s: canonical bytes did not score pass: %v", rw.Name, r.failures)
		}
		corrupted := bytes.Clone(want)
		corrupted[len(corrupted)-1] ^= 0xff
		if err := os.WriteFile(filepath.Join(outDir, rw.Name), corrupted, 0o600); err != nil {
			t.Fatal(err)
		}
		rBad := &result{total: 1}
		scoreAnswer("go", "wire", outDir, expectation{name: rw.Name, want: want}, Instance{Name: rw.Name}, rBad)
		if rBad.pass != 0 || len(rBad.failures) == 0 {
			t.Errorf("%s: non-canonical bytes scored pass", rw.Name)
		}
	}
}
