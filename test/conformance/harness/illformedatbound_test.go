package main

import (
	"bytes"
	"testing"
)

// TestIllFormedAtBoundIsRefused pins the str-illformed-at-bound row of the
// hostile battery to the ruling of docs/SPEC-TABLES.md §16.3: ill-formed
// UTF-8 is REFUSED BY NAME at every string site, before any clamp, so the
// row's verdict is `refused` — and a Go answer of the OLD counters (the row
// carried `0,0,0,1,0,false,read` before the ruling) must score red against
// the manifest's expectation.
func TestIllFormedAtBoundIsRefused(t *testing.T) {
	t.Parallel()

	const root = "../../../"
	m, err := ReadManifest(root+"testdata/conformance/tables/MANIFEST.txt", root+"testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}

	const caseName = "str-illformed-at-bound"
	// the counters the row carried before the ruling: clamped, read
	const oldCounters = "0,0,0,1,0,false,read"

	var found bool
	for _, h := range m.Hostiles {
		if h.Name != caseName {
			continue
		}
		found = true
		if !h.Refused {
			t.Fatalf("%s: the manifest verdict is %s; §16.3 says ill-formed UTF-8 is refused by name at every string site, before any clamp", caseName, h.Verdict())
		}
		// the expectation the harness scores the row against (run.go writes
		// the verdict for the json-hostile surface)
		want := []byte(h.Verdict() + "\n")
		if bytes.Equal([]byte(oldCounters+"\n"), want) {
			t.Fatalf("%s: the expectation %q equals the old counters; the ruling changed nothing", caseName, want)
		}
	}
	if !found {
		t.Fatalf("the manifest names no hostile row %q", caseName)
	}
}
