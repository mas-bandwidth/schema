package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCountedBornStateElides pins a counted [A..B] at its birth value
// (docs/SPEC-TABLES.md section 2.3, section 22.2): the descriptor carries the
// minimum, the field is born with A default elements, and the save walk
// elides that born state. The old emitter carried no minimum and a [] birth
// value, so a value read from text at the born state saved its field
// (verify-tablegen pin:ci32:fresh:var_json, 31 bytes against the engine 10).
func TestCountedBornStateElides(t *testing.T) {
	t.Parallel()
	schema := filepath.Join(t.TempDir(), "Born.schema")
	src := "package born\ntable VarRoot {\n    few [2..4]int32\n}\n"
	if err := os.WriteFile(schema, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, data := range generateElixir(t, loadUnit(t, schema)) {
		b.Write(data)
	}
	all := b.String()
	for _, want := range []string{"min_count: 2", "default: [0, 0]", "list_born?(f, elements, idx_map, ids, order) or"} {
		if !strings.Contains(all, want) {
			t.Errorf("generated Elixir lacks %q: a counted [2..4] is not born at 2 default elements, or its born state is written", want)
		}
	}
}
