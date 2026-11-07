package cpptable

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestStrictOnlyCppNoTolerantEntry is the strict-only grep gate for the C++
// leg (docs/plans/strict-only.md): the generator's output for the leg's
// example schema and internal/tablenames/cpp.go carry none of the retired
// names — no Tolerant entry point, no clamped (renamed out_of_range), no
// kind_mismatch counter, no duplicate counter and no malformed binary
// verdict. kind_mismatch and malformed survive only as refusal reasons.
func TestStrictOnlyCppNoTolerantEntry(t *testing.T) {
	t.Parallel()

	src := `package probe

enum Grade
{
    A
    B
    C
}

table Inner
{
    a int32
    b string(8)
}

union Payload
{
    Num int32
    Txt string(16)
}

table Root
{
    x int32
    y float64
    s string(16)
    inner Inner
    nums [4]int32
    shade Grade
    payload Payload
    tags [..3]string(8)
}

fixed table FixedRoot
{
    x int32
    y float64
    s string(16)
}
`
	u := loadUnitFromSource(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	forbidden := []string{
		"Tolerant",
		"tolerant",
		"clamped",
		"Clamped",
		"duplicate",
		"Duplicate",
		// kind_mismatch as a counter or report member, not as a reason.
		"kind_mismatch++",
		"kind_mismatch +=",
		"->kind_mismatch",
		".kind_mismatch",
		"int32_t kind_mismatch",
		// malformed as a binary verdict or report member, not as a reason.
		"bool malformed",
		"->malformed",
		".malformed",
		"malformed_before",
	}
	check := func(where, text string) {
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s: retired strict-only name %q present", where, bad)
			}
		}
	}
	for _, name := range names {
		check(name, string(files[name]))
	}

	namesPath := filepath.Join("..", "..", "tablenames", "cpp.go")
	data, err := os.ReadFile(namesPath)
	if err != nil {
		t.Fatalf("read %s: %v", namesPath, err)
	}
	check(namesPath, string(data))

	// The hostile corpus (test/cpp-hostile) binds the generated API by
	// hand, so it is scanned too: a retired name there is a compile
	// failure in CI, not a gate failure, unless this scan catches it.
	corpusDir := filepath.Join("..", "..", "..", "test", "cpp-hostile")
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("read %s: %v", corpusDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(corpusDir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		check(p, string(b))
	}
}
