package lobstertable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

func TestStrictOnlyLobsterNoTolerantEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fixedSchema := filepath.Join(dir, "ProbeFixed.schema")
	varSchema := filepath.Join(dir, "ProbeVariable.schema")
	const fixedSrc = `package strictprobe

enum Mode { Alpha, Beta }

fixed table FixedRoot {
	id uint32
	score int32 | min = 0, max = 100
	mode Mode
	title string(16)
}
`
	const varSrc = `package strictprobe

table VariableRoot {
	id uint32
	score int32 | min = 0, max = 100
	label string(16)
	items [..4]int32
}
`
	if err := os.WriteFile(fixedSchema, []byte(fixedSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(varSchema, []byte(varSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{fixedSchema, varSchema})
	if err != nil {
		t.Fatal(err)
	}
	files, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"FixedLoadTolerant",
		"FixedEvolveLoadTolerant",
		"LoadMessagesTolerant",
		"FromJsonTolerant",
		"load_tolerant",
		"json_read_tolerant",
		"tolerant",
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var codeLines []string
		for line := range strings.SplitSeq(string(files[name]), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if idx := strings.Index(line, "//"); idx >= 0 {
				line = line[:idx]
			}
			codeLines = append(codeLines, line)
		}
		src := strings.Join(codeLines, "\n")
		for _, bad := range forbidden {
			if strings.Contains(src, bad) {
				t.Errorf("generated file %s contains forbidden token %q", name, bad)
			}
		}
	}
}
