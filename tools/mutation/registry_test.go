package main

import (
	"path/filepath"
	"testing"
)

// The nightly's target matrix is the conformance registry, and a target's
// scope is built from this package's table. A target in one and not the other
// is a target the sweep would skip without saying so, or a matrix row with no
// scope to run.
func TestTheTargetScopesAreTheConformanceRegistry(t *testing.T) {
	root := repo(t)
	entries, err := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "ci.json"))
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, e := range entries {
		registered[filepath.Base(filepath.Dir(e))] = true
	}
	if len(registered) == 0 {
		t.Fatal("the conformance registry is empty: this test read nothing")
	}
	scoped := map[string]bool{}
	for _, s := range targetScopes {
		scoped[s.lang] = true
		if !registered[s.lang] {
			t.Errorf("target scope %s has no test/conformance/%s/ci.json", s.lang, s.lang)
		}
	}
	for lang := range registered {
		if !scoped[lang] {
			t.Errorf("test/conformance/%s/ci.json registers a target with no mutation scope: add it to targetScopes", lang)
		}
	}
}
