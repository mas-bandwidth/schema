package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// TestLobsterStrictReasonRanksDamageFirst pins docs/SPEC-TABLES.md §6.5's
// ranking on strict_reason: damage outranks a missing required field, because
// damage discards the whole load (§4). A report that counts a missing
// required field alone names missing_required, one flagged malformed alone
// names wire_damaged, and one carrying both names wire_damaged.
func TestLobsterStrictReasonRanksDamageFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Req.schema"), []byte(requiredProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("required probe: %v", err)
	}
	var b strings.Builder
	for _, m := range g.mods {
		b.WriteString("import ." + m + "\n")
	}
	b.WriteString(`import .load
def rank(missing:int, malformed:bool):
    let rep = schema_rt.LoadReport {}
    rep.missing_required = missing
    rep.malformed = malformed
    print(schema_rt.strict_reason(rep))
rank(1, false)
rank(0, true)
rank(1, true)
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{})), "\n"), "\n")
	want := []string{"missing_required", "wire_damaged", "wire_damaged"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("strict_reason for missing only, malformed only, both: %q, want %q", got, want)
	}
}
