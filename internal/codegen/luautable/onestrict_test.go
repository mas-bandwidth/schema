package luautable_test

import (
	"regexp"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// TestLuauOneStrictRead (docs/SPEC-TABLES.md §6.5): the Luau runtime defines
// exactly one strict read, schema_load.luau's L.ReadStrict, so the plain load,
// LoadText, the fixed form's dispatch and the conformance driver all answer
// the same name for the same bytes.
func TestLuauOneStrictRead(t *testing.T) {
	def := regexp.MustCompile(`(?m)^function \w+\.ReadStrict\(`)
	var where []string
	for name, src := range luautable.Runtime() {
		for range def.FindAllIndex(src, -1) {
			where = append(where, name)
		}
	}
	if len(where) != 1 || where[0] != "schema_load.luau" {
		t.Fatalf("the runtime defines ReadStrict in %v, want exactly once, in schema_load.luau", where)
	}
	if _, ok := luautable.Runtime()["schema_refusal.luau"]; ok {
		t.Fatal("the runtime still emits schema_refusal.luau, a second strict-read module")
	}
}
