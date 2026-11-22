package luatable

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"weak"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Generating a unit's table surface and its view keeps nothing about the unit
// once the call returns: a unit the caller has dropped is collectable. A view
// held at package level, keyed by the unit, would keep every unit a long-lived
// process generated, with its emitted view, for ever.
func TestLuaViewKeepsNoUnit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	src := "package probe\n\ntable Probe\n{\n    a int32 = 1\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	wp := func() weak.Pointer[ir.Unit] {
		u := luarun.LoadUnit(t, path)
		if _, err := Generate(u); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		view, ok := ViewFile(u)
		if !ok || !strings.Contains(view, "_M.Package = \"probe\"") {
			t.Fatalf("the view of the probe unit is not built: ok=%v", ok)
		}
		return weak.Make(u)
	}()
	for i := 0; i < 20 && wp.Value() != nil; i++ {
		goruntime.GC()
	}
	if wp.Value() != nil {
		t.Fatal("a unit the caller dropped is still reachable after generating its view: the view outlived its call")
	}
}
