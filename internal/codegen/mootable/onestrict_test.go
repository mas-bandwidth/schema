package mootable_test

import (
	"regexp"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

// TestMooOneStrictRead (docs/SPEC-TABLES.md §6.5): the moo runtime defines
// exactly one strict read, table_load.moo's ReadStrict, so the plain load,
// the text read and the conformance driver all answer the same name for the
// same bytes.
func TestMooOneStrictRead(t *testing.T) {
	def := regexp.MustCompile(`(?m)^function \w+\.ReadStrict\(`)
	var where []string
	for name, src := range mootable.Runtime() {
		for range def.FindAllIndex(src, -1) {
			where = append(where, name)
		}
	}
	if len(where) != 1 || where[0] != "table_load.moo" {
		t.Fatalf("the runtime defines ReadStrict in %v, want exactly once, in table_load.moo", where)
	}
}
