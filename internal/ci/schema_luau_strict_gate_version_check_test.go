package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaLuauStrictGateVersionCheck(t *testing.T) {
	t.Parallel()
	const workflow = ".github/workflows/schema-luau-generated-analyze-strict-gate.yml"
	data, err := os.ReadFile(filepath.Join(repoRoot(t), workflow))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, "luau-analyze --version") || strings.Contains(line, "luau --version") {
			t.Fatalf("%s:%d runs a version probe Luau does not support: %s", workflow, i+1, strings.TrimSpace(line))
		}
	}

	start := -1
	for i, line := range lines {
		if strings.Contains(line, "name: presence check pinned luau-analyze 0.741") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no presence check pinned luau-analyze 0.741 step", workflow)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "      - ") {
			end = i
			break
		}
	}
	body := strings.Join(lines[start:end], "\n")
	for _, want := range []string{
		`want='0.741'`,
		`got=$(readlink -f "$(command -v luau-analyze)")`,
		`*"/luau-$want/"*`,
		`TOOL-MISSING luau-analyze $want ${got:-absent} on $RUNNER_NAME`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the luau presence step does not contain %q", want)
		}
	}
}
