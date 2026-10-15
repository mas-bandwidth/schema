package gotable

import (
	"bytes"
	"strings"
	"testing"
)

func TestGoGenerateSurfaceEmitsOnlyWhatItNames(t *testing.T) {
	t.Parallel()

	const src = `package probe
enum Grade { Gold, Silver }
fixed table Root {
	title string(8) = "fresh"
	health int32 = 7
}`
	u := unitFrom(t, src)

	all := map[string]bool{"wire": true, "fixed": true, "json": true, "cook": true, "block": true, "view": true, "message": true, "retain": true}

	t.Run("default_equals_full_set", func(t *testing.T) {
		t.Parallel()
		def, err := GenerateLineageSurface(u, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		full, err := GenerateLineageSurface(u, nil, all)
		if err != nil {
			t.Fatal(err)
		}
		if len(def) != len(full) {
			t.Fatalf("default has %d files, full set has %d", len(def), len(full))
		}
		for name, b := range def {
			if !bytes.Equal(b, full[name]) {
				t.Fatalf("file %s differs between default and the full set", name)
			}
		}
	})

	surfaces := []struct {
		name string
		only map[string]bool
	}{
		{"wire", map[string]bool{"wire": true}},
		{"fixed", map[string]bool{"fixed": true}},
		{"json", map[string]bool{"json": true}},
		{"cook", map[string]bool{"cook": true}},
		{"block", map[string]bool{"block": true}},
		{"view", map[string]bool{"view": true}},
		{"message", map[string]bool{"message": true}},
		{"retain", map[string]bool{"retain": true}},
		{"block_cook_json_view", map[string]bool{"block": true, "cook": true, "json": true, "view": true}},
	}

	for _, sc := range surfaces {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			files, err := GenerateLineageSurface(u, nil, sc.only)
			if err != nil {
				t.Fatal(err)
			}
			_, hasBlock := files["ProbeBlock.go"]
			_, hasCook := files["ProbeCook.go"]
			var hasJSON bool
			for name := range files {
				if strings.HasSuffix(name, "TableJson.go") {
					hasJSON = true
				}
			}
			_, hasView := files["ProbeView.go"]
			_, hasTable := files["ProbeTable.go"]
			if hasBlock != sc.only["block"] {
				t.Errorf("block file present=%v want=%v", hasBlock, sc.only["block"])
			}
			if hasCook != sc.only["cook"] {
				t.Errorf("cook file present=%v want=%v", hasCook, sc.only["cook"])
			}
			if hasJSON != sc.only["json"] {
				t.Errorf("json file present=%v want=%v", hasJSON, sc.only["json"])
			}
			if hasView != sc.only["view"] {
				t.Errorf("view file present=%v want=%v", hasView, sc.only["view"])
			}
			if !hasTable {
				t.Errorf("per-table source always present=false")
			}
		})
	}
}
