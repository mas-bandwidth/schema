package darttable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A record field may be named like one of its Row cursor's own members
// (`view`, `at`, `rowSize`, `rowAlign`). The cursor's members are fixed — every
// walk and the conformance driver use `at` — so the FIELD's Dart spelling takes
// the same underscore suffix a reserved word does (docs/SPEC-TABLES.md §11).
func TestRowFieldNamedLikeCursorMemberDart(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	for _, c := range []struct {
		name string
		u    *ir.Unit
	}{
		{
			name: "collisions",
			u: unitFrom(t, `package probe
type Cursor
{
    x int32
    y int32
}
fixed table Row
{
    at      Cursor
    view    uint32
    rowSize string(16)
    rowAlign [4]int16
}
`),
		},
		{name: "messages", u: loadDirUnit(t, "../../../tables/messages")},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := writeRowMemberUnit(t, c.u)
			cmd := exec.Command(dartBinary(t), "analyze", "--no-fatal-warnings", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil || strings.Contains(string(out), "error -") {
				t.Fatalf("dart analyze %s: %v\n%s", c.name, err, out)
			}
		})
	}
}

// writeRowMemberUnit generates the packet, block, cook and wire libraries for
// one unit into a fresh directory and answers its path.
func writeRowMemberUnit(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("table generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
