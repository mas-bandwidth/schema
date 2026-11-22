package luatable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestLuaDriverAnswersAWireRefusal runs the Lua conformance driver's wire
// surface over two rows of the shapes unit (testdata/conformance/tables/
// MANIFEST.txt): bounds_octets_below_min, which every port refuses on the
// wire (a [2..8] count of 1, `wire-refused`), and bounds_ends, which re-saves
// to its own bytes. THE READ'S REFUSAL IS THE ROW'S ANSWER whatever its reason
// (docs/rulings.json refusal-verdict-only): the driver writes <case>.absent
// naming the reason, as test/conformance/go/main.go does, and goes on to the
// next row. Before, it wrote nothing for a refusal other than blob_shared, and
// the harness failed the row "expected wire refusal, driver wrote nothing".
func TestLuaDriverAnswersAWireRefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	root := repoRoot(t)
	u := luarun.LoadUnit(t, filepath.Join(root, "test", "tables", "shapes"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate test/tables/shapes: %v", err)
	}
	gen := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gen, "shapes"), 0o755); err != nil {
		t.Fatal(err)
	}
	luarun.WriteFiles(t, filepath.Join(gen, "shapes"), files)
	manifest := filepath.Join(t.TempDir(), "MANIFEST.txt")
	rows := "unit shapes test/tables/shapes\n" +
		"instance shapes_bounds_octets_below_min shapes Bounds testdata/wire/tables/shapes/bounds_octets_below_min.bin no-text wire-refused\n" +
		"instance shapes_bounds_ends shapes Bounds testdata/wire/tables/shapes/bounds_ends.bin\n"
	if err := os.WriteFile(manifest, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	cmd := testproc.Command(t, filepath.Join(root, "test", "conformance", "lua", "driver"), manifest, "wire", out)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "LUA="+luarun.Interpreter(t), "LUA_CONFORMANCE_GENERATED="+gen)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the driver failed: %v\n%s", err, b)
	}
	if _, err := os.Stat(filepath.Join(out, "shapes_bounds_octets_below_min")); err == nil {
		t.Fatal("the driver wrote bytes for a wire the read refuses")
	}
	reason, err := os.ReadFile(filepath.Join(out, "shapes_bounds_octets_below_min.absent"))
	if err != nil {
		t.Fatal("the driver wrote no .absent for the read's refusal: the row's answer is missing")
	}
	if len(reason) == 0 {
		t.Fatal("the .absent is empty, which the harness reads as a missing codec, not a refusal")
	}
	want, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "shapes", "bounds_ends.bin"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "shapes_bounds_ends"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("bounds_ends re-saved as %x (%v), want its own %x", got, err, want)
	}
}
