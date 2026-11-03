package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// strictDir is where the strict-reason vectors live.
const strictDir = "testdata/wire/tables/strict"

// THE STRICT-REASON VECTORS (docs/SPEC-TABLES.md §6.5): wires whose STRICT read
// refuses by a reason no other report or framing wire reaches, hand-built from
// the rule text in the framing vectors' byte-script form. Each has a `strict`
// row in MANIFEST.txt, which only the strict-reason surface reads, so every
// leg's strict reason is compared over it and no other surface is;
// strict-reasons.txt holds the reason it owes.
var strictVectors = []framingVector{
	// §3.3: a plain float whose bits are a NaN. Every read refuses it, tolerant
	// and strict, as non_finite_float. `buff { multiplier = NaN }`: ref 6,
	// kind 13, L 7, then `ref 7, kind 10, 0x7fc00000, 0`, then the root's
	// terminator.
	{name: "strict_non_finite_float", unit: "tblr1", root: "Cfg", ids: cfgIds, body: cat(b(6, 13, 7, 7, 10), u32le(0x7fc00000), b(0, 0))},
	// §2.9: an unbounded array's count of 2^31, one above the int32 storage
	// cap. `values`: ref 1, kind 14, L 6, element kind 4, the count as five
	// LEB128 bytes, no elements; then the root's terminator. The builder read
	// refuses it by name, count_over_extent_cap, and sets no malformed.
	{name: "strict_count_over_extent_cap", unit: "listdemo", root: "Ints", ids: []string{"values"}, body: b(1, 14, 6, 4, 0x80, 0x80, 0x80, 0x80, 0x08, 0)},
}

func u32le(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }

// TestStrictVectorBytesArePinned holds every strict vector's file to its byte
// script, as TestFramingVectorBytesArePinned does for the framing vectors.
// STRICT_WRITE=1 writes the files from the scripts.
func TestStrictVectorBytesArePinned(t *testing.T) {
	_, _, u := corpus(t)
	write := os.Getenv("STRICT_WRITE") == "1"
	for _, v := range strictVectors {
		want, err := v.bytes(u)
		if err != nil {
			t.Fatalf("%s: %v", v.name, err)
		}
		path := filepath.Join(strictDir, v.name+".bin")
		if write {
			if err := os.MkdirAll(strictDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v — run with STRICT_WRITE=1 to write it", v.name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the file is not the script's bytes — run with STRICT_WRITE=1 to rewrite it", v.name)
		}
	}
}

// TestStrictRowsNameEveryVector holds each vector to a `strict` row over its
// unit and root, so no vector sits unread.
func TestStrictRowsNameEveryVector(t *testing.T) {
	m, _, _ := corpus(t)
	rows := map[string]ReportCase{}
	for _, rc := range m.Stricts {
		rows[rc.Name] = rc
	}
	for _, v := range strictVectors {
		row, ok := rows[v.name]
		if !ok {
			t.Errorf("%s: the manifest has no strict row for it", v.name)
			continue
		}
		if row.Wire != filepath.ToSlash(filepath.Join(strictDir, v.name+".bin")) || row.Unit != v.unit || row.Root != v.root {
			t.Errorf("%s: the row reads %s %s.%s", v.name, row.Wire, row.Unit, row.Root)
		}
	}
}

// TestStrictLinesReachOnlyTheStrictSurface: a `strict` line rides the
// strict-reason surface's manifest as a `report` line and is absent from the
// manifest every other surface reads, so no tolerant report is asked of it.
func TestStrictLinesReachOnlyTheStrictSurface(t *testing.T) {
	m, _, _ := corpus(t)
	if len(m.Stricts) == 0 {
		t.Fatal("the manifest carries no strict line")
	}
	dir := t.TempDir()
	runPath, strictPath := filepath.Join(dir, "run.txt"), filepath.Join(dir, "strict.txt")
	if err := deriveManifest(m, runPath); err != nil {
		t.Fatal(err)
	}
	if err := deriveStrictManifest(m, strictPath); err != nil {
		t.Fatal(err)
	}
	run, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatal(err)
	}
	strict, err := os.ReadFile(strictPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range m.Stricts {
		line := []byte("report " + s.Name + " " + s.Unit + " " + s.Root + " ")
		if bytes.Contains(run, []byte(" "+s.Name+" ")) {
			t.Errorf("%s: the run manifest names a strict wire", s.Name)
		}
		if !bytes.Contains(strict, line) {
			t.Errorf("%s: the strict-reason manifest carries no report line for it", s.Name)
		}
	}
	cases := strictReasonCases(m)
	if len(cases) != len(m.Reports)+len(m.Stricts)+len(m.Framings) {
		t.Fatalf("strict-reason cases = %d, want report+strict+framing = %d", len(cases), len(m.Reports)+len(m.Stricts)+len(m.Framings))
	}
}
