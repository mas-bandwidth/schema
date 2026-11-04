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
