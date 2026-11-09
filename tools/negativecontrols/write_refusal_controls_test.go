package main

import (
	"fmt"
	"testing"
)

// TestWriteRefusalControlsExistForEveryLeg holds docs/SPEC.md §5.1's claim that
// each leg carries a control for the variable, fixed and message write-refusal
// surfaces. The leg's controls are make targets, so a leg that names a surface
// in its driver but ships no control for it is a gate nobody has seen go red.
func TestWriteRefusalControlsExistForEveryLeg(t *testing.T) {
	root := testRoot(t)
	defs, err := enumerate(root)
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]string{}
	for _, d := range defs {
		defined[d.Target] = d.File
	}
	m, err := loadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	covered, err := m.covered()
	if err != nil {
		t.Fatal(err)
	}
	surfaces := []string{"write-refusal", "write-refusal-fixed", "write-refusal-message"}
	for _, lang := range []string{"go", "cpp", "c", "rust", "cs"} {
		for _, surface := range surfaces {
			target := fmt.Sprintf("conformance-negative-control-%s-%s", lang, surface)
			if _, ok := defined[target]; !ok {
				t.Errorf("%s is not a make target: %s declares the %s write-refusal surface and its control is missing", target, lang, surface)
			}
			if _, ok := covered[target]; !ok {
				t.Errorf("%s is not in %s, so no leg runs it", target, manifestPath)
			}
		}
	}
}
