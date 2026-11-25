package vuln_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVulnClassifierFixtures is tools/vuln/govulncheck.sh selftest, one
// subtest per fixture line of testdata/cases.txt. The script's classify is
// the check. Nothing here fetches the vulnerability database.
func TestVulnClassifierFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	var cases [][2]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("testdata/cases.txt: %q is not a fixture and a verdict", line)
		}
		cases = append(cases, [2]string{f[0], f[1]})
	}
	if len(cases) == 0 {
		t.Fatal("testdata/cases.txt named no fixtures")
	}
	for _, c := range cases {
		t.Run(c[0], func(t *testing.T) {
			path := filepath.Join("testdata", c[0])
			if _, err := os.ReadFile(path); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("bash", "./govulncheck.sh", "classify", path).Output()
			got := strings.TrimSpace(string(out))
			if err != nil || got != c[1] {
				t.Fatalf("%s -> %q, err %v, expected %s", c[0], got, err, c[1])
			}
		})
	}
}
