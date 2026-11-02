package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

// The original nine are the nightly completion obligation. Other registered
// languages remain visible without being silently promoted to this gate.
var originalNine = []string{"cpp", "c", "cs", "go", "rust", "java", "js", "dart", "elixir"}

type coverageKey struct{ surface, class string }

type coverageExpected struct {
	cases, namedAllowed int
	namedRequired       map[string]int
}

// requiredCoverageShape uses the same expectations and class mapping as a
// real run. No driver, materialisation or reference reread is performed.
// The manifest fixes the denominator independently of received C++ rows.
func requiredCoverageShape(m *Manifest, reports map[string]Counts, jsonDir string) (map[coverageKey]*coverageExpected, error) {
	claims, err := buildClaims(m, nil)
	if err != nil {
		return nil, err
	}
	instances := map[string]Instance{}
	for _, inst := range m.Instances {
		instances[inst.Name] = inst
	}
	shape := map[coverageKey]*coverageExpected{}
	for _, surface := range surfaces {
		want, err := expectations(m, surface, reports, jsonDir)
		if err != nil {
			return nil, fmt.Errorf("%s expectations: %w", surface, err)
		}
		if len(want) == 0 {
			shape[coverageKey{surface, ""}] = &coverageExpected{}
		}
		for _, e := range want {
			key := coverageKey{surface, claims.cases[surface][e.name].class}
			if shape[key] == nil {
				shape[key] = &coverageExpected{namedRequired: map[string]int{}}
			}
			row := shape[key]
			row.cases++
			// Match scoreAnswer's named-refusal rules. Variable messages
			// require the refusal in every language; marked wire rows allow
			// it everywhere and require it for their named ports.
			if surface == "message" && e.want == nil {
				row.namedAllowed++
				for _, lang := range originalNine {
					row.namedRequired[lang]++
				}
			} else if inst := instances[e.name]; surface == "wire" && inst.GoWireRefusal != "" {
				row.namedAllowed++
				for _, lang := range originalNine {
					if inst.WireRefusal(lang) != "" {
						row.namedRequired[lang]++
					}
				}
			}
		}
	}
	total := 0
	for _, row := range shape {
		total += row.cases
	}
	if total == 0 {
		return nil, errors.New("manifest has no coverage cases")
	}
	return shape, nil
}

func originalCoverageProblems(rows []coverageRow, fixed map[string]string, shape map[coverageKey]*coverageExpected) []string {
	required := map[string]bool{}
	for _, lang := range originalNine {
		required[lang] = true
	}
	seen := map[string]map[coverageKey]bool{}
	var problems []string
	for _, row := range rows {
		if !required[row.Lang] || row.Direction != directionReadsCpp {
			continue // expansion languages and optional reread diagnostics
		}
		key := coverageKey{row.Surface, row.Class}
		label := row.Lang + " / " + row.Surface
		if row.Class != "" {
			label += " (" + row.Class + ")"
		}
		want, ok := shape[key]
		if !ok {
			problems = append(problems, label+": unexpected surface/class")
			continue
		}
		if seen[row.Lang] == nil {
			seen[row.Lang] = map[coverageKey]bool{}
		}
		if seen[row.Lang][key] {
			problems = append(problems, label+": duplicate coverage row")
			continue
		}
		seen[row.Lang][key] = true
		if row.Cases != want.cases {
			problems = append(problems, fmt.Sprintf("%s: cases=%d, manifest requires %d", label, row.Cases, want.cases))
			continue
		}
		if row.SurfaceAbsent {
			problems = append(problems, label+": surface is absent")
		}
		// Bounded subtraction checks inconsistent/negative counts without
		// allowing an overflowing damaged JSON counter to appear complete.
		remaining := row.Cases
		valid := true
		for _, n := range []int{row.Answered, row.Failed, row.AbsentClaimed, row.NotCarried, row.Named} {
			if n < 0 || n > remaining {
				valid = false
				break
			}
			remaining -= n
		}
		if !valid || remaining != 0 {
			problems = append(problems, label+": outcome counters do not sum to cases")
			continue
		}
		if row.Failed != 0 || row.AbsentClaimed != 0 || row.NotCarried != 0 {
			problems = append(problems, fmt.Sprintf("%s: incomplete (failed=%d, claimed-absent=%d, not-carried=%d)",
				label, row.Failed, row.AbsentClaimed, row.NotCarried))
		}
		if row.Named < want.namedRequired[row.Lang] || row.Named > want.namedAllowed {
			problems = append(problems, fmt.Sprintf("%s: named refusals=%d, manifest permits %d..%d",
				label, row.Named, want.namedRequired[row.Lang], want.namedAllowed))
		}
	}
	for _, lang := range originalNine {
		for key := range shape {
			if !seen[lang][key] {
				problems = append(problems, fmt.Sprintf("%s / %s (%s): missing required coverage row", lang, key.surface, key.class))
			}
		}
		// C++ generated the fixed corpus before its required coverage run.
		// Each of the eight ports has a separate fixed-form gate receipt.
		if lang != referenceLang && fixed[lang] != "pass" {
			problems = append(problems, fmt.Sprintf("%s: fixed form requires pass, got %q", lang, fixed[lang]))
		}
	}
	sort.Strings(problems)
	return problems
}

// coverageReport always renders available evidence before returning failure.
// Default mode remains an informational report; completion is opt-in.
func coverageReport(w io.Writer, in, manifestPath, jsonDir, reportsPath string, requireNine bool) error {
	rows, fixed, readErr := readCoverage(in)
	coverageMarkdown(w, rows, fixed)
	var problems []string
	if readErr != nil {
		problems = append(problems, "coverage input: "+readErr.Error())
	}
	if requireNine {
		m, err := ReadManifest(manifestPath, jsonDir)
		if err == nil {
			var reports map[string]Counts
			reports, err = readReports(reportsPath)
			if err == nil {
				var shape map[coverageKey]*coverageExpected
				shape, err = requiredCoverageShape(m, reports, jsonDir)
				if err == nil {
					problems = append(problems, originalCoverageProblems(rows, fixed, shape)...)
				}
			}
		}
		if err != nil {
			problems = append(problems, "manifest expectations: "+err.Error())
		}
		fmt.Fprint(w, "## Original nine completion\n\n")
		if len(problems) == 0 {
			fmt.Fprintln(w, "PASS: every original-nine surface/class matches the manifest; all cases are answered or give an allowed named refusal, and all eight port fixed-form gates pass.")
		}
	}
	if len(problems) > 0 {
		fmt.Fprintln(w, "Coverage is incomplete:")
		for _, problem := range problems {
			fmt.Fprintf(w, "\n- %s\n", problem)
		}
		return errors.New("coverage records do not satisfy the requested report")
	}
	return nil
}
