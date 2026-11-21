package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

// nestedCountMinSchema is the hostile corpus's inner unit in small
// (test/tables/inner): a child whose one bound is a counted minimum, held
// one record down by a variable parent, in a package named `inner`.
const nestedCountMinSchema = `package inner

fixed table Child
{
    vals [2..4]uint8
}

table VarLoose
{
    nest [..4]Child
}

table VarTight
{
    nest [1..4]Child
}
`

// A child's count minimum is a bound of every record that holds the child
// (docs/SPEC.md §5.1 row 3), whatever the holding array's own minimum. The
// variable read decodes every counted array, a child's under a parent array
// included, through one runtime branch, which refuses a count under the
// descriptor's minimum as it refuses one past the bound (the
// inner-variable-*-count-below-min rows of the hostile corpus, which Lobster's
// public reader accepted and only its writer refused). And `inner` is a
// package name the target generates: no runtime local binds it, so the
// corpus's inner unit is answered rather than absent.
func TestLobsterHoldsACountMinimumInsideEachCountedChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Inner.schema")
	if err := os.WriteFile(path, []byte(nestedCountMinSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatalf("Lobster refuses package inner: %v", err)
	}
	var all strings.Builder
	for _, src := range files {
		all.Write(src)
	}
	if !strings.Contains(all.String(), `s: "arr", array: "counted", bound: 4, min: 2,`) {
		t.Error("Child.vals's descriptor does not carry its minimum to the variable read")
	}
	load := string(lobstertable.Runtime()["load.lobster"])
	if !strings.Contains(load, "    if f.array == \"counted\" and count < f.min:\n        return read_refuse(rep, \"count_over_bound\"), false\n") {
		t.Error("load.lobster's counted-array decode does not refuse a count under the descriptor's minimum")
	}
}
