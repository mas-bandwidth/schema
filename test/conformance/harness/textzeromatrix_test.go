package main

import "testing"

// THE TEXT-ZERO MATRIX is one shared corpus (MANIFEST.txt, "THE TEXT-ZERO
// MATRIX"): nine forms text storage takes, each with the zero at the front, the
// middle and the end, and every port answers every row through this harness.
// Losing a row, or turning one to read, would quietly shrink what every leg
// owes, so the matrix is pinned whole here, with the \u0001 control beside each
// zero that has one (a control is what stops a zero row passing by refusing for
// some other reason).
func TestTextZeroMatrixRows(t *testing.T) {
	t.Parallel()
	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	forms := []struct {
		name, unit, root string
		control          bool
	}{
		{"s", "blobdemo", "Catalog", true},
		{"t", "tabledemo", "RootConfig", true},
		{"l", "featurewitnesses", "Root", true},
		{"o", "featurewitnesses", "Root", true},
		{"m", "featurewitnesses", "Root", true},
		{"i", "tabledemo", "PackConfig", true},
		{"at", "featurewitnesses", "Root", false},
		{"k", "mapdemo", "Text", false},
		{"u", "featurewitnesses", "Root", true},
	}
	rows := 0
	for _, f := range forms {
		for _, pos := range []string{"front", "middle", "end"} {
			zero := findHostile(t, m, "str-zero-"+f.name+"-"+pos)
			if zero.Unit != f.unit || zero.Root != f.root || !zero.Refused {
				t.Errorf("%s: want %s %s refused, got %s %s %s", zero.Name, f.unit, f.root, zero.Unit, zero.Root, zero.Verdict())
			}
			rows++
			if !f.control {
				continue
			}
			ctl := findHostile(t, m, "str-zero-ctl-"+f.name+"-"+pos)
			if ctl.Unit != f.unit || ctl.Root != f.root || ctl.Refused {
				t.Errorf("%s: want %s %s read, got %s %s %s", ctl.Name, f.unit, f.root, ctl.Unit, ctl.Root, ctl.Verdict())
			}
		}
	}
	if rows != 27 {
		t.Fatalf("the matrix is 9 forms x 3 positions, pinned %d rows", rows)
	}
	// THE ZERO THE READ ONLY SKIPS (§16.3, skipped-value-is-validated): in an
	// unknown key's own name, in a string in an unknown key's array, and in an
	// object key inside an unknown key's object. Each refuses, and its \u0001
	// control reads.
	for _, place := range []string{"key-name", "list", "object-key"} {
		zero := findHostile(t, m, "str-zero-skip-"+place)
		if zero.Unit != "tabledemo" || zero.Root != "RootConfig" || !zero.Refused {
			t.Errorf("%s: want tabledemo RootConfig refused, got %s %s %s", zero.Name, zero.Unit, zero.Root, zero.Verdict())
		}
		ctl := findHostile(t, m, "str-zero-ctl-skip-"+place)
		if ctl.Unit != "tabledemo" || ctl.Root != "RootConfig" || ctl.Refused {
			t.Errorf("%s: want tabledemo RootConfig read, got %s %s %s", ctl.Name, ctl.Unit, ctl.Root, ctl.Verdict())
		}
	}
}
