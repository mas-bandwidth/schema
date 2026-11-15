package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Stored NUL refuses for every port, and so does a NUL in the value of an
// unknown key the read only skips (docs/rulings.json
// skipped-value-is-validated). No carried-by metadata creates an exemption.
func TestHostileZeroEscapeRows(t *testing.T) {
	t.Parallel()
	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"str-zero-byte-bounded", "str-zero-byte-bounded-nested", "str-zero-escape-value", "str-zero-escape-map-key", "str-zero-escape-unknown-key"} {
		row := findHostile(t, m, name)
		for _, lang := range []string{"go", "cpp", "dart", "lobster", "unported"} {
			if row.VerdictFor(lang) != "refused" {
				t.Fatalf("%s/%s lost refusal", name, lang)
			}
			wants, exemptions := hostileExpectations(m, lang)
			if len(exemptions) != 0 {
				t.Fatalf("port exemptions survived: %v", exemptions)
			}
			var want expectation
			for _, e := range wants {
				if e.name == name {
					want = e
					break
				}
			}
			if string(want.want) != "refused\n" {
				t.Fatalf("%s/%s: %+v", name, lang, want)
			}
			dir := t.TempDir()
			for _, verdict := range []string{"read\n", "refused\n"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(verdict), 0600); err != nil {
					t.Fatal(err)
				}
				result := &result{}
				scoreAnswer(lang, "json-hostile", dir, want, Instance{}, result)
				if (result.pass == 1) != (verdict == "refused\n") {
					t.Fatalf("%s/%s verdict %q: %+v", name, lang, verdict, result)
				}
			}
		}
	}
}

func findHostile(t *testing.T, m *Manifest, name string) *Hostile {
	t.Helper()
	for i := range m.Hostiles {
		if m.Hostiles[i].Name == name {
			return &m.Hostiles[i]
		}
	}
	t.Fatalf("the manifest names no hostile row %q", name)
	return nil
}
