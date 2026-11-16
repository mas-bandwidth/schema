package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestSharedBlobWitnessSpellings examines both input and saved wire for every
// shared-blob witness. Shared identity is a graph verdict, not a spelling.
func TestSharedBlobWitnessSpellings(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	dir := filepath.Join(root, "testdata/conformance/feature-witnesses/blob-shared-report")
	u, err := compiler.New().Load([]string{filepath.Join(dir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("missing Root")
	}
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []blobWitness
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty blob witness corpus")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if c.Name == "" || seen[c.Name] {
			t.Fatalf("duplicate or empty witness name %q", c.Name)
		}
		seen[c.Name] = true
		for _, entry := range []struct{ name, hex string }{{"wire", c.Wire}, {"saved", c.Saved}} {
			if entry.name == "saved" && entry.hex == "" {
				continue
			}
			b, err := hex.DecodeString(entry.hex)
			if err != nil {
				t.Errorf("%s/%s: %v", c.Name, entry.name, err)
				continue
			}
			if len(b) == 0 || b[0] != 1 {
				t.Errorf("%s/%s: missing variable file", c.Name, entry.name)
				continue
			}
			if fault := framingTableFault(framingVector{}, b); fault != "" {
				t.Errorf("%s/%s: %s", c.Name, entry.name, fault)
			}
			for _, shapes := range []map[string]bool{rootStructuralSpellings(st, b), rootStoredDefaults(st, b), nestedStructuralSpellings(u, st, b), nestedStoredDefaults(u, st, b)} {
				if len(shapes) > 0 {
					t.Errorf("%s/%s: %v", c.Name, entry.name, shapes)
				}
			}
		}
	}
}
