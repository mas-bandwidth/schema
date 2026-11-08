package squirreltable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// TestStrictOnlySquirrelNoTolerantEntry is the strict-only grep gate for the
// Squirrel leg (docs/plans/strict-only.md layer 4): the generator's output for
// tables/examples and a fixed-form probe schema, the embedded runtime modules,
// and the registered Squirrel names in internal/tablenames/squirrel.go carry
// none of the retired tolerant surface — no Tolerant entry point, no clamped
// (renamed out_of_range), no kindMismatch or duplicate or blobShared counter,
// and no malformed binary verdict on the read report.
func TestStrictOnlySquirrelNoTolerantEntry(t *testing.T) {
	t.Parallel()

	retired := []string{
		"Tolerant",
		"tolerant",
		"clamped",
		"Clamped",
		"kindMismatch",
		"blobShared",
		"duplicate",
		"Duplicate",
	}
	// the binary malformed verdict on a report, as a member: a reason NAME that
	// ends in malformed (layout_malformed, docs/FIXED-FORM-ALGORITHM.md) is not it
	malformedMember := regexp.MustCompile(`(\.malformed\b|(^|[^_A-Za-z"])malformed\s*(=|<-))`)
	// the hand-editable text form's spec name for a key given twice
	// (docs/SPEC-TABLES.md §16.9.4) is not the retired counter
	specNames := strings.NewReplacer("text_duplicate_key", "", "DUPLICATE_KEY", "")
	check := func(where, text string) {
		if m := malformedMember.FindString(text); m != "" {
			t.Errorf("%s: retired strict-only malformed verdict %q present", where, m)
		}
		text = specNames.Replace(text)
		for _, bad := range retired {
			if strings.Contains(text, bad) {
				t.Errorf("%s: retired strict-only name %q present", where, bad)
			}
		}
	}

	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", "..", "tables", "examples")})
	if err != nil {
		t.Fatalf("gather tables/examples: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load tables/examples: %v", err)
	}
	files, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("generate squirrel tables/examples: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generate squirrel tables/examples emitted nothing")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		check("examples/"+name, string(files[name]))
	}

	const fixedSrc = `package strictonly
table Leaf { entries map[int32]int32
    values []int32
    name string(16)
    score int32 | min = 0, max = 100
    ratio float32 }
union Choice { leaf Leaf }
table Holder { inner Leaf
    maybe ?Leaf
    leaves []Leaf
    choice Choice
    head *Leaf }
fixed table Root { a uint16
    b int32 | min = 0, max = 10
    c float32 }
`
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "strictonly.schema")
	if err := os.WriteFile(schemaPath, []byte(fixedSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	fixedUnit, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("load fixed probe: %v", err)
	}
	fixedFiles, err := squirreltable.Generate(fixedUnit)
	if err != nil {
		t.Fatalf("generate squirrel fixed probe: %v", err)
	}
	fixedNames := make([]string, 0, len(fixedFiles))
	for name := range fixedFiles {
		fixedNames = append(fixedNames, name)
	}
	sort.Strings(fixedNames)
	for _, name := range fixedNames {
		check("fixed/"+name, string(fixedFiles[name]))
	}

	namesPath := filepath.Join("..", "..", "tablenames", "squirrel.go")
	data, err := os.ReadFile(namesPath)
	if err != nil {
		t.Fatalf("read %s: %v", namesPath, err)
	}
	check(namesPath, string(data))
}
