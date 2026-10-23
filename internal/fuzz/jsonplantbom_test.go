package fuzz_test

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestJsonPlantLeadingBomIsAccepted(t *testing.T) {
	t.Parallel()

	for seed := range int64(5) {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()

			r := rand.New(rand.NewSource(seed))
			_, u := randomSchema(r)
			baseJSON := validUnitJSON(r, u)
			specs := buildPlantSpecs(u, baseJSON, false)

			var matches []plantSpec
			for _, spec := range specs {
				if spec.name == "leading_bom" {
					matches = append(matches, spec)
				}
			}
			if len(matches) != 1 {
				t.Fatalf("seed %d: got %d leading_bom plants, want exactly 1", seed, len(matches))
			}
			if matches[0].mode != "must_accept" {
				t.Errorf("seed %d: leading_bom mode = %q, want must_accept", seed, matches[0].mode)
			}
			if want := "\ufeff" + baseJSON; matches[0].text != want {
				t.Errorf("seed %d: leading_bom text = %q, want BOM-prefixed base JSON %q", seed, matches[0].text, want)
			}
		})
	}
}
