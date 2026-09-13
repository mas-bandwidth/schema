package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the workflow's actual selector: a green plan that selects no
// harness must not silently skip a changed C++ fixed-table regression test.
func TestFastPlanSelectsTableFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "          # THE BROAD RULE.")
	if !ok {
		t.Fatal("fast workflow broad selector not found")
	}
	selector, _, ok := strings.Cut(tail, "          # THE PACKAGES.")
	if !ok {
		t.Fatal("fast workflow package boundary not found")
	}
	// Remove the remainder of the first comment line and YAML indentation.
	_, selector, _ = strings.Cut(selector, "\n")
	var script strings.Builder
	for line := range strings.SplitSeq(selector, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`printf '%s|%s' "$broad" "$legs"`)
	for _, tc := range []struct{ name, files, want string }{
		{"cpp witness", "test/tables/versioning_numbers.cpp", "no| cpp"},
		{"cpp header", "test/tables/floatnan.h", "no| cpp"},
		{"shared schema", "test/tables/VNEW_floor.schema", "yes|cpp go c rust js cs java dart elixir"},
		{"documentation", "README.md", "no|"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
			cmd.Env = append(os.Environ(), "files="+tc.files)
			got, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("selector: %v: %s", err, got)
			}
			if string(got) != tc.want {
				t.Fatalf("%s selected %q; want %q", tc.files, got, tc.want)
			}
		})
	}
}
