// The C# cells of docs/PORTING.md for controls that exist in the tree (#411,
// #414, #421). In its own file so a port adds a file and edits no shared one.
package compiler

import (
	"slices"
	"strings"
	"testing"
)

func TestPortingRegisterNamesTheCsControls(t *testing.T) {
	t.Parallel()
	register, _ := readPortingInputs(t)
	reg, err := parsePortingRegister(register)
	if err != nil {
		t.Fatal(err)
	}
	findRow := func(prefix string) portingRow {
		t.Helper()
		for _, r := range reg.Rows {
			if strings.HasPrefix(r.Title, prefix) {
				return r
			}
		}
		t.Fatalf("no technique section with prefix %q", prefix)
		return portingRow{}
	}

	for _, tc := range []struct {
		prefix      string
		wantTests   []string
		wantTargets []string
	}{
		{
			prefix:    "M13 ",
			wantTests: []string{"TestCsDescriptorsAreSafelyPublished"},
		},
		{
			prefix:    "I6 ",
			wantTests: []string{"TestCsRuntimeNameScanGoesRed"},
		},
		{
			prefix: "J1 ",
			wantTargets: []string{
				"tables-cs-accessor-descriptor-agreement",
				"tables-cs-accessor-negative-control",
				"tables-cs-slot-negative-control",
			},
		},
	} {
		row := findRow(tc.prefix)
		cell := row.Cells["cs"]
		if !strings.HasPrefix(cell, "✅") {
			t.Errorf("%s / cs: cell is %q, want a carried ✅ cell", row.Title, cell)
			continue
		}
		targets, tests, _ := cellNames(cell)
		for _, want := range tc.wantTests {
			if !slices.Contains(tests, want) {
				t.Errorf("%s / cs: cell %q does not name test %q", row.Title, cell, want)
			}
		}
		for _, want := range tc.wantTargets {
			if !slices.Contains(targets, want) {
				t.Errorf("%s / cs: cell %q does not name target %q", row.Title, cell, want)
			}
		}
	}
}
