package lobstertable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Field-shape fixtures exercise variable binary explicitly. Copy their source
// with variable table declarations; the original fixed-schema/form boundary
// remains covered by the unmodified manifest and fixed-form suites.
func generateVariableFixtureUnit(t *testing.T, paths []string) (*loadUnit, error) {
	t.Helper()
	dir := t.TempDir()
	converted := make([]string, 0, len(paths))
	fixed := regexp.MustCompile(`\bfixed[ \t\r\n]+table\b`)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, filepath.Base(path))
		if _, err := os.Stat(target); err == nil {
			t.Fatalf("duplicate fixture basename: %s", path)
		}
		if err := os.WriteFile(target, fixed.ReplaceAll(data, []byte("table")), 0644); err != nil {
			t.Fatal(err)
		}
		converted = append(converted, target)
	}
	return generateLoadUnit(t, converted)
}

// compareReadOutcomes includes refused rows; selecting writable values must
// never silently remove an invalid acceptance from a feature's test corpus.
func compareReadOutcomes(t *testing.T, g *loadUnit, rows []loadRow) unitResult {
	t.Helper()
	result := runUnit(t, g, rows)
	for i, row := range rows {
		if result.lob[2*i] != result.goLine[i] {
			t.Errorf("%s: acceptance %s, reference %s", row.name, result.lob[2*i], result.goLine[i])
		}
		if result.lob[2*i+1] != result.goDump[i] {
			t.Errorf("%s: value differs at %s", row.name, divergence(result.lob[2*i+1], result.goDump[i]))
		}
	}
	return result
}
