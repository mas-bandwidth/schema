package check

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryEmittedGoSuffixIsClaimed pins every package-level suffix Go emits
// that the section 11 claim list must cover. A declaration colliding with any
// of them must be refused by name at check time; otherwise the generated package fails
// to compile (docs/SPEC-TABLES.md section 11).
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
		"Add",
		"Erase",
		"Each",
		"Insert",
		"Find",
		"IndexMeasure",
		"Index",
		"IndexFind",
		"Append",
		"AppendAbsent",
		"TextAt",
		"Type",
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

var packageSuffixDeclaration = regexp.MustCompile("\\b(func|var|type|const) %(\\[1\\])?s([A-Z][A-Za-z0-9_]*)")

func packageLevelGoSuffixes(source string) []string {
	matches := packageSuffixDeclaration.FindAllStringSubmatch(source, -1)
	suffixes := make([]string, 0, len(matches))
	for _, match := range matches {
		suffixes = append(suffixes, match[3])
	}
	return suffixes
}

// TestEmitterTemplatesNameOnlyClaimedSuffixes keeps the section 11 registry complete
// as the Go emitter grows. Methods are excluded by the declaration pattern;
// this scan covers package-level names only.
func TestEmitterTemplatesNameOnlyClaimedSuffixes(t *testing.T) {
	t.Parallel()
	control := packageLevelGoSuffixes("func %sNotClaimedXyz(")
	if len(control) != 1 || control[0] != "NotClaimedXyz" {
		t.Fatalf("control declaration was not recognized: %v", control)
	}

	const dir = "../codegen/gotable"
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read Go table emitter directory: %v", err)
	}
	for _, entry := range files {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			for _, suffix := range packageLevelGoSuffixes(line) {
				claimed := false
				for _, verb := range tableGeneratedVerbs {
					if suffix == verb {
						claimed = true
						break
					}
				}
				if !claimed {
					t.Errorf("%s:%d: package-level Go table suffix %s is not in tableGeneratedVerbs", path, lineNo+1, suffix)
				}
			}
		}
	}
}
