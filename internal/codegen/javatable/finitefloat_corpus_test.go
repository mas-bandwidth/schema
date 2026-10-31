package javatable

import (
	"sort"
	"strconv"
	"strings"
	"testing"
)

// manifestNonFinite reads, per file of the fixed-form corpus manifest, the leaf
// name of the first value the reference wrote as a NaN or an infinity, and ""
// for a file whose floats are all finite. Under the finite rule of 2026-10-27
// (docs/SPEC-TABLES.md §3.3) such a file is a MUST-REFUSE vector for this leg:
// the C++ reference's dump still writes those patterns until the C++ card of
// the finite-tables plan (tables-finite/cpp) regenerates the corpus with
// finite values, and a leg that refuses them is the leg that keeps the rule.
// The manifest decides, so each identity row comes back by itself the day the
// corpus turns finite. The keys are taken in sorted order (r0 first); every
// corpus file today carries one non-finite field, so that is the one named.
func manifestNonFinite(t *testing.T, corpus string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for file, row := range manifestRows(t, corpus) {
		keys := make([]string, 0, len(row.Values))
		for k := range row.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !manifestValueNonFinite(row.Values[k]) {
				continue
			}
			leaf := k
			if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
				leaf = leaf[i+1:]
			}
			if i := strings.IndexByte(leaf, '['); i >= 0 {
				leaf = leaf[:i]
			}
			out[file] = leaf
			break
		}
	}
	return out
}

// manifestValueNonFinite answers whether one dumped value, `<decimal>|0x<bits>`
// at eight hex digits (a float32) or sixteen (a float64), is a NaN or an
// infinity: its exponent bits all ones.
func manifestValueNonFinite(v string) bool {
	_, hexPart, ok := strings.Cut(v, "|0x")
	if !ok {
		return false
	}
	bits, err := strconv.ParseUint(hexPart, 16, 64)
	if err != nil {
		return false
	}
	switch len(hexPart) {
	case 8:
		return bits&0x7f800000 == 0x7f800000
	case 16:
		return bits&0x7ff0000000000000 == 0x7ff0000000000000
	}
	return false
}

// manifestBitsHex is one dumped float's bits, as the hex digits the manifest
// spells (eight for a float32, sixteen for a float64): what a probe sets so
// the value it writes is the reference's own, finite or not.
func manifestBitsHex(t *testing.T, rows map[string]manifestRow, file, path string) string {
	t.Helper()
	v, ok, why := manifestLookup(rows, file, "values", path)
	if !ok {
		t.Fatalf("%s", why)
	}
	_, hexPart, has := strings.Cut(v, "|0x")
	if !has {
		t.Fatalf("%s %s = %q carries no |0x<bits> half", file, path, v)
	}
	return hexPart
}

// TestManifestNonFinite pins the manifest reading the corpus gates stand on:
// the first NaN or infinity a file's values spell, at either width, by its
// leaf name, and nothing for a file whose floats are all finite.
func TestManifestNonFinite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"nan|0x7F800001", true},
		{"-nan|0xFFA5A5A5", true},
		{"inf|0x7FF0000000000000", true},
		{"nan|0x7FF00DEFACED0001", true},
		{"1.5|0x3FC00000", false},
		{"-0|0x80000000", false},
		{"4.9e-324|0x0000000000000001", false},
		{"-2.25|0xC002000000000000", false},
		{"11", false},
	} {
		if got := manifestValueNonFinite(tc.v); got != tc.want {
			t.Errorf("manifestValueNonFinite(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
