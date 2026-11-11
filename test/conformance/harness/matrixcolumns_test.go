package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

// Every row of the matrix is its surface's name, at least one space, then
// the first leg's cell. The negative controls read a surface's verdict off
// that row with `^<surface> +FAIL` and `^<surface> +pass`, so a name that runs
// into its cell hides the verdict from every control that watches it.
func TestMatrixRowsSeparateTheSurfaceFromItsCell(t *testing.T) {
	t.Parallel()
	results := map[string]map[string]*result{"go": {}}
	for _, surface := range surfaces {
		results["go"][surface] = &result{pass: 1, total: 2, failures: []string{"planted"}}
	}
	var table bytes.Buffer
	report(&table, []string{"go"}, results)
	for _, surface := range surfaces {
		row := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(surface) + ` +FAIL 1/2`)
		if !row.MatchString(table.String()) {
			t.Errorf("the %s row does not read `%s +FAIL`:\n%s", surface, surface, table.String())
		}
	}
	// and the header lines up with the rows beneath it
	lines := strings.Split(table.String(), "\n")
	var header, first string
	for i, line := range lines {
		if strings.HasPrefix(line, "surface") && i+2 < len(lines) {
			header, first = line, lines[i+2]
			break
		}
	}
	if strings.Index(header, "go") != strings.Index(first, "FAIL") {
		t.Errorf("the leg column starts at %d in the header and %d in the first row:\n%s\n%s",
			strings.Index(header, "go"), strings.Index(first, "FAIL"), header, first)
	}
}
