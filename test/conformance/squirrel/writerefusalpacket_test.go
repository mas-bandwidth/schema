package squirrelwriterefusalpacket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// TestWriteRefusalPacketSquirrel holds docs/SPEC.md §5.1's packet writer on
// the Squirrel leg over the shared write-refusal-packet rows: every value the
// declaration does not admit is refused (false, or for a ranged int32 the
// runtime's write-misuse trap), and the lawful value at the same boundary is
// written. The negative control relaxes the emitter's counted-array upper
// bound and requires the boundary case to go red.
func TestWriteRefusalPacketSquirrel(t *testing.T) {
	runWriteRefusalPacketSquirrel(t)

	out, failed := slowtest.RunSabotaged(t, "../../..", "./test/conformance/squirrel",
		"^TestWriteRefusalPacketSquirrelSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/squirrel/write.go",
			From: "%s > %d) return false;\\n\",\n\t\t\tindent, count, count, min, count, bound)",
			To:   "%s >= %d) return false;\\n\",\n\t\t\tindent, count, count, min, count, bound)",
		})
	if !failed {
		t.Fatalf("negative control: the counted-array upper bound moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "FAILED: count_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary case\n%s", out)
	}
}

// TestWriteRefusalPacketSquirrelSurface is the surface alone, so the negative
// control can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketSquirrelSurface(t *testing.T) {
	runWriteRefusalPacketSquirrel(t)
}

// runWriteRefusalPacketSquirrel generates the shared schema for Squirrel,
// stages the vendored runtime and the probe beside it, runs the probe under
// the pinned interpreter (sqrun: a missing interpreter fails by name under
// SCHEMA_SLOW=1, and the test skips without it) and holds its verdicts
// against rows.txt.
func runWriteRefusalPacketSquirrel(t *testing.T) {
	t.Helper()
	sqrun.Require(t)
	root := "../../.."
	dir := wrpacket.Generate(t, root, "squirrel", compiler.Options{})
	for name, data := range squirrel.Serialize() {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe, err := os.ReadFile("writerefusalpacket.nut")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), probe, 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := os.ReadFile(wrpacket.RowsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"rows": rows})
	wrpacket.Check(t, string(out), wrpacket.Rows(t, root), nil)
}
