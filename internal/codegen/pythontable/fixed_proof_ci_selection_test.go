package pythontable_test

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

func TestPythonFixedProofCISelection(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make (fixed proof CI row selection)")
	cmd := exec.Command("make", "-s", "python-fixed-proof-ci-patterns")
	cmd.Dir = "../../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list fixed proof rows: %v\n%s", err, out)
	}
	rows := map[string][]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			rows[fields[0]] = append(rows[fields[0]], fields[1])
		}
	}
	var types []string
	for _, tc := range fixedproof.Cases() {
		if _, ok := pythonFixedSetups[tc.ID]; ok {
			types = append(types, tc.ID)
		}
	}
	check := func(want, patterns []string) bool {
		for _, name := range want {
			matches := 0
			for _, pattern := range patterns {
				if regexp.MustCompile("^(" + pattern + ")$").MatchString(name) {
					matches++
				}
			}
			if matches != 1 {
				return false
			}
		}
		for _, pattern := range patterns {
			for part := range strings.SplitSeq(pattern, "|") {
				found := false
				for _, name := range want {
					found = found || part == name
				}
				if !found {
					return false
				}
			}
		}
		return len(want) > 0 && len(patterns) > 0
	}
	for _, tc := range []struct {
		name string
		want []string
	}{{"values", pythonFixedValueRows}, {"union", pythonUnionOptionalRows}, {"types", types}} {
		if !check(tc.want, rows[tc.name]) {
			t.Errorf("%s CI rows omit, repeat or add a proof case", tc.name)
		}
		if len(rows[tc.name]) == 0 || check(tc.want, rows[tc.name][1:]) {
			t.Errorf("%s omitted-row control did not fail", tc.name)
		}
	}
}
