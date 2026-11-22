package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaKindHeldEscapeRefuses (R17: the newer reader reads old data, never
// the other way): kind 31 is held by name and never written, so a reader meets
// it only as damage, exactly as 33 and 34. The shared input carries a kind 31
// field the reader cannot name; the tolerant walk, the plain load, the strict
// read and the retaining load all refuse the whole load. The old reader
// skipped it by its L and read the file.
func TestLuaKindHeldEscapeRefuses(t *testing.T) {
	t.Parallel()
	top := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(top, "test", "tables", "varevo", "VarEvo1.schema"))
	wire, err := os.ReadFile(filepath.Join(top, "testdata", "wire", "tables", "framing", "varevo_kind31_escape.bin"))
	if err != nil {
		t.Fatal(err)
	}
	got := runOrphanCycleProbe(t, u, "EvoDoc", wire, nil)
	const want = "accepted=false plain=false strict=false retain=false "
	if !strings.HasPrefix(got, want) {
		t.Errorf("kind 31 at an unnamed field: got %q, want every load refused (%q)", got, want)
	}
}
