package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestThreeRefusalsPrintTheVersionOnce pins that a refusal batch is dated once:
// a unit with three section-citing errors prints 'schema <version>' exactly
// once, immediately after the error count, never once per diagnostic (#458).
func TestThreeRefusalsPrintTheVersionOnce(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	dir := t.TempDir()
	src := "package t\nconst A = B\nconst B = A\ntype C { again C }\ntype D { x uint8 }\nenum D { Y }\n"
	if err := os.WriteFile(filepath.Join(dir, "T.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", dir).CombinedOutput()
	if err == nil {
		t.Fatal("want the check to refuse the unit")
	}
	got := string(out)
	const countLine = "schema: 3 error(s)"
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	countIdx, versionIdx := -1, -1
	for i, ln := range lines {
		switch {
		case ln == countLine:
			countIdx = i
		case strings.HasPrefix(ln, "schema ") && !strings.HasPrefix(ln, "schema:"):
			if versionIdx != -1 {
				t.Fatalf("the version line printed more than once:\n%s", got)
			}
			versionIdx = i
		}
	}
	if countIdx == -1 || versionIdx == -1 {
		t.Fatalf("the version line must follow the error count; got:\n%s", got)
	}
	if versionIdx != countIdx+1 {
		t.Fatalf("the version line must be the line right after the error count; got:\n%s", got)
	}
}
