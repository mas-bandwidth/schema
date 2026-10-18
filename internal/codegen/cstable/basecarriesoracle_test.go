package cstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCsBaseCarriesTheGoOracle(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatal(err)
	}

	spec := string(data)
	for _, sentence := range []string{
		"THE PLAIN NAME IS THE STRICT READ",
		"base64_not_canonical",
	} {
		if !strings.Contains(spec, sentence) {
			t.Errorf("docs/SPEC-TABLES.md is missing Go's %q text", sentence)
		}
	}
}
