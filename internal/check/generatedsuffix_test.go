package check

import (
	"strings"
	"testing"
)

// TestEveryEmittedGoSuffixIsClaimed pins every package-level suffix Go's
// table surface emits that §11's claim list must cover: ReadStrict,
// ToJsonMeasureReason, FixedSaveReason, FixedCheckBody, FixedCheckBodyExact,
// LoadTextForm, SaveTextForm and EditTextForm, plus the fixed form's laid-once
// data and its bound probe — FixedResetImage, FixedHoles, FixedLineageHoles,
// FixedPlanMax and GenPastBound. A declaration colliding with any of them must
// be refused by name at check time, because otherwise `table Ship` plus
// `type ShipReadStrict` produces Go that does not compile
// (docs/SPEC-TABLES.md §11).
func TestEveryEmittedGoSuffixIsClaimed(t *testing.T) {
	t.Parallel()
	suffixes := []string{
		"ReadStrict",
		"ToJsonMeasureReason",
		"FixedSaveReason",
		"FixedCheckBody",
		"FixedCheckBodyExact",
		"LoadTextForm",
		"SaveTextForm",
		"EditTextForm",
		"FixedResetImage",
		"FixedHoles",
		"FixedLineageHoles",
		"FixedPlanMax",
		"GenPastBound",
	}
	for _, suffix := range suffixes {
		suffix := suffix
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			src := "package t\ntable Ship { x int32 }\ntype Ship" + suffix + " { y int32 }\n"
			errs := runUnit(t, map[string]string{"T.schema": src})
			if len(errs) == 0 {
				t.Fatalf("table Ship plus type Ship%s must be refused by name, but the checker accepted it", suffix)
			}
			var joined strings.Builder
			for _, e := range errs {
				joined.WriteString(e.Error())
				joined.WriteString("\n")
			}
			if !strings.Contains(joined.String(), "Ship"+suffix) {
				t.Errorf("the refusal must name the colliding symbol Ship%s: %v", suffix, joined.String())
			}
			if !strings.Contains(joined.String(), "Ship") {
				t.Errorf("the refusal must name the table Ship: %v", joined.String())
			}
		})
	}

	t.Run("a near-miss spelling is accepted", func(t *testing.T) {
		t.Parallel()
		src := "package t\ntable Ship { x int32 }\ntype ShipReadStrictly { y int32 }\n"
		if errs := runUnit(t, map[string]string{"T.schema": src}); len(errs) > 0 {
			t.Fatalf("type ShipReadStrictly is not a claimed suffix and must be accepted: %v", errs)
		}
	})
}
