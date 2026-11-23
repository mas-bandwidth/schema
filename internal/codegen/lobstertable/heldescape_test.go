package lobstertable_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLobsterKindHeldEscapeRefuses (R17: the newer reader reads old data,
// never the other way): kind 31 is held by name and never written, so a reader
// meets it only as damage, exactly as 33 and 34. The shared input carries a
// kind 31 field the reader cannot name; the plain load, the strict read and
// the retaining load all refuse the whole load. The old reader skipped it by
// its L and read the file.
func TestLobsterKindHeldEscapeRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated unit)")
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "test", "tables", "varevo", "VarEvo1.schema")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("tblvarevo1: the surface refuses the unit: %v", err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", "varevo_kind31_escape.bin"))
	if err != nil {
		t.Fatal(err)
	}
	rows := []blobKindRow{{name: "held_escape", root: "EvoDoc", read: false, unknown: -1, reason: "wire_damaged"}}
	runBlobKindRead(t, g, rows, map[string][]byte{"held_escape": wire})
}
