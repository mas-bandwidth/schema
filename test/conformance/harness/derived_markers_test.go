package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDerivedManifestCarriesInstanceMarkers holds the derived manifest to
// FORMAT.md: an instance line keeps `no-text` and `wire-refused`, in that
// order, so a driver can tell a wire-refused instance's refused load (its
// answer) from any other refused load (a failure), and a plain instance
// carries neither.
func TestDerivedManifestCarriesInstanceMarkers(t *testing.T) {
	t.Parallel()
	m := &Manifest{Instances: []Instance{
		{Name: "plain", Unit: "u", Root: "R", Wire: "p.bin"},
		{Name: "wireonly", Unit: "u", Root: "R", Wire: "w.bin", NoText: true},
		{Name: "refused", Unit: "u", Root: "R", Wire: "r.bin", NoText: true, WireRefused: true},
	}}
	path := filepath.Join(t.TempDir(), "manifest.txt")
	if err := deriveManifest(m, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"instance plain u R p.bin\n",
		"instance wireonly u R w.bin no-text\n",
		"instance refused u R r.bin no-text wire-refused\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the derived manifest has no line %q:\n%s", strings.TrimSpace(want), data)
		}
	}
}
