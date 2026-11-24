package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOptInFilterKnowsEveryDerivedLineKind (nightly 45130315968): the opt-in
// filter reads every line kind deriveManifest writes. A kind missing from
// derivedRows refuses the whole manifest for any leg that has not opted into
// every opt-in unit, so that leg runs nothing: the committed corpus is
// derived here and filtered for a leg that lists no unit, the shape the Dart
// text-boundary gate hands the harness.
func TestOptInFilterKnowsEveryDerivedLineKind(t *testing.T) {
	t.Chdir(conformanceRoot(t))
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.OptIn) == 0 {
		t.Fatal("the corpus marks no unit opt-in, so the filter is never run")
	}
	derived := filepath.Join(t.TempDir(), "manifest.txt")
	if err := deriveManifest(m, derived); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(derived)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for line := range strings.SplitSeq(string(text), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			kinds[f[0]] = true
		}
		if _, _, err := derivedLine(line); err != nil {
			t.Error(err)
		}
	}
	for _, k := range []string{"write-refusal-cook", "write-refusal-block", "announce", "read-into", "json-write-into"} {
		if !kinds[k] {
			t.Errorf("the derived corpus carries no %s line, so this test no longer holds the filter to it", k)
		}
	}
	if _, _, _, err := legManifest(m, derived, "leg", map[string]bool{}); err != nil {
		t.Fatalf("a leg that opts into no unit is refused its manifest: %v", err)
	}
}

// TestOptInLinesDropEachNewKindOnItsUnit: each kind added to derivedRows is
// dropped when its unit is omitted and handed when it is not.
func TestOptInLinesDropEachNewKindOnItsUnit(t *testing.T) {
	t.Parallel()
	text := "unit demo a\nunit extra b\n" +
		"write-refusal-cook wc_drop extra R w.bin p int 1\nwrite-refusal-cook wc_keep demo R w.bin p int 1\n" +
		"write-refusal-block wb_drop extra R w.bin p int 1\nwrite-refusal-block wb_keep demo R w.bin p int 1\n" +
		"announce an_drop extra R write -\nannounce an_keep demo R write -\n" +
		"read-into ri_drop extra R var r.bin d.bin 1 -\nread-into ri_keep demo R var r.bin d.bin 1 -\n" +
		"json-write-into jw_drop extra R w.bin\njson-write-into jw_keep demo R w.bin"
	kept, dropped, err := optInLines(text, map[string]bool{"extra": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"write-refusal-cook", "write-refusal-block", "announce", "read-into", "json-write-into"} {
		var pre string
		for line := range strings.SplitSeq(text, "\n") {
			if f := strings.Fields(line); len(f) > 1 && f[0] == kind {
				pre = strings.TrimSuffix(f[1], "_drop")
				pre = strings.TrimSuffix(pre, "_keep")
				break
			}
		}
		if handed(kept, kind, pre+"_drop") || !dropped[pre+"_drop"] {
			t.Errorf("%s %s_drop on the omitted unit was handed", kind, pre)
		}
		if !handed(kept, kind, pre+"_keep") || dropped[pre+"_keep"] {
			t.Errorf("%s %s_keep on a kept unit was not handed", kind, pre)
		}
	}
}
