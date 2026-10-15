package javatable

// fixedroadmap_t18 — the java leg's shared byte oracle (java/W13, java/R27)
// and hostile-input (java/P3, java/P4) rows of docs/roadmap.sexp's node
// `fixed-tables`. Law: docs/FIXED-FORM-ALGORITHM.md §2 (the header: form byte
// at 0, hash at 8, layout length at 16, layout at 20) and §7 (the block-equals-
// the-reference, manifest-is-the-wire, hostile sweep and wrong-plan checks);
// docs/SPEC-TABLES.md §3.4 (the record and its constant sizes).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixedJavaT18ManifestAndHeader is the unit tier, pure Go: no javac, no
// java, no corpus beside a synthetic manifest written in t.TempDir(). It proves
// the manifest reader (readManifest) returns a forged row's `forged` values and
// a plain row's `values`, and names a refused row; and that a header built by
// hand (§2) parses to its hash and layout, with a one-hash-byte flip caught.
func TestFixedJavaT18ManifestAndHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := "" +
		"file=hostile_cfloat_range_widen.bin row=cfloat_range_widen side=hostile root=CfloatRangeWiden records=1 " +
		"forged=r0.aim@104=1.5|0x3FC00000 " +
		"values=r0.lead=1,r0.aim=0.5|0x3F000000,r0.trail=2\n" +
		"file=plain.bin row=plain side=none root=Plain records=2 values=a=7,b=8,c=9\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := manifestRows(t, dir)
	forged := rows["hostile_cfloat_range_widen.bin"]
	if forged.Values["r0.lead"] != "1" || forged.Values["r0.aim"] != "0.5|0x3F000000" {
		t.Errorf("forged row's values = %v, want the OLD writer's values before the forge", forged.Values)
	}
	if got := forged.Forged["r0.aim@104"]; got != "1.5|0x3FC00000" {
		t.Errorf("forged row's forged r0.aim@104 = %q, want the forged value", got)
	}
	plain := rows["plain.bin"]
	if plain.Values["a"] != "7" || plain.Values["b"] != "8" || plain.Values["c"] != "9" {
		t.Errorf("plain row's values = %v, want the declared values", plain.Values)
	}
	if plain.Forged != nil && len(plain.Forged) != 0 {
		t.Errorf("plain row carries forged values %v, want none", plain.Forged)
	}

	// A refused row is named: the reader must fail loudly for a file the
	// manifest does not carry, never return an empty oracle, and must name the
	// value a section does not hold.
	if _, ok, err := manifestLookup(rows, "no_such_file.bin", "values", "x"); ok {
		t.Error("the lookup found a file the manifest does not carry")
	} else if !strings.Contains(err, "no_such_file.bin") {
		t.Errorf("the refused row was not named: %q", err)
	}
	if _, ok, err := manifestLookup(rows, "plain.bin", "values", "nothere"); ok {
		t.Error("the lookup found a value the section does not hold")
	} else if !strings.Contains(err, "nothere") {
		t.Errorf("the missing value was not named: %q", err)
	}

	// The header (§2): form byte at 0, hash at 8, layout length at 16, layout
	// at 20. Build one in Go and parse it; flip one hash byte and the parse
	// must catch it.
	hash := uint64(0x0123456789ABCDEF)
	layout := []byte{3, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0}
	good := buildFixedHeader(hash, layout)
	gotHash, gotLayout, ok := parseFixedHeader(good)
	if !ok {
		t.Fatal("the good header did not parse")
	}
	if gotHash != hash {
		t.Errorf("header hash = %#x, want %#x", gotHash, hash)
	}
	if string(gotLayout) != string(layout) {
		t.Errorf("header layout = % x, want % x", gotLayout, layout)
	}
	bad := append([]byte(nil), good...)
	bad[8] ^= 0xFF
	badHash, _, ok := parseFixedHeader(bad)
	if !ok {
		t.Fatal("the corrupted header did not parse; the form and lengths are still lawful")
	}
	if badHash == hash {
		t.Error("a header with one hash byte flipped parsed to the same hash; the corruption is not caught")
	}
}

// buildFixedHeader writes a form-3 file header per FIXED-FORM-ALGORITHM §2:
// the form byte at 0, seven zero bytes, the eight-byte hash at 8, the u32
// layout length at 16, and the layout at 20.
func buildFixedHeader(hash uint64, layout []byte) []byte {
	b := make([]byte, 20+len(layout))
	b[0] = 3
	for i := 0; i < 8; i++ {
		b[8+i] = byte(hash >> (8 * i))
	}
	lePut32(b[16:], uint32(len(layout)))
	copy(b[20:], layout)
	return b
}

// parseFixedHeader answers the header's hash and layout, or false when the form
// byte is not 3, the file is shorter than the fixed header, or the declared
// layout length runs past the file.
func parseFixedHeader(b []byte) (hash uint64, layout []byte, ok bool) {
	if len(b) < 20 || b[0] != 3 {
		return 0, nil, false
	}
	for i := 0; i < 8; i++ {
		hash |= uint64(b[8+i]) << (8 * i)
	}
	l := uint64(le32(b[16:]))
	if 20+l > uint64(len(b)) {
		return 0, nil, false
	}
	return hash, b[20 : 20+l], true
}

func lePut32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

